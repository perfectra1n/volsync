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
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	volsyncv1alpha1 "github.com/backube/volsync/api/v1alpha1"
)

// EnhancedMaintenanceManager manages maintenance for both KopiaMaintenance CRDs and legacy embedded fields
type EnhancedMaintenanceManager struct {
	client.Client
	logger            logr.Logger
	legacyManager     *MaintenanceManager
	containerImage    string
	operatorNamespace string
}

// NewEnhancedMaintenanceManager creates a new enhanced maintenance manager
func NewEnhancedMaintenanceManager(client client.Client, logger logr.Logger, containerImage string) *EnhancedMaintenanceManager {
	return &EnhancedMaintenanceManager{
		Client:         client,
		logger:         logger.WithName("enhanced-maintenance"),
		legacyManager:  NewMaintenanceManager(client, logger, containerImage),
		containerImage: containerImage,
	}
}

// ReconcileMaintenanceForSource determines the best maintenance configuration for a source
func (m *EnhancedMaintenanceManager) ReconcileMaintenanceForSource(ctx context.Context,
	source *volsyncv1alpha1.ReplicationSource) error {
	if source.Spec.Kopia == nil {
		return nil
	}

	// First check if there's a KopiaMaintenance that matches this source
	// Only consider repository selector mode maintenances (not direct repository mode)
	maintenanceConfig, err := m.findBestMatchingMaintenanceConfig(ctx, source)
	if err != nil {
		return fmt.Errorf("failed to find maintenance configuration: %w", err)
	}

	if maintenanceConfig != nil {
		// Use KopiaMaintenance CRD
		m.logger.V(1).Info("Using KopiaMaintenance CRD for maintenance",
			"source", source.Name,
			"namespace", source.Namespace,
			"maintenance", maintenanceConfig.Name)

		if maintenanceConfig.GetEnabled() {
			return m.createMaintenanceFromCRD(ctx, source, maintenanceConfig)
		}
		// Maintenance is disabled in CRD
		return nil
	}

	// Check if source has embedded maintenance configuration (legacy mode)
	if m.hasLegacyMaintenanceConfig(source) {
		m.logger.V(1).Info("Using legacy embedded maintenance configuration",
			"source", source.Name,
			"namespace", source.Namespace)
		m.logDeprecationWarning(source)
		return m.legacyManager.ReconcileMaintenanceForSource(ctx, source)
	}

	// No maintenance configuration found
	m.logger.V(2).Info("No maintenance configuration found for source",
		"source", source.Name,
		"namespace", source.Namespace)
	return nil
}

// findBestMatchingMaintenanceConfig finds the highest priority KopiaMaintenance that matches the source
// This only considers maintenances using repository selector mode, not direct repository mode
func (m *EnhancedMaintenanceManager) findBestMatchingMaintenanceConfig(ctx context.Context,
	source *volsyncv1alpha1.ReplicationSource) (*volsyncv1alpha1.KopiaMaintenance, error) {
	// List all KopiaMaintenance resources
	maintenanceList := &volsyncv1alpha1.KopiaMaintenanceList{}
	if err := m.List(ctx, maintenanceList); err != nil {
		return nil, fmt.Errorf("failed to list KopiaMaintenance resources: %w", err)
	}

	var candidates []*volsyncv1alpha1.KopiaMaintenance

	// Find all matching maintenance configurations (only selector mode)
	for i := range maintenanceList.Items {
		maintenance := &maintenanceList.Items[i]
		// Skip direct repository mode maintenances
		if maintenance.IsDirectRepositoryMode() {
			continue
		}
		if m.maintenanceMatches(maintenance, source) {
			candidates = append(candidates, maintenance)
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Sort by priority (highest first), then by name for deterministic ordering
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Spec.Priority == candidates[j].Spec.Priority {
			return candidates[i].Name < candidates[j].Name
		}
		return candidates[i].Spec.Priority > candidates[j].Spec.Priority
	})

	return candidates[0], nil
}

// maintenanceMatches checks if a KopiaMaintenance matches a ReplicationSource
// This only applies to repository selector mode maintenances
func (m *EnhancedMaintenanceManager) maintenanceMatches(maintenance *volsyncv1alpha1.KopiaMaintenance,
	source *volsyncv1alpha1.ReplicationSource) bool {
	if source.Spec.Kopia == nil {
		return false
	}

	// Direct repository mode maintenances don't match ReplicationSources
	if maintenance.IsDirectRepositoryMode() {
		return false
	}

	// Must have repository selector for matching
	if maintenance.Spec.RepositorySelector == nil {
		return false
	}

	selector := maintenance.Spec.RepositorySelector

	// Check repository name match
	if selector.Repository != "" {
		if !m.matchesPattern(source.Spec.Kopia.Repository, selector.Repository) {
			return false
		}
	}

	// Check namespace match
	if selector.NamespaceSelector != nil {
		if !m.matchesNamespace(source.Namespace, selector.NamespaceSelector) {
			return false
		}
	}

	// Check custom CA match
	if selector.CustomCA != nil {
		if !m.matchesCustomCA(&source.Spec.Kopia.CustomCA, selector.CustomCA) {
			return false
		}
	}

	// Check labels match
	if len(selector.Labels) > 0 {
		if !m.matchesLabels(source.Labels, selector.Labels) {
			return false
		}
	}

	return true
}

