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
	goctx "context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/pkg/errors"
	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	capecontext "github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apitypes "k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	capiv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam"
	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam/metal3io"
	ipamutil "github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam/util"
)

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachines/finalizers,verbs=update
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfmachinetemplates,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=ipam.metal3.io,resources=ippools,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=ipam.metal3.io,resources=ippools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ipam.metal3.io,resources=ipclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ipam.metal3.io,resources=ipclaims/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ipam.metal3.io,resources=ipaddresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ipam.metal3.io,resources=ipaddresses/status,verbs=get;update;patch

// ElfMachineReconciler reconciles a ElfMachine object.
type ElfMachineReconciler struct {
	*capecontext.ControllerContext
}

func AddMachineControllerToManager(ctx *capecontext.ControllerManagerContext, mgr ctrlmgr.Manager) error {
	var (
		controlledType      = &capev1.ElfMachine{}
		controlledTypeName  = reflect.TypeOf(controlledType).Elem().Name()
		controllerNameShort = fmt.Sprintf("ipam-%s-controller", strings.ToLower(controlledTypeName))
	)

	// Build the controller context.
	controllerContext := &capecontext.ControllerContext{
		ControllerManagerContext: ctx,
		Name:                     controllerNameShort,
		Logger:                   ctx.Logger.WithName(controllerNameShort),
	}

	reconciler := &ElfMachineReconciler{
		ControllerContext: controllerContext,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(controlledType).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctx.MaxConcurrentReconciles}).
		Complete(reconciler)
}

func (r *ElfMachineReconciler) Reconcile(ctx goctx.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	// Get the ElfMachine resource for this request.
	var elfMachine capev1.ElfMachine
	if err := r.Client.Get(r, req.NamespacedName, &elfMachine); err != nil {
		if apierrors.IsNotFound(err) {
			r.Logger.Info("ElfMachine not found, won't reconcile", "key", req.NamespacedName)
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	// If the ElfMachine is in an delete/error state, return early.
	if !elfMachine.ObjectMeta.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, nil
	} else if elfMachine.IsFailed() {
		r.Logger.V(2).Info("Error state detected, skipping reconciliation", "namespace", elfMachine.Namespace, "elfMachine", elfMachine.Name)
		return reconcile.Result{}, nil
	}

	// Fetch the CAPI Machine.
	machine, err := capiutil.GetOwnerMachine(r, r.Client, elfMachine.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if machine == nil {
		r.Logger.Info("Waiting for Machine Controller to set OwnerRef on ElfMachine", "namespace", elfMachine.Namespace, "elfMachine", elfMachine.Name)
		return reconcile.Result{}, nil
	}

	// Fetch the CAPI Cluster.
	cluster, err := capiutil.GetClusterFromMetadata(r, r.Client, machine.ObjectMeta)
	//nolint:nilerr
	if err != nil {
		r.Logger.Info("Machine is missing cluster label or cluster does not exist", "namespace", machine.Namespace, "machine", machine.Name)

		return reconcile.Result{}, nil
	}
	if annotations.IsPaused(cluster, &elfMachine) {
		r.Logger.V(2).Info("ElfMachine linked to a cluster that is paused", "namespace", elfMachine.Namespace, "elfMachine", elfMachine.Name)

		return reconcile.Result{}, nil
	}

	logger := r.Logger.WithValues("namespace", elfMachine.Namespace, "cluster", cluster.Name, "elfMachine", elfMachine.Name)

	// Create the machine context for this request.
	machineContext := &context.MachineContext{
		IPAMService: metal3io.NewIpam(r.Client, r.Logger),
		MachineContext: &capecontext.MachineContext{
			ControllerContext: r.ControllerContext,
			Cluster:           cluster,
			Machine:           machine,
			ElfMachine:        &elfMachine,
			Logger:            logger,
		},
	}

	return r.reconcileIPAddress(machineContext)
}

func (r *ElfMachineReconciler) reconcileIPAddress(ctx *context.MachineContext) (reconcile.Result, error) {
	ctx.Logger.Info("reconcile IP address")

	devices := ctx.ElfMachine.Spec.Network.Devices
	if len(devices) == 0 {
		ctx.Logger.V(2).Info("no network device found")
		return ctrl.Result{}, nil
	}

	if !ipamutil.NeedsAllocateIP(devices) {
		ctx.Logger.V(2).Info("no need to allocate IP")
		return ctrl.Result{}, nil
	}

	poolMatchLabels, err := r.getIPPoolMatchLabels(ctx)
	if err != nil {
		ctx.Logger.Error(err, "failed to get IPPool match labels")
		return ctrl.Result{}, nil
	}

	ipPool, err := ctx.IPAMService.GetAvailableIPPool(ctx, poolMatchLabels, ctx.Cluster.ObjectMeta)
	if err != nil {
		ctx.Logger.Error(err, "failed to get an available IPPool")
		return ctrl.Result{}, nil
	}
	if ipPool == nil {
		ctx.Logger.Info("waiting for IPPool to be available")
		return ctrl.Result{}, nil
	}

	// Create the patch helper.
	patchHelper, err := patch.NewHelper(ctx.ElfMachine, r.Client)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to init patch helper for %s %s/%s", ctx.ElfMachine.GroupVersionKind(), ctx.ElfMachine.Namespace, ctx.ElfMachine.Name)
	}

	var errs []error
	requeueAfter := time.Duration(0)
	for i := 0; i < len(devices); i++ {
		if !ipamutil.NeedsAllocateIPForDevice(devices[i]) {
			continue
		}

		result, err := r.reconcileDeviceIPAddress(ctx, ipPool, i)
		if err != nil {
			errs = append(errs, err)
		}
		if result.RequeueAfter != 0 {
			requeueAfter = result.RequeueAfter
		}
	}

	if err := patchHelper.Patch(ctx, ctx.ElfMachine); err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to patch ElfMachine to set static IP")
	}

	if len(errs) > 0 {
		if requeueAfter == time.Duration(0) {
			requeueAfter = config.DefaultRequeue
		}

		return reconcile.Result{RequeueAfter: requeueAfter}, kerrors.NewAggregate(errs)
	}

	ctx.Logger.Info("Set IP address successfully")

	return reconcile.Result{RequeueAfter: requeueAfter}, nil
}

