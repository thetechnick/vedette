/*
Copyright 2020 The Vedette authors.

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

// InterfaceClaimSpec defines the desired state of InterfaceClaim.
type InterfaceClaimSpec struct {
	// Required capacity of a suitable InterfaceInstance.
	Capacity ResourceList `json:"capacity,omitempty"`
	// Required capabilities of the InterfaceInstance.
	Capabilities CapabilitiesList `json:"capabilities,omitempty"`
	// Limit available InterfaceInstances for binding.
	Selector metav1.LabelSelector `json:"selector,omitempty"`
	// Inputs of the bound InterfaceInstance.
	Inputs []InputReference `json:"inputs,omitempty"`
	// Instance this Claim is bound to.
	// Must be set when the Claim is Bound.
	Instance *ObjectReference `json:"instance,omitempty"`
}

type InputReference struct {
	// Name of an output of an InterfaceInstance.
	Name string `json:"name"`
	// SecretRef references a Secret in this namespace.
	SecretRef *ObjectReference `json:"secretRef,omitempty"`
	// ConfigMapRef references a ConfigMap in this namespace.
	ConfigMapRef *ObjectReference `json:"configMapRef,omitempty"`
}

type InputReferenceEnvFrom struct {
	// An optional identifier to prepend to each key in the ConfigMap. Must be a C_IDENTIFIER.
	Prefix string `json:"prefix,omitempty"`
}

type InputReferenceVolumeMount struct {
	// Path within the container at which the volume should be mounted. Must not contain ':'.
	MountPath string `json:"mountPath"`
	// Path within the volume from which the container's volume should be mounted. Defaults to "" (volume's root).
	SubPath string `json:"subPath,omitempty"`
	// Expanded path within the volume from which the container's volume should be mounted. Behaves similarly to SubPath but environment variable references $(VAR_NAME) are expanded using the container's environment. Defaults to "" (volume's root). SubPathExpr and SubPath are mutually exclusive.
	SubPathExpr string `json:"subPathExpr,omitempty"`
}

// InterfaceClaimStatus defines the observed state of InterfaceClaim
type InterfaceClaimStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Current conditions that apply to this instance.
	Conditions []InterfaceClaimCondition `json:"conditions,omitempty"`
	// DEPRECATED.
	// Phase represents the current lifecycle state of this object.
	// Consider this field DEPRECATED, it will be removed as soon as there
	// is a mechanism to map conditions to strings when printing the property.
	// This is only for display purpose, for everything else use conditions.
	Phase InterfaceClaimPhase `json:"phase,omitempty"`
}

func (s *InterfaceClaimStatus) updatePhase() {
	// Lost
	if s.GetCondition(InterfaceClaimLost).Status == ConditionTrue {
		s.Phase = InterfaceClaimPhaseLost
		return
	}

	// Bound
	if s.GetCondition(InterfaceClaimBound).Status == ConditionTrue {
		s.Phase = InterfaceClaimPhaseBound
		return
	}

	// Fallback
	s.Phase = InterfaceClaimPhasePending
}

func (s *InterfaceClaimStatus) SetCondition(condition InterfaceClaimCondition) {
	// always update the phase when conditions change
	defer s.updatePhase()

	// update existing condition
	for i := range s.Conditions {
		if s.Conditions[i].Type == condition.Type {
			if s.Conditions[i].Status != condition.Status {
				s.Conditions[i].LastTransitionTime = metav1.Now()
			}
			s.Conditions[i].Status = condition.Status
			s.Conditions[i].Reason = condition.Reason
			s.Conditions[i].Message = condition.Message
			return
		}
	}

	if condition.LastTransitionTime.IsZero() {
		condition.LastTransitionTime = metav1.Now()
	}

	// add to conditions
	s.Conditions = append(s.Conditions, condition)
}

// GetCondition returns the Condition of the given type.
func (s *InterfaceClaimStatus) GetCondition(t InterfaceClaimConditionType) InterfaceClaimCondition {
	for _, cond := range s.Conditions {
		if cond.Type == t {
			return cond
		}
	}
	return InterfaceClaimCondition{Type: t}
}

// InterfaceClaimPhase is a simple representation of the curent state of an instance for kubectl.
type InterfaceClaimPhase string

const (
	// Pending is the initial state of the InterfaceClaim until Bound to an Instance.
	InterfaceClaimPhasePending InterfaceClaimPhase = "Pending"
	// The Claim is Bound after a matching InterfaceInstance has been found.
	InterfaceClaimPhaseBound InterfaceClaimPhase = "Bound"
	// The Bound InterfaceInstance can no longer be found.
	InterfaceClaimPhaseLost InterfaceClaimPhase = "Lost"
)

type InterfaceClaimCondition struct {
	// Type is the type of the InterfaceClaim condition.
	Type InterfaceClaimConditionType `json:"type"`
	// Status is the status of the condition, one of ('True', 'False', 'Unknown').
	Status ConditionStatus `json:"status"`
	// LastTransitionTime is the last time the condition transits from one status to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
	// Reason is the (brief) reason for the condition's last transition.
	Reason string `json:"reason"`
	// Message is the human readable message indicating details about last transition.
	Message string `json:"message"`
}

type InterfaceClaimConditionType string

const (
	// InterfaceClaimBound signifies whether the InterfaceClaim is bound to an InterfaceInstance.
	InterfaceClaimBound InterfaceClaimConditionType = "Bound"
	// InterfaceClaimLost is set to true when the Bound InterfaceInstance was deleted.
	InterfaceClaimLost InterfaceClaimConditionType = "Lost"
)

// InterfaceClaim is a user's request to bind to an InterfaceInstance.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Instance",type="string",JSONPath=".spec.instance.name"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=ic,categories=vedette
type InterfaceClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InterfaceClaimSpec   `json:"spec,omitempty"`
	Status InterfaceClaimStatus `json:"status,omitempty"`
}

// InterfaceClaimList contains a list of InterfaceClaim
// +kubebuilder:object:root=true
type InterfaceClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InterfaceClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InterfaceClaim{}, &InterfaceClaimList{})
}
