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

func toIPAddress(mIP ipamv1.IPAddress) ipam.IPAddress {
	return NewIP(mIP)
}

func toIPPool(mIPPool ipamv1.IPPool) ipam.IPPool {
	return NewIPPool(mIPPool)
}

func toPools(pool []ipamv1.Pool) []ipam.Pool {
	ipamPools := []ipam.Pool{}
	for _, p := range pool {
		ipamPool := toPool(p)
		ipamPools = append(ipamPools, ipamPool)
	}

	return ipamPools
}

func toPool(mPool ipamv1.Pool) ipam.Pool {
	return NewPool(mPool)
}
