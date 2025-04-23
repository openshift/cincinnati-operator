package functests

import (
	"context"
	"os"
	"os/exec"
	"time"

	"k8s.io/klog"
	"k8s.io/kubectl/pkg/scheme"

	updateservicev1 "github.com/openshift/cincinnati-operator/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	customResourceName = "example"
	operatorName       = "updateservice-operator"
	operatorNamespace  = "openshift-update-service"
	crdName            = "updateservices.updateservice.operator.openshift.io"
	resource           = "updateservices"
	replicas           = 1
	retryInterval      = time.Second * 30
	timeout            = time.Minute * 20
)

// getConfig is the function used to retrieve the kubernetes config
func getConfig() (*rest.Config, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	// K8s Core api client
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}

// getK8sClient is the function used to retrieve the kubernetes client
func getK8sClient() (*kubernetes.Clientset, error) {
	config, err := getConfig()
	if err != nil {
		return nil, err
	}
	k8sClient, err2 := kubernetes.NewForConfig(config)
	if err2 != nil {
		return nil, err2
	}
	return k8sClient, nil
}

// getUpdateServiceClient is the function used to retrieve the updateservice operator rest client
func getUpdateServiceClient() (*rest.RESTClient, error) {
	updateserviceConfig, err := getConfig()
	if err != nil {
		return nil, err
	}
	updateserviceConfig.ContentType = runtime.ContentTypeJSON
	updateserviceConfig.GroupVersion = &updateservicev1.GroupVersion
	updateserviceConfig.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	updateserviceConfig.APIPath = "/apis"
	updateserviceConfig.ContentType = runtime.ContentTypeJSON
	if updateserviceConfig.UserAgent == "" {
		updateserviceConfig.UserAgent = rest.DefaultKubernetesUserAgent()
	}
	// UpdateService Operator rest client
	updateserviceClient, err := rest.RESTClientFor(updateserviceConfig)
	if err != nil {
		return nil, err
	}
	return updateserviceClient, nil
}

// deployCR is the function to deploy a cincinnati custom resource in the cluster
func deployCR(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "oc", "apply", "-f", "../config/samples/updateservice.operator.openshift.io_v1_updateservice_cr.yaml", "-n", operatorNamespace)
	output, err := cmd.Output()
	if err != nil {
		return err
	}
	klog.Info(string(output))
	return nil
}

// waitForDeployment checks to see if a given deployment has a certain number of available replicas after a specified
// amount of time. If the deployment does not have the required number of replicas after 30 * retries seconds,
// the function returns an error.
func waitForDeployment(ctx context.Context, k8sClient *kubernetes.Clientset, name string) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		deployment, err := k8sClient.AppsV1().Deployments(operatorNamespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				klog.Infof("Waiting for availability of %s deployment\n", name)
				return false, nil
			}
			return false, err
		}
		if int(deployment.Status.AvailableReplicas) >= replicas {
			return true, nil
		}
		klog.Infof("Waiting for full availability of %s deployment (%d/%d)\n", name,
			deployment.Status.AvailableReplicas, replicas)
		return false, nil
	})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	klog.Infof("Deployment %s available (%d/%d)\n", name, replicas, replicas)
	return nil
}

// waitForService checks to see if a given service is available after a specified amount of time.
// If the service is not available after 30 * retries seconds, the function returns an error.
func waitForService(ctx context.Context, k8sClient *kubernetes.Clientset, name string) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		_, err2 := k8sClient.CoreV1().Services(operatorNamespace).Get(ctx, name, metav1.GetOptions{})
		if err2 != nil {
			if apierrors.IsNotFound(err2) {
				klog.Infof("Waiting for availability of %s service\n", name)
				return false, nil
			}
			return false, err2
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	klog.Infof("Service %s available\n", name)
	return nil
}

// deleteCR is the function to delete a custom resource from the cluster
func deleteCR(ctx context.Context) error {
	klog.Info("Deleting custom resource")
	updateserviceClient, _ := getUpdateServiceClient()
	err := updateserviceClient.Delete().
		Resource(resource).
		Namespace(operatorNamespace).
		Name(customResourceName).
		Do(ctx).
		Error()
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
