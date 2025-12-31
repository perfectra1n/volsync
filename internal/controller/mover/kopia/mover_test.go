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
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestCalculateCacheLimits(t *testing.T) {
	tests := []struct {
		name             string
		cacheCapacity    *resource.Quantity
		expectedMetadata int32
		expectedContent  int32
	}{
		{
			name:             "nil capacity returns zeros",
			cacheCapacity:    nil,
			expectedMetadata: 0,
			expectedContent:  0,
		},
		{
			name:             "1Gi capacity calculates 70% and 20%",
			cacheCapacity:    resource.NewQuantity(1024*1024*1024, resource.BinarySI),
			expectedMetadata: 716, // 1024 * 0.70
			expectedContent:  204, // 1024 * 0.20
		},
		{
			name:             "5Gi capacity calculates 70% and 20%",
			cacheCapacity:    resource.NewQuantity(5*1024*1024*1024, resource.BinarySI),
			expectedMetadata: 3584, // 5120 * 0.70
			expectedContent:  1024, // 5120 * 0.20
		},
		{
			name:             "small capacity (100Mi) calculates correctly",
			cacheCapacity:    resource.NewQuantity(100*1024*1024, resource.BinarySI),
			expectedMetadata: 70, // 100 * 0.70
			expectedContent:  20, // 100 * 0.20
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Mover{cacheCapacity: tt.cacheCapacity}
			metaMB, contentMB := m.calculateCacheLimits()

			if metaMB != tt.expectedMetadata {
				t.Errorf("metadataMB = %d, want %d", metaMB, tt.expectedMetadata)
			}
			if contentMB != tt.expectedContent {
				t.Errorf("contentMB = %d, want %d", contentMB, tt.expectedContent)
			}
		})
	}
}

//nolint:funlen
func TestAddCacheLimitEnvVars(t *testing.T) {
	int32Ptr := func(i int32) *int32 { return &i }

	tests := []struct {
		name                     string
		metadataCacheSizeLimitMB *int32
		contentCacheSizeLimitMB  *int32
		cacheCapacity            *resource.Quantity
		expectedEnvVars          map[string]string // env var name -> value
	}{
		{
			name:                     "explicit limits are used",
			metadataCacheSizeLimitMB: int32Ptr(2000),
			contentCacheSizeLimitMB:  int32Ptr(500),
			cacheCapacity:            nil,
			expectedEnvVars: map[string]string{
				"KOPIA_METADATA_CACHE_SIZE_LIMIT_MB": "2000",
				"KOPIA_CONTENT_CACHE_SIZE_LIMIT_MB":  "500",
			},
		},
		{
			name:                     "nil limits with capacity triggers auto-calc",
			metadataCacheSizeLimitMB: nil,
			contentCacheSizeLimitMB:  nil,
			cacheCapacity:            resource.NewQuantity(1024*1024*1024, resource.BinarySI),
			expectedEnvVars: map[string]string{
				"KOPIA_METADATA_CACHE_SIZE_LIMIT_MB": "716",
				"KOPIA_CONTENT_CACHE_SIZE_LIMIT_MB":  "204",
			},
		},
		{
			name:                     "zero limits mean unlimited - no env vars",
			metadataCacheSizeLimitMB: int32Ptr(0),
			contentCacheSizeLimitMB:  int32Ptr(0),
			cacheCapacity:            resource.NewQuantity(1024*1024*1024, resource.BinarySI),
			expectedEnvVars:          map[string]string{},
		},
		{
			name:                     "nil limits without capacity - no env vars",
			metadataCacheSizeLimitMB: nil,
			contentCacheSizeLimitMB:  nil,
			cacheCapacity:            nil,
			expectedEnvVars:          map[string]string{},
		},
		{
			name:                     "mixed explicit and auto-calc",
			metadataCacheSizeLimitMB: int32Ptr(1000),
			contentCacheSizeLimitMB:  nil,
			cacheCapacity:            resource.NewQuantity(1024*1024*1024, resource.BinarySI),
			expectedEnvVars: map[string]string{
				"KOPIA_METADATA_CACHE_SIZE_LIMIT_MB": "1000",
				"KOPIA_CONTENT_CACHE_SIZE_LIMIT_MB":  "204",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Mover{
				metadataCacheSizeLimitMB: tt.metadataCacheSizeLimitMB,
				contentCacheSizeLimitMB:  tt.contentCacheSizeLimitMB,
				cacheCapacity:            tt.cacheCapacity,
			}

			envVars := m.addCacheLimitEnvVars(nil)

			// Convert to map for easier comparison
			envMap := make(map[string]string)
			for _, ev := range envVars {
				envMap[ev.Name] = ev.Value
			}

			if len(envMap) != len(tt.expectedEnvVars) {
				t.Errorf("got %d env vars, want %d", len(envMap), len(tt.expectedEnvVars))
			}

			for name, expectedValue := range tt.expectedEnvVars {
				if gotValue, ok := envMap[name]; !ok {
					t.Errorf("missing env var %s", name)
				} else if gotValue != expectedValue {
					t.Errorf("env var %s = %s, want %s", name, gotValue, expectedValue)
				}
			}
		})
	}
}
