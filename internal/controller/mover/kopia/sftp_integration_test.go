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

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	volsyncv1alpha1 "github.com/backube/volsync/api/v1alpha1"
)

// TestSFTPPasswordConfiguration tests that SFTP password authentication is properly configured
func TestSFTPPasswordConfiguration(t *testing.T) {
	tests := []struct {
		name           string
		secretData     map[string][]byte
		expectPassword bool
		expectKeyFile  bool
	}{
		{
			name: "SFTP with password only",
			secretData: map[string][]byte{
				"SFTP_HOST":     []byte("sftp.example.com"),
				"SFTP_USERNAME": []byte("user"),
				"SFTP_PASSWORD": []byte("secret-pass"),
				"SFTP_PATH":     []byte("/backup"),
			},
			expectPassword: true,
			expectKeyFile:  false,
		},
		{
			name: "SFTP with SSH key only",
			secretData: map[string][]byte{
				"SFTP_HOST":     []byte("sftp.example.com"),
				"SFTP_USERNAME": []byte("user"),
				"SFTP_KEY_FILE": []byte("ssh-private-key-content"),
				"SFTP_PATH":     []byte("/backup"),
			},
			expectPassword: false,
			expectKeyFile:  true,
		},
		{
			name: "SFTP with both password and SSH key (key takes precedence)",
			secretData: map[string][]byte{
				"SFTP_HOST":     []byte("sftp.example.com"),
				"SFTP_USERNAME": []byte("user"),
				"SFTP_PASSWORD": []byte("secret-pass"),
				"SFTP_KEY_FILE": []byte("ssh-private-key-content"),
				"SFTP_PATH":     []byte("/backup"),
			},
			expectPassword: true,
			expectKeyFile:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock owner
			owner := &volsyncv1alpha1.ReplicationSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-source",
					Namespace: "test-namespace",
				},
			}

			mover := &Mover{
				logger:   logr.Discard(),
				owner:    owner,
				username: "test-user",
				hostname: "test-host",
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: tt.secretData,
			}

			// Build environment variables
			envVars := mover.buildEnvironmentVariables(secret)

			// Check SFTP_PASSWORD
			// Note: Environment variables are always created with optional: true,
			// regardless of whether they exist in the secret data
			foundPassword := false
			for _, env := range envVars {
				if env.Name == "SFTP_PASSWORD" {
					foundPassword = true
					// Verify it's from secret
					if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
						t.Error("SFTP_PASSWORD should be from secret")
					}
					if env.ValueFrom.SecretKeyRef.Name != secret.Name {
						t.Errorf("SFTP_PASSWORD should reference secret %s", secret.Name)
					}
					if env.ValueFrom.SecretKeyRef.Key != "SFTP_PASSWORD" {
						t.Error("SFTP_PASSWORD should reference correct key")
					}
					// Should be optional
					if !*env.ValueFrom.SecretKeyRef.Optional {
						t.Error("SFTP_PASSWORD should be optional")
					}
					break
				}
			}

			// SFTP_PASSWORD env var is always present, but it only has a value if it exists in the secret
			if !foundPassword {
				t.Error("SFTP_PASSWORD environment variable should always be present")
			}

			// Check SFTP_KEY_FILE handling via credentials configuration
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "kopia",
					Image: "test-image",
					Env:   []corev1.EnvVar{},
				}},
				Volumes: []corev1.Volume{},
			}

			mover.configureCredentials(podSpec, secret)

			if tt.expectKeyFile {
				// Check that SSH key file is mounted
				foundKeyMount := false
				for _, mount := range podSpec.Containers[0].VolumeMounts {
					if mount.MountPath == "/credentials" {
						foundKeyMount = true
						break
					}
				}
				if !foundKeyMount {
					t.Error("Expected SSH key mount not found")
				}

				// Check the volume contains the SSH key
				if len(podSpec.Volumes) == 0 {
					t.Error("Expected volume for SSH key not found")
				} else {
					volume := podSpec.Volumes[0]
					foundKey := false
					for _, item := range volume.VolumeSource.Secret.Items {
						if item.Path == "sftp_key" {
							foundKey = true
							// Verify permissions
							if item.Mode == nil || *item.Mode != 0600 {
								t.Errorf("SSH key should have mode 0600, got %v", item.Mode)
							}
							break
						}
					}
					if !foundKey {
						t.Error("SSH key file not found in volume")
					}
				}
			}
		})
	}
}

