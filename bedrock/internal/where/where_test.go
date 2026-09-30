package where

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The fixture repositories: flat holds the layers at its root, nested under
// infrastructure/, and apprepo is an application repository whose infrastructure/ is the
// stack itself, the application named by the module at its root. Each is copied into a
// temporary directory and given its .git there, a directory for flat and apprepo and a
// file (as a worktree carries) for nested: a committed .git would make the fixture a
// repository of its own.
const (
	flat    = "flat"
	nested  = "nested"
	apprepo = "apprepo"
	// harborApp is the application fixture the derive package reads.
	harborApp = "../derive/testdata/harbor"
	// harborPlacement is the placement it is derived under.
	harborPlacement = "../derive/testdata/placement.json"
)

// copyRepo copies the fixture repository into a temporary directory and marks it a
// repository.
func copyRepo(t *testing.T, fixture string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), fixture)
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("testdata", fixture))); err != nil {
		t.Fatalf("os.CopyFS() error = %v", err)
	}
	if fixture == nested {
		if err := os.WriteFile(filepath.Join(dir, gitDir), []byte("gitdir: ../elsewhere/.git\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	} else if err := os.Mkdir(filepath.Join(dir, gitDir), 0o700); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestRepoRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		start   func(dir string) string
		wantErr string
	}{
		{name: "the root itself", fixture: flat, start: func(dir string) string {
			return dir
		}},
		{name: "a directory deep inside", fixture: flat, start: func(dir string) string {
			return filepath.Join(dir, "3-app", "quill")
		}},
		{name: "a .git file marks a worktree", fixture: nested, start: func(dir string) string {
			return filepath.Join(dir, "infrastructure", "3-app")
		}},
		{name: "no .git anywhere above", fixture: "", start: func(dir string) string {
			return dir
		}, wantErr: "no repository above"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if tt.fixture != "" {
				dir = copyRepo(t, tt.fixture)
			}
			got, err := RepoRoot(tt.start(dir))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("RepoRoot() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("RepoRoot() error = %v", err)
			}
			if got != dir {
				t.Errorf("RepoRoot() = %q, want %q", got, dir)
			}
		})
	}
}

func TestInfrastructureRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		// mutate reshapes the copied repository before it is looked at.
		mutate  func(t *testing.T, dir string)
		want    string
		wantErr string
	}{
		{name: "the layers at the root", fixture: flat, want: "."},
		{name: "the layers under infrastructure", fixture: nested, want: "infrastructure"},
		{name: "the application's stack under infrastructure", fixture: apprepo, want: "infrastructure"},
		{
			name:    "the placement alone marks the stack before the first render",
			fixture: apprepo,
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.Rename(filepath.Join(dir, infrastructureDir, tfvarsFile), filepath.Join(dir, infrastructureDir, placementFile)); err != nil {
					t.Fatal(err)
				}
			},
			want: "infrastructure",
		},
		{
			name:    "a stack beside a layer is neither layout",
			fixture: apprepo,
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.Mkdir(filepath.Join(dir, infrastructureDir, appLayers), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "is not the application's stack (a terraform.tfvars or a placement.json, and no layers): pass --dir",
		},
		{name: "no layers in either", fixture: "", wantErr: "holds the layers (1-org and 3-app), and"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if tt.fixture != "" {
				dir = copyRepo(t, tt.fixture)
			}
			if tt.mutate != nil {
				tt.mutate(t, dir)
			}
			got, err := InfrastructureRoot(dir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("InfrastructureRoot() error = %v, wantErr %q", err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), filepath.Join(dir, infrastructureDir)) {
					t.Errorf("InfrastructureRoot() error = %v, want both candidates named", err)
				}

				return
			}
			if err != nil {
				t.Fatalf("InfrastructureRoot() error = %v", err)
			}
			if want := filepath.Join(dir, tt.want); got != want {
				t.Errorf("InfrastructureRoot() = %q, want %q", got, want)
			}
		})
	}
}

func TestApplicationDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		repoRoot  string
		infraRoot string
		want      string
	}{
		{name: "the infrastructure directory says the code is at the root", repoRoot: "/repo", infraRoot: "/repo/infrastructure", want: "/repo"},
		{name: "a relative infrastructure directory is read the same", repoRoot: "/repo", infraRoot: "/repo/./infrastructure/", want: "/repo"},
		{name: "the layers at the root say nothing", repoRoot: "/repo", infraRoot: "/repo"},
		{name: "an infrastructure root elsewhere says nothing", repoRoot: "/repo", infraRoot: "/other/infrastructure"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ApplicationDir(tt.repoRoot, tt.infraRoot); got != tt.want {
				t.Errorf("ApplicationDir(%q, %q) = %q, want %q", tt.repoRoot, tt.infraRoot, got, tt.want)
			}
		})
	}
}

func TestApplications(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		infra   string
		// goMod replaces the module file at the repository root; "" leaves the fixture's,
		// and "-" removes it.
		goMod      string
		want       []string
		wantSingle string
		// wantLayer is the single application's layer, relative to the infrastructure
		// root.
		wantLayer  string
		wantErr    string
		wantOneErr string
	}{
		{name: "one application, a directory without a placement skipped", fixture: flat, want: []string{"quill"}, wantSingle: "quill", wantLayer: filepath.Join(appLayers, "quill")},
		{name: "two applications", fixture: nested, infra: "infrastructure", want: []string{"harbor", "quill"}, wantOneErr: "several application layers under"},
		{name: "no 3-app", fixture: nested, wantErr: "no 3-app under"},
		{name: "the application's stack itself, named by the module", fixture: apprepo, infra: "infrastructure", want: []string{"quill"}, wantSingle: "quill", wantLayer: "."},
		{name: "a module whose last segment is not an application code", fixture: apprepo, infra: "infrastructure", goMod: "module example.com/acme/quill-app\n\ngo 1.26\n", wantErr: `application code "quill-app" (the module path's last segment) is not one to six lowercase letters`},
		{name: "a module file without a module directive", fixture: apprepo, infra: "infrastructure", goMod: "go 1.26\n", wantErr: "has no module directive to name the application"},
		{name: "no go.mod at the repository root", fixture: apprepo, infra: "infrastructure", goMod: "-", wantErr: "the application layout needs the module at the repository root"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := copyRepo(t, tt.fixture)
			switch tt.goMod {
			case "":
			case "-":
				if err := os.Remove(filepath.Join(repo, goModFile)); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(filepath.Join(repo, goModFile), []byte(tt.goMod), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			dir := filepath.Join(repo, tt.infra)
			got, err := Applications(dir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Applications() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Applications() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Applications() = %q, want %q", got, tt.want)
			}
			single, err := SingleApplication(dir)
			if tt.wantOneErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantOneErr) || !strings.Contains(err.Error(), strings.Join(tt.want, ", ")) {
					t.Fatalf("SingleApplication() error = %v, wantErr %q listing %q", err, tt.wantOneErr, tt.want)
				}

				return
			}
			if err != nil {
				t.Fatalf("SingleApplication() error = %v", err)
			}
			if single != tt.wantSingle {
				t.Errorf("SingleApplication() = %q, want %q", single, tt.wantSingle)
			}
			if got, want := LayerDir(dir, single), filepath.Join(dir, tt.wantLayer); got != want {
				t.Errorf("LayerDir() = %q, want %q", got, want)
			}
		})
	}
}

func TestSingleApplicationNone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, appLayers, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := SingleApplication(dir)
	if err == nil || !strings.Contains(err.Error(), "no application layer under") {
		t.Errorf("SingleApplication() error = %v, want none found", err)
	}
}

func TestEnvironments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		infra   string
		app     string
		want    []string
		wantErr string
	}{
		{name: "the placement's environments in order", fixture: flat, app: "quill", want: []string{"tst", "stg", "prd"}},
		{name: "a placement pinning two", fixture: nested, infra: "infrastructure", app: "harbor", want: []string{"tst", "prd"}},
		{name: "the application's stack itself", fixture: apprepo, infra: "infrastructure", app: "quill", want: []string{"tst", "stg"}},
		{name: "an application that is not there", fixture: flat, app: "none", wantErr: "no placement at"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(copyRepo(t, tt.fixture), tt.infra)
			got, err := Environments(dir, tt.app)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Environments() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Environments() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Environments() = %q, want %q", got, tt.want)
			}
			if fromLayer, err := LayerEnvironments(LayerDir(dir, tt.app)); err != nil || !slices.Equal(fromLayer, tt.want) {
				t.Errorf("LayerEnvironments() = %q, %v, want %q", fromLayer, err, tt.want)
			}
		})
	}
}

