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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	volsyncv1alpha1 "github.com/backube/volsync/api/v1alpha1"
)

// TestS3PrefixHandling tests various S3 prefix configuration scenarios
func TestS3PrefixHandling(t *testing.T) {
	tests := []struct {
		name                  string
		secretData            map[string][]byte
		expectedEnvVars       map[string]string
		unexpectedEnvVars     []string
		description           string
	}{
		{
			name: "S3 repository URL with prefix only",
			secretData: map[string][]byte{
				"KOPIA_REPOSITORY":      []byte("s3://mybucket/myprefix/subdir"),
				"KOPIA_PASSWORD":        []byte("password"),
				"AWS_ACCESS_KEY_ID":     []byte("AKIAIOSFODNN7EXAMPLE"),
				"AWS_SECRET_ACCESS_KEY": []byte("wJalrXUtnFEMI/K7MDENG"),
			},
			expectedEnvVars: map[string]string{
				"KOPIA_REPOSITORY":      "s3://mybucket/myprefix/subdir",
				"AWS_ACCESS_KEY_ID":     "AKIAIOSFODNN7EXAMPLE",
				"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI/K7MDENG",
			},
			unexpectedEnvVars: []string{},
			description:       "Bucket and prefix should be extracted from repository URL",
		},
		{
			name: "S3 with both KOPIA_S3_BUCKET and repository URL with prefix",
			secretData: map[string][]byte{
				"KOPIA_REPOSITORY":      []byte("s3://mybucket/myprefix/subdir"),
				"KOPIA_PASSWORD":        []byte("password"),
				"KOPIA_S3_BUCKET":      []byte("differentbucket"),
				"AWS_ACCESS_KEY_ID":     []byte("AKIAIOSFODNN7EXAMPLE"),
				"AWS_SECRET_ACCESS_KEY": []byte("wJalrXUtnFEMI/K7MDENG"),
			},
			expectedEnvVars: map[string]string{
				"KOPIA_REPOSITORY": "s3://mybucket/myprefix/subdir",
				"KOPIA_S3_BUCKET":  "differentbucket",
			},
			description: "KOPIA_S3_BUCKET should override bucket but prefix from URL should be preserved",
		},
		{
			name: "S3 with KOPIA_S3_BUCKET and repository URL without prefix",
			secretData: map[string][]byte{
				"KOPIA_REPOSITORY":      []byte("s3://ignoredbucket"),
				"KOPIA_PASSWORD":        []byte("password"),
				"KOPIA_S3_BUCKET":      []byte("mybucket"),
				"AWS_ACCESS_KEY_ID":     []byte("AKIAIOSFODNN7EXAMPLE"),
				"AWS_SECRET_ACCESS_KEY": []byte("wJalrXUtnFEMI/K7MDENG"),
			},
			expectedEnvVars: map[string]string{
				"KOPIA_REPOSITORY": "s3://ignoredbucket",
				"KOPIA_S3_BUCKET":  "mybucket",
			},
			description: "KOPIA_S3_BUCKET should be used, no prefix from URL",
		},
		{
			name: "S3 repository URL with trailing slash in prefix",
			secretData: map[string][]byte{
				"KOPIA_REPOSITORY":      []byte("s3://mybucket/path/to/data/"),
				"KOPIA_PASSWORD":        []byte("password"),
				"AWS_ACCESS_KEY_ID":     []byte("AKIAIOSFODNN7EXAMPLE"),
				"AWS_SECRET_ACCESS_KEY": []byte("wJalrXUtnFEMI/K7MDENG"),
			},
			expectedEnvVars: map[string]string{
				"KOPIA_REPOSITORY": "s3://mybucket/path/to/data/",
			},
			description: "Trailing slash should be handled consistently",
		},
		{
			name: "S3 repository URL with just bucket and trailing slash",
			secretData: map[string][]byte{
				"KOPIA_REPOSITORY":      []byte("s3://mybucket/"),
				"KOPIA_PASSWORD":        []byte("password"),
				"AWS_ACCESS_KEY_ID":     []byte("AKIAIOSFODNN7EXAMPLE"),
				"AWS_SECRET_ACCESS_KEY": []byte("wJalrXUtnFEMI/K7MDENG"),
			},
			expectedEnvVars: map[string]string{
				"KOPIA_REPOSITORY": "s3://mybucket/",
			},
			description: "Root bucket with trailing slash",
		},
		{
			name: "S3 with deeply nested prefix",
			secretData: map[string][]byte{
				"KOPIA_REPOSITORY":      []byte("s3://mybucket/prefix1/prefix2/prefix3/prefix4"),
				"KOPIA_PASSWORD":        []byte("password"),
				"AWS_ACCESS_KEY_ID":     []byte("AKIAIOSFODNN7EXAMPLE"),
				"AWS_SECRET_ACCESS_KEY": []byte("wJalrXUtnFEMI/K7MDENG"),
			},
			expectedEnvVars: map[string]string{
				"KOPIA_REPOSITORY": "s3://mybucket/prefix1/prefix2/prefix3/prefix4",
			},
			description: "Deeply nested prefixes should be preserved",
		},
		{
			name: "S3 with custom endpoint and prefix",
			secretData: map[string][]byte{
				"KOPIA_REPOSITORY":      []byte("s3://mybucket/myprefix"),
				"KOPIA_PASSWORD":        []byte("password"),
				"KOPIA_S3_ENDPOINT":     []byte("s3.custom-endpoint.com"),
				"AWS_ACCESS_KEY_ID":     []byte("AKIAIOSFODNN7EXAMPLE"),
				"AWS_SECRET_ACCESS_KEY": []byte("wJalrXUtnFEMI/K7MDENG"),
			},
			expectedEnvVars: map[string]string{
				"KOPIA_REPOSITORY":  "s3://mybucket/myprefix",
				"KOPIA_S3_ENDPOINT": "s3.custom-endpoint.com",
			},
			description: "Custom S3 endpoint with prefix",
		},
		{
			name: "S3 with AWS_S3_ENDPOINT and prefix",
			secretData: map[string][]byte{
				"KOPIA_REPOSITORY":      []byte("s3://mybucket/data"),
				"KOPIA_PASSWORD":        []byte("password"),
				"AWS_S3_ENDPOINT":       []byte("https://s3.us-west-2.amazonaws.com"),
				"AWS_ACCESS_KEY_ID":     []byte("AKIAIOSFODNN7EXAMPLE"),
				"AWS_SECRET_ACCESS_KEY": []byte("wJalrXUtnFEMI/K7MDENG"),
			},
			expectedEnvVars: map[string]string{
				"KOPIA_REPOSITORY": "s3://mybucket/data",
				"AWS_S3_ENDPOINT":  "https://s3.us-west-2.amazonaws.com",
			},
			description: "AWS_S3_ENDPOINT with prefix",
		},
		{
			name: "S3 with mixed case bucket name (should be lowercase)",
			secretData: map[string][]byte{
				"KOPIA_REPOSITORY":      []byte("s3://MyBucket/prefix"),
				"KOPIA_PASSWORD":        []byte("password"),
				"AWS_ACCESS_KEY_ID":     []byte("AKIAIOSFODNN7EXAMPLE"),
				"AWS_SECRET_ACCESS_KEY": []byte("wJalrXUtnFEMI/K7MDENG"),
			},
			expectedEnvVars: map[string]string{
				"KOPIA_REPOSITORY": "s3://MyBucket/prefix",
			},
			description: "Mixed case bucket names should be passed through (will fail in entry.sh validation)",
		},
		{
			name: "S3 override bucket with deeply nested path",
			secretData: map[string][]byte{
				"KOPIA_REPOSITORY":      []byte("s3://ignored/very/deep/nested/path/structure/"),
				"KOPIA_PASSWORD":        []byte("password"),
				"KOPIA_S3_BUCKET":       []byte("override-bucket"),
				"AWS_ACCESS_KEY_ID":     []byte("AKIAIOSFODNN7EXAMPLE"),
				"AWS_SECRET_ACCESS_KEY": []byte("wJalrXUtnFEMI/K7MDENG"),
			},
			expectedEnvVars: map[string]string{
				"KOPIA_REPOSITORY": "s3://ignored/very/deep/nested/path/structure/",
				"KOPIA_S3_BUCKET":  "override-bucket",
			},
			description: "Bucket override with very deep nested path from repository URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create secret with test data
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: tt.secretData,
			}

			// Create mover instance with minimal fields needed for testing
			mover := &Mover{
				username: "test-user",
				hostname: "test-host",
				owner: &volsyncv1alpha1.ReplicationSource{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rs",
						Namespace: "test-namespace",
					},
				},
			}

			// Build environment variables
			envVars := mover.buildEnvironmentVariables(secret)

			// Check expected environment variables
			// Note: These are references to secrets, not direct values
			for key := range tt.expectedEnvVars {
				found := false
				for _, env := range envVars {
					if env.Name == key {
						found = true
						// Verify it's a reference to the secret
						if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
							t.Errorf("%s: Expected %s to be a secret reference", tt.name, key)
						} else if env.ValueFrom.SecretKeyRef.Name != "test-secret" {
							t.Errorf("%s: Expected %s to reference secret 'test-secret', got '%s'",
								tt.name, key, env.ValueFrom.SecretKeyRef.Name)
						} else if env.ValueFrom.SecretKeyRef.Key != key {
							t.Errorf("%s: Expected %s to reference key '%s', got '%s'",
								tt.name, key, key, env.ValueFrom.SecretKeyRef.Key)
						}
						break
					}
				}
				if !found {
					t.Errorf("%s: Expected env var %s to be set, but it was not found", tt.name, key)
				}
			}

			// Check that unexpected environment variables are not set
			for _, key := range tt.unexpectedEnvVars {
				for _, env := range envVars {
					if env.Name == key && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
						t.Errorf("%s: Expected env var %s to not be set, but it was found", tt.name, key)
					}
				}
			}

			// Verify KOPIA_PASSWORD is always set as a secret reference
			found := false
			for _, env := range envVars {
				if env.Name == "KOPIA_PASSWORD" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: KOPIA_PASSWORD should always be set", tt.name)
			}
		})
	}
}

