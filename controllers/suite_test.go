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
	"flag"
	"fmt"
	"os"
	"testing"

	ipamv1 "github.com/metal3-io/ip-address-manager/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	capecontext "github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"k8s.io/klog/v2"
	capiv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam"
	ipamutil "github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam/util"
	"github.com/smartxworks/cluster-api-provider-elf-static-ip/test/fake"
	"github.com/smartxworks/cluster-api-provider-elf-static-ip/test/helpers"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var (
	testEnv *helpers.TestEnvironment
)

func TestMain(m *testing.M) {
	code := 0

	defer func() { os.Exit(code) }()

	setup()

	defer teardown()

	code = m.Run()
}

func setup() {
	// set log
	klog.InitFlags(nil)
	if err := flag.Set("logtostderr", "false"); err != nil {
		_ = fmt.Errorf("Error setting logtostderr flag")
	}
	if err := flag.Set("v", "6"); err != nil {
		_ = fmt.Errorf("Error setting v flag")
	}
	if err := flag.Set("alsologtostderr", "false"); err != nil {
		_ = fmt.Errorf("Error setting alsologtostderr flag")
	}

	testEnv = helpers.NewTestEnvironment()

	// Set kubeconfig.
	os.Setenv("KUBECONFIG", testEnv.Kubeconfig)

	controllerOpts := controller.Options{MaxConcurrentReconciles: 10}

	if err := AddMachineControllerToManager(testEnv.GetContext(), testEnv.Manager, controllerOpts); err != nil {
		panic(fmt.Sprintf("unable to setup ElfMachine controller: %v", err))
	}

	go func() {
		fmt.Println("Starting the manager")
		if err := testEnv.StartManager(testEnv.GetContext()); err != nil {
			panic(fmt.Sprintf("failed to start the envtest manager: %v", err))
		}
	}()

	<-testEnv.Manager.Elected()
}

func teardown() {
	if err := testEnv.Stop(); err != nil {
		panic(fmt.Sprintf("Failed to stop envtest: %v", err))
	}
}

func newCtrlContexts(objs ...client.Object) *capecontext.ControllerContext {
	ctrlMgrContext := fake.NewControllerManagerContext(objs...)
	ctrlContext := &capecontext.ControllerContext{
		ControllerManagerContext: ctrlMgrContext,
		Logger:                   ctrllog.Log,
	}

	return ctrlContext
}

func setMetal3IPForClaim(ipClaim *ipamv1.IPClaim, ip *ipamv1.IPAddress) {
	ref := ipamutil.GetObjRef(ip)
	ipClaim.Status.Address = &ref
}

func newMachineContext(
	ctrlContext *capecontext.ControllerContext,
	ipamService ipam.IPAddressManager,
	cluster *capiv1.Cluster, machine *capiv1.Machine, elfMachine *capev1.ElfMachine) *context.MachineContext {
	return &context.MachineContext{
		IPAMService: ipamService,
		MachineContext: &capecontext.MachineContext{
			ControllerContext: ctrlContext,
			Cluster:           cluster,
			Machine:           machine,
			ElfMachine:        elfMachine,
			Logger:            ctrlContext.Logger,
		},
	}
}
