package org

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSecretPlace: each key's secret is named as the root organization's 0-bootstrap
// names it, in the project that holds the keys: a root organization's own boot project,
// a hosting organization's keys.project; a boot project not recorded yet is refused with
// the key to record.
func TestSecretPlace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		secret        string
		placement     Placement
		wantProject   string
		wantContainer string
		wantErr       string
	}{
		{
			name:          "the infrastructure key, in the root organization's boot project",
			secret:        InfrastructureKey,
			placement:     Placement{Kind: RootKind, Prefix: "ex", Projects: map[string]string{"boot": "ex-boot-gbl-core-a1b2"}},
			wantProject:   "ex-boot-gbl-core-a1b2",
			wantContainer: "ex-boot-gbl-github-infrastructure-key",
		},
		{
			name:          "the deployer key",
			secret:        DeployerKey,
			placement:     Placement{Kind: RootKind, Prefix: "ex", Projects: map[string]string{"boot": "ex-boot-gbl-core-a1b2"}},
			wantProject:   "ex-boot-gbl-core-a1b2",
			wantContainer: "ex-boot-gbl-github-deployer-key",
		},
		{
			name:          "the release key",
			secret:        ReleaseKey,
			placement:     Placement{Kind: RootKind, Prefix: "ex", Projects: map[string]string{"boot": "ex-boot-gbl-core-a1b2"}},
			wantProject:   "ex-boot-gbl-core-a1b2",
			wantContainer: "ex-boot-gbl-github-release-key",
		},
		{
			name:          "a hosting organization names the root organization's",
			secret:        DeployerKey,
			placement:     Placement{Prefix: "hs", Projects: map[string]string{"boot": "hs-boot-gbl-core-1111"}, Keys: &KeysPlace{Organization: "ex", Project: "ex-boot-gbl-core-a1b2"}},
			wantProject:   "ex-boot-gbl-core-a1b2",
			wantContainer: "ex-boot-gbl-github-deployer-key",
		},
		{
			name:      "the boot project not recorded yet",
			secret:    InfrastructureKey,
			placement: Placement{Kind: RootKind, Prefix: "ex"},
			wantErr:   "placement.json records no project for boot (projects.boot): record the seed's value there, or pass --project",
		},
		{
			name:      "a hosting organization without keys",
			secret:    DeployerKey,
			placement: Placement{Prefix: "hs", Projects: map[string]string{"boot": "hs-boot-gbl-core-1111"}},
			wantErr:   "placement.json records no project for boot (projects.boot)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := tt.placement
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

// TestSecretVersionNames: every reader names the alias current, in the project that holds
// the keys, whether or not a version is pinned; empty while no project holds them.
func TestSecretVersionNames(t *testing.T) {
	t.Parallel()

	const rootProject = "projects/ex-boot-gbl-core-a1b2/secrets/ex-boot-gbl-github-"
	tests := []struct {
		name      string
		placement Placement
		want      string
		release   string
	}{
		{name: "a root organization, nothing pinned", placement: Placement{Kind: RootKind, Prefix: "ex", Projects: map[string]string{"boot": "ex-boot-gbl-core-a1b2"}}, want: rootProject + "infrastructure-key/versions/current|" + rootProject + "deployer-key/versions/current", release: rootProject + "release-key/versions/current"},
		{name: "a root organization, versions pinned: the alias all the same", placement: Placement{Kind: RootKind, Prefix: "ex", Projects: map[string]string{"boot": "ex-boot-gbl-core-a1b2"}, GithubInfrastructureKeyVersion: "2", GithubDeployerKeyVersion: "1", GithubReleaseKeyVersion: "3"}, want: rootProject + "infrastructure-key/versions/current|" + rootProject + "deployer-key/versions/current", release: rootProject + "release-key/versions/current"},
		{name: "a root organization before its boot project is recorded", placement: Placement{Kind: RootKind, Prefix: "ex", GithubInfrastructureKeyVersion: "2"}, want: "|"},
		{name: "a hosting organization names the root organization's", placement: Placement{Prefix: "hs", Projects: map[string]string{"boot": "hs-boot-gbl-core-1111"}, Keys: &KeysPlace{Organization: "ex", Project: "ex-boot-gbl-core-a1b2"}}, want: rootProject + "infrastructure-key/versions/current|" + rootProject + "deployer-key/versions/current", release: rootProject + "release-key/versions/current"},
		{name: "a hosting organization without keys", placement: Placement{Prefix: "hs", Projects: map[string]string{"boot": "hs-boot-gbl-core-1111"}}, want: "|"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := tt.placement
			if got := p.SecretVersionName(InfrastructureKey) + "|" + p.DeployerKeyVersionName(); got != tt.want {
				t.Errorf("version names = %q, want %q", got, tt.want)
			}
			if got := p.ReleaseKeyVersionName(); got != tt.release {
				t.Errorf("ReleaseKeyVersionName() = %q, want %q", got, tt.release)
			}
		})
	}
}

// TestKeyAliasHCL: the alias rendered on a root organization's secret, at the version
// pinned, or an empty map before any pin; and the key operators as an HCL set.
func TestKeyAliasHCL(t *testing.T) {
	t.Parallel()

	p := Placement{Kind: RootKind, Prefix: "ex", GithubDeployerKeyVersion: "7", KeyOperators: []string{"group:keys@ex.example", "user:op@ex.example"}}
	if got := p.KeyAliasHCL(DeployerKey); got != `{ current = "7" }` {
		t.Errorf("KeyAliasHCL(deployer) = %q", got)
	}
	if got := p.KeyAliasHCL(ReleaseKey); got != "{}" {
		t.Errorf("KeyAliasHCL(release) = %q, want {}", got)
	}
	if got := p.KeyOperatorsHCL(); got != `toset(["group:keys@ex.example", "user:op@ex.example"])` {
		t.Errorf("KeyOperatorsHCL() = %q", got)
	}
	if got := (&Placement{Kind: RootKind}).KeyOperatorsHCL(); got != "toset([])" {
		t.Errorf("KeyOperatorsHCL() with none = %q", got)
	}
	if got := (HostingOrganization{Prefix: "hs", BootProject: "hs-boot-gbl-core-1111"}).BootIdentityMember(); got != "serviceAccount:hs-boot-gbl-tofu@hs-boot-gbl-core-1111.iam.gserviceaccount.com" {
		t.Errorf("BootIdentityMember() = %q", got)
	}
}

// TestKeysElsewhere: a hosting organization is sent to the root organization that holds
// the keys; a root organization is not.
func TestKeysElsewhere(t *testing.T) {
	t.Parallel()

	hosting := Placement{Prefix: "hs", Keys: &KeysPlace{Organization: "ex", Project: "ex-boot-gbl-core-a1b2"}}
	err := hosting.KeysElsewhere()
	want := "the GitHub Apps' keys live in the root organization ex (keys.organization in placement.json), in its boot project ex-boot-gbl-core-a1b2: add, check and pin a key in that organization's infrastructure repository; this organization reads each secret's alias current, which that organization's placement pins"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("KeysElsewhere() = %v, want %q", err, want)
	}
	if err := (&Placement{Kind: RootKind, Prefix: "ex"}).KeysElsewhere(); err != nil {
		t.Errorf("a root organization's KeysElsewhere() = %v, want nil", err)
	}
}