// TestSFTPKnownHostsConfiguration tests SFTP known hosts environment variables
func TestSFTPKnownHostsConfiguration(t *testing.T) {
	tests := []struct {
		name                 string
		secretData           map[string][]byte
		expectKnownHosts     bool
		expectKnownHostsData bool
	}{
		{
			name: "SFTP with known hosts file",
			secretData: map[string][]byte{
				"SFTP_HOST":        []byte("sftp.example.com"),
				"SFTP_USERNAME":    []byte("user"),
				"SFTP_PASSWORD":    []byte("secret-pass"),
				"SFTP_KNOWN_HOSTS": []byte("/etc/ssh/known_hosts"),
			},
			expectKnownHosts:     true,
			expectKnownHostsData: false,
		},
		{
			name: "SFTP with known hosts data",
			secretData: map[string][]byte{
				"SFTP_HOST":             []byte("sftp.example.com"),
				"SFTP_USERNAME":         []byte("user"),
				"SFTP_PASSWORD":         []byte("secret-pass"),
				"SFTP_KNOWN_HOSTS_DATA": []byte("sftp.example.com ssh-rsa AAAAB3NzaC1yc2E..."),
			},
			expectKnownHosts:     false,
			expectKnownHostsData: true,
		},
		{
			name: "SFTP with both known hosts file and data",
			secretData: map[string][]byte{
				"SFTP_HOST":             []byte("sftp.example.com"),
				"SFTP_USERNAME":         []byte("user"),
				"SFTP_PASSWORD":         []byte("secret-pass"),
				"SFTP_KNOWN_HOSTS":      []byte("/etc/ssh/known_hosts"),
				"SFTP_KNOWN_HOSTS_DATA": []byte("sftp.example.com ssh-rsa AAAAB3NzaC1yc2E..."),
			},
			expectKnownHosts:     true,
			expectKnownHostsData: true,
		},
		{
			name: "SFTP without known hosts",
			secretData: map[string][]byte{
				"SFTP_HOST":     []byte("sftp.example.com"),
				"SFTP_USERNAME": []byte("user"),
				"SFTP_PASSWORD": []byte("secret-pass"),
			},
			expectKnownHosts:     false,
			expectKnownHostsData: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := &volsyncv1alpha1.ReplicationSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-source",
					Namespace: "test-namespace",
				},
			}

			mover := &Mover{
				logger:   logr.Discard(),
				owner:    owner,
				username: "test-user",
				hostname: "test-host",
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: tt.secretData,
			}

			envVars := mover.buildEnvironmentVariables(secret)

			// Check SFTP_KNOWN_HOSTS
			// Note: Both env vars are always present, but only have values if they exist in the secret
			foundKnownHosts := false
			foundKnownHostsData := false
			for _, env := range envVars {
				if env.Name == "SFTP_KNOWN_HOSTS" {
					foundKnownHosts = true
					// Verify it's optional
					if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil || !*env.ValueFrom.SecretKeyRef.Optional {
						t.Error("SFTP_KNOWN_HOSTS should be optional")
					}
				}
				if env.Name == "SFTP_KNOWN_HOSTS_DATA" {
					foundKnownHostsData = true
					// Verify it's optional
					if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil || !*env.ValueFrom.SecretKeyRef.Optional {
						t.Error("SFTP_KNOWN_HOSTS_DATA should be optional")
					}
				}
			}

			// Both variables should always be present
			if !foundKnownHosts {
				t.Error("SFTP_KNOWN_HOSTS environment variable should always be present")
			}
			if !foundKnownHostsData {
				t.Error("SFTP_KNOWN_HOSTS_DATA environment variable should always be present")
			}
		})
	}
}

