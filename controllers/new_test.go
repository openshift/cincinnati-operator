package controllers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/yaml"

	cv1 "github.com/openshift/cincinnati-operator/api/v1"
)

func Test_newKubeResources(t *testing.T) {
	collected := sets.New[string]()
	sample := &cv1.UpdateService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample",
			Namespace: "sample-ns",
		},
		Spec: cv1.UpdateServiceSpec{
			GraphDataImage: "example.com/library/graph-data@sha256:123",
			Releases:       "example.com/library/xyz@sha256:123",
		},
	}
	actual, actualErr := newKubeResources(
		sample,
		"image",
		&corev1.Secret{Data: map[string][]byte{"a": []byte("b")}},
		&corev1.ConfigMap{Data: map[string]string{"a": "b"}},
		nil,
	)
	assert.Nil(t, actualErr)
	dir := filepath.Join("testdata", "resources")
	for k, v := range map[string]interface{}{
		"env_config_map":           actual.envConfig,
		"graph_builder_config_map": actual.graphBuilderConfig,
		"deployment":               actual.deployment,
		"graph_builder_service":    actual.graphBuilderService,
		"network_policy":           actual.networkPolicy,
		"pod_disruption_budget":    actual.podDisruptionBudget,
		"policy_engine_old_route":  actual.policyEngineRoute,
		"policy_engine_route":      actual.policyEngineRoute,
		"policy_engine_service":    actual.policyEngineService,
		"pull_secret":              actual.pullSecret,
		"trusted_ca_config_map":    actual.trustedCAConfig,
	} {
		manifestFile := filepath.Join(dir, fmt.Sprintf("zz_fixture_%s_%s.yaml", t.Name(), k))
		collected.Insert(manifestFile)
		data, err := yaml.Marshal(v)
		assert.Nil(t, err)

		if strings.ToLower(os.Getenv("UPDATE")) == "true" {
			assert.Nil(t, os.WriteFile(manifestFile, data, 0644))
		}

		expected, err := os.ReadFile(manifestFile)
		assert.Nil(t, err)
		assert.Equal(t, string(expected), string(data))
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory: %v", err)
	}
	existing := sets.New[string]()
	for _, entry := range files {
		if entry.IsDir() {
			t.Fatalf("found an unexpected directory: %s", entry.Name())
		}
		existing.Insert(filepath.Join(dir, entry.Name()))
	}
	assert.Empty(t, existing.Difference(collected), "We should extend the mapping in the test to allow for more fixture files")
}

