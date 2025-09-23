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
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	cron "github.com/robfig/cron/v3"
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

	volsyncv1alpha1 "github.com/backube/volsync/api/v1alpha1"
	"github.com/backube/volsync/internal/controller/mover/kopia"
	"github.com/backube/volsync/internal/controller/utils"
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
	// Initialize container image if not already set
	if r.containerImage == "" {
		r.containerImage = utils.GetDefaultKopiaImage()
	}

	// Watch KopiaMaintenance resources and CronJobs they own
	return ctrl.NewControllerManagedBy(mgr).
		For(&volsyncv1alpha1.KopiaMaintenance{}).
		Owns(&batchv1.CronJob{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 3,
		}).
		Complete(r)
}

// +kubebuilder:rbac:groups=volsync.backube,resources=kopiamaintenances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=volsync.backube,resources=kopiamaintenances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=volsync.backube,resources=kopiamaintenances/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch
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
		return ctrl.Result{}, r.updateStatus(ctx, maintenance, "")
	}

	// Check if maintenance is enabled
	if !maintenance.GetEnabled() {
		logger.V(1).Info("Maintenance is disabled")
		// Clean up any existing CronJobs
		if err := r.cleanupCronJob(ctx, maintenance); err != nil {
			logger.Error(err, "Failed to cleanup CronJob")
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}
		return ctrl.Result{}, r.updateStatus(ctx, maintenance, "")
	}

	// Ensure the CronJob exists for the repository
	cronJobName, err := r.ensureCronJob(ctx, maintenance)
	if err != nil {
		logger.Error(err, "Failed to ensure CronJob")
		r.EventRecorder.Event(maintenance, corev1.EventTypeWarning, "CronJobFailed",
			fmt.Sprintf("Failed to ensure CronJob: %v", err))
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Update status
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, r.updateStatus(ctx, maintenance, cronJobName)
}

// ensureCronJob creates or updates a CronJob for the KopiaMaintenance
func (r *KopiaMaintenanceReconciler) ensureCronJob(ctx context.Context, maintenance *volsyncv1alpha1.KopiaMaintenance) (string, error) {
	// Verify that the repository secret exists
	repositorySecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      maintenance.GetRepositorySecret(),
		Namespace: maintenance.Namespace,
	}, repositorySecret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("repository secret %s not found in namespace %s",
				maintenance.GetRepositorySecret(), maintenance.Namespace)
		}
		return "", fmt.Errorf("failed to get repository secret: %w", err)
	}

	// Create a synthetic ReplicationSource for the maintenance manager
	// This allows us to reuse the existing kopia maintenance manager code
	syntheticSource := &volsyncv1alpha1.ReplicationSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("maintenance-%s", maintenance.Name),
			Namespace: maintenance.Namespace,
		},
		Spec: volsyncv1alpha1.ReplicationSourceSpec{
			Kopia: &volsyncv1alpha1.ReplicationSourceKopiaSpec{
				Repository: maintenance.GetRepositorySecret(),
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

	// Apply pod configuration if specified
	if maintenance.Spec.ServiceAccountName != nil {
		syntheticSource.Spec.Kopia.MoverServiceAccount = maintenance.Spec.ServiceAccountName
	}
	if len(maintenance.Spec.MoverPodLabels) > 0 {
		syntheticSource.Spec.Kopia.MoverPodLabels = maintenance.Spec.MoverPodLabels
	}
	if maintenance.Spec.Affinity != nil {
		syntheticSource.Spec.Kopia.MoverAffinity = maintenance.Spec.Affinity
	}

	// Handle NodeSelector and Tolerations through pod spec override if they are specified
	// Note: These fields require future implementation in the Kopia mover to be fully supported
	// For now, they are preserved in the spec but not actively used
	if len(maintenance.Spec.NodeSelector) > 0 || len(maintenance.Spec.Tolerations) > 0 {
		r.Log.V(1).Info("NodeSelector and Tolerations are not yet implemented in Kopia mover",
			"namespace", maintenance.Namespace, "name", maintenance.Name)
	}

	// Create maintenance manager
	mgr := kopia.NewMaintenanceManager(r.Client, r.Log, r.containerImage)

	// Set the owner reference so the CronJob is owned by the KopiaMaintenance
	syntheticSource.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: maintenance.APIVersion,
			Kind:       maintenance.Kind,
			Name:       maintenance.Name,
			UID:        maintenance.UID,
			Controller: &[]bool{true}[0],
		},
	}

	// Ensure the CronJob exists
	if err := mgr.EnsureMaintenanceCronJob(ctx, syntheticSource); err != nil {
		return "", fmt.Errorf("failed to ensure maintenance CronJob: %w", err)
	}

	// Generate the CronJob name with a hash to avoid conflicts
	// Include namespace to ensure uniqueness across namespaces
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s", maintenance.Namespace, maintenance.Name)))

	// Kubernetes names have a 63 character limit
	// "kopia-maint-" is 12 chars, hash suffix is 8 chars (4 bytes in hex), plus 1 for hyphen = 21 chars overhead
	// This leaves 42 chars for the maintenance name
	maxNameLength := 42
	truncatedName := maintenance.Name
	if len(truncatedName) > maxNameLength {
		truncatedName = truncatedName[:maxNameLength]
	}
	cronJobName := fmt.Sprintf("kopia-maint-%s-%x", truncatedName, hash[:4])

	return cronJobName, nil
}

