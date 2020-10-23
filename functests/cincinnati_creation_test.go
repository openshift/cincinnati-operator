package functests

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	cincinnativ1beta1 "github.com/openshift/cincinnati-operator/pkg/apis/cincinnati/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCustomResource(t *testing.T) {
	k8sClient, err := getK8sClient()
	if err != nil {
		t.Fatal(err)
	}

	if err := waitForDeployment(k8sClient, operatorName); err != nil {
		t.Fatal(err)
	}

	labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{"name": operatorName}}
	listOptions := metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	}
	if pod, err := k8sClient.CoreV1().Pods(operatorNamespace).List(listOptions); err != nil {
		t.Fatal(err)
	} else {
		if len(pod.Items) < 1 {
			t.Fatalf("no pods found in %s matching %s", operatorNamespace, operatorName)
		}

		if pod.Items[0].Status.Phase != "Running" {
			t.Fatalf("unexpected pod %s phase %q (expected Running)", operatorName, pod.Items[0].Status.Phase)
		}

		if !pod.Items[0].Status.ContainerStatuses[0].Ready {
			t.Fatalf("unexpected pod %s container status ready %t (expected true)", operatorName, pod.Items[0].Status.ContainerStatuses[0].Ready)
		}
	}

	/* FIXME: I don't care about metrics at the momement
	if err := waitForService(k8sClient, operatorName+"-metrics"); err != nil {
		t.Fatal(err)
	}
	*/

	defer func() {
		if err := deleteCR(); err != nil {
			t.Log(err)
		}
	}()

	if err := deployCR(); err != nil {
		t.Fatal(err)
	}

	cincinnatiClient, err := getCincinnatiClient()
	if err != nil {
		t.Fatal(err)
	}

	result := &cincinnativ1beta1.Cincinnati{}
	err = cincinnatiClient.Get().
		Resource(resource).
		Namespace(operatorNamespace).
		Name(customResourceName).
		Do().
		Into(result)
	if err != nil {
		t.Fatal(err)
	}

	if err := waitForDeployment(k8sClient, customResourceName); err != nil {
		t.Fatal(err)
	}

	labelSelector = metav1.LabelSelector{MatchLabels: map[string]string{"app": customResourceName}}
	listOptions = metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	}
	if pod, err := k8sClient.CoreV1().Pods(operatorNamespace).List(listOptions); err != nil {
		t.Fatal(err)
	} else {
		if len(pod.Items) < 1 {
			t.Fatalf("no pods found in %s matching %s", operatorNamespace, customResourceName)
		}

		if pod.Items[0].Status.Phase != "Running" {
			t.Fatalf("unexpected pod %s phase %q (expected Running)", customResourceName, pod.Items[0].Status.Phase)
		}

		for _, container := range pod.Items[0].Status.InitContainerStatuses {
			if !container.Ready {
				t.Fatalf("unexpected pod %s init-container %s status ready %t (expected true)", customResourceName, container.Name, container.Ready)
			}
		}

		for _, container := range pod.Items[0].Status.ContainerStatuses {
			if !container.Ready {
				t.Fatalf("unexpected pod %s container %s status ready %t (expected true)", customResourceName, container.Name, container.Ready)
			}
		}
	}

	if err := waitForService(k8sClient, customResourceName+"-graph-builder"); err != nil {
		t.Fatal(err)
	}

	if err := waitForService(k8sClient, customResourceName+"-policy-engine"); err != nil {
		t.Fatal(err)
	}

	// Checks to see if a given PodDisruptionBudget is available after a specified amount of time.
	// If the PodDisruptionBudget is not available after 30 * retries seconds, the condition function returns an error.
	if err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		if _, err := k8sClient.PolicyV1beta1().PodDisruptionBudgets(operatorNamespace).Get(customResourceName, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of %s PodDisruptionBudget\n", operatorName)
				return false, nil
			}
			return false, err
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Logf("PodDisruptionBudget %s available", operatorName)

	crClient, err := getCrClient()
	if err != nil {
		t.Fatal(err)
	}

	route := &routev1.Route{}
	if err := crClient.Get(context.Background(), client.ObjectKey{
		Namespace: operatorNamespace,
		Name:      routeName,
	}, route); err != nil {
		t.Fatal(err)
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := &http.Client{Transport: tr}

	req, err := http.NewRequest("GET", fmt.Sprintf("https://%s/api/upgrades_info/v1/graph?channel=stable-4.4", route.Spec.Host), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "application/json")
	if err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		if resp, err := httpClient.Do(req); err != nil {
			t.Fatal(err)
		} else if resp.StatusCode > 200 {
			t.Logf("Waiting for availability of %s route", routeName)
			return false, nil
		}
		t.Logf("Route %s available", routeName)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
}
