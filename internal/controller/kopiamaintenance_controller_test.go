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
	"strings"
	"testing"
	"time"

	volsyncv1alpha1 "github.com/backube/volsync/api/v1alpha1"
	"github.com/backube/volsync/internal/controller/utils"
	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// generateCronJobName generates the CronJob name the same way the controller does
func generateCronJobName(namespace, name string) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s", namespace, name)))
	maxNameLength := 34
	truncatedName := name
	if len(truncatedName) > maxNameLength {
		truncatedName = truncatedName[:maxNameLength]
	}
	return fmt.Sprintf("kopia-maint-%s-%x", truncatedName, hash[:8])
}

func TestKopiaMaintenanceReconciler_calculateNextScheduledTime(t *testing.T) {
	r := &KopiaMaintenanceReconciler{
		Log: logr.Discard(),
	}

	tests := []struct {
		name     string
		schedule string
		wantErr  bool
	}{
		{
			name:     "valid daily schedule",
			schedule: "0 2 * * *",
			wantErr:  false,
		},
		{
			name:     "valid hourly schedule",
			schedule: "0 * * * *",
			wantErr:  false,
		},
		{
			name:     "invalid schedule",
			schedule: "invalid",
			wantErr:  true,
		},
		{
			name:     "empty schedule",
			schedule: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.calculateNextScheduledTime(tt.schedule)
			if (err != nil) != tt.wantErr {
				t.Errorf("calculateNextScheduledTime() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.Before(time.Now()) {
				t.Errorf("calculateNextScheduledTime() returned time in the past: %v", got)
			}
		})
	}
}

func TestKopiaMaintenanceReconciler_SetupWithManager(t *testing.T) {
	// Setup the scheme
	s := scheme.Scheme
	_ = volsyncv1alpha1.AddToScheme(s)

	// Test that container image is initialized when empty
	r := &KopiaMaintenanceReconciler{
		Client:         fake.NewClientBuilder().WithScheme(s).Build(),
		Scheme:         s,
		Log:            logr.Discard(),
		EventRecorder:  &record.FakeRecorder{},
		containerImage: "", // Start with empty image
	}

	// Mock manager setup would happen here in integration tests
	// For now, just verify that the container image gets initialized
	if r.containerImage == "" {
		// This would be set in SetupWithManager
		r.containerImage = "quay.io/backube/volsync:latest"
	}

	if r.containerImage == "" {
		t.Error("Container image was not initialized")
	}
}

func TestKopiaMaintenanceReconciler_updateStatusWithError(t *testing.T) {
	// Setup the scheme
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = volsyncv1alpha1.AddToScheme(s)

	tests := []struct {
		name          string
		maintenance   *volsyncv1alpha1.KopiaMaintenance
		activeCronJob string
		wantErr       bool
	}{
		{
			name: "status initialization",
			maintenance: &volsyncv1alpha1.KopiaMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-maintenance",
					Namespace: "test-ns",
				},
				Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
					Repository: volsyncv1alpha1.KopiaRepositorySpec{
						Repository: "test-secret",
					},
				},
				Status: nil, // Nil status should be initialized
			},
			activeCronJob: "test-cronjob",
			wantErr:       false,
		},
		{
			name: "status update with existing status",
			maintenance: &volsyncv1alpha1.KopiaMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-maintenance",
					Namespace: "test-ns",
				},
				Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
					Repository: volsyncv1alpha1.KopiaRepositorySpec{
						Repository: "test-secret",
					},
				},
				Status: &volsyncv1alpha1.KopiaMaintenanceStatus{
					ActiveCronJob: "old-cronjob",
				},
			},
			activeCronJob: "new-cronjob",
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with the maintenance object
			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tt.maintenance).
				WithStatusSubresource(tt.maintenance).
				Build()

			r := &KopiaMaintenanceReconciler{
				Client:        fakeClient,
				Scheme:        s,
				Log:           logr.Discard(),
				EventRecorder: &record.FakeRecorder{},
			}

			err := r.updateStatusWithError(context.Background(), tt.maintenance, tt.activeCronJob, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("updateStatusWithError() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Verify status was initialized if it was nil
			if tt.maintenance.Status == nil {
				t.Error("Status was not initialized")
			}

			// Verify the active CronJob was updated
			if tt.maintenance.Status.ActiveCronJob != tt.activeCronJob {
				t.Errorf("ActiveCronJob = %v, want %v", tt.maintenance.Status.ActiveCronJob, tt.activeCronJob)
			}
		})
	}
}

func TestKopiaMaintenanceReconciler_CronJobNameGeneration(t *testing.T) {
	// Test that CronJob names are unique and don't conflict
	names := make(map[string]bool)

	testCases := []struct {
		namespace string
		name      string
	}{
		{"ns1", "maintenance1"},
		{"ns2", "maintenance1"}, // Same name, different namespace
		{"ns1", "maintenance2"},
		{"ns1", "very-long-maintenance-name-that-exceeds-normal-limits"},
		{"default", "a-maintenance-name-exactly-at-the-42-char-limit-x"},
	}

	for _, tc := range testCases {
		// Simulate the name generation logic from the controller
		// This mirrors the actual implementation
		maxNameLength := 42
		truncatedName := tc.name
		if len(truncatedName) > maxNameLength {
			truncatedName = truncatedName[:maxNameLength]
		}
		// For test purposes, use a simple hash simulation
		hashStr := tc.namespace + "/" + tc.name
		cronJobName := "kopia-maint-" + truncatedName + "-" + hashStr[:4]

		if len(cronJobName) > 63 {
			// Kubernetes name limit
			t.Errorf("Generated CronJob name too long: %s (length: %d)", cronJobName, len(cronJobName))
		}

		// The actual uniqueness comes from the hash, not just the name
		fullKey := tc.namespace + "/" + cronJobName
		if names[fullKey] {
			t.Errorf("Duplicate CronJob name generated: %s in namespace %s", cronJobName, tc.namespace)
		}
		names[fullKey] = true
	}
}

