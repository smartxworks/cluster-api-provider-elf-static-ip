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
	ipamv1 "github.com/metal3-io/ip-address-manager/api/v1alpha1"

	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam"
)

type Metal3IPPool struct {
	ipamv1.IPPool
}

func NewIPPool(pool ipamv1.IPPool) ipam.IPPool {
	return &Metal3IPPool{
		IPPool: pool,
	}
}

func (m *Metal3IPPool) GetName() string {
	return m.Name
}

func (m *Metal3IPPool) GetNamespace() string {
	return m.Namespace
}

func (m *Metal3IPPool) GetClusterName() string {
	if m.IPPool.Spec.ClusterName == nil {
		return ""
	}

	return *m.IPPool.Spec.ClusterName
}

func (m *Metal3IPPool) GetPools() []ipam.Pool {
	return toPools(m.IPPool.Spec.Pools)
}

func (m *Metal3IPPool) GetPreAllocations() map[string]string {
	preAllocations := make(map[string]string)
	for k, v := range m.IPPool.Spec.PreAllocations {
		preAllocations[k] = string(v)
	}

	return preAllocations
}

func (m *Metal3IPPool) GetPrefix() int {
	return m.IPPool.Spec.Prefix
}

func (m *Metal3IPPool) GetGateway() string {
	if m.IPPool.Spec.Gateway == nil {
		return ""
	}

	return string(*m.IPPool.Spec.Gateway)
}

func (m *Metal3IPPool) GetDNSServers() []string {
	dnsServers := make([]string, 0, len(m.IPPool.Spec.DNSServers))
	for i := range len(m.IPPool.Spec.DNSServers) {
		dnsServers = append(dnsServers, string(m.IPPool.Spec.DNSServers[i]))
	}

	return dnsServers
}

func (m *Metal3IPPool) GetNamePrefix() string {
	return m.IPPool.Spec.NamePrefix
}

type Metal3Pool struct {
	ipamv1.Pool
}

func NewPool(pool ipamv1.Pool) ipam.Pool {
	return &Metal3Pool{
		Pool: pool,
	}
}

func (m *Metal3Pool) GetStart() string {
	if m.Start == nil {
		return ""
	}

	return string(*m.Start)
}

func (m *Metal3Pool) GetEnd() string {
	if m.End == nil {
		return ""
	}

	return string(*m.End)
}

func (m *Metal3Pool) GetSubnet() string {
	if m.Subnet == nil {
		return ""
	}

	return string(*m.Subnet)
}

func (m *Metal3Pool) GetPrefix() int {
	return m.Prefix
}

func (m *Metal3Pool) GetGateway() string {
	if m.Gateway == nil {
		return ""
	}

	return string(*m.Gateway)
}

func (m *Metal3Pool) GetDNSServers() []string {
	dnsServers := make([]string, 0, len(m.DNSServers))
	for i := range len(m.DNSServers) {
		dnsServers = append(dnsServers, string(m.DNSServers[i]))
	}

	return dnsServers
}