// TestKeysValidate: a hosting organization's placement names the root organization that
// holds the keys and records nothing of that organization's own.
func TestKeysValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(p *Placement)
		wantErr string
	}{
		{name: "the fixture", mutate: func(*Placement) {}},
		{name: "no keys", mutate: func(p *Placement) { p.Keys = nil }, wantErr: "keys is empty: a hosting organization reads the GitHub Apps' keys from the root organization that holds them; record its prefix (keys.organization) and its boot project (keys.project)"},
		{name: "a project alone", mutate: func(p *Placement) { p.Keys.Organization = "" }, wantErr: "keys is empty"},
		{name: "a prefix that is not one", mutate: func(p *Placement) { p.Keys.Organization = "Zones" }, wantErr: `keys.organization "Zones" is not an organization's prefix`},
		{name: "its own prefix", mutate: func(p *Placement) { p.Keys.Organization = "imp" }, wantErr: `keys.organization "imp" is this organization's own prefix: a hosting organization holds no key`},
		{name: "a project that is not one", mutate: func(p *Placement) { p.Keys.Project = "IMZ" }, wantErr: `keys.project "IMZ" is not a project id`},
		{name: "a version pinned", mutate: func(p *Placement) { p.GithubDeployerKeyVersion = "2" }, wantErr: "githubDeployerKeyVersion is set, and a hosting organization pins no key: the root organization imr pins the version in its own placement.json, and every reader here names the alias current; remove it"},
		{name: "key operators", mutate: func(p *Placement) { p.KeyOperators = []string{"group:keys@imp.example"} }, wantErr: "keyOperators and hostingOrganizations are a root organization's, which holds the keys"},
		{name: "hosting organizations", mutate: func(p *Placement) {
			p.HostingOrganizations = []HostingOrganization{{Prefix: "rgb", BootProject: "rgb-boot-gbl-core-0a0b"}}
		}, wantErr: "keyOperators and hostingOrganizations are a root organization's"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			tt.mutate(p)
			err := p.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, wantErr %q", err, tt.wantErr)
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
