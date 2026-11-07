package org

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

// TestAppManifests: the three apps are declared in the bootstrap's order, named after the
// GitHub organization or the prefix chosen for them, each with Metadata read among its
// permissions; the manifest posts as GitHub reads it, with the webhook off and the app
// private.
func TestAppManifests(t *testing.T) {
	t.Parallel()

	p := rootPlacement(t)
	manifests := p.AppManifests()
	var names []string
	for _, m := range manifests {
		names = append(names, m.Name)
		if m.Permissions[PermissionMetadata] != LevelRead {
			t.Errorf("%s declares no Metadata read, which GitHub gives every app", m.Name)
		}
		if m.Description == "" || !strings.HasPrefix(m.Description, "bedrock's "+string(m.Role)+" app") {
			t.Errorf("%s's description = %q", m.Name, m.Description)
		}
	}
	if want := []string{"imp-example-infrastructure", "imp-example-deployer", "imp-example-release"}; !slices.Equal(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	infra := p.AppManifest(InfrastructureApp)
	wantInfra := map[string]string{
		PermissionActions: LevelRead, PermissionAdministration: LevelWrite, PermissionContents: LevelWrite, PermissionMetadata: LevelRead, PermissionPullRequests: LevelWrite,
		PermissionOrgAdmin: LevelWrite, PermissionOrgVariables: LevelWrite, PermissionOrgRunners: LevelWrite,
	}
	if diff := cmp.Diff(wantInfra, infra.Permissions); diff != "" {
		t.Errorf("the infrastructure app's permissions mismatch (-want +got):\n%s", diff)
	}
	if got := p.AppManifest(DeployerApp).Permissions; got[PermissionContents] != "" || got[PermissionChecks] != LevelWrite || got[PermissionDeployments] != LevelWrite {
		t.Errorf("the deployer app's permissions = %v", got)
	}
	if got := p.AppManifest(ReleaseApp).Permissions; got[PermissionContents] != LevelWrite || got[PermissionAdministration] != "" {
		t.Errorf("the release app's permissions = %v", got)
	}

	gh := infra.GitHub("https://github.com/imp-example/imr-infrastructure", "http://127.0.0.1:4000/made", "http://127.0.0.1:4000/installed")
	if gh.Name != "imp-example-infrastructure" || gh.Public || gh.HookAttributes.Active || len(gh.DefaultEvents) != 0 || gh.RedirectURL != "http://127.0.0.1:4000/made" || gh.SetupURL != "http://127.0.0.1:4000/installed" || gh.URL != "https://github.com/imp-example/imr-infrastructure" {
		t.Errorf("GitHub() = %+v", gh)
	}
	if diff := cmp.Diff(wantInfra, gh.DefaultPermissions); diff != "" {
		t.Errorf("the manifest's permissions mismatch (-want +got):\n%s", diff)
	}

	prefixed := *p
	prefixed.GithubAppPrefix = "imr"
	if got := prefixed.AppName(ReleaseApp); got != "imr-release" {
		t.Errorf("AppName() with a prefix = %q", got)
	}
	for _, role := range AppRoles {
		if got, want := role.Key(), "github-"+string(role)+"-key"; got != want || !slices.Contains(Secrets, got) {
			t.Errorf("%s.Key() = %q, want %q among the secrets", role, got, want)
		}
	}
}

// TestAppNamesValidate: a prefix that makes a name longer than GitHub's limit, or that is
// not a login's shape, is refused; the organization's login stands in when none is set.
func TestAppNamesValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(p *Placement)
		wantErr string
	}{
		{name: "the organization's login, short enough", mutate: func(*Placement) {}},
		{name: "a prefix chosen", mutate: func(p *Placement) { p.GithubAppPrefix = "imr" }},
		{
			name:    "a login too long for the names",
			mutate:  func(p *Placement) { p.GithubOrganization = "an-organization-with-a-long-login" },
			wantErr: "the GitHub App name an-organization-with-a-long-login-infrastructure is 48 characters, and GitHub allows 34: set githubAppPrefix to a shorter word than an-organization-with-a-long-login for the three apps' names",
		},
		{
			name:    "a prefix that is not a word GitHub takes",
			mutate:  func(p *Placement) { p.GithubAppPrefix = "imr apps" },
			wantErr: `githubAppPrefix "imr apps" is not a word the GitHub Apps' names can start with (letters, digits and hyphens)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := rootPlacement(t)
			tt.mutate(p)
			err := p.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// TestAppManifestVerify: an app as declared has no finding; another owner, a permission
// lacking, held too low or held beyond the declaration, no installation on the
// organization and an installation on a selection of repositories are each one.
func TestAppManifestVerify(t *testing.T) {
	t.Parallel()

	p := rootPlacement(t)
	m := p.AppManifest(DeployerApp)
	declared := func() *github.App {
		return &github.App{ID: 5080800, Name: "imp-example-deployer", Owner: github.Account{Login: "imp-example", Type: "Organization"}, Permissions: map[string]string{
			PermissionChecks: LevelWrite, PermissionDeployments: LevelWrite, PermissionIssues: LevelWrite, PermissionMetadata: LevelRead, PermissionPullRequests: LevelWrite,
		}}
	}
	installed := []github.Installation{{ID: 1, AppID: 5080800, Account: github.Account{Login: "imp-example", Type: "Organization"}, RepositorySelection: github.AllRepositories}}
	tests := []struct {
		name          string
		app           func() *github.App
		installations []github.Installation
		want          []string
	}{
		{name: "as declared", app: declared, installations: installed},
		{
			name: "owned by another organization",
			app: func() *github.App {
				a := declared()
				a.Owner.Login = "someone-else"

				return a
			},
			installations: installed,
			want:          []string{"the deployer app (App 5080800, imp-example-deployer) is owned by someone-else, not the imp-example organization"},
		},
		{
			name: "a permission lacking, one too low and one beyond the declaration",
			app: func() *github.App {
				a := declared()
				delete(a.Permissions, PermissionDeployments)
				a.Permissions[PermissionIssues] = LevelRead
				a.Permissions[PermissionOrgAdmin] = LevelWrite

				return a
			},
			installations: installed,
			want: []string{
				"the deployer app lacks the Deployments permission (write): add it on the app's settings page (Permissions & events), then accept the new permission on the organization's installation of the app (the organization's settings, GitHub Apps)",
				"the deployer app holds the Issues permission as read, not write: raise it on the app's settings page (Permissions & events), then accept the new permission on the organization's installation of the app (the organization's settings, GitHub Apps)",
				"the deployer app holds the Administration (organization) permission (write), which it does not need: take it off on the app's settings page",
			},
		},
		{
			name:          "not installed on the organization",
			app:           declared,
			installations: []github.Installation{{ID: 2, Account: github.Account{Login: "another-org"}, RepositorySelection: github.AllRepositories}},
			want:          []string{"the deployer app is not installed on the imp-example organization: install it there, for all repositories"},
		},
		{
			name:          "installed on a selection",
			app:           declared,
			installations: []github.Installation{{ID: 3, Account: github.Account{Login: "imp-example"}, RepositorySelection: "selected"}},
			want:          []string{"the deployer app is installed on a selection of the imp-example organization's repositories, not all of them: a repository made later would be outside it"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := m.Verify(tt.app(), tt.installations, p.GithubOrganization)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Verify() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
