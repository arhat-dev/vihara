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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	MaintenanceConditionSucceeded = "succeeded"
	MaintenanceConditionFailed    = "failed"
)

type MaintenanceDependency struct {
	// Name of the maintenance
	Name string `json:"name,omitempty"`
	// Individual if set to true, use the result of single node specific MaintenanceJob to determine
	// if condition has met the requirement, and do not wait until the Maintenance has finished
	//
	// otherwise this Maintenance will not be scheduled until the dependent Maintenance has been expired
	// and all existing MaintenanceJobs's status will be ANDed as the condition
	//
	// +optional
	Individual bool `json:"individual,omitempty"`
	// Condition of the MaintenanceJob(s)
	// possible values: [succeeded, failed]
	// +optional
	Condition string `json:"condition,omitempty"`
}

type MaintenanceSchedule struct {
	// +optional
	Since *metav1.Time `json:"since,omitempty"`
	// +optional
	Delay metav1.Duration `json:"delay,omitempty"`
	// +optional
	Until *metav1.Time `json:"until,omitempty"`
	// Duration of the maintenance, if `until` is not specified, will set `until` = `since` + `duration`
	// +optional
	Duration metav1.Duration `json:"duration,omitempty"`
	// +optional
	Depends []MaintenanceDependency `json:"depends,omitempty"`
}

type MaintenanceNodeSelector struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	// reference value from the related node object
	// +optional
	ValueFrom ValueFrom `json:"valueFrom,omitempty"`
}

type MaintenanceNodeDrainSpec struct {
	NodeSelector     []MaintenanceNodeSelector `json:"nodeSelector,omitempty"`
	GracePeriod      metav1.Duration           `json:"gracePeriod,omitempty"`
	PodSelector      map[string]string         `json:"podSelector,omitempty"`
	DeleteLocalData  bool                      `json:"deleteLocalData,omitempty"`
	IgnoreDaemonsets bool                      `json:"ignoreDaemonsets,omitempty"`
}

type MaintenanceNodeUncordonSpec struct {
	NodeSelector []MaintenanceNodeSelector `json:"nodeSelector,omitempty"`
}

type MaintenanceStage struct {
	// +kubebuilder:validation:Pattern=[a-z0-9]([-a-z0-9]*[a-z0-9])?
	Name string `json:"name"`

	// +optional
	Delay metav1.Duration `json:"delay,omitempty"`

	// +optional
	Timeout metav1.Duration `json:"timeout,omitempty"`

	// +optional
	Drain *MaintenanceNodeDrainSpec `json:"drain,omitempty"`

	// +optional
	Uncordon *MaintenanceNodeUncordonSpec `json:"uncordon,omitempty"`

	// +optional
	Pod *corev1.PodTemplateSpec `json:"pod,omitempty"`
}

type ValueFrom struct {
	// +optional
	NodeFieldRef corev1.ObjectFieldSelector `json:"nodeFieldRef,omitempty"`
}

type ObjectNameRef struct {
	// +optional
	Name string `json:"name,omitempty"`
	// +optional
	NameFrom *ValueFrom `json:"nameFrom,omitempty"`
	// +optional
	NameFormat string `json:"nameFormat,omitempty"`
}

func (r ObjectNameRef) ValidateName() error {
	if r.Name != "" {
		return nil
	}

	if r.NameFrom == nil || r.NameFrom.NodeFieldRef.FieldPath == "" {
		return fmt.Errorf("no value available for name")
	}

	return nil
}

type ObjectNameKeyRef struct {
	ObjectNameRef `json:",inline"`
	// +optional
	Key string `json:"key,omitempty"`
	// +optional
	KeyFrom *ValueFrom `json:"keyFrom,omitempty"`
	// +optional
	KeyFormat string `json:"keyFormat,omitempty"`
}

func (r ObjectNameKeyRef) ValidateNameAndValue() error {
	if err := r.ObjectNameRef.ValidateName(); err != nil {
		return err
	}

	if r.Key != "" {
		return nil
	}

	if r.KeyFrom == nil || r.KeyFrom.NodeFieldRef.FieldPath == "" {
		return fmt.Errorf("no value available for key")
	}

	return nil
}

type NodeSpecificEnvValueFrom struct {
	// +optional
	ConfigMapKeyRef *ObjectNameKeyRef `json:"configMapKeyRef,omitempty"`
	// +optional
	SecretKeyRef *ObjectNameKeyRef `json:"secretKeyRef,omitempty"`
}

func (f NodeSpecificEnvValueFrom) Validate() error {
	switch {
	case f.ConfigMapKeyRef != nil:
		return f.ConfigMapKeyRef.ValidateNameAndValue()
	case f.SecretKeyRef != nil:
		return f.SecretKeyRef.ValidateNameAndValue()
	}

	return fmt.Errorf("must provide at least one value source")
}

type NodeSpecificEnvVar struct {
	Name string `json:"name"`
	// +optional
	Value string `json:"value,omitempty"`
	// +optional
	ValueFrom NodeSpecificEnvValueFrom `json:"valueFrom,omitempty"`
}

func (e NodeSpecificEnvVar) Validate() error {
	if e.Name == "" {
		return fmt.Errorf("must provide name")
	}

	if e.Value != "" {
		return nil
	}

	return e.ValueFrom.Validate()
}

type NodeSpecificEnvFromSource struct {
	// +optional
	Prefix string `json:"prefix,omitempty"`
	// +optional
	ConfigMapRef *ObjectNameRef `json:"configMapRef,omitempty"`
	// +optional
	SecretRef *ObjectNameRef `json:"secretRef,omitempty"`
}

