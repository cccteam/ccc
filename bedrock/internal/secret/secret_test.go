package secret

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
)

// repo is the fixture infrastructure repository: the application layer of quill, whose
// placement pins the cookie key at version 2 in tst and nothing elsewhere.
const repo = "testdata/repo"

// app is the fixture application.
const app = "quill"

// Resource names of the versions the fake is asked about.
const (
	cookieKey3      = "projects/lab-tst-1/secrets/imp-tst-gbl-quill-cookie-key/versions/3"
	cookieKeyLatest = "projects/lab-tst-1/secrets/imp-tst-gbl-quill-cookie-key/versions/latest"
	cookieKey4      = "projects/lab-tst-1/secrets/imp-tst-gbl-quill-cookie-key/versions/4"
	mailKey1        = "projects/lab-stg-1/secrets/imp-stg-gbl-quill-mail-api-key/versions/1"
	customKey3      = "projects/lab-tst-1/secrets/custom-cookie/versions/3"
)

func TestValidateApp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		app     string
		wantErr bool
	}{
		{name: "a word", app: "quill"},
		{name: "one character", app: "q"},
		{name: "six characters", app: "quills"},
		{name: "digits and an inner hyphen", app: "q1-2"},
		{name: "seven characters", app: "quilled", wantErr: true},
		{name: "uppercase", app: "Quill", wantErr: true},
		{name: "a leading hyphen", app: "-quill", wantErr: true},
		{name: "a trailing hyphen", app: "quill-", wantErr: true},
		{name: "an underscore", app: "qu_ll", wantErr: true},
		{name: "empty", app: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateApp(tt.app)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateApp(%q) error = %v, wantErr %v", tt.app, err, tt.wantErr)
			}
		})
	}
}

func TestValidateEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     string
		wantErr bool
	}{
		{name: "tst", env: "tst"},
		{name: "stg", env: "stg"},
		{name: "prd", env: "prd"},
		{name: "another word", env: "dev", wantErr: true},
		{name: "uppercase", env: "TST", wantErr: true},
		{name: "empty", env: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateEnv(tt.env)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEnv(%q) error = %v, wantErr %v", tt.env, err, tt.wantErr)
			}
		})
	}
}

func TestValidateVariable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		variable string
		wantErr  bool
	}{
		{name: "two words", variable: "APP_COOKIE_KEY"},
		{name: "one word", variable: "APP_KEY"},
		{name: "a digit", variable: "APP_S3_BUCKET"},
		{name: "no APP_ prefix", variable: "COOKIE_KEY", wantErr: true},
		{name: "the prefix alone", variable: "APP_", wantErr: true},
		{name: "lowercase", variable: "app_cookie_key", wantErr: true},
		{name: "a double underscore", variable: "APP_COOKIE__KEY", wantErr: true},
		{name: "a trailing underscore", variable: "APP_COOKIE_KEY_", wantErr: true},
		{name: "a hyphen", variable: "APP_COOKIE-KEY", wantErr: true},
		{name: "empty", variable: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateVariable(tt.variable)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateVariable(%q) error = %v, wantErr %v", tt.variable, err, tt.wantErr)
			}
		})
	}
}

func TestValidateVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{name: "one", version: "1"},
		{name: "many digits", version: "120"},
		{name: "latest", version: "latest"},
		{name: "zero", version: "0", wantErr: true},
		{name: "a leading zero", version: "03", wantErr: true},
		{name: "negative", version: "-1", wantErr: true},
		{name: "a word", version: "newest", wantErr: true},
		{name: "uppercase latest", version: "LATEST", wantErr: true},
		{name: "empty", version: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateVersion(tt.version)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateVersion(%q) error = %v, wantErr %v", tt.version, err, tt.wantErr)
			}
		})
	}
}

// fake answers about the versions it holds, keyed by resource name, and records how it
// was used. Pinning never lists anything, so the two list calls answer nothing.
type fake struct {
	versions map[string]Version
	openErr  error
	project  string
	names    []string
}

// open is the fake's ClientFunc.
func (f *fake) open(_ context.Context, project string) (Client, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	f.project = project

	return f, nil
}

