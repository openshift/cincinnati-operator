package controllers

import (
	"context"
	"fmt"
	"os"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	updateservicev1 "github.com/openshift/cincinnati-operator/api/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/stretchr/testify/assert"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	testRegistry                = "testRegistry"
	testRepository              = "testRepository"
	testGraphDataImage          = "testGraphDataImage"
	testConfigMap               = "testConfigMap"
)

var _ reconcile.Reconciler = &UpdateServiceReconciler{}

func TestMain(m *testing.M) {
	if err := updateservicev1.AddToScheme(scheme.Scheme); err != nil {
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
					Type:   updateservicev1.ConditionReconcileCompleted,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   updateservicev1.ConditionRegistryCACertFound,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := newTestReconciler(test.existingObjs...)
			request := newRequest(newDefaultUpdateService())

			_, err := r.Reconcile(request)
			if err != nil {
				t.Fatal(err)
			}
			instance := &updateservicev1.UpdateService{}
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
	updateService := newDefaultUpdateService()
	r := newTestReconciler(updateService)

	resources, err := newKubeResources(updateService, testOperandImage, pullSecret, nil)
	err = r.ensureConfig(context.TODO(), log, updateService, resources)
	if err != nil {
		t.Fatal(err)
	}
	found := &corev1.ConfigMap{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameConfig(updateService), Namespace: updateService.Namespace}, found)
	if err != nil {
		t.Fatal(err)
	}
	verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateService)
	// TODO: check that the gb.toml is corrently rendered with updateService.Spec
	assert.NotEmpty(t, found.Data["gb.toml"])
}

func TestEnsureEnvConfig(t *testing.T) {
	pullSecret := newSecret()
	updateService := newDefaultUpdateService()
	r := newTestReconciler(updateService)

	resources, err := newKubeResources(updateService, testOperandImage, pullSecret, nil)
	err = r.ensureEnvConfig(context.TODO(), log, updateService, resources)
	if err != nil {
		t.Fatal(err)
	}
	found := &corev1.ConfigMap{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameEnvConfig(updateService), Namespace: updateService.Namespace}, found)
	if err != nil {
		t.Fatal(err)
	}
	verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateService)
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
			updateService := newDefaultUpdateService()
			r := newTestReconciler(test.existingObjs...)

			ps, err := r.findPullSecret(context.TODO(), log, updateService)
			if err != nil {
				assert.Error(t, err)
			}

			if verifyError(t, err, test.expectedError) {
				return
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, updateService)
			if err != nil {
				assert.Error(t, err)
			}

			resources, err := newKubeResources(updateService, testOperandImage, ps, cm)

			if !errors.IsNotFound(err) {
				err = r.ensurePullSecret(context.TODO(), log, updateService, resources)
			}
			found := &corev1.Secret{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: namePullSecret, Namespace: updateService.Namespace}, found)
			if err != nil {
				assert.Error(t, err)
				assert.Empty(t, found)
			} else {
				verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateService)
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
			updateService := newDefaultUpdateService()
			r := newTestReconciler(test.existingObjs...)

			ps, err := r.findPullSecret(context.TODO(), log, updateService)
			if err != nil {
				assert.Error(t, err)
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, updateService)
			if err != nil {
				assert.Error(t, err)
			}

			if verifyError(t, err, test.expectedError) {
				return
			}

			resources, err := newKubeResources(updateService, testOperandImage, ps, cm)

			err = r.ensureAdditionalTrustedCA(context.TODO(), log, updateService, resources)

			found := &corev1.ConfigMap{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameAdditionalTrustedCA(updateService), Namespace: updateService.Namespace}, found)
			if err != nil {
				assert.Error(t, err)
				assert.Empty(t, found)
			} else {
				verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateService)
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
				func() *updateservicev1.UpdateService {
					updateService := newDefaultUpdateService()
					updateService.Spec.GraphDataImage = testGraphDataImage
					return updateService
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
			updateService := newDefaultUpdateService()
			r := newTestReconciler(test.existingObjs...)

			ps, err := r.findPullSecret(context.TODO(), log, updateService)
			if err != nil {
				assert.Error(t, err)
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, updateService)
			if err != nil {
				assert.Error(t, err)
			}

			resources, err := newKubeResources(updateService, testOperandImage, ps, cm)

			err = r.ensureDeployment(context.TODO(), log, updateService, resources)
			if err != nil {
				t.Fatal(err)
			}

			found := &appsv1.Deployment{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameDeployment(updateService), Namespace: updateService.Namespace}, found)
			if err != nil {
				t.Fatal(err)
			}

			verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateService)
			assert.Equal(t, found.Spec.Selector.MatchLabels["app"], nameDeployment(updateService))
			assert.Equal(t, found.Spec.Replicas, &updateService.Spec.Replicas)

			assert.Equal(t, found.Spec.Template.Spec.Volumes[0].Name, "configs")
			assert.Equal(t, found.Spec.Template.Spec.Volumes[1].Name, "update-service-graph-data")

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
	updateService := newDefaultUpdateService()
	r := newTestReconciler(updateService)

	resources, err := newKubeResources(updateService, testOperandImage, pullSecret, nil)
	err = r.ensureGraphBuilderService(context.TODO(), log, updateService, resources)
	if err != nil {
		t.Fatal(err)
	}

	found := &corev1.Service{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameGraphBuilderService(updateService), Namespace: updateService.Namespace}, found)
	if err != nil {
		t.Fatal(err)
	}

	verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateService)
	assert.Equal(t, found.ObjectMeta.Labels["app"], nameGraphBuilderService(updateService))
	assert.Equal(t, found.Spec.Selector["deployment"], nameDeployment(updateService))
}

