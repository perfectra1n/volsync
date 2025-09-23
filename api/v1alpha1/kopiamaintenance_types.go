/*
Copyright 2025 The VolSync authors.

This file may be used, at your option, according to either the GNU AGPL 3.0 or
the Apache V2 license.

---
This program is free software: you can redistribute it and/or modify it under
the terms of the GNU Affero General Public License as published by the Free
Software Foundation, either version 3 of the License, or (at your option) any
later version.

This program is distributed in the hope that it will be useful, but WITHOUT ANY
WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
PARTICULAR PURPOSE.  See the GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License along
with this program.  If not, see <https://www.gnu.org/licenses/>.

---
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

// +kubebuilder:validation:Required
package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KopiaMaintenanceSpec defines the desired state of KopiaMaintenance
type KopiaMaintenanceSpec struct {
	// Repository defines the repository configuration for maintenance.
	// The repository secret must exist in the same namespace as the KopiaMaintenance resource.
	// +kubebuilder:validation:Required
	Repository KopiaRepositorySpec `json:"repository"`

	// Schedule is a cron schedule for when maintenance should run.
	// The schedule is interpreted in the controller's timezone.
	// +kubebuilder:validation:Pattern=`^(@(annually|yearly|monthly|weekly|daily|hourly))|((((\d+,)*\d+|(\d+(\/|-)\d+)|\*(\/\d+)?)\s?){5})$`
	// +kubebuilder:default="0 2 * * *"
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// Enabled determines if maintenance should be performed.
	// When false, no maintenance will be scheduled.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Suspend can be used to temporarily stop maintenance. When true,
	// the CronJob will not create new Jobs, but existing Jobs will be allowed
	// to complete.
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// SuccessfulJobsHistoryLimit specifies how many successful maintenance Jobs
	// should be kept. Defaults to 3.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=3
	// +optional
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty"`

	// FailedJobsHistoryLimit specifies how many failed maintenance Jobs
	// should be kept. Defaults to 1.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	// +optional
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit,omitempty"`

	// Resources represents compute resources required by the maintenance container.
	// If not specified, defaults to 256Mi memory request and 1Gi memory limit.
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// ServiceAccountName allows specifying a custom ServiceAccount for maintenance jobs.
	// If not specified, a default maintenance ServiceAccount will be used.
	// +optional
	ServiceAccountName *string `json:"serviceAccountName,omitempty"`

	// MoverPodLabels that should be added to maintenance pods.
	// These will be in addition to any labels that VolSync may add.
	// +optional
	MoverPodLabels map[string]string `json:"moverPodLabels,omitempty"`

	// NodeSelector for maintenance pods.
	// NOTE: This field is preserved for future implementation. Currently, NodeSelector is not
	// directly supported by the Kopia mover spec and will be ignored.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations for maintenance pods.
	// NOTE: This field is preserved for future implementation. Currently, Tolerations are not
	// directly supported by the Kopia mover spec and will be ignored.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Affinity for maintenance pods.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
}

// KopiaRepositorySpec defines the repository configuration for maintenance
type KopiaRepositorySpec struct {
	// Repository is the secret name containing repository configuration.
	// This secret should contain the repository connection details (URL, credentials, etc.)
	// in the same format as used by ReplicationSources.
	// The secret must exist in the same namespace as the KopiaMaintenance resource.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Repository string `json:"repository"`

	// CustomCA is optional custom CA configuration for repository access.
	// +optional
	CustomCA *ReplicationSourceKopiaCA `json:"customCA,omitempty"`

	// RepositoryType specifies the type of repository (e.g., "s3", "azure", "gcs", "filesystem").
	// This helps with validation and provides metadata for maintenance operations.
	// +optional
	RepositoryType string `json:"repositoryType,omitempty"`
}


// KopiaMaintenanceStatus defines the observed state of KopiaMaintenance
type KopiaMaintenanceStatus struct {
	// ActiveCronJob is the name of the CronJob currently managed by this maintenance configuration.
	// +optional
	ActiveCronJob string `json:"activeCronJob,omitempty"`

	// LastReconcileTime is the last time this maintenance configuration was reconciled.
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`

	// LastMaintenanceTime is the last time maintenance was successfully performed.
	// +optional
	LastMaintenanceTime *metav1.Time `json:"lastMaintenanceTime,omitempty"`

	// NextScheduledMaintenance is the next scheduled maintenance time.
	// +optional
	NextScheduledMaintenance *metav1.Time `json:"nextScheduledMaintenance,omitempty"`

	// MaintenanceFailures counts the number of consecutive maintenance failures.
	// +optional
	MaintenanceFailures int32 `json:"maintenanceFailures,omitempty"`

	// Conditions represent the latest available observations of the
	// maintenance configuration's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// KopiaMaintenance is a VolSync resource that defines maintenance configuration
// for Kopia repositories. It manages repository maintenance operations
// on a defined schedule.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Repository",type="string",JSONPath=`.spec.repository.repository`
// +kubebuilder:printcolumn:name="Enabled",type="boolean",JSONPath=`.spec.enabled`
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="Suspended",type="boolean",JSONPath=`.spec.suspend`
// +kubebuilder:printcolumn:name="Last Maintenance",type="string",format="date-time",JSONPath=`.status.lastMaintenanceTime`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`
type KopiaMaintenance struct {
	metav1.TypeMeta `json:",inline"`
	//+optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// spec is the desired state of the KopiaMaintenance, including the
	// repository matching criteria and maintenance configuration.
	Spec KopiaMaintenanceSpec `json:"spec,omitempty"`
	// status is the observed state of the KopiaMaintenance as determined by
	// the controller.
	//+optional
	Status *KopiaMaintenanceStatus `json:"status,omitempty"`
}

// KopiaMaintenanceList contains a list of KopiaMaintenance
// +kubebuilder:object:root=true
type KopiaMaintenanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KopiaMaintenance `json:"items"`
}

// GetEnabled returns whether maintenance is enabled
func (km *KopiaMaintenance) GetEnabled() bool {
	if km.Spec.Enabled == nil {
		return true // Default to enabled
	}
	return *km.Spec.Enabled
}

// GetSchedule returns the maintenance schedule
func (km *KopiaMaintenance) GetSchedule() string {
	if km.Spec.Schedule == "" {
		return "0 2 * * *" // Default schedule
	}
	return km.Spec.Schedule
}

// GetRepositorySecret returns the repository secret name
func (km *KopiaMaintenance) GetRepositorySecret() string {
	return km.Spec.Repository.Repository
}


// Validate validates the KopiaMaintenance configuration
func (km *KopiaMaintenance) Validate() error {
	if km.Spec.Repository.Repository == "" {
		return fmt.Errorf("repository.repository field is required")
	}

	return nil
}

func init() {
	SchemeBuilder.Register(&KopiaMaintenance{}, &KopiaMaintenanceList{})
}