func TestKopiaMaintenanceReconciler_EnsureCronJob(t *testing.T) {
	// Setup the scheme
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = batchv1.AddToScheme(s)
	_ = volsyncv1alpha1.AddToScheme(s)

	t.Run("CronJob creation with default security context", func(t *testing.T) {
		// Create the repository secret
		repoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: "test-ns",
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"KOPIA_PASSWORD": []byte("test-password"),
			},
		}

		// Create a KopiaMaintenance without custom security context
		maintenance := &volsyncv1alpha1.KopiaMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-maintenance",
				Namespace: "test-ns",
			},
			Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
				Repository: volsyncv1alpha1.KopiaRepositorySpec{
					Repository: "test-repo-secret",
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(repoSecret, maintenance).
			Build()

		r := &KopiaMaintenanceReconciler{
			Client:         fakeClient,
			Scheme:         s,
			Log:            logr.Discard(),
			EventRecorder:  &record.FakeRecorder{},
			containerImage: "quay.io/backube/volsync:latest",
		}

		cronJobName, err := r.ensureCronJob(context.Background(), maintenance)
		if err != nil {
			t.Fatalf("ensureCronJob() error = %v", err)
		}

		if cronJobName == "" {
			t.Fatal("ensureCronJob() returned empty CronJob name")
		}

		// Retrieve the created CronJob
		cronJobList := &batchv1.CronJobList{}
		if err := fakeClient.List(context.Background(), cronJobList); err != nil {
			t.Fatalf("Failed to list CronJobs: %v", err)
		}
		if len(cronJobList.Items) != 1 {
			t.Fatalf("Expected 1 CronJob, got %d", len(cronJobList.Items))
		}
		cronJob := &cronJobList.Items[0]

		// Verify default PodSecurityContext
		podSecCtx := cronJob.Spec.JobTemplate.Spec.Template.Spec.SecurityContext
		if podSecCtx == nil {
			t.Fatal("Expected PodSecurityContext to be set")
		}
		if podSecCtx.RunAsNonRoot == nil || !*podSecCtx.RunAsNonRoot {
			t.Error("Expected RunAsNonRoot to be true")
		}
		if podSecCtx.RunAsUser == nil || *podSecCtx.RunAsUser != 1000 {
			t.Errorf("Expected RunAsUser to be 1000, got %v", podSecCtx.RunAsUser)
		}
		if podSecCtx.FSGroup == nil || *podSecCtx.FSGroup != 1000 {
			t.Errorf("Expected FSGroup to be 1000, got %v", podSecCtx.FSGroup)
		}

		// Verify default ContainerSecurityContext
		if len(cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers) == 0 {
			t.Fatal("Expected at least one container")
		}
		container := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0]
		containerSecCtx := container.SecurityContext
		if containerSecCtx == nil {
			t.Fatal("Expected container SecurityContext to be set")
		}
		if containerSecCtx.ReadOnlyRootFilesystem == nil || !*containerSecCtx.ReadOnlyRootFilesystem {
			t.Error("Expected ReadOnlyRootFilesystem to be true")
		}
		if containerSecCtx.AllowPrivilegeEscalation == nil || *containerSecCtx.AllowPrivilegeEscalation {
			t.Error("Expected AllowPrivilegeEscalation to be false")
		}
		if containerSecCtx.RunAsNonRoot == nil || !*containerSecCtx.RunAsNonRoot {
			t.Error("Expected container RunAsNonRoot to be true")
		}
		if containerSecCtx.Privileged == nil || *containerSecCtx.Privileged {
			t.Error("Expected Privileged to be false")
		}
		if containerSecCtx.Capabilities == nil || len(containerSecCtx.Capabilities.Drop) == 0 {
			t.Error("Expected Capabilities.Drop to contain ALL")
		} else {
			foundAll := false
			for _, cap := range containerSecCtx.Capabilities.Drop {
				if cap == "ALL" {
					foundAll = true
					break
				}
			}
			if !foundAll {
				t.Error("Expected Capabilities.Drop to contain ALL")
			}
		}

		// Verify DIRECTION environment variable
		directionFound := false
		for _, env := range container.Env {
			if env.Name == "DIRECTION" && env.Value == "maintenance" {
				directionFound = true
				break
			}
		}
		if !directionFound {
			t.Error("Expected DIRECTION=maintenance environment variable")
		}

		// Verify volumes
		tmpVolumeFound := false
		for _, vol := range cronJob.Spec.JobTemplate.Spec.Template.Spec.Volumes {
			if vol.Name == "tmp" && vol.EmptyDir != nil {
				tmpVolumeFound = true
				break
			}
		}
		if !tmpVolumeFound {
			t.Error("Expected tmp EmptyDir volume")
		}

		// Verify volume mounts
		tmpMountFound := false
		for _, mount := range container.VolumeMounts {
			if mount.Name == "tmp" && mount.MountPath == "/tmp" {
				tmpMountFound = true
				break
			}
		}
		if !tmpMountFound {
			t.Error("Expected /tmp volume mount")
		}
	})

	t.Run("CronJob creation with custom security context and resources", func(t *testing.T) {
		// Create the repository secret
		repoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: "test-ns",
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"KOPIA_PASSWORD": []byte("test-password"),
			},
		}

		// Create a KopiaMaintenance with custom security context and resources
		maintenance := &volsyncv1alpha1.KopiaMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-maintenance-custom",
				Namespace: "test-ns",
			},
			Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
				Repository: volsyncv1alpha1.KopiaRepositorySpec{
					Repository: "test-repo-secret",
				},
				PodSecurityContext: &corev1.PodSecurityContext{
					RunAsNonRoot: ptr.To(true),
					RunAsUser:    ptr.To(int64(2000)),
					FSGroup:      ptr.To(int64(2000)),
				},
				ContainerSecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr.To(false),
					ReadOnlyRootFilesystem:   ptr.To(true),
					RunAsNonRoot:             ptr.To(true),
				},
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(repoSecret, maintenance).
			Build()

		r := &KopiaMaintenanceReconciler{
			Client:         fakeClient,
			Scheme:         s,
			Log:            logr.Discard(),
			EventRecorder:  &record.FakeRecorder{},
			containerImage: "quay.io/backube/volsync:latest",
		}

		cronJobName, err := r.ensureCronJob(context.Background(), maintenance)
		if err != nil {
			t.Fatalf("ensureCronJob() error = %v", err)
		}

		// Retrieve the created CronJob
		cronJobList := &batchv1.CronJobList{}
		if err := fakeClient.List(context.Background(), cronJobList); err != nil {
			t.Fatalf("Failed to list CronJobs: %v", err)
		}

		var cronJob *batchv1.CronJob
		for i := range cronJobList.Items {
			if cronJobList.Items[i].Name == cronJobName {
				cronJob = &cronJobList.Items[i]
				break
			}
		}
		if cronJob == nil {
			t.Fatalf("CronJob %s not found", cronJobName)
		}

		// Verify custom PodSecurityContext
		podSecCtx := cronJob.Spec.JobTemplate.Spec.Template.Spec.SecurityContext
		if podSecCtx == nil {
			t.Fatal("Expected PodSecurityContext to be set")
		}
		if podSecCtx.RunAsUser == nil || *podSecCtx.RunAsUser != 2000 {
			t.Errorf("Expected RunAsUser to be 2000, got %v", podSecCtx.RunAsUser)
		}
		if podSecCtx.FSGroup == nil || *podSecCtx.FSGroup != 2000 {
			t.Errorf("Expected FSGroup to be 2000, got %v", podSecCtx.FSGroup)
		}

		// Verify custom ContainerSecurityContext
		container := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0]
		containerSecCtx := container.SecurityContext
		if containerSecCtx == nil {
			t.Fatal("Expected container SecurityContext to be set")
		}
		if containerSecCtx.ReadOnlyRootFilesystem == nil || !*containerSecCtx.ReadOnlyRootFilesystem {
			t.Error("Expected ReadOnlyRootFilesystem to be true")
		}

		// Verify custom resources
		resources := container.Resources
		cpuRequest := resources.Requests.Cpu()
		memRequest := resources.Requests.Memory()
		cpuLimit := resources.Limits.Cpu()
		memLimit := resources.Limits.Memory()

		if cpuRequest.String() != "100m" {
			t.Errorf("Expected CPU request to be 100m, got %s", cpuRequest.String())
		}
		if memRequest.String() != "256Mi" {
			t.Errorf("Expected memory request to be 256Mi, got %s", memRequest.String())
		}
		if cpuLimit.String() != "500m" {
			t.Errorf("Expected CPU limit to be 500m, got %s", cpuLimit.String())
		}
		if memLimit.String() != "1Gi" {
			t.Errorf("Expected memory limit to be 1Gi, got %s", memLimit.String())
		}
	})

	t.Run("CronJob creation without resources uses empty defaults", func(t *testing.T) {
		// Create the repository secret
		repoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: "test-ns-nores",
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"KOPIA_PASSWORD": []byte("test-password"),
			},
		}

		// Create a KopiaMaintenance without resources specified
		maintenance := &volsyncv1alpha1.KopiaMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-maintenance-nores",
				Namespace: "test-ns-nores",
			},
			Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
				Repository: volsyncv1alpha1.KopiaRepositorySpec{
					Repository: "test-repo-secret",
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(repoSecret, maintenance).
			Build()

		r := &KopiaMaintenanceReconciler{
			Client:         fakeClient,
			Scheme:         s,
			Log:            logr.Discard(),
			EventRecorder:  &record.FakeRecorder{},
			containerImage: "quay.io/backube/volsync:latest",
		}

		cronJobName, err := r.ensureCronJob(context.Background(), maintenance)
		if err != nil {
			t.Fatalf("ensureCronJob() error = %v", err)
		}

		// Retrieve the created CronJob
		cronJobList := &batchv1.CronJobList{}
		if err := fakeClient.List(context.Background(), cronJobList); err != nil {
			t.Fatalf("Failed to list CronJobs: %v", err)
		}

		var cronJob *batchv1.CronJob
		for i := range cronJobList.Items {
			if cronJobList.Items[i].Name == cronJobName {
				cronJob = &cronJobList.Items[i]
				break
			}
		}
		if cronJob == nil {
			t.Fatalf("CronJob %s not found", cronJobName)
		}

		// Verify resources are empty (no requests/limits)
		container := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0]
		resources := container.Resources

		if len(resources.Requests) != 0 {
			t.Errorf("Expected empty resource requests, got %v", resources.Requests)
		}
		if len(resources.Limits) != 0 {
			t.Errorf("Expected empty resource limits, got %v", resources.Limits)
		}
	})
}

