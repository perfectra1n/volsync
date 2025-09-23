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
	"os"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	volsyncv1alpha1 "github.com/backube/volsync/api/v1alpha1"
)

func TestMaintenanceManager(t *testing.T) {
	// Set operator namespace for tests
	os.Setenv("POD_NAMESPACE", "test-operator-namespace")
	defer os.Unsetenv("POD_NAMESPACE")

	// Create a fake client with scheme
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)
	_ = volsyncv1alpha1.AddToScheme(scheme)

	t.Run("ReconcileMaintenanceForSource", func(t *testing.T) {
		// Create source secret that will be copied
		sourceSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: "test-namespace",
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"KOPIA_PASSWORD": []byte("test-password"),
			},
		}

		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sourceSecret).Build()
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

		// Verify that a CronJob was created in operator namespace
		cronJobList := &batchv1.CronJobList{}
		err = client.List(context.Background(), cronJobList, ctrlclient.InNamespace("test-operator-namespace"))
		if err != nil {
			t.Fatalf("Failed to list CronJobs: %v", err)
		}

		// Also check all namespaces to debug
		allCronJobs := &batchv1.CronJobList{}
		err = client.List(context.Background(), allCronJobs)
		if err != nil {
			t.Fatalf("Failed to list all CronJobs: %v", err)
		}

		if len(allCronJobs.Items) > 0 {
			t.Logf("Found %d CronJobs in all namespaces:", len(allCronJobs.Items))
			for _, cj := range allCronJobs.Items {
				t.Logf("  - %s/%s", cj.Namespace, cj.Name)
			}
		}

		if len(cronJobList.Items) != 1 {
			t.Fatalf("Expected 1 CronJob in operator namespace, got %d", len(cronJobList.Items))
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

		// Verify EnvFrom uses copied secret
		if len(container.EnvFrom) != 1 {
			t.Errorf("Expected 1 EnvFrom source, got %d", len(container.EnvFrom))
		}
		// The secret should be copied with a prefixed name
		expectedSecretPrefix := "maintenance-test-namespace-"
		if container.EnvFrom[0].SecretRef == nil ||
			!strings.HasPrefix(container.EnvFrom[0].SecretRef.Name, expectedSecretPrefix) {
			t.Errorf("Expected EnvFrom secret to start with %s, got %v",
				expectedSecretPrefix, container.EnvFrom[0].SecretRef)
		}

		// Verify secret was copied to operator namespace
		secretList := &corev1.SecretList{}
		err = client.List(context.Background(), secretList, ctrlclient.InNamespace("test-operator-namespace"))
		if err != nil {
			t.Fatalf("Failed to list secrets: %v", err)
		}

		// Should have at least one maintenance secret
		foundMaintenanceSecret := false
		for _, secret := range secretList.Items {
			if secret.Labels[maintenanceSecretLabel] == "true" {
				foundMaintenanceSecret = true
				// Verify it has the correct data
				if string(secret.Data["KOPIA_PASSWORD"]) != "test-password" {
					t.Errorf("Copied secret has incorrect password data")
				}
				break
			}
		}
		if !foundMaintenanceSecret {
			t.Errorf("Expected to find copied maintenance secret in operator namespace")
		}
	})

	t.Run("MaintenanceDisabledWhenIntervalIsZero", func(t *testing.T) {
		client := fake.NewClientBuilder().WithScheme(scheme).Build()
		logger := logr.Discard()
		manager := NewMaintenanceManager(client, logger, "test-image:latest")

		// Create a test ReplicationSource with maintenance disabled
		// maintenanceInterval := int32(0) // Field removed
		source := &volsyncv1alpha1.ReplicationSource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-source",
				Namespace: "test-namespace",
			},
			Spec: volsyncv1alpha1.ReplicationSourceSpec{
				SourcePVC: "test-pvc",
				Kopia: &volsyncv1alpha1.ReplicationSourceKopiaSpec{
					Repository: "test-repo-secret",
					// MaintenanceIntervalDays removed - use KopiaMaintenance CRD
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
		// Create an orphaned CronJob in operator namespace
		orphanedCronJob := &batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kopia-maintenance-orphan",
				Namespace: "test-operator-namespace", // CronJobs are now centralized
				Labels: map[string]string{
					maintenanceLabelKey:        "true",
					maintenanceRepositoryLabel: "orphan-hash",
					maintenanceNamespaceLabel:  "test-namespace", // Track source namespace
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

		// Create an orphaned secret
		orphanedSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "maintenance-test-namespace-orphan",
				Namespace: "test-operator-namespace",
				Labels: map[string]string{
					maintenanceSecretLabel:     "true",
					maintenanceRepositoryLabel: "orphan-hash",
					maintenanceNamespaceLabel:  "test-namespace",
				},
			},
			Data: map[string][]byte{
				"KOPIA_PASSWORD": []byte("orphan"),
			},
		}

		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(orphanedCronJob, orphanedSecret).Build()
		logger := logr.Discard()
		manager := NewMaintenanceManager(client, logger, "test-image:latest")

		// Cleanup orphaned CronJobs (no sources exist)
		err := manager.CleanupOrphanedMaintenanceCronJobs(context.Background(), "test-namespace")
		if err != nil {
			t.Fatalf("Failed to cleanup orphaned CronJobs: %v", err)
		}

		// Verify that the orphaned CronJob was deleted from operator namespace
		cronJobList := &batchv1.CronJobList{}
		err = client.List(context.Background(), cronJobList, ctrlclient.InNamespace("test-operator-namespace"))
		if err != nil {
			t.Fatalf("Failed to list CronJobs: %v", err)
		}

		if len(cronJobList.Items) != 0 {
			t.Fatalf("Expected orphaned CronJob to be deleted, but %d CronJobs remain", len(cronJobList.Items))
		}

		// Verify that the orphaned secret was also deleted
		secretList := &corev1.SecretList{}
		err = client.List(context.Background(), secretList,
			ctrlclient.InNamespace("test-operator-namespace"),
			ctrlclient.MatchingLabels{maintenanceSecretLabel: "true"})
		if err != nil {
			t.Fatalf("Failed to list secrets: %v", err)
		}

		if len(secretList.Items) != 0 {
			t.Fatalf("Expected orphaned secret to be deleted, but %d secrets remain", len(secretList.Items))
		}
	})

	t.Run("RepositoryConfigHash", func(t *testing.T) {
		// Test that the hash is deterministic and only based on repository
		config1 := &RepositoryConfig{
			Repository: "repo1",
			Namespace:  "ns1",
			Schedule:   "0 2 * * *",
		}

		config2 := &RepositoryConfig{
			Repository: "repo1",
			Namespace:  "ns2", // Different namespace
			Schedule:   "0 3 * * *", // Different schedule
		}

		// Phase 1: Hash should be the same since only repository matters
		if config1.Hash() != config2.Hash() {
			t.Errorf("Expected configs with same repository to have same hash regardless of namespace/schedule")
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

		// Test with CustomCA
		config4 := &RepositoryConfig{
			Repository: "repo1",
			CustomCA: &volsyncv1alpha1.CustomCASpec{
				SecretName: "ca-secret",
				Key:        "ca.crt",
			},
		}

		config5 := &RepositoryConfig{
			Repository: "repo1",
			CustomCA: &volsyncv1alpha1.CustomCASpec{
				SecretName: "ca-secret",
				Key:        "ca.crt",
			},
		}

		// Same repository with same CA should have same hash
		if config4.Hash() != config5.Hash() {
			t.Errorf("Expected identical configs with CA to have same hash")
		}

		// Same repository with different CA should have different hash
		if config1.Hash() == config4.Hash() {
			t.Errorf("Expected different CA configs to have different hashes")
		}
	})

	t.Run("SecurityContextAndResourceLimits", func(t *testing.T) {
		// Create source secret that will be copied
		sourceSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: "test-namespace",
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"KOPIA_PASSWORD": []byte("test-password"),
			},
		}

		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sourceSecret).Build()
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

		// Get the created CronJob from operator namespace
		cronJobList := &batchv1.CronJobList{}
		err = client.List(context.Background(), cronJobList, ctrlclient.InNamespace("test-operator-namespace"))
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
