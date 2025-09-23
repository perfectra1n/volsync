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

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	volsyncv1alpha1 "github.com/backube/volsync/api/v1alpha1"
	"github.com/backube/volsync/internal/controller/mover/kopia"
)

// KopiaMaintenanceReconciler reconciles a KopiaMaintenance object
type KopiaMaintenanceReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Log            logr.Logger
	EventRecorder  record.EventRecorder
	containerImage string
}

// SetupWithManager sets up the controller with the Manager.
func (r *KopiaMaintenanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Watch KopiaMaintenance resources
	return ctrl.NewControllerManagedBy(mgr).
		For(&volsyncv1alpha1.KopiaMaintenance{}).
		// Watch ReplicationSources and reconcile matching KopiaMaintenance resources
		Watches(&volsyncv1alpha1.ReplicationSource{}, handler.EnqueueRequestsFromMapFunc(r.findMatchingKopiaMaintenances)).
		// Watch CronJobs created by KopiaMaintenance
		Owns(&batchv1.CronJob{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 3,
		}).
		Complete(r)
}

// findMatchingKopiaMaintenances finds KopiaMaintenance resources that match a ReplicationSource
func (r *KopiaMaintenanceReconciler) findMatchingKopiaMaintenances(ctx context.Context, obj client.Object) []reconcile.Request {
	source, ok := obj.(*volsyncv1alpha1.ReplicationSource)
	if !ok || source.Spec.Kopia == nil {
		return nil
	}

	// List all KopiaMaintenance resources
	maintenanceList := &volsyncv1alpha1.KopiaMaintenanceList{}
	if err := r.List(ctx, maintenanceList); err != nil {
		r.Log.Error(err, "Failed to list KopiaMaintenance resources")
		return nil
	}

	var requests []reconcile.Request
	for _, maintenance := range maintenanceList.Items {
		if r.maintenanceMatchesSource(&maintenance, source) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: maintenance.Name,
				},
			})
		}
	}

	return requests
}

// maintenanceMatchesSource checks if a KopiaMaintenance matches a ReplicationSource
func (r *KopiaMaintenanceReconciler) maintenanceMatchesSource(maintenance *volsyncv1alpha1.KopiaMaintenance, source *volsyncv1alpha1.ReplicationSource) bool {
	if source.Spec.Kopia == nil {
		return false
	}

	selector := maintenance.Spec.RepositorySelector

	// Check repository name match
	if selector.Repository != "" {
		if !r.matchesPattern(source.Spec.Kopia.Repository, selector.Repository) {
			return false
		}
	}

	// Check namespace match
	if selector.NamespaceSelector != nil {
		if !r.matchesNamespace(source.Namespace, selector.NamespaceSelector) {
			return false
		}
	}

	// Check custom CA match
	if selector.CustomCA != nil {
		if !r.matchesCustomCA(&source.Spec.Kopia.CustomCA, selector.CustomCA) {
			return false
		}
	}

	// Check labels match
	if len(selector.Labels) > 0 {
		if !r.matchesLabels(source.Labels, selector.Labels) {
			return false
		}
	}

	return true
}

// matchesPattern checks if a string matches a pattern with wildcards
func (r *KopiaMaintenanceReconciler) matchesPattern(str, pattern string) bool {
	// Convert wildcard pattern to simple matching
	if pattern == "*" {
		return true
	}

	// Support * and ? wildcards
	// * matches any sequence of characters
	// ? matches any single character
	pattern = strings.ReplaceAll(pattern, "*", ".*")
	pattern = strings.ReplaceAll(pattern, "?", ".")
	pattern = "^" + pattern + "$"

	// For simplicity, we'll do basic string matching here
	// In production, use regexp.MatchString
	if strings.HasPrefix(pattern, "^.*") && strings.HasSuffix(pattern, ".*$") {
		contains := pattern[3 : len(pattern)-3]
		return strings.Contains(str, contains)
	} else if strings.HasPrefix(pattern, "^.*") {
		suffix := pattern[3 : len(pattern)-1]
		return strings.HasSuffix(str, suffix)
	} else if strings.HasSuffix(pattern, ".*$") {
		prefix := pattern[1 : len(pattern)-3]
		return strings.HasPrefix(str, prefix)
	}

	// Exact match
	return str == strings.Trim(pattern, "^$")
}