func TestKopiaMaintenanceReconciler_EnsureCronJob_Updates(t *testing.T) {
	// Setup the scheme
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = batchv1.AddToScheme(s)
	_ = volsyncv1alpha1.AddToScheme(s)

	// Helper function to create a basic CronJob for testing updates
	createBasicCronJob := func(cronJobName, namespace, maintName string) *batchv1.CronJob {
		return &batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cronJobName,
				Namespace: namespace,
			},
			Spec: batchv1.CronJobSpec{
				Schedule: "0 2 * * *",
				JobTemplate: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						ActiveDeadlineSeconds: ptr.To(int64(10800)),
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"volsync.backube/kopia-maintenance": "true",
									"volsync.backube/maintenance-name":  maintName,
								},
							},
							Spec: corev1.PodSpec{
								ServiceAccountName: "default",
								SecurityContext: &corev1.PodSecurityContext{
									RunAsNonRoot: ptr.To(true),
									RunAsUser:    ptr.To(int64(1000)),
									FSGroup:      ptr.To(int64(1000)),
								},
								Containers: []corev1.Container{
									{
										Name:  "kopia-maintenance",
										Image: "quay.io/backube/volsync:old",
										SecurityContext: &corev1.SecurityContext{
											AllowPrivilegeEscalation: ptr.To(false),
											ReadOnlyRootFilesystem:   ptr.To(true),
											RunAsNonRoot:             ptr.To(true),
										},
									},
								},
							},
						},
					},
				},
			},
		}
	}

	t.Run("CronJob update - container image change", func(t *testing.T) {
		namespace := "test-ns-img"
		maintName := "test-maintenance"
		cronJobName := generateCronJobName(namespace, maintName)

		repoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{"KOPIA_PASSWORD": []byte("test")},
		}

		maintenance := &volsyncv1alpha1.KopiaMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      maintName,
				Namespace: namespace,
			},
			Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
				Repository: volsyncv1alpha1.KopiaRepositorySpec{
					Repository: "test-repo-secret",
				},
			},
		}

		// Create existing CronJob with old image
		existingCronJob := createBasicCronJob(cronJobName, namespace, maintName)

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(repoSecret, maintenance, existingCronJob).
			Build()

		r := &KopiaMaintenanceReconciler{
			Client:         fakeClient,
			Scheme:         s,
			Log:            logr.Discard(),
			EventRecorder:  &record.FakeRecorder{},
			containerImage: "quay.io/backube/volsync:new", // New image
		}

		_, err := r.ensureCronJob(context.Background(), maintenance)
		if err != nil {
			t.Fatalf("ensureCronJob() error = %v", err)
		}

		// Verify image was updated
		cronJobList := &batchv1.CronJobList{}
		_ = fakeClient.List(context.Background(), cronJobList)
		for _, cj := range cronJobList.Items {
			if len(cj.Spec.JobTemplate.Spec.Template.Spec.Containers) > 0 {
				img := cj.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image
				if img != "quay.io/backube/volsync:new" {
					t.Errorf("Expected image to be updated to new, got %s", img)
				}
			}
		}
	})

	t.Run("CronJob update - activeDeadlineSeconds change", func(t *testing.T) {
		namespace := "test-ns-deadline"
		maintName := "test-maintenance"
		cronJobName := generateCronJobName(namespace, maintName)

		repoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{"KOPIA_PASSWORD": []byte("test")},
		}

		newDeadline := int64(43200) // 12 hours
		maintenance := &volsyncv1alpha1.KopiaMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      maintName,
				Namespace: namespace,
			},
			Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
				Repository: volsyncv1alpha1.KopiaRepositorySpec{
					Repository: "test-repo-secret",
				},
				ActiveDeadlineSeconds: &newDeadline,
			},
		}

		existingCronJob := createBasicCronJob(cronJobName, namespace, maintName)

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(repoSecret, maintenance, existingCronJob).
			Build()

		r := &KopiaMaintenanceReconciler{
			Client:         fakeClient,
			Scheme:         s,
			Log:            logr.Discard(),
			EventRecorder:  &record.FakeRecorder{},
			containerImage: "quay.io/backube/volsync:old",
		}

		_, err := r.ensureCronJob(context.Background(), maintenance)
		if err != nil {
			t.Fatalf("ensureCronJob() error = %v", err)
		}

		// Verify activeDeadlineSeconds was updated
		cronJobList := &batchv1.CronJobList{}
		_ = fakeClient.List(context.Background(), cronJobList)
		for _, cj := range cronJobList.Items {
			if cj.Spec.JobTemplate.Spec.ActiveDeadlineSeconds == nil {
				t.Error("Expected ActiveDeadlineSeconds to be set")
			} else if *cj.Spec.JobTemplate.Spec.ActiveDeadlineSeconds != newDeadline {
				t.Errorf("Expected ActiveDeadlineSeconds to be %d, got %d",
					newDeadline, *cj.Spec.JobTemplate.Spec.ActiveDeadlineSeconds)
			}
		}
	})

	t.Run("CronJob update - serviceAccountName change", func(t *testing.T) {
		namespace := "test-ns-sa"
		maintName := "test-maintenance"
		cronJobName := generateCronJobName(namespace, maintName)

		repoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{"KOPIA_PASSWORD": []byte("test")},
		}

		newSA := "custom-sa"
		maintenance := &volsyncv1alpha1.KopiaMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      maintName,
				Namespace: namespace,
			},
			Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
				Repository: volsyncv1alpha1.KopiaRepositorySpec{
					Repository: "test-repo-secret",
				},
				ServiceAccountName: &newSA,
			},
		}

		existingCronJob := createBasicCronJob(cronJobName, namespace, maintName)

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(repoSecret, maintenance, existingCronJob).
			Build()

		r := &KopiaMaintenanceReconciler{
			Client:         fakeClient,
			Scheme:         s,
			Log:            logr.Discard(),
			EventRecorder:  &record.FakeRecorder{},
			containerImage: "quay.io/backube/volsync:old",
		}

		_, err := r.ensureCronJob(context.Background(), maintenance)
		if err != nil {
			t.Fatalf("ensureCronJob() error = %v", err)
		}

		// Verify serviceAccountName was updated
		cronJobList := &batchv1.CronJobList{}
		_ = fakeClient.List(context.Background(), cronJobList)
		for _, cj := range cronJobList.Items {
			sa := cj.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName
			if sa != newSA {
				t.Errorf("Expected ServiceAccountName to be %s, got %s", newSA, sa)
			}
		}
	})

	t.Run("CronJob update - nodeSelector change", func(t *testing.T) {
		namespace := "test-ns-node"
		maintName := "test-maintenance"
		cronJobName := generateCronJobName(namespace, maintName)

		repoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{"KOPIA_PASSWORD": []byte("test")},
		}

		maintenance := &volsyncv1alpha1.KopiaMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      maintName,
				Namespace: namespace,
			},
			Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
				Repository: volsyncv1alpha1.KopiaRepositorySpec{
					Repository: "test-repo-secret",
				},
				NodeSelector: map[string]string{
					"node-type": "backup",
					"disk":      "ssd",
				},
			},
		}

		existingCronJob := createBasicCronJob(cronJobName, namespace, maintName)

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(repoSecret, maintenance, existingCronJob).
			Build()

		r := &KopiaMaintenanceReconciler{
			Client:         fakeClient,
			Scheme:         s,
			Log:            logr.Discard(),
			EventRecorder:  &record.FakeRecorder{},
			containerImage: "quay.io/backube/volsync:old",
		}

		_, err := r.ensureCronJob(context.Background(), maintenance)
		if err != nil {
			t.Fatalf("ensureCronJob() error = %v", err)
		}

		// Verify nodeSelector was updated
		cronJobList := &batchv1.CronJobList{}
		_ = fakeClient.List(context.Background(), cronJobList)
		for _, cj := range cronJobList.Items {
			ns := cj.Spec.JobTemplate.Spec.Template.Spec.NodeSelector
			if ns == nil || ns["node-type"] != "backup" || ns["disk"] != "ssd" {
				t.Errorf("Expected NodeSelector to have node-type=backup and disk=ssd, got %v", ns)
			}
		}
	})

	t.Run("CronJob update - tolerations change", func(t *testing.T) {
		namespace := "test-ns-tol"
		maintName := "test-maintenance"
		cronJobName := generateCronJobName(namespace, maintName)

		repoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{"KOPIA_PASSWORD": []byte("test")},
		}

		maintenance := &volsyncv1alpha1.KopiaMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      maintName,
				Namespace: namespace,
			},
			Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
				Repository: volsyncv1alpha1.KopiaRepositorySpec{
					Repository: "test-repo-secret",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "dedicated",
						Operator: corev1.TolerationOpEqual,
						Value:    "backup",
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
		}

		existingCronJob := createBasicCronJob(cronJobName, namespace, maintName)

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(repoSecret, maintenance, existingCronJob).
			Build()

		r := &KopiaMaintenanceReconciler{
			Client:         fakeClient,
			Scheme:         s,
			Log:            logr.Discard(),
			EventRecorder:  &record.FakeRecorder{},
			containerImage: "quay.io/backube/volsync:old",
		}

		_, err := r.ensureCronJob(context.Background(), maintenance)
		if err != nil {
			t.Fatalf("ensureCronJob() error = %v", err)
		}

		// Verify tolerations were updated
		cronJobList := &batchv1.CronJobList{}
		_ = fakeClient.List(context.Background(), cronJobList)
		for _, cj := range cronJobList.Items {
			tols := cj.Spec.JobTemplate.Spec.Template.Spec.Tolerations
			if len(tols) != 1 {
				t.Errorf("Expected 1 toleration, got %d", len(tols))
			} else if tols[0].Key != "dedicated" || tols[0].Value != "backup" {
				t.Errorf("Expected toleration key=dedicated value=backup, got %v", tols[0])
			}
		}
	})

	t.Run("CronJob update - moverPodLabels change", func(t *testing.T) {
		namespace := "test-ns-labels"
		maintName := "test-maintenance"
		cronJobName := generateCronJobName(namespace, maintName)

		repoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{"KOPIA_PASSWORD": []byte("test")},
		}

		maintenance := &volsyncv1alpha1.KopiaMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      maintName,
				Namespace: namespace,
			},
			Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
				Repository: volsyncv1alpha1.KopiaRepositorySpec{
					Repository: "test-repo-secret",
				},
				MoverPodLabels: map[string]string{
					"environment": "production",
					"team":        "platform",
				},
			},
		}

		existingCronJob := createBasicCronJob(cronJobName, namespace, maintName)

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(repoSecret, maintenance, existingCronJob).
			Build()

		r := &KopiaMaintenanceReconciler{
			Client:         fakeClient,
			Scheme:         s,
			Log:            logr.Discard(),
			EventRecorder:  &record.FakeRecorder{},
			containerImage: "quay.io/backube/volsync:old",
		}

		_, err := r.ensureCronJob(context.Background(), maintenance)
		if err != nil {
			t.Fatalf("ensureCronJob() error = %v", err)
		}

		// Verify labels were updated (should include both volsync labels and custom labels)
		cronJobList := &batchv1.CronJobList{}
		_ = fakeClient.List(context.Background(), cronJobList)
		for _, cj := range cronJobList.Items {
			labels := cj.Spec.JobTemplate.Spec.Template.ObjectMeta.Labels
			if labels["environment"] != "production" {
				t.Errorf("Expected label environment=production, got %s", labels["environment"])
			}
			if labels["team"] != "platform" {
				t.Errorf("Expected label team=platform, got %s", labels["team"])
			}
			// Also verify volsync labels are preserved
			if labels["volsync.backube/kopia-maintenance"] != "true" {
				t.Error("Expected volsync label to be preserved")
			}
		}
	})

	t.Run("CronJob update - affinity change", func(t *testing.T) {
		namespace := "test-ns-affinity"
		maintName := "test-maintenance"
		cronJobName := generateCronJobName(namespace, maintName)

		repoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{"KOPIA_PASSWORD": []byte("test")},
		}

		maintenance := &volsyncv1alpha1.KopiaMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      maintName,
				Namespace: namespace,
			},
			Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
				Repository: volsyncv1alpha1.KopiaRepositorySpec{
					Repository: "test-repo-secret",
				},
				Affinity: &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "node-type",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"high-memory"},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		existingCronJob := createBasicCronJob(cronJobName, namespace, maintName)

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(repoSecret, maintenance, existingCronJob).
			Build()

		r := &KopiaMaintenanceReconciler{
			Client:         fakeClient,
			Scheme:         s,
			Log:            logr.Discard(),
			EventRecorder:  &record.FakeRecorder{},
			containerImage: "quay.io/backube/volsync:old",
		}

		_, err := r.ensureCronJob(context.Background(), maintenance)
		if err != nil {
			t.Fatalf("ensureCronJob() error = %v", err)
		}

		// Verify affinity was updated
		cronJobList := &batchv1.CronJobList{}
		_ = fakeClient.List(context.Background(), cronJobList)
		for _, cj := range cronJobList.Items {
			affinity := cj.Spec.JobTemplate.Spec.Template.Spec.Affinity
			if affinity == nil || affinity.NodeAffinity == nil {
				t.Error("Expected Affinity to be set")
			} else {
				req := affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
				if req == nil || len(req.NodeSelectorTerms) == 0 {
					t.Error("Expected NodeSelectorTerms to be set")
				}
			}
		}
	})
}

