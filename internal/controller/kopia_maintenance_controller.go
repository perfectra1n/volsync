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
	"os"
	"sync"
	"time"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	volsyncv1alpha1 "github.com/backube/volsync/api/v1alpha1"
	"github.com/backube/volsync/internal/controller/mover/kopia"
	"github.com/backube/volsync/internal/controller/utils"
)

const (
	// Default reconciliation interval for periodic checks
	defaultReconcileInterval = 5 * time.Minute
	// Annotation to track the container image version used by the CronJob
	containerVersionAnnotation = "volsync.backube/container-version"
	// Label to identify CronJobs managed by this controller
	maintenanceControllerLabel = "volsync.backube/managed-by-maintenance-controller"
)

// KopiaMaintenanceController proactively manages Kopia maintenance CronJobs
// for all ReplicationSources across all namespaces
type KopiaMaintenanceController struct {
	client.Client
	Log             logr.Logger
	Scheme          *runtime.Scheme
	EventRecorder   record.EventRecorder
	containerImage  string
	imageUpdateLock sync.RWMutex
}

// ContainerImageSource defines methods for getting and watching container image updates
type ContainerImageSource interface {
	// GetCurrentImage returns the current container image
	GetCurrentImage() string
	// WatchForUpdates returns a channel that receives notifications when the image changes
	WatchForUpdates() <-chan string
}

// EnvContainerImageSource implements ContainerImageSource using environment variables
type EnvContainerImageSource struct {
	envVar         string
	currentImage   string
	updateChannels []chan string
	mu             sync.RWMutex
}

// NewEnvContainerImageSource creates a new environment-based container image source
func NewEnvContainerImageSource(envVar string) *EnvContainerImageSource {
	source := &EnvContainerImageSource{
		envVar:       envVar,
		currentImage: os.Getenv(envVar),
	}
	// Start a goroutine to periodically check for environment changes
	// (In production, this would be triggered by operator restart/upgrade)
	return source
}

// GetCurrentImage returns the current container image
func (e *EnvContainerImageSource) GetCurrentImage() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currentImage
}

// WatchForUpdates returns a channel for image update notifications
func (e *EnvContainerImageSource) WatchForUpdates() <-chan string {
	e.mu.Lock()
	defer e.mu.Unlock()
	ch := make(chan string, 1)
	e.updateChannels = append(e.updateChannels, ch)
	return ch
}

// UpdateImage updates the current image and notifies watchers
func (e *EnvContainerImageSource) UpdateImage(newImage string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.currentImage != newImage {
		e.currentImage = newImage
		for _, ch := range e.updateChannels {
			select {
			case ch <- newImage:
			default:
				// Channel is full, skip
			}
		}
	}
}

// +kubebuilder:rbac:groups=volsync.backube,resources=replicationsources,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch

// Reconcile handles the reconciliation loop for maintenance CronJob management
func (r *KopiaMaintenanceController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("request", req.NamespacedName)
	logger.V(2).Info("Starting reconciliation")

	// Check if this is a ReplicationSource event
	rs := &volsyncv1alpha1.ReplicationSource{}
	err := r.Get(ctx, req.NamespacedName, rs)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// ReplicationSource was deleted, check if we need to clean up orphaned CronJobs
			logger.V(1).Info("ReplicationSource not found, checking for orphaned CronJobs")
			return r.cleanupOrphanedCronJobs(ctx, req.Namespace)
		}
		return ctrl.Result{}, err
	}

	// Process the ReplicationSource
	return r.reconcileReplicationSource(ctx, rs, logger)
}

