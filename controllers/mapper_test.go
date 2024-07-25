package controllers

import (
	"testing"

	apicfgv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestMap(t *testing.T) {
	tests := []struct {
		name             string
		image            *apicfgv1.Image
		configMap        *corev1.ConfigMap
		existingObjs     []runtime.Object
		expectedRequests []reconcile.Request
	}{
		{
			name: "IncorrectImageNameNoRequeue",
			image: &apicfgv1.Image{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: "",
				},
			},
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
			},
			expectedRequests: []reconcile.Request{},
		},
		{
			name: "IncorrectImageNamespaceNameNoRequeue",
			image: &apicfgv1.Image{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testNamespace,
				},
			},
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
			},
			expectedRequests: []reconcile.Request{},
		},
		{
			name: "IncorrectImageNamespaceNoRequeue",
			image: &apicfgv1.Image{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaults.ImageConfigName,
					Namespace: testNamespace,
				},
			},
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
			},
			expectedRequests: []reconcile.Request{},
		},
		{
			name: "ImageRequeue",
			image: &apicfgv1.Image{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaults.ImageConfigName,
					Namespace: "",
				},
			},
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
			},
			expectedRequests: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      newDefaultUpdateService().Name,
						Namespace: newDefaultUpdateService().Namespace,
					},
				},
			},
		},
		{
			name: "IncorrectConfigMapNamespaceNoRequeue",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testConfigMap,
					Namespace: testNamespace,
				},
			},
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
			},
			expectedRequests: []reconcile.Request{},
		},
		{
			name: "ConfigMapRequeue",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testConfigMap,
					Namespace: OpenshiftConfigNamespace,
				},
			},
			existingObjs: []runtime.Object{
				newDefaultUpdateService(),
				newImage(),
			},
			expectedRequests: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      newDefaultUpdateService().Name,
						Namespace: newDefaultUpdateService().Namespace,
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := newTestReconciler(test.existingObjs...)
			m := mapper{r.Client, ""}
			var reqs []reconcile.Request
			if test.image != nil {
				reqs = m.Map(test.image)
			} else {
				reqs = m.Map(test.configMap)
			}
			assert.Equal(t, test.expectedRequests, reqs)
		})
	}
}