// TestAdditionalArgsWithAllRepositoryTypes tests that KOPIA_ADDITIONAL_ARGS works with all repository types
func TestAdditionalArgsWithAllRepositoryTypes(t *testing.T) {
	repositoryTypes := []struct {
		name       string
		secretData map[string][]byte
	}{
		{
			name: "S3 repository",
			secretData: map[string][]byte{
				"KOPIA_S3_BUCKET":       []byte("my-bucket"),
				"AWS_ACCESS_KEY_ID":     []byte("access-key"),
				"AWS_SECRET_ACCESS_KEY": []byte("secret-key"),
			},
		},
		{
			name: "Azure repository",
			secretData: map[string][]byte{
				"KOPIA_AZURE_CONTAINER":      []byte("my-container"),
				"KOPIA_AZURE_STORAGE_ACCOUNT": []byte("storage-account"),
				"KOPIA_AZURE_STORAGE_KEY":     []byte("storage-key"),
			},
		},
		{
			name: "GCS repository",
			secretData: map[string][]byte{
				"KOPIA_GCS_BUCKET":               []byte("my-bucket"),
				"GOOGLE_APPLICATION_CREDENTIALS": []byte(`{"type": "service_account"}`),
			},
		},
		{
			name: "Filesystem repository",
			secretData: map[string][]byte{
				"KOPIA_REPOSITORY": []byte("filesystem:///backup"),
			},
		},
		{
			name: "B2 repository",
			secretData: map[string][]byte{
				"KOPIA_B2_BUCKET":    []byte("my-bucket"),
				"B2_ACCOUNT_ID":      []byte("account-id"),
				"B2_APPLICATION_KEY": []byte("app-key"),
			},
		},
		{
			name: "WebDAV repository",
			secretData: map[string][]byte{
				"WEBDAV_URL":      []byte("https://webdav.example.com"),
				"WEBDAV_USERNAME": []byte("user"),
				"WEBDAV_PASSWORD": []byte("pass"),
			},
		},
		{
			name: "SFTP repository",
			secretData: map[string][]byte{
				"SFTP_HOST":     []byte("sftp.example.com"),
				"SFTP_USERNAME": []byte("user"),
				"SFTP_PASSWORD": []byte("pass"),
				"SFTP_PATH":     []byte("/backup"),
			},
		},
		{
			name: "Rclone repository",
			secretData: map[string][]byte{
				"RCLONE_REMOTE_PATH": []byte("remote:path"),
				"RCLONE_CONFIG":      []byte("[remote]\ntype = s3\n..."),
			},
		},
		{
			name: "Google Drive repository",
			secretData: map[string][]byte{
				"GOOGLE_DRIVE_FOLDER_ID":   []byte("folder-id"),
				"GOOGLE_DRIVE_CREDENTIALS": []byte(`{"type": "oauth2"}`),
			},
		},
	}

	additionalArgs := []string{
		"--one-file-system",
		"--parallel=8",
		"--compression=zstd",
	}

	for _, repo := range repositoryTypes {
		t.Run(repo.name, func(t *testing.T) {
			owner := &volsyncv1alpha1.ReplicationSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-source",
					Namespace: "test-namespace",
				},
			}

			mover := &Mover{
				logger:         logr.Discard(),
				owner:          owner,
				username:       "test-user",
				hostname:       "test-host",
				additionalArgs: additionalArgs,
			}

			// Add common password
			repo.secretData["KOPIA_PASSWORD"] = []byte("test-password")

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: repo.secretData,
			}

			envVars := mover.buildEnvironmentVariables(secret)

			// Check that KOPIA_ADDITIONAL_ARGS is included
			foundAdditionalArgs := false
			for _, env := range envVars {
				if env.Name == "KOPIA_ADDITIONAL_ARGS" {
					foundAdditionalArgs = true
					expected := "--one-file-system|VOLSYNC_ARG_SEP|--parallel=8|VOLSYNC_ARG_SEP|--compression=zstd"
					if env.Value != expected {
						t.Errorf("Expected KOPIA_ADDITIONAL_ARGS value '%s', got '%s'", expected, env.Value)
					}
					break
				}
			}

			if !foundAdditionalArgs {
				t.Errorf("KOPIA_ADDITIONAL_ARGS not found for %s", repo.name)
			}
		})
	}
}

