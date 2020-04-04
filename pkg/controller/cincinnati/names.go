package cincinnati

import (
	cv1alpha1 "github.com/openshift/cincinnati-operator/pkg/apis/cincinnati/v1alpha1"
)

const (
	// NameContainerGraphBuilder is the Name property of the graph builder container
	NameContainerGraphBuilder string = "graph-builder"
	// NameContainerPolicyEngine is the Name property of the policy engine container
	NameContainerPolicyEngine string = "policy-engine"
)

func nameDeployment(instance *cv1alpha1.Cincinnati) string {
	return instance.Name
}

func namePDB(instance *cv1alpha1.Cincinnati) string {
	return instance.Name
}

func nameEnvConfig(instance *cv1alpha1.Cincinnati) string {
	return instance.Name + "-env"
}

func nameConfig(instance *cv1alpha1.Cincinnati) string {
	return instance.Name + "-config"
}

func namePEService(instance *cv1alpha1.Cincinnati) string {
	return instance.Name + "-policy-engine"
}

func nameGBService(instance *cv1alpha1.Cincinnati) string {
	return instance.Name + "-graph-builder"
}
