package cli

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// keyReader is the deploy clients' Secret Manager in these tests: payloads by version
// name, nothing else.
type keyReader struct {
	payloads map[string]string
}

func (r *keyReader) open(context.Context) (deploy.Secrets, error) {
	return r, nil
}

func (r *keyReader) Access(_ context.Context, version string) ([]byte, error) {
	payload, ok := r.payloads[version]
	if !ok {
		return nil, errors.Newf("not found: %s", version)
	}

	return []byte(payload), nil
}

func (*keyReader) State(context.Context, string) (string, error) {
	return "ENABLED", nil
}

func (*keyReader) Container(context.Context, string) (bool, error) {
	return true, nil
}

func (*keyReader) Create(context.Context, string, string, map[string]string) error {
	return nil
}

func (*keyReader) Newest(context.Context, string) (string, error) {
	return "", nil
}

func (*keyReader) Grant(context.Context, string, string, string) error {
	return nil
}

func (*keyReader) Add(context.Context, string, []byte) (string, error) {
	return "", nil
}

func (*keyReader) Close() error {
	return nil
}

// appServer stands in for GitHub's /app: it answers for the app whose id the JWT's
// issuer names when that is appID, and refuses every other issuer as GitHub refuses a
// key that is not the app's.
func appServer(t *testing.T, appID, slug string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app" {
			http.Error(w, "not found", http.StatusNotFound)

			return
		}
		parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
		if len(parts) != 3 {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)

			return
		}
		claims, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			http.Error(w, `{"message":"A JSON web token could not be decoded"}`, http.StatusUnauthorized)

			return
		}
		var issued map[string]any
		if json.Unmarshal(claims, &issued) != nil || issued["iss"] != appID {
			http.Error(w, `{"message":"A JSON web token could not be decoded"}`, http.StatusUnauthorized)

			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 5080800, "slug": slug, "name": "Deployer"})
	}))
	t.Cleanup(srv.Close)

	return srv
}

// testPEM is an RSA key in PEM, the shape GitHub hands out.
func testPEM(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// TestOrgSecretsCheck: in the root organization's repository, org secrets check reads the
// version, signs as the app placement.json records and prints GitHub's answer with the
// pin to make; a version that is not the app's key is refused with GitHub's answer; a
// disabled version and a hosting organization are refused before anything is read.
func TestOrgSecretsCheck(t *testing.T) {
	t.Parallel()

	const deployerVersion2 = "projects/" + keysProject + "/secrets/" + deployerKey + "/versions/2"
	pemKey := testPEM(t)
	tests := []struct {
		name    string
		hosting bool
		args    []string
		edit    func(p *org.Placement)
		wantErr string
		wantOut []string
	}{
		{
			name: "a version the app's key signs as, not yet pinned",
			args: []string{"github-deployer-key", "2"},
			wantOut: []string{
				"Version 2 of imr-boot-gbl-github-deployer-key in project imr-boot-gbl-core-c3d4 is enabled, and GitHub accepts a token signed with it as the deployer GitHub App's private key: App 5080800 (imp-deployer).\n",
				"The alias current is pinned to version 1 (githubDeployerKeyVersion in placement.json); nothing reads version 2 yet.\n",
				"Next: bedrock secret pin github-deployer-key 2, then the pull request with the render; its apply moves the alias, and every reader in every organization switches. Delete the old key on GitHub once the next run has gone through clean.\n",
			},
		},
		{
			name: "the version pinned already",
			args: []string{"github-deployer-key", "2"},
			edit: func(p *org.Placement) { p.GithubDeployerKeyVersion = "2" },
			wantOut: []string{
				"The alias current is pinned to version 2 already (githubDeployerKeyVersion in placement.json); every reader names it.\n",
			},
		},
		{
			name:    "a key that is not the app's: GitHub refuses",
			args:    []string{"github-deployer-key", "2"},
			edit:    func(p *org.Placement) { p.GithubDeployerAppID = "999" },
			wantErr: "GitHub refuses a token signed with version 2 of imr-boot-gbl-github-deployer-key as App 999 (githubDeployerAppId): the version is not that app's key, or githubDeployerAppId records another app's id; nothing changes",
		},
		{
			name:    "a disabled version is refused before GitHub is asked",
			args:    []string{"github-infrastructure-key", "3"},
			wantErr: "version 3 of imr-boot-gbl-github-infrastructure-key in project imr-boot-gbl-core-c3d4 is DISABLED, not ENABLED",
		},
		{
			name:    "the app not recorded is refused",
			args:    []string{"github-release-key", "1"},
			edit:    func(p *org.Placement) { p.GithubReleaseAppID = "" },
			wantErr: "placement.json records no githubReleaseAppId: record the app's App ID there, from its settings page, since the check signs as the app",
		},
		{
			name:    "latest is refused",
			args:    []string{"github-deployer-key", "latest"},
			wantErr: `version "latest": a key's version is checked by its number, never latest`,
		},
		{
			name:    "a hosting organization is sent to the root organization",
			hosting: true,
			args:    []string{"github-deployer-key", "2"},
			wantErr: "the GitHub Apps' keys live in the root organization imr (keys.organization in placement.json), in its boot project imr-boot-gbl-core-c3d4: add, check and pin a key in that organization's infrastructure repository",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var dir string
			if tt.hosting {
				dir = orgRepo(t)
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
			} else {
				dir = orgSecretRepo(t)
			}
			if tt.edit != nil {
				editPlacement(t, dir, tt.edit)
			}
			secrets := orgSecrets()
			srv := appServer(t, "5080800", "imp-deployer")
			d := deps{domains: noCloudDomains, secrets: secrets.open, projects: noProjects, interactive: never, deploy: &deploy.Clients{
				Secrets: (&keyReader{payloads: map[string]string{deployerVersion2: pemKey}}).open,
				GitHub:  func(token string) *github.Client { return github.New(srv.URL, token) },
			}}
			out, err := execute(d, "", append([]string{"org", "secrets", "check"}, append(tt.args, "--dir", dir)...)...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out)
				}

				return
			}
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

// TestOrgSecretInHosting: a hosting organization's repository refuses secret add and pin,
// naming the root organization that holds the keys, and nothing is looked up.
func TestOrgSecretInHosting(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"add", "github-deployer-key"}, {"pin", "github-infrastructure-key", "2"}} {
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			if code, out := runOrg(t, "org", "new", dir); code != 0 {
				t.Fatalf("org new: %d %s", code, out)
			}
			d := deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, interactive: never}
			out, err := execute(d, "pem", append([]string{secretCommand}, append(args, "--dir", dir)...)...)
			want := "the GitHub Apps' keys live in the root organization imr (keys.organization in placement.json), in its boot project imr-boot-gbl-core-c3d4: add, check and pin a key in that organization's infrastructure repository; this organization reads each secret's alias current, which that organization's placement pins"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Execute() error = %v, want %q; output:\n%s", err, want, out)
			}
		})
	}
}
