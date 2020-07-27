package cincinnati

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	apicfgv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	cv1beta1 "github.com/openshift/cincinnati-operator/pkg/apis/cincinnati/v1beta1"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

var log = logf.Log.WithName("controller_cincinnati")

// Options holds settings for the reconciler
type Options struct {
	// OperandImage is the full reference to a container image for the operand.
	OperandImage string
}

// Add creates a new Cincinnati Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, options Options) error {
	return add(mgr, newReconciler(mgr, options))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, options Options) reconcile.Reconciler {
	return &ReconcileCincinnati{
		client:       mgr.GetClient(),
		scheme:       mgr.GetScheme(),
		operandImage: options.OperandImage,
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("cincinnati-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Cincinnati
	err = c.Watch(&source.Kind{Type: &cv1beta1.Cincinnati{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resources
	for _, obj := range []runtime.Object{
		&appsv1.Deployment{},
		&corev1.ConfigMap{},
		&corev1.Service{},
		&policyv1beta1.PodDisruptionBudget{},
		&routev1.Route{},
	} {
		err = c.Watch(&source.Kind{Type: obj}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &cv1beta1.Cincinnati{},
		})
		if err != nil {
			return err
		}
	}

	// Watch for all Image changes, only Reconcile when image found is named defaults.ImageConfigName and is at cluster level
	err = c.Watch(&source.Kind{Type: &apicfgv1.Image{}},
		&handler.EnqueueRequestsFromMapFunc{ToRequests: &mapper{mgr.GetClient()}},
		predicate.GenerationChangedPredicate{})
	if err != nil {
		log.Error(err, "Error watching ImageConfig API")
		return err
	}

	//Watch for all ConfigMap changes, only Reconcile when name == Image.Spec.AdditionalTrustedCA.Name
	err = c.Watch(&source.Kind{Type: &corev1.ConfigMap{}},
		&handler.EnqueueRequestsFromMapFunc{ToRequests: &mapper{mgr.GetClient()}},
	)
	if err != nil {
		log.Error(err, "Error watching ConfigMap API")
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileCincinnati implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileCincinnati{}

// ReconcileCincinnati reconciles a Cincinnati object
type ReconcileCincinnati struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client       client.Client
	scheme       *runtime.Scheme
	operandImage string
}

// Reconcile reads that state of the cluster for a Cincinnati object and makes changes based on the state read
// and what is in the Cincinnati.Spec
func (r *ReconcileCincinnati) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	/*    **Reconcile Pattern**
	1. Create all the kubeResources
	2. Make a few modifications to the kubeResources
	3. Regenerate some of the kubeResources
	4. Ensure all the kubeResources are correct in the Cluster
	*/

	ctx := context.TODO()
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Cincinnati")

	// Fetch the Cincinnati instance
	instance := &cv1beta1.Cincinnati{}
	err := r.client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	instanceCopy := instance.DeepCopy()
	instanceCopy.Status = cv1beta1.CincinnatiStatus{}

	// Start construction of resources

	// 1. Create all the kubeResources
	//    'newKubeResources' creates all the kube resources we need and holds
	//    them in 'resources' as the canonical reference for those resources
	//    during reconciliation.
	resources, err := newKubeResources(instanceCopy, r.operandImage)
	if err != nil {
		reqLogger.Error(err, "Failed to render resources")
		return reconcile.Result{}, err
	}

	// 2. Make a few modifications to the kubeResources
	//    The supplemental kube resources changes are modifications that are
	//    applied to resources when certain coditions are met.
	//
	//    Example: A user has an on-premise PKI they want to use with their
	//             registry.  The operator will check for the expected cluster
	//             resources and modify the deployment if conditions are met.
	for _, f := range []func(context.Context, logr.Logger, *cv1beta1.Cincinnati, *kubeResources) error{
		r.postAddPullSecret,
		r.postAddExternalCACert,
	} {
		err = f(ctx, reqLogger, instanceCopy, resources)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// 3. Regenerate some of the kubeResources
	//    After modifiing some of the resources, we need to rebuild them.
	//    Since the larger objects are made up of numerous smaller objects,
	//    rebuild the smaller objects all the way up to the largest object.
	resources.regenerate(instance)

	// End construction of resources

	conditionsv1.SetStatusCondition(&instanceCopy.Status.Conditions, conditionsv1.Condition{
		Type:    cv1beta1.ConditionReconcileCompleted,
		Status:  corev1.ConditionFalse,
		Reason:  "Reconcile started",
		Message: "",
	})

	// 4. Ensure all the kubeResources are correct in the Cluster
	//    The ensure functions will compare the expected resources with the actual
	//    resources and work towards making actual = expected.
	for _, f := range []func(context.Context, logr.Logger, *cv1beta1.Cincinnati, *kubeResources) error{
		r.ensureConfig,
		r.ensurePullSecret,
		r.ensureEnvConfig,
		r.ensureAdditionalTrustedCA,
		r.ensureDeployment,
		r.ensureGraphBuilderService,
		r.ensurePolicyEngineService,
		r.ensurePodDisruptionBudget,
		r.ensurePolicyEngineRoute,
	} {
		err = f(ctx, reqLogger, instanceCopy, resources)
		if err != nil {
			break
		}
	}

	// handle status. Ensure functions should set conditions on the passed-in
	// instance as appropriate but not save. If an ensure function returns an
	// error, it should also set the ReconcileCompleted condition to false with an
	// appropriate message. Otherwise it should set any other conditions as
	// appropriate.
	if err == nil {
		conditionsv1.SetStatusCondition(&instanceCopy.Status.Conditions, conditionsv1.Condition{
			Type:    cv1beta1.ConditionReconcileCompleted,
			Status:  corev1.ConditionTrue,
			Reason:  "Success",
			Message: "",
		})
	}

	if err := r.client.Status().Update(ctx, instanceCopy); err != nil {
		reqLogger.Error(err, "Failed to update Status")
	}

	return reconcile.Result{}, err
}

// handleErr logs the error and sets an appropriate Condition on the status.
func handleErr(reqLogger logr.Logger, status *cv1beta1.CincinnatiStatus, reason string, e error) {
	conditionsv1.SetStatusCondition(&status.Conditions, conditionsv1.Condition{
		Type:    cv1beta1.ConditionReconcileCompleted,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: e.Error(),
	})
	reqLogger.Error(e, reason)
}

// handleCACertStatus logs the message and sets an appropriate Condition on the ConditionRegistryCACertFound status.
func handleCACertStatus(reqLogger logr.Logger, status *cv1beta1.CincinnatiStatus, reason string, message string) {
	conditionsv1.SetStatusCondition(&status.Conditions, conditionsv1.Condition{
		Type:    cv1beta1.ConditionRegistryCACertFound,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	reqLogger.Info(message)
}

func (r *ReconcileCincinnati) postAddPullSecret(ctx context.Context, reqLogger logr.Logger, instance *cv1beta1.Cincinnati, resources *kubeResources) error {
	sourcePS := &corev1.Secret{}
	err := r.client.Get(ctx, types.NamespacedName{Name: namePullSecret, Namespace: openshiftConfigNamespace}, sourcePS)
	if err != nil && errors.IsNotFound(err) {
		handleErr(reqLogger, &instance.Status, "PullSecretNotFound", err)
		return err
	} else if err != nil {
		return err
	}
	resources.addPullSecret(instance, sourcePS)

	return nil
}

func (r *ReconcileCincinnati) postAddExternalCACert(ctx context.Context, reqLogger logr.Logger, instance *cv1beta1.Cincinnati, resources *kubeResources) error {
	// Search for the the pull-secret in openshift-config
	sourceCM, err := r.findTrustedCAConfig(ctx, reqLogger, instance, resources)
	if err != nil {
		return err
	} else if sourceCM == nil {
		return nil
	}

	resources.addExternalCACert(instance, sourceCM)

	return nil
}

// findTrustedCAConfig - Locate the ConfigMap referenced by the ImageConfig resource in openshift-config and return it
func (r *ReconcileCincinnati) findTrustedCAConfig(ctx context.Context, reqLogger logr.Logger, instance *cv1beta1.Cincinnati, resources *kubeResources) (*corev1.ConfigMap, error) {

	// Check if the Cluster is aware of a registry requiring an
	// AdditionalTrustedCA
	image := &apicfgv1.Image{}
	err := r.client.Get(ctx, types.NamespacedName{Name: defaults.ImageConfigName, Namespace: ""}, image)
	if err != nil && errors.IsNotFound(err) {
		m := fmt.Sprintf("image.config.openshift.io not found for (Name: %v, Namespace: %v)", defaults.ImageConfigName, "")
		handleCACertStatus(reqLogger, &instance.Status, "FindAdditionalTrustedCAFailed", m)
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	if image.Spec.AdditionalTrustedCA.Name == "" {
		m := fmt.Sprintf("image.config.openshift.io.Spec.AdditionalTrustedCA.Name not found for image (Name: %v, Namespace: %v)", defaults.ImageConfigName, "")
		handleCACertStatus(reqLogger, &instance.Status, "FindAdditionalTrustedCAFailed", m)
		return nil, nil
	}

	// Search for the ConfigMap in openshift-config
	sourceCM := &corev1.ConfigMap{}
	err = r.client.Get(ctx, types.NamespacedName{Name: image.Spec.AdditionalTrustedCA.Name, Namespace: openshiftConfigNamespace}, sourceCM)
	if err != nil && errors.IsNotFound(err) {
		m := fmt.Sprintf("Found image.config.openshift.io.Spec.AdditionalTrustedCA.Name but did not find expected ConfigMap (Name: %v, Namespace: %v)", image.Spec.AdditionalTrustedCA.Name, openshiftConfigNamespace)
		handleCACertStatus(reqLogger, &instance.Status, "FindAdditionalTrustedCAFailed", m)
		return nil, err
	} else if err != nil {
		return nil, err
	}

	if _, ok := sourceCM.Data[NameCertConfigMapKey]; !ok {
		m := fmt.Sprintf("Found ConfigMap referenced by ImageConfig.Spec.AdditionalTrustedCA.Name but did not find key 'cincinnati-registry' for registry CA cert in ConfigMap (Name: %v, Namespace: %v)", image.Spec.AdditionalTrustedCA.Name, openshiftConfigNamespace)
		handleCACertStatus(reqLogger, &instance.Status, "EnsureAdditionalTrustedCAFailed", m)
		return nil, nil
	}

	return sourceCM, nil
}

func (r *ReconcileCincinnati) ensurePullSecret(ctx context.Context, reqLogger logr.Logger, instance *cv1beta1.Cincinnati, resources *kubeResources) error {
	// Set Cincinnati instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, resources.pullSecret, r.scheme); err != nil {
		return err
	}

	if err := r.ensureSecret(ctx, reqLogger, resources.pullSecret); err != nil {
		handleErr(reqLogger, &instance.Status, "EnsureSecretFailed", err)
		return err
	}

	return nil
}

func (r *ReconcileCincinnati) ensureAdditionalTrustedCA(ctx context.Context, reqLogger logr.Logger, instance *cv1beta1.Cincinnati, resources *kubeResources) error {
	sourceCM, err := r.findTrustedCAConfig(ctx, reqLogger, instance, resources)
	if err != nil {
		return err
	} else if sourceCM == nil {
		return nil
	}

	conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
		Type:    cv1beta1.ConditionRegistryCACertFound,
		Status:  corev1.ConditionTrue,
		Reason:  "CACertFound",
		Message: "",
	})

	// Set Cincinnati instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, resources.trustedCAConfig, r.scheme); err != nil {
		return err
	}

	if err := r.ensureConfigMap(ctx, reqLogger, resources.trustedCAConfig); err != nil {
		handleErr(reqLogger, &instance.Status, "EnsureConfigMapFailed", err)
		return err
	}
	return nil
}

func (r *ReconcileCincinnati) ensureDeployment(ctx context.Context, reqLogger logr.Logger, instance *cv1beta1.Cincinnati, resources *kubeResources) error {
	deployment := resources.deployment
	if err := controllerutil.SetControllerReference(instance, deployment, r.scheme); err != nil {
		return err
	}
	// Check if this deployment already exists
	found := &appsv1.Deployment{}
	err := r.client.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, found)

	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating Deployment", "Namespace", deployment.Namespace, "Name", deployment.Name)
		err := r.client.Create(ctx, deployment)
		if err != nil {
			handleErr(reqLogger, &instance.Status, "CreateDeploymentFailed", err)
		}
		return err
	} else if err != nil {
		handleErr(reqLogger, &instance.Status, "GetDeploymentFailed", err)
		return err
	}

	// found existing deployment; let's compare and update if needed. We'll make
	// a copy of what we found, overwrite values with those that were just now
	// generated from the spec, and compare the result with what was found.
	updated := found.DeepCopy()
	updated.Spec.Replicas = deployment.Spec.Replicas
	updated.Spec.Selector = deployment.Spec.Selector
	updated.Spec.Strategy = deployment.Spec.Strategy

	// apply labels and annotations
	if updated.Spec.Template.ObjectMeta.Labels == nil {
		updated.Spec.Template.ObjectMeta.Labels = map[string]string{}
	}
	for key, value := range deployment.Spec.Template.ObjectMeta.Labels {
		updated.Spec.Template.ObjectMeta.Labels[key] = value
	}
	if updated.Spec.Template.ObjectMeta.Annotations == nil {
		updated.Spec.Template.ObjectMeta.Annotations = map[string]string{}
	}
	for key, value := range deployment.Spec.Template.ObjectMeta.Annotations {
		updated.Spec.Template.ObjectMeta.Annotations[key] = value
	}

	updated.Spec.Template.Spec.Volumes = deployment.Spec.Template.Spec.Volumes
	containers := updated.Spec.Template.Spec.Containers
	for i := range containers {
		var original *corev1.Container
		switch containers[i].Name {
		case NameContainerGraphBuilder:
			original = resources.graphBuilderContainer
		case NameContainerPolicyEngine:
			original = resources.policyEngineContainer
		default:
			reqLogger.Info("encountered unexpected container in pod", "Container.Name", containers[i].Name)
			continue
		}
		containers[i].Image = original.Image
		containers[i].ImagePullPolicy = original.ImagePullPolicy
		containers[i].Command = original.Command
		containers[i].Args = original.Args
		containers[i].Ports = original.Ports
		containers[i].Env = original.Env

		// Resources don't seem to work with DeepEqual, so we check equality
		// manually.
		if containers[i].Resources.Limits == nil {
			containers[i].Resources.Limits = corev1.ResourceList{}
		}
		for k, origVal := range original.Resources.Limits {
			value, ok := containers[i].Resources.Limits[k]
			if !ok || !origVal.Equal(value) {
				containers[i].Resources.Limits[k] = origVal
			}
		}
		if containers[i].Resources.Requests == nil {
			containers[i].Resources.Requests = corev1.ResourceList{}
		}
		for k, origVal := range original.Resources.Requests {
			value, ok := containers[i].Resources.Requests[k]
			if !ok || !origVal.Equal(value) {
				containers[i].Resources.Requests[k] = origVal
			}
		}
		containers[i].VolumeMounts = original.VolumeMounts
		containers[i].LivenessProbe = original.LivenessProbe
		containers[i].ReadinessProbe = original.ReadinessProbe
	}

	if !reflect.DeepEqual(updated.Spec, found.Spec) {
		reqLogger.Info("Updating Deployment", "Namespace", deployment.Namespace, "Name", deployment.Name)
		err = r.client.Update(ctx, updated)
		if err != nil {
			handleErr(reqLogger, &instance.Status, "UpdateDeploymentFailed", err)
			return err
		}
	}

	return nil
}

func (r *ReconcileCincinnati) ensurePodDisruptionBudget(ctx context.Context, reqLogger logr.Logger, instance *cv1beta1.Cincinnati, resources *kubeResources) error {
	pdb := resources.podDisruptionBudget
	// Set Cincinnati instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, pdb, r.scheme); err != nil {
		return err
	}

	// Check if it already exists
	found := &policyv1beta1.PodDisruptionBudget{}
	err := r.client.Get(ctx, types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating PodDisruptionBudget", "Namespace", pdb.Namespace, "Name", pdb.Name)
		if err = r.client.Create(ctx, pdb); err != nil {
			handleErr(reqLogger, &instance.Status, "CreatePDBFailed", err)
		}
		return err
	} else if err != nil {
		handleErr(reqLogger, &instance.Status, "GetPDBFailed", err)
		return err
	}

	// found existing resource; let's compare and update if needed
	if !reflect.DeepEqual(found.Spec, pdb.Spec) {
		reqLogger.Info("Updating PodDisruptionBudget", "Namespace", pdb.Namespace, "Name", pdb.Name)
		updated := found.DeepCopy()
		updated.Spec = pdb.Spec
		err = r.client.Update(ctx, updated)
		if err != nil {
			handleErr(reqLogger, &instance.Status, "UpdatePDBFailed", err)
		}
	}

	return nil
}

func (r *ReconcileCincinnati) ensureConfig(ctx context.Context, reqLogger logr.Logger, instance *cv1beta1.Cincinnati, resources *kubeResources) error {
	config := resources.graphBuilderConfig
	// Set Cincinnati instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, config, r.scheme); err != nil {
		return err
	}

	if err := r.ensureConfigMap(ctx, reqLogger, config); err != nil {
		handleErr(reqLogger, &instance.Status, "EnsureConfigMapFailed", err)
		return err
	}
	return nil
}

