package v1beta1

import (
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CincinnatiSpec defines the desired state of Cincinnati
type CincinnatiSpec struct {
	// replicas is the number of pods to run. When >=2, a PodDisruptionBudget
	// will ensure that voluntary disruption leaves at least one Pod running at
	// all times.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Required
	Replicas int32 `json:"replicas"`

	// registry is the container registry to use, such as "quay.io".
	// +kubebuilder:validation:Required
	Registry string `json:"registry"`

	// repository is the repository to use in the Registry, such as
	// "openshift-release-dev/ocp-release"
	// +kubebuilder:validation:Required
	Repository string `json:"repository"`

	// graphDataImage is a container image that contains the Cincinnati graph
	// data.
	// +kubebuilder:validation:Required
	GraphDataImage string `json:"graphDataImage"`
}

// CincinnatiStatus defines the observed state of Cincinnati
type CincinnatiStatus struct {
	// Conditions describe the state of the Cincinnati resource.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +kubebuilder:validation:Optional
	Conditions []conditionsv1.Condition `json:"conditions,omitempty"  patchStrategy:"merge" patchMergeKey:"type"`
}

// Condition Types
const (
	// ConditionReconcileCompleted reports whether all required resources have been created
	// in the cluster and reflect the specified state.
	ConditionReconcileCompleted conditionsv1.ConditionType = "ReconcileCompleted"

	// ConditionRegistryCACertFound reports whether the cincinnati registry CA cert had been found
	ConditionRegistryCACertFound conditionsv1.ConditionType = "RegistryCACertFound"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Cincinnati is the Schema for a Cincinnati service.
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=cincinnatis,scope=Namespaced
type Cincinnati struct {
	metav1.TypeMeta   `json:",inline"`

	// metadata is standard object metadata.  More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +kubebuilder:validation:Required
	metav1.ObjectMeta `json:"metadata"`

	// spec is the desired state of the Cincinnati service.  The
	// operator will work to ensure that the desired configuration is
	// applied to the cluster.
	// +kubebuilder:validation:Required
	Spec   CincinnatiSpec   `json:"spec"`

	// status contains information about the current state of the
	// Cincinnati service.
	// +kubebuilder:validation:Optional
	Status CincinnatiStatus `json:"status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CincinnatiList contains a list of Cincinnati
type CincinnatiList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cincinnati `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cincinnati{}, &CincinnatiList{})
}
