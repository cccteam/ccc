package github_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// TestConvertManifest: the conversion takes no token and answers the app the code makes,
// with its key; a code is good once, and an unknown one is not found, as GitHub answers.
func TestConvertManifest(t *testing.T) {
	t.Parallel()

	srv := githubtest.New(t)
	made := &github.App{ID: 77, Slug: "acme-deployer", Name: "acme-deployer", Owner: github.Account{Login: "acme", Type: "Organization"}, Permissions: map[string]string{"issues": "write"}, PEM: "the key GitHub made, in PEM", ClientSecret: "the client secret"}
	srv.Conversions["c0de"] = made
	c := github.New(srv.URL, "")
	app, err := c.ConvertManifest(context.Background(), "c0de")
	if err != nil {
		t.Fatalf("ConvertManifest() error = %v", err)
	}
	if diff := cmp.Diff(made, app); diff != "" {
		t.Errorf("ConvertManifest() mismatch (-want +got):\n%s", diff)
	}
	if _, err := c.ConvertManifest(context.Background(), "c0de"); err == nil || !github.NotFound(err) {
		t.Errorf("a spent code: error = %v, want not found", err)
	}
	if want := "POST /app-manifests/c0de/conversions"; srv.Calls[0] != want {
		t.Errorf("Calls[0] = %q, want %q", srv.Calls[0], want)
	}
}

// TestAppInstallations: the app's JWT lists the installations of the app its issuer
// names; a JWT of an app the stand-in does not know is refused.
func TestAppInstallations(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv := githubtest.New(t)
	srv.Apps["4242"] = &githubtest.App{
		Key:           &key.PublicKey,
		Record:        github.App{ID: 4242, Slug: "acme-release", Name: "acme-release", Owner: github.Account{Login: "acme", Type: "Organization"}},
		Installations: []github.Installation{{ID: 9, AppID: 4242, Account: github.Account{Login: "acme", Type: "Organization"}, RepositorySelection: github.AllRepositories}},
	}
	jwt := githubtest.AppJWT(t, "4242", key)
	c := github.New(srv.URL, jwt)
	installations, err := c.AppInstallations(context.Background())
	if err != nil {
		t.Fatalf("AppInstallations() error = %v", err)
	}
	if diff := cmp.Diff(srv.Apps["4242"].Installations, installations); diff != "" {
		t.Errorf("AppInstallations() mismatch (-want +got):\n%s", diff)
	}
	app, err := c.App(context.Background())
	if err != nil {
		t.Fatalf("App() error = %v", err)
	}
	if app.Slug != "acme-release" || app.Owner.Login != "acme" {
		t.Errorf("App() = %+v", app)
	}
	other := github.New(srv.URL, githubtest.AppJWT(t, "4343", key))
	if _, err := other.App(context.Background()); err == nil || !strings.Contains(err.Error(), "is not app") {
		t.Errorf("an unknown app's JWT: error = %v, want refused", err)
	}
}

// TestManifestJSON: the manifest posts as GitHub reads it: its field names, the webhook
// off, no events, private.
func TestManifestJSON(t *testing.T) {
	t.Parallel()

	m := github.Manifest{
		Name:               "acme-deployer",
		URL:                "https://github.com/acme/acme-infrastructure",
		HookAttributes:     github.HookAttributes{URL: "https://github.com/acme/acme-infrastructure", Active: false},
		RedirectURL:        "http://127.0.0.1:4000/made",
		SetupURL:           "http://127.0.0.1:4000/installed",
		Description:        "bedrock's deployer app",
		DefaultEvents:      []string{},
		DefaultPermissions: map[string]string{"issues": "write", "metadata": "read"},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"acme-deployer","url":"https://github.com/acme/acme-infrastructure","hook_attributes":{"url":"https://github.com/acme/acme-infrastructure","active":false},"redirect_url":"http://127.0.0.1:4000/made","setup_url":"http://127.0.0.1:4000/installed","setup_on_update":false,"description":"bedrock's deployer app","public":false,"default_events":[],"default_permissions":{"issues":"write","metadata":"read"}}`
	if string(data) != want {
		t.Errorf("Manifest JSON = %s\nwant %s", data, want)
	}
	if got, want := github.ManifestFormURL("acme", "st ate"), "https://github.com/organizations/acme/settings/apps/new?state=st+ate"; got != want {
		t.Errorf("ManifestFormURL() = %q, want %q", got, want)
	}
}
