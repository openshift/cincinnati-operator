package controllers

import (
	"context"
	"fmt"
	"reflect"
	"regexp"

	"github.com/go-logr/logr"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	apicfgv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	cv1 "github.com/openshift/cincinnati-operator/api/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/library-go/pkg/route/routeapihelpers"
)

const dns1123LabelFmt string = "^[a-z]([-a-z0-9]*[a-z0-9])?$"

// DNS1123LabelMaxLength is a label's max length in DNS (RFC 1123)
const DNS1123LabelMaxLength int = 63

var dns1123LabelRegexp = regexp.MustCompile(dns1123LabelFmt)

var log = logf.Log.WithName("controller_updateservice")

// blank assignment to verify that ReconcileUpdateService implements reconcile.Reconciler
var _ reconcile.Reconciler = &UpdateServiceReconciler{}

// UpdateServiceReconciler reconciles a UpdateService object
type UpdateServiceReconciler struct {
	Client       client.Client
	Scheme       *runtime.Scheme
	Log          logr.Logger
	OperandImage string
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=get
// +kubebuilder:rbac:groups="",resources=pods;services;services/finalizers;endpoints;persistentvolumeclaims;events;configmaps;secrets,verbs=create;delete;get;list;patch;update;watch
// +kubebuilder:rbac:groups="apps",resourceNames=updateservice-operator,resources=deployments/finalizers,verbs=update
// +kubebuilder:rbac:groups="apps",resources=deployments;daemonsets;replicasets;statefulsets,verbs=create;delete;get;list;patch;update;watch
// +kubebuilder:rbac:groups="apps",resources=replicasets;deployments,verbs=get
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=servicemonitors,verbs=create;get
// +kubebuilder:rbac:groups="policy",resources=poddisruptionbudgets,verbs=create;delete;get;list;patch;update;watch
// +kubebuilder:rbac:groups=config.openshift.io,resources=images,verbs=get;list;watch
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=create;get;list;patch;update;watch
// +kubebuilder:rbac:groups=updateservice.operator.openshift.io,resources=*,verbs=create;delete;get;list;patch;update;watch

func (r *UpdateServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	/*    **Reconcile Pattern**
	      1. Gather conditions
	      2. Create all the kubeResources
	      3. Ensure all the kubeResources are correct in the Cluster
	*/

	reqLogger := log.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	reqLogger.Info("Reconciling UpdateService")

	// Fetch the UpdateService instance
	instance := &cv1.UpdateService{}
	err := r.Client.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	instanceCopy := instance.DeepCopy()
	instanceCopy.Status = cv1.UpdateServiceStatus{}

	if err := validateRouteName(instanceCopy, req.Name, req.Namespace); err != nil {
		conditionsv1.SetStatusCondition(&instanceCopy.Status.Conditions, conditionsv1.Condition{
			Type:    cv1.ConditionReconcileError,
			Status:  corev1.ConditionTrue,
			Reason:  "Unable to create UpdateService route",
			Message: err.Error(),
		})
		if err := r.Client.Status().Update(ctx, instanceCopy); err != nil {
			reqLogger.Error(err, "Failed to update Status")
		}
		reqLogger.Error(err, "Unable to create UpdateService route")
		return ctrl.Result{}, nil
	}

	// 1. Gather conditions
	//    Look at the existing cluster resources and communicate to kubeResources
	//    how it should create resources.
	//
	//    Example: A user has an on-premise PKI they want to use with their
	//             registry.  The operator will check for the expected cluster
	//             resources and inform kubeResources how the deployment will look.
	ps, err := r.findPullSecret(ctx, reqLogger, instanceCopy)
	if err != nil {
		return ctrl.Result{}, err
	}

	cm, err := r.findTrustedCAConfig(ctx, reqLogger, instanceCopy)
	if err != nil {
		return ctrl.Result{}, err
	}

	// 2. Create all the kubeResources
	//    'newKubeResources' creates all the kube resources we need and holds
	//    them in 'resources' as the canonical reference for those resources
	//    during reconciliation.
	resources, err := newKubeResources(instanceCopy, r.OperandImage, ps, cm)
	if err != nil {
		reqLogger.Error(err, "Failed to render resources")
		return ctrl.Result{}, err
	}

	conditionsv1.SetStatusCondition(&instanceCopy.Status.Conditions, conditionsv1.Condition{
		Type:    cv1.ConditionReconcileCompleted,
		Status:  corev1.ConditionFalse,
		Reason:  "Reconcile started",
		Message: "",
	})

	// 3. Ensure all the kubeResources are correct in the Cluster
	//    The ensure functions will compare the expected resources with the actual
	//    resources and work towards making actual = expected.
	for _, f := range []func(context.Context, logr.Logger, *cv1.UpdateService, *kubeResources) error{
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
			Type:    cv1.ConditionReconcileCompleted,
			Status:  corev1.ConditionTrue,
			Reason:  "Success",
			Message: "",
		})
	}

	if err := r.Client.Status().Update(ctx, instanceCopy); err != nil {
		reqLogger.Error(err, "Failed to update Status")
	}

	return ctrl.Result{}, err
}

