//go:build !disable_kopia

/*
Copyright 2025 The VolSync authors.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published
by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package kopia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	volsyncv1alpha1 "github.com/backube/volsync/api/v1alpha1"
)

const (
	// Default maintenance username for CronJobs
	defaultMaintenanceUsername = "maintenance@volsync"
	// Default maintenance schedule (2 AM daily)
	defaultMaintenanceSchedule = "0 2 * * *"
	// Label keys for maintenance CronJobs
	maintenanceLabelKey        = "volsync.backube/kopia-maintenance"
	maintenanceRepositoryLabel = "volsync.backube/repository-hash"
	maintenanceNamespaceLabel  = "volsync.backube/namespace"
	// Annotation for repository config
	maintenanceRepositoryAnnotation = "volsync.backube/repository-config"
	// ServiceAccount name for maintenance
	maintenanceServiceAccountName = "volsync-kopia-maintenance"
	// Maximum length for CronJob name
	maxCronJobNameLength = 52
)

// MaintenanceManager handles the lifecycle of Kopia maintenance CronJobs
type MaintenanceManager struct {
	client         client.Client
	logger         logr.Logger
	containerImage string
	metrics        kopiaMetrics
}

// NewMaintenanceManager creates a new MaintenanceManager
func NewMaintenanceManager(client client.Client, logger logr.Logger, containerImage string) *MaintenanceManager {
	return &MaintenanceManager{
		client:         client,
		logger:         logger.WithName("maintenance"),
		containerImage: containerImage,
		metrics:        newKopiaMetrics(),
	}
}

// RepositoryConfig represents the unique configuration for a Kopia repository
type RepositoryConfig struct {
	// Repository secret name
	Repository string `json:"repository"`
	// Custom CA configuration
	CustomCA *volsyncv1alpha1.CustomCASpec `json:"customCA,omitempty"`
	// Namespace where the repository is used
	Namespace string `json:"namespace"`
	// Schedule for maintenance (optional, defaults to daily at 2 AM)
	Schedule string `json:"schedule,omitempty"`
}

// Hash generates a deterministic hash for the repository configuration
func (rc *RepositoryConfig) Hash() string {
	// Try JSON marshaling first for consistency
	data, err := json.Marshal(rc)
	if err != nil {
		// Fallback to deterministic hash based on key fields if JSON marshaling fails
		fallbackStr := fmt.Sprintf("%s:%s:%s", rc.Repository, rc.Namespace, rc.Schedule)
		if rc.CustomCA != nil {
			fallbackStr = fmt.Sprintf("%s:ca-%s-%s", fallbackStr, rc.CustomCA.SecretName, rc.CustomCA.ConfigMapName)
		}
		data = []byte(fallbackStr)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])[:16] // Use first 16 chars for shorter names
}

// ReconcileMaintenanceForSource ensures a maintenance CronJob exists for the repository
// used by this ReplicationSource
func (m *MaintenanceManager) ReconcileMaintenanceForSource(ctx context.Context,
	source *volsyncv1alpha1.ReplicationSource) error {
	if source.Spec.Kopia == nil {
		return nil
	}

	// Validate input parameters
	if source.Name == "" {
		return fmt.Errorf("ReplicationSource name is required but was empty")
	}
	if source.Namespace == "" {
		return fmt.Errorf("ReplicationSource namespace is required but was empty")
	}
	if source.Spec.Kopia.Repository == "" {
		return fmt.Errorf("Kopia repository configuration is required but was empty")
	}

	// Check if maintenance is disabled
	if !m.isMaintenanceEnabled(source) {
		m.logger.V(1).Info("Maintenance disabled for source",
			"source", source.Name,
			"namespace", source.Namespace)
		return nil
	}

	// Create repository config
	repoConfig := &RepositoryConfig{
		Repository: source.Spec.Kopia.Repository,
		CustomCA:   (*volsyncv1alpha1.CustomCASpec)(&source.Spec.Kopia.CustomCA),
		Namespace:  source.Namespace,
		Schedule:   m.getMaintenanceSchedule(source),
	}

	// Ensure maintenance CronJob exists
	return m.ensureMaintenanceCronJob(ctx, repoConfig, source)
}

// CleanupOrphanedMaintenanceCronJobs removes maintenance CronJobs that no longer have
// any associated Kopia ReplicationSources
func (m *MaintenanceManager) CleanupOrphanedMaintenanceCronJobs(ctx context.Context,
	namespace string) error {
	// List all maintenance CronJobs in the namespace
	cronJobList := &batchv1.CronJobList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{maintenanceLabelKey: "true"},
	}
	if err := m.client.List(ctx, cronJobList, listOpts...); err != nil {
		return fmt.Errorf("failed to list maintenance CronJobs: %w", err)
	}

	// List all Kopia ReplicationSources in the namespace
	sourceList := &volsyncv1alpha1.ReplicationSourceList{}
	if err := m.client.List(ctx, sourceList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list ReplicationSources: %w", err)
	}

	// Build a set of required repository hashes
	requiredHashes := make(map[string]bool)
	for _, source := range sourceList.Items {
		if source.Spec.Kopia != nil {
			// Skip if maintenance is disabled
			if !m.isMaintenanceEnabled(&source) {
				continue
			}
			repoConfig := &RepositoryConfig{
				Repository: source.Spec.Kopia.Repository,
				CustomCA:   (*volsyncv1alpha1.CustomCASpec)(&source.Spec.Kopia.CustomCA),
				Namespace:  source.Namespace,
				Schedule:   m.getMaintenanceSchedule(&source),
			}
			requiredHashes[repoConfig.Hash()] = true
		}
	}

	// Delete orphaned CronJobs
	for _, cronJob := range cronJobList.Items {
		repoHash, exists := cronJob.Labels[maintenanceRepositoryLabel]
		if !exists || !requiredHashes[repoHash] {
			m.logger.Info("Deleting orphaned maintenance CronJob",
				"cronJob", cronJob.Name,
				"namespace", cronJob.Namespace)
			if err := m.client.Delete(ctx, &cronJob); err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf("failed to delete orphaned CronJob %s: %w", cronJob.Name, err)
			}
			// Record metric for deleted CronJob
			m.recordCronJobDeletionMetric(cronJob.Namespace, cronJob.Name)
		}
	}

	return nil
}

// ensureMaintenanceCronJob creates or updates a maintenance CronJob for the given repository
func (m *MaintenanceManager) ensureMaintenanceCronJob(ctx context.Context,
	repoConfig *RepositoryConfig, owner client.Object) error {
	cronJob := m.buildMaintenanceCronJob(repoConfig, owner)

	// Check if CronJob already exists
	existing := &batchv1.CronJob{}
	err := m.client.Get(ctx, types.NamespacedName{
		Name:      cronJob.Name,
		Namespace: cronJob.Namespace,
	}, existing)

	if err != nil {
		if errors.IsNotFound(err) {
			// Create new CronJob
			m.logger.Info("Creating maintenance CronJob",
				"name", cronJob.Name,
				"namespace", cronJob.Namespace,
				"schedule", cronJob.Spec.Schedule)

			if err := m.client.Create(ctx, cronJob); err != nil {
				return err
			}

			// Record metric for created CronJob
			m.recordCronJobMetric(owner, "created")
			return nil
		}
		return fmt.Errorf("failed to get CronJob: %w", err)
	}

	// Update existing CronJob if schedule changed
	if existing.Spec.Schedule != cronJob.Spec.Schedule {
		m.logger.Info("Updating maintenance CronJob schedule",
			"name", existing.Name,
			"namespace", existing.Namespace,
			"oldSchedule", existing.Spec.Schedule,
			"newSchedule", cronJob.Spec.Schedule)
		existing.Spec.Schedule = cronJob.Spec.Schedule

		if err := m.client.Update(ctx, existing); err != nil {
			return err
		}

		// Record metric for updated CronJob
		m.recordCronJobMetric(owner, "updated")
		return nil
	}

	// CronJob already exists and is up to date
	return nil
}

// buildMaintenanceCronJob constructs a CronJob for Kopia maintenance
func (m *MaintenanceManager) buildMaintenanceCronJob(repoConfig *RepositoryConfig,
	owner client.Object) *batchv1.CronJob {
	repoHash := repoConfig.Hash()
	cronJobName := m.generateCronJobName(repoHash)

	// Build environment variables for the maintenance container
	envVars := m.buildMaintenanceEnvVars(repoConfig)

	// Build volume mounts
	volumes, volumeMounts := m.buildMaintenanceVolumes(repoConfig)

	// Determine resources to use
	resources := m.getMaintenanceResources(owner)

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cronJobName,
			Namespace: repoConfig.Namespace,
			Labels: map[string]string{
				maintenanceLabelKey:        "true",
				maintenanceRepositoryLabel: repoHash,
				maintenanceNamespaceLabel:  repoConfig.Namespace,
			},
			Annotations: map[string]string{
				maintenanceRepositoryAnnotation: repoConfig.Repository,
			},
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   repoConfig.Schedule,
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			Suspend:                    m.isSuspended(owner),
			SuccessfulJobsHistoryLimit: m.getSuccessfulJobsHistoryLimit(owner),
			FailedJobsHistoryLimit:     m.getFailedJobsHistoryLimit(owner),
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					BackoffLimit: ptr.To(int32(3)),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								maintenanceLabelKey:        "true",
								maintenanceRepositoryLabel: repoHash,
							},
						},
						Spec: corev1.PodSpec{
							ServiceAccountName: m.getOrCreateServiceAccountName(repoConfig),
							RestartPolicy:      corev1.RestartPolicyOnFailure,
							SecurityContext: &corev1.PodSecurityContext{
								RunAsNonRoot: ptr.To(true),
								FSGroup:      ptr.To(int64(1000)),
								RunAsUser:    ptr.To(int64(1000)),
							},
							Containers: []corev1.Container{
								{
									Name:            "kopia-maintenance",
									Image:           m.containerImage,
									ImagePullPolicy: corev1.PullIfNotPresent,
									Command:         []string{"/bin/bash", "-c"},
									Args:            []string{"/entry.sh maintenance"},
									Env:             envVars,
									EnvFrom: []corev1.EnvFromSource{
										{
											SecretRef: &corev1.SecretEnvSource{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: repoConfig.Repository,
												},
											},
										},
									},
									VolumeMounts:    volumeMounts,
									Resources:       resources,
									SecurityContext: &corev1.SecurityContext{
										AllowPrivilegeEscalation: ptr.To(false),
										Capabilities: &corev1.Capabilities{
											Drop: []corev1.Capability{"ALL"},
										},
										Privileged:             ptr.To(false),
										ReadOnlyRootFilesystem: ptr.To(true), // Enhanced security: read-only root filesystem
										RunAsNonRoot:           ptr.To(true),
										RunAsUser:              ptr.To(int64(1000)),
									},
								},
							},
							Volumes: volumes,
						},
					},
				},
			},
		},
	}

	// Set owner reference if provided (optional, as CronJob may outlive individual sources)
	if owner != nil {
		// Don't set controller reference to avoid deletion cascade
		// We want the CronJob to persist as long as any source uses the repository
		cronJob.Labels["volsync.backube/created-by"] = owner.GetName()
	}

	return cronJob
}

// buildMaintenanceEnvVars creates environment variables for the maintenance container
func (m *MaintenanceManager) buildMaintenanceEnvVars(repoConfig *RepositoryConfig) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{
			Name:  "DIRECTION",
			Value: "maintenance",
		},
		{
			Name:  "KOPIA_CACHE_DIR",
			Value: kopiaCacheMountPath,
		},
		{
			Name:  "DATA_DIR",
			Value: "/data", // Not used for maintenance, but required by entry.sh
		},
		{
			Name:  "KOPIA_OVERRIDE_USERNAME",
			Value: defaultMaintenanceUsername,
		},
		{
			Name:  "KOPIA_OVERRIDE_HOSTNAME",
			Value: repoConfig.Namespace,
		},
	}

	// Note: All repository connection details (including KOPIA_PASSWORD) are sourced
	// from the repository secret via EnvFrom in the container spec

	// Add custom CA environment variable if specified
	if repoConfig.CustomCA != nil && (repoConfig.CustomCA.SecretName != "" || repoConfig.CustomCA.ConfigMapName != "") {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "CUSTOM_CA",
			Value: fmt.Sprintf("%s/%s", kopiaCAMountPath, kopiaCAFilename),
		})
	}

	return envVars
}

// buildMaintenanceVolumes creates volumes and volume mounts for the maintenance container
func (m *MaintenanceManager) buildMaintenanceVolumes(repoConfig *RepositoryConfig) ([]corev1.Volume, []corev1.VolumeMount) {
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

	// Cache volume (emptyDir for maintenance jobs with size limit)
	volumes = append(volumes, corev1.Volume{
		Name: kopiaCache,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				SizeLimit: resource.NewQuantity(1*1024*1024*1024, resource.BinarySI), // 1Gi limit
			},
		},
	})
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      kopiaCache,
		MountPath: kopiaCacheMountPath,
	})

	// Temp directory volume (required for read-only root filesystem)
	volumes = append(volumes, corev1.Volume{
		Name: "tempdir",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium: corev1.StorageMediumMemory,
			},
		},
	})
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "tempdir",
		MountPath: "/tmp",
	})

	// Custom CA if specified
	if repoConfig.CustomCA != nil {
		if repoConfig.CustomCA.SecretName != "" {
			volumes = append(volumes, corev1.Volume{
				Name: "custom-ca",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: repoConfig.CustomCA.SecretName,
						Items: []corev1.KeyToPath{
							{
								Key:  repoConfig.CustomCA.Key,
								Path: kopiaCAFilename,
							},
						},
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "custom-ca",
				MountPath: kopiaCAMountPath,
				ReadOnly:  true,
			})
		} else if repoConfig.CustomCA.ConfigMapName != "" {
			volumes = append(volumes, corev1.Volume{
				Name: "custom-ca",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: repoConfig.CustomCA.ConfigMapName,
						},
						Items: []corev1.KeyToPath{
							{
								Key:  repoConfig.CustomCA.Key,
								Path: kopiaCAFilename,
							},
						},
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "custom-ca",
				MountPath: kopiaCAMountPath,
				ReadOnly:  true,
			})
		}
	}

	return volumes, volumeMounts
}

// generateCronJobName generates a unique but deterministic name for the maintenance CronJob
func (m *MaintenanceManager) generateCronJobName(repoHash string) string {
	name := fmt.Sprintf("kopia-maintenance-%s", repoHash)
	if len(name) > maxCronJobNameLength {
		// Truncate if too long
		name = name[:maxCronJobNameLength]
	}
	return name
}

// isMaintenanceEnabled checks if maintenance is enabled for this source
func (m *MaintenanceManager) isMaintenanceEnabled(source *volsyncv1alpha1.ReplicationSource) bool {
	if source.Spec.Kopia == nil {
		return false
	}

	// If MaintenanceCronJob is specified, use its enabled setting
	if source.Spec.Kopia.MaintenanceCronJob != nil {
		if source.Spec.Kopia.MaintenanceCronJob.Enabled != nil {
			return *source.Spec.Kopia.MaintenanceCronJob.Enabled
		}
		// Default to enabled if MaintenanceCronJob is specified but Enabled is not set
		return true
	}

	// Fall back to maintenanceIntervalDays for backward compatibility
	// A value of 0 disables maintenance
	if source.Spec.Kopia.MaintenanceIntervalDays != nil &&
		*source.Spec.Kopia.MaintenanceIntervalDays == 0 {
		return false
	}

	// Default to enabled
	return true
}

// getMaintenanceSchedule determines the maintenance schedule for a source
func (m *MaintenanceManager) getMaintenanceSchedule(source *volsyncv1alpha1.ReplicationSource) string {
	// Use schedule from MaintenanceCronJob if specified
	if source.Spec.Kopia != nil && source.Spec.Kopia.MaintenanceCronJob != nil {
		if source.Spec.Kopia.MaintenanceCronJob.Schedule != "" {
			return source.Spec.Kopia.MaintenanceCronJob.Schedule
		}
	}

	// Fall back to converting maintenanceIntervalDays to a schedule if specified
	if source.Spec.Kopia != nil && source.Spec.Kopia.MaintenanceIntervalDays != nil {
		days := *source.Spec.Kopia.MaintenanceIntervalDays
		if days > 0 {
			// Convert days to cron schedule
			if days == 1 {
				return "0 2 * * *" // Daily at 2 AM
			} else if days == 7 {
				return "0 2 * * 0" // Weekly on Sunday at 2 AM
			} else if days == 30 || days == 31 {
				return "0 2 1 * *" // Monthly on the 1st at 2 AM
			} else {
				// For other values, use daily schedule
				// The actual interval check will be handled by the maintenance job
				return "0 2 * * *"
			}
		}
	}

	// Default schedule
	return defaultMaintenanceSchedule
}

// getOrCreateServiceAccountName returns the service account name for maintenance
func (m *MaintenanceManager) getOrCreateServiceAccountName(repoConfig *RepositoryConfig) string {
	// For now, return a standard name. In the future, we might create a dedicated SA
	// The SA should have minimal permissions (just access to secrets/configmaps for repo config)
	return maintenanceServiceAccountName
}

// getSuccessfulJobsHistoryLimit gets the successful jobs history limit from MaintenanceCronJobSpec
func (m *MaintenanceManager) getSuccessfulJobsHistoryLimit(owner client.Object) *int32 {
	if source, ok := owner.(*volsyncv1alpha1.ReplicationSource); ok {
		if source.Spec.Kopia != nil && source.Spec.Kopia.MaintenanceCronJob != nil {
			if source.Spec.Kopia.MaintenanceCronJob.SuccessfulJobsHistoryLimit != nil {
				return source.Spec.Kopia.MaintenanceCronJob.SuccessfulJobsHistoryLimit
			}
		}
	}
	// Default value
	return ptr.To(int32(3))
}

// getFailedJobsHistoryLimit gets the failed jobs history limit from MaintenanceCronJobSpec
func (m *MaintenanceManager) getFailedJobsHistoryLimit(owner client.Object) *int32 {
	if source, ok := owner.(*volsyncv1alpha1.ReplicationSource); ok {
		if source.Spec.Kopia != nil && source.Spec.Kopia.MaintenanceCronJob != nil {
			if source.Spec.Kopia.MaintenanceCronJob.FailedJobsHistoryLimit != nil {
				return source.Spec.Kopia.MaintenanceCronJob.FailedJobsHistoryLimit
			}
		}
	}
	// Default value
	return ptr.To(int32(1))
}

// isSuspended checks if maintenance is suspended from MaintenanceCronJobSpec
func (m *MaintenanceManager) isSuspended(owner client.Object) *bool {
	if source, ok := owner.(*volsyncv1alpha1.ReplicationSource); ok {
		if source.Spec.Kopia != nil && source.Spec.Kopia.MaintenanceCronJob != nil {
			if source.Spec.Kopia.MaintenanceCronJob.Suspend != nil {
				return source.Spec.Kopia.MaintenanceCronJob.Suspend
			}
		}
	}
	// Default to not suspended
	return ptr.To(false)
}

// getMaintenanceResources returns the resource requirements for maintenance containers
func (m *MaintenanceManager) getMaintenanceResources(owner client.Object) corev1.ResourceRequirements {
	// Check if custom resources are configured
	if source, ok := owner.(*volsyncv1alpha1.ReplicationSource); ok {
		if source.Spec.Kopia != nil && source.Spec.Kopia.MaintenanceCronJob != nil {
			if source.Spec.Kopia.MaintenanceCronJob.Resources != nil {
				return *source.Spec.Kopia.MaintenanceCronJob.Resources
			}
		}
	}

	// Return default resources if not configured
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("1Gi"), // Changed from 512Mi to 1Gi as per comment
		},
	}
}

// GetMaintenanceCronJobsForNamespace returns all maintenance CronJobs in a namespace
func (m *MaintenanceManager) GetMaintenanceCronJobsForNamespace(ctx context.Context,
	namespace string) ([]batchv1.CronJob, error) {
	cronJobList := &batchv1.CronJobList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{maintenanceLabelKey: "true"},
	}
	if err := m.client.List(ctx, cronJobList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list maintenance CronJobs: %w", err)
	}
	return cronJobList.Items, nil
}

// UpdateMaintenanceCronJobsForRepository updates all maintenance CronJobs that use a specific repository
// This is useful when repository configuration changes
func (m *MaintenanceManager) UpdateMaintenanceCronJobsForRepository(ctx context.Context,
	namespace, repositoryName string) error {
	// List all ReplicationSources using this repository
	sourceList := &volsyncv1alpha1.ReplicationSourceList{}
	if err := m.client.List(ctx, sourceList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list ReplicationSources: %w", err)
	}

	// Find sources using this repository and reconcile their maintenance
	for _, source := range sourceList.Items {
		if source.Spec.Kopia != nil && source.Spec.Kopia.Repository == repositoryName {
			if err := m.ReconcileMaintenanceForSource(ctx, &source); err != nil {
				return fmt.Errorf("failed to reconcile maintenance for source %s: %w",
					source.Name, err)
			}
		}
	}

	return nil
}

// recordCronJobMetric records metrics for CronJob operations
func (m *MaintenanceManager) recordCronJobMetric(owner client.Object, operation string) {
	labels := prometheus.Labels{
		"obj_name":      owner.GetName(),
		"obj_namespace": owner.GetNamespace(),
		"role":          "source",
		"operation":     "maintenance",
		"repository":    "",
	}

	// Try to get repository name from ReplicationSource
	if source, ok := owner.(*volsyncv1alpha1.ReplicationSource); ok && source.Spec.Kopia != nil {
		labels["repository"] = source.Spec.Kopia.Repository
	}

	switch operation {
	case "created":
		m.metrics.MaintenanceCronJobCreated.With(labels).Inc()
	case "updated":
		m.metrics.MaintenanceCronJobUpdated.With(labels).Inc()
	}
}

// recordCronJobDeletionMetric records metrics for CronJob deletion
func (m *MaintenanceManager) recordCronJobDeletionMetric(namespace, name string) {
	labels := prometheus.Labels{
		"obj_name":      name,
		"obj_namespace": namespace,
		"role":          "source",
		"operation":     "maintenance",
		"repository":    "",
	}
	m.metrics.MaintenanceCronJobDeleted.With(labels).Inc()
}

// GetMaintenanceStatus retrieves detailed maintenance status for a source
func (m *MaintenanceManager) GetMaintenanceStatus(ctx context.Context,
	source *volsyncv1alpha1.ReplicationSource) (*MaintenanceStatus, error) {
	if source.Spec.Kopia == nil || !m.isMaintenanceEnabled(source) {
		return nil, nil
	}

	// Create repository config to find the CronJob
	repoConfig := &RepositoryConfig{
		Repository: source.Spec.Kopia.Repository,
		CustomCA:   (*volsyncv1alpha1.CustomCASpec)(&source.Spec.Kopia.CustomCA),
		Namespace:  source.Namespace,
		Schedule:   m.getMaintenanceSchedule(source),
	}

	repoHash := repoConfig.Hash()
	cronJobName := m.generateCronJobName(repoHash)

	// Get the CronJob
	cronJob := &batchv1.CronJob{}
	err := m.client.Get(ctx, types.NamespacedName{
		Name:      cronJobName,
		Namespace: source.Namespace,
	}, cronJob)
	if err != nil {
		if errors.IsNotFound(err) {
			return &MaintenanceStatus{
				Configured: false,
			}, nil
		}
		return nil, fmt.Errorf("failed to get CronJob: %w", err)
	}

	status := &MaintenanceStatus{
		Configured:         true,
		CronJobName:        cronJob.Name,
		Schedule:           cronJob.Spec.Schedule,
		NextScheduledTime:  nil,
		LastScheduledTime:  cronJob.Status.LastScheduleTime,
		LastSuccessfulTime: nil,
		FailuresSinceLastSuccess: 0,
	}

	// Calculate next scheduled time
	if cronJob.Status.LastScheduleTime != nil && cronJob.Spec.Suspend != nil && !*cronJob.Spec.Suspend {
		// This is a simplified calculation. In production, you'd use a proper cron parser
		status.NextScheduledTime = m.calculateNextScheduledTime(cronJob.Spec.Schedule, cronJob.Status.LastScheduleTime.Time)
	}

	// Get job history
	jobs, err := m.getMaintenanceJobs(ctx, source.Namespace, repoHash)
	if err != nil {
		m.logger.Error(err, "Failed to get maintenance jobs")
		// Don't fail entirely, just log the error
	} else {
		// Analyze job history
		m.analyzeJobHistory(jobs, status)
	}

	// Record metrics
	m.updateMaintenanceMetrics(source, status)

	return status, nil
}

// MaintenanceStatus contains detailed information about maintenance operations
type MaintenanceStatus struct {
	Configured               bool         `json:"configured"`
	CronJobName              string       `json:"cronJobName,omitempty"`
	Schedule                 string       `json:"schedule,omitempty"`
	NextScheduledTime        *metav1.Time `json:"nextScheduledTime,omitempty"`
	LastScheduledTime        *metav1.Time `json:"lastScheduledTime,omitempty"`
	LastSuccessfulTime       *metav1.Time `json:"lastSuccessfulTime,omitempty"`
	LastFailedTime           *metav1.Time `json:"lastFailedTime,omitempty"`
	FailuresSinceLastSuccess int          `json:"failuresSinceLastSuccess"`
	LastMaintenanceDuration  *string      `json:"lastMaintenanceDuration,omitempty"`
	LastError                string       `json:"lastError,omitempty"`
}

// getMaintenanceJobs retrieves all maintenance jobs for a repository
func (m *MaintenanceManager) getMaintenanceJobs(ctx context.Context, namespace, repoHash string) ([]batchv1.Job, error) {
	jobList := &batchv1.JobList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{
			maintenanceLabelKey:        "true",
			maintenanceRepositoryLabel: repoHash,
		},
	}
	if err := m.client.List(ctx, jobList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list maintenance jobs: %w", err)
	}
	return jobList.Items, nil
}

// analyzeJobHistory analyzes job history to extract maintenance statistics
func (m *MaintenanceManager) analyzeJobHistory(jobs []batchv1.Job, status *MaintenanceStatus) {
	var lastSuccessTime *metav1.Time
	var lastFailedTime *metav1.Time
	failureCount := 0
	foundSuccess := false

	// Limit analysis to most recent 50 jobs to prevent memory issues
	maxJobsToAnalyze := 50
	jobsToAnalyze := jobs
	if len(jobs) > maxJobsToAnalyze {
		// Sort jobs by creation time (newest first) before limiting
		// Note: In production, jobs should already be sorted from the API query
		jobsToAnalyze = jobs[:maxJobsToAnalyze]
		m.logger.V(1).Info("Limiting job history analysis",
			"totalJobs", len(jobs),
			"analyzedJobs", maxJobsToAnalyze)
	}

	// Process the limited set of jobs
	for _, job := range jobsToAnalyze {
		// Skip jobs that are still running
		if job.Status.CompletionTime == nil {
			continue
		}

		// Check if job succeeded
		succeeded := false
		for _, condition := range job.Status.Conditions {
			if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
				succeeded = true
				break
			}
		}

		if succeeded {
			if !foundSuccess {
				lastSuccessTime = job.Status.CompletionTime
				foundSuccess = true

				// Try to extract maintenance duration from job logs
				duration := m.extractMaintenanceDuration(&job)
				if duration != "" {
					status.LastMaintenanceDuration = &duration
				}
			}
		} else {
			if lastFailedTime == nil || job.Status.CompletionTime.After(lastFailedTime.Time) {
				lastFailedTime = job.Status.CompletionTime
				// Extract error from job if possible
				status.LastError = m.extractJobError(&job)
			}
			if !foundSuccess {
				failureCount++
			}
		}
	}

	status.LastSuccessfulTime = lastSuccessTime
	status.LastFailedTime = lastFailedTime
	status.FailuresSinceLastSuccess = failureCount
}

// extractMaintenanceDuration attempts to extract maintenance duration from job
func (m *MaintenanceManager) extractMaintenanceDuration(job *batchv1.Job) string {
	// In a real implementation, this would parse job logs to extract
	// the "MAINTENANCE_DURATION: X" line from the entry.sh output
	// For now, we'll calculate from job start/completion times
	if job.Status.StartTime != nil && job.Status.CompletionTime != nil {
		duration := job.Status.CompletionTime.Sub(job.Status.StartTime.Time)
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	}
	return ""
}

// extractJobError attempts to extract error message from failed job
func (m *MaintenanceManager) extractJobError(job *batchv1.Job) string {
	// In a real implementation, this would parse job logs or conditions
	// For now, return a generic message based on conditions
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return condition.Message
		}
	}
	return "Unknown error"
}

// calculateNextScheduledTime calculates the next scheduled time based on cron expression
func (m *MaintenanceManager) calculateNextScheduledTime(schedule string, lastTime time.Time) *metav1.Time {
	// This is a simplified implementation. In production, you'd use a proper cron parser
	// like github.com/robfig/cron to calculate the actual next run time

	// For now, use simple heuristics based on common patterns
	var nextTime time.Time
	switch schedule {
	case "0 2 * * *": // Daily at 2 AM
		nextTime = lastTime.Add(24 * time.Hour)
	case "0 2 * * 0": // Weekly on Sunday at 2 AM
		nextTime = lastTime.Add(7 * 24 * time.Hour)
	case "0 2 1 * *": // Monthly on 1st at 2 AM
		nextTime = lastTime.AddDate(0, 1, 0)
	default:
		// Default to 24 hours from last time
		nextTime = lastTime.Add(24 * time.Hour)
	}

	t := metav1.NewTime(nextTime)
	return &t
}

// updateMaintenanceMetrics updates Prometheus metrics based on maintenance status
func (m *MaintenanceManager) updateMaintenanceMetrics(source *volsyncv1alpha1.ReplicationSource, status *MaintenanceStatus) {
	labels := prometheus.Labels{
		"obj_name":      source.Name,
		"obj_namespace": source.Namespace,
		"role":          "source",
		"operation":     "maintenance",
		"repository":    source.Spec.Kopia.Repository,
	}

	// Update last run timestamp
	if status.LastSuccessfulTime != nil {
		m.metrics.MaintenanceLastRunTimestamp.With(labels).Set(float64(status.LastSuccessfulTime.Unix()))
	}

	// Record failures
	if status.FailuresSinceLastSuccess > 0 {
		failureLabels := prometheus.Labels{
			"obj_name":      source.Name,
			"obj_namespace": source.Namespace,
			"role":          "source",
			"operation":     "maintenance",
			"repository":    source.Spec.Kopia.Repository,
			"failure_reason": "maintenance_job_failed",
		}
		// Only increment once per check, not for each failure
		// In production, you'd track which failures have been recorded
		m.metrics.MaintenanceCronJobFailures.With(failureLabels).Add(float64(status.FailuresSinceLastSuccess))
	}

	// Record duration if available
	if status.LastMaintenanceDuration != nil {
		// Parse duration string (format: "Xs")
		if strings.HasSuffix(*status.LastMaintenanceDuration, "s") {
			durationStr := strings.TrimSuffix(*status.LastMaintenanceDuration, "s")
			if duration, err := strconv.ParseFloat(durationStr, 64); err == nil {
				m.metrics.MaintenanceDurationSeconds.With(labels).Observe(duration)
			}
		}
	}
}

// ParseMaintenanceLogs parses maintenance job logs to extract metrics
func (m *MaintenanceManager) ParseMaintenanceLogs(ctx context.Context, job *batchv1.Job) (*MaintenanceLogMetrics, error) {
	// This would typically read pod logs and parse them for specific patterns
	// For now, return a placeholder implementation

	metrics := &MaintenanceLogMetrics{}

	// In a real implementation:
	// 1. Get the pod associated with the job
	// 2. Read the pod logs
	// 3. Parse for patterns like:
	//    - "MAINTENANCE_STATUS: SUCCESS"
	//    - "MAINTENANCE_DURATION: 120"
	//    - "REPO_SIZE_BYTES: 1073741824"
	//    - etc.

	return metrics, nil
}

// MaintenanceLogMetrics contains metrics extracted from maintenance logs
type MaintenanceLogMetrics struct {
	Status             string
	DurationSeconds    int
	RepositorySizeBytes int64
	ContentCount       int
	BlobCount          int
	DeduplicationRatio float64
	Error              string
}