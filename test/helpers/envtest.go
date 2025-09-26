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

package helpers

import (
	goctx "context"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	ipamv1 "github.com/metal3-io/ip-address-manager/api/v1alpha1"
	"github.com/onsi/ginkgo/v2"
	capev1 "github.com/smartxworks/cluster-api-provider-elf/api/v1beta1"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/context"
	"github.com/smartxworks/cluster-api-provider-elf/pkg/manager"
	"golang.org/x/tools/go/packages"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cgscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
	addonsv1 "sigs.k8s.io/cluster-api/api/addons/v1beta1"
	capiv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	clusterctlv1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func init() {
	ctrl.SetLogger(klog.Background())
	// add logger for ginkgo
	klog.SetOutput(ginkgo.GinkgoWriter)
}

var (
	scheme   = runtime.NewScheme()
	crdPaths []string
)

func init() {
	// Calculate the scheme.
	utilruntime.Must(cgscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(admissionv1.AddToScheme(scheme))
	utilruntime.Must(capiv1.AddToScheme(scheme))
	utilruntime.Must(controlplanev1.AddToScheme(scheme))
	utilruntime.Must(bootstrapv1.AddToScheme(scheme))
	utilruntime.Must(addonsv1.AddToScheme(scheme))
	utilruntime.Must(clusterctlv1.AddToScheme(scheme))
	utilruntime.Must(capev1.AddToScheme(scheme))
	utilruntime.Must(ipamv1.AddToScheme(scheme))

	crdPaths = []string{}

	// append CAPI CRDs path
	if capiPaths := getFilePathToCAPICRDs(); len(capiPaths) > 0 {
		crdPaths = append(crdPaths, capiPaths...)
	}

	// append CAPE CRDs path
	if capePath := getFilePathToCAPECRDs(); capePath != "" {
		crdPaths = append(crdPaths, capePath)
	}
}

type (
	// TestEnvironment encapsulates a Kubernetes local test environment.
	TestEnvironment struct {
		manager.Manager
		client.Client
		Env        *envtest.Environment
		Config     *rest.Config
		Kubeconfig string

		cancel goctx.CancelFunc
	}
)

// NewTestEnvironment creates a new environment spinning up a local api-server.
func NewTestEnvironment(ctx goctx.Context) *TestEnvironment {
	// Create the test environment.
	env := &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths:     crdPaths,
	}

	if _, err := env.Start(); err != nil {
		err = kerrors.NewAggregate([]error{err, env.Stop()})
		panic(err)
	}

	// Localhost is used on MacOS to avoid Firewall warning popups.
	host := "localhost"
	if strings.EqualFold(os.Getenv("USE_EXISTING_CLUSTER"), "true") {
		// 0.0.0.0 is required on Linux when using kind because otherwise the kube-apiserver running in kind
		// is unable to reach the webhook, because the webhook would be only listening on 127.0.0.1.
		// Somehow that's not an issue on MacOS.
		if goruntime.GOOS == "linux" {
			host = "0.0.0.0"
		}
	}

	managerOpts := manager.Options{
		Options: ctrl.Options{
			Scheme: scheme,
			Metrics: metricsserver.Options{
				BindAddress: "0",
			},
			WebhookServer: webhook.NewServer(
				webhook.Options{
					Port:    env.WebhookInstallOptions.LocalServingPort,
					CertDir: env.WebhookInstallOptions.LocalServingCertDir,
					Host:    host,
				},
			),
		},
		KubeConfig: env.Config,
	}
	managerOpts.AddToManager = func(ctx goctx.Context, ctrlMgrCtx *context.ControllerManagerContext, mgr ctrlmgr.Manager) error {
		return nil
	}

	mgr, err := manager.New(ctx, managerOpts)
	if err != nil {
		klog.Fatalf("failed to create the SKS controller manager: %v", err)
	}

	kubeconfig, err := CreateKubeconfig(mgr.GetConfig(), capiutil.RandomString(6)+"-cluster")
	if err != nil {
		klog.Fatalf("failed to create kubeconfig: %v", err)
	}

	return &TestEnvironment{
		Manager:    mgr,
		Client:     mgr.GetClient(),
		Config:     mgr.GetConfig(),
		Env:        env,
		Kubeconfig: kubeconfig,
	}
}

func (t *TestEnvironment) StartManager(ctx goctx.Context) error {
	ctx, cancel := goctx.WithCancel(ctx)
	t.cancel = cancel
	return t.Manager.Start(ctx)
}

func (t *TestEnvironment) Stop() error {
	t.cancel()

	if err := t.Env.Stop(); err != nil {
		return err
	}

	return os.Remove(t.Kubeconfig)
}

func (t *TestEnvironment) Cleanup(ctx goctx.Context, objs ...client.Object) error {
	errs := make([]error, 0, len(objs))
	for _, o := range objs {
		err := t.Client.Delete(ctx, o)
		if apierrors.IsNotFound(err) {
			// If the object is not found, it must've been garbage collected
			// already. For example, if we delete namespace first and then
			// objects within it.
			continue
		}

		errs = append(errs, err)
	}

	return kerrors.NewAggregate(errs)
}

func (t *TestEnvironment) CreateNamespace(ctx goctx.Context, name string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"testenv/original-name": name,
			},
		},
	}

	if err := t.Client.Create(ctx, ns); err != nil {
		return nil, err
	}

	return ns, nil
}

func (t *TestEnvironment) CreateObjects(ctx goctx.Context, objs ...client.Object) error {
	for i := range objs {
		if err := t.Client.Create(ctx, objs[i]); err != nil {
			return err
		}
	}

	return nil
}

// CreateKubeconfig returns a new kubeconfig from the envtest config.
func CreateKubeconfig(cfg *rest.Config, clusterName string) (string, error) {
	contextName := fmt.Sprintf("%s@%s", cfg.Username, clusterName)
	c := api.Config{
		Clusters: map[string]*api.Cluster{
			clusterName: {
				Server:                   cfg.Host,
				CertificateAuthorityData: cfg.CAData,
			},
		},
		Contexts: map[string]*api.Context{
			contextName: {
				Cluster:  clusterName,
				AuthInfo: cfg.Username,
			},
		},
		AuthInfos: map[string]*api.AuthInfo{
			cfg.Username: {
				ClientKeyData:         cfg.KeyData,
				ClientCertificateData: cfg.CertData,
			},
		},
		CurrentContext: contextName,
	}

	kubeconfig := "/var/tmp/" + clusterName + ".kubeconfig"
	if err := clientcmd.WriteToFile(c, kubeconfig); err != nil {
		return "", err
	}

	return kubeconfig, nil
}

func getFilePathToCAPICRDs() []string {
	packageName := "sigs.k8s.io/cluster-api"
	packageConfig := &packages.Config{
		Mode: packages.NeedModule,
	}

	pkgs, err := packages.Load(packageConfig, packageName)
	if err != nil {
		return nil
	}

	pkg := pkgs[0]

	return []string{
		filepath.Join(pkg.Module.Dir, "config", "crd", "bases"),
		filepath.Join(pkg.Module.Dir, "controlplane", "kubeadm", "config", "crd", "bases"),
	}
}

func getFilePathToCAPECRDs() string {
	packageName := "github.com/smartxworks/cluster-api-provider-elf"
	packageConfig := &packages.Config{
		Mode: packages.NeedModule,
	}

	pkgs, err := packages.Load(packageConfig, packageName)
	if err != nil {
		return ""
	}

	pkg := pkgs[0]

	return filepath.Join(pkg.Module.Dir, "config", "crd", "bases")
}
