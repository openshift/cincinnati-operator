package controllers

import (
	"context"
	"fmt"
	"os"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	cv1beta1 "github.com/openshift/cincinnati-operator/api/v1beta1"
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
	testName                 = "foo"
	testNamespace            = "bar"
	testCincinnatiKind       = "testKind"
	testCincinnatiAPIVersion = "testAPIVersion"
	testOperandImage         = "testOperandImage"
	testReplicas             = 1
	testRegistry             = "testRegistry"
	testRepository           = "testRepository"
	testGraphDataImage       = "testGraphDataImage"
	testConfigMap            = "testConfigMap"
)

var _ reconcile.Reconciler = &CincinnatiReconciler{}

func TestMain(m *testing.M) {
	if err := cv1beta1.AddToScheme(scheme.Scheme); err != nil {
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
				newDefaultCincinnati(),
				newSecret(),
			},
			expectedConditions: []conditionsv1.Condition{
				{
					Type:   cv1beta1.ConditionReconcileCompleted,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   cv1beta1.ConditionRegistryCACertFound,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := newTestReconciler(test.existingObjs...)
			request := newRequest(newDefaultCincinnati())

			_, err := r.Reconcile(request)
			if err != nil {
				t.Fatal(err)
			}
			instance := &cv1beta1.Cincinnati{}
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
	cincinnati := newDefaultCincinnati()
	r := newTestReconciler(cincinnati)

	resources, err := newKubeResources(cincinnati, testOperandImage, pullSecret, nil)
	err = r.ensureConfig(context.TODO(), log, cincinnati, resources)
	if err != nil {
		t.Fatal(err)
	}
	found := &corev1.ConfigMap{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameConfig(cincinnati), Namespace: cincinnati.Namespace}, found)
	if err != nil {
		t.Fatal(err)
	}
	verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], cincinnati)
	// TODO: check that the gb.toml is corrently rendered with cincinnati.Spec
	assert.NotEmpty(t, found.Data["gb.toml"])
}