func (r *ElfMachineReconciler) reconcileDeviceIPAddress(ctx *context.MachineContext, ipPool ipam.IPPool, index int) (reconcile.Result, error) {
	ipName := ipamutil.GetFormattedClaimName(ctx.ElfMachine.Name, index)
	ip, err := ctx.IPAMService.GetIP(ctx, ipName, ipPool)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to get allocated IP address %s", ipName)
	}
	if ip == nil {
		if _, err := ctx.IPAMService.AllocateIP(ctx, ipName, ipPool, ctx.ElfMachine); err != nil {
			return ctrl.Result{}, errors.Wrapf(err, "failed to allocate IP address %s", ipName)
		}

		ctx.Logger.Info(fmt.Sprintf("waiting for IP address %s to be available", ipName))

		return ctrl.Result{RequeueAfter: config.DefaultRequeue}, nil
	}

	if err := ipamutil.ValidateIP(ip); err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "invalid IP address retrieved %s", ipName)
	}

	ctx.Logger.Info("static IP selected", "IPAddress", ip.GetName())

	device := &ctx.ElfMachine.Spec.Network.Devices[index]
	device.IPAddrs = []string{ip.GetAddress()}
	device.Netmask = ip.GetMask()
	device.Routes = []capev1.NetworkDeviceRouteSpec{{Gateway: ip.GetGateway()}}

	return ctrl.Result{}, nil
}

// getIPPoolMatchLabels matchs labels for the IPPool are retrieved from the ElfMachineTemplate.
func (r *ElfMachineReconciler) getIPPoolMatchLabels(ctx *context.MachineContext) (map[string]string, error) {
	vmTemplateName, ok := ctx.ElfMachine.GetAnnotations()[capiv1.TemplateClonedFromNameAnnotation]
	if !ok {
		return nil, errors.Errorf("ElfMachine %s has no value set in the 'cloned-from-name' annotation", ctx.ElfMachine.Name)
	}

	var elfMachineTemplate capev1.ElfMachineTemplate
	if err := ctx.Client.Get(ctx, apitypes.NamespacedName{
		Namespace: ctx.ElfMachine.Namespace,
		Name:      vmTemplateName,
	}, &elfMachineTemplate); err != nil {
		return nil, errors.Wrapf(err, "failed to get ElfMachineTemplate %s", vmTemplateName)
	}

	return elfMachineTemplate.GetLabels(), nil
}
