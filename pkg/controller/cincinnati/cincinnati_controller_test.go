package cincinnati

import (
	"context"
	"os"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cincinnati-operator/pkg/apis"
	cv1alpha1 "github.com/openshift/cincinnati-operator/pkg/apis/cincinnati/v1alpha1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	testReplicas             = -1
	testRegistry             = "testRegistry"
	testRepository           = "testRepository"
	testGitHubOrg            = "testGitHubOrg"
	testGitHubRepo           = "testGitHubRepo"
	testGitHubBranch         = "testGitHubBranch"
)

var _ reconcile.Reconciler = &ReconcileCincinnati{}

func TestMain(m *testing.M) {
	if err := apis.AddToScheme(scheme.Scheme); err != nil {
		log.Error(err, "Failed adding apis to scheme")
		os.Exit(1)
	}
	if err := configv1.AddToScheme(scheme.Scheme); err != nil {
		log.Error(err, "Failed adding apis to scheme")
		os.Exit(1)
	}

	exitVal := m.Run()
	os.Exit(exitVal)
}

// TestReconcileComplete ensures that the cincinnati object is reconciled successfully
func TestReconcileComplete(t *testing.T) {
	cincinnati := newDefaultCincinnati()
	r := newTestReconciler(cincinnati)
	request := newRequest(cincinnati)

	_, err := r.Reconcile(request)
	if err != nil {
		t.Fatal(err)
	}
	instance := &cv1alpha1.Cincinnati{}
	r.client.Get(context.TODO(), request.NamespacedName, instance)
	assert.True(t, conditionsv1.IsStatusConditionTrue(instance.Status.Conditions, cv1alpha1.ConditionReconcileCompleted))
}

// TestEnsureConfig ensures that Config is created successully and the OwnerReference is set.
func TestEnsureConfig(t *testing.T) {
	cincinnati := newDefaultCincinnati()
	r := newTestReconciler(cincinnati)

	resources, err := newKubeResources(cincinnati, testOperandImage)
	err = r.ensureConfig(context.TODO(), log, cincinnati, resources)
	if err != nil {
		t.Fatal(err)
	}
	found := &corev1.ConfigMap{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: nameConfig(cincinnati), Namespace: cincinnati.Namespace}, found)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, found.ObjectMeta.OwnerReferences[0].Name, cincinnati.Name)
	// TODO: check that the gb.toml is corrently rendered with cincinnati.Spec
	assert.NotNil(t, found.Data["gb.toml"])
}

// TestEnsureConfig ensures that EnvConfig is created successully and the OwnerReference is set.
func TestEnsureEnvConfig(t *testing.T) {
	cincinnati := newDefaultCincinnati()
	r := newTestReconciler(cincinnati)

	resources, err := newKubeResources(cincinnati, testOperandImage)
	err = r.ensureEnvConfig(context.TODO(), log, cincinnati, resources)
	if err != nil {
		t.Fatal(err)
	}
	found := &corev1.ConfigMap{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: nameEnvConfig(cincinnati), Namespace: cincinnati.Namespace}, found)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, found.ObjectMeta.OwnerReferences[0].Name, cincinnati.Name)
	assert.NotNil(t, found.Data["pe.upstream"])
}

func newRequest(cincinnati *cv1alpha1.Cincinnati) reconcile.Request {
	namespacedName := types.NamespacedName{
		Namespace: cincinnati.ObjectMeta.Namespace,
		Name:      cincinnati.ObjectMeta.Name,
	}
	return reconcile.Request{NamespacedName: namespacedName}
}

func newTestReconciler(initObjs ...runtime.Object) *ReconcileCincinnati {
	c := fake.NewFakeClientWithScheme(scheme.Scheme, initObjs...)
	return &ReconcileCincinnati{
		client: c,
		scheme: scheme.Scheme,
	}
}

func newDefaultCincinnati() *cv1alpha1.Cincinnati {
	return &cv1alpha1.Cincinnati{
		TypeMeta: metav1.TypeMeta{
			Kind: testCincinnatiKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
		},
		Spec: cv1alpha1.CincinnatiSpec{
			Replicas:   testReplicas,
			Registry:   testRegistry,
			Repository: testRepository,
			GitHubOrg:  testGitHubOrg,
			GitHubRepo: testGitHubRepo,
			Branch:     testGitHubBranch,
		},
	}
}
