package functests

import (
	"os"
	"os/exec"
	"time"

	"k8s.io/klog"
	"k8s.io/kubectl/pkg/scheme"

	cincinnativ1beta1 "github.com/openshift/cincinnati-operator/pkg/apis/cincinnati/v1beta1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	customResourceName = "example-cincinnati"
	operatorName       = "cincinnati-operator"
	operatorNamespace  = "openshift-cincinnati"
	crdName            = "cincinnatis.cincinnati.openshift.io"
	resource           = "cincinnatis"
	routeName          = customResourceName + "-policy-engine-route"
	replicas           = 1
	retryInterval      = time.Second * 30
	timeout            = time.Second * 600
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

// getCrClient is a function used to retrieve the controller runtime client
func getCrClient() (client.Client, error) {
	config, err := getConfig()
	if err != nil {
		return nil, err
	}
	crClient, err := client.New(config, client.Options{})
	if err != nil {
		return nil, err
	}
	return crClient, nil
}

// getCincinnatiClient is the function used to retrieve the cincinnati operator rest client
func getCincinnatiClient() (*rest.RESTClient, error) {
	cincinnatiConfig, err := getConfig()
	if err != nil {
		return nil, err
	}
	cincinnatiConfig.ContentType = runtime.ContentTypeJSON
	cincinnatiConfig.GroupVersion = &cincinnativ1beta1.SchemeGroupVersion
	cincinnatiConfig.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	cincinnatiConfig.APIPath = "/apis"
	cincinnatiConfig.ContentType = runtime.ContentTypeJSON
	if cincinnatiConfig.UserAgent == "" {
		cincinnatiConfig.UserAgent = rest.DefaultKubernetesUserAgent()
	}
	// Cincinnati Operator rest client
	cincinnatiClient, err := rest.RESTClientFor(cincinnatiConfig)
	if err != nil {
		return nil, err
	}
	return cincinnatiClient, nil
}

// deployCR is the function to deploy a cincinnati custom resource in the cluster
func deployCR() error {
	cmd := exec.Command("oc", "apply", "-f", "../deploy/crds/cincinnati.openshift.io_v1beta1_cincinnati_cr.yaml", "-n", operatorNamespace)
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
func waitForDeployment(k8sClient *kubernetes.Clientset, name string) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		deployment, err := k8sClient.AppsV1().Deployments(operatorNamespace).Get(name, metav1.GetOptions{})
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
func waitForService(k8sClient *kubernetes.Clientset, name string) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		_, err2 := k8sClient.CoreV1().Services(operatorNamespace).Get(name, metav1.GetOptions{})
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

// deleteServiceAccount is the function to delete a service account from the cluster
func deleteServiceAccount() error {
	klog.Info("Deleting service account")
	k8sClient, _ := getK8sClient()
	err := k8sClient.CoreV1().ServiceAccounts(operatorNamespace).Delete(operatorName, &metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// deleteClusterRole is the function to delete a cluster role from the cluster
func deleteClusterRole() error {
	klog.Info("Deleting cluster role")
	k8sClient, _ := getK8sClient()
	err := k8sClient.RbacV1().ClusterRoles().Delete(operatorName, &metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// deleteClusterRoleBinding is the function to delete a cluster role binding from the cluster
func deleteClusterRoleBinding() error {
	klog.Info("Deleting cluster role binding")
	k8sClient, _ := getK8sClient()
	err := k8sClient.RbacV1().ClusterRoleBindings().Delete(operatorName, &metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// deleteDeployment is the function to delete a deployment from the cluster
func deleteDeployment() error {
	klog.Info("Deleting deployment")
	k8sClient, _ := getK8sClient()
	err := k8sClient.AppsV1().Deployments(operatorNamespace).Delete(operatorName, &metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// deleteCR is the function to delete a custom resource from the cluster
func deleteCR() error {
	klog.Info("Deleting custom resource")
	cincinnatiClient, _ := getCincinnatiClient()
	err := cincinnatiClient.Delete().
		Resource(resource).
		Namespace(operatorNamespace).
		Name(customResourceName).
		Do().
		Error()
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// deleteCRD is the function to delete a custom resource definition from the cluster
func deleteCRD() error {
	klog.Info("Deleting custom resource definition")
	config, _ := getConfig()
	apiextensionsClient := apiextensionsclientset.NewForConfigOrDie(config)
	err := apiextensionsClient.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(crdName, &metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// deleteNamespace is the function to delete a namespace from the cluster
func deleteNamespace() error {
	klog.Info("Deleting namespace")
	k8sClient, _ := getK8sClient()
	err := k8sClient.CoreV1().Namespaces().Delete(operatorNamespace, &metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
