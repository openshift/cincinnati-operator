package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"

	updateservicev1 "github.com/openshift/cincinnati-operator/api/v1"
	"github.com/openshift/cincinnati-operator/controllers"
	"github.com/openshift/cincinnati-operator/version"

	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
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
	scheme = apiruntime.NewScheme()
	log    = ctrl.Log.WithName("cmd")
)

func init() {
	utilruntime.Must(configv1.AddToScheme(scheme))
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
	utilruntime.Must(updateservicev1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func printVersion() {
	log.Info(fmt.Sprintf("Operator Version: %s", version.Operator))
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
}

// getEnvVar returns the given environment variable value or an error if it is not set.
func getEnvVar(envVarName string) (string, error) {
	val, found := os.LookupEnv(envVarName)
	if !found {
		return "", fmt.Errorf("%s not found", envVarName)
	}
	return val, nil
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	printVersion()

	operandImage, err := getEnvVar("RELATED_IMAGE_OPERAND")
	if err != nil {
		log.Error(err, "RELATED_IMAGE_OPERAND must be set; unable to start manager")
		os.Exit(1)
	}
	podNamespace, err := getEnvVar("POD_NAMESPACE")
	if err != nil {
		log.Error(err, "POD_NAMESPACE must be set; unable to start manager")
		os.Exit(1)
	}
	options := ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "48ad1930.openshift.io",
		Namespace:          "",
	}
	nsList := []string{podNamespace, "", controllers.OpenshiftConfigNamespace}
	options.NewCache = cache.MultiNamespacedCacheBuilder(nsList)
	log.Info(fmt.Sprintf("list: %v", nsList))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		log.Error(err, "unable to start manager")
		os.Exit(1)
	}
	if err = (&controllers.UpdateServiceReconciler{
		Client:            mgr.GetClient(),
		Log:               ctrl.Log.WithName("controllers").WithName("UpdateService"),
		Scheme:            mgr.GetScheme(),
		OperandImage:      operandImage,
		OperatorNamespace: podNamespace,
	}).SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create controller", "controller", "UpdateService")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	log.Info(fmt.Sprintf("Starting in Namespace %s...", podNamespace))

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "Manager exited non-zero")
		os.Exit(1)
	}
}
