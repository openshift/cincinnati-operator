package controllers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	cv1 "github.com/openshift/cincinnati-operator/api/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/stretchr/testify/assert"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	testName                    = "foo"
	testNamespace               = "bar"
	testUpdateServiceKind       = "testKind"
	testUpdateServiceAPIVersion = "testAPIVersion"
	testOperandImage            = "testOperandImage"
	testReplicas                = 1
	testReleases                = "testRegistry/testRepository"
	testGraphDataImage          = "testGraphDataImage"
	testConfigMap               = "testConfigMap"
)

var _ reconcile.Reconciler = &UpdateServiceReconciler{}

func TestMain(m *testing.M) {
	if err := cv1.AddToScheme(scheme.Scheme); err != nil {
		log.Error(err, "Failed adding apis to scheme")
		os.Exit(1)
	}
	if err := configv1.AddToScheme(scheme.Scheme); err != nil {
		log.Error(err, "Failed adding apis to scheme")
		os.Exit(1)
	}

	if err := routev1.AddToScheme(scheme.Scheme); err != nil {
		log.Error(err, "Failed adding apis to scheme")
		os.Exit(1)
	}

	exitVal := m.Run()
	os.Exit(exitVal)
}

func TestReconcile(t *testing.T) {
	tests := []struct {
		name               string
		existingObjs       []runtime.Object
		expectedConditions []conditionsv1.Condition
	}{
		{
			name: "Reconcile",
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
				newSecret(),
			},
			expectedConditions: []conditionsv1.Condition{
				{
					Type:   cv1.ConditionReconcileCompleted,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   cv1.ConditionRegistryCACertFound,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := newTestReconciler(test.existingObjs...)
			request := newRequest(newDefaultUpdateService())

			ctx := context.TODO()
			_, err := r.Reconcile(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			instance := &cv1.UpdateService{}
			err = r.Client.Get(context.TODO(), request.NamespacedName, instance)
			if err != nil {
				t.Fatal(err)
			}
			verifyConditions(t, test.expectedConditions, instance)
		})
	}
}

func TestEnsureConfig(t *testing.T) {
	pullSecret := newSecret()
	updateservice := newDefaultUpdateService()
	r := newTestReconciler(updateservice)

	resources, err := newKubeResources(updateservice, testOperandImage, pullSecret, nil, nil)
	err = r.ensureConfig(context.TODO(), log, updateservice, resources)
	if err != nil {
		t.Fatal(err)
	}
	found := &corev1.ConfigMap{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameConfig(updateservice), Namespace: updateservice.Namespace}, found)
	if err != nil {
		t.Fatal(err)
	}
	verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateservice)
	verifyAnnotation(t, found.ObjectMeta.Annotations, "the configuration file for the graph-builder")
	// TODO: check that the gb.toml is corrently rendered with updateservice.Spec
	assert.NotEmpty(t, found.Data["gb.toml"])
}

func verifyAnnotation(t *testing.T, annotations map[string]string, keywords ...string) {
	assert.Contains(t, annotations, DescriptionAnnotation)
	for _, keyword := range keywords {
		assert.Contains(t, annotations[DescriptionAnnotation], keyword)
	}
}

func TestEnsureEnvConfig(t *testing.T) {
	pullSecret := newSecret()
	updateservice := newDefaultUpdateService()
	r := newTestReconciler(updateservice)

	resources, err := newKubeResources(updateservice, testOperandImage, pullSecret, nil, nil)
	err = r.ensureEnvConfig(context.TODO(), log, updateservice, resources)
	if err != nil {
		t.Fatal(err)
	}
	found := &corev1.ConfigMap{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameEnvConfig(updateservice), Namespace: updateservice.Namespace}, found)
	if err != nil {
		t.Fatal(err)
	}
	verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateservice)
	verifyAnnotation(t, found.ObjectMeta.Annotations, "the environment information shared")
	assert.NotEmpty(t, found.Data["pe.upstream"])
}

