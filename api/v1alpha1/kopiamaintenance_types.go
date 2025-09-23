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
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KopiaMaintenanceSpec defines the desired state of KopiaMaintenance
type KopiaMaintenanceSpec struct {
	// Repository matching configuration for finding existing ReplicationSources.
	// This approach matches existing ReplicationSources by their repository configuration.
	// Either RepositorySelector OR Repository must be specified, but not both.
	// +optional
	RepositorySelector *KopiaRepositorySelector `json:"repositorySelector,omitempty"`

	// Repository defines a direct repository configuration for maintenance.
	// This approach allows KopiaMaintenance to work independently of ReplicationSources.
	// Either RepositorySelector OR Repository must be specified, but not both.
	// +optional
	Repository *KopiaRepositorySpec `json:"repository,omitempty"`

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

	// Priority defines the priority of this maintenance configuration.
	// When multiple KopiaMaintenance resources match the same repository,
	// the one with the highest priority wins. Default is 0.
	// +kubebuilder:validation:Minimum=-100
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=0
	// +optional
	Priority int32 `json:"priority,omitempty"`

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
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations for maintenance pods.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Affinity for maintenance pods.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
}

// KopiaRepositorySpec defines a direct repository configuration for maintenance
type KopiaRepositorySpec struct {
	// Repository is the secret name containing repository configuration.
	// This secret should contain the repository connection details (URL, credentials, etc.)
	// in the same format as used by ReplicationSources.
	Repository string `json:"repository"`

	// Namespace where the repository secret is located.
	// If not specified, defaults to the VolSync operator namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// CustomCA is optional custom CA configuration for repository access.
	// +optional
	CustomCA *ReplicationSourceKopiaCA `json:"customCA,omitempty"`

	// RepositoryType specifies the type of repository (e.g., "s3", "azure", "gcs", "filesystem").
	// This helps with validation and provides metadata for maintenance operations.
	// +optional
	RepositoryType string `json:"repositoryType,omitempty"`
}

