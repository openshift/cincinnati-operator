package cincinnati

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	cv1alpha1 "github.com/openshift/cincinnati-operator/pkg/apis/cincinnati/v1alpha1"
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
	err = c.Watch(&source.Kind{Type: &cv1alpha1.Cincinnati{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resources
	for _, obj := range []runtime.Object{
		&appsv1.Deployment{},
		&corev1.ConfigMap{},
		&corev1.Service{},
		&policyv1beta1.PodDisruptionBudget{},
	} {
		err = c.Watch(&source.Kind{Type: obj}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &cv1alpha1.Cincinnati{},
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
		predicate.GenerationChangedPredicate{})
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
	ctx := context.TODO()
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Cincinnati")

	// Fetch the Cincinnati instance
	instance := &cv1alpha1.Cincinnati{}
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
	instanceCopy.Status = cv1alpha1.CincinnatiStatus{}

	// this object creates all the kube resources we need and then holds them as
	// the canonical reference for those resources during reconciliation.
	resources, err := newKubeResources(instanceCopy, r.operandImage)
	if err != nil {
		reqLogger.Error(err, "Failed to render resources")
		return reconcile.Result{}, err
	}

	for _, f := range []func(context.Context, logr.Logger, *cv1alpha1.Cincinnati, *kubeResources) error{
		r.ensureConfig,
		r.ensureEnvConfig,
		r.ensureAdditionalTrustedCA,
		r.ensureDeployment,
		r.ensureGraphBuilderService,
		r.ensurePolicyEngineService,
		r.ensurePodDisruptionBudget,
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
			Type:    cv1alpha1.ConditionReconcileCompleted,
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
func handleErr(reqLogger logr.Logger, status *cv1alpha1.CincinnatiStatus, reason string, e error) {
	conditionsv1.SetStatusCondition(&status.Conditions, conditionsv1.Condition{
		Type:    cv1alpha1.ConditionReconcileCompleted,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: e.Error(),
	})
	reqLogger.Error(e, reason)
}

func (r *ReconcileCincinnati) ensureAdditionalTrustedCA(ctx context.Context, reqLogger logr.Logger, instance *cv1alpha1.Cincinnati, resources *kubeResources) error {
	// Check if the Cluster is aware of a registry requiring an
	// AdditionalTrustedCA
	image := &apicfgv1.Image{}
	err := r.client.Get(ctx, types.NamespacedName{Name: defaults.ImageConfigName, Namespace: ""}, image)
	if err != nil && errors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	if image.Spec.AdditionalTrustedCA.Name == "" {
		return nil
	}

	// Search for the ConfigMap in openshift-config
	sourceCM := &corev1.ConfigMap{}
	err = r.client.Get(ctx, types.NamespacedName{Name: image.Spec.AdditionalTrustedCA.Name, Namespace: openshiftConfigNamespace}, sourceCM)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Found image.config.openshift.io.Spec.AdditionalTrustedCA.Name but did not find expected ConfigMap", "Name", image.Spec.AdditionalTrustedCA.Name, "Namespace", openshiftConfigNamespace)
		return err
	} else if err != nil {
		return err
	}

	if _, ok := sourceCM.Data[NameCertConfigMapKey]; ok {
		localCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nameAdditionalTrustedCA(instance),
				Namespace: instance.Namespace,
			},
			Data: sourceCM.Data,
		}

		// Set Cincinnati instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, localCM, r.scheme); err != nil {
			return err
		}

		if err := r.ensureConfigMap(ctx, reqLogger, localCM); err != nil {
			handleErr(reqLogger, &instance.Status, "EnsureConfigMapFailed", err)
			return err
		}
		// Mount in ConfigMap data from the cincinnati-registry key
		externalCACert := true
		resources.graphBuilderContainer = resources.newGraphBuilderContainer(instance, r.operandImage, externalCACert)
		resources.deployment = resources.newDeployment(instance, externalCACert)
	} else {
		reqLogger.Info("Found ConfigMap referenced by ImageConfig.Spec.AdditionalTrustedCA.Name but did not find key 'cincinnati-registry' for registry CA cert.", "Name", image.Spec.AdditionalTrustedCA.Name, "Namespace", openshiftConfigNamespace)
	}
	return nil
}

func (r *ReconcileCincinnati) ensureDeployment(ctx context.Context, reqLogger logr.Logger, instance *cv1alpha1.Cincinnati, resources *kubeResources) error {
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

func (r *ReconcileCincinnati) ensurePodDisruptionBudget(ctx context.Context, reqLogger logr.Logger, instance *cv1alpha1.Cincinnati, resources *kubeResources) error {
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

func (r *ReconcileCincinnati) ensureConfig(ctx context.Context, reqLogger logr.Logger, instance *cv1alpha1.Cincinnati, resources *kubeResources) error {
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

func (r *ReconcileCincinnati) ensureEnvConfig(ctx context.Context, reqLogger logr.Logger, instance *cv1alpha1.Cincinnati, resources *kubeResources) error {
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

func (r *ReconcileCincinnati) ensureGraphBuilderService(ctx context.Context, reqLogger logr.Logger, instance *cv1alpha1.Cincinnati, resources *kubeResources) error {
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

func (r *ReconcileCincinnati) ensurePolicyEngineService(ctx context.Context, reqLogger logr.Logger, instance *cv1alpha1.Cincinnati, resources *kubeResources) error {
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
