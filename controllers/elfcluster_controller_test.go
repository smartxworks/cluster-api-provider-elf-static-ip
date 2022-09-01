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

package controllers

import (
	"bytes"
	goctx "context"
	"flag"
	"fmt"
	"time"

	ipamv1 "github.com/metal3-io/ip-address-manager/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	capiv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam"
	ipamutil "github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam/util"
	"github.com/smartxworks/cluster-api-provider-elf-static-ip/test/fake"
)

var _ = Describe("ElfClusterReconciler", func() {
	var (
		logBuffer       *bytes.Buffer
		elfCluster      *capev1.ElfCluster
		cluster         *capiv1.Cluster
		metal3IPPool    *ipamv1.IPPool
		metal3IPClaim   *ipamv1.IPClaim
		metal3IPAddress *ipamv1.IPAddress
	)

	ctx := goctx.Background()

	BeforeEach(func() {
		// set log
		if err := flag.Set("logtostderr", "false"); err != nil {
			_ = fmt.Errorf("Error setting logtostderr flag")
		}
		if err := flag.Set("v", "6"); err != nil {
			_ = fmt.Errorf("Error setting v flag")
		}
		klog.SetOutput(GinkgoWriter)
		logBuffer = new(bytes.Buffer)

		elfCluster, cluster, _, _, _ = fake.NewClusterAndMachineObjects()
		metal3IPPool = fake.NewMetal3IPPool()
	})

	Context("Reconcile an ElfCluster", func() {
		It("should not reconcile when ElfCluster not found", func() {
			klog.SetOutput(logBuffer)
			ctrlContext := newCtrlContexts()

			reconciler := &ElfClusterReconciler{ctrlContext}

			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).To(BeNil())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("ElfCluster not found, won't reconcile"))
		})

		It("should not error and not requeue the request without cluster", func() {
			klog.SetOutput(logBuffer)
			ctrlContext := newCtrlContexts(elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ctrlContext}

			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).To(BeNil())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for Cluster Controller to set OwnerRef on ElfCluster"))
		})

		It("should not error and not requeue the request when Cluster is paused", func() {
			klog.SetOutput(logBuffer)
			cluster.Spec.Paused = true
			ctrlContext := newCtrlContexts(elfCluster, cluster)
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ctrlContext}
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).To(BeNil())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("ElfCluster linked to a cluster that is paused"))
		})

		It("should not reconcile when no need to allocate static IP", func() {
			klog.SetOutput(logBuffer)
			elfCluster.Spec.ControlPlaneEndpoint.Host = fake.IP()
			ctrlContext := newCtrlContexts(elfCluster, cluster)
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ctrlContext}
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).To(BeNil())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("no need to allocate static IP for control plane endpoint"))
		})

		It("should not reconcile when no IPPool", func() {
			klog.SetOutput(logBuffer)
			ctrlContext := newCtrlContexts(elfCluster, cluster)
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ctrlContext}
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).To(BeNil())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("waiting for IPPool to be available"))
			Expect(ctrlContext.Client.Get(ctrlContext, capiutil.ObjectKey(elfCluster), elfCluster)).To(Succeed())
			Expect(ctrlutil.ContainsFinalizer(elfCluster, ClusterStaticIPFinalizer)).To(BeFalse())
		})

		It("should create IPClaim and wait when no IPClaim", func() {
			klog.SetOutput(logBuffer)
			elfCluster.Labels = map[string]string{ipam.ClusterIPPoolNameKey: metal3IPPool.Name}
			ctrlContext := newCtrlContexts(elfCluster, cluster, metal3IPPool)
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ctrlContext}
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).To(BeNil())
			Expect(result.RequeueAfter).To(Equal(config.DefaultRequeue))
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("waiting for IP address %s to be available", ipamutil.GetControlPlaneEndpointClaimName(elfCluster.Name))))
			var ipClaim ipamv1.IPClaim
			Expect(ctrlContext.Client.Get(ctrlContext, apitypes.NamespacedName{
				Namespace: metal3IPPool.Namespace,
				Name:      ipamutil.GetControlPlaneEndpointClaimName(elfCluster.Name),
			}, &ipClaim)).To(Succeed())
			Expect(ipClaim.Spec.Pool.Name).To(Equal(metal3IPPool.Name))
			Expect(ctrlContext.Client.Get(ctrlContext, capiutil.ObjectKey(elfCluster), elfCluster)).To(Succeed())
			Expect(ctrlutil.ContainsFinalizer(elfCluster, ClusterStaticIPFinalizer)).To(BeFalse())
		})

		It("should wait for IP when IPClaim does not get IP yet", func() {
			klog.SetOutput(logBuffer)
			metal3IPPool.Labels = map[string]string{
				ipam.ClusterIPPoolGroupKey: "ip-pool-group",
				ipam.ClusterNetworkNameKey: "ip-pool-vm-network",
			}
			elfCluster.Labels = metal3IPPool.Labels
			metal3IPClaim, metal3IPAddress = fake.NewMetal3IPObjects(metal3IPPool, ipamutil.GetControlPlaneEndpointClaimName(elfCluster.Name))
			ctrlContext := newCtrlContexts(elfCluster, cluster, metal3IPPool, metal3IPClaim, metal3IPAddress)
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ctrlContext}
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).To(BeNil())
			Expect(result.RequeueAfter).To(Equal(config.DefaultRequeue))
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("IPClaim %s already exists, skipping creation", ipamutil.GetControlPlaneEndpointClaimName(elfCluster.Name))))
			Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("waiting for IP address %s to be available", ipamutil.GetControlPlaneEndpointClaimName(elfCluster.Name))))
			Expect(ctrlContext.Client.Get(ctrlContext, capiutil.ObjectKey(elfCluster), elfCluster)).To(Succeed())
			Expect(ctrlutil.ContainsFinalizer(elfCluster, ClusterStaticIPFinalizer)).To(BeFalse())
		})

		It("should set IP when IPClaim gets IP", func() {
			klog.SetOutput(logBuffer)
			metal3IPPool.Namespace = ipam.DefaultIPPoolNamespace
			metal3IPPool.Labels = map[string]string{ipam.DefaultIPPoolKey: "true"}
			metal3IPClaim, metal3IPAddress = fake.NewMetal3IPObjects(metal3IPPool, ipamutil.GetControlPlaneEndpointClaimName(elfCluster.Name))
			setMetal3IPForClaim(metal3IPClaim, metal3IPAddress)
			ctrlContext := newCtrlContexts(elfCluster, cluster, metal3IPPool, metal3IPClaim, metal3IPAddress)
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ctrlContext}
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(err).To(BeNil())
			Expect(result).To(BeZero())
			Expect(logBuffer.String()).To(ContainSubstring("Set control plane endpoint successfully"))
			Expect(ctrlContext.Client.Get(ctrlContext, capiutil.ObjectKey(elfCluster), elfCluster)).To(Succeed())
			Expect(elfCluster.Spec.ControlPlaneEndpoint.Host).To(Equal(string(metal3IPAddress.Spec.Address)))
			Expect(ctrlutil.ContainsFinalizer(elfCluster, ClusterStaticIPFinalizer)).To(BeTrue())
		})
	})

	Context("Delete a ElfCluster", func() {
		BeforeEach(func() {
			ctrlutil.AddFinalizer(elfCluster, capev1.ClusterFinalizer)
			ctrlutil.AddFinalizer(elfCluster, ClusterStaticIPFinalizer)
			elfCluster.DeletionTimestamp = &metav1.Time{Time: time.Now().UTC()}
		})

		It("should delete normally after ClusterFinalizer was removed", func() {
			ctrlutil.RemoveFinalizer(elfCluster, capev1.ClusterFinalizer)
			ctrlContext := newCtrlContexts(elfCluster, cluster)
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ctrlContext}
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(result).To(BeZero())
			Expect(err).To(BeNil())
			Expect(apierrors.IsNotFound(ctrlContext.Client.Get(ctrlContext, capiutil.ObjectKey(elfCluster), elfCluster))).To(BeTrue())
		})

		It("should not reconcile when without ClusterFinalizer", func() {
			ctrlutil.RemoveFinalizer(elfCluster, ClusterStaticIPFinalizer)
			elfCluster.Spec.ControlPlaneEndpoint.Host = string(metal3IPAddress.Spec.Address)
			ctrlContext := newCtrlContexts(elfCluster, cluster)
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ctrlContext}
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(result).To(BeZero())
			Expect(err).To(BeNil())
			Expect(ctrlContext.Client.Get(ctrlContext, capiutil.ObjectKey(elfCluster), elfCluster)).To(Succeed())
			Expect(ctrlutil.ContainsFinalizer(elfCluster, ClusterStaticIPFinalizer)).To(BeFalse())
		})

		It("should remove ClusterStaticIPFinalizer and delete related IPs", func() {
			klog.SetOutput(logBuffer)
			metal3IPClaim, metal3IPAddress = fake.NewMetal3IPObjects(metal3IPPool, ipamutil.GetControlPlaneEndpointClaimName(elfCluster.Name))
			elfCluster.Spec.ControlPlaneEndpoint.Host = string(metal3IPAddress.Spec.Address)
			ctrlContext := newCtrlContexts(elfCluster, cluster, metal3IPPool, metal3IPClaim, metal3IPAddress)
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ctrlContext}
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(result).To(BeZero())
			Expect(err).To(BeNil())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for ClusterFinalizer to be removed"))
			Expect(ctrlContext.Client.Get(ctrlContext, capiutil.ObjectKey(elfCluster), elfCluster)).To(Succeed())
			Expect(ctrlutil.ContainsFinalizer(elfCluster, ClusterStaticIPFinalizer)).To(BeTrue())
		})

		It("should remove ClusterStaticIPFinalizer and delete related IPs", func() {
			klog.SetOutput(logBuffer)
			ctrlutil.RemoveFinalizer(elfCluster, capev1.ClusterFinalizer)
			metal3IPPool.Namespace = ipam.DefaultIPPoolNamespace
			metal3IPPool.Labels = map[string]string{ipam.DefaultIPPoolKey: "true"}
			metal3IPClaim, metal3IPAddress = fake.NewMetal3IPObjects(metal3IPPool, ipamutil.GetControlPlaneEndpointClaimName(elfCluster.Name))
			setMetal3IPForClaim(metal3IPClaim, metal3IPAddress)
			elfCluster.Spec.ControlPlaneEndpoint.Host = string(metal3IPAddress.Spec.Address)
			ctrlContext := newCtrlContexts(elfCluster, cluster, metal3IPPool, metal3IPClaim, metal3IPAddress)
			fake.InitClusterOwnerReferences(ctrlContext, elfCluster, cluster)

			reconciler := &ElfClusterReconciler{ctrlContext}
			result, err := reconciler.Reconcile(goctx.Background(), ctrl.Request{NamespacedName: capiutil.ObjectKey(elfCluster)})
			Expect(result).To(BeZero())
			Expect(err).To(BeNil())
			Expect(apierrors.IsNotFound(ctrlContext.Client.Get(ctrlContext, capiutil.ObjectKey(elfCluster), elfCluster))).To(BeTrue())
		})
	})
})
