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
	capecontext "github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	capefake "github.com/smartxworks/cluster-api-provider-elf/test/fake"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cgscheme "k8s.io/client-go/kubernetes/scheme"
	addonsv1 "sigs.k8s.io/cluster-api/api/addons/v1beta1"
	capiv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	clusterctlv1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// NewControllerManagerContext returns a fake ControllerManagerContext for unit
// testing reconcilers and webhooks with a fake client. You can choose to
// initialize it with a slice of runtime.Object.
func NewControllerManagerContext(initObjects ...client.Object) *capecontext.ControllerManagerContext {
	scheme := runtime.NewScheme()
	utilruntime.Must(cgscheme.AddToScheme(scheme))
	utilruntime.Must(capiv1.AddToScheme(scheme))
	utilruntime.Must(controlplanev1.AddToScheme(scheme))
	utilruntime.Must(bootstrapv1.AddToScheme(scheme))
	utilruntime.Must(addonsv1.AddToScheme(scheme))
	utilruntime.Must(clusterctlv1.AddToScheme(scheme))
	utilruntime.Must(capev1.AddToScheme(scheme))
	utilruntime.Must(ipamv1.AddToScheme(scheme))

	clientWithObjects := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(
		&capev1.ElfCluster{},
		&capev1.ElfMachine{},
	).WithObjects(initObjects...).Build()

	return &capecontext.ControllerManagerContext{
		Client:                  clientWithObjects,
		Scheme:                  scheme,
		Name:                    capefake.ControllerManagerName,
		LeaderElectionNamespace: capefake.LeaderElectionNamespace,
		LeaderElectionID:        capefake.LeaderElectionID,
	}
}
