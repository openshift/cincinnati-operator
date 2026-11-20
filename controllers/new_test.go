package controllers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

func Test_namespaceFromInternalHost(t *testing.T) {
	for _, tc := range []struct {
		name     string
		host     string
		expected string
	}{
		{
			name:     "service.namespace.svc",
			host:     "image-registry.openshift-image-registry.svc",
			expected: "openshift-image-registry",
		},
		{
			name:     "service.namespace.svc.cluster.local",
			host:     "image-registry.openshift-image-registry.svc.cluster.local",
			expected: "openshift-image-registry",
		},
		{
			name:     "service.namespace.svc with port",
			host:     "image-registry.openshift-image-registry.svc:5000",
			expected: "openshift-image-registry",
		},
		{
			name:     "service.namespace.svc.cluster.local with port",
			host:     "image-registry.openshift-image-registry.svc.cluster.local:5000",
			expected: "openshift-image-registry",
		},
		{
			name: "external host returns empty",
			host: "quay.io",
		},
		{
			name: "bare hostname returns empty",
			host: "localhost",
		},
		{
			name: "svc in domain name but not cluster-internal",
			host: "my.svc.company.com",
		},
		{
			name: "service only with .svc suffix returns empty",
			host: "registry.svc",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := namespaceFromInternalHost(tc.host)
			assert.Equal(t, tc.expected, got)
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

func Test_newNetworkPolicy_egress_namespace(t *testing.T) {
	for _, tc := range []struct {
		name          string
		releases      string
		wantNamespace string
	}{
		{
			name:          "external registry has no namespace restriction",
			releases:      "quay.io/openshift-release-dev/ocp-release",
			wantNamespace: "",
		},
		{
			name:          "cluster-internal .svc restricts to namespace",
			releases:      "image-registry.openshift-image-registry.svc:5000/openshift/release",
			wantNamespace: "openshift-image-registry",
		},
		{
			name:          "cluster-internal .svc.cluster.local restricts to namespace",
			releases:      "image-registry.openshift-image-registry.svc.cluster.local:5000/openshift/release",
			wantNamespace: "openshift-image-registry",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HTTP_PROXY", "")
			t.Setenv("HTTPS_PROXY", "")

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

			registryEgress := np.Spec.Egress[0]
			if tc.wantNamespace == "" {
				assert.Empty(t, registryEgress.To, "external registry should have no destination restriction")
			} else {
				assert.Len(t, registryEgress.To, 1)
				assert.Equal(t, tc.wantNamespace, registryEgress.To[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"])
			}
		})
	}
}

func Test_mergeResourceRequirements(t *testing.T) {
	for _, tc := range []struct {
		name             string
		overrides        cv1.ResourceRequirements
		expectedLimits   map[corev1.ResourceName]string
		expectedRequests map[corev1.ResourceName]string
	}{
		{
			name:             "no overrides keeps defaults",
			expectedLimits:   map[corev1.ResourceName]string{corev1.ResourceCPU: "750m", corev1.ResourceMemory: "512Mi"},
			expectedRequests: map[corev1.ResourceName]string{corev1.ResourceCPU: "150m", corev1.ResourceMemory: "64Mi"},
		},
		{
			name: "memory limit override keeps other defaults",
			overrides: cv1.ResourceRequirements{
				Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")},
			},
			expectedLimits:   map[corev1.ResourceName]string{corev1.ResourceCPU: "750m", corev1.ResourceMemory: "2Gi"},
			expectedRequests: map[corev1.ResourceName]string{corev1.ResourceCPU: "150m", corev1.ResourceMemory: "64Mi"},
		},
		{
			name: "request override keeps other defaults",
			overrides: cv1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
			},
			expectedLimits:   map[corev1.ResourceName]string{corev1.ResourceCPU: "750m", corev1.ResourceMemory: "512Mi"},
			expectedRequests: map[corev1.ResourceName]string{corev1.ResourceCPU: "150m", corev1.ResourceMemory: "256Mi"},
		},
		{
			name: "new resource can be added",
			overrides: cv1.ResourceRequirements{
				Limits: corev1.ResourceList{corev1.ResourceName("example.com/gpu"): resource.MustParse("1")},
			},
			expectedLimits:   map[corev1.ResourceName]string{corev1.ResourceCPU: "750m", corev1.ResourceMemory: "512Mi", corev1.ResourceName("example.com/gpu"): "1"},
			expectedRequests: map[corev1.ResourceName]string{corev1.ResourceCPU: "150m", corev1.ResourceMemory: "64Mi"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual := mergeResourceRequirements(defaultOperandResources(), tc.overrides)
			assertResourceList(t, tc.expectedLimits, actual.Limits)
			assertResourceList(t, tc.expectedRequests, actual.Requests)
		})
	}
}

func Test_newOperandResources(t *testing.T) {
	instance := &cv1.UpdateService{
		Spec: cv1.UpdateServiceSpec{
			Resources: cv1.UpdateServiceResources{
				GraphBuilder: cv1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")},
				},
				PolicyEngine: cv1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
				},
			},
		},
	}

	actual := newOperandResources(instance)
	assertResourceList(t,
		map[corev1.ResourceName]string{corev1.ResourceCPU: "750m", corev1.ResourceMemory: "2Gi"},
		actual.graphBuilder.Limits)
	assertResourceList(t,
		map[corev1.ResourceName]string{corev1.ResourceCPU: "150m", corev1.ResourceMemory: "64Mi"},
		actual.graphBuilder.Requests)
	assertResourceList(t,
		map[corev1.ResourceName]string{corev1.ResourceCPU: "2", corev1.ResourceMemory: "512Mi"},
		actual.policyEngine.Limits)
	assertResourceList(t,
		map[corev1.ResourceName]string{corev1.ResourceCPU: "150m", corev1.ResourceMemory: "64Mi"},
		actual.policyEngine.Requests)
}

func Test_newKubeResources_resources(t *testing.T) {
	sample := &cv1.UpdateService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample",
			Namespace: "sample-ns",
		},
		Spec: cv1.UpdateServiceSpec{
			GraphDataImage: "example.com/library/graph-data@sha256:123",
			Releases:       "example.com/library/xyz@sha256:123",
			Resources: cv1.UpdateServiceResources{
				GraphBuilder: cv1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")},
				},
				PolicyEngine: cv1.ResourceRequirements{
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
				},
			},
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

	assertResourceList(t,
		map[corev1.ResourceName]string{corev1.ResourceCPU: "750m", corev1.ResourceMemory: "2Gi"},
		actual.graphBuilderContainer.Resources.Limits)
	assertResourceList(t,
		map[corev1.ResourceName]string{corev1.ResourceCPU: "150m", corev1.ResourceMemory: "64Mi"},
		actual.graphBuilderContainer.Resources.Requests)

	assertResourceList(t,
		map[corev1.ResourceName]string{corev1.ResourceCPU: "750m", corev1.ResourceMemory: "1Gi"},
		actual.policyEngineContainer.Resources.Limits)
	assertResourceList(t,
		map[corev1.ResourceName]string{corev1.ResourceCPU: "500m", corev1.ResourceMemory: "64Mi"},
		actual.policyEngineContainer.Resources.Requests)
}

