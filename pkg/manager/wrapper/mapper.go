package wrapper

import (
	"context"
	apicfgv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apicfgv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

type mapper struct{}

// Map will return a reconcile request for a ConfigMap if the event is for a
// BareMetalHost and that BareMetalHost references a Machine.
func (m *mapper) Map(obj handler.MapObject) []reconcile.Request {

	// Need to give mapper a working client so Map can GET the ImageConfig object
	if cm, ok := obj.Object.(*corev1.ConfigMap); ok {
		image := &apicfgv1.Image{}
		err := m.manager.client.Get(context.TODO(), types.NamespacedName{Name: defaults.ImageConfigName, Namespace: ""}, image)
		if err != nil {
			return []reconcile.Request{}
		}
		if image.Spec.AdditionalTrustedCA.Name == "" {
			return []reconcile.Request{}
		}
		if image.Spec.AdditionalTrustedCA.Name == cm.ObjectMeta.Name && cm.Data != nil {
			return []reconcile.Request{
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      cm.ObjectMeta.Name,
						Namespace: cm.ObjectMeta.Namespace,
					},
				},
			}
		}
	} else if img, ok := obj.Object.(*apicfgv1.Image); ok {
		if img.Spec.AdditionalTrustedCA != nil {
			return []reconcile.Request{
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      defaults.ImageConfigName,
						Namespace: "",
					},
				},
			}
		}
	}

	return []reconcile.Request{}
}
