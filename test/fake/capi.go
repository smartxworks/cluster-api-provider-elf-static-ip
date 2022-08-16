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
	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	capefake "github.com/smartxworks/cluster-api-provider-elf/test/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/storage/names"
	capiv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam"
)

func NewClusterAndMachineObjects() (*capev1.ElfCluster, *capiv1.Cluster, *capev1.ElfMachine, *capiv1.Machine, *capev1.ElfMachineTemplate) {
	elfCluster, cluster := capefake.NewClusterObjects()
	elfMachine, machine := capefake.NewMachineObjects(elfCluster, cluster)
	elfMachine.Spec.Network.Devices = []capev1.NetworkDeviceSpec{{NetworkType: capev1.NetworkTypeIPV4, IPAddrs: []string{}}}

	template := &capev1.ElfMachineTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName("elfMmachinetemplate-"),
			Namespace: Namespace,
			Labels:    map[string]string{},
		},
	}
	elfMachine.SetAnnotations(map[string]string{
		capiv1.TemplateClonedFromNameAnnotation: template.Name,
	})

	return elfCluster, cluster, elfMachine, machine, template
}

func InitOwnerReferences(
	ctrlContext *context.ControllerContext,
	elfCluster *capev1.ElfCluster, cluster *capiv1.Cluster,
	elfMachine *capev1.ElfMachine, machine *capiv1.Machine) {
	capefake.InitOwnerReferences(ctrlContext, elfCluster, cluster, elfMachine, machine)
}

func SetIPPoolForMachineTemplate(elfMachineTemplate *capev1.ElfMachineTemplate, pool *ipamv1.IPPool) {
	labels := elfMachineTemplate.Labels
	if labels == nil {
		labels = make(map[string]string)
	}

	labels[ipam.ClusterIPPoolNameKey] = pool.Name
}