func Test_validateResources(t *testing.T) {
	for _, tc := range []struct {
		name      string
		resources cv1.UpdateServiceResources
		expected  []string
	}{
		{
			name: "defaults are valid",
		},
		{
			name: "raising graph-builder memory limit and request is valid",
			resources: cv1.UpdateServiceResources{
				GraphBuilder: cv1.ResourceRequirements{
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")},
					Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
				},
			},
		},
		{
			name: "request above the merged default limit is invalid",
			resources: cv1.UpdateServiceResources{
				GraphBuilder: cv1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
				},
			},
			expected: []string{"graph-builder container: request for memory (1Gi) must not exceed the limit (512Mi)"},
		},
		{
			name: "negative policy-engine limit is invalid",
			resources: cv1.UpdateServiceResources{
				PolicyEngine: cv1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("-1Mi")},
				},
			},
			expected: []string{"policy-engine container: limit for memory must not be negative"},
		},
		{
			name: "qualified extended resource is valid",
			resources: cv1.UpdateServiceResources{
				GraphBuilder: cv1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceName("example.com/gpu"): resource.MustParse("1")},
				},
			},
		},
		{
			name: "extended resource must be an integer",
			resources: cv1.UpdateServiceResources{
				GraphBuilder: cv1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceName("example.com/gpu"): resource.MustParse("500m")},
				},
			},
			expected: []string{"graph-builder container: limit for example.com/gpu (500m) must be an integer"},
		},
		{
			name: "hugepage limit is valid",
			resources: cv1.UpdateServiceResources{
				GraphBuilder: cv1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceName("hugepages-2Mi"): resource.MustParse("64Mi")},
				},
			},
		},
		{
			name: "hugepage amount must be a multiple of the page size",
			resources: cv1.UpdateServiceResources{
				GraphBuilder: cv1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceName("hugepages-2Mi"): resource.MustParse("3Mi")},
				},
			},
			expected: []string{"graph-builder container: limit for hugepages-2Mi: 3Mi is not a positive integer multiple of the hugepages-2Mi page size"},
		},
		{
			name: "hugepage page size must not overflow an int64",
			resources: cv1.UpdateServiceResources{
				GraphBuilder: cv1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceName("hugepages-1e100"): resource.MustParse("1")},
				},
			},
			expected: []string{"graph-builder container: limit for hugepages-1e100: resource name has an invalid hugepage page size"},
		},
		{
			name: "hugepage request must equal the limit",
			resources: cv1.UpdateServiceResources{
				GraphBuilder: cv1.ResourceRequirements{
					Limits:   corev1.ResourceList{corev1.ResourceName("hugepages-2Mi"): resource.MustParse("64Mi")},
					Requests: corev1.ResourceList{corev1.ResourceName("hugepages-2Mi"): resource.MustParse("32Mi")},
				},
			},
			expected: []string{"graph-builder container: request for hugepages-2Mi (32Mi) must equal the limit (64Mi) because hugepages-2Mi cannot be overcommitted"},
		},
		{
			name: "misspelled native resource name is invalid",
			resources: cv1.UpdateServiceResources{
				GraphBuilder: cv1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceName("memroy"): resource.MustParse("1Gi")},
				},
			},
			expected: []string{`graph-builder container: limit name "memroy" must be a standard resource for containers`},
		},
		{
			name: "invalid resource name in requests is invalid",
			resources: cv1.UpdateServiceResources{
				PolicyEngine: cv1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceName("memroy"): resource.MustParse("1Gi")},
				},
			},
			expected: []string{`policy-engine container: request name "memroy" must be a standard resource for containers`},
		},
		{
			name: "reserved kubernetes.io domain is invalid",
			resources: cv1.UpdateServiceResources{
				GraphBuilder: cv1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceName("kubernetes.io/foo"): resource.MustParse("1")},
				},
			},
			expected: []string{`graph-builder container: limit name "kubernetes.io/foo" must not use the reserved "kubernetes.io/" namespace`},
		},
		{
			name: "reserved kubernetes.io subdomain is invalid",
			resources: cv1.UpdateServiceResources{
				GraphBuilder: cv1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceName("example.kubernetes.io/gpu"): resource.MustParse("1")},
				},
			},
			expected: []string{`graph-builder container: limit name "example.kubernetes.io/gpu" must not use the reserved "kubernetes.io/" namespace`},
		},
		{
			name: "malformed extended resource name is invalid",
			resources: cv1.UpdateServiceResources{
				GraphBuilder: cv1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceName("Example.com/gpu"): resource.MustParse("1")},
				},
			},
			expected: []string{"must be a valid resource name"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			instance := &cv1.UpdateService{
				Spec: cv1.UpdateServiceSpec{Resources: tc.resources},
			}
			err := validateResources(newOperandResources(instance))
			if len(tc.expected) == 0 {
				assert.NoError(t, err)
				return
			}
			if !assert.Error(t, err) {
				return
			}
			assert.Contains(t, err.Error(), "invalid spec.resources: ")
			for _, expected := range tc.expected {
				assert.Contains(t, err.Error(), expected)
			}
		})
	}
}