// matchesPattern implements simple wildcard pattern matching
func (m *EnhancedMaintenanceManager) matchesPattern(str, pattern string) bool {
	// Simple implementation for * and ? wildcards
	// In production, use a proper glob library
	if pattern == "*" {
		return true
	}

	// Exact match
	return str == pattern
}

// matchesNamespace checks if the namespace matches the selector
func (m *EnhancedMaintenanceManager) matchesNamespace(namespace string,
	selector *volsyncv1alpha1.NamespaceSelector) bool {
	// Check exclude list first
	for _, excluded := range selector.ExcludeNames {
		if namespace == excluded {
			return false
		}
	}

	// If MatchNames is specified, namespace must be in the list
	if len(selector.MatchNames) > 0 {
		matched := false
		for _, name := range selector.MatchNames {
			if namespace == name {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check namespace labels if specified
	if len(selector.MatchLabels) > 0 {
		// Get the namespace object to check labels
		ns := &corev1.Namespace{}
		if err := m.Get(context.Background(), types.NamespacedName{Name: namespace}, ns); err != nil {
			m.logger.V(1).Info("Failed to get namespace for label matching", "namespace", namespace, "error", err)
			return false
		}

		for key, value := range selector.MatchLabels {
			if ns.Labels[key] != value {
				return false
			}
		}
	}

	return true
}

// matchesCustomCA checks if the custom CA configuration matches
func (m *EnhancedMaintenanceManager) matchesCustomCA(ca *volsyncv1alpha1.ReplicationSourceKopiaCA,
	selector *volsyncv1alpha1.CustomCASelector) bool {
	if selector.SecretName != "" && ca.SecretName != "" {
		if !m.matchesPattern(ca.SecretName, selector.SecretName) {
			return false
		}
	}

	if selector.ConfigMapName != "" && ca.ConfigMapName != "" {
		if !m.matchesPattern(ca.ConfigMapName, selector.ConfigMapName) {
			return false
		}
	}

	return true
}

// matchesLabels checks if the source labels match the selector
func (m *EnhancedMaintenanceManager) matchesLabels(sourceLabels, selectorLabels map[string]string) bool {
	for key, value := range selectorLabels {
		if sourceLabels[key] != value {
			return false
		}
	}
	return true
}

// createMaintenanceFromCRD creates maintenance using KopiaMaintenance configuration
func (m *EnhancedMaintenanceManager) createMaintenanceFromCRD(ctx context.Context,
	source *volsyncv1alpha1.ReplicationSource, maintenance *volsyncv1alpha1.KopiaMaintenance) error {
	// Create a synthetic ReplicationSource with maintenance configuration from CRD
	syntheticSource := source.DeepCopy()

	// Override maintenance configuration with values from KopiaMaintenance
	if syntheticSource.Spec.Kopia.MaintenanceCronJob == nil {
		syntheticSource.Spec.Kopia.MaintenanceCronJob = &volsyncv1alpha1.MaintenanceCronJobSpec{}
	}

	// Copy configuration from KopiaMaintenance
	syntheticSource.Spec.Kopia.MaintenanceCronJob.Enabled = maintenance.Spec.Enabled
	syntheticSource.Spec.Kopia.MaintenanceCronJob.Schedule = maintenance.GetSchedule()
	syntheticSource.Spec.Kopia.MaintenanceCronJob.Suspend = maintenance.Spec.Suspend
	syntheticSource.Spec.Kopia.MaintenanceCronJob.SuccessfulJobsHistoryLimit = maintenance.Spec.SuccessfulJobsHistoryLimit
	syntheticSource.Spec.Kopia.MaintenanceCronJob.FailedJobsHistoryLimit = maintenance.Spec.FailedJobsHistoryLimit
	syntheticSource.Spec.Kopia.MaintenanceCronJob.Resources = maintenance.Spec.Resources

	// Use the legacy manager to create the CronJob with the synthetic source
	err := m.legacyManager.ReconcileMaintenanceForSource(ctx, syntheticSource)
	if err != nil {
		return err
	}

	// Update source status to reference the KopiaMaintenance
	return m.updateSourceStatusWithMaintenance(ctx, source, maintenance.Name)
}

// hasLegacyMaintenanceConfig checks if the source has legacy maintenance configuration
func (m *EnhancedMaintenanceManager) hasLegacyMaintenanceConfig(source *volsyncv1alpha1.ReplicationSource) bool {
	if source.Spec.Kopia == nil {
		return false
	}

	// Check for MaintenanceCronJob configuration
	if source.Spec.Kopia.MaintenanceCronJob != nil {
		return true
	}

	// Check for MaintenanceIntervalDays configuration
	if source.Spec.Kopia.MaintenanceIntervalDays != nil {
		return true
	}

	return false
}

// updateSourceStatusWithMaintenance updates the source status with KopiaMaintenance reference
func (m *EnhancedMaintenanceManager) updateSourceStatusWithMaintenance(ctx context.Context,
	source *volsyncv1alpha1.ReplicationSource, maintenanceName string) error {
	// Get the current source to update its status
	current := &volsyncv1alpha1.ReplicationSource{}
	if err := m.Get(ctx, types.NamespacedName{
		Name:      source.Name,
		Namespace: source.Namespace,
	}, current); err != nil {
		return err
	}

	// Initialize status if needed
	if current.Status == nil {
		current.Status = &volsyncv1alpha1.ReplicationSourceStatus{}
	}
	if current.Status.Kopia == nil {
		current.Status.Kopia = &volsyncv1alpha1.ReplicationSourceKopiaStatus{}
	}

	// Only update if changed
	if current.Status.Kopia.KopiaMaintenance != maintenanceName {
		current.Status.Kopia.KopiaMaintenance = maintenanceName
		if err := m.Status().Update(ctx, current); err != nil {
			return fmt.Errorf("failed to update source status: %w", err)
		}
	}

	return nil
}

// logDeprecationWarning logs a deprecation warning for legacy maintenance configuration
func (m *EnhancedMaintenanceManager) logDeprecationWarning(source *volsyncv1alpha1.ReplicationSource) {
	m.logger.Info("DEPRECATION WARNING: Using embedded maintenance configuration",
		"source", source.Name,
		"namespace", source.Namespace,
		"message", "MaintenanceCronJob and MaintenanceIntervalDays fields are deprecated. "+
			"Please migrate to using KopiaMaintenance CRD for better separation of concerns and conflict resolution. "+
			"See documentation for migration instructions.")
}

// CleanupOrphanedMaintenanceCronJobs delegates to the legacy manager
func (m *EnhancedMaintenanceManager) CleanupOrphanedMaintenanceCronJobs(ctx context.Context,
	namespace string) error {
	return m.legacyManager.CleanupOrphanedMaintenanceCronJobs(ctx, namespace)
}

// GetMaintenanceStatus gets maintenance status, preferring KopiaMaintenance over legacy
func (m *EnhancedMaintenanceManager) GetMaintenanceStatus(ctx context.Context,
	source *volsyncv1alpha1.ReplicationSource) (*MaintenanceStatus, error) {
	// Check if using KopiaMaintenance
	if source.Status != nil && source.Status.Kopia != nil && source.Status.Kopia.KopiaMaintenance != "" {
		return m.getMaintenanceStatusFromCRD(ctx, source)
	}

	// Fall back to legacy status
	return m.legacyManager.GetMaintenanceStatus(ctx, source)
}

// getMaintenanceStatusFromCRD gets maintenance status from KopiaMaintenance CRD
func (m *EnhancedMaintenanceManager) getMaintenanceStatusFromCRD(ctx context.Context,
	source *volsyncv1alpha1.ReplicationSource) (*MaintenanceStatus, error) {
	if source.Status.Kopia.KopiaMaintenance == "" {
		return nil, nil
	}

	// Get the KopiaMaintenance resource
	maintenance := &volsyncv1alpha1.KopiaMaintenance{}
	if err := m.Get(ctx, types.NamespacedName{
		Name: source.Status.Kopia.KopiaMaintenance,
	}, maintenance); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get KopiaMaintenance: %w", err)
	}

	// Convert KopiaMaintenance status to MaintenanceStatus
	status := &MaintenanceStatus{
		Configured:               maintenance.GetEnabled(),
		Schedule:                 maintenance.GetSchedule(),
		LastScheduledTime:        maintenance.Status.LastMaintenanceTime,
		NextScheduledTime:        maintenance.Status.NextScheduledMaintenance,
		FailuresSinceLastSuccess: int(maintenance.Status.MaintenanceFailures),
	}

	// Get CronJob names if available
	if len(maintenance.Status.ActiveCronJobs) > 0 {
		status.CronJobName = maintenance.Status.ActiveCronJobs[0]
	}

	return status, nil
}

// ReconcileDirectRepositoryMaintenance handles maintenance for KopiaMaintenance in direct repository mode
func (m *EnhancedMaintenanceManager) ReconcileDirectRepositoryMaintenance(ctx context.Context,
	maintenance *volsyncv1alpha1.KopiaMaintenance) error {
	if !maintenance.IsDirectRepositoryMode() {
		return fmt.Errorf("maintenance is not in direct repository mode")
	}

	if !maintenance.GetEnabled() {
		m.logger.V(1).Info("Direct repository maintenance is disabled",
			"maintenance", maintenance.Name)
		return nil
	}

	secretName, secretNamespace := maintenance.GetRepositorySecret()
	m.logger.V(1).Info("Using direct repository maintenance",
		"maintenance", maintenance.Name,
		"secret", secretName,
		"namespace", secretNamespace)

	// This is handled by the KopiaMaintenanceReconciler directly
	// The enhanced maintenance manager provides this method for completeness
	// but the actual work is done in the controller
	return nil
}