func TestEnsurePullSecret(t *testing.T) {
	tests := []struct {
		name           string
		existingObjs   []runtime.Object
		expectedError  error
		expectedSecret *corev1.Secret
	}{
		{
			name: "NoSecret",
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
			},
			expectedError: fmt.Errorf("secrets \"%v\" not found", namePullSecret),
		},
		{
			name: "SecretCreate",
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
				newSecret(),
			},
			expectedSecret: func() *corev1.Secret {
				localSecret := newSecret()
				localSecret.Namespace = newDefaultUpdateService().Namespace
				return localSecret
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updateservice := newDefaultUpdateService()
			r := newTestReconciler(test.existingObjs...)

			ps, err := r.findPullSecret(context.TODO(), log, updateservice)
			if err != nil {
				assert.Error(t, err)
			}

			if verifyError(t, err, test.expectedError) {
				return
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, updateservice)
			if err != nil {
				assert.Error(t, err)
			}

			resources, err := newKubeResources(updateservice, testOperandImage, ps, cm, nil)

			if !apierrors.IsNotFound(err) {
				err = r.ensurePullSecret(context.TODO(), log, updateservice, resources)
			}
			found := &corev1.Secret{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: namePullSecret, Namespace: updateservice.Namespace}, found)
			if err != nil {
				assert.Error(t, err)
				assert.Empty(t, found)
			} else {
				verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateservice)
				verifyAnnotation(t, found.ObjectMeta.Annotations, "the pull credentials from the global pull secret")
				assert.Equal(t, found.Data, test.expectedSecret.Data)
			}
		})
	}
}

func TestEnsureAdditionalTrustedCA(t *testing.T) {
	tests := []struct {
		name              string
		existingObjs      []runtime.Object
		expectedError     error
		expectedConfigMap *corev1.ConfigMap
	}{
		{
			name: "NoImage",
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
				newSecret(),
			},
		},
		{
			name: "NoAdditionalTrustedCAName",
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
				newSecret(),
				func() *configv1.Image {
					image := newImage()
					image.Spec.AdditionalTrustedCA.Name = ""
					return image
				}(),
			},
		},
		{
			name: "NoConfigMap",
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
				newSecret(),
				newImage(),
			},
			expectedError: fmt.Errorf("configmaps \"%v\" not found", newImage().Spec.AdditionalTrustedCA.Name),
		},
		{
			name: "NoConfigMapKey",
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
				newSecret(),
				newImage(),
				newConfigMap(),
			},
			expectedConfigMap: func() *corev1.ConfigMap {
				localConfigMap := newConfigMap()
				localConfigMap.Namespace = newDefaultUpdateService().Namespace
				return localConfigMap
			}(),
		},
		{
			name: "ConfigMapCreate",
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
				newSecret(),
				newImage(),
				func() *corev1.ConfigMap {
					localConfigMap := newConfigMap()
					localConfigMap.Data[NameCertConfigMapKey] = "some random text"
					return localConfigMap
				}(),
			},
			expectedConfigMap: func() *corev1.ConfigMap {
				localConfigMap := newConfigMap()
				localConfigMap.Namespace = newDefaultUpdateService().Namespace
				localConfigMap.Data[NameCertConfigMapKey] = "some random text"
				return localConfigMap
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updateservice := newDefaultUpdateService()
			r := newTestReconciler(test.existingObjs...)

			ps, err := r.findPullSecret(context.TODO(), log, updateservice)
			if err != nil {
				assert.Error(t, err)
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, updateservice)
			if err != nil {
				assert.Error(t, err)
			}

			if verifyError(t, err, test.expectedError) {
				return
			}

			resources, err := newKubeResources(updateservice, testOperandImage, ps, cm, nil)

			err = r.ensureAdditionalTrustedCA(context.TODO(), log, updateservice, resources)

			found := &corev1.ConfigMap{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameAdditionalTrustedCA(updateservice), Namespace: updateservice.Namespace}, found)
			if err != nil {
				assert.Error(t, err)
				assert.Empty(t, found)
			} else {
				verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateservice)
				verifyAnnotation(t, found.ObjectMeta.Annotations, "additional certificate authorities to be trusted")
				assert.Equal(t, found.Data, test.expectedConfigMap.Data)
			}
		})
	}
}

