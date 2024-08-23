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
	"net"

	ipamv1 "github.com/metal3-io/ip-address-manager/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"

	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam"
)

type Metal3IP struct {
	ipamv1.IPAddress
}

func NewIP(ipAddress ipamv1.IPAddress) ipam.IPAddress {
	return &Metal3IP{
		IPAddress: ipAddress,
	}
}

func (m *Metal3IP) GetName() string {
	return m.Name
}

func (m *Metal3IP) GetClaim() *corev1.ObjectReference {
	return &m.Spec.Claim
}

func (m *Metal3IP) GetPool() corev1.ObjectReference {
	return m.Spec.Pool
}

func (m *Metal3IP) GetMask() string {
	mask := net.CIDRMask(m.Spec.Prefix, 32)
	if len(mask) == 0 {
		return ""
	}

	return net.IP(mask).String()
}

func (m *Metal3IP) GetGateway() string {
	if m.Spec.Gateway == nil {
		return ""
	}

	return string(*m.Spec.Gateway)
}

func (m *Metal3IP) GetAddress() string {
	return string(m.Spec.Address)
}

func (m *Metal3IP) GetDNSServers() []string {
	dnsServers := make([]string, 0, len(m.Spec.DNSServers))
	for i := range len(m.Spec.DNSServers) {
		dnsServers = append(dnsServers, string(m.Spec.DNSServers[i]))
	}

	return dnsServers
}
