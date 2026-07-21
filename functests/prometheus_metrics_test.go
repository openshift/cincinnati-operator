package functests

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	monitoringNamespace    = "openshift-monitoring"
	serviceMonitorPath     = "../config/prometheus/monitor.yaml"
	prometheusReadyTimeout = time.Minute * 5

	monitoringSAName  = "prometheus-metrics-test"
	monitoringCRBName = "prometheus-metrics-test"
)

func TestPrometheusMetrics(t *testing.T) {
	ctx := context.Background()

	k8sClient, err := getK8sClient()
	if err != nil {
		t.Fatal(err)
	}

	// Step 1: Verify Prometheus is running in openshift-monitoring.
	// On OpenShift the monitoring stack is installed by default; use a
	// shorter timeout here since we just need to confirm it exists.
	if err := wait.PollUntilContextTimeout(ctx, retryInterval, prometheusReadyTimeout, true, func(pollCtx context.Context) (bool, error) {
		ss, err := k8sClient.AppsV1().StatefulSets(monitoringNamespace).Get(pollCtx, "prometheus-k8s", metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Log("Waiting for prometheus-k8s StatefulSet")
				return false, nil
			}
			return false, err
		}
		if ss.Status.ReadyReplicas > 0 {
			return true, nil
		}
		t.Log("Waiting for prometheus-k8s to have ready replicas")
		return false, nil
	}); err != nil {
		t.Skipf("Prometheus not available in openshift-monitoring, skipping: %v", err)
	}
	t.Log("Prometheus is running in openshift-monitoring")

	// Ensure the operator deployment is running (it serves the metrics endpoint).
	if err := waitForDeployment(ctx, k8sClient, operatorName); err != nil {
		t.Fatal(err)
	}

	// Step 2: Label the operator namespace so that the platform Prometheus
	// discovers ServiceMonitors in it. The openshift-monitoring Prometheus
	// only watches namespaces with the openshift.io/cluster-monitoring label.
	ns, err := k8sClient.CoreV1().Namespaces().Get(ctx, operatorNamespace, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get namespace %s: %v", operatorNamespace, err)
	}
	if ns.Labels == nil {
		ns.Labels = map[string]string{}
	}
	if ns.Labels["openshift.io/cluster-monitoring"] != "true" {
		ns.Labels["openshift.io/cluster-monitoring"] = "true"
		if _, err := k8sClient.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{}); err != nil {
			t.Fatalf("failed to label namespace for monitoring: %v", err)
		}
		t.Logf("Namespace %s labeled for cluster monitoring", operatorNamespace)
	}

	// Step 3: Apply the ServiceMonitor from the repo. This resource is not
	// installed automatically; it tells Prometheus how to scrape the
	// operator's TLS-secured metrics endpoint.
	applyCmd := exec.CommandContext(ctx, "oc", "apply", "-f", serviceMonitorPath)
	if out, err := applyCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to apply ServiceMonitor: %v\n%s", err, out)
	}
	t.Log("ServiceMonitor applied")
	defer func() {
		delCmd := exec.CommandContext(context.Background(), "oc", "delete", "-f", serviceMonitorPath, "--ignore-not-found")
		if out, err := delCmd.CombinedOutput(); err != nil {
			t.Logf("failed to delete ServiceMonitor: %v\n%s", err, out)
		}
	}()

	// Step 4: Create a ServiceAccount with cluster-monitoring-view permissions
	// and mint a bearer token for querying thanos-querier. The CI kubeconfig
	// uses client certificate auth which thanos-querier's OAuth proxy does
	// not accept, so we need an explicit bearer token.
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      monitoringSAName,
			Namespace: operatorNamespace,
		},
	}
	if _, err := k8sClient.CoreV1().ServiceAccounts(operatorNamespace).Create(ctx, sa, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create monitoring ServiceAccount: %v", err)
	}
	defer func() {
		if err := k8sClient.CoreV1().ServiceAccounts(operatorNamespace).Delete(context.Background(), monitoringSAName, metav1.DeleteOptions{}); err != nil {
			t.Logf("failed to delete monitoring ServiceAccount: %v", err)
		}
	}()

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: monitoringCRBName,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "cluster-monitoring-view",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      monitoringSAName,
				Namespace: operatorNamespace,
			},
		},
	}
	if _, err := k8sClient.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create ClusterRoleBinding: %v", err)
	}
	defer func() {
		if err := k8sClient.RbacV1().ClusterRoleBindings().Delete(context.Background(), monitoringCRBName, metav1.DeleteOptions{}); err != nil {
			t.Logf("failed to delete ClusterRoleBinding: %v", err)
		}
	}()

	tokenReq, err := k8sClient.CoreV1().ServiceAccounts(operatorNamespace).CreateToken(ctx, monitoringSAName, &authenticationv1.TokenRequest{}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create token for monitoring ServiceAccount: %v", err)
	}
	bearerToken := tokenReq.Status.Token
	t.Log("Created monitoring ServiceAccount with cluster-monitoring-view and minted bearer token")

	// Step 5: Get the thanos-querier route host and build an authenticated client.
	// thanos-querier is the authenticated Prometheus API proxy on OpenShift.
	hostCmd := exec.CommandContext(ctx, "oc", "get", "route", "thanos-querier",
		"-n", monitoringNamespace, "-o", "jsonpath={.spec.host}")
	hostOut, err := hostCmd.Output()
	if err != nil {
		t.Fatalf("failed to get thanos-querier route: %v", err)
	}
	thanosHost := strings.TrimSpace(string(hostOut))
	if thanosHost == "" {
		t.Fatal("thanos-querier route host is empty")
	}
	t.Logf("Thanos querier at %s", thanosHost)

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-only, cluster-internal route
		},
	}

	// Step 6: Poll Prometheus until operator metrics appear.
	query := `up{service="updateservice-operator-metrics"}`
	queryURL := fmt.Sprintf("https://%s/api/v1/query?query=%s", thanosHost, url.QueryEscape(query))

	if err := wait.PollUntilContextTimeout(ctx, retryInterval, timeout, true, func(pollCtx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(pollCtx, "GET", queryURL, nil)
		if err != nil {
			return false, err
		}
		req.Header.Set("Authorization", "Bearer "+bearerToken)

		resp, err := httpClient.Do(req)
		if err != nil {
			t.Logf("Error querying Prometheus: %v", err)
			return false, nil
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Logf("Error reading response: %v", err)
			return false, nil
		}

		if resp.StatusCode != http.StatusOK {
			t.Logf("Prometheus returned %d: %s", resp.StatusCode, body)
			return false, nil
		}

		var promResp prometheusResponse
		if err := json.Unmarshal(body, &promResp); err != nil {
			t.Logf("Error parsing Prometheus response: %v", err)
			return false, nil
		}

		if promResp.Status != "success" {
			t.Logf("Prometheus query status: %s", promResp.Status)
			return false, nil
		}

		if len(promResp.Data.Result) == 0 {
			t.Log("Waiting for metrics to appear in Prometheus")
			return false, nil
		}

		t.Logf("Found %d metric result(s) for query %q", len(promResp.Data.Result), query)
		return true, nil
	}); err != nil {
		t.Fatalf("Operator metrics never appeared in Prometheus: %v", err)
	}

	t.Log("Operator metrics successfully scraped by Prometheus via ServiceMonitor")
}

// prometheusResponse represents the relevant fields of the Prometheus
// /api/v1/query JSON response.
type prometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"`
		} `json:"result"`
	} `json:"data"`
}
