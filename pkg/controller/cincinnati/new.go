package cincinnati

import (
	"strings"
	"text/template"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	cv1alpha1 "github.com/openshift/cincinnati-operator/pkg/apis/cincinnati/v1alpha1"
)

const graphBuilderTOML string = `verbosity = "vvv"

[service]
pause_secs = 300
address = "0.0.0.0"
port = 8080

[status]
address = "0.0.0.0"
port = 9080

[[plugin_settings]]
name = "release-scrape-dockerv2"
registry = "{{.Registry}}"
repository = "{{.Repository}}"
fetch_concurrency = 16

[[plugin_settings]]
name = "github-secondary-metadata-scrape"
github_org = "{{.GitHubOrg}}"
github_repo = "{{.GitHubRepo}}"
reference_branch = "{{.Branch}}"
output_directory = "/tmp/cincinnati/graph-data"

[[plugin_settings]]
name = "openshift-secondary-metadata-parse"
data_directory = "/tmp/cincinnati/graph-data"

[[plugin_settings]]
name = "edge-add-remove"`

func newPodDisruptionBudget(instance *cv1alpha1.Cincinnati) *policyv1beta1.PodDisruptionBudget {
	// When running a single replica, allow 0 available so we don't block node
	// drains. Otherwise require 1.
	minAvailable := intstr.FromInt(0)
	if instance.Spec.Replicas >= 2 {
		minAvailable = intstr.FromInt(1)
	}
	return &policyv1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namePodDisruptionBudget(instance),
			Namespace: instance.Namespace,
		},
		Spec: policyv1beta1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": nameDeployment(instance),
				},
			},
		},
	}
}

func newGraphBuilderService(instance *cv1alpha1.Cincinnati) *corev1.Service {
	name := nameGraphBuilderService(instance)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app": name,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				corev1.ServicePort{
					Name:       "graph-builder",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
				corev1.ServicePort{
					Name:       "status-gb",
					Port:       9080,
					TargetPort: intstr.FromInt(9080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				"deployment": nameDeployment(instance),
			},
			SessionAffinity: corev1.ServiceAffinityNone,
		},
	}
}

func newPolicyEngineService(instance *cv1alpha1.Cincinnati) *corev1.Service {
	name := namePolicyEngineService(instance)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app": name,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				corev1.ServicePort{
					Name:       "policy-engine",
					Port:       80,
					TargetPort: intstr.FromInt(8081),
					Protocol:   corev1.ProtocolTCP,
				},
				corev1.ServicePort{
					Name:       "status-pe",
					Port:       9081,
					TargetPort: intstr.FromInt(9081),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				"deployment": nameDeployment(instance),
			},
			SessionAffinity: corev1.ServiceAffinityNone,
		},
	}
}

func newEnvConfig(instance *cv1alpha1.Cincinnati) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nameEnvConfig(instance),
			Namespace: instance.Namespace,
		},
		Data: map[string]string{
			"gb.rust_backtrace":              "0",
			"pe.address":                     "0.0.0.0",
			"pe.log.verbosity":               "vv",
			"pe.mandatory_client_parameters": "channel",
			"pe.rust_backtrace":              "0",
			"pe.status.address":              "0.0.0.0",
			"pe.upstream":                    "http://localhost:8080/v1/graph",
		},
	}
}

func newGraphBuilderConfig(instance *cv1alpha1.Cincinnati) (*corev1.ConfigMap, error) {
	tmpl, err := template.New("gb").Parse(graphBuilderTOML)
	if err != nil {
		return nil, err
	}
	builder := strings.Builder{}
	if err = tmpl.Execute(&builder, instance.Spec); err != nil {
		return nil, err
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nameConfig(instance),
			Namespace: instance.Namespace,
		},
		Data: map[string]string{
			"gb.toml": builder.String(),
		},
	}, nil
}

func newDeployment(instance *cv1alpha1.Cincinnati, image string) *appsv1.Deployment {
	name := nameDeployment(instance)
	maxUnavailable := intstr.FromString("50%")
	maxSurge := intstr.FromString("100%")
	mode := int32(420) // 0644
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &instance.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: &maxUnavailable,
					MaxSurge:       &maxSurge,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":        name,
						"deployment": name,
					},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						corev1.Volume{
							Name: "configs",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									DefaultMode: &mode,
									LocalObjectReference: corev1.LocalObjectReference{
										Name: nameConfig(instance),
									},
								},
							},
						},
					},
					Containers: []corev1.Container{
						newGraphBuilderContainer(instance, image),
						newPolicyEngineContainer(instance, image),
					},
				},
			},
		},
	}
}

