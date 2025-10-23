package controllers

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/template"

	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	cv1 "github.com/openshift/cincinnati-operator/api/v1"
)

const (
	// GraphBuilderConfigHashAnnotation is the key for an annotation storing a
	// hash of the graph builder config on the operand Pod. Storing the
	// annotation ensures that the Pod will be replaced whenever the content of
	// the ConfigMap changes.
	GraphBuilderConfigHashAnnotation string = "updateservice.operator.openshift.io/graph-builder-config-hash"

	// EnvConfigHashAnnotation is the key for an annotation storing a hash of
	// the env config on the operand Pod. Storing the annotation ensures that
	// the Pod will be replaced whenever the content of the ConfigMap changes.
	EnvConfigHashAnnotation string = "updateservice.operator.openshift.io/env-config-hash"

	// DescriptionAnnotation is the key for an annotation used for describing specific behaviour of given object.
	//  https://kubernetes.io/docs/reference/labels-annotations-taints/#description
	DescriptionAnnotation = "kubernetes.io/description"
)

type graphBuilderProperties struct {
	Registry   string
	Repository string
}

const graphBuilderTOML string = `verbosity = "vvv"

[service]
pause_secs = 300
address = "::"
port = 8080

[status]
address = "::"
port = 9080

[[plugin_settings]]
name = "release-scrape-dockerv2"
registry = "{{.Registry}}"
repository = "{{.Repository}}"
fetch_concurrency = 16
credentials_path = "/var/lib/cincinnati/registry-credentials/.dockerconfigjson"

[[plugin_settings]]
name = "openshift-secondary-metadata-parse"
data_directory = "/var/lib/cincinnati/graph-data"

[[plugin_settings]]
name = "edge-add-remove"`

// kubeResources holds a reference to all of the kube resources we need during
// reconciliation. This object enables us to create all of the resources
// up-front at the beginning of the Reconcile function, and then have one place
// to reference each of the resources when needed. This is especially helpful
// because creation of at least one of the resources can return an error, since
// it renders a Template. Creating the resources and handling the error just
// once up-front makes it MUCH easier to access those resources as-needed
// throughout the reconciliation code.
type kubeResources struct {
	envConfig                *corev1.ConfigMap
	envConfigHash            string
	graphBuilderConfig       *corev1.ConfigMap
	graphBuilderConfigHash   string
	podDisruptionBudget      *policyv1.PodDisruptionBudget
	deployment               *appsv1.Deployment
	graphBuilderContainer    *corev1.Container
	graphDataInitContainer   *corev1.Container
	policyEngineContainer    *corev1.Container
	graphBuilderService      *corev1.Service
	policyEngineService      *corev1.Service
	policyEngineRoute        *routev1.Route
	policyEngineOldRoute     *routev1.Route
	networkPolicy            *networkingv1.NetworkPolicy
	trustedCAConfig          *corev1.ConfigMap
	trustedClusterCAConfig   *corev1.ConfigMap
	pullSecret               *corev1.Secret
	volumes                  []corev1.Volume
	graphBuilderVolumeMounts []corev1.VolumeMount
}

func newKubeResources(instance *cv1.UpdateService, image string, pullSecret *corev1.Secret, caConfigMap *corev1.ConfigMap, clusterCA *corev1.ConfigMap) (*kubeResources, error) {
	k := kubeResources{}

	gbConfig, err := k.newGraphBuilderConfig(instance)
	if err != nil {
		return nil, err
	}

	// order matters in some cases. For example, the Deployment needs the
	// Containers to already exist.
	k.graphBuilderConfig = gbConfig
	graphBuilderConfigHash, err := checksumMap(k.graphBuilderConfig.Data)
	if err != nil {
		return nil, err
	}
	k.graphBuilderConfigHash = graphBuilderConfigHash
	k.envConfig = k.newEnvConfig(instance)
	envConfigHash, err := checksumMap(k.envConfig.Data)
	if err != nil {
		return nil, err
	}
	k.trustedCAConfig = k.newTrustedCAConfig(instance, caConfigMap)
	k.trustedClusterCAConfig = newTrustedClusterCAConfig(instance, clusterCA)
	k.pullSecret = k.newPullSecret(instance, pullSecret)
	k.envConfigHash = envConfigHash
	k.podDisruptionBudget = k.newPodDisruptionBudget(instance)
	k.volumes = k.newVolumes(instance)
	k.graphBuilderVolumeMounts = k.newGraphBuilderVolumeMounts(instance)
	k.graphBuilderContainer = k.newGraphBuilderContainer(instance, image)
	k.graphDataInitContainer = k.newGraphDataInitContainer(instance)
	k.policyEngineContainer = k.newPolicyEngineContainer(instance, image)
	k.deployment = k.newDeployment(instance)
	k.graphBuilderService = k.newGraphBuilderService(instance)
	k.policyEngineService = k.newPolicyEngineService(instance)
	k.policyEngineRoute = k.newPolicyEngineRoute(instance)
	k.policyEngineOldRoute = k.oldPolicyEngineRoute(instance)
	k.networkPolicy = k.newNetworkPolicy(instance)
	return &k, nil
}