// TestExecuteRepositoryCommandFunction tests the execute_repository_command function behavior
func TestExecuteRepositoryCommandFunction(t *testing.T) {
	// This test validates that the execute_repository_command function in entry.sh
	// would properly apply KOPIA_ADDITIONAL_ARGS for all repository types
	// The actual shell script testing would require a different approach (e.g., BATS)
	// but we can test that the Go code sets up the environment correctly

	tests := []struct {
		name           string
		additionalArgs []string
		connectionType string // "direct", "json", "legacy"
		expectEnvVar   bool
	}{
		{
			name:           "Direct connection with additional args",
			additionalArgs: []string{"--one-file-system"},
			connectionType: "direct",
			expectEnvVar:   true,
		},
		{
			name:           "JSON config with additional args",
			additionalArgs: []string{"--parallel=8"},
			connectionType: "json",
			expectEnvVar:   true,
		},
		{
			name:           "Legacy config with additional args",
			additionalArgs: []string{"--compression=zstd"},
			connectionType: "legacy",
			expectEnvVar:   true,
		},
		{
			name:           "No additional args",
			additionalArgs: nil,
			connectionType: "direct",
			expectEnvVar:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := &volsyncv1alpha1.ReplicationSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-source",
					Namespace: "test-namespace",
				},
			}

			mover := &Mover{
				logger:         logr.Discard(),
				owner:          owner,
				username:       "test-user",
				hostname:       "test-host",
				additionalArgs: tt.additionalArgs,
			}

			secretData := map[string][]byte{
				"KOPIA_PASSWORD": []byte("test-password"),
			}

			// Add connection-specific data
			switch tt.connectionType {
			case "json":
				secretData["KOPIA_CONFIG_JSON"] = []byte(`{"repository": {"s3": {"bucket": "test"}}}`)
			case "legacy":
				secretData["KOPIA_CONFIG_PATH"] = []byte("/config/kopia")
			default: // direct
				secretData["KOPIA_S3_BUCKET"] = []byte("test-bucket")
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: secretData,
			}

			envVars := mover.buildEnvironmentVariables(secret)

			foundAdditionalArgs := false
			for _, env := range envVars {
				if env.Name == "KOPIA_ADDITIONAL_ARGS" {
					foundAdditionalArgs = true
					break
				}
			}

			if tt.expectEnvVar != foundAdditionalArgs {
				t.Errorf("KOPIA_ADDITIONAL_ARGS presence: expected %v, got %v", tt.expectEnvVar, foundAdditionalArgs)
			}
		})
	}
}

// TestSFTPPortConfiguration tests that SFTP_PORT is properly handled
func TestSFTPPortConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		secretData map[string][]byte
		expectPort bool
	}{
		{
			name: "SFTP with custom port",
			secretData: map[string][]byte{
				"SFTP_HOST":     []byte("sftp.example.com"),
				"SFTP_USERNAME": []byte("user"),
				"SFTP_PASSWORD": []byte("pass"),
				"SFTP_PORT":     []byte("2222"),
			},
			expectPort: true,
		},
		{
			name: "SFTP without custom port (uses default 22)",
			secretData: map[string][]byte{
				"SFTP_HOST":     []byte("sftp.example.com"),
				"SFTP_USERNAME": []byte("user"),
				"SFTP_PASSWORD": []byte("pass"),
			},
			expectPort: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := &volsyncv1alpha1.ReplicationSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-source",
					Namespace: "test-namespace",
				},
			}

			mover := &Mover{
				logger:   logr.Discard(),
				owner:    owner,
				username: "test-user",
				hostname: "test-host",
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: tt.secretData,
			}

			envVars := mover.buildEnvironmentVariables(secret)

			foundPort := false
			for _, env := range envVars {
				if env.Name == "SFTP_PORT" {
					foundPort = true
					if tt.expectPort {
						// Verify it references the secret
						if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
							t.Error("SFTP_PORT should be from secret")
						}
					}
					break
				}
			}

			// SFTP_PORT env var is always present, but only has a value if it exists in the secret
			if !foundPort {
				t.Error("SFTP_PORT environment variable should always be present")
			}
		})
	}
}

// TestCredentialPrecedence tests that credentials are handled with correct precedence
func TestCredentialPrecedence(t *testing.T) {
	// Test that when both SSH key and password are provided,
	// both are made available (entry.sh will handle precedence)
	secretData := map[string][]byte{
		"SFTP_HOST":     []byte("sftp.example.com"),
		"SFTP_USERNAME": []byte("user"),
		"SFTP_PASSWORD": []byte("password"),
		"SFTP_KEY_FILE": []byte("ssh-key-content"),
		"SFTP_PATH":     []byte("/backup"),
	}

	owner := &volsyncv1alpha1.ReplicationSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-source",
			Namespace: "test-namespace",
		},
	}

	mover := &Mover{
		logger:   logr.Discard(),
		owner:    owner,
		username: "test-user",
		hostname: "test-host",
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-secret",
		},
		Data: secretData,
	}

	// Build pod spec
	podSpec := &corev1.PodSpec{
		Containers: []corev1.Container{{
			Name:  "kopia",
			Image: "test-image",
			Env:   []corev1.EnvVar{},
		}},
		Volumes: []corev1.Volume{},
	}

	// Configure credentials
	mover.configureCredentials(podSpec, secret)

	// Build environment variables
	envVars := mover.buildEnvironmentVariables(secret)
	podSpec.Containers[0].Env = envVars

	// Check both password and key are available
	foundPassword := false
	foundKeyEnv := false
	for _, env := range envVars {
		if env.Name == "SFTP_PASSWORD" {
			foundPassword = true
		}
		if env.Name == "SFTP_KEY_FILE" {
			foundKeyEnv = true
		}
	}

	if !foundPassword {
		t.Error("SFTP_PASSWORD should be available even when SSH key is present")
	}

	if !foundKeyEnv {
		t.Error("SFTP_KEY_FILE environment variable should be set")
	}

	// Check that SSH key is mounted
	foundKeyMount := false
	for _, mount := range podSpec.Containers[0].VolumeMounts {
		if mount.MountPath == "/credentials" {
			foundKeyMount = true
			break
		}
	}

	if !foundKeyMount {
		t.Error("SSH key should be mounted at /credentials")
	}
}