// matchesNamespace checks if the namespace matches the selector
func (r *KopiaMaintenanceReconciler) matchesNamespace(namespace string, selector *volsyncv1alpha1.NamespaceSelector) bool {
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
		if err := r.Get(context.Background(), types.NamespacedName{Name: namespace}, ns); err != nil {
			r.Log.V(1).Info("Failed to get namespace for label matching", "namespace", namespace, "error", err)
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
func (r *KopiaMaintenanceReconciler) matchesCustomCA(ca *volsyncv1alpha1.ReplicationSourceKopiaCA, selector *volsyncv1alpha1.CustomCASelector) bool {
	if selector.SecretName != "" && ca.SecretName != "" {
		if !r.matchesPattern(ca.SecretName, selector.SecretName) {
			return false
		}
	}

	if selector.ConfigMapName != "" && ca.ConfigMapName != "" {
		if !r.matchesPattern(ca.ConfigMapName, selector.ConfigMapName) {
			return false
		}
	}

	return true
}

// matchesLabels checks if the source labels match the selector
func (r *KopiaMaintenanceReconciler) matchesLabels(sourceLabels, selectorLabels map[string]string) bool {
	for key, value := range selectorLabels {
		if sourceLabels[key] != value {
			return false
		}
	}
	return true
}

// +kubebuilder:rbac:groups=volsync.backube,resources=kopiamaintenances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=volsync.backube,resources=kopiamaintenances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=volsync.backube,resources=kopiamaintenances/finalizers,verbs=update
// +kubebuilder:rbac:groups=volsync.backube,resources=replicationsources,verbs=get;list;watch
// +kubebuilder:rbac:groups=volsync.backube,resources=replicationsources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile is the main reconciliation loop for KopiaMaintenance resources
func (r *KopiaMaintenanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("kopiamaintenance", req.NamespacedName)

	// Fetch the KopiaMaintenance instance
	maintenance := &volsyncv1alpha1.KopiaMaintenance{}
	err := r.Get(ctx, req.NamespacedName, maintenance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, could have been deleted
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Validate the KopiaMaintenance configuration
	if err := maintenance.Validate(); err != nil {
		logger.Error(err, "Invalid KopiaMaintenance configuration")
		r.EventRecorder.Event(maintenance, corev1.EventTypeWarning, "ValidationFailed", err.Error())
		return ctrl.Result{}, r.updateStatus(ctx, maintenance, nil, nil)
	}

	// Check if maintenance is enabled
	if !maintenance.GetEnabled() {
		logger.V(1).Info("Maintenance is disabled")
		// Clean up any existing CronJobs
		if err := r.cleanupCronJobs(ctx, maintenance); err != nil {
			logger.Error(err, "Failed to cleanup CronJobs")
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}
		return ctrl.Result{}, r.updateStatus(ctx, maintenance, nil, nil)
	}

	// Handle different maintenance modes
	var activeCronJobs []string
	var matchedSources []*volsyncv1alpha1.ReplicationSource

	if maintenance.IsDirectRepositoryMode() {
		// Direct repository mode - create maintenance for the specified repository
		logger.V(1).Info("Using direct repository mode")
		cronJobName, err := r.ensureCronJobForDirectRepository(ctx, maintenance)
		if err != nil {
			logger.Error(err, "Failed to ensure CronJob for direct repository")
			r.EventRecorder.Event(maintenance, corev1.EventTypeWarning, "CronJobFailed",
				fmt.Sprintf("Failed to ensure CronJob for direct repository: %v", err))
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}
		if cronJobName != "" {
			activeCronJobs = append(activeCronJobs, cronJobName)
		}
	} else {
		// Repository selector mode - find matching ReplicationSources
		logger.V(1).Info("Using repository selector mode")
		var err error
		matchedSources, err = r.findMatchingSources(ctx, maintenance)
		if err != nil {
			logger.Error(err, "Failed to find matching ReplicationSources")
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}

		if len(matchedSources) == 0 {
			logger.V(1).Info("No matching ReplicationSources found")
			// Clean up any orphaned CronJobs
			if err := r.cleanupCronJobs(ctx, maintenance); err != nil {
				logger.Error(err, "Failed to cleanup CronJobs")
				return ctrl.Result{RequeueAfter: time.Minute}, err
			}
			return ctrl.Result{}, r.updateStatus(ctx, maintenance, matchedSources, nil)
		}

		// Group sources by repository configuration
		repoGroups := r.groupSourcesByRepository(matchedSources)

		// Create or update CronJobs for each repository group
		for repoKey, sources := range repoGroups {
			cronJobName, err := r.ensureCronJobForRepository(ctx, maintenance, repoKey, sources)
			if err != nil {
				logger.Error(err, "Failed to ensure CronJob for repository", "repository", repoKey)
				r.EventRecorder.Event(maintenance, corev1.EventTypeWarning, "CronJobFailed",
					fmt.Sprintf("Failed to ensure CronJob for repository %s: %v", repoKey, err))
				continue
			}
			activeCronJobs = append(activeCronJobs, cronJobName)

			// Update ReplicationSource status to reference this KopiaMaintenance
			for _, source := range sources {
				if err := r.updateSourceStatus(ctx, source, maintenance.Name); err != nil {
					logger.Error(err, "Failed to update source status", "source", source.Name)
				}
			}
		}
	}

	// Update status
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, r.updateStatus(ctx, maintenance, matchedSources, activeCronJobs)
}

// findMatchingSources finds all ReplicationSources that match this KopiaMaintenance
func (r *KopiaMaintenanceReconciler) findMatchingSources(ctx context.Context, maintenance *volsyncv1alpha1.KopiaMaintenance) ([]*volsyncv1alpha1.ReplicationSource, error) {
	// List all ReplicationSources
	sourceList := &volsyncv1alpha1.ReplicationSourceList{}
	if err := r.List(ctx, sourceList); err != nil {
		return nil, fmt.Errorf("failed to list ReplicationSources: %w", err)
	}

	// Check for conflicts with other KopiaMaintenance resources
	conflictingMaintenances, err := r.findConflictingMaintenances(ctx, maintenance)
	if err != nil {
		r.Log.Error(err, "Failed to find conflicting maintenances")
	}

	var matchedSources []*volsyncv1alpha1.ReplicationSource
	for i := range sourceList.Items {
		source := &sourceList.Items[i]
		if r.maintenanceMatchesSource(maintenance, source) {
			// Check if this source is claimed by a higher priority maintenance
			if !r.isHighestPriority(maintenance, source, conflictingMaintenances) {
				r.Log.V(1).Info("Source matched but claimed by higher priority maintenance",
					"source", source.Name,
					"namespace", source.Namespace)
				continue
			}
			matchedSources = append(matchedSources, source)
		}
	}

	return matchedSources, nil
}

// findConflictingMaintenances finds other KopiaMaintenance resources that might conflict
func (r *KopiaMaintenanceReconciler) findConflictingMaintenances(ctx context.Context, current *volsyncv1alpha1.KopiaMaintenance) ([]*volsyncv1alpha1.KopiaMaintenance, error) {
	maintenanceList := &volsyncv1alpha1.KopiaMaintenanceList{}
	if err := r.List(ctx, maintenanceList); err != nil {
		return nil, err
	}

	var conflicts []*volsyncv1alpha1.KopiaMaintenance
	for i := range maintenanceList.Items {
		maintenance := &maintenanceList.Items[i]
		if maintenance.Name != current.Name {
			conflicts = append(conflicts, maintenance)
		}
	}

	return conflicts, nil
}

// isHighestPriority checks if this maintenance has the highest priority for a source
func (r *KopiaMaintenanceReconciler) isHighestPriority(maintenance *volsyncv1alpha1.KopiaMaintenance, source *volsyncv1alpha1.ReplicationSource, others []*volsyncv1alpha1.KopiaMaintenance) bool {
	for _, other := range others {
		if r.maintenanceMatchesSource(other, source) {
			// If other maintenance has higher priority, this one doesn't win
			if other.Spec.Priority > maintenance.Spec.Priority {
				return false
			}
			// If priorities are equal, use name for deterministic ordering
			if other.Spec.Priority == maintenance.Spec.Priority && other.Name < maintenance.Name {
				return false
			}
		}
	}
	return true
}

// RepositoryKey uniquely identifies a repository configuration
type RepositoryKey struct {
	Repository string
	CustomCA   *volsyncv1alpha1.ReplicationSourceKopiaCA
}

// String returns a string representation of the repository key
func (rk RepositoryKey) String() string {
	key := rk.Repository
	if rk.CustomCA != nil {
		if rk.CustomCA.SecretName != "" {
			key += "-ca-secret-" + rk.CustomCA.SecretName
		}
		if rk.CustomCA.ConfigMapName != "" {
			key += "-ca-cm-" + rk.CustomCA.ConfigMapName
		}
	}
	return key
}

// groupSourcesByRepository groups ReplicationSources by their repository configuration
func (r *KopiaMaintenanceReconciler) groupSourcesByRepository(sources []*volsyncv1alpha1.ReplicationSource) map[RepositoryKey][]*volsyncv1alpha1.ReplicationSource {
	groups := make(map[RepositoryKey][]*volsyncv1alpha1.ReplicationSource)

	for _, source := range sources {
		if source.Spec.Kopia != nil {
			key := RepositoryKey{
				Repository: source.Spec.Kopia.Repository,
				CustomCA:   &source.Spec.Kopia.CustomCA,
			}
			groups[key] = append(groups[key], source)
		}
	}

	return groups
}

// ensureCronJobForDirectRepository creates or updates a CronJob for direct repository mode
func (r *KopiaMaintenanceReconciler) ensureCronJobForDirectRepository(ctx context.Context, maintenance *volsyncv1alpha1.KopiaMaintenance) (string, error) {
	if !maintenance.IsDirectRepositoryMode() {
		return "", fmt.Errorf("maintenance is not in direct repository mode")
	}

	// Copy the secret to the operator namespace if necessary
	sourceSecretName, sourceNamespace := maintenance.GetRepositorySecret()
	if sourceNamespace == "" {
		// If no namespace specified, assume it's already in the operator namespace
		sourceNamespace = r.getOperatorNamespace()
	}

	operatorNamespace := r.getOperatorNamespace()
	var targetSecretName string

	if sourceNamespace != operatorNamespace {
		// Copy the secret to the operator namespace
		var err error
		targetSecretName, err = r.copySecretToOperatorNamespace(ctx, sourceSecretName, sourceNamespace, maintenance.Name)
		if err != nil {
			return "", fmt.Errorf("failed to copy secret to operator namespace: %w", err)
		}
	} else {
		// Secret is already in the operator namespace
		targetSecretName = sourceSecretName
	}

	// Create a synthetic ReplicationSource for the maintenance manager
	syntheticSource := &volsyncv1alpha1.ReplicationSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("synthetic-%s", maintenance.Name),
			Namespace: operatorNamespace,
		},
		Spec: volsyncv1alpha1.ReplicationSourceSpec{
			Kopia: &volsyncv1alpha1.ReplicationSourceKopiaSpec{
				Repository: targetSecretName,
				MaintenanceCronJob: &volsyncv1alpha1.MaintenanceCronJobSpec{
					Enabled:                    maintenance.Spec.Enabled,
					Schedule:                   maintenance.GetSchedule(),
					Suspend:                    maintenance.Spec.Suspend,
					SuccessfulJobsHistoryLimit: maintenance.Spec.SuccessfulJobsHistoryLimit,
					FailedJobsHistoryLimit:     maintenance.Spec.FailedJobsHistoryLimit,
					Resources:                  maintenance.Spec.Resources,
				},
			},
		},
	}

	// Set CustomCA if specified
	if maintenance.Spec.Repository.CustomCA != nil {
		syntheticSource.Spec.Kopia.CustomCA = *maintenance.Spec.Repository.CustomCA
	}

	// Create maintenance manager
	mgr := kopia.NewMaintenanceManager(r.Client, r.Log, r.containerImage)

	// Ensure the CronJob exists
	if err := mgr.EnsureMaintenanceCronJob(ctx, syntheticSource); err != nil {
		return "", err
	}

	// Generate the CronJob name based on the maintenance resource
	return fmt.Sprintf("kopia-maintenance-%s", maintenance.Name), nil
}

// ensureCronJobForRepository creates or updates a CronJob for a repository
func (r *KopiaMaintenanceReconciler) ensureCronJobForRepository(ctx context.Context, maintenance *volsyncv1alpha1.KopiaMaintenance, repoKey RepositoryKey, sources []*volsyncv1alpha1.ReplicationSource) (string, error) {
	if len(sources) == 0 {
		return "", nil
	}

	// Use the first source as a template (all sources in group share same repository config)
	templateSource := sources[0]

	// Create maintenance manager
	mgr := kopia.NewMaintenanceManager(r.Client, r.Log, r.containerImage)

	// Create a synthetic ReplicationSource with KopiaMaintenance configuration
	syntheticSource := &volsyncv1alpha1.ReplicationSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      templateSource.Name,
			Namespace: templateSource.Namespace,
		},
		Spec: volsyncv1alpha1.ReplicationSourceSpec{
			Kopia: &volsyncv1alpha1.ReplicationSourceKopiaSpec{
				Repository: templateSource.Spec.Kopia.Repository,
				CustomCA:   templateSource.Spec.Kopia.CustomCA,
				// Use maintenance configuration from KopiaMaintenance
				MaintenanceCronJob: &volsyncv1alpha1.MaintenanceCronJobSpec{
					Enabled:                    maintenance.Spec.Enabled,
					Schedule:                   maintenance.Spec.Schedule,
					Suspend:                    maintenance.Spec.Suspend,
					SuccessfulJobsHistoryLimit: maintenance.Spec.SuccessfulJobsHistoryLimit,
					FailedJobsHistoryLimit:     maintenance.Spec.FailedJobsHistoryLimit,
					Resources:                  maintenance.Spec.Resources,
				},
			},
		},
	}

	// Ensure the CronJob exists
	if err := mgr.EnsureMaintenanceCronJob(ctx, syntheticSource); err != nil {
		return "", err
	}

	// Return the CronJob name
	// The maintenance manager generates a deterministic name based on repository hash
	return fmt.Sprintf("kopia-maintenance-%s", repoKey.String()[:16]), nil
}

