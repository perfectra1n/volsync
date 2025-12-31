//go:build !disable_kopia

/*
Copyright 2024 The VolSync authors.

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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Kopia Cache Limits", func() {
	Describe("calculateCacheLimits", func() {
		Context("when cacheCapacity is nil", func() {
			It("should return zeros", func() {
				m := &Mover{cacheCapacity: nil}
				metaMB, contentMB := m.calculateCacheLimits()
				Expect(metaMB).To(Equal(int32(0)))
				Expect(contentMB).To(Equal(int32(0)))
			})
		})

		Context("when cacheCapacity is 1Gi", func() {
			It("should calculate 70% for metadata and 20% for content", func() {
				capacity := resource.NewQuantity(1024*1024*1024, resource.BinarySI)
				m := &Mover{cacheCapacity: capacity}
				metaMB, contentMB := m.calculateCacheLimits()
				Expect(metaMB).To(Equal(int32(716)))    // 1024 * 0.70
				Expect(contentMB).To(Equal(int32(204))) // 1024 * 0.20
			})
		})

		Context("when cacheCapacity is 5Gi", func() {
			It("should calculate 70% for metadata and 20% for content", func() {
				capacity := resource.NewQuantity(5*1024*1024*1024, resource.BinarySI)
				m := &Mover{cacheCapacity: capacity}
				metaMB, contentMB := m.calculateCacheLimits()
				Expect(metaMB).To(Equal(int32(3584)))   // 5120 * 0.70
				Expect(contentMB).To(Equal(int32(1024))) // 5120 * 0.20
			})
		})

		Context("when cacheCapacity is small (100Mi)", func() {
			It("should calculate correctly", func() {
				capacity := resource.NewQuantity(100*1024*1024, resource.BinarySI)
				m := &Mover{cacheCapacity: capacity}
				metaMB, contentMB := m.calculateCacheLimits()
				Expect(metaMB).To(Equal(int32(70)))   // 100 * 0.70
				Expect(contentMB).To(Equal(int32(20))) // 100 * 0.20
			})
		})
	})

	Describe("addCacheLimitEnvVars", func() {
		int32Ptr := func(i int32) *int32 { return &i }

		Context("when explicit limits are provided", func() {
			It("should use the explicit limits", func() {
				m := &Mover{
					metadataCacheSizeLimitMB: int32Ptr(2000),
					contentCacheSizeLimitMB:  int32Ptr(500),
					cacheCapacity:            nil,
				}
				envVars := m.addCacheLimitEnvVars(nil)

				envMap := make(map[string]string)
				for _, ev := range envVars {
					envMap[ev.Name] = ev.Value
				}

				Expect(envMap).To(HaveLen(2))
				Expect(envMap["KOPIA_METADATA_CACHE_SIZE_LIMIT_MB"]).To(Equal("2000"))
				Expect(envMap["KOPIA_CONTENT_CACHE_SIZE_LIMIT_MB"]).To(Equal("500"))
			})
		})

		Context("when limits are nil but capacity is set", func() {
			It("should auto-calculate limits from capacity", func() {
				capacity := resource.NewQuantity(1024*1024*1024, resource.BinarySI)
				m := &Mover{
					metadataCacheSizeLimitMB: nil,
					contentCacheSizeLimitMB:  nil,
					cacheCapacity:            capacity,
				}
				envVars := m.addCacheLimitEnvVars(nil)

				envMap := make(map[string]string)
				for _, ev := range envVars {
					envMap[ev.Name] = ev.Value
				}

				Expect(envMap).To(HaveLen(2))
				Expect(envMap["KOPIA_METADATA_CACHE_SIZE_LIMIT_MB"]).To(Equal("716"))
				Expect(envMap["KOPIA_CONTENT_CACHE_SIZE_LIMIT_MB"]).To(Equal("204"))
			})
		})

		Context("when limits are explicitly set to zero", func() {
			It("should not add env vars (unlimited)", func() {
				capacity := resource.NewQuantity(1024*1024*1024, resource.BinarySI)
				m := &Mover{
					metadataCacheSizeLimitMB: int32Ptr(0),
					contentCacheSizeLimitMB:  int32Ptr(0),
					cacheCapacity:            capacity,
				}
				envVars := m.addCacheLimitEnvVars(nil)
				Expect(envVars).To(BeEmpty())
			})
		})

		Context("when limits are nil and capacity is nil", func() {
			It("should not add env vars", func() {
				m := &Mover{
					metadataCacheSizeLimitMB: nil,
					contentCacheSizeLimitMB:  nil,
					cacheCapacity:            nil,
				}
				envVars := m.addCacheLimitEnvVars(nil)
				Expect(envVars).To(BeEmpty())
			})
		})

		Context("when mixing explicit and auto-calculated limits", func() {
			It("should use explicit for one and auto-calculate for the other", func() {
				capacity := resource.NewQuantity(1024*1024*1024, resource.BinarySI)
				m := &Mover{
					metadataCacheSizeLimitMB: int32Ptr(1000),
					contentCacheSizeLimitMB:  nil,
					cacheCapacity:            capacity,
				}
				envVars := m.addCacheLimitEnvVars(nil)

				envMap := make(map[string]string)
				for _, ev := range envVars {
					envMap[ev.Name] = ev.Value
				}

				Expect(envMap).To(HaveLen(2))
				Expect(envMap["KOPIA_METADATA_CACHE_SIZE_LIMIT_MB"]).To(Equal("1000"))
				Expect(envMap["KOPIA_CONTENT_CACHE_SIZE_LIMIT_MB"]).To(Equal("204"))
			})
		})
	})
})
