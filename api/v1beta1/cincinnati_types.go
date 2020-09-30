/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CincinnatiSpec defines the desired state of Cincinnati
type CincinnatiSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

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
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
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

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=cincinnatis,scope=Namespaced

// Cincinnati is the Schema for the cincinnatis API
type Cincinnati struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CincinnatiSpec   `json:"spec,omitempty"`
	Status CincinnatiStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CincinnatiList contains a list of Cincinnati
type CincinnatiList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cincinnati `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cincinnati{}, &CincinnatiList{})
}