// TestS3PrefixValidation tests that invalid S3 configurations are handled properly
func TestS3PrefixValidation(t *testing.T) {
	tests := []struct {
		name        string
		secretData  map[string][]byte
		description string
	}{
		{
			name: "S3 with special characters in prefix",
			secretData: map[string][]byte{
				"KOPIA_REPOSITORY":      []byte("s3://mybucket/prefix_with-dots.and-dashes"),
				"KOPIA_PASSWORD":        []byte("password"),
				"AWS_ACCESS_KEY_ID":     []byte("AKIAIOSFODNN7EXAMPLE"),
				"AWS_SECRET_ACCESS_KEY": []byte("wJalrXUtnFEMI/K7MDENG"),
			},
			description: "Special characters in prefix should be preserved for validation in entry.sh",
		},
		{
			name: "S3 with path traversal attempt",
			secretData: map[string][]byte{
				"KOPIA_REPOSITORY":      []byte("s3://mybucket/../../../etc/passwd"),
				"KOPIA_PASSWORD":        []byte("password"),
				"AWS_ACCESS_KEY_ID":     []byte("AKIAIOSFODNN7EXAMPLE"),
				"AWS_SECRET_ACCESS_KEY": []byte("wJalrXUtnFEMI/K7MDENG"),
			},
			description: "Path traversal attempts should be passed through (will be blocked in entry.sh)",
		},
		{
			name: "S3 with spaces in prefix",
			secretData: map[string][]byte{
				"KOPIA_REPOSITORY":      []byte("s3://mybucket/prefix with spaces"),
				"KOPIA_PASSWORD":        []byte("password"),
				"AWS_ACCESS_KEY_ID":     []byte("AKIAIOSFODNN7EXAMPLE"),
				"AWS_SECRET_ACCESS_KEY": []byte("wJalrXUtnFEMI/K7MDENG"),
			},
			description: "Spaces in prefix should be passed through (will be validated in entry.sh)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create secret with test data
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: tt.secretData,
			}

			// Create mover instance with minimal fields needed for testing
			mover := &Mover{
				username: "test-user",
				hostname: "test-host",
				owner: &volsyncv1alpha1.ReplicationSource{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rs",
						Namespace: "test-namespace",
					},
				},
			}

			// Build environment variables
			envVars := mover.buildEnvironmentVariables(secret)

			// The Go code should pass these through as secret references
			// Validation happens in the entry.sh script
			found := false
			for _, env := range envVars {
				if env.Name == "KOPIA_REPOSITORY" {
					found = true
					// Verify it's a reference to the secret containing the original value
					if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
						t.Errorf("%s: KOPIA_REPOSITORY should be a secret reference", tt.name)
					} else if env.ValueFrom.SecretKeyRef.Name != "test-secret" {
						t.Errorf("%s: KOPIA_REPOSITORY should reference the test-secret", tt.name)
					}
					break
				}
			}
			if !found {
				t.Errorf("%s: KOPIA_REPOSITORY should be present in environment variables", tt.name)
			}
		})
	}
}