func (s NodeSpecificEnvFromSource) Validate() error {
	switch {
	case s.ConfigMapRef != nil:
		return s.ConfigMapRef.ValidateName()
	case s.SecretRef != nil:
		return s.SecretRef.ValidateName()
	}

	return fmt.Errorf("must provide at least one value source")
}

// MaintenanceSpec defines the desired state of Maintenance
type MaintenanceSpec struct {
	// TargetNamespaces schedule to specific namespaces
	// if not set, use all namespaces
	// +optional
	TargetNamespaces []string `json:"targetNamespaces,omitempty"`
	// DryRun will instruct controller not to create real job workloads for this maintenance
	// useful for pre-maintenance check
	// +optional
	DryRun bool `json:"dryRun,omitempty"`
	// NodeSelector which must match a node's labels for the Maintenance to be scheduled on that node.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// +optional
	Nodes []string `json:"nodes,omitempty"`
	// +optional
	Env []NodeSpecificEnvVar `json:"env,omitempty"`
	// +optional
	EnvFrom []NodeSpecificEnvFromSource `json:"envFrom,omitempty"`
	// Tolerations shared by all stages in this maintenance.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Schedule of this maintenance
	// +optional
	Schedule MaintenanceSchedule `json:"schedule,omitempty"`
	// Stages
	Stages []MaintenanceStage `json:"stages,omitempty"`
}

type ResourceReference struct {
	// Name of the referent.
	// More info: http://kubernetes.io/docs/user-guide/identifiers#names
	Name string `json:"name"`

	// Namespace of the referent
	Namespace string `json:"namespace"`
	// UID of the referent.
	// More info: http://kubernetes.io/docs/user-guide/identifiers#uids
	UID types.UID `json:"uid"`
}

type MaintenancePhaseAdmitted struct {
	// +optional
	At *metav1.Time `json:"at,omitempty"`
	// +optional
	By ResourceReference `json:"by,omitempty"`
	// +optional
	ScheduleSince *metav1.Time `json:"scheduleSince,omitempty"`
	// +optional
	ScheduleUntil *metav1.Time `json:"scheduleUntil,omitempty"`
}

type MaintenancePhaseScheduled struct {
	// +optional
	At *metav1.Time `json:"at,omitempty"`
	// +optional
	By ResourceReference `json:"by,omitempty"`
	// +optional
	SpecSnapshot string `json:"specSnapshot,omitempty"`
	// +optional
	SpecSnapshotHash Hash `json:"specSnapshotHash,omitempty"`
}

type Hash struct {
	// +optional
	Sha256Hex string `json:"sha256Hex,omitempty"`
}

type MaintenancePhaseFinished struct {
	// +optional
	At *metav1.Time `json:"at,omitempty"`
	// +optional
	By ResourceReference `json:"by,omitempty"`
}

// MaintenanceStatus defines the observed state of Maintenance
type MaintenanceStatus struct {
	// +optional
	Admitted MaintenancePhaseAdmitted `json:"admitted,omitempty"`
	// +optional
	Scheduled MaintenancePhaseScheduled `json:"scheduled,omitempty"`
	// +optional
	Finished MaintenancePhaseFinished `json:"finished,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Maintenance is the Schema for the Maintenance API
// process of a single Maintenance:
//		User creation of Maintenance object
//		-> Admit by controller if Maintenance is valid
//		-> Enqueue by controller
//		-> Schedule by controller:
//			- spec becomes immutable since this time
//			- Create MaintenanceJob(s) in the selected namespace for each node selected according to the Maintenance object
// nolint:lll
// +kubebuilder:printcolumn:name="Schedule-Since",type="string",format="date-time",JSONPath=".status.admitted.scheduleSince"
// +kubebuilder:printcolumn:name="Schedule-Until",type="string",format="date-time",JSONPath=".status.admitted.scheduleUntil"
// +kubebuilder:printcolumn:name="Admitted-At",type="string",format="date-time",JSONPath=".status.admitted.at"
// +kubebuilder:printcolumn:name="Scheduled-At",type="string",format="date-time",JSONPath=".status.scheduled.at"
// +kubebuilder:printcolumn:name="Finished-At",type="string",format="date-time",JSONPath=".status.finished.at"
// +kubebuilder:resource:scope=Cluster,shortName=mt,path=maintenance
// +kubebuilder:subresource:status
type Maintenance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MaintenanceSpec   `json:"spec,omitempty"`
	Status MaintenanceStatus `json:"status,omitempty"`
}

func (mt *Maintenance) CanBeScheduledIn(namespace string) bool {
	if len(mt.Spec.TargetNamespaces) != 0 {
		for _, ns := range mt.Spec.TargetNamespaces {
			if namespace == ns {
				return true
			}
		}

		return false
	}

	return true
}

func (mt *Maintenance) Finished() bool {
	return mt.Status.Finished.At != nil && !mt.Status.Finished.At.Time.IsZero()
}

func (mt *Maintenance) Admitted() bool {
	return mt.Status.Admitted.At != nil && !mt.Status.Admitted.At.Time.IsZero()
}

func (mt *Maintenance) Canceled() bool {
	return mt.DeletionTimestamp != nil && !mt.DeletionTimestamp.IsZero()
}

func (mt *Maintenance) Started(now time.Time) bool {
	return mt.Status.Admitted.ScheduleSince != nil && !mt.Status.Admitted.ScheduleSince.Time.After(now)
}

func (mt *Maintenance) Scheduled() bool {
	return mt.Status.Scheduled.At != nil && !mt.Status.Scheduled.At.Time.IsZero()
}

func (mt *Maintenance) Expired(now time.Time) bool {
	return mt.Status.Admitted.ScheduleUntil != nil && mt.Status.Admitted.ScheduleUntil.Time.Before(now)
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MaintenanceList contains a list of Maintenance
type MaintenanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Maintenance `json:"items"`
}