func (f *fake) GetSecretVersion(_ context.Context, name string) (*Version, error) {
	f.names = append(f.names, name)
	v, ok := f.versions[name]
	if !ok {
		return nil, errors.Newf("rpc error: code = NotFound desc = Secret Version [%s] not found", name)
	}

	return &v, nil
}

func (f *fake) ListSecrets(context.Context, string, string) ([]string, error) {
	return nil, errors.New("pinning lists no secrets")
}

func (f *fake) ListSecretVersions(context.Context, string, string) ([]Version, error) {
	return nil, errors.New("pinning lists no versions")
}

// enabled holds each named version, enabled.
func enabled(names ...string) map[string]Version {
	versions := make(map[string]Version, len(names))
	for _, name := range names {
		versions[name] = Version{Name: name, State: Enabled}
	}

	return versions
}

// copyRepo copies the fixture repository into a directory the test can edit.
func copyRepo(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "infrastructure")
	if err := os.CopyFS(dir, os.DirFS(repo)); err != nil {
		t.Fatalf("os.CopyFS() error = %v", err)
	}

	return dir
}

// layerFile is the application layer's placement in the copied repository.
func layerFile(dir string) string {
	return filepath.Join(dir, DefaultLayer(app), tfvarsFile)
}

// editLayer rewrites the application layer's placement through edit.
func editLayer(t *testing.T, dir string, edit func([]byte) []byte) {
	t.Helper()

	data, err := os.ReadFile(layerFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layerFile(dir), edit(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPin(t *testing.T) {
	t.Parallel()

	cookie3 := Request{App: app, Env: "tst", Variable: "APP_COOKIE_KEY", Version: "3"}
	verified3 := Request{App: app, Env: "tst", Variable: "APP_COOKIE_KEY", Version: "3", Project: "lab-tst-1", Container: "imp-tst-gbl-quill-cookie-key"}
	tests := []struct {
		name          string
		req           Request
		versions      map[string]Version
		openErr       error
		mutate        func(t *testing.T, dir string)
		wantNames     []string
		wantErr       string
		wantUnchanged bool
		wantVerified  bool
		wantContainer string
		wantResolved  string
		wantFile      []string
	}{
		{
			name:          "a version Secret Manager reports enabled is pinned",
			req:           verified3,
			versions:      enabled(cookieKey3),
			wantNames:     []string{cookieKey3},
			wantVerified:  true,
			wantContainer: "imp-tst-gbl-quill-cookie-key",
			wantResolved:  "3",
			wantFile:      []string{`APP_COOKIE_KEY = "3"`, "stg = {}", "prd = {}", "# Committed on purpose"},
		},
		{
			name:      "a disabled version is refused before editing",
			req:       verified3,
			versions:  map[string]Version{cookieKey3: {Name: cookieKey3, State: "DISABLED"}},
			wantNames: []string{cookieKey3},
			wantErr:   "version 3 of imp-tst-gbl-quill-cookie-key in project lab-tst-1 is DISABLED, not ENABLED",
		},
		{
			name:      "a version Secret Manager does not have is refused before editing",
			req:       verified3,
			versions:  enabled(),
			wantNames: []string{cookieKey3},
			wantErr:   "Secret Version [" + cookieKey3 + "] not found",
		},
		{
			name:     "without a project the version is pinned unverified",
			req:      cookie3,
			versions: enabled(cookieKey3),
			wantFile: []string{`APP_COOKIE_KEY = "3"`},
		},
		{
			name:          "another variable is pinned in another environment",
			req:           Request{App: app, Env: "stg", Variable: "APP_MAIL_API_KEY", Version: "1", Project: "lab-stg-1", Container: "imp-stg-gbl-quill-mail-api-key"},
			versions:      enabled(mailKey1),
			wantNames:     []string{mailKey1},
			wantVerified:  true,
			wantContainer: "imp-stg-gbl-quill-mail-api-key",
			wantResolved:  "1",
			wantFile:      []string{`APP_MAIL_API_KEY = "1"`, `APP_COOKIE_KEY = "2"`},
		},
		{
			name:          "the container asked for is the one verified",
			req:           Request{App: app, Env: "tst", Variable: "APP_COOKIE_KEY", Version: "3", Project: "lab-tst-1", Container: "custom-cookie"},
			versions:      enabled(customKey3),
			wantNames:     []string{customKey3},
			wantVerified:  true,
			wantContainer: "custom-cookie",
			wantResolved:  "3",
			wantFile:      []string{`APP_COOKIE_KEY = "3"`},
		},
		{
			name:    "a project without the container's name is refused",
			req:     Request{App: app, Env: "tst", Variable: "APP_COOKIE_KEY", Version: "3", Project: "lab-tst-1"},
			wantErr: "verifying through project lab-tst-1 needs the secret container's name: pass --container <name>",
		},
		{
			name:          "latest is looked up as it is and reports the version it resolves to",
			req:           Request{App: app, Env: "tst", Variable: "APP_COOKIE_KEY", Version: "latest", Project: "lab-tst-1", Container: "imp-tst-gbl-quill-cookie-key"},
			versions:      map[string]Version{cookieKeyLatest: {Name: cookieKey4, State: Enabled}},
			wantNames:     []string{cookieKeyLatest},
			wantVerified:  true,
			wantContainer: "imp-tst-gbl-quill-cookie-key",
			wantResolved:  "4",
			wantFile:      []string{`APP_COOKIE_KEY = "latest"`},
		},
		{
			name:          "a variable already pinned to the version leaves the file as it is, unasked",
			req:           Request{App: app, Env: "tst", Variable: "APP_COOKIE_KEY", Version: "2", Project: "lab-tst-1", Container: "imp-tst-gbl-quill-cookie-key"},
			versions:      enabled(),
			wantUnchanged: true,
		},
		{
			name:    "a client that cannot be opened is refused before editing",
			req:     verified3,
			openErr: errors.New("no credentials"),
			wantErr: "no credentials",
		},
		{
			name:    "a missing placement is refused",
			req:     Request{App: app, Env: "tst", Variable: "APP_COOKIE_KEY", Version: "3", Layer: "9-none"},
			wantErr: "no placement at",
		},
		{
			name: "an environment the placement lacks is refused",
			req:  Request{App: app, Env: "prd", Variable: "APP_COOKIE_KEY", Version: "3"},
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				editLayer(t, dir, func(data []byte) []byte {
					return bytes.Replace(data, []byte("  prd = {}\n"), nil, 1)
				})
			},
			wantErr: "has no prd map: its keys are tst, stg",
		},
		{
			name: "a placement without the map is refused",
			req:  cookie3,
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				editLayer(t, dir, func([]byte) []byte {
					return []byte("state_bucket = \"lab-boot-state-1234\"\n")
				})
			},
			wantErr: "no secret_versions in",
		},
		{
			name:    "an app of the wrong shape is refused",
			req:     Request{App: "Quill", Env: "tst", Variable: "APP_COOKIE_KEY", Version: "3"},
			wantErr: `app "Quill": lowercase letters, digits and inner hyphens, 1 to 6 characters`,
		},
		{
			name:    "an environment of the wrong shape is refused",
			req:     Request{App: app, Env: "dev", Variable: "APP_COOKIE_KEY", Version: "3"},
			wantErr: `environment "dev": one of tst, stg, prd`,
		},
		{
			name:    "a variable of the wrong shape is refused",
			req:     Request{App: app, Env: "tst", Variable: "cookie_key", Version: "3"},
			wantErr: `variable "cookie_key": an environment variable in upper snake case under the APP_ prefix`,
		},
		{
			name:    "a version of the wrong shape is refused",
			req:     Request{App: app, Env: "tst", Variable: "APP_COOKIE_KEY", Version: "0"},
			wantErr: `version "0": a positive integer or the word latest`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := copyRepo(t)
			if tt.mutate != nil {
				tt.mutate(t, dir)
			}
			before, _ := os.ReadFile(layerFile(dir))
			f := &fake{versions: tt.versions, openErr: tt.openErr}
			req := tt.req
			req.Dir = dir
			r, err := Pin(context.Background(), f.open, &req)
			if !slices.Equal(f.names, tt.wantNames) {
				t.Errorf("Secret Manager was asked about %q, want %q", f.names, tt.wantNames)
			}
			after, _ := os.ReadFile(layerFile(dir))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Pin() error = %v, wantErr %q", err, tt.wantErr)
				}
				if !bytes.Equal(before, after) {
					t.Errorf("Pin() edited the placement on a refusal:\n%s", after)
				}

				return
			}
			if err != nil {
				t.Fatalf("Pin() error = %v", err)
			}
			if r.File != layerFile(dir) || r.App != req.App || r.Env != req.Env || r.Variable != req.Variable || r.Version != req.Version {
				t.Errorf("Pin() = %+v, want the request's pin in %s", r, layerFile(dir))
			}
			if r.Unchanged != tt.wantUnchanged || r.Verified != tt.wantVerified || r.Container != tt.wantContainer || r.Resolved != tt.wantResolved {
				t.Errorf("Pin() unchanged %v, verified %v, container %q, resolved %q; want %v, %v, %q, %q", r.Unchanged, r.Verified, r.Container, r.Resolved, tt.wantUnchanged, tt.wantVerified, tt.wantContainer, tt.wantResolved)
			}
			if tt.wantVerified && (r.Project != req.Project || f.project != req.Project) {
				t.Errorf("Pin() verified in project %q (client opened for %q), want %q", r.Project, f.project, req.Project)
			}
			if tt.wantUnchanged {
				if !bytes.Equal(before, after) {
					t.Errorf("Pin() edited a placement that already held the pin:\n%s", after)
				}

				return
			}
			for _, want := range tt.wantFile {
				if !strings.Contains(string(after), want) {
					t.Errorf("the placement lacks %q:\n%s", want, after)
				}
			}
		})
	}
}