func (r *ReconcileCincinnati) ensureEnvConfig(ctx context.Context, reqLogger logr.Logger, instance *cv1beta1.Cincinnati, resources *kubeResources) error {
	config := resources.envConfig
	// Set Cincinnati instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, config, r.scheme); err != nil {
		return err
	}

	if err := r.ensureConfigMap(ctx, reqLogger, config); err != nil {
		handleErr(reqLogger, &instance.Status, "EnsureConfigMapFailed", err)
		return err
	}
	return nil
}

func (r *ReconcileCincinnati) ensureGraphBuilderService(ctx context.Context, reqLogger logr.Logger, instance *cv1beta1.Cincinnati, resources *kubeResources) error {
	service := resources.graphBuilderService
	// Set Cincinnati instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, service, r.scheme); err != nil {
		return err
	}

	if err := r.ensureService(ctx, reqLogger, service); err != nil {
		handleErr(reqLogger, &instance.Status, "EnsureServiceFailed", err)
		return err
	}
	return nil
}

func (r *ReconcileCincinnati) ensurePolicyEngineService(ctx context.Context, reqLogger logr.Logger, instance *cv1beta1.Cincinnati, resources *kubeResources) error {
	service := resources.policyEngineService
	// Set Cincinnati instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, service, r.scheme); err != nil {
		return err
	}

	if err := r.ensureService(ctx, reqLogger, service); err != nil {
		handleErr(reqLogger, &instance.Status, "EnsureServiceFailed", err)
		return err
	}
	return nil
}

