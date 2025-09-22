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
	"testing"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	volsyncv1alpha1 "github.com/backube/volsync/api/v1alpha1"
)

func TestMaintenanceManager(t *testing.T) {
	// Create a fake client with scheme
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	_ = volsyncv1alpha1.AddToScheme(scheme)

	t.Run("ReconcileMaintenanceForSource", func(t *testing.T) {
		client := fake.NewClientBuilder().WithScheme(scheme).Build()
		logger := logr.Discard()
		manager := NewMaintenanceManager(client, logger, "test-image:latest")

		// Create a test ReplicationSource
		source := &volsyncv1alpha1.ReplicationSource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-source",
				Namespace: "test-namespace",
			},
			Spec: volsyncv1alpha1.ReplicationSourceSpec{
				SourcePVC: "test-pvc",
				Kopia: &volsyncv1alpha1.ReplicationSourceKopiaSpec{
					Repository: "test-repo-secret",
				},
			},
		}

		// Reconcile maintenance for the source
		err := manager.ReconcileMaintenanceForSource(context.Background(), source)
		if err != nil {
			t.Fatalf("Failed to reconcile maintenance: %v", err)
		}

		// Verify that a CronJob was created
		cronJobList := &batchv1.CronJobList{}
		err = client.List(context.Background(), cronJobList, ctrlclient.InNamespace("test-namespace"))
		if err != nil {
			t.Fatalf("Failed to list CronJobs: %v", err)
		}

		if len(cronJobList.Items) != 1 {
			t.Fatalf("Expected 1 CronJob, got %d", len(cronJobList.Items))
		}

		cronJob := cronJobList.Items[0]

		// Verify CronJob properties
		if cronJob.Spec.Schedule != defaultMaintenanceSchedule {
			t.Errorf("Expected schedule %s, got %s", defaultMaintenanceSchedule, cronJob.Spec.Schedule)
		}

		if cronJob.Labels[maintenanceLabelKey] != "true" {
			t.Errorf("Expected maintenance label to be true")
		}

		// Verify container configuration
		containers := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers
		if len(containers) != 1 {
			t.Fatalf("Expected 1 container, got %d", len(containers))
		}

		container := containers[0]
		if container.Image != "test-image:latest" {
			t.Errorf("Expected image test-image:latest, got %s", container.Image)
		}

		// Verify environment variables
		envFound := false
		for _, env := range container.Env {
			if env.Name == "DIRECTION" && env.Value == "maintenance" {
				envFound = true
				break
			}
		}
		if !envFound {
			t.Errorf("Expected DIRECTION=maintenance environment variable not found")
		}

		// Verify EnvFrom for repository secret
		if len(container.EnvFrom) != 1 {
			t.Errorf("Expected 1 EnvFrom source, got %d", len(container.EnvFrom))
		}
		if container.EnvFrom[0].SecretRef.Name != "test-repo-secret" {
			t.Errorf("Expected EnvFrom secret reference to test-repo-secret, got %s",
				container.EnvFrom[0].SecretRef.Name)
		}
	})

	t.Run("MaintenanceDisabledWhenIntervalIsZero", func(t *testing.T) {
		client := fake.NewClientBuilder().WithScheme(scheme).Build()
		logger := logr.Discard()
		manager := NewMaintenanceManager(client, logger, "test-image:latest")

		// Create a test ReplicationSource with maintenance disabled
		maintenanceInterval := int32(0)
		source := &volsyncv1alpha1.ReplicationSource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-source",
				Namespace: "test-namespace",
			},
			Spec: volsyncv1alpha1.ReplicationSourceSpec{
				SourcePVC: "test-pvc",
				Kopia: &volsyncv1alpha1.ReplicationSourceKopiaSpec{
					Repository:              "test-repo-secret",
					MaintenanceIntervalDays: &maintenanceInterval,
				},
			},
		}

		// Reconcile maintenance for the source
		err := manager.ReconcileMaintenanceForSource(context.Background(), source)
		if err != nil {
			t.Fatalf("Failed to reconcile maintenance: %v", err)
		}

		// Verify that no CronJob was created
		cronJobList := &batchv1.CronJobList{}
		err = client.List(context.Background(), cronJobList, ctrlclient.InNamespace("test-namespace"))
		if err != nil {
			t.Fatalf("Failed to list CronJobs: %v", err)
		}

		if len(cronJobList.Items) != 0 {
			t.Fatalf("Expected 0 CronJobs when maintenance is disabled, got %d", len(cronJobList.Items))
		}
	})

	t.Run("CleanupOrphanedMaintenanceCronJobs", func(t *testing.T) {
		// Create an orphaned CronJob
		orphanedCronJob := &batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kopia-maintenance-orphan",
				Namespace: "test-namespace",
				Labels: map[string]string{
					maintenanceLabelKey:        "true",
					maintenanceRepositoryLabel: "orphan-hash",
				},
			},
			Spec: batchv1.CronJobSpec{
				Schedule: "0 2 * * *",
				JobTemplate: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "test",
										Image: "test",
									},
								},
							},
						},
					},
				},
			},
		}

		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(orphanedCronJob).Build()
		logger := logr.Discard()
		manager := NewMaintenanceManager(client, logger, "test-image:latest")

		// Cleanup orphaned CronJobs (no sources exist)
		err := manager.CleanupOrphanedMaintenanceCronJobs(context.Background(), "test-namespace")
		if err != nil {
			t.Fatalf("Failed to cleanup orphaned CronJobs: %v", err)
		}

		// Verify that the orphaned CronJob was deleted
		cronJobList := &batchv1.CronJobList{}
		err = client.List(context.Background(), cronJobList, ctrlclient.InNamespace("test-namespace"))
		if err != nil {
			t.Fatalf("Failed to list CronJobs: %v", err)
		}

		if len(cronJobList.Items) != 0 {
			t.Fatalf("Expected orphaned CronJob to be deleted, but %d CronJobs remain", len(cronJobList.Items))
		}
	})

	t.Run("RepositoryConfigHash", func(t *testing.T) {
		// Test that the hash is deterministic
		config1 := &RepositoryConfig{
			Repository: "repo1",
			Namespace:  "ns1",
			Schedule:   "0 2 * * *",
		}

		config2 := &RepositoryConfig{
			Repository: "repo1",
			Namespace:  "ns1",
			Schedule:   "0 2 * * *",
		}

		if config1.Hash() != config2.Hash() {
			t.Errorf("Expected identical configs to have same hash")
		}

		// Different repository should have different hash
		config3 := &RepositoryConfig{
			Repository: "repo2",
			Namespace:  "ns1",
			Schedule:   "0 2 * * *",
		}

		if config1.Hash() == config3.Hash() {
			t.Errorf("Expected different repositories to have different hashes")
		}
	})

	t.Run("SecurityContextAndResourceLimits", func(t *testing.T) {
		client := fake.NewClientBuilder().WithScheme(scheme).Build()
		logger := logr.Discard()
		manager := NewMaintenanceManager(client, logger, "test-image:latest")

		// Create a test ReplicationSource
		source := &volsyncv1alpha1.ReplicationSource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-source",
				Namespace: "test-namespace",
			},
			Spec: volsyncv1alpha1.ReplicationSourceSpec{
				SourcePVC: "test-pvc",
				Kopia: &volsyncv1alpha1.ReplicationSourceKopiaSpec{
					Repository: "test-repo-secret",
				},
			},
		}

		// Reconcile maintenance for the source
		err := manager.ReconcileMaintenanceForSource(context.Background(), source)
		if err != nil {
			t.Fatalf("Failed to reconcile maintenance: %v", err)
		}

		// Get the created CronJob
		cronJobList := &batchv1.CronJobList{}
		err = client.List(context.Background(), cronJobList, ctrlclient.InNamespace("test-namespace"))
		if err != nil {
			t.Fatalf("Failed to list CronJobs: %v", err)
		}

		if len(cronJobList.Items) != 1 {
			t.Fatalf("Expected 1 CronJob, got %d", len(cronJobList.Items))
		}

		cronJob := cronJobList.Items[0]
		container := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0]

		// Verify security context
		secContext := container.SecurityContext
		if secContext == nil {
			t.Fatal("Expected security context to be set")
		}

		if *secContext.ReadOnlyRootFilesystem != true {
			t.Errorf("Expected ReadOnlyRootFilesystem to be true, got %v", *secContext.ReadOnlyRootFilesystem)
		}

		if *secContext.AllowPrivilegeEscalation != false {
			t.Errorf("Expected AllowPrivilegeEscalation to be false")
		}

		if *secContext.RunAsNonRoot != true {
			t.Errorf("Expected RunAsNonRoot to be true")
		}

		// Verify resource limits
		limits := container.Resources.Limits
		cpuLimit := limits.Cpu().String()
		memLimit := limits.Memory().String()

		if cpuLimit != "500m" {
			t.Errorf("Expected CPU limit to be 500m, got %s", cpuLimit)
		}

		if memLimit != "1Gi" {
			t.Errorf("Expected memory limit to be 1Gi, got %s", memLimit)
		}

		// Verify resource requests remain unchanged
		requests := container.Resources.Requests
		cpuRequest := requests.Cpu().String()
		memRequest := requests.Memory().String()

		if cpuRequest != "100m" {
			t.Errorf("Expected CPU request to be 100m, got %s", cpuRequest)
		}

		if memRequest != "256Mi" {
			t.Errorf("Expected memory request to be 256Mi, got %s", memRequest)
		}

		// Verify volumes are properly configured
		volumes := cronJob.Spec.JobTemplate.Spec.Template.Spec.Volumes
		volumeMounts := container.VolumeMounts

		// Check for cache volume with size limit
		cacheVolumeFound := false
		for _, vol := range volumes {
			if vol.Name == kopiaCache {
				cacheVolumeFound = true
				if vol.EmptyDir == nil {
					t.Error("Expected cache volume to be EmptyDir")
				} else if vol.EmptyDir.SizeLimit == nil {
					t.Error("Expected cache volume to have size limit")
				} else if vol.EmptyDir.SizeLimit.String() != "1Gi" {
					t.Errorf("Expected cache volume size limit to be 1Gi, got %s", vol.EmptyDir.SizeLimit.String())
				}
				break
			}
		}
		if !cacheVolumeFound {
			t.Error("Cache volume not found")
		}

		// Check for temp directory volume
		tempVolumeFound := false
		for _, vol := range volumes {
			if vol.Name == "tempdir" {
				tempVolumeFound = true
				if vol.EmptyDir == nil {
					t.Error("Expected tempdir volume to be EmptyDir")
				} else if vol.EmptyDir.Medium != corev1.StorageMediumMemory {
					t.Error("Expected tempdir volume to use memory medium")
				}
				break
			}
		}
		if !tempVolumeFound {
			t.Error("Temp directory volume not found")
		}

		// Verify volume mounts
		cacheMountFound := false
		tempMountFound := false
		for _, mount := range volumeMounts {
			if mount.Name == kopiaCache && mount.MountPath == kopiaCacheMountPath {
				cacheMountFound = true
			}
			if mount.Name == "tempdir" && mount.MountPath == "/tmp" {
				tempMountFound = true
			}
		}
		if !cacheMountFound {
			t.Error("Cache volume mount not found")
		}
		if !tempMountFound {
			t.Error("Temp directory volume mount not found")
		}
	})
}