func newGraphBuilderContainer(instance *cv1alpha1.Cincinnati, image string) corev1.Container {
	return corev1.Container{
		Name:            NameContainerGraphBuilder,
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{
			"/usr/bin/graph-builder",
		},
		Args: []string{
			"-c",
			"/etc/configs/gb.toml",
		},
		Ports: []corev1.ContainerPort{
			corev1.ContainerPort{
				Name:          "graph-builder",
				ContainerPort: 8080,
				Protocol:      corev1.ProtocolTCP,
			},
			corev1.ContainerPort{
				Name:          "status-gb",
				ContainerPort: 9080,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env: []corev1.EnvVar{
			newCMEnvVar("RUST_BACKTRACE", "gb.rust_backtrace", nameEnvConfig(instance)),
		},
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewMilliQuantity(750, resource.DecimalSI),
				corev1.ResourceMemory: *resource.NewQuantity(512*1024*1024, resource.BinarySI),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewMilliQuantity(150, resource.DecimalSI),
				corev1.ResourceMemory: *resource.NewQuantity(64*1024*1024, resource.BinarySI),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			corev1.VolumeMount{
				Name:      "configs",
				ReadOnly:  true,
				MountPath: "/etc/configs",
			},
		},
		LivenessProbe: &corev1.Probe{
			FailureThreshold:    3,
			SuccessThreshold:    1,
			InitialDelaySeconds: 3,
			PeriodSeconds:       10,
			TimeoutSeconds:      3,
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/liveness",
					Port:   intstr.FromInt(9080),
					Scheme: corev1.URISchemeHTTP,
				},
			},
		},
		ReadinessProbe: &corev1.Probe{
			FailureThreshold:    3,
			SuccessThreshold:    1,
			InitialDelaySeconds: 3,
			PeriodSeconds:       10,
			TimeoutSeconds:      3,
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/readiness",
					Port:   intstr.FromInt(9080),
					Scheme: corev1.URISchemeHTTP,
				},
			},
		},
	}
}

func newPolicyEngineContainer(instance *cv1alpha1.Cincinnati, image string) corev1.Container {
	envConfigName := nameEnvConfig(instance)
	return corev1.Container{
		Name:            NameContainerPolicyEngine,
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{
			"/usr/bin/policy-engine",
		},
		Args: []string{
			"-$(PE_LOG_VERBOSITY)",
			"--service.address",
			"$(ADDRESS)",
			"--service.mandatory_client_parameters",
			"$(PE_MANDATORY_CLIENT_PARAMETERS)",
			"--service.path_prefix",
			"/api/upgrades_info",
			"--service.port",
			"8081",
			"--status.address",
			"$(PE_STATUS_ADDRESS)",
			"--status.port",
			"9081",
			"--upstream.cincinnati.url",
			"$(UPSTREAM)",
		},
		Ports: []corev1.ContainerPort{
			corev1.ContainerPort{
				Name:          "policy-engine",
				ContainerPort: 8081,
				Protocol:      corev1.ProtocolTCP,
			},
			corev1.ContainerPort{
				Name:          "status-pe",
				ContainerPort: 9081,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env: []corev1.EnvVar{
			newCMEnvVar("ADDRESS", "pe.address", envConfigName),
			newCMEnvVar("PE_STATUS_ADDRESS", "pe.status.address", envConfigName),
			newCMEnvVar("UPSTREAM", "pe.upstream", envConfigName),
			newCMEnvVar("PE_LOG_VERBOSITY", "pe.log.verbosity", envConfigName),
			newCMEnvVar("PE_MANDATORY_CLIENT_PARAMETERS", "pe.mandatory_client_parameters", envConfigName),
			newCMEnvVar("RUST_BACKTRACE", "pe.rust_backtrace", envConfigName),
		},
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewMilliQuantity(750, resource.DecimalSI),
				corev1.ResourceMemory: *resource.NewQuantity(512*1024*1024, resource.BinarySI),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewMilliQuantity(150, resource.DecimalSI),
				corev1.ResourceMemory: *resource.NewQuantity(64*1024*1024, resource.BinarySI),
			},
		},
		LivenessProbe: &corev1.Probe{
			FailureThreshold:    3,
			SuccessThreshold:    1,
			InitialDelaySeconds: 3,
			PeriodSeconds:       10,
			TimeoutSeconds:      3,
			Handler: corev1.Handler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(8081),
				},
			},
		},
	}
}

func newCMEnvVar(name, key, cmName string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
				Key: key,
				LocalObjectReference: corev1.LocalObjectReference{
					Name: cmName,
				},
			},
		},
	}
}