// handleErr logs the error and sets an appropriate Condition on the status.
func handleErr(reqLogger logr.Logger, status *cv1.UpdateServiceStatus, reason string, e error) {
	conditionsv1.SetStatusCondition(&status.Conditions, conditionsv1.Condition{
		Type:    cv1.ConditionReconcileCompleted,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: e.Error(),
	})
	reqLogger.Error(e, reason)
}

// handleCACertStatus logs the message and sets an appropriate Condition on the ConditionRegistryCACertFound status.
func handleCACertStatus(reqLogger logr.Logger, status *cv1.UpdateServiceStatus, reason string, message string) {
	conditionsv1.SetStatusCondition(&status.Conditions, conditionsv1.Condition{
		Type:    cv1.ConditionRegistryCACertFound,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	reqLogger.Info(message)
}

// findPullSecet - Locate the PullSecrt in openshift-config and return it
func (r *UpdateServiceReconciler) findPullSecret(ctx context.Context, reqLogger logr.Logger, instance *cv1.UpdateService) (*corev1.Secret, error) {
	sourcePS := &corev1.Secret{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: namePullSecret, Namespace: openshiftConfigNamespace}, sourcePS)
	if err != nil && errors.IsNotFound(err) {
		handleErr(reqLogger, &instance.Status, "PullSecretNotFound", err)
		return nil, err
	} else if err != nil {
		return nil, err
	}
	return sourcePS, nil
}

// findTrustedCAConfig - Locate the ConfigMap referenced by the ImageConfig resource in openshift-config and return it
func (r *UpdateServiceReconciler) findTrustedCAConfig(ctx context.Context, reqLogger logr.Logger, instance *cv1.UpdateService) (*corev1.ConfigMap, error) {

	// Check if the Cluster is aware of a registry requiring an
	// AdditionalTrustedCA
	image := &apicfgv1.Image{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: defaults.ImageConfigName}, image)
	if err != nil && errors.IsNotFound(err) {
		m := fmt.Sprintf("image.config.openshift.io not found for name %s", defaults.ImageConfigName)
		handleCACertStatus(reqLogger, &instance.Status, "FindAdditionalTrustedCAFailed", m)
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	if image.Spec.AdditionalTrustedCA.Name == "" {
		m := fmt.Sprintf("image.config.openshift.io.Spec.AdditionalTrustedCA.Name not set for image name %s", defaults.ImageConfigName)
		handleCACertStatus(reqLogger, &instance.Status, "NotConfigured", m)
		return nil, nil
	}

	// Search for the ConfigMap in openshift-config
	sourceCM := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: image.Spec.AdditionalTrustedCA.Name, Namespace: openshiftConfigNamespace}, sourceCM)
	if err != nil && errors.IsNotFound(err) {
		m := fmt.Sprintf("Found image.config.openshift.io.Spec.AdditionalTrustedCA.Name but did not find expected ConfigMap (Name: %v, Namespace: %v)", image.Spec.AdditionalTrustedCA.Name, openshiftConfigNamespace)
		handleCACertStatus(reqLogger, &instance.Status, "FindAdditionalTrustedCAFailed", m)
		return nil, err
	} else if err != nil {
		return nil, err
	}

	if _, ok := sourceCM.Data[NameCertConfigMapKey]; !ok {
		m := fmt.Sprintf("Found ConfigMap referenced by ImageConfig.Spec.AdditionalTrustedCA.Name but did not find key 'updateservice-registry' for registry CA cert in ConfigMap (Name: %v, Namespace: %v)", image.Spec.AdditionalTrustedCA.Name, openshiftConfigNamespace)
		handleCACertStatus(reqLogger, &instance.Status, "EnsureAdditionalTrustedCAFailed", m)
		return nil, nil
	}

	return sourceCM, nil
}

func (r *UpdateServiceReconciler) ensurePullSecret(ctx context.Context, reqLogger logr.Logger, instance *cv1.UpdateService, resources *kubeResources) error {
	if resources.pullSecret == nil {
		return nil
	}

	// Set UpdateService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, resources.pullSecret, r.Scheme); err != nil {
		return err
	}

	if err := r.ensureSecret(ctx, reqLogger, resources.pullSecret); err != nil {
		handleErr(reqLogger, &instance.Status, "EnsureSecretFailed", err)
		return err
	}

	return nil
}

