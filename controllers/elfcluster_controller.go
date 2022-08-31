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

	"github.com/pkg/errors"
	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	capecontext "github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/config"
	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam"
	"github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam/metal3io"
	ipamutil "github.com/smartxworks/cluster-api-provider-elf-static-ip/pkg/ipam/util"
)

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch

const (
	// ClusterStaticIPFinalizer allows ReconcileElfCluster to clean up static ip
	// resources associated with ElfCluster before removing it from the
	// API Server.
	ClusterStaticIPFinalizer = "elfclusterstaticip.infrastructure.cluster.x-k8s.io"
)

// AddClusterControllerToManager adds the cluster controller to the provided
// manager.
func AddClusterControllerToManager(ctx *capecontext.ControllerManagerContext, mgr ctrlmgr.Manager) error {
	var (
		clusterControlledType     = &capev1.ElfCluster{}
		clusterControlledTypeName = reflect.TypeOf(clusterControlledType).Elem().Name()
		controllerNameShort       = fmt.Sprintf("ipam-%s-controller", strings.ToLower(clusterControlledTypeName))
	)

	// Build the controller context.
	controllerContext := &capecontext.ControllerContext{
		ControllerManagerContext: ctx,
		Name:                     controllerNameShort,
		Logger:                   ctx.Logger.WithName(controllerNameShort),
	}

	reconciler := &ElfClusterReconciler{ControllerContext: controllerContext}

	return ctrl.NewControllerManagedBy(mgr).
		// Watch the controlled, infrastructure resource.
		For(clusterControlledType).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctx.MaxConcurrentReconciles}).
		Complete(reconciler)
}

// ElfClusterReconciler reconciles a ElfCluster object.
type ElfClusterReconciler struct {
	*capecontext.ControllerContext
}

