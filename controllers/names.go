package controllers

import (
	cv1beta1 "github.com/openshift/cincinnati-operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// NameContainerGraphBuilder is the Name property of the graph builder container
	NameContainerGraphBuilder string = "graph-builder"
	// NameContainerPolicyEngine is the Name property of the policy engine container
	NameContainerPolicyEngine string = "policy-engine"
	// NameInitContainerGraphData is the Name property of the graph data container
	NameInitContainerGraphData string = "graph-data"
	// openshiftConfigNamespace is the name of openshift's configuration namespace
	openshiftConfigNamespace = "openshift-config"
	// NameTrustedCAVolume is the name of the Volume used in Cincinnati's deployment containing the CA Cert
	NameTrustedCAVolume = "trusted-ca"
	// NameCertConfigMapKey is the ConfigMap key name where the operator expects the external registry CA Cert
	NameCertConfigMapKey = "cincinnati-registry"
	// namePullSecret is the OpenShift pull secret name
	namePullSecret = "pull-secret"
)

func nameDeployment(instance *cv1beta1.Cincinnati) string {
	return instance.Name
}

func namePodDisruptionBudget(instance *cv1beta1.Cincinnati) string {
	return instance.Name
}

func nameEnvConfig(instance *cv1beta1.Cincinnati) string {
	return instance.Name + "-env"
}

func nameConfig(instance *cv1beta1.Cincinnati) string {
	return instance.Name + "-config"
}

func namePolicyEngineService(instance *cv1beta1.Cincinnati) string {
	return instance.Name + "-policy-engine"
}

func nameGraphBuilderService(instance *cv1beta1.Cincinnati) string {
	return instance.Name + "-graph-builder"
}

func namePolicyEngineRoute(instance *cv1beta1.Cincinnati) string {
	return namePolicyEngineService(instance) + "-route"
}

func nameAdditionalTrustedCA(instance *cv1beta1.Cincinnati) string {
	return instance.Name + "-trusted-ca"
}

func namePullSecretCopy(instance *cv1beta1.Cincinnati) string {
	return instance.Name + "-" + namePullSecret
}

// When running a single replica, allow 0 available so we don't block node
// drains. Otherwise require 1.
func getMinAvailablePBD(instance *cv1beta1.Cincinnati) intstr.IntOrString {
	minAvailable := intstr.FromInt(0)
	if instance.Spec.Replicas >= 2 {
		minAvailable = intstr.FromInt(1)
	}
	return minAvailable
}
