/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metal3io

import (
	goctx "context"
	"fmt"

	"github.com/go-logr/logr"
	ipamv1 "github.com/metal3-io/ip-address-manager/api/v1alpha1"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam"
	ipamutil "github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam/util"
)

type Metal3IPAM struct {
	client.Client
	logger logr.Logger
}

func NewIpam(client client.Client, logger logr.Logger) ipam.IPAddressManager {
	return &Metal3IPAM{
		Client: client,
		logger: logger,
	}
}

func (m *Metal3IPAM) GetIP(ctx goctx.Context, ipName string, ipPool ipam.IPPool) (ipam.IPAddress, error) {
	ipClaim, err := m.getIPClaim(ctx, ipPool, ipName)
	if err != nil {
		m.logger.Info(fmt.Sprintf("failed to get IPClaim %s", ipName))
		return nil, err
	}

	if ipClaim == nil || ipClaim.Status.Address == nil {
		m.logger.Info(fmt.Sprintf("Waiting for IPClaim %s", ipName))
		return nil, nil
	}

	var ip ipamv1.IPAddress
	if err := m.Client.Get(ctx, apitypes.NamespacedName{
		Namespace: ipPool.GetNamespace(),
		Name:      ipClaim.Status.Address.Name,
	}, &ip); err != nil {
		return nil, errors.Wrapf(err, "failed to get IPAddress %s", ipClaim.Status.Address.Name)
	}

	return toIPAddress(ip), nil
}

func (m *Metal3IPAM) AllocateIP(ctx goctx.Context, ipName string, pool ipam.IPPool, owner metav1.Object) (ipam.IPAddress, error) {
	ipClaim, err := m.getIPClaim(ctx, pool, ipName)
	if err != nil {
		m.logger.Info(fmt.Sprintf("failed to get IPClaim %s", ipName))
		return nil, err
	}

	// if IPClaim exists, the corresponding IPAddress is expected to be generated
	if ipClaim != nil {
		m.logger.V(2).Info(fmt.Sprintf("IPClaim %s already exists, skipping creation", ipName))
		return nil, nil
	}

	// create a new ip claim
	if err = m.createIPClaim(ctx, pool, ipName, owner); err != nil {
		return nil, err
	}

	return nil, nil
}

func (m *Metal3IPAM) ReleaseIP(ctx goctx.Context, ipName string, pool ipam.IPPool) error {
	ipClaim, err := m.getIPClaim(ctx, pool, ipName)
	if err != nil {
		return err
	}
	if ipClaim == nil {
		return nil
	}

	if err := m.Client.Delete(ctx, ipClaim); err != nil {
		message := fmt.Sprintf("failed to delete IPClaim %s", ipName)
		m.logger.Error(err, message)

		return errors.Wrapf(err, message)
	}

	m.logger.Info(fmt.Sprintf("IPClaim %s already deleted", ipName))

	return nil
}

func (m *Metal3IPAM) ReleaseIPs(ctx goctx.Context, owner metav1.Object, pool ipam.IPPool) (int, error) {
	ipClaimList := &ipamv1.IPClaimList{}
	labels := map[string]string{ipam.IPOwnerNameKey: fmt.Sprintf("%s-%s", owner.GetNamespace(), owner.GetName())}

	if err := m.Client.List(ctx, ipClaimList, client.InNamespace(pool.GetNamespace()), client.MatchingLabels(labels)); err != nil {
		return 0, err
	}

	for i := 0; i < len(ipClaimList.Items); i++ {
		if err := m.Client.Delete(ctx, &ipClaimList.Items[i]); err != nil {
			return 0, errors.Wrapf(err, "failed to delete IPClaim %s", ipClaimList.Items[i].Name)
		}

		m.logger.Info(fmt.Sprintf("IPClaim %s already deleted", ipClaimList.Items[i].Name))
	}

	return len(ipClaimList.Items), nil
}

