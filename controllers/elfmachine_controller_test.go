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
	"bytes"
	goctx "context"
	"fmt"
	"time"

	ipamv1 "github.com/metal3-io/ip-address-manager/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	capiv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam"
	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam/metal3io"
	ipamutil "github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam/util"
	"github.com/smartxworks/cluster-api-provider-elf-static-ip/test/fake"
)

var _ = Describe("ElfMachineReconciler", func() {
	var (
		logBuffer          *bytes.Buffer
		elfCluster         *capev1.ElfCluster
		cluster            *capiv1.Cluster
		elfMachine         *capev1.ElfMachine
		machine            *capiv1.Machine
		elfMachineTemplate *capev1.ElfMachineTemplate
		metal3IPPool       *ipamv1.IPPool
		metal3IPClaim      *ipamv1.IPClaim
		metal3IPAddress    *ipamv1.IPAddress
	)

	ctx := goctx.Background()

	BeforeEach(func() {
		logBuffer = new(bytes.Buffer)
		klog.SetOutput(logBuffer)

		elfCluster, cluster, elfMachine, machine, elfMachineTemplate = fake.NewClusterAndMachineObjects()
		metal3IPPool = fake.NewMetal3IPPool()
	})

	It("should not reconcile when ElfMachine not found", func() {
		ctrlMgrCtx := fake.NewControllerManagerContext()

		reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
		Expect(result).To(BeZero())
		Expect(err).ToNot(HaveOccurred())
		Expect(logBuffer.String()).To(ContainSubstring("ElfMachine not found, won't reconcile"))
	})

	It("should not reconcile when ElfMachine in an error state", func() {
		ctrlutil.AddFinalizer(elfMachine, capev1.MachineFinalizer)
		ctrlutil.AddFinalizer(elfMachine, MachineStaticIPFinalizer)
		elfMachine.Status.FailureMessage = ptr.To("some error")
		ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate)
		fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

		reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
		Expect(result).To(BeZero())
		Expect(err).ToNot(HaveOccurred())
		Expect(logBuffer.String()).To(ContainSubstring("Error state detected, skipping reconciliation"))
	})

	It("should not reconcile when ElfMachine without Machine", func() {
		ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate)

		reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
		Expect(result).To(BeZero())
		Expect(err).ToNot(HaveOccurred())
		Expect(logBuffer.String()).To(ContainSubstring("Waiting for Machine Controller to set OwnerRef on ElfMachine"))
	})

	It("should not reconcile when ElfMachine without Machine", func() {
		cluster.Spec.Paused = true
		ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate)
		fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

		reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
		Expect(result).To(BeZero())
		Expect(err).ToNot(HaveOccurred())
		Expect(logBuffer.String()).To(ContainSubstring("ElfMachine linked to a cluster that is paused"))
	})

	It("should not reconcile without static IP network devices", func() {
		ctrlutil.RemoveFinalizer(elfMachine, MachineStaticIPFinalizer)
		elfMachine.Spec.Network.Devices = []capev1.NetworkDeviceSpec{}
		ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate)
		fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

		reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
		Expect(result).To(BeZero())
		Expect(err).ToNot(HaveOccurred())
		Expect(logBuffer.String()).To(ContainSubstring("No static IP network device found"))
		Expect(ctrlMgrCtx.Client.Get(ctx, capiutil.ObjectKey(elfMachine), elfMachine)).To(Succeed())
		Expect(ctrlutil.ContainsFinalizer(elfMachine, MachineStaticIPFinalizer)).To(BeFalse())
	})

	It("should not reconcile when no need to allocate static IP", func() {
		ctrlutil.AddFinalizer(elfMachine, capev1.MachineFinalizer)
		ctrlutil.AddFinalizer(elfMachine, MachineStaticIPFinalizer)
		elfMachine.Spec.Network.Devices = []capev1.NetworkDeviceSpec{
			{NetworkType: capev1.NetworkTypeIPV4, IPAddrs: []string{fake.IP()}},
			{NetworkType: capev1.NetworkTypeIPV4DHCP},
			{NetworkType: capev1.NetworkTypeNone},
		}
		ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate)
		fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

		reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
		Expect(result).To(BeZero())
		Expect(err).ToNot(HaveOccurred())
		Expect(logBuffer.String()).To(ContainSubstring("No need to allocate static IP"))
		Expect(ctrlMgrCtx.Client.Get(ctx, capiutil.ObjectKey(elfMachine), elfMachine)).To(Succeed())
	})

	It("should set MachineStaticIPFinalizer first", func() {
		ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate)
		fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

		reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
		Expect(result).NotTo(BeZero())
		Expect(err).NotTo(HaveOccurred())
		Expect(ctrlMgrCtx.Client.Get(ctx, capiutil.ObjectKey(elfMachine), elfMachine)).To(Succeed())
		Expect(ctrlutil.ContainsFinalizer(elfMachine, MachineStaticIPFinalizer)).To(BeFalse())
		Expect(logBuffer.String()).To(ContainSubstring("Waiting for CAPE to set MachineFinalizer on ElfMachine"))

		ctrlutil.AddFinalizer(elfMachine, capev1.MachineFinalizer)
		ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate)
		fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

		reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
		result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
		Expect(result).NotTo(BeZero())
		Expect(err).NotTo(HaveOccurred())
		Expect(ctrlMgrCtx.Client.Get(ctx, capiutil.ObjectKey(elfMachine), elfMachine)).To(Succeed())
		Expect(ctrlutil.ContainsFinalizer(elfMachine, MachineStaticIPFinalizer)).To(BeTrue())
	})

	It("should not reconcile when no cloned-from-name annotation", func() {
		ctrlutil.AddFinalizer(elfMachine, MachineStaticIPFinalizer)
		elfMachine.Annotations[capiv1.TemplateClonedFromNameAnnotation] = ""
		ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate)
		fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

		reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
		Expect(result).To(BeZero())
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
		Expect(logBuffer.String()).To(ContainSubstring("failed to get IPPool match labels"))
		Expect(ctrlMgrCtx.Client.Get(ctx, capiutil.ObjectKey(elfMachine), elfMachine)).To(Succeed())
		Expect(ctrlutil.ContainsFinalizer(elfMachine, MachineStaticIPFinalizer)).To(BeTrue())
	})

	It("should not reconcile when no IPPool", func() {
		ctrlutil.AddFinalizer(elfMachine, MachineStaticIPFinalizer)
		ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate)
		fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

		reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
		Expect(result).To(BeZero())
		Expect(err).ToNot(HaveOccurred())
		Expect(logBuffer.String()).To(ContainSubstring("Waiting for IPPool to be available"))
		Expect(ctrlMgrCtx.Client.Get(ctx, capiutil.ObjectKey(elfMachine), elfMachine)).To(Succeed())
		Expect(ctrlutil.ContainsFinalizer(elfMachine, MachineStaticIPFinalizer)).To(BeTrue())
	})

	It("should create IPClaim and wait when no IPClaim", func() {
		ctrlutil.AddFinalizer(elfMachine, MachineStaticIPFinalizer)
		elfMachineTemplate.Labels[ipam.ClusterIPPoolNamespaceKey] = metal3IPPool.Namespace
		elfMachineTemplate.Labels[ipam.ClusterIPPoolNameKey] = metal3IPPool.Name
		ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate, metal3IPPool)
		fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

		reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
		Expect(result.RequeueAfter).To(Equal(config.DefaultRequeue))
		Expect(err).ToNot(HaveOccurred())
		Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("Waiting for IP address for %s to be available", ipamutil.GetFormattedClaimName(elfMachine.Namespace, elfMachine.Name, 0))))
		var ipClaim ipamv1.IPClaim
		Expect(ctrlMgrCtx.Client.Get(ctx, apitypes.NamespacedName{
			Namespace: metal3IPPool.Namespace,
			Name:      ipamutil.GetFormattedClaimName(elfMachine.Namespace, elfMachine.Name, 0),
		}, &ipClaim)).To(Succeed())
		Expect(ipClaim.Spec.Pool.Name).To(Equal(metal3IPPool.Name))
		Expect(ctrlMgrCtx.Client.Get(ctx, capiutil.ObjectKey(elfMachine), elfMachine)).To(Succeed())
		Expect(ctrlutil.ContainsFinalizer(elfMachine, MachineStaticIPFinalizer)).To(BeTrue())
	})

	It("should wait for IP when IPClaim without IP", func() {
		ctrlutil.AddFinalizer(elfMachine, MachineStaticIPFinalizer)
		metal3IPPool.Namespace = ipam.DefaultIPPoolNamespace
		metal3IPPool.Labels = map[string]string{
			ipam.ClusterIPPoolGroupKey: "ip-pool-group",
			ipam.ClusterNetworkNameKey: "ip-pool-vm-network",
			ipam.DefaultIPPoolKey:      "true",
		}
		elfMachineTemplate.Labels = metal3IPPool.Labels
		metal3IPClaim, metal3IPAddress = fake.NewMetal3IPObjects(metal3IPPool, ipamutil.GetFormattedClaimName(elfMachine.Namespace, elfMachine.Name, 0))
		ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate, metal3IPPool, metal3IPClaim)
		fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

		reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
		Expect(result.RequeueAfter).To(Equal(config.DefaultRequeue))
		Expect(err).ToNot(HaveOccurred())
		Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("IPClaim %s already exists, skipping creation", ipamutil.GetFormattedClaimName(elfMachine.Namespace, elfMachine.Name, 0))))
		Expect(logBuffer.String()).To(ContainSubstring(fmt.Sprintf("Waiting for IP address for %s to be available", ipamutil.GetFormattedClaimName(elfMachine.Namespace, elfMachine.Name, 0))))
		Expect(ctrlMgrCtx.Client.Get(ctx, capiutil.ObjectKey(elfMachine), elfMachine)).To(Succeed())
		Expect(ctrlutil.ContainsFinalizer(elfMachine, MachineStaticIPFinalizer)).To(BeTrue())
	})

	It("should set IP for devices when IP ready", func() {
		ctrlutil.AddFinalizer(elfMachine, MachineStaticIPFinalizer)
		metal3IPPool.Namespace = ipam.DefaultIPPoolNamespace
		metal3IPPool.Labels = map[string]string{ipam.DefaultIPPoolKey: "true"}
		metal3IPPool.Spec.DNSServers = append(metal3IPPool.Spec.DNSServers, ipamv1.IPAddressStr("1.1.1.1"), ipamv1.IPAddressStr("4.4.4.4"))
		metal3IPClaim, metal3IPAddress = fake.NewMetal3IPObjects(metal3IPPool, ipamutil.GetFormattedClaimName(elfMachine.Namespace, elfMachine.Name, 0))
		metal3IPAddress.Spec.DNSServers = append(metal3IPAddress.Spec.DNSServers, ipamv1.IPAddressStr("2.2.2.2"), ipamv1.IPAddressStr("3.3.3.3"))
		setMetal3IPForClaim(metal3IPClaim, metal3IPAddress)
		elfMachine.Spec.Network.Nameservers = []string{"3.3.3.3"}
		ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate, metal3IPPool, metal3IPClaim, metal3IPAddress)
		fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

		reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
		Expect(result).To(BeZero())
		Expect(err).ToNot(HaveOccurred())
		Expect(ctrlMgrCtx.Client.Get(ctx, capiutil.ObjectKey(elfMachine), elfMachine)).To(Succeed())
		Expect(elfMachine.Spec.Network.Devices[0].IPAddrs).To(Equal([]string{string(metal3IPAddress.Spec.Address)}))
		// DNS server is unique and DNS server priority of ElfMachine is higher than IPPool.
		Expect(elfMachine.Spec.Network.Nameservers).To(Equal([]string{"3.3.3.3", "2.2.2.2", "1.1.1.1"}))
		Expect(ctrlutil.ContainsFinalizer(elfMachine, MachineStaticIPFinalizer)).To(BeTrue())
	})

	Context("Delete a ElfMachine", func() {
		BeforeEach(func() {
			ctrlutil.AddFinalizer(elfMachine, capev1.MachineFinalizer)
			ctrlutil.AddFinalizer(elfMachine, MachineStaticIPFinalizer)
			elfMachine.DeletionTimestamp = &metav1.Time{Time: time.Now().UTC()}
		})

		It("should remove MachineStaticIPFinalizer without IPV4 devices", func() {
			ctrlutil.AddFinalizer(elfMachine, MachineStaticIPFinalizer)
			elfMachine.Spec.Network.Devices = []capev1.NetworkDeviceSpec{{NetworkType: capev1.NetworkTypeIPV4DHCP}}
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
			Expect(result).To(BeZero())
			Expect(err).ToNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("No static IP network device found, but MachineStaticIPFinalizer is set and remove it"))
			Expect(ctrlMgrCtx.Client.Get(ctx, capiutil.ObjectKey(elfMachine), elfMachine)).To(Succeed())
			Expect(ctrlutil.ContainsFinalizer(elfMachine, MachineStaticIPFinalizer)).To(BeFalse())
		})

		It("should remove MachineStaticIPFinalizer without IPPool", func() {
			ctrlutil.RemoveFinalizer(elfMachine, capev1.MachineFinalizer)
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
			Expect(result).To(BeZero())
			Expect(err).ToNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("IPPool is not found, so no need to release the IP"))
			Expect(apierrors.IsNotFound(ctrlMgrCtx.Client.Get(ctx, capiutil.ObjectKey(elfMachine), elfMachine))).To(BeTrue())
		})

		It("should not reconcile with MachineFinalizer", func() {
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
			Expect(result).To(BeZero())
			Expect(err).ToNot(HaveOccurred())
			Expect(logBuffer.String()).To(ContainSubstring("Waiting for MachineFinalizer to be removed"))
			Expect(ctrlMgrCtx.Client.Get(ctx, capiutil.ObjectKey(elfMachine), elfMachine)).To(Succeed())
			Expect(ctrlutil.ContainsFinalizer(elfMachine, MachineStaticIPFinalizer)).To(BeTrue())
		})

		It("should remove MachineStaticIPFinalizer and delete related IPs", func() {
			ctrlutil.RemoveFinalizer(elfMachine, capev1.MachineFinalizer)
			metal3IPPool.Namespace = ipam.DefaultIPPoolNamespace
			metal3IPPool.Labels = map[string]string{ipam.DefaultIPPoolKey: "true"}
			metal3IPClaim, metal3IPAddress = fake.NewMetal3IPObjects(metal3IPPool, ipamutil.GetFormattedClaimName(elfMachine.Namespace, elfMachine.Name, 0))
			setMetal3IPForClaim(metal3IPClaim, metal3IPAddress)
			metal3IPClaim.Labels = map[string]string{ipam.IPOwnerNameKey: fmt.Sprintf("%s-%s", elfMachine.GetNamespace(), elfMachine.GetName())}
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate, metal3IPPool, metal3IPClaim, metal3IPAddress)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)

			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: capiutil.ObjectKey(elfMachine)})
			Expect(result).To(BeZero())
			Expect(err).ToNot(HaveOccurred())
			Expect(apierrors.IsNotFound(ctrlMgrCtx.Client.Get(ctx, capiutil.ObjectKey(elfMachine), elfMachine))).To(BeTrue())
			Expect(apierrors.IsNotFound(ctrlMgrCtx.Client.Get(ctx, capiutil.ObjectKey(metal3IPClaim), metal3IPClaim))).To(BeTrue())
		})
	})

	Context("getIPPool", func() {
		It("should prefer IPPool of device", func() {
			metal3IPPool.Namespace = elfMachine.Namespace
			elfMachine.Spec.Network.Devices = []capev1.NetworkDeviceSpec{
				{NetworkType: capev1.NetworkTypeIPV4, IPAddrs: []string{}, AddressesFromPools: []corev1.TypedLocalObjectReference{
					{APIGroup: ptr.To("ipam.metal3.io"), Kind: "IPPool", Name: metal3IPPool.Name},
				}},
			}
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate, metal3IPPool, metal3IPClaim, metal3IPAddress)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(metal3io.NewIpam(ctrlMgrCtx.Client, ctrl.LoggerFrom(ctx)), cluster, machine, elfMachine)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
			ipPool, err := reconciler.getIPPool(ctx, machineContext, elfMachine.Spec.Network.Devices[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(ipPool.GetNamespace()).To(Equal(metal3IPPool.Namespace))
			Expect(ipPool.GetName()).To(Equal(metal3IPPool.Name))

			metal3IPPool.Namespace = ipam.DefaultIPPoolNamespace
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate, metal3IPPool, metal3IPClaim, metal3IPAddress)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext = newMachineContext(metal3io.NewIpam(ctrlMgrCtx.Client, ctrl.LoggerFrom(ctx)), cluster, machine, elfMachine)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
			ipPool, err = reconciler.getIPPool(ctx, machineContext, elfMachine.Spec.Network.Devices[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(ipPool.GetName()).To(Equal(metal3IPPool.Name))

			elfMachine.Spec.Network.Devices[0].AddressesFromPools[0].Name = "notfound"
			ipPool, err = reconciler.getIPPool(ctx, machineContext, elfMachine.Spec.Network.Devices[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(ipPool).To(BeNil())
		})

		It("should get the default ip-pool", func() {
			metal3IPPool.Namespace = cluster.Namespace
			metal3IPPool.Labels = map[string]string{ipam.DefaultIPPoolKey: "true"}
			ctrlMgrCtx := fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate, metal3IPPool, metal3IPClaim, metal3IPAddress)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext := newMachineContext(metal3io.NewIpam(ctrlMgrCtx.Client, ctrl.LoggerFrom(ctx)), cluster, machine, elfMachine)
			reconciler := &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
			ipPool, err := reconciler.getIPPool(ctx, machineContext, elfMachine.Spec.Network.Devices[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(ipPool.GetNamespace()).To(Equal(metal3IPPool.Namespace))
			Expect(ipPool.GetName()).To(Equal(metal3IPPool.Name))

			metal3IPPool.Namespace = ipam.DefaultIPPoolNamespace
			metal3IPPool.Labels = map[string]string{ipam.DefaultIPPoolKey: "true"}
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate, metal3IPPool, metal3IPClaim, metal3IPAddress)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext = newMachineContext(metal3io.NewIpam(ctrlMgrCtx.Client, ctrl.LoggerFrom(ctx)), cluster, machine, elfMachine)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
			ipPool, err = reconciler.getIPPool(ctx, machineContext, elfMachine.Spec.Network.Devices[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(ipPool.GetNamespace()).To(Equal(metal3IPPool.Namespace))
			Expect(ipPool.GetName()).To(Equal(metal3IPPool.Name))

			metal3IPPool.Namespace = "notfoud"
			metal3IPPool.Labels = map[string]string{ipam.DefaultIPPoolKey: "true"}
			ctrlMgrCtx = fake.NewControllerManagerContext(elfCluster, cluster, elfMachine, machine, elfMachineTemplate, metal3IPPool, metal3IPClaim, metal3IPAddress)
			fake.InitOwnerReferences(ctx, ctrlMgrCtx, elfCluster, cluster, elfMachine, machine)
			machineContext = newMachineContext(metal3io.NewIpam(ctrlMgrCtx.Client, ctrl.LoggerFrom(ctx)), cluster, machine, elfMachine)
			reconciler = &ElfMachineReconciler{ControllerManagerContext: ctrlMgrCtx}
			ipPool, err = reconciler.getIPPool(ctx, machineContext, elfMachine.Spec.Network.Devices[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(ipPool).To(BeNil())
		})
	})
})