func (r *ReconcileCincinnati) ensurePolicyEngineRoute(ctx context.Context, reqLogger logr.Logger, instance *cv1beta1.Cincinnati, resources *kubeResources) error {
	route := resources.policyEngineRoute
	// Set Cincinnati instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, route, r.scheme); err != nil {
		return err
	}

	// Check if it already exists
	found := &routev1.Route{}
	err := r.client.Get(ctx, types.NamespacedName{Name: route.Name, Namespace: route.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating Route", "Namespace", route.Namespace, "Name", route.Name)
		if err = r.client.Create(ctx, route); err != nil {
			handleErr(reqLogger, &instance.Status, "CreateRouteFailed", err)
		}
		return err
	} else if err != nil {
		handleErr(reqLogger, &instance.Status, "GetRouteFailed", err)
		return err
	}

	updated := found.DeepCopy()
	// Keep found tls for later use
	tls := updated.Spec.TLS
	// This is just so we compare the Spec on the two objects but make an exception for Spec.TLS
	updated.Spec.TLS = route.Spec.TLS

	// found existing resource; let's compare and update if needed
	if !reflect.DeepEqual(updated.Spec, route.Spec) {
		reqLogger.Info("Updating Route", "Namespace", route.Namespace, "Name", route.Name)
		updated.Spec = route.Spec
		// We want to allow user to update the TLS cert/key manually on the route and we don't want to override that change.
		// Keep the existing tls on the route
		updated.Spec.TLS = tls
		err = r.client.Update(ctx, updated)
		if err != nil {
			handleErr(reqLogger, &instance.Status, "UpdateRouteFailed", err)
		}
	}

	return nil
}