func (m *Metal3IPAM) GetAvailableIPPool(ctx goctx.Context, poolMatchLabels map[string]string, clusterMeta metav1.ObjectMeta) (ipam.IPPool, error) {
	poolNamespace := getIPPoolNamespace(poolMatchLabels, clusterMeta)

	// if the specific ip-pool name is provided use that to get the ip-pool
	var ipPool ipamv1.IPPool
	if poolName, ok := poolMatchLabels[ipam.ClusterIPPoolNameKey]; ok && poolName != "" {
		if err := m.Get(ctx, apitypes.NamespacedName{
			Namespace: poolNamespace,
			Name:      poolName,
		}, &ipPool); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil
			}

			return nil, errors.Wrapf(err, "failed to get IPPool %s/%s", poolNamespace, poolName)
		}

		return toIPPool(ipPool), nil
	}

	matchLabels := map[string]string{}

	// use labels 'ip-pool-group' & 'network-name' to select the ip-pool
	if label, ok := poolMatchLabels[ipam.ClusterIPPoolGroupKey]; ok && label != "" {
		matchLabels[ipam.ClusterIPPoolGroupKey] = label
	}
	if label, ok := poolMatchLabels[ipam.ClusterNetworkNameKey]; ok && label != "" {
		matchLabels[ipam.ClusterNetworkNameKey] = label
	}

	// use default ip-pool
	if len(matchLabels) == 0 {
		poolNamespace = ipam.DefaultIPPoolNamespace
		matchLabels[ipam.DefaultIPPoolKey] = "true"
	}

	ipPoolList := &ipamv1.IPPoolList{}
	if err := m.List(
		ctx,
		ipPoolList,
		client.InNamespace(poolNamespace),
		client.MatchingLabels(matchLabels)); err != nil {
		return nil, err
	}

	if len(ipPoolList.Items) == 0 {
		m.logger.Info("failed to get a matching IPPool")
		return nil, nil
	}
	ipPool = ipPoolList.Items[0]

	m.logger.Info(fmt.Sprintf("IPPool %s is available", ipPool.Name))

	return toIPPool(ipPool), nil
}

func (m *Metal3IPAM) getIPClaim(ctx goctx.Context, pool ipam.IPPool, claimName string) (*ipamv1.IPClaim, error) {
	var ipClaim ipamv1.IPClaim
	if err := m.Client.Get(ctx, apitypes.NamespacedName{
		Namespace: pool.GetNamespace(),
		Name:      claimName,
	}, &ipClaim); err != nil {
		return nil, ipamutil.IgnoreNotFound(err)
	}

	return &ipClaim, nil
}

func (m *Metal3IPAM) createIPClaim(ctx goctx.Context, pool ipam.IPPool, claimName string, owner metav1.Object) error {
	m.logger.Info(fmt.Sprintf("Creating IPClaim %s", claimName))

	var ipPool ipamv1.IPPool
	if err := m.Client.Get(ctx, apitypes.NamespacedName{
		Namespace: pool.GetNamespace(),
		Name:      pool.GetName()}, &ipPool); err != nil {
		m.logger.Info(fmt.Sprintf("failed to get IPPool %s", pool.GetName()))
		return ipamutil.IgnoreNotFound(err)
	}

	ipClaim := &ipamv1.IPClaim{
		TypeMeta: metav1.TypeMeta{
			Kind:       "IPClaim",
			APIVersion: ipamv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      claimName,
			Namespace: pool.GetNamespace(),
		},
		Spec: ipamv1.IPClaimSpec{
			Pool: ipamutil.GetObjRef(&ipPool),
		},
	}

	if owner != nil {
		ipClaim.Labels = map[string]string{ipam.IPOwnerNameKey: fmt.Sprintf("%s-%s", owner.GetNamespace(), owner.GetName())}
	}

	if err := m.Client.Create(ctx, ipClaim); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return errors.Wrapf(err, "failed to create IPClaim %s", claimName)
		}
	}

	m.logger.V(2).Info(fmt.Sprintf("Created IPClaim %s. Waiting for IPAddress to be available", claimName))

	return nil
}

func getIPPoolNamespace(labels map[string]string, meta metav1.ObjectMeta) string {
	if namespace, ok := labels[ipam.ClusterIPPoolNamespaceKey]; ok && namespace != "" {
		return namespace
	}

	// default to cluster namespace
	return meta.Namespace
}