func Test_egressPorts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		releases   string
		httpProxy  string
		httpsProxy string
		noProxy    string
		expected   []int32
	}{
		{
			name:     "default port when no port in registry URI",
			releases: "quay.io/openshift-release-dev/ocp-release",
			expected: []int32{443},
		},
		{
			name:     "explicit port in registry URI",
			releases: "registry.example.com:8080/openshift/release",
			expected: []int32{8080},
		},
		{
			name:       "proxy port used instead of registry port for external registry",
			releases:   "quay.io/openshift-release-dev/ocp-release",
			httpsProxy: "https://proxy.example.com:3128",
			expected:   []int32{3128},
		},
		{
			name:       "registry as no proxy",
			releases:   "quay.io/openshift-release-dev/ocp-release",
			httpsProxy: "https://proxy.example.com:3128",
			noProxy:    "quay.io, docker.io",
			expected:   []int32{443},
		},
		{
			name:       "registry and no proxy with ip",
			releases:   "10.1.0.3:8443/openshift-release-dev/ocp-release",
			httpsProxy: "https://proxy.example.com:3128",
			noProxy:    "10.1.0.3, quay.io, docker.io",
			expected:   []int32{8443},
		},
		{
			name:       "registry and no proxy with CIDR",
			releases:   "10.1.0.2:8443/openshift-release-dev/ocp-release",
			httpsProxy: "https://proxy.example.com:3128",
			noProxy:    "10.1.0.0/16, quay.io, docker.io",
			expected:   []int32{8443},
		},
		{
			name:       "registry and no proxy with exact subdomain",
			releases:   "sub.quay.io/openshift-release-dev/ocp-release",
			httpsProxy: "https://proxy.example.com:3128",
			noProxy:    "quay.io, docker.io",
			expected:   []int32{3128},
		},
		{
			name:       "registry and no proxy with dot",
			releases:   "sub.quay.io/openshift-release-dev/ocp-release",
			httpsProxy: "https://proxy.example.com:3128",
			noProxy:    ".quay.io, docker.io",
			expected:   []int32{443},
		},
		{
			name:       "registry and no proxy with asterisk",
			releases:   "sub.quay.io/openshift-release-dev/ocp-release",
			httpsProxy: "https://proxy.example.com:3128",
			noProxy:    "docker.io, *",
			expected:   []int32{443},
		},
		{
			name:       "proxy port ignored for cluster-internal registry (.svc)",
			releases:   "image-registry.openshift-image-registry.svc:5000/openshift/release",
			httpsProxy: "https://proxy.example.com:3128",
			expected:   []int32{5000},
		},
		{
			name:       "proxy port ignored for .svc.cluster.local registry",
			releases:   "quay.apps.internal.svc.cluster.local:5000/openshift/release",
			httpsProxy: "https://proxy.example.com:3128",
			expected:   []int32{5000},
		},
		{
			name:      "HTTP_PROXY port used instead of registry port",
			releases:  "quay.io/openshift-release-dev/ocp-release",
			httpProxy: "https://proxy.example.com:8888",
			expected:  []int32{8888},
		},
		{
			name:       "dedup when proxy port equals registry port",
			releases:   "quay.io/openshift-release-dev/ocp-release",
			httpsProxy: "https://proxy.example.com:443",
			expected:   []int32{443},
		},
		{
			name:       "http proxy with IP",
			releases:   "quay.io/openshift-release-dev/ocp-release",
			httpsProxy: "https://127.0.0.1:8443",
			expected:   []int32{8443},
		},
		{
			name:       "proxy without explicit port defaults to 443 for https",
			releases:   "quay.io/openshift-release-dev/ocp-release",
			httpsProxy: "https://proxy.example.com",
			expected:   []int32{443},
		},
		{
			name:      "proxy without explicit port defaults to 80 for http",
			releases:  "quay.io/openshift-release-dev/ocp-release",
			httpProxy: "http://proxy.example.com",
			expected:  []int32{80},
		},
		{
			name:       "host with svc in domain name is not cluster-internal",
			releases:   "my.svc.company.com/openshift/release",
			httpsProxy: "https://proxy.example.com:3128",
			expected:   []int32{3128},
		},
		{
			name:       "both HTTP_PROXY and HTTPS_PROXY with different ports",
			releases:   "quay.io/openshift-release-dev/ocp-release",
			httpProxy:  "http://user:pass@proxy.example.com:8080",
			httpsProxy: "https://user:pass@proxy.example.com:3128",
			expected:   []int32{8080, 3128},
		}, {
			name:       "bad proxy",
			releases:   "my.svc.company.com/openshift/release",
			httpsProxy: "i am a very bad proxy",
			expected:   []int32{443},
		},
		{
			name:      "bad proxy port",
			releases:  "my.svc.company.com/openshift/release",
			httpProxy: "http://user:pass@proxy.example.com:80000080",
			expected:  []int32{443},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HTTP_PROXY", tc.httpProxy)
			t.Setenv("HTTPS_PROXY", tc.httpsProxy)
			t.Setenv("NO_PROXY", tc.noProxy)

			got := egressPorts(tc.releases)
			assert.ElementsMatch(t, tc.expected, got)
		})
	}
}

func Test_newNetworkPolicy_egress_ports(t *testing.T) {
	for _, tc := range []struct {
		name       string
		releases   string
		httpProxy  string
		httpsProxy string
		wantPorts  []int32
	}{
		{
			name:      "default registry gets port 443",
			releases:  "quay.io/openshift-release-dev/ocp-release",
			wantPorts: []int32{443},
		},
		{
			name:      "custom port in registry URI",
			releases:  "registry.example.com:5000/library/images",
			wantPorts: []int32{5000},
		},
		{
			name:       "proxy port replaces registry port for external registry",
			releases:   "quay.io/openshift-release-dev/ocp-release",
			httpsProxy: "http://squid.corp:3128",
			wantPorts:  []int32{3128},
		},
		{
			name:       "proxy port ignored for .svc registry",
			releases:   "image-registry.openshift-image-registry.svc:5000/openshift/release",
			httpsProxy: "http://squid.corp:3128",
			wantPorts:  []int32{5000},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HTTP_PROXY", tc.httpProxy)
			t.Setenv("HTTPS_PROXY", tc.httpsProxy)

			instance := &cv1.UpdateService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test-ns",
				},
				Spec: cv1.UpdateServiceSpec{
					Releases: tc.releases,
				},
			}
			k := &kubeResources{}
			np := k.newNetworkPolicy(instance)

			// First egress rule should have specific ports
			assert.NotEmpty(t, np.Spec.Egress)
			registryEgress := np.Spec.Egress[0]
			var gotPorts []int32
			for _, p := range registryEgress.Ports {
				gotPorts = append(gotPorts, p.Port.IntVal)
				assert.Equal(t, corev1.ProtocolTCP, *p.Protocol)
			}
			assert.ElementsMatch(t, tc.wantPorts, gotPorts)
		})
	}
}