func TestEnsureDeployment(t *testing.T) {
	tests := []struct {
		name         string
		existingObjs []runtime.Object
		caCert       bool
	}{
		{
			name: "EnsureDeployment",
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
				newSecret(),
			},
			caCert: false,
		},
		{
			name: "EnsureDeploymentWithGraphDataImage",
			existingObjs: []runtime.Object{
				func() *cv1.UpdateService {
					updateservice := newDefaultUpdateService()
					updateservice.Spec.GraphDataImage = testGraphDataImage
					return updateservice
				}(),
				newSecret(),
			},
			caCert: false,
		},
		{
			name: "EnsureDeploymentWithCaCert",
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
				newSecret(),
				newImage(),
				func() *corev1.ConfigMap {
					localConfigMap := newConfigMap()
					localConfigMap.Data[NameCertConfigMapKey] = "some random text"
					return localConfigMap
				}(),
			},
			caCert: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updateservice := newDefaultUpdateService()
			r := newTestReconciler(test.existingObjs...)

			ps, err := r.findPullSecret(context.TODO(), log, updateservice)
			if err != nil {
				assert.Error(t, err)
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, updateservice)
			if err != nil {
				assert.Error(t, err)
			}

			resources, err := newKubeResources(updateservice, testOperandImage, ps, cm, nil)

			err = r.ensureDeployment(context.TODO(), log, updateservice, resources, "")
			if err != nil {
				t.Fatal(err)
			}

			found := &appsv1.Deployment{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameDeployment(updateservice), Namespace: updateservice.Namespace}, found)
			if err != nil {
				t.Fatal(err)
			}

			verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateservice)
			verifyAnnotation(t, found.ObjectMeta.Annotations, "launches the components for the OpenShift UpdateService", updateservice.Name)
			assert.Equal(t, found.Spec.Selector.MatchLabels["app"], nameDeployment(updateservice))
			assert.Equal(t, found.Spec.Replicas, &updateservice.Spec.Replicas)

			assert.Equal(t, found.Spec.Template.Spec.Volumes[0].Name, "configs")
			assert.Equal(t, found.Spec.Template.Spec.Volumes[1].Name, "cincinnati-graph-data")

			assert.Equal(t, found.Spec.Template.Spec.Containers[0].Name, resources.graphBuilderContainer.Name)
			assert.Equal(t, found.Spec.Template.Spec.Containers[1].Image, resources.graphBuilderContainer.Image)
			assert.Equal(t, found.Spec.Template.Spec.Containers[1].Name, resources.policyEngineContainer.Name)
			assert.Equal(t, found.Spec.Template.Spec.Containers[1].Image, resources.graphBuilderContainer.Image)
			assert.Equal(t, found.Spec.Template.Spec.Volumes[2].Name, namePullSecret)
			assert.Equal(t, found.Spec.Template.Spec.Containers[0].VolumeMounts[2].Name, namePullSecret)

			if test.caCert {
				assert.Equal(t, found.Spec.Template.Spec.Volumes[3].Name, NameTrustedCAVolume)
				assert.Equal(t, found.Spec.Template.Spec.Containers[0].VolumeMounts[3].Name, NameTrustedCAVolume)
			}

			initContainer := found.Spec.Template.Spec.InitContainers[0]
			assert.Equal(t, &initContainer, resources.graphDataInitContainer)
		})
	}
}

func TestEnsureGraphBuilderService(t *testing.T) {
	pullSecret := newSecret()
	updateservice := newDefaultUpdateService()
	r := newTestReconciler(updateservice)

	resources, err := newKubeResources(updateservice, testOperandImage, pullSecret, nil, nil)
	err = r.ensureGraphBuilderService(context.TODO(), log, updateservice, resources)
	if err != nil {
		t.Fatal(err)
	}

	found := &corev1.Service{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameGraphBuilderService(updateservice), Namespace: updateservice.Namespace}, found)
	if err != nil {
		t.Fatal(err)
	}

	verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateservice)
	verifyAnnotation(t, found.ObjectMeta.Annotations, "a client-agnostic update graph to other clients within the cluster")
	assert.Equal(t, found.ObjectMeta.Labels["app"], nameGraphBuilderService(updateservice))
	assert.Equal(t, found.Spec.Selector["deployment"], nameDeployment(updateservice))
}