// placedLayer is a layer directory holding the harbor placement, for deriving the model.
func placedLayer(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(harborPlacement)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, placementFile), data, 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestVariables(t *testing.T) {
	t.Parallel()

	quillLocals := []string{"APP_COOKIE_KEY", "APP_MAIL_API_KEY"}
	harborSecrets := []string{"APP_COOKIE_KEY", "APP_STAFF_OIDC_CLIENT_SECRET"}
	tests := []struct {
		name     string
		appDir   string
		layerDir func(t *testing.T) string
		want     []string
		wantErr  []string
	}{
		{
			name:   "the code's secrets through the model",
			appDir: harborApp,
			layerDir: func(t *testing.T) string {
				t.Helper()

				return placedLayer(t)
			},
			want: harborSecrets,
		},
		{
			name: "no code known: the layer's locals",
			layerDir: func(t *testing.T) string {
				t.Helper()

				return LayerDir(copyRepo(t, flat), "quill")
			},
			want: quillLocals,
		},
		{
			name:   "an application repository whose code cannot be read falls back to its stack's locals",
			appDir: "testdata/apprepo",
			layerDir: func(t *testing.T) string {
				t.Helper()

				return LayerDir(filepath.Join(copyRepo(t, apprepo), infrastructureDir), "quill")
			},
			want: quillLocals,
		},
		{
			name:   "code that cannot be read falls back to the layer's locals",
			appDir: "testdata/nested",
			layerDir: func(t *testing.T) string {
				t.Helper()

				return LayerDir(copyRepo(t, flat), "quill")
			},
			want: quillLocals,
		},
		{
			name:   "a layer without the placement falls back to its locals",
			appDir: harborApp,
			layerDir: func(t *testing.T) string {
				t.Helper()

				return LayerDir(copyRepo(t, flat), "quill")
			},
			want: quillLocals,
		},
		{
			name:   "neither readable names both",
			appDir: "testdata/nested",
			layerDir: func(t *testing.T) string {
				t.Helper()

				return t.TempDir()
			},
			wantErr: []string{"locals.tf: the application layer's locals list the secret variables", "reading the code at testdata/nested: no go.mod"},
		},
		{
			name: "no code known and no locals",
			layerDir: func(t *testing.T) string {
				t.Helper()

				return t.TempDir()
			},
			wantErr: []string{"locals.tf: the application layer's locals list the secret variables"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Variables(tt.appDir, tt.layerDir(t))
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("Variables() = %q, wantErr %q", got, tt.wantErr)
				}
				for _, want := range tt.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("Variables() error = %v, wantErr %q", err, want)
					}
				}

				return
			}
			if err != nil {
				t.Fatalf("Variables() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Variables() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLocalsSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		want    []string
		wantErr string
	}{
		{name: "the keys in order", src: "locals {\n  secrets = {\n    APP_A_KEY = { name = \"a-key\" }\n    \"APP_B_KEY\" = { name = \"b-key\" }\n  }\n}\n", want: []string{"APP_A_KEY", "APP_B_KEY"}},
		{name: "the secrets local in a second locals block", src: "locals {\n  app = \"quill\"\n}\nlocals {\n  secrets = {}\n}\n", want: []string{}},
		{name: "no secrets local", src: "locals {\n  app = \"quill\"\n}\n", wantErr: "no secrets local in"},
		{name: "a secrets local that is not written out", src: "locals {\n  secrets = merge({}, {})\n}\n", wantErr: "is not a map written out"},
		{name: "a file that will not parse", src: "locals {\n", wantErr: "hclsyntax.ParseConfig()"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file := filepath.Join(t.TempDir(), localsFile)
			if err := os.WriteFile(file, []byte(tt.src), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := localsSecrets(file)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("localsSecrets() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("localsSecrets() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("localsSecrets() = %q, want %q", got, tt.want)
			}
		})
	}
}