func TestResultWrite(t *testing.T) {
	t.Parallel()

	file := filepath.Join("3-app", "quill", "terraform.tfvars")
	tests := []struct {
		name    string
		r       Result
		want    []string
		wantNot []string
	}{
		{
			name: "a verified pin",
			r:    Result{App: app, Env: "tst", Variable: "APP_COOKIE_KEY", Version: "3", File: file, Verified: true, Project: "lab-tst-1", Container: "imp-tst-gbl-quill-cookie-key", Resolved: "3"},
			want: []string{
				"Pinned APP_COOKIE_KEY to version 3 for quill in tst: secret_versions.tst in 3-app/quill/terraform.tfvars.",
				"Secret Manager confirms version 3 of imp-tst-gbl-quill-cookie-key in project lab-tst-1 is enabled.",
				"Next: commit the change to the values file and open the pull request; the plan for tst shows the change to the service's configuration (Cloud Run's revision template) and nothing in the other environments. After the merge, the run of that commit in tst applies it, builds the release, deploys it and moves traffic.",
			},
			wantNot: []string{"not verified", "today"},
		},
		{
			name: "an unverified pin",
			r:    Result{App: app, Env: "stg", Variable: "APP_MAIL_API_KEY", Version: "1", File: file},
			want: []string{
				"Pinned APP_MAIL_API_KEY to version 1 for quill in stg: secret_versions.stg in 3-app/quill/terraform.tfvars.",
				"The version was not verified: without --project, Secret Manager is not asked whether it exists and is enabled.",
				"the plan for stg shows the change to the service's configuration (Cloud Run's revision template) and nothing in the other environments. After the merge, the run of that commit in stg",
			},
			wantNot: []string{"confirms"},
		},
		{
			name: "latest names the version it resolved to",
			r:    Result{App: app, Env: "tst", Variable: "APP_COOKIE_KEY", Version: "latest", File: file, Verified: true, Project: "lab-tst-1", Container: "imp-tst-gbl-quill-cookie-key", Resolved: "4"},
			want: []string{
				"Pinned APP_COOKIE_KEY to latest for quill in tst",
				"Secret Manager confirms latest of imp-tst-gbl-quill-cookie-key in project lab-tst-1 is enabled (latest is version 4 today).",
			},
		},
		{
			name:    "a pin already in place",
			r:       Result{App: app, Env: "tst", Variable: "APP_COOKIE_KEY", Version: "2", File: file, Unchanged: true},
			want:    []string{"APP_COOKIE_KEY is already pinned to version 2 for quill in tst (secret_versions.tst in 3-app/quill/terraform.tfvars); nothing to change."},
			wantNot: []string{"Next:", "verified"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			tt.r.Write(&out)
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("Write() output lacks %q:\n%s", want, out.String())
				}
			}
			for _, not := range tt.wantNot {
				if strings.Contains(out.String(), not) {
					t.Errorf("Write() output has %q:\n%s", not, out.String())
				}
			}
		})
	}
}
