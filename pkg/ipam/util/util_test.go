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

package util

import (
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

func TestLimitDNSServers(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	testCases := []struct {
		name             string
		sourceDNSServers []string
		dnsServers       []string
	}{
		{"should return empty server", []string{}, []string{}},
		{"should return one server", []string{"1.1.1.1"}, []string{"1.1.1.1"}},
		{"should filter duplicate servers", []string{"1.1.1.1", "1.1.1.1"}, []string{"1.1.1.1"}},
		{"should limit servers", []string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4"}, []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dnsServers := LimitDNSServers(tc.sourceDNSServers)
			g.Expect(dnsServers).To(gomega.Equal(tc.dnsServers))
		})
	}
}

func TestIsMetal3IPPoolRef(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	t.Run("should return true if ref is Metal3 IPPool", func(t *testing.T) {
		g.Expect(IsMetal3IPPoolRef(corev1.TypedLocalObjectReference{})).To(gomega.BeTrue())
		g.Expect(IsMetal3IPPoolRef(corev1.TypedLocalObjectReference{
			APIGroup: ptr.To("ipam.metal3.io"),
			Kind:     "IPPool",
		})).To(gomega.BeTrue())
		g.Expect(IsMetal3IPPoolRef(corev1.TypedLocalObjectReference{
			APIGroup: ptr.To("ipam.metal3.io"),
			Kind:     "XPool",
		})).To(gomega.BeFalse())
	})
}
