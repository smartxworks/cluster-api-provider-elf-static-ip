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

package fake

import (
	ipamv1 "github.com/metal3-io/ip-address-manager/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/storage/names"

	ipamutil "github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam/util"
)

func NewMetal3IPPool() *ipamv1.IPPool {
	return &ipamv1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: Namespace,
			Name:      names.SimpleNameGenerator.GenerateName("ippool-"),
		},
		Spec: ipamv1.IPPoolSpec{},
	}
}

func NewMetal3IPObjects(pool *ipamv1.IPPool, claimName string) (*ipamv1.IPClaim, *ipamv1.IPAddress) {
	claim := &ipamv1.IPClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pool.Namespace,
			Name:      claimName,
		},
		Spec: ipamv1.IPClaimSpec{
			Pool: ipamutil.GetObjRef(pool),
		},
	}

	gateway := ipamv1.IPAddressStr(IP())
	ip := &ipamv1.IPAddress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pool.Namespace,
			Name:      claimName,
		},
		Spec: ipamv1.IPAddressSpec{
			Pool:    ipamutil.GetObjRef(pool),
			Claim:   ipamutil.GetObjRef(claim),
			Address: ipamv1.IPAddressStr(IP()),
			Gateway: &gateway,
		},
	}

	return claim, ip
}