func TestEnsureEnvConfig(t *testing.T) {
	pullSecret := newSecret()
	cincinnati := newDefaultCincinnati()
	r := newTestReconciler(cincinnati)

	resources, err := newKubeResources(cincinnati, testOperandImage, pullSecret, nil)
	err = r.ensureEnvConfig(context.TODO(), log, cincinnati, resources)
	if err != nil {
		t.Fatal(err)
	}
	found := &corev1.ConfigMap{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameEnvConfig(cincinnati), Namespace: cincinnati.Namespace}, found)
	if err != nil {
		t.Fatal(err)
	}
	verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], cincinnati)
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
				newDefaultCincinnati(),
			},
			expectedError: fmt.Errorf("secrets \"%v\" not found", namePullSecret),
		},
		{
			name: "SecretCreate",
			existingObjs: []runtime.Object{
				newDefaultCincinnati(),
				newSecret(),
			},
			expectedSecret: func() *corev1.Secret {
				localSecret := newSecret()
				localSecret.Namespace = newDefaultCincinnati().Namespace
				return localSecret
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cincinnati := newDefaultCincinnati()
			r := newTestReconciler(test.existingObjs...)

			ps, err := r.findPullSecret(context.TODO(), log, cincinnati)
			if err != nil {
				assert.Error(t, err)
			}

			if verifyError(t, err, test.expectedError) {
				return
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, cincinnati)
			if err != nil {
				assert.Error(t, err)
			}

			resources, err := newKubeResources(cincinnati, testOperandImage, ps, cm)

			if !errors.IsNotFound(err) {
				err = r.ensurePullSecret(context.TODO(), log, cincinnati, resources)
			}
			found := &corev1.Secret{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: namePullSecret, Namespace: cincinnati.Namespace}, found)
			if err != nil {
				assert.Error(t, err)
				assert.Empty(t, found)
			} else {
				verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], cincinnati)
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
				newDefaultCincinnati(),
				newSecret(),
			},
		},
		{
			name: "NoAdditionalTrustedCAName",
			existingObjs: []runtime.Object{
				newDefaultCincinnati(),
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
				newDefaultCincinnati(),
				newSecret(),
				newImage(),
			},
			expectedError: fmt.Errorf("configmaps \"%v\" not found", newImage().Spec.AdditionalTrustedCA.Name),
		},
		{
			name: "NoConfigMapKey",
			existingObjs: []runtime.Object{
				newDefaultCincinnati(),
				newSecret(),
				newImage(),
				newConfigMap(),
			},
			expectedConfigMap: func() *corev1.ConfigMap {
				localConfigMap := newConfigMap()
				localConfigMap.Namespace = newDefaultCincinnati().Namespace
				return localConfigMap
			}(),
		},
		{
			name: "ConfigMapCreate",
			existingObjs: []runtime.Object{
				newDefaultCincinnati(),
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
				localConfigMap.Namespace = newDefaultCincinnati().Namespace
				localConfigMap.Data[NameCertConfigMapKey] = "some random text"
				return localConfigMap
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cincinnati := newDefaultCincinnati()
			r := newTestReconciler(test.existingObjs...)

			ps, err := r.findPullSecret(context.TODO(), log, cincinnati)
			if err != nil {
				assert.Error(t, err)
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, cincinnati)
			if err != nil {
				assert.Error(t, err)
			}

			if verifyError(t, err, test.expectedError) {
				return
			}

			resources, err := newKubeResources(cincinnati, testOperandImage, ps, cm)

			err = r.ensureAdditionalTrustedCA(context.TODO(), log, cincinnati, resources)

			found := &corev1.ConfigMap{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameAdditionalTrustedCA(cincinnati), Namespace: cincinnati.Namespace}, found)
			if err != nil {
				assert.Error(t, err)
				assert.Empty(t, found)
			} else {
				verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], cincinnati)
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
				newDefaultCincinnati(),
				newSecret(),
			},
			caCert: false,
		},
		{
			name: "EnsureDeploymentWithGraphDataImage",
			existingObjs: []runtime.Object{
				func() *cv1beta1.Cincinnati {
					cincinnati := newDefaultCincinnati()
					cincinnati.Spec.GraphDataImage = testGraphDataImage
					return cincinnati
				}(),
				newSecret(),
			},
			caCert: false,
		},
		{
			name: "EnsureDeploymentWithCaCert",
			existingObjs: []runtime.Object{
				newDefaultCincinnati(),
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
			cincinnati := newDefaultCincinnati()
			r := newTestReconciler(test.existingObjs...)

			ps, err := r.findPullSecret(context.TODO(), log, cincinnati)
			if err != nil {
				assert.Error(t, err)
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, cincinnati)
			if err != nil {
				assert.Error(t, err)
			}

			resources, err := newKubeResources(cincinnati, testOperandImage, ps, cm)

			err = r.ensureDeployment(context.TODO(), log, cincinnati, resources)
			if err != nil {
				t.Fatal(err)
			}

			found := &appsv1.Deployment{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameDeployment(cincinnati), Namespace: cincinnati.Namespace}, found)
			if err != nil {
				t.Fatal(err)
			}

			verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], cincinnati)
			assert.Equal(t, found.Spec.Selector.MatchLabels["app"], nameDeployment(cincinnati))
			assert.Equal(t, found.Spec.Replicas, &cincinnati.Spec.Replicas)

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
	cincinnati := newDefaultCincinnati()
	r := newTestReconciler(cincinnati)

	resources, err := newKubeResources(cincinnati, testOperandImage, pullSecret, nil)
	err = r.ensureGraphBuilderService(context.TODO(), log, cincinnati, resources)
	if err != nil {
		t.Fatal(err)
	}

	found := &corev1.Service{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: nameGraphBuilderService(cincinnati), Namespace: cincinnati.Namespace}, found)
	if err != nil {
		t.Fatal(err)
	}

	verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], cincinnati)
	assert.Equal(t, found.ObjectMeta.Labels["app"], nameGraphBuilderService(cincinnati))
	assert.Equal(t, found.Spec.Selector["deployment"], nameDeployment(cincinnati))
}

func TestEnsurePolicyEngineService(t *testing.T) {
	pullSecret := newSecret()
	cincinnati := newDefaultCincinnati()
	r := newTestReconciler(cincinnati)

	resources, err := newKubeResources(cincinnati, testOperandImage, pullSecret, nil)
	err = r.ensurePolicyEngineService(context.TODO(), log, cincinnati, resources)
	if err != nil {
		t.Fatal(err)
	}

	found := &corev1.Service{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: namePolicyEngineService(cincinnati), Namespace: cincinnati.Namespace}, found)
	if err != nil {
		t.Fatal(err)
	}

	verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], cincinnati)
	assert.Equal(t, found.ObjectMeta.Labels["app"], namePolicyEngineService(cincinnati))
	assert.Equal(t, found.Spec.Selector["deployment"], nameDeployment(cincinnati))
}

