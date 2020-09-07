/*
Copyright 2020 The arhat.dev Authors.

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

// MaintenanceJobSpec defines the desired state of MaintenanceJob
type MaintenanceJobSpec struct {
	// NodeName of the node this job scheduled
	NodeName  string             `json:"nodeName,omitempty"`
	MachineID string             `json:"machineID,omitempty"`
	Stages    []MaintenanceStage `json:"stages,omitempty"`
}

type MaintenanceJobStageStatus struct {
	// Name of the stage
	Name string `json:"name,omitempty"`
	// ScheduledAt the time this stage get scheduled as a kubernetes Job
	// +optional
	ScheduledAt *metav1.Time `json:"scheduledAt,omitempty"`
	// FinishedAt the time this stage marked as finished by the controller
	// +optional
	FinishedAt *metav1.Time `json:"finishedAt,omitempty"`
	// A human readable message indicating details about why the pod is in this condition.
	// +optional
	Message string `json:"message,omitempty"`
	// A brief CamelCase message indicating details about why the pod is in this state.
	// e.g. 'Evicted'
	// +optional
	Reason string `json:"reason,omitempty"`

	// +optional
	Running bool `json:"running,omitempty"`
	// +optional
	Succeeded bool `json:"succeeded,omitempty"`
	// +optional
	Failed bool `json:"failed,omitempty"`
}

// MaintenanceJobStatus defines the observed state of MaintenanceJob
type MaintenanceJobStatus struct {
	// +optional
	SucceededAt *metav1.Time `json:"succeededAt,omitempty"`
	// +optional
	FailedAt *metav1.Time `json:"failedAt,omitempty"`

	Stages []MaintenanceJobStageStatus `json:"stages,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MaintenanceJob is the Schema for the MaintenanceJobs API
//
// +kubebuilder:resource:scope=Namespaced,shortName={mtjob,mtjobs},path=maintenancejobs
// +kubebuilder:printcolumn:name="Node",type="string",JSONPath=".spec.nodeName"
// +kubebuilder:printcolumn:name="Machine-ID",type="string",JSONPath=".spec.machineID"
// +kubebuilder:printcolumn:name="Failed-At",type="boolean",JSONPath=".status.failedAt"
// +kubebuilder:printcolumn:name="Succeeded-At",type="boolean",JSONPath=".status.succeededAt"
// +kubebuilder:subresource:status
type MaintenanceJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MaintenanceJobSpec   `json:"spec,omitempty"`
	Status MaintenanceJobStatus `json:"status,omitempty"`
}

func (mtJob *MaintenanceJob) Finished() bool {
	return len(mtJob.Status.Stages) == len(mtJob.Spec.Stages) &&
		mtJob.Status.Stages[len(mtJob.Status.Stages)-1].FinishedAt != nil &&
		!mtJob.Status.Stages[len(mtJob.Status.Stages)-1].FinishedAt.IsZero()
}

func (mtJob *MaintenanceJob) Succeeded() bool {
	return len(mtJob.Status.Stages) == len(mtJob.Spec.Stages) &&
		mtJob.Status.Stages[len(mtJob.Status.Stages)-1].Succeeded
}

func (mtJob *MaintenanceJob) Failed() bool {
	return len(mtJob.Status.Stages) == len(mtJob.Spec.Stages) && mtJob.Status.Stages[len(mtJob.Status.Stages)-1].Failed
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MaintenanceJobList contains a list of MaintenanceJob
type MaintenanceJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MaintenanceJob `json:"items"`
}
