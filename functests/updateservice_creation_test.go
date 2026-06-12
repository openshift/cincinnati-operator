package functests

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"testing"

	updateservicev1 "github.com/openshift/cincinnati-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	if err := wait.PollUntilContextTimeout(ctx, retryInterval, timeout, true, func(_ context.Context) (done bool, err error) {
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

	// Checks to see if the NetworkPolicy is available and has the expected rules.
	var np *networkingv1.NetworkPolicy
	if err := wait.PollUntilContextTimeout(ctx, retryInterval, timeout, true, func(_ context.Context) (done bool, err error) {
		np, err = k8sClient.NetworkingV1().NetworkPolicies(operatorNamespace).Get(ctx, customResourceName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of %s NetworkPolicy", customResourceName)
				return false, nil
			}
			return false, err
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Logf("NetworkPolicy %s available", customResourceName)

	if len(np.OwnerReferences) < 1 {
		t.Fatal("NetworkPolicy has no owner references")
	}
	if np.OwnerReferences[0].Name != customResourceName {
		t.Fatalf("NetworkPolicy owner reference name %q does not match expected %q", np.OwnerReferences[0].Name, customResourceName)
	}

	// Validate ingress rules: router namespace → policy-engine port
	if len(np.Spec.Ingress) != 1 {
		t.Fatalf("expected 1 ingress rule, got %d", len(np.Spec.Ingress))
	}
	ingressRule := np.Spec.Ingress[0]
	if len(ingressRule.From) != 1 {
		t.Fatalf("expected 1 ingress peer, got %d", len(ingressRule.From))
	}
	ingressNSSelector := ingressRule.From[0].NamespaceSelector
	if ingressNSSelector == nil {
		t.Fatal("ingress rule missing namespace selector")
	}
	if _, ok := ingressNSSelector.MatchLabels["policy-group.network.openshift.io/ingress"]; !ok {
		t.Fatal("ingress namespace selector missing policy-group.network.openshift.io/ingress label")
	}
	if len(ingressRule.Ports) != 1 {
		t.Fatalf("expected 1 ingress port, got %d", len(ingressRule.Ports))
	}
	expectedIngressPort := intstr.FromString("policy-engine")
	if *ingressRule.Ports[0].Port != expectedIngressPort {
		t.Fatalf("expected ingress port %v, got %v", expectedIngressPort, *ingressRule.Ports[0].Port)
	}

	// Validate egress rules: registry (port 443 TCP) and DNS (openshift-dns, port 5353)
	if len(np.Spec.Egress) != 2 {
		t.Fatalf("expected 2 egress rules, got %d", len(np.Spec.Egress))
	}

	registryEgress := np.Spec.Egress[0]
	if len(registryEgress.Ports) < 1 {
		t.Fatal("registry egress rule has no ports")
	}
	expectedRegistryPort := intstr.FromInt32(443)
	if *registryEgress.Ports[0].Port != expectedRegistryPort {
		t.Fatalf("expected registry egress port %v, got %v", expectedRegistryPort, *registryEgress.Ports[0].Port)
	}
	if *registryEgress.Ports[0].Protocol != corev1.ProtocolTCP {
		t.Fatalf("expected registry egress protocol TCP, got %v", *registryEgress.Ports[0].Protocol)
	}
	if len(registryEgress.To) != 0 {
		t.Fatalf("expected no namespace restriction for external registry egress, got %d peers", len(registryEgress.To))
	}

	dnsEgress := np.Spec.Egress[1]
	if len(dnsEgress.To) != 1 {
		t.Fatalf("expected 1 DNS egress peer, got %d", len(dnsEgress.To))
	}
	dnsNSSelector := dnsEgress.To[0].NamespaceSelector
	if dnsNSSelector == nil {
		t.Fatal("DNS egress rule missing namespace selector")
	}
	if dnsNSSelector.MatchLabels["kubernetes.io/metadata.name"] != "openshift-dns" {
		t.Fatalf("DNS egress namespace selector expected openshift-dns, got %q", dnsNSSelector.MatchLabels["kubernetes.io/metadata.name"])
	}
	dnsPodSelector := dnsEgress.To[0].PodSelector
	if dnsPodSelector == nil {
		t.Fatal("DNS egress rule missing pod selector")
	}
	if dnsPodSelector.MatchLabels["dns.operator.openshift.io/daemonset-dns"] != "default" {
		t.Fatalf("DNS egress pod selector expected default, got %q", dnsPodSelector.MatchLabels["dns.operator.openshift.io/daemonset-dns"])
	}
	if len(dnsEgress.Ports) != 2 {
		t.Fatalf("expected 2 DNS egress ports (TCP+UDP 5353), got %d", len(dnsEgress.Ports))
	}
	expectedDNSPort := intstr.FromInt32(5353)
	var sawTCP, sawUDP bool
	for _, p := range dnsEgress.Ports {
		if *p.Port != expectedDNSPort {
			t.Fatalf("expected DNS port 5353, got %v", *p.Port)
		}
		switch *p.Protocol {
		case corev1.ProtocolTCP:
			sawTCP = true
		case corev1.ProtocolUDP:
			sawUDP = true
		default:
			t.Fatalf("unexpected DNS egress protocol %v", *p.Protocol)
		}
	}
	if !sawTCP || !sawUDP {
		t.Fatalf("DNS egress should have both TCP and UDP, got TCP=%t UDP=%t", sawTCP, sawUDP)
	}
	t.Log("NetworkPolicy ingress and egress rules validated")

	var policyEngineURI string
	if err := wait.PollUntilContextTimeout(ctx, retryInterval, timeout, true, func(_ context.Context) (done bool, err error) {
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

	graphURI := fmt.Sprintf("%s/api/upgrades_info/graph?channel=stable-4.13", policyEngineURI)
	req, err := http.NewRequestWithContext(ctx, "GET", graphURI, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "application/json")
	if err := wait.PollUntilContextTimeout(ctx, retryInterval, timeout, true, func(_ context.Context) (done bool, err error) {
		if resp, err := httpClient.Do(req); err != nil {
			t.Fatal(err)
		} else if resp.StatusCode > http.StatusOK {
			t.Logf("Waiting for availability of policy engine %s", graphURI)
			return false, nil
		}
		t.Logf("Policy engine %s available", graphURI)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := deleteCR(ctx); err != nil {
		t.Log(err)
	}

	// Verify NetworkPolicy is garbage-collected after CR deletion via owner references.
	if err := wait.PollUntilContextTimeout(ctx, retryInterval, timeout, true, func(_ context.Context) (done bool, err error) {
		_, err = k8sClient.NetworkingV1().NetworkPolicies(operatorNamespace).Get(ctx, customResourceName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		t.Logf("Waiting for deletion of %s NetworkPolicy", customResourceName)
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Logf("NetworkPolicy %s garbage-collected after CR deletion", customResourceName)
}