func (k *kubeResources) newPodDisruptionBudget(instance *cv1.UpdateService) *policyv1.PodDisruptionBudget {
	minAvailable := getMinAvailablePBD(instance)
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namePodDisruptionBudget(instance),
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				DescriptionAnnotation: "This PodDisruptionBudget blocks graceful evictions " +
					"(but cannot guard against all external disruption) " +
					"to try and keep at least one Pod running at all times, if the Update Service instance " +
					"specifies two or more replicas.",
			},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": nameDeployment(instance),
				},
			},
		},
	}
}

func (k *kubeResources) newGraphBuilderService(instance *cv1.UpdateService) *corev1.Service {
	name := nameGraphBuilderService(instance)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				DescriptionAnnotation: "This Service exposes a client-agnostic update graph to other clients within the cluster. " +
					"This allows convenient in-cluster access to those graphs, and also allows platform monitoring to " +
					"scrape graph-builder containers for Prometheus metrics. " +
					"See https://github.com/openshift/cincinnati/blob/master/docs/design/cincinnati.md#graph-builder for more details",
			},
			Labels: map[string]string{
				"app": name,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "graph-builder",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
				{
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

func (k *kubeResources) newPolicyEngineService(instance *cv1.UpdateService) *corev1.Service {
	name := namePolicyEngineService(instance)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				DescriptionAnnotation: "It exposes views of the update graph by applying a set of filters " +
					"which are defined within the particular Policy Engine instance. " +
					"See https://github.com/openshift/cincinnati/blob/master/docs/design/cincinnati.md#policy-engine for more details",
			},
			Labels: map[string]string{
				"app": name,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "policy-engine",
					Port:       80,
					TargetPort: intstr.FromInt(8081),
					Protocol:   corev1.ProtocolTCP,
				},
				{
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

func (k *kubeResources) newPolicyEngineRoute(instance *cv1.UpdateService) *routev1.Route {
	name := namePolicyEngineRoute(instance)
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				DescriptionAnnotation: "It exposes views of the update graph by applying a set of filters " +
					"which are defined within the particular Policy Engine instance. " +
					"See https://github.com/openshift/cincinnati/blob/master/docs/design/cincinnati.md#policy-engine for more details",
			},
			Labels: map[string]string{
				"app": nameDeployment(instance),
			},
		},
		Spec: routev1.RouteSpec{
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("policy-engine"),
			},
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: namePolicyEngineService(instance),
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationEdge,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyNone,
			},
		},
	}
}

func (k *kubeResources) oldPolicyEngineRoute(instance *cv1.UpdateService) *routev1.Route {
	name := oldPolicyEngineRouteName(instance)
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				DescriptionAnnotation: "It exposes views of the update graph by applying a set of filters " +
					"which are defined within the particular Policy Engine instance. " +
					"See https://github.com/openshift/cincinnati/blob/master/docs/design/cincinnati.md#policy-engine for more details",
			},
			Labels: map[string]string{
				"app": nameDeployment(instance),
			},
		},
		Spec: routev1.RouteSpec{
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("policy-engine"),
			},
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: namePolicyEngineService(instance),
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationEdge,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyNone,
			},
		},
	}
}

