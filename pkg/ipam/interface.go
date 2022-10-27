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

package ipam

import (
	goctx "context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type IPAddressManager interface {
	// GetIP gets the allocated static ip by name
	GetIP(ctx goctx.Context, name string, pool IPPool) (IPAddress, error)

	// AllocateIP requests a new static ip for the resource, if it does not exist
	// source ip pool is fetched using optional poolSelector, default is using poolKey
	AllocateIP(ctx goctx.Context, name string, pool IPPool, owner metav1.Object) (IPAddress, error)

	// ReleaseIP releases static ip back to the ip pool
	ReleaseIP(ctx goctx.Context, name string, pool IPPool) error

	// ReleaseIPs releases static ips back to the ip pool
	ReleaseIPs(ctx goctx.Context, owner metav1.Object, pool IPPool) (int, error)

	// GetAvailableIPPool gets an available ip pool in the cluster namespace
	GetAvailableIPPool(ctx goctx.Context, poolMatchLabels map[string]string, clusterMeta metav1.ObjectMeta) (IPPool, error)
}

type IPAddress interface {
	GetName() string
	GetClaim() *corev1.ObjectReference
	GetPool() corev1.ObjectReference
	GetMask() string
	GetGateway() string
	GetAddress() string
	GetDNSServers() []string
}

type IPPool interface {
	GetName() string
	GetNamespace() string
	GetClusterName() string
	GetPools() []Pool
	GetPreAllocations() map[string]string
	GetPrefix() int
	GetGateway() string
	GetDNSServers() []string
	GetNamePrefix() string
}

type Pool interface {
	GetStart() string
	GetEnd() string
	GetSubnet() string
	GetPrefix() int
	GetGateway() string
	GetDNSServers() []string
}