func TestEnsurePolicyEngineService(t *testing.T) {
	pullSecret := newSecret()
	updateservice := newDefaultUpdateService()
	r := newTestReconciler(updateservice)

	resources, err := newKubeResources(updateservice, testOperandImage, pullSecret, nil, nil)
	err = r.ensurePolicyEngineService(context.TODO(), log, updateservice, resources)
	if err != nil {
		t.Fatal(err)
	}

	found := &corev1.Service{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: namePolicyEngineService(updateservice), Namespace: updateservice.Namespace}, found)
	if err != nil {
		t.Fatal(err)
	}

	verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateservice)
	verifyAnnotation(t, found.ObjectMeta.Annotations, "views of the update graph by applying a set of filters")
	assert.Equal(t, found.ObjectMeta.Labels["app"], namePolicyEngineService(updateservice))
	assert.Equal(t, found.Spec.Selector["deployment"], nameDeployment(updateservice))
}

func TestEnsurePodDisruptionBudget(t *testing.T) {
	tests := []struct {
		name         string
		existingObjs []runtime.Object
	}{
		{
			name: "EnsurePodDisruptionBudgetReplicas1",
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
				newSecret(),
			},
		},
		{
			name: "EnsurePodDisruptionBudgetReplicas10",
			existingObjs: []runtime.Object{
				func() *cv1.UpdateService {
					updateservice := newDefaultUpdateService()
					updateservice.Spec.Replicas = 10
					return updateservice
				}(),
				newSecret(),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := newTestReconciler(test.existingObjs...)

			updateservice := &cv1.UpdateService{}
			err := r.Client.Get(context.TODO(), types.NamespacedName{Name: testName, Namespace: testNamespace}, updateservice)
			if err != nil {
				if apierrors.IsNotFound(err) {
					assert.Error(t, err)
				}
				assert.Error(t, err)
			}

			ps, err := r.findPullSecret(context.TODO(), log, updateservice)
			if err != nil {
				assert.Error(t, err)
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, updateservice)
			if err != nil {
				assert.Error(t, err)
			}

			resources, err := newKubeResources(updateservice, testOperandImage, ps, cm, nil)
			err = r.ensurePodDisruptionBudget(context.TODO(), log, updateservice, resources)
			if err != nil {
				t.Fatal(err)
			}

			found := &policyv1.PodDisruptionBudget{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: namePodDisruptionBudget(updateservice), Namespace: updateservice.Namespace}, found)
			if err != nil {
				t.Fatal(err)
			}

			verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateservice)
			verifyAnnotation(t, found.ObjectMeta.Annotations, "at least one Pod running at all times")
			assert.Equal(t, found.Spec.Selector.MatchLabels["app"], nameDeployment(updateservice))

			minAvailable := getMinAvailablePBD(updateservice)
			assert.Equal(t, found.Spec.MinAvailable, &minAvailable)
		})
	}
}

func TestValidateRouteName(t *testing.T) {
	tests := []struct {
		name      string
		appName   string
		namespace string
		err       error
	}{
		{
			name:      "RouteNameValid",
			appName:   "foo",
			namespace: "openshift-update-service",
			err:       nil,
		},
		{
			name:      "RouteNameMaxLen",
			appName:   "foo",
			namespace: "openshift-update-service-0123456789012345678901234567",
			err:       nil,
		},
		{
			name:      "RouteNameTooLong",
			appName:   "foo",
			namespace: "openshift-update-service-01234567890123456789012345678",
			err:       errors.New("UpdateService route name \"foo-route-openshift-update-service-01234567890123456789012345678\" cannot exceed RFC 1123 maximum length of 63. Shorten the application name and/or namespace."),
		},
		{
			name:      "RouteNameInvalidFormat",
			appName:   "foo",
			namespace: "openshift-update-service-012345678901234567890123456.",
			err:       errors.New(fmt.Sprintf("UpdateService route name \"foo-route-openshift-update-service-012345678901234567890123456.\" has invalid format; must comply with %q.", dns1123LabelFmt)),
		},
		{
			name:      "RouteNameMultipleErrors",
			appName:   "foo",
			namespace: "openshift-update-service-0123456789012345678901234567.",
			err: errors.New("UpdateService route name \"foo-route-openshift-update-service-0123456789012345678901234567.\" cannot exceed RFC 1123 maximum length of 63. Shorten the application name and/or namespace. " +
				fmt.Sprintf("Route name has invalid format; must comply with %q.", dns1123LabelFmt)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updateservice := newDefaultUpdateService()
			err := validateRouteName(updateservice, test.appName, test.namespace)
			assert.Equal(t, test.err, err)
		})
	}
}