func (k *kubeResources) newNetworkPolicy(instance *cv1.UpdateService) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				DescriptionAnnotation: "This NetworkPolicy allows all egress, to support graph-builder scraping and DNS. " +
					"It allows ingress from the router, to support serving policy-engine responses. " +
					"All other ingress is blocked, including, for now, metrics scraping.",
			},
			Labels: map[string]string{
				"app": instance.Name,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": nameDeployment(instance),
				},
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				// Traffic from the router to the policy-engine service
				From: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"policy-group.network.openshift.io/ingress": "",
						},
					},
				}},
				Ports: []networkingv1.NetworkPolicyPort{{
					Protocol: corev1ProtocolPtr(corev1.ProtocolTCP),
					Port:     intOrStringPtr(intstr.FromString("policy-engine")),
				}},
			}},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				// TCP access to all ports, for registry access, possibly via proxies
				Ports: []networkingv1.NetworkPolicyPort{{
					Protocol: corev1ProtocolPtr(corev1.ProtocolTCP),
				}},
			}, {
				// DNS access to the cluster's openshift-dns DaemonSet.
				To: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": "openshift-dns",
						},
					},
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"dns.operator.openshift.io/daemonset-dns": "default",
						},
					},
				}},
				Ports: []networkingv1.NetworkPolicyPort{{
					Protocol: corev1ProtocolPtr(corev1.ProtocolTCP),
					Port:     intOrStringPtr(intstr.FromInt32(5353)),
				}, {
					Protocol: corev1ProtocolPtr(corev1.ProtocolUDP),
					Port:     intOrStringPtr(intstr.FromInt32(5353)),
				}},
			}},
		},
	}
}

func corev1ProtocolPtr(proto corev1.Protocol) *corev1.Protocol { return &proto }

func intOrStringPtr(intOrString intstr.IntOrString) *intstr.IntOrString { return &intOrString }

func (k *kubeResources) newEnvConfig(instance *cv1.UpdateService) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nameEnvConfig(instance),
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				DescriptionAnnotation: "This ConfigMap contains the environment information shared by the containers of UpdateService",
			},
		},
		Data: map[string]string{
			"gb.rust_backtrace":              "0",
			"pe.address":                     "::",
			"pe.log.verbosity":               "vv",
			"pe.mandatory_client_parameters": "channel",
			"pe.rust_backtrace":              "0",
			"pe.status.address":              "::",
			"pe.upstream":                    "http://localhost:8080/v1/graph",
			"m.rust_backtrace":               "0",
		},
	}
}

func (k *kubeResources) newGraphBuilderConfig(instance *cv1.UpdateService) (*corev1.ConfigMap, error) {
	var registry, repository string
	if segments := strings.SplitN(instance.Spec.Releases, "/", 2); len(segments) != 2 {
		return nil, fmt.Errorf("failed to split %q into registry and repository components", instance.Spec.Releases)
	} else {
		registry = segments[0]
		repository = segments[1]
	}

	tmpl, err := template.New("gb").Parse(graphBuilderTOML)
	if err != nil {
		return nil, err
	}
	builder := strings.Builder{}
	if err = tmpl.Execute(&builder, &graphBuilderProperties{
		Registry:   registry,
		Repository: repository,
	}); err != nil {
		return nil, err
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nameConfig(instance),
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				DescriptionAnnotation: "This ConfigMap contains the configuration file for the graph-builder",
			},
		},
		Data: map[string]string{
			"gb.toml": builder.String(),
		},
	}, nil
}

func (k *kubeResources) newDeployment(instance *cv1.UpdateService) *appsv1.Deployment {
	name := nameDeployment(instance)
	maxUnavailable := intstr.FromString("50%")
	maxSurge := intstr.FromString("100%")
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				DescriptionAnnotation: fmt.Sprintf("This deployment launches the components for the OpenShift UpdateService %s", instance.Name),
			},
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
					Annotations: map[string]string{
						GraphBuilderConfigHashAnnotation: k.graphBuilderConfigHash,
						EnvConfigHashAnnotation:          k.envConfigHash,
					},
				},
				Spec: corev1.PodSpec{
					Volumes: k.volumes,
					Containers: []corev1.Container{
						*k.graphBuilderContainer,
						*k.policyEngineContainer,
					},
				},
			},
		},
	}
	if k.graphDataInitContainer != nil {
		dep.Spec.Template.Spec.InitContainers = []corev1.Container{
			*k.graphDataInitContainer,
		}
	}
	return dep
}