// reconcileReplicationSource ensures maintenance CronJob exists for a ReplicationSource if needed
func (r *KopiaMaintenanceController) reconcileReplicationSource(ctx context.Context, rs *volsyncv1alpha1.ReplicationSource, logger logr.Logger) (ctrl.Result, error) {
	// Check if this ReplicationSource uses Kopia
	if rs.Spec.Kopia == nil {
		logger.V(2).Info("ReplicationSource does not use Kopia, skipping")
		return ctrl.Result{}, nil
	}

	// Check if maintenance is enabled
	if rs.Spec.Kopia.MaintenanceCronJob != nil &&
		rs.Spec.Kopia.MaintenanceCronJob.Enabled != nil &&
		!*rs.Spec.Kopia.MaintenanceCronJob.Enabled {
		logger.V(2).Info("Maintenance is disabled for this ReplicationSource")
		return ctrl.Result{}, nil
	}

	// Create maintenance manager
	mgr := kopia.NewMaintenanceManager(r.Client, logger, r.getContainerImage())

	// Ensure maintenance CronJob exists
	err := mgr.EnsureMaintenanceCronJob(ctx, rs)
	if err != nil {
		logger.Error(err, "Failed to ensure maintenance CronJob")
		r.EventRecorder.Event(rs, corev1.EventTypeWarning, "MaintenanceCronJobFailed",
			fmt.Sprintf("Failed to ensure maintenance CronJob: %v", err))
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	logger.V(1).Info("Successfully ensured maintenance CronJob")
	return ctrl.Result{}, nil
}

// cleanupOrphanedCronJobs removes CronJobs that no longer have corresponding ReplicationSources
func (r *KopiaMaintenanceController) cleanupOrphanedCronJobs(ctx context.Context, namespace string) (ctrl.Result, error) {
	logger := r.Log.WithValues("namespace", namespace)

	// List all maintenance CronJobs in the namespace
	cronJobList := &batchv1.CronJobList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{
			"volsync.backube/kopia-maintenance": "true",
		},
	}

	if err := r.List(ctx, cronJobList, listOpts...); err != nil {
		logger.Error(err, "Failed to list maintenance CronJobs")
		return ctrl.Result{}, err
	}

	// Check each CronJob to see if its ReplicationSource still exists
	for _, cronJob := range cronJobList.Items {
		// Extract source namespace from labels
		sourceNS := cronJob.Labels["volsync.backube/source-namespace"]
		sourceName := cronJob.Annotations["volsync.backube/source-name"]

		if sourceNS == "" || sourceName == "" {
			logger.V(1).Info("CronJob missing source information, skipping",
				"cronJob", cronJob.Name)
			continue
		}

		// Check if the ReplicationSource still exists
		rs := &volsyncv1alpha1.ReplicationSource{}
		err := r.Get(ctx, types.NamespacedName{
			Namespace: sourceNS,
			Name:      sourceName,
		}, rs)

		if apierrors.IsNotFound(err) {
			// ReplicationSource doesn't exist, delete the CronJob
			logger.Info("Deleting orphaned maintenance CronJob",
				"cronJob", cronJob.Name,
				"sourceNamespace", sourceNS,
				"sourceName", sourceName)

			if err := r.Delete(ctx, &cronJob); err != nil && !apierrors.IsNotFound(err) {
				logger.Error(err, "Failed to delete orphaned CronJob", "cronJob", cronJob.Name)
				// Continue with other CronJobs
			}
		}
	}

	return ctrl.Result{}, nil
}

// reconcileAllReplicationSources processes all ReplicationSources across all namespaces
func (r *KopiaMaintenanceController) reconcileAllReplicationSources(ctx context.Context) error {
	logger := r.Log.WithName("reconcile-all")
	logger.Info("Starting reconciliation of all ReplicationSources")

	// List all ReplicationSources across all namespaces
	rsList := &volsyncv1alpha1.ReplicationSourceList{}
	if err := r.List(ctx, rsList); err != nil {
		logger.Error(err, "Failed to list ReplicationSources")
		return err
	}

	logger.Info("Found ReplicationSources", "count", len(rsList.Items))

	// Process each ReplicationSource
	var errors []error
	for _, rs := range rsList.Items {
		rsLogger := logger.WithValues(
			"namespace", rs.Namespace,
			"name", rs.Name,
		)

		_, err := r.reconcileReplicationSource(ctx, &rs, rsLogger)
		if err != nil {
			errors = append(errors, err)
			rsLogger.Error(err, "Failed to reconcile ReplicationSource")
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors during reconciliation", len(errors))
	}

	logger.Info("Completed reconciliation of all ReplicationSources")
	return nil
}

// updateAllCronJobContainerVersions updates the container image version for all maintenance CronJobs
func (r *KopiaMaintenanceController) updateAllCronJobContainerVersions(ctx context.Context, newImage string) error {
	logger := r.Log.WithName("update-container-versions")
	logger.Info("Updating container versions for all maintenance CronJobs", "newImage", newImage)

	// List all maintenance CronJobs across all namespaces
	cronJobList := &batchv1.CronJobList{}
	listOpts := []client.ListOption{
		client.MatchingLabels{
			"volsync.backube/kopia-maintenance": "true",
		},
	}

	if err := r.List(ctx, cronJobList, listOpts...); err != nil {
		logger.Error(err, "Failed to list maintenance CronJobs")
		return err
	}

	logger.Info("Found maintenance CronJobs", "count", len(cronJobList.Items))

	// Update each CronJob
	var errors []error
	for _, cronJob := range cronJobList.Items {
		cronJobLogger := logger.WithValues(
			"namespace", cronJob.Namespace,
			"name", cronJob.Name,
		)

		// Check if update is needed
		currentVersion := cronJob.Annotations[containerVersionAnnotation]
		if currentVersion == newImage {
			cronJobLogger.V(2).Info("CronJob already has current version, skipping")
			continue
		}

		// Update the CronJob
		cronJobCopy := cronJob.DeepCopy()

		// Update annotation
		if cronJobCopy.Annotations == nil {
			cronJobCopy.Annotations = make(map[string]string)
		}
		cronJobCopy.Annotations[containerVersionAnnotation] = newImage

		// Update container image in the job template
		if len(cronJobCopy.Spec.JobTemplate.Spec.Template.Spec.Containers) > 0 {
			cronJobCopy.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image = newImage
		}

		// Apply the update
		if err := r.Update(ctx, cronJobCopy); err != nil {
			errors = append(errors, err)
			cronJobLogger.Error(err, "Failed to update CronJob container version")
		} else {
			cronJobLogger.Info("Updated CronJob container version", "oldVersion", currentVersion)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors updating container versions", len(errors))
	}

	logger.Info("Completed updating container versions")
	return nil
}

// getContainerImage returns the current container image with thread-safe access
func (r *KopiaMaintenanceController) getContainerImage() string {
	r.imageUpdateLock.RLock()
	defer r.imageUpdateLock.RUnlock()
	return r.containerImage
}

// setContainerImage updates the container image with thread-safe access
func (r *KopiaMaintenanceController) setContainerImage(image string) {
	r.imageUpdateLock.Lock()
	defer r.imageUpdateLock.Unlock()
	r.containerImage = image
}

// SetupWithManager sets up the controller with the Manager
func (r *KopiaMaintenanceController) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize container image
	if r.containerImage == "" {
		r.containerImage = utils.GetDefaultKopiaImage()
	}

	// Build the controller with a unique name
	err := ctrl.NewControllerManagedBy(mgr).
		Named("kopia-maintenance-manager"). // Unique name to avoid conflicts
		For(&volsyncv1alpha1.ReplicationSource{}).
		// Watch CronJobs that we manage
		Watches(&batchv1.CronJob{},
			handler.EnqueueRequestsFromMapFunc(r.cronJobToReplicationSource),
			builder.WithPredicates(r.cronJobPredicate())).
		// Configure controller options
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 10, // Allow parallel processing
		}).
		Complete(r)

	if err != nil {
		return err
	}

	// Start a goroutine for initial reconciliation at startup
	go func() {
		// Wait a bit for the manager to be ready
		time.Sleep(10 * time.Second)

		ctx := context.Background()
		r.Log.Info("Starting initial reconciliation of all ReplicationSources")
		if err := r.reconcileAllReplicationSources(ctx); err != nil {
			r.Log.Error(err, "Initial reconciliation failed")
		}
	}()

	// Start a goroutine for periodic reconciliation
	go r.periodicReconciliation()

	// Start a goroutine to watch for container image updates
	go r.watchContainerImageUpdates()

	return nil
}

