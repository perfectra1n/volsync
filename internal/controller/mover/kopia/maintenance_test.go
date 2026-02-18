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

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	volsyncv1alpha1 "github.com/backube/volsync/api/v1alpha1"
)

var _ = Describe("MaintenanceManager", func() {
	var scheme *runtime.Scheme

	BeforeEach(func() {
		// Set operator namespace for tests
		os.Setenv("POD_NAMESPACE", "test-operator-namespace")

		// Create a scheme with required types
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(batchv1.AddToScheme(scheme)).To(Succeed())
		Expect(rbacv1.AddToScheme(scheme)).To(Succeed())
		Expect(volsyncv1alpha1.AddToScheme(scheme)).To(Succeed())
	})

	AfterEach(func() {
		os.Unsetenv("POD_NAMESPACE")
	})

	// NOTE: ReconcileMaintenanceForSource test removed - maintenance is now managed by KopiaMaintenance CRD

	Describe("MaintenanceDisabledWhenIntervalIsZero", func() {
		It("should not create CronJobs when maintenance is disabled", func() {
			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			logger := logr.Discard()
			manager := NewMaintenanceManager(client, logger, "test-image:latest", nil)

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
			Expect(err).NotTo(HaveOccurred())

			// Verify that no CronJob was created
			cronJobList := &batchv1.CronJobList{}
			err = client.List(context.Background(), cronJobList, ctrlclient.InNamespace("test-namespace"))
			Expect(err).NotTo(HaveOccurred())
			Expect(cronJobList.Items).To(BeEmpty())
		})
	})

	Describe("CleanupOrphanedMaintenanceCronJobs", func() {
		It("should delete orphaned CronJobs and secrets", func() {
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
			manager := NewMaintenanceManager(client, logger, "test-image:latest", nil)

			// Cleanup orphaned CronJobs (no sources exist)
			err := manager.CleanupOrphanedMaintenanceCronJobs(context.Background(), "test-namespace")
			Expect(err).NotTo(HaveOccurred())

			// Verify that the orphaned CronJob was deleted from operator namespace
			cronJobList := &batchv1.CronJobList{}
			err = client.List(context.Background(), cronJobList, ctrlclient.InNamespace("test-operator-namespace"))
			Expect(err).NotTo(HaveOccurred())
			Expect(cronJobList.Items).To(BeEmpty())

			// Verify that the orphaned secret was also deleted
			secretList := &corev1.SecretList{}
			err = client.List(context.Background(), secretList,
				ctrlclient.InNamespace("test-operator-namespace"),
				ctrlclient.MatchingLabels{maintenanceSecretLabel: "true"})
			Expect(err).NotTo(HaveOccurred())
			Expect(secretList.Items).To(BeEmpty())
		})
	})

	Describe("RepositoryConfigHash", func() {
		It("should produce the same hash for configs with same repository regardless of namespace/schedule", func() {
			config1 := &RepositoryConfig{
				Repository: "repo1",
				Namespace:  "ns1",
				Schedule:   "0 2 * * *",
			}

			config2 := &RepositoryConfig{
				Repository: "repo1",
				Namespace:  "ns2",       // Different namespace
				Schedule:   "0 3 * * *", // Different schedule
			}

			// Phase 1: Hash should be the same since only repository matters
			Expect(config1.Hash()).To(Equal(config2.Hash()))
		})

		It("should produce different hashes for different repositories", func() {
			config1 := &RepositoryConfig{
				Repository: "repo1",
				Namespace:  "ns1",
				Schedule:   "0 2 * * *",
			}

			config3 := &RepositoryConfig{
				Repository: "repo2",
				Namespace:  "ns1",
				Schedule:   "0 2 * * *",
			}

			Expect(config1.Hash()).NotTo(Equal(config3.Hash()))
		})

		It("should produce the same hash for identical configs with CustomCA", func() {
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

			Expect(config4.Hash()).To(Equal(config5.Hash()))
		})

		It("should produce different hashes for configs with different CA settings", func() {
			config1 := &RepositoryConfig{
				Repository: "repo1",
				Namespace:  "ns1",
				Schedule:   "0 2 * * *",
			}

			config4 := &RepositoryConfig{
				Repository: "repo1",
				CustomCA: &volsyncv1alpha1.CustomCASpec{
					SecretName: "ca-secret",
					Key:        "ca.crt",
				},
			}

			Expect(config1.Hash()).NotTo(Equal(config4.Hash()))
		})
	})

	Describe("buildMaintenanceVolumes", func() {
		var manager *MaintenanceManager

		BeforeEach(func() {
			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			logger := logr.Discard()
			manager = NewMaintenanceManager(client, logger, "test-image:latest", nil)
		})

		It("should default cache volume size to 8Gi when CacheCapacity is nil", func() {
			repoConfig := &RepositoryConfig{
				Repository: "repo1",
				Namespace:  "ns1",
			}

			volumes, volumeMounts := manager.buildMaintenanceVolumes(repoConfig)

			// Find the cache volume
			var cacheVolume *corev1.Volume
			for i := range volumes {
				if volumes[i].Name == kopiaCache {
					cacheVolume = &volumes[i]
					break
				}
			}
			Expect(cacheVolume).NotTo(BeNil())
			Expect(cacheVolume.VolumeSource.EmptyDir).NotTo(BeNil())

			expectedSize := resource.MustParse("8Gi")
			Expect(cacheVolume.VolumeSource.EmptyDir.SizeLimit.Cmp(expectedSize)).To(Equal(0))

			// Verify cache mount exists
			var cacheMount *corev1.VolumeMount
			for i := range volumeMounts {
				if volumeMounts[i].Name == kopiaCache {
					cacheMount = &volumeMounts[i]
					break
				}
			}
			Expect(cacheMount).NotTo(BeNil())
			Expect(cacheMount.MountPath).To(Equal(kopiaCacheMountPath))
		})

		It("should use custom CacheCapacity when specified", func() {
			customSize := resource.MustParse("4Gi")
			repoConfig := &RepositoryConfig{
				Repository:    "repo1",
				Namespace:     "ns1",
				CacheCapacity: &customSize,
			}

			volumes, _ := manager.buildMaintenanceVolumes(repoConfig)

			// Find the cache volume
			var cacheVolume *corev1.Volume
			for i := range volumes {
				if volumes[i].Name == kopiaCache {
					cacheVolume = &volumes[i]
					break
				}
			}
			Expect(cacheVolume).NotTo(BeNil())
			Expect(cacheVolume.VolumeSource.EmptyDir).NotTo(BeNil())
			Expect(cacheVolume.VolumeSource.EmptyDir.SizeLimit.Cmp(customSize)).To(Equal(0))
		})
	})

	Describe("buildMaintenanceEnvVars", func() {
		var manager *MaintenanceManager

		BeforeEach(func() {
			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			logger := logr.Discard()
			manager = NewMaintenanceManager(client, logger, "test-image:latest", nil)
		})

		It("should auto-calculate cache limits from default 8Gi capacity", func() {
			repoConfig := &RepositoryConfig{
				Repository: "repo1",
				Namespace:  "ns1",
			}

			envVars := manager.buildMaintenanceEnvVars(repoConfig)
			envMap := make(map[string]string)
			for _, env := range envVars {
				envMap[env.Name] = env.Value
			}

			// 8Gi = 8192 MB -> metadata = 70% = 5734, content = 20% = 1638
			Expect(envMap).To(HaveKey("KOPIA_METADATA_CACHE_SIZE_LIMIT_MB"))
			Expect(envMap["KOPIA_METADATA_CACHE_SIZE_LIMIT_MB"]).To(Equal("5734"))
			Expect(envMap).To(HaveKey("KOPIA_CONTENT_CACHE_SIZE_LIMIT_MB"))
			Expect(envMap["KOPIA_CONTENT_CACHE_SIZE_LIMIT_MB"]).To(Equal("1638"))
			Expect(envMap).To(HaveKey("KOPIA_CACHE_CAPACITY_BYTES"))
			Expect(envMap["KOPIA_CACHE_CAPACITY_BYTES"]).To(Equal("8589934592")) // 8Gi in bytes
		})

		It("should auto-calculate cache limits from custom CacheCapacity", func() {
			customSize := resource.MustParse("1Gi")
			repoConfig := &RepositoryConfig{
				Repository:    "repo1",
				Namespace:     "ns1",
				CacheCapacity: &customSize,
			}

			envVars := manager.buildMaintenanceEnvVars(repoConfig)
			envMap := make(map[string]string)
			for _, env := range envVars {
				envMap[env.Name] = env.Value
			}

			// 1Gi = 1024 MB -> metadata = 70% = 716, content = 20% = 204
			Expect(envMap).To(HaveKey("KOPIA_METADATA_CACHE_SIZE_LIMIT_MB"))
			Expect(envMap["KOPIA_METADATA_CACHE_SIZE_LIMIT_MB"]).To(Equal("716"))
			Expect(envMap).To(HaveKey("KOPIA_CONTENT_CACHE_SIZE_LIMIT_MB"))
			Expect(envMap["KOPIA_CONTENT_CACHE_SIZE_LIMIT_MB"]).To(Equal("204"))
			Expect(envMap).To(HaveKey("KOPIA_CACHE_CAPACITY_BYTES"))
			Expect(envMap["KOPIA_CACHE_CAPACITY_BYTES"]).To(Equal("1073741824")) // 1Gi in bytes
		})
	})
})
