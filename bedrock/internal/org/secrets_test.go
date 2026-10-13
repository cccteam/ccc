package org

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSecretPlace: each organization secret's container is named as 0-bootstrap names
// it, in the boot project the placement records; a boot project not recorded yet is
// refused with the key to record.
func TestSecretPlace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		secret        string
		projects      map[string]string
		wantProject   string
		wantContainer string
		wantErr       string
	}{
		{
			name:          "the infrastructure key, in the boot project",
			secret:        InfrastructureKey,
			projects:      map[string]string{"boot": "ex-boot-gbl-core-a1b2"},
			wantProject:   "ex-boot-gbl-core-a1b2",
			wantContainer: "ex-boot-gbl-github-infrastructure-key",
		},
		{
			name:          "the deployer key, in the boot project too",
			secret:        DeployerKey,
			projects:      map[string]string{"boot": "ex-boot-gbl-core-a1b2", "stg": "ex-stg-gbl-core-3c4d"},
			wantProject:   "ex-boot-gbl-core-a1b2",
			wantContainer: "ex-boot-gbl-github-deployer-key",
		},
		{
			name:    "the boot project not recorded yet",
			secret:  InfrastructureKey,
			wantErr: "placement.json records no project for boot (projects.boot): record the seed's value there, or pass --project",
		},
		{
			name:     "the boot project not recorded yet, for the deployer key",
			secret:   DeployerKey,
			projects: map[string]string{"prd": "ex-prd-gbl-core-5e6f"},
			wantErr:  "placement.json records no project for boot (projects.boot)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &Placement{Prefix: "ex", Projects: tt.projects}
			project, err := p.SecretProject()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("SecretProject() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("SecretProject() error = %v", err)
			}
			if project != tt.wantProject {
				t.Errorf("SecretProject() = %q, want %q", project, tt.wantProject)
			}
			if got := p.SecretContainer(tt.secret); got != tt.wantContainer {
				t.Errorf("SecretContainer() = %q, want %q", got, tt.wantContainer)
			}
		})
	}
}

// TestSecretVersionNames: the pinned versions' resource names, in the boot project,
// empty until pinned or while the boot project is not recorded.
func TestSecretVersionNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		placement Placement
		want      string
	}{
		{name: "nothing pinned", placement: Placement{Prefix: "ex", Projects: map[string]string{"boot": "ex-boot-gbl-core-a1b2"}}, want: "|"},
		{name: "the boot project not recorded", placement: Placement{Prefix: "ex", GithubInfrastructureKeyVersion: "2", GithubDeployerKeyVersion: "1"}, want: "|"},
		{
			name:      "both pinned",
			placement: Placement{Prefix: "ex", Projects: map[string]string{"boot": "ex-boot-gbl-core-a1b2"}, GithubInfrastructureKeyVersion: "2", GithubDeployerKeyVersion: "1"},
			want:      "projects/ex-boot-gbl-core-a1b2/secrets/ex-boot-gbl-github-infrastructure-key/versions/2|projects/ex-boot-gbl-core-a1b2/secrets/ex-boot-gbl-github-deployer-key/versions/1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := tt.placement
			if got := p.SecretVersionName(InfrastructureKey) + "|" + p.DeployerKeyVersionName(); got != tt.want {
				t.Errorf("version names = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestIsOrganizationPlacement: a placement naming the organization is one; an
// application's placement, a file that is not JSON and a file that is absent are not.
func TestIsOrganizationPlacement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		absent  bool
		want    bool
	}{
		{name: "the organization's placement", content: `{"prefix": "ex", "organizationId": "123456789012"}`, want: true},
		{name: "an application's placement", content: `{"prefix": "ex", "repository": "quill"}`},
		{name: "a file that is not JSON", content: "prefix = ex\n"},
		{name: "no file", absent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file := filepath.Join(t.TempDir(), "placement.json")
			if !tt.absent {
				if err := os.WriteFile(file, []byte(tt.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := IsOrganizationPlacement(file)
			if err != nil {
				t.Fatalf("IsOrganizationPlacement() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("IsOrganizationPlacement() = %t, want %t", got, tt.want)
			}
		})
	}
}
