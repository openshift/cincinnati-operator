/*


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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	//"github.com/operator-framework/operator-sdk/pkg/leader"
	//"github.com/operator-framework/operator-sdk/pkg/log/zap"
	//sdkVersion "github.com/operator-framework/operator-sdk/version"
	"github.com/operator-framework/operator-lib/leader"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"

	updateservicev1 "github.com/openshift/cincinnati-operator/api/v1"
	uscontroller "github.com/openshift/cincinnati-operator/controllers"
	"github.com/openshift/cincinnati-operator/version"

	//k8sruntime "k8s.io/apimachinery/pkg/runtime"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

// Change below variables to serve metrics on different host or port.
var (
	metricsHost               = "0.0.0.0"
	metricsPort         int32 = 8383
	operatorMetricsPort int32 = 8686
)

var (
	//scheme = k8sruntime.NewScheme()
	//setupLog = ctrl.Log.WithName("setup")
	log = ctrl.Log.WithName("cmd")
)

/*
func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(updateservicev1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}
*/

func printVersion() {
	log.Info(fmt.Sprintf("Operator Version: %s", version.Version))
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	log.Info(fmt.Sprintf("Version of operator-sdk: %s", version.SdkVersion))
}

// getWatchNamespace returns the Namespace the operator should be watching for changes
func getWatchNamespace() (string, error) {
	// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
	// which specifies the Namespace to watch.
	var watchNamespaceEnvVar = "WATCH_NAMESPACE"

	ns, found := os.LookupEnv(watchNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("%s must be set", watchNamespaceEnvVar)
	}
	return ns, nil
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	printVersion()

	namespace, err := getWatchNamespace()
	if err != nil {
		log.Error(err, "unable to get WatchNamespace; unable to start manager")
		os.Exit(1)
	}
	log.Info(fmt.Sprintf("Namespace: %s", namespace))

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	ctx := context.TODO()
	// Become the leader before proceeding
	err = leader.Become(ctx, "updateservice-operator-lock")
	if err != nil {
		log.Error(err, "Could not become leader")
		os.Exit(1)
	}
	log.Info("1")
	/*
		cfg := ctrl.GetConfigOrDie()
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:             scheme,
			Namespace:          namespace,
			MetricsBindAddress: metricsAddr,
			Port:               9443,
			LeaderElection:     enableLeaderElection,
			LeaderElectionID:   "4cf75da7.openshift.io",
		})
		if err != nil {
			log.Error(err, "unable to start manager")
			os.Exit(1)
		}

		if err = (&uscontroller.UpdateServiceReconciler{
			Client: mgr.GetClient(),
			Log:    ctrl.Log.WithName("controllers").WithName("UpdateService"),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			log.Error(err, "unable to create controller", "controller", "UpdateService")
			os.Exit(1)
		}
		// +kubebuilder:scaffold:builder
	*/

	// Set default manager options
	options := manager.Options{
		Namespace:          namespace,
		MetricsBindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
	}

	// Add support for MultiNamespace set in WATCH_NAMESPACE (e.g ns1,ns2)
	// Note that this is not intended to be used for excluding namespaces, this is better done via a Predicate
	// Also note that you may face performance issues when using this with a high number of namespaces.
	// More Info: https://godoc.org/github.com/kubernetes-sigs/controller-runtime/pkg/cache#MultiNamespacedCacheBuilder
	if strings.Contains(namespace, ",") {
		options.Namespace = ""
		options.NewCache = cache.MultiNamespacedCacheBuilder(strings.Split(namespace, ","))
	}

	// Create a new manager to provide shared dependencies and start components
	mgr, err := manager.New(cfg, options)
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	log.Info("Registering Components.")

	// Setup Scheme for all resources
	if err := updateservicev1.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	if err := configv1.Install(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	if err := routev1.Install(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	opts := uscontroller.Options{
		OperandImage: os.Getenv("OPERAND_IMAGE"),
	}
	if opts.OperandImage == "" {
		log.Error(errors.New("Must set envvar OPERAND_IMAGE"), "")
		os.Exit(1)
	}
	// Usually this is done with an init() function in the package, but since we
	// need to provide input, we're doing it here instead.
	uscontroller.AddToManagerFuncs = append(uscontroller.AddToManagerFuncs, func(mgr manager.Manager) error {
		return uscontroller.Add(mgr, opts)
	})
	// Setup UpdateService controllers
	if err := uscontroller.AddToManager(mgr); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Add the Metrics Service
	//addMetrics(ctx, cfg)

	log.Info("Starting the Cmd.")

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "Manager exited non-zero")
		os.Exit(1)
	}
}