func (r *UpdateServiceReconciler) ensureAdditionalTrustedCA(ctx context.Context, reqLogger logr.Logger, instance *cv1.UpdateService, resources *kubeResources) error {
	// Found ConfigMap referenced by ImageConfig.Spec.AdditionalTrustedCA.Name
	// but did not find key 'updateservice-registry' for registry CA cert in ConfigMap
	if resources.trustedCAConfig == nil {
		return nil
	}

	conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
		Type:    cv1.ConditionRegistryCACertFound,
		Status:  corev1.ConditionTrue,
		Reason:  "CACertFound",
		Message: "",
	})

	// Set UpdateService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, resources.trustedCAConfig, r.Scheme); err != nil {
		return err
	}

	if err := r.ensureConfigMap(ctx, reqLogger, resources.trustedCAConfig); err != nil {
		handleErr(reqLogger, &instance.Status, "EnsureConfigMapFailed", err)
		return err
	}
	return nil
}

func (r *UpdateServiceReconciler) ensureDeployment(ctx context.Context, reqLogger logr.Logger, instance *cv1.UpdateService, resources *kubeResources) error {
	deployment := resources.deployment
	if err := controllerutil.SetControllerReference(instance, deployment, r.Scheme); err != nil {
		return err
	}
	// Check if this deployment already exists
	found := &appsv1.Deployment{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, found)

	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating Deployment", "Namespace", deployment.Namespace, "Name", deployment.Name)
		err := r.Client.Create(ctx, deployment)
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
		err = r.Client.Update(ctx, updated)
		if err != nil {
			handleErr(reqLogger, &instance.Status, "UpdateDeploymentFailed", err)
			return err
		}
	}

	return nil
}

func (r *UpdateServiceReconciler) ensurePodDisruptionBudget(ctx context.Context, reqLogger logr.Logger, instance *cv1.UpdateService, resources *kubeResources) error {
	pdb := resources.podDisruptionBudget
	// Set UpdateService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, pdb, r.Scheme); err != nil {
		return err
	}

	// Check if it already exists
	found := &policyv1beta1.PodDisruptionBudget{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating PodDisruptionBudget", "Namespace", pdb.Namespace, "Name", pdb.Name)
		if err = r.Client.Create(ctx, pdb); err != nil {
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
		err = r.Client.Update(ctx, updated)
		if err != nil {
			handleErr(reqLogger, &instance.Status, "UpdatePDBFailed", err)
		}
	}

	return nil
}

func (r *UpdateServiceReconciler) ensureConfig(ctx context.Context, reqLogger logr.Logger, instance *cv1.UpdateService, resources *kubeResources) error {
	config := resources.graphBuilderConfig
	// Set UpdateService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, config, r.Scheme); err != nil {
		return err
	}

	if err := r.ensureConfigMap(ctx, reqLogger, config); err != nil {
		handleErr(reqLogger, &instance.Status, "EnsureConfigMapFailed", err)
		return err
	}
	return nil
}

func (r *UpdateServiceReconciler) ensureEnvConfig(ctx context.Context, reqLogger logr.Logger, instance *cv1.UpdateService, resources *kubeResources) error {
	config := resources.envConfig
	// Set UpdateService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, config, r.Scheme); err != nil {
		return err
	}

	if err := r.ensureConfigMap(ctx, reqLogger, config); err != nil {
		handleErr(reqLogger, &instance.Status, "EnsureConfigMapFailed", err)
		return err
	}
	return nil
}

func (r *UpdateServiceReconciler) ensureGraphBuilderService(ctx context.Context, reqLogger logr.Logger, instance *cv1.UpdateService, resources *kubeResources) error {
	service := resources.graphBuilderService
	// Set UpdateService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, service, r.Scheme); err != nil {
		return err
	}

	if err := r.ensureService(ctx, reqLogger, service); err != nil {
		handleErr(reqLogger, &instance.Status, "EnsureServiceFailed", err)
		return err
	}
	return nil
}

func (r *UpdateServiceReconciler) ensurePolicyEngineService(ctx context.Context, reqLogger logr.Logger, instance *cv1.UpdateService, resources *kubeResources) error {
	service := resources.policyEngineService
	// Set UpdateService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, service, r.Scheme); err != nil {
		return err
	}

	if err := r.ensureService(ctx, reqLogger, service); err != nil {
		handleErr(reqLogger, &instance.Status, "EnsureServiceFailed", err)
		return err
	}
	return nil
}

