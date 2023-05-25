package v1beta1

import (
	goctx "context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func (v *ElfMachineValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&capev1.ElfMachine{}).
		WithValidator(v).
		Complete()
}

//+kubebuilder:object:generate=false
//+kubebuilder:webhook:verbs=create;update;delete,path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-elfMachine,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=elfMachines,versions=v1beta1,name=velfMachine.kb.io,admissionReviewVersions=v1
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=elfMachines,verbs=get;list;watch

// ElfMachineValidator implements a validating webhook for ElfMachine.
type ElfMachineValidator struct {
	client.Client
	logr.Logger
}

var _ webhook.CustomValidator = &ElfMachineValidator{}

func (v *ElfMachineValidator) ValidateCreate(ctx goctx.Context, obj runtime.Object) error {
	return nil
}

func (v *ElfMachineValidator) ValidateUpdate(ctx goctx.Context, oldObj, newObj runtime.Object) error {
	oldElfMachine, ok := oldObj.(*capev1.ElfMachine) //nolint:forcetypeassert
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected an ElfMachine but got a %T", oldObj))
	}
	elfMachine, ok := newObj.(*capev1.ElfMachine) //nolint:forcetypeassert
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected an ElfMachine but got a %T", newObj))
	}

	if !reflect.DeepEqual(elfMachine.Finalizers, oldElfMachine.Finalizers) {
		v.Logger.Info("finalizers changed", "newElfMachine", elfMachine, "oldElfMachine", oldElfMachine)
	}

	return nil
}

func (v *ElfMachineValidator) ValidateDelete(ctx goctx.Context, obj runtime.Object) error {
	elfMachine, ok := obj.(*capev1.ElfMachine) //nolint:forcetypeassert
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected an ElfMachine but got a %T", obj))
	}

	v.Logger.Info("ValidateDelete", "elfMachine", elfMachine)

	return nil
}
