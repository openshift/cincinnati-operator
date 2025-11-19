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