func validateRouteName(instance *cv1.UpdateService, name string, namespace string) error {
	var errReasons []string
	routeName := namePolicyEngineRoute(instance) + "-" + namespace

	if len(routeName) > DNS1123LabelMaxLength {
		errReasons = append(errReasons,
			fmt.Sprintf("cannot exceed RFC 1123 maximum length of %d. Shorten the application name and/or namespace.",
				DNS1123LabelMaxLength))
	}
	if !dns1123LabelRegexp.MatchString(routeName) {
		errReasons = append(errReasons,
			fmt.Sprintf("has invalid format; must comply with %q.", dns1123LabelFmt))
	}
	numErrors := len(errReasons)
	if numErrors == 0 {
		return nil
	} else if numErrors == 1 {
		return fmt.Errorf(fmt.Sprintf("UpdateService route name %q %s", routeName, errReasons[0]))
	}
	return fmt.Errorf(fmt.Sprintf("UpdateService route name %q %s Route name %s", routeName, errReasons[0], errReasons[1]))
}

func (r *UpdateServiceReconciler) ensurePolicyEngineRoute(ctx context.Context, reqLogger logr.Logger, instance *cv1.UpdateService, resources *kubeResources) error {
	route := resources.policyEngineRoute
	// Set UpdateService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, route, r.Scheme); err != nil {
		return err
	}

	// Check if it already exists
	found := &routev1.Route{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: route.Name, Namespace: route.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating Route", "Namespace", route.Namespace, "Name", route.Name)
		if err = r.Client.Create(ctx, route); err != nil {
			handleErr(reqLogger, &instance.Status, "CreateRouteFailed", err)
		}
		return err
	} else if err != nil {
		handleErr(reqLogger, &instance.Status, "GetRouteFailed", err)
		return err
	}

	if uri, _, err := routeapihelpers.IngressURI(found, ""); err == nil {
		instance.Status.PolicyEngineURI = uri.String()
	} else {
		handleErr(reqLogger, &instance.Status, "RouteIngressFailed", err)
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
		err = r.Client.Update(ctx, updated)
		if err != nil {
			handleErr(reqLogger, &instance.Status, "UpdateRouteFailed", err)
		}
	}

	return nil
}

func (r *UpdateServiceReconciler) ensureService(ctx context.Context, reqLogger logr.Logger, service *corev1.Service) error {
	// Check if this Service already exists
	found := &corev1.Service{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating Service", "Namespace", service.Namespace, "Name", service.Name)
		return r.Client.Create(ctx, service)
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
		return r.Client.Update(ctx, updated)
	}

	return nil
}

func (r *UpdateServiceReconciler) ensureConfigMap(ctx context.Context, reqLogger logr.Logger, cm *corev1.ConfigMap) error {
	// Check if this configmap already exists
	found := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating ConfigMap", "Namespace", cm.Namespace, "Name", cm.Name)
		return r.Client.Create(ctx, cm)
	} else if err != nil {
		return err
	}

	// found existing configmap; let's compare and update if needed
	if !reflect.DeepEqual(found.Data, cm.Data) {
		reqLogger.Info("Updating ConfigMap", "Namespace", cm.Namespace, "Name", cm.Name)
		updated := found.DeepCopy()
		updated.Data = cm.Data
		return r.Client.Update(ctx, updated)
	}

	return nil
}

func (r *UpdateServiceReconciler) ensureSecret(ctx context.Context, reqLogger logr.Logger, secret *corev1.Secret) error {
	// Check if this secret already exists
	found := &corev1.Secret{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating Secret", "Namespace", secret.Namespace, "Name", secret.Name)
		return r.Client.Create(ctx, secret)
	} else if err != nil {
		return err
	}

	// found existing secret; let's compare and update if needed
	if !reflect.DeepEqual(found.Data, secret.Data) {
		reqLogger.Info("Updating Secret", "Namespace", secret.Namespace, "Name", secret.Name)
		updated := found.DeepCopy()
		updated.Data = secret.Data
		return r.Client.Update(ctx, updated)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *UpdateServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapped := &mapper{mgr.GetClient()}

	return ctrl.NewControllerManagedBy(mgr).
		For(&cv1.UpdateService{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&policyv1beta1.PodDisruptionBudget{}).
		Owns(&routev1.Route{}).
		Watches(
			&source.Kind{Type: &apicfgv1.Image{}},
			handler.EnqueueRequestsFromMapFunc(mapped.Map),
		).
		Watches(
			&source.Kind{Type: &corev1.ConfigMap{}},
			handler.EnqueueRequestsFromMapFunc(mapped.Map),
		).
		Complete(r)
}
