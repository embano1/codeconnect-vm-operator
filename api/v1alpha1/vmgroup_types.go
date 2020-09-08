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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VmGroupSpec defines the desired state of VmGroup
type VmGroupSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4
	CPU int32 `json:"cpu"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=8
	Memory int32 `json:"memory"`
	// +kubebuilder:validation:Required
	Template string `json:"template"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	Replicas int32 `json:"replicas"`
}

type StatusPhase string

const (
	RunningStatusPhase StatusPhase = "RUNNING"
	PendingStatusPhase StatusPhase = "PENDING"
	ErrorStatusPhase   StatusPhase = "ERROR"
)

// VmGroupStatus defines the observed state of VmGroup
type VmGroupStatus struct {
	// +kubebuilder:validation:Optional
	Phase           StatusPhase `json:"phase"`
	CurrentReplicas *int32      `json:"currentReplicas,omitempty"`
	DesiredReplicas int32       `json:"desiredReplicas"`
	LastMessage     string      `json:"lastMessage"`
}

// +kubebuilder:object:root=true
// +kubebuilder:validation:Optional
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.desiredReplicas
// +kubebuilder:resource:shortName={"vg"}
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Current",type=integer,JSONPath=`.status.currentReplicas`
// +kubebuilder:printcolumn:name="Desired",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="CPU",type=integer,JSONPath=`.spec.cpu`
// +kubebuilder:printcolumn:name="Memory",type=integer,JSONPath=`.spec.memory`
// +kubebuilder:printcolumn:name="Template",type=string,JSONPath=`.spec.template`
// +kubebuilder:printcolumn:name="Last_Message",type=string,JSONPath=`.status.lastMessage`

// VmGroup is the Schema for the vmgroups API
type VmGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VmGroupSpec   `json:"spec,omitempty"`
	Status VmGroupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VmGroupList contains a list of VmGroup
type VmGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VmGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VmGroup{}, &VmGroupList{})
}