// cleanupCronJob removes the CronJob managed by this KopiaMaintenance
func (r *KopiaMaintenanceReconciler) cleanupCronJob(ctx context.Context, maintenance *volsyncv1alpha1.KopiaMaintenance) error {
	// The CronJob should be automatically deleted due to owner references
	// But we can try to delete it explicitly if needed
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s", maintenance.Namespace, maintenance.Name)))

	// Use same truncation logic as in ensureCronJob
	maxNameLength := 42
	truncatedName := maintenance.Name
	if len(truncatedName) > maxNameLength {
		truncatedName = truncatedName[:maxNameLength]
	}
	cronJobName := fmt.Sprintf("kopia-maint-%s-%x", truncatedName, hash[:4])
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cronJobName,
			Namespace: maintenance.Namespace,
		},
	}

	if err := r.Delete(ctx, cronJob); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete CronJob: %w", err)
	}

	return nil
}

// updateStatus updates the KopiaMaintenance status
func (r *KopiaMaintenanceReconciler) updateStatus(ctx context.Context, maintenance *volsyncv1alpha1.KopiaMaintenance, activeCronJob string) error {
	// Ensure status is initialized
	if maintenance.Status == nil {
		maintenance.Status = &volsyncv1alpha1.KopiaMaintenanceStatus{
			Conditions: []metav1.Condition{},
		}
	}

	// Update active CronJob
	maintenance.Status.ActiveCronJob = activeCronJob

	// Update last reconcile time
	maintenance.Status.LastReconcileTime = &metav1.Time{Time: time.Now()}

	// Check if there's an active CronJob and get its status
	if activeCronJob != "" {
		cronJob := &batchv1.CronJob{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      activeCronJob,
			Namespace: maintenance.Namespace,
		}, cronJob); err == nil {
			// Calculate the actual next scheduled maintenance time using cron parser
			if schedule := maintenance.GetSchedule(); schedule != "" {
				if nextTime, err := r.calculateNextScheduledTime(schedule); err == nil {
					maintenance.Status.NextScheduledMaintenance = &metav1.Time{Time: nextTime}
				} else {
					r.Log.V(1).Info("Failed to parse cron schedule", "schedule", schedule, "error", err)
					// Fallback to approximate calculation
					if cronJob.Status.LastScheduleTime != nil {
						nextTime := cronJob.Status.LastScheduleTime.Add(24 * time.Hour)
						maintenance.Status.NextScheduledMaintenance = &metav1.Time{Time: nextTime}
					}
				}
			}

			// Check for recent job completions
			if cronJob.Status.LastSuccessfulTime != nil {
				maintenance.Status.LastMaintenanceTime = cronJob.Status.LastSuccessfulTime
				maintenance.Status.MaintenanceFailures = 0
			}
		}
	}

	// Update conditions
	r.updateConditions(maintenance, activeCronJob)

	// Update status with error handling
	if err := r.Status().Update(ctx, maintenance); err != nil {
		r.Log.Error(err, "Failed to update KopiaMaintenance status",
			"namespace", maintenance.Namespace, "name", maintenance.Name)
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// updateConditions updates the status conditions
func (r *KopiaMaintenanceReconciler) updateConditions(maintenance *volsyncv1alpha1.KopiaMaintenance, activeCronJob string) {
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
	} else if activeCronJob != "" {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "MaintenanceActive"
		readyCondition.Message = fmt.Sprintf("Maintenance CronJob %s is active for repository %s",
			activeCronJob, maintenance.GetRepositorySecret())
	} else {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "CronJobCreationFailed"
		readyCondition.Message = "Failed to create maintenance CronJob"
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

// calculateNextScheduledTime calculates the next scheduled time based on the cron expression
func (r *KopiaMaintenanceReconciler) calculateNextScheduledTime(schedule string) (time.Time, error) {
	// Parse the cron schedule
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := parser.Parse(schedule)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse cron schedule: %w", err)
	}

	// Get the next scheduled time
	now := time.Now()
	next := sched.Next(now)
	return next, nil
}