// TestBackendEnvironmentVariablesComplete tests that all required backend variables are included
func TestBackendEnvironmentVariablesComplete(t *testing.T) {
	owner := &volsyncv1alpha1.ReplicationSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-source",
			Namespace: "test-namespace",
		},
	}

	mover := &Mover{
		logger:   logr.Discard(),
		owner:    owner,
		username: "test-user",
		hostname: "test-host",
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-secret",
		},
		Data: map[string][]byte{
			"KOPIA_PASSWORD": []byte("test-password"),
		},
	}

	envVars := mover.buildEnvironmentVariables(secret)

	// List of all expected SFTP-related variables
	expectedSFTPVars := []string{
		"SFTP_HOST",
		"SFTP_USERNAME",
		"SFTP_KEY_FILE",
		"SFTP_PORT",
		"SFTP_PASSWORD",
		"SFTP_PATH",
		"SFTP_KNOWN_HOSTS",
		"SFTP_KNOWN_HOSTS_DATA",
	}

	// Check that all SFTP variables are present in the environment
	for _, varName := range expectedSFTPVars {
		found := false
		for _, env := range envVars {
			if env.Name == varName {
				found = true
				// Verify they reference the secret appropriately
				if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
					t.Errorf("%s should reference secret", varName)
				}
				if env.ValueFrom.SecretKeyRef.Name != secret.Name {
					t.Errorf("%s should reference secret %s", varName, secret.Name)
				}
				if env.ValueFrom.SecretKeyRef.Key != varName {
					t.Errorf("%s should reference correct key", varName)
				}
				// All SFTP variables should be optional
				if !*env.ValueFrom.SecretKeyRef.Optional {
					t.Errorf("%s should be optional", varName)
				}
				break
			}
		}
		if !found {
			t.Errorf("Expected SFTP environment variable %s not found", varName)
		}
	}
}

// TestManualConfigWithAdditionalArgs tests manual configuration with additional args
func TestManualConfigWithAdditionalArgs(t *testing.T) {
	tests := []struct {
		name           string
		secretData     map[string][]byte
		additionalArgs []string
	}{
		{
			name: "Manual config with additional args",
			secretData: map[string][]byte{
				"KOPIA_CONFIG_PATH": []byte("/config/kopia"),
				"KOPIA_PASSWORD":    []byte("test-password"),
			},
			additionalArgs: []string{"--one-file-system", "--parallel=8"},
		},
		{
			name: "JSON config with additional args",
			secretData: map[string][]byte{
				"KOPIA_CONFIG_JSON": []byte(`{"repository": {"s3": {"bucket": "test"}}}`),
				"KOPIA_PASSWORD":    []byte("test-password"),
			},
			additionalArgs: []string{"--compression=zstd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := &volsyncv1alpha1.ReplicationSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-source",
					Namespace: "test-namespace",
				},
			}

			mover := &Mover{
				logger:         logr.Discard(),
				owner:          owner,
				username:       "test-user",
				hostname:       "test-host",
				additionalArgs: tt.additionalArgs,
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: tt.secretData,
			}

			envVars := mover.buildEnvironmentVariables(secret)

			// Verify KOPIA_ADDITIONAL_ARGS is set
			foundAdditionalArgs := false
			for _, env := range envVars {
				if env.Name == "KOPIA_ADDITIONAL_ARGS" {
					foundAdditionalArgs = true
					if len(tt.additionalArgs) > 0 && env.Value == "" {
						t.Error("KOPIA_ADDITIONAL_ARGS should not be empty")
					}
					break
				}
			}

			if len(tt.additionalArgs) > 0 && !foundAdditionalArgs {
				t.Error("Expected KOPIA_ADDITIONAL_ARGS for manual config")
			}
		})
	}
}