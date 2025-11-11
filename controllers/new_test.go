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
	"sigs.k8s.io/yaml"

	cv1 "github.com/openshift/cincinnati-operator/api/v1"
)

func Test_newKubeResources(t *testing.T) {
	sample := &cv1.UpdateService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample",
			Namespace: "sample-ns",
		},
		Spec: cv1.UpdateServiceSpec{
			Releases: "docker.io/library/xyz@sha256:123",
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
		manifestFile := filepath.Join("testdata", fmt.Sprintf("zz_fixture_%s_%s.yaml", t.Name(), k))
		data, err := yaml.Marshal(v)
		assert.Nil(t, err)

		if strings.ToLower(os.Getenv("UPDATE")) == "true" {
			if err := os.WriteFile(manifestFile, data, 0644); err != nil {
				assert.Nil(t, actualErr)
			}
		}

		expected, err := os.ReadFile(manifestFile)
		assert.Nil(t, err)
		assert.Equal(t, string(expected), string(data))
	}
}