// cleanupCronJobs removes CronJobs managed by this KopiaMaintenance
func (r *KopiaMaintenanceReconciler) cleanupCronJobs(ctx context.Context, maintenance *volsyncv1alpha1.KopiaMaintenance) error {
	// List CronJobs with our labels
	cronJobList := &batchv1.CronJobList{}
	if err := r.List(ctx, cronJobList, client.MatchingLabels{
		"volsync.backube/kopia-maintenance": "true",
		"volsync.backube/managed-by":        maintenance.Name,
	}); err != nil {
		return err
	}

	for _, cronJob := range cronJobList.Items {
		if err := r.Delete(ctx, &cronJob); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	// Also cleanup copied secrets if using direct repository mode
	if maintenance.IsDirectRepositoryMode() {
		if err := r.cleanupCopiedSecrets(ctx, maintenance.Name); err != nil {
			r.Log.Error(err, "Failed to cleanup copied secrets")
			// Don't fail the cleanup process
		}
	}

	return nil
}

// updateSourceStatus updates the ReplicationSource status with KopiaMaintenance reference
func (r *KopiaMaintenanceReconciler) updateSourceStatus(ctx context.Context, source *volsyncv1alpha1.ReplicationSource, maintenanceName string) error {
	// Update the source status
	if source.Status == nil {
		source.Status = &volsyncv1alpha1.ReplicationSourceStatus{}
	}
	if source.Status.Kopia == nil {
		source.Status.Kopia = &volsyncv1alpha1.ReplicationSourceKopiaStatus{}
	}

	// Only update if changed
	if source.Status.Kopia.KopiaMaintenance != maintenanceName {
		source.Status.Kopia.KopiaMaintenance = maintenanceName
		if err := r.Status().Update(ctx, source); err != nil {
			return err
		}
	}

	return nil
}

// updateStatus updates the KopiaMaintenance status
func (r *KopiaMaintenanceReconciler) updateStatus(ctx context.Context, maintenance *volsyncv1alpha1.KopiaMaintenance, matchedSources []*volsyncv1alpha1.ReplicationSource, activeCronJobs []string) error {
	if maintenance.Status == nil {
		maintenance.Status = &volsyncv1alpha1.KopiaMaintenanceStatus{}
	}

	// Update matched sources
	maintenance.Status.MatchedSources = []volsyncv1alpha1.MatchedSource{}
	for _, source := range matchedSources {
		maintenance.Status.MatchedSources = append(maintenance.Status.MatchedSources, volsyncv1alpha1.MatchedSource{
			Name:       source.Name,
			Namespace:  source.Namespace,
			Repository: source.Spec.Kopia.Repository,
			LastMatched: &metav1.Time{Time: time.Now()},
		})
	}

	// Update active CronJobs
	maintenance.Status.ActiveCronJobs = activeCronJobs

	// Update last reconcile time
	maintenance.Status.LastReconcileTime = &metav1.Time{Time: time.Now()}

	// Update conditions
	r.updateConditions(maintenance, matchedSources, activeCronJobs)

	return r.Status().Update(ctx, maintenance)
}

// getOperatorNamespace returns the namespace where the operator is running
func (r *KopiaMaintenanceReconciler) getOperatorNamespace() string {
	// This should be set from the operator's environment or configuration
	// For now, return a default value. In production, this would come from
	// the operator's deployment configuration
	return "volsync-system"
}

// copySecretToOperatorNamespace copies a secret from one namespace to the operator namespace
func (r *KopiaMaintenanceReconciler) copySecretToOperatorNamespace(ctx context.Context, secretName, sourceNamespace, maintenanceName string) (string, error) {
	// Get the source secret
	sourceSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      secretName,
		Namespace: sourceNamespace,
	}, sourceSecret); err != nil {
		return "", fmt.Errorf("failed to get source secret %s/%s: %w", sourceNamespace, secretName, err)
	}

	// Generate a unique name for the copied secret
	targetSecretName := fmt.Sprintf("kopia-maintenance-%s-%s", maintenanceName, secretName)
	operatorNamespace := r.getOperatorNamespace()

	// Create the target secret
	targetSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      targetSecretName,
			Namespace: operatorNamespace,
			Labels: map[string]string{
				"volsync.backube/kopia-maintenance": "true",
				"volsync.backube/managed-by":        maintenanceName,
				"volsync.backube/source-secret":     secretName,
				"volsync.backube/source-namespace":  sourceNamespace,
			},
		},
		Data: sourceSecret.Data,
		Type: sourceSecret.Type,
	}

	// Check if the secret already exists
	existingSecret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      targetSecretName,
		Namespace: operatorNamespace,
	}, existingSecret)

	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create the secret
			if err := r.Create(ctx, targetSecret); err != nil {
				return "", fmt.Errorf("failed to create secret %s/%s: %w", operatorNamespace, targetSecretName, err)
			}
		} else {
			return "", fmt.Errorf("failed to check for existing secret: %w", err)
		}
	} else {
		// Update the existing secret
		existingSecret.Data = sourceSecret.Data
		existingSecret.Type = sourceSecret.Type
		if err := r.Update(ctx, existingSecret); err != nil {
			return "", fmt.Errorf("failed to update secret %s/%s: %w", operatorNamespace, targetSecretName, err)
		}
	}

	return targetSecretName, nil
}

