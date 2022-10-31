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
	"fmt"

	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam"
)

// Elf virtual machine support three DNS servers.
const (
	DNSServerLimit = 3
)

func HasStaticIPDevice(devices []capev1.NetworkDeviceSpec) bool {
	for _, device := range devices {
		if IsStaticIPDevice(device) {
			return true
		}
	}

	return false
}

func IsStaticIPDevice(device capev1.NetworkDeviceSpec) bool {
	return device.NetworkType == capev1.NetworkTypeIPV4
}

func NeedsAllocateIP(devices []capev1.NetworkDeviceSpec) bool {
	for _, device := range devices {
		if NeedsAllocateIPForDevice(device) {
			return true
		}
	}

	return false
}

func NeedsAllocateIPForDevice(device capev1.NetworkDeviceSpec) bool {
	if device.NetworkType == capev1.NetworkTypeIPV4 && len(device.IPAddrs) == 0 {
		return true
	}

	return false
}

func ValidateIP(ip ipam.IPAddress) error {
	if ip.GetAddress() == "" {
		return fmt.Errorf("invalid 'address' in IPAddress")
	}

	if ip.GetMask() == "" {
		return fmt.Errorf("invalid 'mask' in IPAddress")
	}

	if ip.GetGateway() == "" {
		return fmt.Errorf("invalid 'gateway' in IPAddress")
	}

	return nil
}

func LimitDNSServers(sourceDNSServers []string) []string {
	dnsServers := []string{}
	set := make(map[string]struct{}, len(sourceDNSServers))
	for i := 0; i < len(sourceDNSServers); i++ {
		if _, ok := set[sourceDNSServers[i]]; ok {
			continue
		}

		dnsServers = append(dnsServers, sourceDNSServers[i])
		set[sourceDNSServers[i]] = struct{}{}
	}

	limit := DNSServerLimit
	if limit > len(dnsServers) {
		limit = len(dnsServers)
	}

	return dnsServers[:limit]
}

func IgnoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}

	return err
}

func GetObjRef(obj runtime.Object) corev1.ObjectReference {
	m, err := meta.Accessor(obj)
	if err != nil {
		return corev1.ObjectReference{}
	}

	v, kind := obj.GetObjectKind().GroupVersionKind().ToAPIVersionAndKind()
	return corev1.ObjectReference{
		APIVersion: v,
		Kind:       kind,
		Namespace:  m.GetNamespace(),
		Name:       m.GetName(),
		UID:        m.GetUID(),
	}
}

func GetFormattedClaimName(ownerNamespace, ownerName string, deviceCount int) string {
	return fmt.Sprintf("%s-%s-%d", ownerNamespace, ownerName, deviceCount)
}
