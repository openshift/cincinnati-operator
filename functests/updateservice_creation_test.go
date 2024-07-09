package functests

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"testing"

	updateservicev1 "github.com/openshift/cincinnati-operator/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
)

func TestCustomResource(t *testing.T) {
	ctx := context.Background()

	k8sClient, err := getK8sClient()
	if err != nil {
		t.Fatal(err)
	}

	if err := waitForDeployment(ctx, k8sClient, operatorName); err != nil {
		t.Fatal(err)
	}

	labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{"name": operatorName}}
	listOptions := metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	}
	if pod, err := k8sClient.CoreV1().Pods(operatorNamespace).List(ctx, listOptions); err != nil {
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

	if err := waitForService(ctx, k8sClient, operatorName+"-metrics"); err != nil {
		t.Fatal(err)
	}

	if err := deployCR(ctx); err != nil {
		t.Fatal(err)
	}

	updateServiceClient, err := getUpdateServiceClient()
	if err != nil {
		t.Fatal(err)
	}

	result := &updateservicev1.UpdateService{}
	err = updateServiceClient.Get().
		Resource(resource).
		Namespace(operatorNamespace).
		Name(customResourceName).
		Do(ctx).
		Into(result)
	if err != nil {
		t.Fatal(err)
	}

	if err := waitForDeployment(ctx, k8sClient, customResourceName); err != nil {
		t.Fatal(err)
	}

	labelSelector = metav1.LabelSelector{MatchLabels: map[string]string{"app": customResourceName}}
	listOptions = metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	}
	if pod, err := k8sClient.CoreV1().Pods(operatorNamespace).List(ctx, listOptions); err != nil {
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

	if err := waitForService(ctx, k8sClient, customResourceName+"-graph-builder"); err != nil {
		t.Fatal(err)
	}

	if err := waitForService(ctx, k8sClient, customResourceName+"-policy-engine"); err != nil {
		t.Fatal(err)
	}

	// Checks to see if a given PodDisruptionBudget is available after a specified amount of time.
	// If the PodDisruptionBudget is not available after 30 * retries seconds, the condition function returns an error.
	if err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		if _, err := k8sClient.PolicyV1().PodDisruptionBudgets(operatorNamespace).Get(ctx, customResourceName, metav1.GetOptions{}); err != nil {
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

	var policyEngineURI string
	if err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		result := &updateservicev1.UpdateService{}
		err = updateServiceClient.Get().
			Resource(resource).
			Namespace(operatorNamespace).
			Name(customResourceName).
			Do(ctx).
			Into(result)
		if err != nil {
			return false, err
		}
		if result.Status.PolicyEngineURI != "" {
			policyEngineURI = result.Status.PolicyEngineURI
			return true, nil
		}
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Logf("Policy engine route available at %s", policyEngineURI)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := &http.Client{Transport: tr}

	graphURI := fmt.Sprintf("%s/api/upgrades_info/v1/graph?channel=stable-4.4", policyEngineURI)
	req, err := http.NewRequestWithContext(ctx, "GET", graphURI, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "application/json")
	if err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		if resp, err := httpClient.Do(req); err != nil {
			t.Fatal(err)
		} else if resp.StatusCode > 200 {
			t.Logf("Waiting for availability of policy engine%s", graphURI)
			return false, nil
		}
		t.Logf("Policy engine %s available", graphURI)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	ctx = context.Background()
	if err := deleteCR(ctx); err != nil {
		t.Log(err)
	}

}
