package cincinnati

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apicfgv1 "github.com/openshift/api/config/v1"
	cv1alpha1 "github.com/openshift/cincinnati-operator/pkg/apis/cincinnati/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

type mapper struct {
	client client.Client
}

// Map will return a reconcile request for a Cincinnati if the event is for a
// ImageConfigName Image or a ConfigMap referenced by AdditionalTrustedCA.Name.
func (m *mapper) Map(obj handler.MapObject) []reconcile.Request {
	if cm, ok := obj.Object.(*corev1.ConfigMap); ok {
		// There is already a watch on local configMap as a secondary resource
		// This watch is for the source configMap in openshift-config namespace
		if cm.Namespace != openshiftConfigNamespace {
			return []reconcile.Request{}
		}
		image := &apicfgv1.Image{}
		err := m.client.Get(context.TODO(), types.NamespacedName{Name: defaults.ImageConfigName, Namespace: ""}, image)
		if err != nil {
			if !errors.IsNotFound(err) {
				log.Error(err, "Could not get Image with Name:%v, Namespace: %v", defaults.ImageConfigName, "")
			}
			return []reconcile.Request{}
		}
		if image.Spec.AdditionalTrustedCA.Name == cm.ObjectMeta.Name {
			// If the object is configMap that we are watching, requeue all Cincinnati instances
			return m.requeueCincinnatis()
		}
	} else if img, ok := obj.Object.(*apicfgv1.Image); ok {
		// Check if this is the image we are interested in
		if img.Name == defaults.ImageConfigName && img.Namespace == "" {
			// Requeue all Cincinnati instances
			return m.requeueCincinnatis()
		}
	}
	return []reconcile.Request{}
}

func (m *mapper) requeueCincinnatis() []reconcile.Request {
	cincinnatis := &cv1alpha1.CincinnatiList{}
	err := m.client.List(context.TODO(), cincinnatis)
	if err != nil {
		return []reconcile.Request{}
	}
	var requests []reconcile.Request
	for _, cincinnati := range cincinnatis.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      cincinnati.Name,
				Namespace: cincinnati.Namespace,
			},
		})
	}
	return requests
}
