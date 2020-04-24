package wrapper

import (
	"fmt"

	apicfgv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// New returns a new manager wrapper. It intercepts the controller when it gets
// added and causes that controller to Watch ImageConfig and Configmap events.
func New(mgr manager.Manager) manager.Manager {
	return &managerWrapper{
		manager: mgr,
	}
}

// managerWrapper is a wrapper around a "real manager". It intercepts the
// Controller when it gets added and causes that controller to Watch
// ImageConfig and Configmap events.
type managerWrapper struct {
	manager manager.Manager
}

// Add causes the controller to Watch for ImageConfig and Configmap events, and then
// calls the wrapped manager's Add function.
func (m *managerWrapper) Add(r manager.Runnable) error {
	err := m.manager.Add(r)
	if err != nil {
		return err
	}

	c, ok := r.(controller.Controller)
	if !ok {
		return fmt.Errorf("Runnable was not a Controller")
	}

	if c == nil {
		return fmt.Errorf("Controller was nil")
	}

	// if Image changes, Reconcile when Image.Spec.Additional.name exists.  Watch for all CM changes, only Reconcile when name == Image.Spec.Additional.name

	// Watch for changes to apicfgv1.Image and reconcile when
	// Image.Spec.Additional.name exists.
	err = c.Watch(&source.Kind{Type: &apicfgv1.Image{}}, &handler.EnqueueRequestsFromMapFunc{ToRequests: &mapper{m.manager.client}})
	if err != nil {
		return err
	}

	// Watch for changes to ConfigMaps in all namespaces and
	// reconcile only when ConfigMap.Name == Image.Spec.Additional.name
	err = c.Watch(&source.Kind{Type: &corev1.ConfigMap{}}, &handler.EnqueueRequestsFromMapFunc{ToRequests: &mapper{}})
	if err != nil {
		return err
	}

	return nil
}

// SetFields will set any dependencies on an object for which the object has implemented the inject
// interface - e.g. inject.Client.
func (m *managerWrapper) SetFields(i interface{}) error {
	return m.manager.SetFields(i)
}

// AddHealthzCheck allows you to add Healthz checker
func (m *managerWrapper) AddHealthzCheck(name string, check healthz.Checker) error {
	return m.manager.AddHealthzCheck(name, check)
}

// AddReadyzCheck allows you to add Readyz checker
func (m *managerWrapper) AddReadyzCheck(name string, check healthz.Checker) error {
	return m.manager.AddReadyzCheck(name, check)
}

// Start starts all registered Controllers and blocks until the Stop channel is closed.
// Returns an error if there is an error starting any controller.
func (m *managerWrapper) Start(c <-chan struct{}) error {
	return m.manager.Start(c)
}

// GetConfig returns an initialized Config
func (m *managerWrapper) GetConfig() *rest.Config {
	return m.manager.GetConfig()
}

// GetScheme returns and initialized Scheme
func (m *managerWrapper) GetScheme() *runtime.Scheme {
	return m.manager.GetScheme()
}

// GetClient returns a client configured with the Config. This client may
// not be a fully "direct" client -- it may read from a cache, for
// instance.  See Options.NewClient for more information on how the default
// implementation works.
func (m *managerWrapper) GetClient() client.Client {
	return m.manager.GetClient()
}

// GetFieldIndexer returns a client.FieldIndexer configured with the client
func (m *managerWrapper) GetFieldIndexer() client.FieldIndexer {
	return m.manager.GetFieldIndexer()
}

// GetCache returns a cache.Cache
func (m *managerWrapper) GetCache() cache.Cache {
	return m.manager.GetCache()
}

// GetEventRecorderFor returns a new EventRecorder for the provided name
func (m *managerWrapper) GetEventRecorderFor(name string) record.EventRecorder {
	return m.manager.GetEventRecorderFor(name)
}

// GetRESTMapper returns a RESTMapper
func (m *managerWrapper) GetRESTMapper() meta.RESTMapper {
	return m.manager.GetRESTMapper()
}

// GetAPIReader returns a reader that will be configured to use the API server.
// This should be used sparingly and only when the client does not fit your
// use case.
func (m *managerWrapper) GetAPIReader() client.Reader {
	return m.manager.GetAPIReader()
}

// GetWebhookServer returns a webhook.Server
func (m *managerWrapper) GetWebhookServer() *webhook.Server {
	return m.manager.GetWebhookServer()
}