// cleanupCopiedSecrets removes secrets copied for this KopiaMaintenance
func (r *KopiaMaintenanceReconciler) cleanupCopiedSecrets(ctx context.Context, maintenanceName string) error {
	operatorNamespace := r.getOperatorNamespace()

	// List secrets with our labels
	secretList := &corev1.SecretList{}
	if err := r.List(ctx, secretList, client.InNamespace(operatorNamespace), client.MatchingLabels{
		"volsync.backube/kopia-maintenance": "true",
		"volsync.backube/managed-by":        maintenanceName,
	}); err != nil {
		return err
	}

	for _, secret := range secretList.Items {
		if err := r.Delete(ctx, &secret); err != nil && !apierrors.IsNotFound(err) {
			r.Log.Error(err, "Failed to delete copied secret", "secret", secret.Name)
			// Continue with other secrets
		}
	}

	return nil
}

// updateConditions updates the status conditions
func (r *KopiaMaintenanceReconciler) updateConditions(maintenance *volsyncv1alpha1.KopiaMaintenance, matchedSources []*volsyncv1alpha1.ReplicationSource, activeCronJobs []string) {
	// Ready condition
	readyCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: maintenance.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if !maintenance.GetEnabled() {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "MaintenanceDisabled"
		readyCondition.Message = "Maintenance is disabled"
	} else if maintenance.IsDirectRepositoryMode() {
		// Direct repository mode
		if len(activeCronJobs) > 0 {
			readyCondition.Status = metav1.ConditionTrue
			readyCondition.Reason = "MaintenanceActive"
			secretName, namespace := maintenance.GetRepositorySecret()
			readyCondition.Message = fmt.Sprintf("Managing maintenance for direct repository %s/%s with %d CronJobs", namespace, secretName, len(activeCronJobs))
		} else {
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = "CronJobCreationFailed"
			readyCondition.Message = "Failed to create maintenance CronJob for direct repository"
		}
	} else {
		// Repository selector mode
		if len(matchedSources) > 0 && len(activeCronJobs) > 0 {
			readyCondition.Status = metav1.ConditionTrue
			readyCondition.Reason = "MaintenanceActive"
			readyCondition.Message = fmt.Sprintf("Managing maintenance for %d sources with %d CronJobs", len(matchedSources), len(activeCronJobs))
		} else if len(matchedSources) == 0 {
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = "NoMatchingSources"
			readyCondition.Message = "No ReplicationSources match the selector"
		} else {
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = "CronJobCreationFailed"
			readyCondition.Message = "Failed to create maintenance CronJobs"
		}
	}

	// Update or append the condition
	found := false
	for i, condition := range maintenance.Status.Conditions {
		if condition.Type == readyCondition.Type {
			maintenance.Status.Conditions[i] = readyCondition
			found = true
			break
		}
	}
	if !found {
		maintenance.Status.Conditions = append(maintenance.Status.Conditions, readyCondition)
	}
}