// KopiaRepositorySelector defines how to match Kopia repositories
type KopiaRepositorySelector struct {
	// Repository is the name of the repository secret to match.
	// Can use wildcards (* for any characters, ? for single character).
	// Examples: "kopia-*", "backup-?-repo", "prod-*-backup"
	// +optional
	Repository string `json:"repository,omitempty"`

	// NamespaceSelector defines which namespaces to match.
	// If not specified, matches all namespaces.
	// +optional
	NamespaceSelector *NamespaceSelector `json:"namespaceSelector,omitempty"`

	// CustomCA matches repositories using specific custom CA configuration.
	// +optional
	CustomCA *CustomCASelector `json:"customCA,omitempty"`

	// RepositoryType matches specific repository types (e.g., "s3", "azure", "gcs", "filesystem").
	// If not specified, matches all types.
	// +optional
	RepositoryType string `json:"repositoryType,omitempty"`

	// Labels matches ReplicationSources with specific labels.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// NamespaceSelector defines namespace selection criteria
type NamespaceSelector struct {
	// MatchNames lists specific namespace names to match.
	// +optional
	MatchNames []string `json:"matchNames,omitempty"`

	// MatchLabels matches namespaces by their labels.
	// +optional
	MatchLabels map[string]string `json:"matchLabels,omitempty"`

	// ExcludeNames lists namespace names to exclude.
	// +optional
	ExcludeNames []string `json:"excludeNames,omitempty"`
}

// CustomCASelector defines CA selection criteria
type CustomCASelector struct {
	// SecretName matches repositories using this CA secret name.
	// Can use wildcards.
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// ConfigMapName matches repositories using this CA ConfigMap name.
	// Can use wildcards.
	// +optional
	ConfigMapName string `json:"configMapName,omitempty"`
}

// KopiaMaintenanceStatus defines the observed state of KopiaMaintenance
type KopiaMaintenanceStatus struct {
	// MatchedSources lists the ReplicationSources currently matched by this maintenance configuration.
	// +optional
	MatchedSources []MatchedSource `json:"matchedSources,omitempty"`

	// ActiveCronJobs lists the CronJobs currently managed by this maintenance configuration.
	// +optional
	ActiveCronJobs []string `json:"activeCronJobs,omitempty"`

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

	// ConflictingMaintenances lists other KopiaMaintenance resources that match
	// the same repositories but have lower priority.
	// +optional
	ConflictingMaintenances []string `json:"conflictingMaintenances,omitempty"`
}

// MatchedSource represents a ReplicationSource matched by this maintenance configuration
type MatchedSource struct {
	// Name is the name of the ReplicationSource
	Name string `json:"name"`

	// Namespace is the namespace of the ReplicationSource
	Namespace string `json:"namespace"`

	// Repository is the repository being used
	Repository string `json:"repository"`

	// LastMatched is when this source was last matched
	// +optional
	LastMatched *metav1.Time `json:"lastMatched,omitempty"`
}

// KopiaMaintenance is a VolSync resource that defines maintenance configuration
// for Kopia repositories. It matches ReplicationSources based on repository
// configuration and schedules maintenance operations.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Enabled",type="boolean",JSONPath=`.spec.enabled`
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="Priority",type="integer",JSONPath=`.spec.priority`
// +kubebuilder:printcolumn:name="Matched",type="integer",JSONPath=`.status.matchedSources[*]`,description="Number of matched sources"
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

// IsDirectRepositoryMode returns true if this KopiaMaintenance uses direct repository configuration
func (km *KopiaMaintenance) IsDirectRepositoryMode() bool {
	return km.Spec.Repository != nil
}

// GetRepositorySecret returns the repository secret name and namespace for direct mode
func (km *KopiaMaintenance) GetRepositorySecret() (secretName, namespace string) {
	if km.Spec.Repository == nil {
		return "", ""
	}
	return km.Spec.Repository.Repository, km.Spec.Repository.Namespace
}

// Matches determines if this KopiaMaintenance matches the given ReplicationSource
// This only applies when using RepositorySelector mode (not direct repository mode)
func (km *KopiaMaintenance) Matches(source *ReplicationSource) bool {
	if source.Spec.Kopia == nil {
		return false
	}

	// Direct repository mode doesn't match ReplicationSources
	if km.IsDirectRepositoryMode() {
		return false
	}

	// Must have RepositorySelector for matching
	if km.Spec.RepositorySelector == nil {
		return false
	}

	// Check repository name match
	if km.Spec.RepositorySelector.Repository != "" {
		if !matchesPattern(source.Spec.Kopia.Repository, km.Spec.RepositorySelector.Repository) {
			return false
		}
	}

	// Check namespace match
	if km.Spec.RepositorySelector.NamespaceSelector != nil {
		if !km.matchesNamespace(source.Namespace) {
			return false
		}
	}

	// Check custom CA match
	if km.Spec.RepositorySelector.CustomCA != nil {
		if !km.matchesCustomCA(&source.Spec.Kopia.CustomCA) {
			return false
		}
	}

	// Check labels match
	if len(km.Spec.RepositorySelector.Labels) > 0 {
		if !km.matchesLabels(source.Labels) {
			return false
		}
	}

	return true
}

// matchesNamespace checks if the namespace matches the selector
func (km *KopiaMaintenance) matchesNamespace(namespace string) bool {
	if km.Spec.RepositorySelector == nil || km.Spec.RepositorySelector.NamespaceSelector == nil {
		return true
	}
	ns := km.Spec.RepositorySelector.NamespaceSelector

	// Check exclude list first
	for _, excluded := range ns.ExcludeNames {
		if namespace == excluded {
			return false
		}
	}

	// Check specific names
	if len(ns.MatchNames) > 0 {
		matched := false
		for _, name := range ns.MatchNames {
			if namespace == name {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check namespace labels (would need namespace object to check)
	// This would be implemented in the controller

	return true
}

// matchesCustomCA checks if the custom CA configuration matches
func (km *KopiaMaintenance) matchesCustomCA(ca *ReplicationSourceKopiaCA) bool {
	if km.Spec.RepositorySelector == nil {
		return true
	}
	selector := km.Spec.RepositorySelector.CustomCA

	if selector.SecretName != "" && ca.SecretName != "" {
		if !matchesPattern(ca.SecretName, selector.SecretName) {
			return false
		}
	}

	if selector.ConfigMapName != "" && ca.ConfigMapName != "" {
		if !matchesPattern(ca.ConfigMapName, selector.ConfigMapName) {
			return false
		}
	}

	return true
}

// matchesLabels checks if the source labels match the selector
func (km *KopiaMaintenance) matchesLabels(sourceLabels map[string]string) bool {
	if km.Spec.RepositorySelector == nil {
		return true
	}
	for key, value := range km.Spec.RepositorySelector.Labels {
		if sourceLabels[key] != value {
			return false
		}
	}
	return true
}

// matchesPattern checks if a string matches a pattern with wildcards
func matchesPattern(str, pattern string) bool {
	// Simple wildcard matching implementation
	// In production, would use a proper glob matcher
	if pattern == "*" {
		return true
	}

	// For now, support simple prefix/suffix matching
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		return strings.Contains(str, pattern[1:len(pattern)-1])
	} else if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(str, pattern[1:])
	} else if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(str, pattern[:len(pattern)-1])
	}

	return str == pattern
}

// Validate validates the KopiaMaintenance configuration
func (km *KopiaMaintenance) Validate() error {
	hasSelector := km.Spec.RepositorySelector != nil
	hasRepository := km.Spec.Repository != nil

	if !hasSelector && !hasRepository {
		return fmt.Errorf("either repositorySelector or repository must be specified")
	}

	if hasSelector && hasRepository {
		return fmt.Errorf("repositorySelector and repository are mutually exclusive - specify only one")
	}

	if hasRepository {
		if km.Spec.Repository.Repository == "" {
			return fmt.Errorf("repository.repository field is required when using direct repository mode")
		}
	}

	return nil
}

func init() {
	SchemeBuilder.Register(&KopiaMaintenance{}, &KopiaMaintenanceList{})
}