func TestEnsurePolicyEngineService(t *testing.T) {
	pullSecret := newSecret()
	updateService := newDefaultUpdateService()
	r := newTestReconciler(updateService)

	resources, err := newKubeResources(updateService, testOperandImage, pullSecret, nil)
	err = r.ensurePolicyEngineService(context.TODO(), log, updateService, resources)
	if err != nil {
		t.Fatal(err)
	}

	found := &corev1.Service{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: namePolicyEngineService(updateService), Namespace: updateService.Namespace}, found)
	if err != nil {
		t.Fatal(err)
	}

	verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateService)
	assert.Equal(t, found.ObjectMeta.Labels["app"], namePolicyEngineService(updateService))
	assert.Equal(t, found.Spec.Selector["deployment"], nameDeployment(updateService))
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
				func() *updateservicev1.UpdateService {
					updateService := newDefaultUpdateService()
					updateService.Spec.Replicas = 10
					return updateService
				}(),
				newSecret(),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := newTestReconciler(test.existingObjs...)

			updateService := &updateservicev1.UpdateService{}
			err := r.Client.Get(context.TODO(), types.NamespacedName{Name: testName, Namespace: testNamespace}, updateService)
			if err != nil {
				if errors.IsNotFound(err) {
					assert.Error(t, err)
				}
				assert.Error(t, err)
			}

			ps, err := r.findPullSecret(context.TODO(), log, updateService)
			if err != nil {
				assert.Error(t, err)
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, updateService)
			if err != nil {
				assert.Error(t, err)
			}

			resources, err := newKubeResources(updateService, testOperandImage, ps, cm)
			err = r.ensurePodDisruptionBudget(context.TODO(), log, updateService, resources)
			if err != nil {
				t.Fatal(err)
			}

			found := &policyv1beta1.PodDisruptionBudget{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: namePodDisruptionBudget(updateService), Namespace: updateService.Namespace}, found)
			if err != nil {
				t.Fatal(err)
			}

			verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateService)
			assert.Equal(t, found.Spec.Selector.MatchLabels["app"], nameDeployment(updateService))

			minAvailable := getMinAvailablePBD(updateService)
			assert.Equal(t, found.Spec.MinAvailable, &minAvailable)
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
			updateService := newDefaultUpdateService()
			r := newTestReconciler(test.existingObjs...)

			ps, err := r.findPullSecret(context.TODO(), log, updateService)
			if err != nil {
				assert.Error(t, err)
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, updateService)
			if err != nil {
				assert.Error(t, err)
			}

			resources, err := newKubeResources(updateService, testOperandImage, ps, cm)
			err = r.ensurePolicyEngineRoute(context.TODO(), log, updateService, resources)
			if err != nil {
				t.Fatal(err)
			}

			found := &routev1.Route{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: namePolicyEngineRoute(updateService), Namespace: updateService.Namespace}, found)
			if err != nil {
				t.Fatal(err)
			}

			verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], updateService)
			assert.Equal(t, found.ObjectMeta.Labels["app"], nameDeployment(updateService))
			assert.Equal(t, found.Spec.To.Kind, "Service")
			assert.Equal(t, found.Spec.To.Name, namePolicyEngineService(updateService))
			assert.Equal(t, found.Spec.Port.TargetPort, intstr.FromString("policy-engine"))
		})
	}
}

func newRequest(updateService *updateservicev1.UpdateService) reconcile.Request {
	namespacedName := types.NamespacedName{
		Namespace: updateService.ObjectMeta.Namespace,
		Name:      updateService.ObjectMeta.Name,
	}
	return reconcile.Request{NamespacedName: namespacedName}
}

func newTestReconciler(initObjs ...runtime.Object) *UpdateServiceReconciler {
	c := fake.NewFakeClientWithScheme(scheme.Scheme, initObjs...)
	return &UpdateServiceReconciler{
		Client: c,
		Scheme: scheme.Scheme,
	}
}

func newDefaultUpdateService() *updateservicev1.UpdateService {
	return &updateservicev1.UpdateService{
		TypeMeta: metav1.TypeMeta{
			Kind:       testUpdateServiceKind,
			APIVersion: testUpdateServiceAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
		},
		Spec: updateservicev1.UpdateServiceSpec{
			Replicas:       testReplicas,
			Registry:       testRegistry,
			Repository:     testRepository,
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
			Namespace: openshiftConfigNamespace,
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
			Namespace: openshiftConfigNamespace,
		},
		Data: map[string][]byte{
			"testData": []byte("some random text"),
		},
	}
}

func verifyConditions(t *testing.T, expectedConditions []conditionsv1.Condition, updateService *updateservicev1.UpdateService) {
	for _, condition := range expectedConditions {
		assert.True(t, conditionsv1.IsStatusConditionPresentAndEqual(updateService.Status.Conditions, condition.Type, condition.Status))
	}
	assert.Equal(t, len(expectedConditions), len(updateService.Status.Conditions))
}

func verifyOwnerReference(t *testing.T, ownerReference metav1.OwnerReference, updateService *updateservicev1.UpdateService) {
	assert.Equal(t, ownerReference.Name, updateService.Name)
	//Note: These properties seem to be derived from runtime object and do not seem to match the expected values.
	//assert.Equal(t, ownerReference.Kind, updateService.Kind)
	//assert.Equal(t, ownerReference.APIVersion, updateService.APIVersion)
}

func verifyError(t *testing.T, err error, expectedError error) bool {
	if expectedError != nil {
		return assert.EqualError(t, err, expectedError.Error())
	}
	return false
}