// periodicReconciliation runs periodic checks on all ReplicationSources
func (r *KopiaMaintenanceController) periodicReconciliation() {
	ticker := time.NewTicker(defaultReconcileInterval)
	defer ticker.Stop()

	for range ticker.C {
		ctx := context.Background()
		r.Log.V(1).Info("Starting periodic reconciliation")

		if err := r.reconcileAllReplicationSources(ctx); err != nil {
			r.Log.Error(err, "Periodic reconciliation failed")
		}
	}
}

// watchContainerImageUpdates monitors for container image version changes
func (r *KopiaMaintenanceController) watchContainerImageUpdates() {
	// In production, this would monitor for operator upgrades or config changes
	// For now, we'll check the environment variable periodically
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		newImage := utils.GetDefaultKopiaImage()

		currentImage := r.getContainerImage()
		if newImage != currentImage {
			r.Log.Info("Container image version changed",
				"old", currentImage,
				"new", newImage)

			r.setContainerImage(newImage)

			// Update all CronJobs with the new image
			ctx := context.Background()
			if err := r.updateAllCronJobContainerVersions(ctx, newImage); err != nil {
				r.Log.Error(err, "Failed to update CronJob container versions")
			}
		}
	}
}

// cronJobToReplicationSource maps CronJob events to ReplicationSource reconciliation requests
func (r *KopiaMaintenanceController) cronJobToReplicationSource(ctx context.Context, obj client.Object) []reconcile.Request {
	cronJob := obj.(*batchv1.CronJob)

	// Check if this is a maintenance CronJob we care about
	if cronJob.Labels["volsync.backube/kopia-maintenance"] != "true" {
		return []reconcile.Request{}
	}

	// Get the source information from annotations
	sourceNS := cronJob.Labels["volsync.backube/source-namespace"]
	sourceName := cronJob.Annotations["volsync.backube/source-name"]

	if sourceNS == "" || sourceName == "" {
		r.Log.V(1).Info("CronJob missing source information",
			"namespace", cronJob.Namespace,
			"name", cronJob.Name)
		return []reconcile.Request{}
	}

	// Return a request to reconcile the associated ReplicationSource
	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Namespace: sourceNS,
				Name:      sourceName,
			},
		},
	}
}

// cronJobPredicate returns predicates for filtering CronJob events
func (r *KopiaMaintenanceController) cronJobPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// We care about creation of maintenance CronJobs
			return r.isMaintenanceCronJob(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// We care about updates to maintenance CronJobs
			return r.isMaintenanceCronJob(e.ObjectNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// We care about deletion of maintenance CronJobs
			return r.isMaintenanceCronJob(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}

// isMaintenanceCronJob checks if an object is a maintenance CronJob
func (r *KopiaMaintenanceController) isMaintenanceCronJob(obj client.Object) bool {
	cronJob, ok := obj.(*batchv1.CronJob)
	if !ok {
		return false
	}

	return cronJob.Labels["volsync.backube/kopia-maintenance"] == "true"
}