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

// InterfaceInstanceSpec defines the desired state of InterfaceInstance.
type InterfaceInstanceSpec struct {
	// Name of the provisioner this instance was assigned to.	//
	ProvisionerName string `json:"provisionerName,omitempty"`
	// Required capacity of a suitable InterfaceInstance.
	Capacity ResourceList `json:"capacity,omitempty"`
	// Capabilities that this instance support.
	Capabilities CapabilitiesList `json:"capabilities"`
	// What happens with this Instance after it is released from a Claim.
	// +kubebuilder:default=Delete
	ReclaimPolicy InterfaceReclaimPolicy `json:"reclaimPolicy,omitempty"`
	// Outputs is a list of Secrets or ConfigMaps that holds configuration and credentials to interact with this instance.
	Outputs []OutputReference `json:"outputs,omitempty"`
	// Back-Reference to the Claim binding to this instance.
	Claim *ObjectReference `json:"claim,omitempty"`
}

type OutputReference struct {
	// Name of this output reference.
	Name string `json:"name"`
	// SecretRef references a Secret in this namespace.
	SecretRef *ObjectReference `json:"secretRef,omitempty"`
	// ConfigMapRef references a ConfigMap in this namespace.
	ConfigMapRef *ObjectReference `json:"configMapRef,omitempty"`
}

// InterfaceInstanceStatus defines the observed state of InterfaceInstance.
type InterfaceInstanceStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Current conditions that apply to this instance.
	Conditions []InterfaceInstanceCondition `json:"conditions,omitempty"`
	// DEPRECATED.
	// Phase represents the current lifecycle state of this object.
	// Consider this field DEPRECATED, it will be removed as soon as there
	// is a mechanism to map conditions to strings when printing the property.
	// This is only for display purpose, for everything else use conditions.
	Phase InterfaceInstancePhase `json:"phase,omitempty"`

	Outputs []OutputStatus `json:"outputs,omitempty"`
}

type OutputStatus struct {
	// Name of this output reference.
	Name     string `json:"name"`
	Checksum []byte `json:"checksum"`
}

func (s *InterfaceInstanceStatus) updatePhase() {
	// Unready
	if s.GetCondition(InterfaceInstanceReady).Status != ConditionTrue {
		s.Phase = InterfaceInstancePhaseUnready
		return
	}

	// Bound
	if bound := s.GetCondition(InterfaceInstanceBound); bound.Status == ConditionTrue {
		s.Phase = InterfaceInstancePhaseBound
		return
	} else if bound.Status == ConditionFalse &&
		// Released
		bound.Reason == "Released" {
		s.Phase = InterfaceInstancePhaseReleased
		return
	}

	// Available
	if s.GetCondition(InterfaceInstanceAvailable).Status == ConditionTrue {
		s.Phase = InterfaceInstancePhaseAvailable
		return
	}

	// Fallback
	s.Phase = InterfaceInstancePhasePending
}

func (s *InterfaceInstanceStatus) SetCondition(condition InterfaceInstanceCondition) {
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
func (s *InterfaceInstanceStatus) GetCondition(t InterfaceInstanceConditionType) InterfaceInstanceCondition {
	for _, cond := range s.Conditions {
		if cond.Type == t {
			return cond
		}
	}
	return InterfaceInstanceCondition{Type: t}
}

// InterfaceInstancePhase is a simple representation of the curent state of an instance for kubectl.
type InterfaceInstancePhase string

const (
	// used for InterfaceInstances that are not yet available.
	InterfaceInstancePhasePending InterfaceInstancePhase = "Pending"
	// used for InterfaceInstances that are not yet bound.
	InterfaceInstancePhaseAvailable InterfaceInstancePhase = "Available"
	// Bound InterfaceInstances are bound to an InterfaceClaim.
	InterfaceInstancePhaseBound InterfaceInstancePhase = "Bound"
	// used for InterfaceInstances that are not ready. May appear on Bound or Available instances.
	InterfaceInstancePhaseUnready InterfaceInstancePhase = "Unready"
	// used for InterfaceInstances that where previously bound to a Claim.
	InterfaceInstancePhaseReleased InterfaceInstancePhase = "Released"
)

type InterfaceInstanceCondition struct {
	// Type is the type of the InterfaceInstance condition.
	Type InterfaceInstanceConditionType `json:"type"`
	// Status is the status of the condition, one of ('True', 'False', 'Unknown').
	Status ConditionStatus `json:"status"`
	// LastTransitionTime is the last time the condition transits from one status to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
	// Reason is the (brief) reason for the condition's last transition.
	Reason string `json:"reason"`
	// Message is the human readable message indicating details about last transition.
	Message string `json:"message"`
}

type InterfaceInstanceConditionType string

const (
	// InterfaceInstanceBound tracks if the InterfaceInstance is bound to an InterfaceClaim.
	InterfaceInstanceBound InterfaceInstanceConditionType = "Bound"
	// InterfaceInstanceReady tracks if the InterfaceInstance is alive and well.
	// InterfaceInstances may become unready, after beeing bound or when reconfigured as part of their normal lifecycle.
	InterfaceInstanceReady InterfaceInstanceConditionType = "Ready"
	// InterfaceInstanceAvailable tracks if the InterfaceInstance ready to be bound.
	// Instances are marked as Available as soon as their provisioning is completed.
	// Instances stay Available until deleted or bound.
	InterfaceInstanceAvailable InterfaceInstanceConditionType = "Available"
)

// InterfaceInstance is the Schema for the interfaceinstances API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Claim",type="string",JSONPath=".spec.claim.name"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=ii,categories=vedette
type InterfaceInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InterfaceInstanceSpec   `json:"spec,omitempty"`
	Status InterfaceInstanceStatus `json:"status,omitempty"`
}

// InterfaceInstanceList contains a list of InterfaceInstance
// +kubebuilder:object:root=true
type InterfaceInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InterfaceInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InterfaceInstance{}, &InterfaceInstanceList{})
}
