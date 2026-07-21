package tlsconfig

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	configlistersv1 "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// directAPIServerLister adapts a controller-runtime client.Reader to the
// configlistersv1.APIServerLister interface required by library-go's
// TLS security profile observer. This follows the pattern from
// openshift/hive#2800.
type directAPIServerLister struct {
	ctx    context.Context
	reader client.Reader
}

func (d *directAPIServerLister) List(selector labels.Selector) ([]*configv1.APIServer, error) {
	list := &configv1.APIServerList{}
	if err := d.reader.List(d.ctx, list); err != nil {
		return nil, err
	}
	var result []*configv1.APIServer
	for i := range list.Items {
		result = append(result, &list.Items[i])
	}
	return result, nil
}

func (d *directAPIServerLister) Get(name string) (*configv1.APIServer, error) {
	obj := &configv1.APIServer{}
	if err := d.reader.Get(d.ctx, client.ObjectKey{Name: name}, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// observerListers satisfies both configobserver.Listers and
// apiserver.APIServerLister by delegating APIServer lookups to a
// directAPIServerLister. PreRunHasSynced and ResourceSyncer are stubbed
// since we make direct API calls rather than using informer caches.
type observerListers struct {
	lister configlistersv1.APIServerLister
}

func (o *observerListers) APIServerLister() configlistersv1.APIServerLister {
	return o.lister
}

func (o *observerListers) PreRunHasSynced() []cache.InformerSynced {
	return []cache.InformerSynced{}
}

func (o *observerListers) ResourceSyncer() resourcesynccontroller.ResourceSyncer {
	return nil
}