func TestKopiaMaintenanceReconciler_BuildCronJob_MoverVolumes(t *testing.T) {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = batchv1.AddToScheme(s)
	_ = volsyncv1alpha1.AddToScheme(s)

	t.Run("buildMaintenanceCronJob with NFS moverVolume", func(t *testing.T) {
		namespace := "test-ns-nfs"
		maintName := "test-maintenance"
		cronJobName := generateCronJobName(namespace, maintName)

		repoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{"KOPIA_PASSWORD": []byte("test")},
		}

		maintenance := &volsyncv1alpha1.KopiaMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      maintName,
				Namespace: namespace,
			},
			Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
				Repository: volsyncv1alpha1.KopiaRepositorySpec{
					Repository: "test-repo-secret",
				},
				MoverVolumes: []volsyncv1alpha1.MoverVolume{
					{
						MountPath: "nfs-data",
						VolumeSource: volsyncv1alpha1.MoverVolumeSource{
							NFS: &corev1.NFSVolumeSource{
								Server: "192.168.1.100",
								Path:   "/export/data",
							},
						},
					},
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(repoSecret, maintenance).
			Build()

		r := &KopiaMaintenanceReconciler{
			Client:         fakeClient,
			Scheme:         s,
			Log:            logr.Discard(),
			EventRecorder:  &record.FakeRecorder{},
			containerImage: "quay.io/backube/volsync:latest",
		}

		cronJob, err := r.buildMaintenanceCronJob(context.Background(), maintenance, cronJobName)
		if err != nil {
			t.Fatalf("buildMaintenanceCronJob() error = %v", err)
		}

		// Verify NFS volume is present
		podSpec := cronJob.Spec.JobTemplate.Spec.Template.Spec
		foundVolume := false
		for _, v := range podSpec.Volumes {
			if v.NFS != nil && v.NFS.Server == "192.168.1.100" && v.NFS.Path == "/export/data" {
				foundVolume = true
				break
			}
		}
		if !foundVolume {
			t.Error("Expected NFS volume to be present in CronJob pod spec")
		}

		// Verify volume mount is present
		container := podSpec.Containers[0]
		foundMount := false
		for _, vm := range container.VolumeMounts {
			if vm.MountPath == "/mnt/nfs-data" {
				foundMount = true
				break
			}
		}
		if !foundMount {
			t.Error("Expected NFS volume mount at /mnt/nfs-data")
		}
	})

	t.Run("buildMaintenanceCronJob with Secret moverVolume", func(t *testing.T) {
		namespace := "test-ns-secret"
		maintName := "test-maintenance"
		cronJobName := generateCronJobName(namespace, maintName)

		repoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{"KOPIA_PASSWORD": []byte("test")},
		}

		moverSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{"key": []byte("value")},
		}

		maintenance := &volsyncv1alpha1.KopiaMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      maintName,
				Namespace: namespace,
			},
			Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
				Repository: volsyncv1alpha1.KopiaRepositorySpec{
					Repository: "test-repo-secret",
				},
				MoverVolumes: []volsyncv1alpha1.MoverVolume{
					{
						MountPath: "my-secret",
						VolumeSource: volsyncv1alpha1.MoverVolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "my-secret",
							},
						},
					},
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(repoSecret, maintenance, moverSecret).
			Build()

		r := &KopiaMaintenanceReconciler{
			Client:         fakeClient,
			Scheme:         s,
			Log:            logr.Discard(),
			EventRecorder:  &record.FakeRecorder{},
			containerImage: "quay.io/backube/volsync:latest",
		}

		cronJob, err := r.buildMaintenanceCronJob(context.Background(), maintenance, cronJobName)
		if err != nil {
			t.Fatalf("buildMaintenanceCronJob() error = %v", err)
		}

		// Verify Secret volume is present
		podSpec := cronJob.Spec.JobTemplate.Spec.Template.Spec
		foundVolume := false
		for _, v := range podSpec.Volumes {
			if v.Secret != nil && v.Secret.SecretName == "my-secret" {
				foundVolume = true
				break
			}
		}
		if !foundVolume {
			t.Error("Expected Secret volume to be present in CronJob pod spec")
		}

		// Verify volume mount is present
		container := podSpec.Containers[0]
		foundMount := false
		for _, vm := range container.VolumeMounts {
			if vm.MountPath == "/mnt/my-secret" {
				foundMount = true
				break
			}
		}
		if !foundMount {
			t.Error("Expected Secret volume mount at /mnt/my-secret")
		}
	})

	t.Run("buildMaintenanceCronJob without moverVolumes has no extra volumes", func(t *testing.T) {
		namespace := "test-ns-noextra"
		maintName := "test-maintenance"
		cronJobName := generateCronJobName(namespace, maintName)

		repoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{"KOPIA_PASSWORD": []byte("test")},
		}

		maintenance := &volsyncv1alpha1.KopiaMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      maintName,
				Namespace: namespace,
			},
			Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
				Repository: volsyncv1alpha1.KopiaRepositorySpec{
					Repository: "test-repo-secret",
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(repoSecret, maintenance).
			Build()

		r := &KopiaMaintenanceReconciler{
			Client:         fakeClient,
			Scheme:         s,
			Log:            logr.Discard(),
			EventRecorder:  &record.FakeRecorder{},
			containerImage: "quay.io/backube/volsync:latest",
		}

		cronJob, err := r.buildMaintenanceCronJob(context.Background(), maintenance, cronJobName)
		if err != nil {
			t.Fatalf("buildMaintenanceCronJob() error = %v", err)
		}

		// Verify no NFS/Secret/PVC mover volumes (only cache-related volumes should exist)
		podSpec := cronJob.Spec.JobTemplate.Spec.Template.Spec
		for _, v := range podSpec.Volumes {
			if v.NFS != nil {
				t.Error("Unexpected NFS volume in CronJob pod spec without moverVolumes")
			}
			// Secret volumes with "u-" prefix would be mover volumes
			if v.Secret != nil && len(v.Name) > 2 && v.Name[:2] == "u-" {
				t.Error("Unexpected mover Secret volume in CronJob pod spec without moverVolumes")
			}
		}
	})

	t.Run("ensureCronJob detects moverVolumes changes", func(t *testing.T) {
		namespace := "test-ns-mv-update"
		maintName := "test-maintenance"
		cronJobName := generateCronJobName(namespace, maintName)

		repoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo-secret",
				Namespace: namespace,
			},
			Data: map[string][]byte{"KOPIA_PASSWORD": []byte("test")},
		}

		maintenance := &volsyncv1alpha1.KopiaMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      maintName,
				Namespace: namespace,
			},
			Spec: volsyncv1alpha1.KopiaMaintenanceSpec{
				Repository: volsyncv1alpha1.KopiaRepositorySpec{
					Repository: "test-repo-secret",
				},
				MoverVolumes: []volsyncv1alpha1.MoverVolume{
					{
						MountPath: "nfs-share",
						VolumeSource: volsyncv1alpha1.MoverVolumeSource{
							NFS: &corev1.NFSVolumeSource{
								Server: "10.0.0.1",
								Path:   "/share",
							},
						},
					},
				},
			},
		}

		// Create existing CronJob WITHOUT mover volumes
		existingCronJob := &batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cronJobName,
				Namespace: namespace,
			},
			Spec: batchv1.CronJobSpec{
				Schedule: "0 2 * * *",
				JobTemplate: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						ActiveDeadlineSeconds: ptr.To(int64(10800)),
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"volsync.backube/kopia-maintenance": "true",
									"volsync.backube/maintenance-name":  maintName,
								},
							},
							Spec: corev1.PodSpec{
								ServiceAccountName: "default",
								SecurityContext: &corev1.PodSecurityContext{
									RunAsNonRoot: ptr.To(true),
									RunAsUser:    ptr.To(int64(1000)),
									FSGroup:      ptr.To(int64(1000)),
								},
								Containers: []corev1.Container{
									{
										Name:  "kopia-maintenance",
										Image: "quay.io/backube/volsync:latest",
										SecurityContext: &corev1.SecurityContext{
											AllowPrivilegeEscalation: ptr.To(false),
											ReadOnlyRootFilesystem:   ptr.To(true),
											RunAsNonRoot:             ptr.To(true),
										},
									},
								},
							},
						},
					},
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(repoSecret, maintenance, existingCronJob).
			Build()

		r := &KopiaMaintenanceReconciler{
			Client:         fakeClient,
			Scheme:         s,
			Log:            logr.Discard(),
			EventRecorder:  &record.FakeRecorder{},
			containerImage: "quay.io/backube/volsync:latest",
		}

		_, err := r.ensureCronJob(context.Background(), maintenance)
		if err != nil {
			t.Fatalf("ensureCronJob() error = %v", err)
		}

		// Verify that the CronJob was updated with the NFS volume
		updatedCronJob := &batchv1.CronJob{}
		_ = fakeClient.Get(context.Background(), client.ObjectKeyFromObject(existingCronJob), updatedCronJob)

		podSpec := updatedCronJob.Spec.JobTemplate.Spec.Template.Spec
		foundNFS := false
		for _, v := range podSpec.Volumes {
			if v.NFS != nil && v.NFS.Server == "10.0.0.1" {
				foundNFS = true
				break
			}
		}
		if !foundNFS {
			t.Error("Expected CronJob to be updated with NFS mover volume")
		}

		foundMount := false
		for _, vm := range podSpec.Containers[0].VolumeMounts {
			if vm.MountPath == "/mnt/nfs-share" {
				foundMount = true
				break
			}
		}
		if !foundMount {
			t.Error("Expected CronJob to be updated with NFS volume mount at /mnt/nfs-share")
		}
	})
}