func TestEnsurePolicyEngineRoute(t *testing.T) {
	tests := []struct {
		name         string
		existingObjs []runtime.Object
	}{
		{
			name: "EnsurePolicyEngineRoute",
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
				newSecret(),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updateservice := newDefaultUpdateService()
			r := newTestReconciler(test.existingObjs...)

			ps, err := r.findPullSecret(context.TODO(), log, updateservice)
			if err != nil {
				assert.Error(t, err)
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, updateservice)
			if err != nil {
				assert.Error(t, err)
			}

			resources, err := newKubeResources(updateservice, testOperandImage, ps, cm, nil)
			err = r.ensurePolicyEngineRoute(context.TODO(), log, updateservice, resources)
			if err != nil {
				t.Fatal(err)
			}

			found := &routev1.Route{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: namePolicyEngineRoute(updateservice), Namespace: updateservice.Namespace}, found)
			if err != nil {
				t.Fatal(err)
			}

			verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateservice)
			verifyAnnotation(t, found.ObjectMeta.Annotations, "exposes views of the update graph")
			assert.Equal(t, found.ObjectMeta.Labels["app"], nameDeployment(updateservice))
			assert.Equal(t, found.Spec.To.Kind, "Service")
			assert.Equal(t, found.Spec.To.Name, namePolicyEngineService(updateservice))
			assert.Equal(t, found.Spec.Port.TargetPort, intstr.FromString("policy-engine"))
		})
	}
}

func newRequest(updateservice *cv1.UpdateService) reconcile.Request {
	namespacedName := types.NamespacedName{
		Namespace: updateservice.ObjectMeta.Namespace,
		Name:      updateservice.ObjectMeta.Name,
	}
	return reconcile.Request{NamespacedName: namespacedName}
}

func newTestReconciler(initObjs ...runtime.Object) *UpdateServiceReconciler {
	c := fake.NewFakeClientWithScheme(scheme.Scheme, initObjs...)
	return &UpdateServiceReconciler{
		Client:            c,
		Scheme:            scheme.Scheme,
		OperatorNamespace: "bar",
	}
}

func newDefaultUpdateService() *cv1.UpdateService {
	return &cv1.UpdateService{
		TypeMeta: metav1.TypeMeta{
			Kind:       testUpdateServiceKind,
			APIVersion: testUpdateServiceAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
		},
		Spec: cv1.UpdateServiceSpec{
			Replicas:       testReplicas,
			Releases:       testReleases,
			GraphDataImage: testGraphDataImage,
		},
	}
}

func newImage() *configv1.Image {
	return &configv1.Image{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaults.ImageConfigName,
		},
		Spec: configv1.ImageSpec{
			AdditionalTrustedCA: configv1.ConfigMapNameReference{
				Name: testConfigMap,
			},
		},
	}
}

func newConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testConfigMap,
			Namespace: OpenshiftConfigNamespace,
		},
		Data: map[string]string{
			"testData": "some random text",
		},
	}
}

func newSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namePullSecret,
			Namespace: OpenshiftConfigNamespace,
		},
		Data: map[string][]byte{
			"testData": []byte("some random text"),
		},
	}
}

func verifyConditions(t *testing.T, expectedConditions []conditionsv1.Condition, updateservice *cv1.UpdateService) {
	for _, condition := range expectedConditions {
		assert.True(t, conditionsv1.IsStatusConditionPresentAndEqual(updateservice.Status.Conditions, condition.Type, condition.Status))
	}
	assert.Equal(t, len(expectedConditions), len(updateservice.Status.Conditions))
}

func verifyOwnerReference(t *testing.T, ownerReference metav1.OwnerReference, updateservice *cv1.UpdateService) {
	assert.Equal(t, ownerReference.Name, updateservice.Name)
	//Note: These properties seem to be derived from runtime object and do not seem to match the expected values.
	//assert.Equal(t, ownerReference.Kind, updateservice.Kind)
	//assert.Equal(t, ownerReference.APIVersion, updateservice.APIVersion)
}

func verifyError(t *testing.T, err error, expectedError error) bool {
	if expectedError != nil {
		return assert.EqualError(t, err, expectedError.Error())
	}
	return false
}