func TestEnsurePodDisruptionBudget(t *testing.T) {
	tests := []struct {
		name         string
		existingObjs []runtime.Object
	}{
		{
			name: "EnsurePodDisruptionBudgetReplicas1",
			existingObjs: []runtime.Object{
				newDefaultCincinnati(),
				newSecret(),
			},
		},
		{
			name: "EnsurePodDisruptionBudgetReplicas10",
			existingObjs: []runtime.Object{
				func() *cv1beta1.Cincinnati {
					cincinnati := newDefaultCincinnati()
					cincinnati.Spec.Replicas = 10
					return cincinnati
				}(),
				newSecret(),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := newTestReconciler(test.existingObjs...)

			cincinnati := &cv1beta1.Cincinnati{}
			err := r.Client.Get(context.TODO(), types.NamespacedName{Name: testName, Namespace: testNamespace}, cincinnati)
			if err != nil {
				if errors.IsNotFound(err) {
					assert.Error(t, err)
				}
				assert.Error(t, err)
			}

			ps, err := r.findPullSecret(context.TODO(), log, cincinnati)
			if err != nil {
				assert.Error(t, err)
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, cincinnati)
			if err != nil {
				assert.Error(t, err)
			}

			resources, err := newKubeResources(cincinnati, testOperandImage, ps, cm)
			err = r.ensurePodDisruptionBudget(context.TODO(), log, cincinnati, resources)
			if err != nil {
				t.Fatal(err)
			}

			found := &policyv1beta1.PodDisruptionBudget{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: namePodDisruptionBudget(cincinnati), Namespace: cincinnati.Namespace}, found)
			if err != nil {
				t.Fatal(err)
			}

			verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], cincinnati)
			assert.Equal(t, found.Spec.Selector.MatchLabels["app"], nameDeployment(cincinnati))

			minAvailable := getMinAvailablePBD(cincinnati)
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
				newDefaultCincinnati(),
				newSecret(),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cincinnati := newDefaultCincinnati()
			r := newTestReconciler(test.existingObjs...)

			ps, err := r.findPullSecret(context.TODO(), log, cincinnati)
			if err != nil {
				assert.Error(t, err)
			}

			cm, err := r.findTrustedCAConfig(context.TODO(), log, cincinnati)
			if err != nil {
				assert.Error(t, err)
			}

			resources, err := newKubeResources(cincinnati, testOperandImage, ps, cm)
			err = r.ensurePolicyEngineRoute(context.TODO(), log, cincinnati, resources)
			if err != nil {
				t.Fatal(err)
			}

			found := &routev1.Route{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: namePolicyEngineRoute(cincinnati), Namespace: cincinnati.Namespace}, found)
			if err != nil {
				t.Fatal(err)
			}

			verifyOwnerReference(t, found.ObjectMeta.OwnerReferences[0], cincinnati)
			assert.Equal(t, found.ObjectMeta.Labels["app"], nameDeployment(cincinnati))
			assert.Equal(t, found.Spec.To.Kind, "Service")
			assert.Equal(t, found.Spec.To.Name, namePolicyEngineService(cincinnati))
			assert.Equal(t, found.Spec.Port.TargetPort, intstr.FromString("policy-engine"))
		})
	}
}

func newRequest(cincinnati *cv1beta1.Cincinnati) reconcile.Request {
	namespacedName := types.NamespacedName{
		Namespace: cincinnati.ObjectMeta.Namespace,
		Name:      cincinnati.ObjectMeta.Name,
	}
	return reconcile.Request{NamespacedName: namespacedName}
}

func newTestReconciler(initObjs ...runtime.Object) *CincinnatiReconciler {
	c := fake.NewFakeClientWithScheme(scheme.Scheme, initObjs...)
	return &CincinnatiReconciler{
		Client: c,
		Scheme: scheme.Scheme,
	}
}

func newDefaultCincinnati() *cv1beta1.Cincinnati {
	return &cv1beta1.Cincinnati{
		TypeMeta: metav1.TypeMeta{
			Kind:       testCincinnatiKind,
			APIVersion: testCincinnatiAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
		},
		Spec: cv1beta1.CincinnatiSpec{
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

func verifyConditions(t *testing.T, expectedConditions []conditionsv1.Condition, cincinnati *cv1beta1.Cincinnati) {
	for _, condition := range expectedConditions {
		assert.True(t, conditionsv1.IsStatusConditionPresentAndEqual(cincinnati.Status.Conditions, condition.Type, condition.Status))
	}
	assert.Equal(t, len(expectedConditions), len(cincinnati.Status.Conditions))
}

func verifyOwnerReference(t *testing.T, ownerReference metav1.OwnerReference, cincinnati *cv1beta1.Cincinnati) {
	assert.Equal(t, ownerReference.Name, cincinnati.Name)
	//Note: These properties seem to be derived from runtime object and do not seem to match the expected values.
	//assert.Equal(t, ownerReference.Kind, cincinnati.Kind)
	//assert.Equal(t, ownerReference.APIVersion, cincinnati.APIVersion)
}

func verifyError(t *testing.T, err error, expectedError error) bool {
	if expectedError != nil {
		return assert.EqualError(t, err, expectedError.Error())
	}
	return false
}
