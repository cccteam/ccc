package org

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConnectionUnset: the two connection values are read from 2-env's placement as a
// person writes them; a value commented out, absent or null is unset, and a placement
// that is missing or does not parse is refused.
func TestConnectionUnset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// tfvars is 2-env/terraform.tfvars; "" writes none.
		tfvars    string
		wantUnset string
		wantErr   string
	}{
		{
			name:   "both set",
			tfvars: "boot_project_id = \"imp-boot-gbl-core-a1b2\"\ngithub_app_installation_id        = 12345678\ngithub_oauth_token_secret_version = \"projects/imp-tst-gbl-core-1a2b/locations/us-central1/secrets/github-github-oauthtoken-abcdef/versions/1\"\n",
		},
		{
			name:      "the seeded placement leaves both commented out",
			tfvars:    "boot_project_id = \"imp-boot-gbl-core-a1b2\"\n# github_app_installation_id        = <installation id>\n# github_oauth_token_secret_version = \"projects/<tst project>/secrets/<name>/versions/1\"\n",
			wantUnset: "github_app_installation_id,github_oauth_token_secret_version",
		},
		{
			name:      "one set and one absent names the absent one",
			tfvars:    "github_app_installation_id = 12345678\n",
			wantUnset: "github_oauth_token_secret_version",
		},
		{
			name:      "null is unset",
			tfvars:    "github_app_installation_id        = null\ngithub_oauth_token_secret_version = \"projects/p/secrets/s/versions/1\"\n",
			wantUnset: "github_app_installation_id",
		},
		{
			name:    "a placement that does not parse is refused",
			tfvars:  "github_app_installation_id = \n",
			wantErr: "hclsyntax.ParseConfig(): 2-env/terraform.tfvars",
		},
		{
			name:    "a missing placement is refused by name",
			wantErr: "os.ReadFile(): 2-env/terraform.tfvars",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if tt.tfvars != "" {
				if err := os.MkdirAll(filepath.Join(dir, "2-env"), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "2-env", "terraform.tfvars"), []byte(tt.tfvars), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			unset, err := ConnectionUnset(dir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ConnectionUnset() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("ConnectionUnset() error = %v", err)
			}
			if got := strings.Join(unset, ","); got != tt.wantUnset {
				t.Errorf("unset = %q, want %q", got, tt.wantUnset)
			}
		})
	}
}