func Test_validateContainerResources(t *testing.T) {
	for _, tc := range []struct {
		name         string
		requirements corev1.ResourceRequirements
		expected     []string
	}{
		{
			name: "operand defaults are valid",
			requirements: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("750m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("150m"),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				},
			},
		},
		{
			name: "ephemeral storage can be requested",
			requirements: corev1.ResourceRequirements{
				Limits:   corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse("1Gi")},
				Requests: corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse("500Mi")},
			},
		},
		{
			name: "hugepages require cpu or memory",
			requirements: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{corev1.ResourceName("hugepages-2Mi"): resource.MustParse("64Mi")},
			},
			expected: []string{"hugepages require a cpu or memory request or limit"},
		},
		{
			name: "hugepages require a limit",
			requirements: corev1.ResourceRequirements{
				Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")},
				Requests: corev1.ResourceList{corev1.ResourceName("hugepages-2Mi"): resource.MustParse("64Mi")},
			},
			expected: []string{"a limit for hugepages-2Mi must be set because hugepages-2Mi cannot be overcommitted"},
		},
		{
			name: "hugepage page size must divide the quantity",
			requirements: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory:                resource.MustParse("512Mi"),
					corev1.ResourceName("hugepages-1Gi"): resource.MustParse("1500Mi"),
				},
			},
			expected: []string{"1500Mi is not a positive integer multiple of the hugepages-1Gi page size"},
		},
		{
			name: "hugepage page size must be a whole quantity",
			requirements: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory:                resource.MustParse("512Mi"),
					corev1.ResourceName("hugepages-1.5"): resource.MustParse("3Mi"),
				},
			},
			expected: []string{"resource name has an invalid hugepage page size"},
		},
		{
			name: "hugepage page size must be numeric",
			requirements: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory:                  resource.MustParse("512Mi"),
					corev1.ResourceName("hugepages-giant"): resource.MustParse("3Mi"),
				},
			},
			expected: []string{"resource name has an invalid hugepage page size"},
		},
		{
			name: "hugepage page size must not overflow an int64",
			requirements: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory:                  resource.MustParse("512Mi"),
					corev1.ResourceName("hugepages-1e100"): resource.MustParse("1"),
				},
			},
			expected: []string{"resource name has an invalid hugepage page size"},
		},
		{
			name: "extended resource requires a limit",
			requirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceName("example.com/gpu"): resource.MustParse("1")},
			},
			expected: []string{"a limit for example.com/gpu must be set because example.com/gpu cannot be overcommitted"},
		},
		{
			name: "extended resource request must equal the limit",
			requirements: corev1.ResourceRequirements{
				Limits:   corev1.ResourceList{corev1.ResourceName("example.com/gpu"): resource.MustParse("2")},
				Requests: corev1.ResourceList{corev1.ResourceName("example.com/gpu"): resource.MustParse("1")},
			},
			expected: []string{"request for example.com/gpu (1) must equal the limit (2)"},
		},
		{
			name: "storage is not a container resource",
			requirements: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{corev1.ResourceName("storage"): resource.MustParse("1Gi")},
			},
			expected: []string{`limit name "storage" must be a standard resource for containers`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errReasons := validateContainerResources("operand", tc.requirements)
			if len(tc.expected) == 0 {
				assert.Empty(t, errReasons)
				return
			}
			joined := strings.Join(errReasons, "; ")
			for _, expected := range tc.expected {
				assert.Contains(t, joined, expected)
			}
		})
	}
}
func assertResourceList(t *testing.T, expected map[corev1.ResourceName]string, actual corev1.ResourceList) {
	t.Helper()
	assert.Len(t, actual, len(expected))
	for name, quantity := range expected {
		actualQuantity, ok := actual[name]
		if !assert.True(t, ok, "missing resource %s", name) {
			continue
		}
		assert.True(t, resource.MustParse(quantity).Equal(actualQuantity),
			"%s: expected %s, got %s", name, quantity, actualQuantity.String())
	}
}