func (r *ReconcileCincinnati) ensureService(ctx context.Context, reqLogger logr.Logger, service *corev1.Service) error {
	// Check if this Service already exists
	found := &corev1.Service{}
	err := r.client.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating Service", "Namespace", service.Namespace, "Name", service.Name)
		return r.client.Create(ctx, service)
	} else if err != nil {
		return err
	}

	// found existing configmap; let's compare and update if needed
	// ClusterIP gets set externally, so we need to set it before comparing
	service.Spec.ClusterIP = found.Spec.ClusterIP
	if !reflect.DeepEqual(found.Spec, service.Spec) {
		reqLogger.Info("Updating Service", "Namespace", service.Namespace, "Name", service.Name)
		updated := found.DeepCopy()
		updated.Spec = service.Spec
		return r.client.Update(ctx, updated)
	}

	return nil
}

func (r *ReconcileCincinnati) ensureConfigMap(ctx context.Context, reqLogger logr.Logger, cm *corev1.ConfigMap) error {
	// Check if this configmap already exists
	found := &corev1.ConfigMap{}
	err := r.client.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating ConfigMap", "Namespace", cm.Namespace, "Name", cm.Name)
		return r.client.Create(ctx, cm)
	} else if err != nil {
		return err
	}

	// found existing configmap; let's compare and update if needed
	if !reflect.DeepEqual(found.Data, cm.Data) {
		reqLogger.Info("Updating ConfigMap", "Namespace", cm.Namespace, "Name", cm.Name)
		updated := found.DeepCopy()
		updated.Data = cm.Data
		return r.client.Update(ctx, updated)
	}

	return nil
}

func (r *ReconcileCincinnati) ensureSecret(ctx context.Context, reqLogger logr.Logger, secret *corev1.Secret) error {
	// Check if this secret already exists
	found := &corev1.Secret{}
	err := r.client.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating Secret", "Namespace", secret.Namespace, "Name", secret.Name)
		return r.client.Create(ctx, secret)
	} else if err != nil {
		return err
	}

	// found existing secret; let's compare and update if needed
	if !reflect.DeepEqual(found.Data, secret.Data) {
		reqLogger.Info("Updating Secret", "Namespace", secret.Namespace, "Name", secret.Name)
		updated := found.DeepCopy()
		updated.Data = secret.Data
		return r.client.Update(ctx, updated)
	}

	return nil
}