func (k *kubeResources) newVolumes(instance *cv1.UpdateService) []corev1.Volume {
	mode := int32(420) // 0644
	v := []corev1.Volume{
		{
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
		{
			Name: "cincinnati-graph-data",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: namePullSecret,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  namePullSecretCopy(instance),
					DefaultMode: &mode,
				},
			},
		},
	}

	if k.trustedCAConfig != nil {
		v = append(v, corev1.Volume{
			Name: NameTrustedCAVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					DefaultMode: &mode,
					LocalObjectReference: corev1.LocalObjectReference{
						Name: nameAdditionalTrustedCA(instance),
					},
					Items: []corev1.KeyToPath{
						{
							Path: "tls-ca-bundle.pem",
							Key:  NameCertConfigMapKey,
						},
					},
				},
			},
		})
	}

	if k.trustedClusterCAConfig != nil {
		v = append(v, corev1.Volume{
			Name: NameClusterTrustedCAVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					DefaultMode: &mode,
					LocalObjectReference: corev1.LocalObjectReference{
						Name: NameClusterTrustedCAVolume,
					},
				},
			},
		})
	}
	return v

}

func (k *kubeResources) newPullSecret(instance *cv1.UpdateService, s *corev1.Secret) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namePullSecretCopy(instance),
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				DescriptionAnnotation: "It contains the pull credentials from the global pull secret for the cluster",
			},
		},
		Data: s.Data,
	}
}

func (k *kubeResources) newTrustedCAConfig(instance *cv1.UpdateService, cm *corev1.ConfigMap) *corev1.ConfigMap {
	// Found ConfigMap referenced by ImageConfig.Spec.AdditionalTrustedCA.Name
	// but did not find key 'updateservice-registry' for registry CA cert in ConfigMap
	if cm == nil {
		return nil
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nameAdditionalTrustedCA(instance),
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				DescriptionAnnotation: "This ConfigMap contains additional certificate authorities to be trusted during image registry access.",
			},
		},
		Data: cm.Data,
	}
}

func newTrustedClusterCAConfig(instance *cv1.UpdateService, clusterCA *corev1.ConfigMap) *corev1.ConfigMap {

	// check if the proxy variables are set by olm
	httpProxy := os.Getenv("HTTP_PROXY")
	httpsProxy := os.Getenv("HTTPS_PROXY")
	noProxy := os.Getenv("NO_PROXY")

	if httpProxy == "" && httpsProxy == "" && noProxy == "" {
		// cluster wide proxy is not set, so dont create configmap
		return nil
	}

	if clusterCA != nil {
		return clusterCA
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        NameClusterTrustedCAVolume,
			Namespace:   instance.Namespace,
			Labels:      map[string]string{"config.openshift.io/inject-trusted-cabundle": "true"},
			Annotations: map[string]string{"release.openshift.io/create-only": "true"},
		},
	}
}

func (k *kubeResources) newGraphDataInitContainer(instance *cv1.UpdateService) *corev1.Container {
	return &corev1.Container{
		Name:            NameInitContainerGraphData,
		Image:           instance.Spec.GraphDataImage,
		ImagePullPolicy: corev1.PullAlways,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "cincinnati-graph-data",
				MountPath: "/var/lib/cincinnati/graph-data",
			},
		},
	}
}

