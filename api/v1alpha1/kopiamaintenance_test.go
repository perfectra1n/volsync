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

package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestKopiaMaintenance_IsDirectRepositoryMode(t *testing.T) {
	tests := []struct {
		name string
		spec KopiaMaintenanceSpec
		want bool
	}{
		{
			name: "direct repository mode",
			spec: KopiaMaintenanceSpec{
				Repository: &KopiaRepositorySpec{
					Repository: "test-secret",
					Namespace:  "test-ns",
				},
			},
			want: true,
		},
		{
			name: "repository selector mode",
			spec: KopiaMaintenanceSpec{
				RepositorySelector: &KopiaRepositorySelector{
					Repository: "test-*",
				},
			},
			want: false,
		},
		{
			name: "neither mode specified",
			spec: KopiaMaintenanceSpec{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			km := &KopiaMaintenance{
				Spec: tt.spec,
			}
			if got := km.IsDirectRepositoryMode(); got != tt.want {
				t.Errorf("KopiaMaintenance.IsDirectRepositoryMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKopiaMaintenance_GetRepositorySecret(t *testing.T) {
	tests := []struct {
		name           string
		spec           KopiaMaintenanceSpec
		wantSecretName string
		wantNamespace  string
	}{
		{
			name: "direct repository with namespace",
			spec: KopiaMaintenanceSpec{
				Repository: &KopiaRepositorySpec{
					Repository: "test-secret",
					Namespace:  "test-ns",
				},
			},
			wantSecretName: "test-secret",
			wantNamespace:  "test-ns",
		},
		{
			name: "direct repository without namespace",
			spec: KopiaMaintenanceSpec{
				Repository: &KopiaRepositorySpec{
					Repository: "test-secret",
				},
			},
			wantSecretName: "test-secret",
			wantNamespace:  "",
		},
		{
			name: "repository selector mode",
			spec: KopiaMaintenanceSpec{
				RepositorySelector: &KopiaRepositorySelector{
					Repository: "test-*",
				},
			},
			wantSecretName: "",
			wantNamespace:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			km := &KopiaMaintenance{
				Spec: tt.spec,
			}
			gotSecretName, gotNamespace := km.GetRepositorySecret()
			if gotSecretName != tt.wantSecretName {
				t.Errorf("KopiaMaintenance.GetRepositorySecret() secretName = %v, want %v", gotSecretName, tt.wantSecretName)
			}
			if gotNamespace != tt.wantNamespace {
				t.Errorf("KopiaMaintenance.GetRepositorySecret() namespace = %v, want %v", gotNamespace, tt.wantNamespace)
			}
		})
	}
}

func TestKopiaMaintenance_Validate(t *testing.T) {
	tests := []struct {
		name    string
		spec    KopiaMaintenanceSpec
		wantErr bool
	}{
		{
			name: "valid direct repository",
			spec: KopiaMaintenanceSpec{
				Repository: &KopiaRepositorySpec{
					Repository: "test-secret",
					Namespace:  "test-ns",
				},
			},
			wantErr: false,
		},
		{
			name: "valid repository selector",
			spec: KopiaMaintenanceSpec{
				RepositorySelector: &KopiaRepositorySelector{
					Repository: "test-*",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid - both specified",
			spec: KopiaMaintenanceSpec{
				Repository: &KopiaRepositorySpec{
					Repository: "test-secret",
				},
				RepositorySelector: &KopiaRepositorySelector{
					Repository: "test-*",
				},
			},
			wantErr: true,
		},
		{
			name:    "invalid - neither specified",
			spec:    KopiaMaintenanceSpec{},
			wantErr: true,
		},
		{
			name: "invalid - empty repository name",
			spec: KopiaMaintenanceSpec{
				Repository: &KopiaRepositorySpec{
					Repository: "",
					Namespace:  "test-ns",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			km := &KopiaMaintenance{
				Spec: tt.spec,
			}
			err := km.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("KopiaMaintenance.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestKopiaMaintenance_Matches_DirectRepositoryMode(t *testing.T) {
	// Test that direct repository mode never matches ReplicationSources
	km := &KopiaMaintenance{
		Spec: KopiaMaintenanceSpec{
			Repository: &KopiaRepositorySpec{
				Repository: "test-secret",
				Namespace:  "test-ns",
			},
		},
	}

	rs := &ReplicationSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-source",
			Namespace: "test-ns",
		},
		Spec: ReplicationSourceSpec{
			Kopia: &ReplicationSourceKopiaSpec{
				Repository: "test-secret",
			},
		},
	}

	// Direct repository mode should never match ReplicationSources
	if km.Matches(rs) {
		t.Error("KopiaMaintenance in direct repository mode should not match ReplicationSources")
	}
}

func TestKopiaMaintenance_Matches_RepositorySelectorMode(t *testing.T) {
	tests := []struct {
		name     string
		selector KopiaRepositorySelector
		source   ReplicationSource
		want     bool
	}{
		{
			name: "exact repository match",
			selector: KopiaRepositorySelector{
				Repository: "test-repo",
			},
			source: ReplicationSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-source",
					Namespace: "test-ns",
				},
				Spec: ReplicationSourceSpec{
					Kopia: &ReplicationSourceKopiaSpec{
						Repository: "test-repo",
					},
				},
			},
			want: true,
		},
		{
			name: "wildcard repository match",
			selector: KopiaRepositorySelector{
				Repository: "test-*",
			},
			source: ReplicationSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-source",
					Namespace: "test-ns",
				},
				Spec: ReplicationSourceSpec{
					Kopia: &ReplicationSourceKopiaSpec{
						Repository: "test-repo",
					},
				},
			},
			want: true,
		},
		{
			name: "repository mismatch",
			selector: KopiaRepositorySelector{
				Repository: "other-repo",
			},
			source: ReplicationSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-source",
					Namespace: "test-ns",
				},
				Spec: ReplicationSourceSpec{
					Kopia: &ReplicationSourceKopiaSpec{
						Repository: "test-repo",
					},
				},
			},
			want: false,
		},
		{
			name: "label match",
			selector: KopiaRepositorySelector{
				Repository: "test-repo",
				Labels: map[string]string{
					"environment": "production",
				},
			},
			source: ReplicationSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-source",
					Namespace: "test-ns",
					Labels: map[string]string{
						"environment": "production",
						"team":        "backend",
					},
				},
				Spec: ReplicationSourceSpec{
					Kopia: &ReplicationSourceKopiaSpec{
						Repository: "test-repo",
					},
				},
			},
			want: true,
		},
		{
			name: "label mismatch",
			selector: KopiaRepositorySelector{
				Repository: "test-repo",
				Labels: map[string]string{
					"environment": "production",
				},
			},
			source: ReplicationSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-source",
					Namespace: "test-ns",
					Labels: map[string]string{
						"environment": "development",
					},
				},
				Spec: ReplicationSourceSpec{
					Kopia: &ReplicationSourceKopiaSpec{
						Repository: "test-repo",
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			km := &KopiaMaintenance{
				Spec: KopiaMaintenanceSpec{
					RepositorySelector: &tt.selector,
				},
			}

			if got := km.Matches(&tt.source); got != tt.want {
				t.Errorf("KopiaMaintenance.Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKopiaMaintenance_GetEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled *bool
		want    bool
	}{
		{
			name:    "default enabled",
			enabled: nil,
			want:    true,
		},
		{
			name:    "explicitly enabled",
			enabled: ptr.To(true),
			want:    true,
		},
		{
			name:    "explicitly disabled",
			enabled: ptr.To(false),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			km := &KopiaMaintenance{
				Spec: KopiaMaintenanceSpec{
					Enabled: tt.enabled,
				},
			}
			if got := km.GetEnabled(); got != tt.want {
				t.Errorf("KopiaMaintenance.GetEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKopiaMaintenance_GetSchedule(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
		want     string
	}{
		{
			name:     "default schedule",
			schedule: "",
			want:     "0 2 * * *",
		},
		{
			name:     "custom schedule",
			schedule: "0 4 * * 0",
			want:     "0 4 * * 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			km := &KopiaMaintenance{
				Spec: KopiaMaintenanceSpec{
					Schedule: tt.schedule,
				},
			}
			if got := km.GetSchedule(); got != tt.want {
				t.Errorf("KopiaMaintenance.GetSchedule() = %v, want %v", got, tt.want)
			}
		})
	}
}