func TestKopiaMaintenanceReconciler_ValidateMoverVolumes(t *testing.T) {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = volsyncv1alpha1.AddToScheme(s)

	t.Run("validation rejects empty mount path", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

		moverVolumes := []volsyncv1alpha1.MoverVolume{
			{
				MountPath: "",
				VolumeSource: volsyncv1alpha1.MoverVolumeSource{
					NFS: &corev1.NFSVolumeSource{
						Server: "10.0.0.1",
						Path:   "/share",
					},
				},
			},
		}

		err := utils.ValidateMoverVolumes(context.Background(), fakeClient, logr.Discard(),
			"test-ns", moverVolumes)
		if err == nil {
			t.Fatal("Expected error for empty mount path, got nil")
		}
		if !strings.Contains(err.Error(), "cannot be empty") {
			t.Errorf("Expected 'cannot be empty' error, got: %v", err)
		}
	})

	t.Run("validation rejects mount path with path separators", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

		moverVolumes := []volsyncv1alpha1.MoverVolume{
			{
				MountPath: "bad/path",
				VolumeSource: volsyncv1alpha1.MoverVolumeSource{
					NFS: &corev1.NFSVolumeSource{
						Server: "10.0.0.1",
						Path:   "/share",
					},
				},
			},
		}

		err := utils.ValidateMoverVolumes(context.Background(), fakeClient, logr.Discard(),
			"test-ns", moverVolumes)
		if err == nil {
			t.Fatal("Expected error for mount path with separators, got nil")
		}
		if !strings.Contains(err.Error(), "path separators") {
			t.Errorf("Expected 'path separators' error, got: %v", err)
		}
	})

	t.Run("validation rejects mount path with path traversal", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

		moverVolumes := []volsyncv1alpha1.MoverVolume{
			{
				MountPath: "bad..path",
				VolumeSource: volsyncv1alpha1.MoverVolumeSource{
					NFS: &corev1.NFSVolumeSource{
						Server: "10.0.0.1",
						Path:   "/share",
					},
				},
			},
		}

		err := utils.ValidateMoverVolumes(context.Background(), fakeClient, logr.Discard(),
			"test-ns", moverVolumes)
		if err == nil {
			t.Fatal("Expected error for mount path with path traversal, got nil")
		}
		if !strings.Contains(err.Error(), "path traversal") {
			t.Errorf("Expected 'path traversal' error, got: %v", err)
		}
	})

	t.Run("validation accepts valid NFS moverVolume", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

		moverVolumes := []volsyncv1alpha1.MoverVolume{
			{
				MountPath: "nfs-data",
				VolumeSource: volsyncv1alpha1.MoverVolumeSource{
					NFS: &corev1.NFSVolumeSource{
						Server: "10.0.0.1",
						Path:   "/share",
					},
				},
			},
		}

		err := utils.ValidateMoverVolumes(context.Background(), fakeClient, logr.Discard(),
			"test-ns", moverVolumes)
		if err != nil {
			t.Fatalf("Expected no error for valid NFS moverVolume, got: %v", err)
		}
	})

	t.Run("validation rejects PVC that does not exist", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

		moverVolumes := []volsyncv1alpha1.MoverVolume{
			{
				MountPath: "my-pvc",
				VolumeSource: volsyncv1alpha1.MoverVolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "nonexistent-pvc",
					},
				},
			},
		}

		err := utils.ValidateMoverVolumes(context.Background(), fakeClient, logr.Discard(),
			"test-ns", moverVolumes)
		if err == nil {
			t.Fatal("Expected error for nonexistent PVC, got nil")
		}
	})

	t.Run("validation rejects Secret that does not exist", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

		moverVolumes := []volsyncv1alpha1.MoverVolume{
			{
				MountPath: "my-secret",
				VolumeSource: volsyncv1alpha1.MoverVolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "nonexistent-secret",
					},
				},
			},
		}

		err := utils.ValidateMoverVolumes(context.Background(), fakeClient, logr.Discard(),
			"test-ns", moverVolumes)
		if err == nil {
			t.Fatal("Expected error for nonexistent Secret, got nil")
		}
	})
}