func (k *kubeResources) newGraphBuilderContainer(instance *cv1.UpdateService, image string) *corev1.Container {

	gbENV := []corev1.EnvVar{
		newCMEnvVar("RUST_BACKTRACE", "gb.rust_backtrace", nameEnvConfig(instance)),
	}

	// get the cluster proxy variables, and append to ENV var if set
	if httpProxy := os.Getenv("HTTP_PROXY"); httpProxy != "" {
		gbENV = append(gbENV,
			corev1.EnvVar{
				Name:  "HTTP_PROXY",
				Value: httpProxy,
			},
		)
	}
	if httpsProxy := os.Getenv("HTTPS_PROXY"); httpsProxy != "" {
		gbENV = append(gbENV,
			corev1.EnvVar{
				Name:  "HTTPS_PROXY",
				Value: httpsProxy,
			},
		)
	}
	if noProxy := os.Getenv("NO_PROXY"); noProxy != "" {
		gbENV = append(gbENV,
			corev1.EnvVar{
				Name:  "NO_PROXY",
				Value: noProxy,
			},
		)
	}

	g := &corev1.Container{
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
			{
				Name:          "graph-builder",
				ContainerPort: 8080,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "status-gb",
				ContainerPort: 9080,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env: gbENV,
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
		VolumeMounts: k.graphBuilderVolumeMounts,
		LivenessProbe: &corev1.Probe{
			FailureThreshold:    3,
			SuccessThreshold:    1,
			InitialDelaySeconds: 3,
			PeriodSeconds:       10,
			TimeoutSeconds:      3,
			ProbeHandler: corev1.ProbeHandler{
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
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/readiness",
					Port:   intstr.FromInt(9080),
					Scheme: corev1.URISchemeHTTP,
				},
			},
		},
	}
	return g
}

func (k *kubeResources) newGraphBuilderVolumeMounts(instance *cv1.UpdateService) []corev1.VolumeMount {
	vm := []corev1.VolumeMount{
		{
			Name:      "configs",
			ReadOnly:  true,
			MountPath: "/etc/configs",
		},
		{
			Name:      "cincinnati-graph-data",
			MountPath: "/var/lib/cincinnati/graph-data",
		},
		{
			Name:      namePullSecret,
			ReadOnly:  true,
			MountPath: "/var/lib/cincinnati/registry-credentials",
		},
	}

	if k.trustedCAConfig != nil {
		vm = append(vm, corev1.VolumeMount{
			Name:      NameTrustedCAVolume,
			ReadOnly:  true,
			MountPath: "/etc/pki/ca-trust/extracted/pem",
		})
	}
	if k.trustedClusterCAConfig != nil {
		vm = append(vm, corev1.VolumeMount{
			Name:      NameClusterTrustedCAVolume,
			ReadOnly:  true,
			MountPath: ClusterCAMountDir,
		})
	}

	return vm
}

func (k *kubeResources) newPolicyEngineContainer(instance *cv1.UpdateService, image string) *corev1.Container {
	envConfigName := nameEnvConfig(instance)
	return &corev1.Container{
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
			{
				Name:          "policy-engine",
				ContainerPort: 8081,
				Protocol:      corev1.ProtocolTCP,
			},
			{
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
			InitialDelaySeconds: 120,
			PeriodSeconds:       30,
			TimeoutSeconds:      3,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/livez",
					Port:   intstr.FromInt(9081),
					Scheme: corev1.URISchemeHTTP,
				},
			},
		},
		ReadinessProbe: &corev1.Probe{
			FailureThreshold:    3,
			SuccessThreshold:    1,
			InitialDelaySeconds: 120,
			PeriodSeconds:       30,
			TimeoutSeconds:      3,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/readyz",
					Port:   intstr.FromInt(9081),
					Scheme: corev1.URISchemeHTTP,
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

// checksumMap produces a checksum of a ConfigMap's Data attribute. The checksum
// can be used to detect when the contents of a ConfigMap have changed.
func checksumMap(m map[string]string) (string, error) {
	keys := sort.StringSlice([]string{})
	for k := range m {
		keys = append(keys, k)
	}
	keys.Sort()

	hash := sha256.New()
	encoder := base64.NewEncoder(base64.StdEncoding, hash)

	for _, k := range keys {
		for _, data := range [][]byte{
			[]byte(k),
			[]byte(m[k]),
		} {
			// We base64 encode the data to limit the character set and then use
			// ":" as a separator.
			_, err := encoder.Write(data)
			if err != nil {
				return "", err
			}
			_, err = hash.Write([]byte(":"))
			if err != nil {
				return "", err
			}
		}
	}
	encoder.Close()

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