// Reconcile ensures the back-end state reflects the Kubernetes resource state intent.
func (r *ElfClusterReconciler) Reconcile(ctx goctx.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	// Get the ElfCluster resource for this request.
	var elfCluster capev1.ElfCluster
	if err := r.Client.Get(r, req.NamespacedName, &elfCluster); err != nil {
		if apierrors.IsNotFound(err) {
			r.Logger.Info("ElfCluster not found, won't reconcile", "key", req.NamespacedName)
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	// Fetch the CAPI Cluster.
	cluster, err := capiutil.GetOwnerCluster(r, r.Client, elfCluster.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if cluster == nil {
		r.Logger.Info("Waiting for Cluster Controller to set OwnerRef on ElfCluster", "namespace", elfCluster.Namespace, "elfCluster", elfCluster.Name)
		return reconcile.Result{}, nil
	}

	if annotations.IsPaused(cluster, &elfCluster) {
		r.Logger.V(4).Info("ElfCluster linked to a cluster that is paused", "namespace", elfCluster.Namespace, "elfCluster", elfCluster.Name)
		return reconcile.Result{}, nil
	}

	// Create the patch helper.
	patchHelper, err := patch.NewHelper(&elfCluster, r.Client)
	if err != nil {
		return reconcile.Result{}, errors.Wrapf(err, "failed to init patch helper for %s %s/%s", elfCluster.GroupVersionKind(), elfCluster.Namespace, elfCluster.Name)
	}

	// Create the cluster context for this request.
	logger := r.Logger.WithValues("namespace", cluster.Namespace, "elfCluster", elfCluster.Name)
	clusterContext := &context.ClusterContext{
		IPAMService: metal3io.NewIpam(r.Client, r.Logger),
		ClusterContext: &capecontext.ClusterContext{
			ControllerContext: r.ControllerContext,
			Cluster:           cluster,
			ElfCluster:        &elfCluster,
			Logger:            logger,
			PatchHelper:       patchHelper,
		},
	}

	// Always issue a patch when exiting this function so changes to the
	// resource are patched back to the API server.
	defer func() {
		if err := clusterContext.Patch(); err != nil {
			if reterr == nil {
				reterr = err
			}

			clusterContext.Logger.Error(err, "patch failed", "elfCluster", clusterContext.String())
		}
	}()

	// Handle deleted clusters
	if !elfCluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(clusterContext)
	}

	return r.reconcileControlPlaneEndpoint(clusterContext)
}

func (r *ElfClusterReconciler) reconcileDelete(ctx *context.ClusterContext) (reconcile.Result, error) {
	if ctx.ElfCluster.Spec.ControlPlaneEndpoint.Host == "" || !ctrlutil.ContainsFinalizer(ctx.ElfCluster, ClusterStaticIPFinalizer) {
		ctrlutil.RemoveFinalizer(ctx.ElfCluster, ClusterStaticIPFinalizer)
		return ctrl.Result{}, nil
	}

	if ctrlutil.ContainsFinalizer(ctx.ElfCluster, capev1.ClusterFinalizer) {
		ctx.Logger.V(1).Info("Waiting for ClusterFinalizer to be removed")
		return ctrl.Result{}, nil
	}

	ipPool, err := r.getIPPool(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	if ipPool == nil {
		return ctrl.Result{}, nil
	}

	if err := ctx.IPAMService.ReleaseIP(ctx, ipamutil.GetControlPlaneEndpointClaimName(ctx.ElfCluster.Name), ipPool); err != nil {
		return reconcile.Result{}, err
	}

	ctrlutil.RemoveFinalizer(ctx.ElfCluster, ClusterStaticIPFinalizer)

	return reconcile.Result{}, nil
}

func (r *ElfClusterReconciler) reconcileControlPlaneEndpoint(ctx *context.ClusterContext) (reconcile.Result, error) {
	if ctx.ElfCluster.Spec.ControlPlaneEndpoint.Host != "" {
		ctx.Logger.V(2).Info("no need to allocate static IP for control plane endpoint")
		return ctrl.Result{}, nil
	}

	ctx.Logger.V(1).Info("Reconciling ElfCluster control plane endpoint")

	ipPool, err := r.getIPPool(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	if ipPool == nil {
		return ctrl.Result{}, nil
	}

	ipName := ipamutil.GetControlPlaneEndpointClaimName(ctx.ElfCluster.Name)
	ip, err := ctx.IPAMService.GetIP(ctx, ipName, ipPool)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to get allocated IP address %s", ipName)
	}
	if ip == nil {
		if _, err := ctx.IPAMService.AllocateIP(ctx, ipName, ipPool, ctx.ElfCluster); err != nil {
			return ctrl.Result{}, errors.Wrapf(err, "failed to allocate IP address %s", ipName)
		}

		ctx.Logger.Info(fmt.Sprintf("waiting for IP address %s to be available", ipName))

		return ctrl.Result{RequeueAfter: config.DefaultRequeue}, nil
	}

	if err := ipamutil.ValidateIP(ip); err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "invalid IP address retrieved %s", ipName)
	}

	// If the ElfCluster doesn't have our finalizer, add it.
	ctrlutil.AddFinalizer(ctx.ElfCluster, ClusterStaticIPFinalizer)
	ctx.ElfCluster.Spec.ControlPlaneEndpoint.Host = ip.GetAddress()

	ctx.Logger.Info("Set control plane endpoint successfully", "IPAddress", ip.GetName())

	return reconcile.Result{}, nil
}

func (r *ElfClusterReconciler) getIPPool(ctx *context.ClusterContext) (ipam.IPPool, error) {
	ipPool, err := ctx.IPAMService.GetAvailableIPPool(ctx, ctx.ElfCluster.Labels, ctx.Cluster.ObjectMeta)
	if err != nil {
		ctx.Logger.Error(err, "failed to get an available IPPool")
		return nil, err
	}
	if ipPool == nil {
		ctx.Logger.Info("waiting for IPPool to be available")
		return nil, nil
	}

	return ipPool, nil
}
