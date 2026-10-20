package layers

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
)

// handRun scripts the fake tofu of one layer for org destroy: what state list prints,
// what state pull prints, and how many destroys fail before one succeeds.
type handRun struct {
	stateList       []string
	statePull       string
	destroyFailures int
}

// handRunner is tofu in the destroy's tests: it records every command by the directory
// it ran in (the copy of 0-bootstrap told apart by its override file), with the GitHub
// token and the data directory the command got, and what the copy held when it was
// initialized.
type handRunner struct {
	mu        sync.Mutex
	runs      map[string]*handRun
	ran       []string
	copyHeld  []string
	copyState string
}

func (r *handRunner) where(c deploy.Command) string {
	base := filepath.Base(c.Dir)
	if _, err := os.Stat(filepath.Join(c.Dir, backendOverrideFile)); err == nil {
		base += "(copy)"
	}

	return base
}

func (r *handRunner) line(c deploy.Command) string {
	line := r.where(c) + ": " + c.String()
	for _, e := range c.Env {
		switch {
		case strings.HasPrefix(e, githubEnv+"="):
			line += " [token]"
		case strings.HasPrefix(e, dataDirEnv+"="):
			line += " [data " + filepath.Base(strings.TrimPrefix(e, dataDirEnv+"=")) + "]"
		}
	}

	return line
}

func (r *handRunner) Run(_ context.Context, c deploy.Command, out io.Writer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ran = append(r.ran, r.line(c))
	run := r.runs[filepath.Base(c.Dir)]
	switch c.Args[0] {
	case "init":
		if strings.HasSuffix(r.where(c), "(copy)") {
			entries, err := os.ReadDir(c.Dir)
			if err != nil {
				return errors.Wrap(err, "os.ReadDir()")
			}
			for _, e := range entries {
				r.copyHeld = append(r.copyHeld, e.Name())
			}
			state, err := os.ReadFile(filepath.Join(c.Dir, stateFile))
			if err != nil {
				return errors.Wrap(err, "os.ReadFile()")
			}
			r.copyState = string(state)
		}
		_, _ = io.WriteString(out, "OpenTofu has been successfully initialized!\n")
	case "destroy":
		if run != nil && run.destroyFailures > 0 {
			run.destroyFailures--
			_, _ = io.WriteString(out, "Error: Error deleting folder: Precondition check failed\n")

			return errors.New("tofu failed: exit status 1")
		}
		_, _ = io.WriteString(out, "Destroy complete! Resources: 3 destroyed.\n")
	case "apply":
		_, _ = io.WriteString(out, "Apply complete! Resources: 0 added, 2 changed, 0 destroyed.\n")
	}

	return nil
}

func (r *handRunner) Output(_ context.Context, c deploy.Command, _ io.Writer) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ran = append(r.ran, r.line(c))
	run := r.runs[filepath.Base(c.Dir)]
	if run == nil {
		return nil, nil
	}
	switch strings.Join(c.Args[:2], " ") {
	case "state list":
		return []byte(strings.Join(run.stateList, "\n") + "\n"), nil
	case "state pull":
		return []byte(run.statePull), nil
	}

	return nil, nil
}

// destroyCheckout is a checkout with the two layers' files the destroy reads and copies:
// the variables, with or without the destroy variable, the backend, the values and the
// lock, and the OpenTofu version at the root.
func destroyCheckout(t *testing.T, destroyVariable bool) string {
	t.Helper()

	dir := t.TempDir()
	variables := "variable \"boot_project_id\" {\n  type = string\n}\n"
	if destroyVariable {
		variables += "\nvariable \"destroy\" {\n  type    = bool\n  default = false\n}\n"
	}
	files := map[string]string{
		tofuVersionFile:                "1.12.6\n",
		"0-bootstrap/" + variablesFile: variables,
		"0-bootstrap/initialize.tf":    "terraform {\n  backend \"gcs\" {\n    bucket = \"imp-boot-gbl-state-a1b2\"\n    prefix = \"0-bootstrap\"\n  }\n}\n",
		"0-bootstrap/terraform.tfvars": "boot_project_id = \"imp-boot-gbl-core-a1b2\"\n",
		"0-bootstrap/" + lockFile:      "# lock\n",
		"0-bootstrap/README.md":        "# not copied\n",
		"1-org/" + variablesFile:       variables,
		"1-org/initialize.tf":          "terraform {\n  backend \"gcs\" {\n    bucket = \"imp-boot-gbl-state-a1b2\"\n    prefix = \"1-org\"\n  }\n}\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// The two layers' states in the tests: 1-org with its repositories, a ruleset, two
// folders, two projects and a lien; 0-bootstrap with its folders, project and an identity.
var (
	orgState = []string{
		"github_repository.infrastructure",
		`github_repository.app["quill"]`,
		`github_repository_ruleset.branch["quill"]`,
		`google_folder.this["shared"]`,
		`google_folder.this["tst"]`,
		`module.project["net"].module.project-factory.google_project.main`,
		`module.project["net"].module.project-factory.module.project_services.google_project_service.project_services["dns.googleapis.com"]`,
		`module.project["tst"].module.project-factory.google_project.main`,
		`module.project["tst"].module.project-factory.google_resource_manager_lien.lien[0]`,
		`module.project["tst"].module.project-factory.module.project_services.google_project_service.project_services["compute.googleapis.com"]`,
	}
	bootState = []string{
		"google_folder.organization",
		"google_folder.terraform",
		"google_service_account.boot_tofu",
		"module.boot_project.module.project-factory.google_project.main",
		`module.boot_project.module.project-factory.module.project_services.google_project_service.project_services["iam.googleapis.com"]`,
	}
	pulledState = `{"version": 4, "resources": []}` + "\n"
)

func fixedToken(context.Context) (string, error) {
	return "ghs_token", nil
}

func noToken(context.Context) (string, error) {
	return "", errors.New("no GitHub token in this test")
}

func TestDestroy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		destroyVariable bool
		keepNet         bool
		token           func(context.Context) (string, error)
		runs            map[string]*handRun
		wantErr         string
		wantRan         []string
		wantOut         []string
		wantCopy        bool
	}{
		{
			name:            "destroys 1-org then 0-bootstrap, the repositories out of the state and the projects and folders unprotected first",
			destroyVariable: true,
			token:           fixedToken,
			runs: map[string]*handRun{
				orgLayer:       {stateList: orgState},
				BootstrapLayer: {stateList: bootState, statePull: pulledState},
			},
			wantRan: []string{
				"1-org: tofu init -input=false -no-color -reconfigure [data 1-org.data]",
				"1-org: tofu state list [data 1-org.data]",
				"1-org: tofu state rm github_repository.infrastructure [token] [data 1-org.data]",
				`1-org: tofu state rm github_repository.app["quill"] [token] [data 1-org.data]`,
				`1-org: tofu apply -input=false -no-color -auto-approve -var destroy=true -target=google_folder.this["shared"] -target=google_folder.this["tst"] -target=module.project["net"].module.project-factory.google_project.main -target=module.project["net"].module.project-factory.module.project_services.google_project_service.project_services["dns.googleapis.com"] -target=module.project["tst"].module.project-factory.google_project.main -target=module.project["tst"].module.project-factory.module.project_services.google_project_service.project_services["compute.googleapis.com"] [token] [data 1-org.data]`,
				"1-org: tofu destroy -input=false -no-color -auto-approve -var destroy=true [token] [data 1-org.data]",
				"0-bootstrap: tofu init -input=false -no-color -reconfigure [data 0-bootstrap.data]",
				"0-bootstrap: tofu state pull [data 0-bootstrap.data]",
				"0-bootstrap(copy): tofu init -input=false -no-color -reconfigure",
				"0-bootstrap(copy): tofu state list",
				`0-bootstrap(copy): tofu apply -input=false -no-color -auto-approve -var destroy=true -target=google_folder.organization -target=google_folder.terraform -target=module.boot_project.module.project-factory.google_project.main -target=module.boot_project.module.project-factory.module.project_services.google_project_service.project_services["iam.googleapis.com"]`,
				"0-bootstrap(copy): tofu destroy -input=false -no-color -auto-approve -var destroy=true",
			},
			wantOut:  []string{"Took github_repository.infrastructure out of 1-org's state: the repository stays.", "Recording on 1-org that the destroy deletes 2 project(s) with 2 service(s) and 2 folder(s), disabling no API first.", "Destroyed 1-org.", "Copied 0-bootstrap's state and files to", "Recording on 0-bootstrap that the destroy deletes 1 project(s) with 1 service(s) and 2 folder(s), disabling no API first.", "Destroyed 0-bootstrap."},
			wantCopy: true,
		},
		{
			name:            "keeps the network project and the organization's folder when the layer registers domains",
			destroyVariable: true,
			keepNet:         true,
			token:           fixedToken,
			runs: map[string]*handRun{
				orgLayer:       {stateList: orgState},
				BootstrapLayer: {stateList: bootState, statePull: pulledState},
			},
			wantRan: []string{
				"1-org: tofu init -input=false -no-color -reconfigure [data 1-org.data]",
				"1-org: tofu state list [data 1-org.data]",
				"1-org: tofu state rm github_repository.infrastructure [token] [data 1-org.data]",
				`1-org: tofu state rm github_repository.app["quill"] [token] [data 1-org.data]`,
				`1-org: tofu apply -input=false -no-color -auto-approve -var destroy=true -target=google_folder.this["shared"] -target=google_folder.this["tst"] -target=module.project["tst"].module.project-factory.google_project.main -target=module.project["tst"].module.project-factory.module.project_services.google_project_service.project_services["compute.googleapis.com"] [token] [data 1-org.data]`,
				`1-org: tofu destroy -input=false -no-color -auto-approve -var destroy=true -exclude=module.project["net"] [token] [data 1-org.data]`,
				"0-bootstrap: tofu init -input=false -no-color -reconfigure [data 0-bootstrap.data]",
				"0-bootstrap: tofu state pull [data 0-bootstrap.data]",
				"0-bootstrap(copy): tofu init -input=false -no-color -reconfigure",
				"0-bootstrap(copy): tofu state list",
				`0-bootstrap(copy): tofu apply -input=false -no-color -auto-approve -var destroy=true -target=google_folder.terraform -target=module.boot_project.module.project-factory.google_project.main -target=module.boot_project.module.project-factory.module.project_services.google_project_service.project_services["iam.googleapis.com"]`,
				"0-bootstrap(copy): tofu destroy -input=false -no-color -auto-approve -var destroy=true -exclude=google_folder.organization",
			},
			wantOut:  []string{"Destroyed 1-org.", "Destroyed 0-bootstrap."},
			wantCopy: true,
		},
		{
			name:            "skips a layer whose state holds nothing, without asking for a token",
			destroyVariable: true,
			token:           noToken,
			runs: map[string]*handRun{
				orgLayer:       {},
				BootstrapLayer: {statePull: pulledState},
			},
			wantRan: []string{
				"1-org: tofu init -input=false -no-color -reconfigure [data 1-org.data]",
				"1-org: tofu state list [data 1-org.data]",
				"0-bootstrap: tofu init -input=false -no-color -reconfigure [data 0-bootstrap.data]",
				"0-bootstrap: tofu state pull [data 0-bootstrap.data]",
				"0-bootstrap(copy): tofu init -input=false -no-color -reconfigure",
				"0-bootstrap(copy): tofu state list",
			},
			wantOut:  []string{"1-org's state holds nothing: nothing to destroy there.", "0-bootstrap's state holds nothing: nothing to destroy there."},
			wantCopy: true,
		},
		{
			name:            "tries a destroy again while it fails",
			destroyVariable: true,
			token:           fixedToken,
			runs: map[string]*handRun{
				orgLayer:       {stateList: orgState[3:], destroyFailures: 2},
				BootstrapLayer: {statePull: pulledState},
			},
			wantRan: []string{
				"1-org: tofu init -input=false -no-color -reconfigure [data 1-org.data]",
				"1-org: tofu state list [data 1-org.data]",
				`1-org: tofu apply -input=false -no-color -auto-approve -var destroy=true -target=google_folder.this["shared"] -target=google_folder.this["tst"] -target=module.project["net"].module.project-factory.google_project.main -target=module.project["net"].module.project-factory.module.project_services.google_project_service.project_services["dns.googleapis.com"] -target=module.project["tst"].module.project-factory.google_project.main -target=module.project["tst"].module.project-factory.module.project_services.google_project_service.project_services["compute.googleapis.com"] [token] [data 1-org.data]`,
				"1-org: tofu destroy -input=false -no-color -auto-approve -var destroy=true [token] [data 1-org.data]",
				"1-org: tofu destroy -input=false -no-color -auto-approve -var destroy=true [token] [data 1-org.data]",
				"1-org: tofu destroy -input=false -no-color -auto-approve -var destroy=true [token] [data 1-org.data]",
				"0-bootstrap: tofu init -input=false -no-color -reconfigure [data 0-bootstrap.data]",
				"0-bootstrap: tofu state pull [data 0-bootstrap.data]",
				"0-bootstrap(copy): tofu init -input=false -no-color -reconfigure",
				"0-bootstrap(copy): tofu state list",
			},
			wantOut:  []string{"The destroy of 1-org failed (attempt 1 of 6)", "The destroy of 1-org failed (attempt 2 of 6)", "Destroyed 1-org."},
			wantCopy: true,
		},
		{
			name:            "gives up after the attempts",
			destroyVariable: true,
			token:           fixedToken,
			runs: map[string]*handRun{
				orgLayer: {stateList: orgState[3:], destroyFailures: destroyAttempts},
			},
			wantErr: "the destroy of 1-org failed 6 times; the log says why",
			wantOut: []string{"The destroy of 1-org failed (attempt 5 of 6)"},
		},
		{
			name:            "refuses a checkout rendered without the destroy variable",
			destroyVariable: false,
			token:           fixedToken,
			runs:            map[string]*handRun{},
			wantErr:         "1-org/variables.tf declares no destroy variable, which bedrock org destroy sets",
		},
		{
			name:            "refuses 1-org without a GitHub token, before anything changes",
			destroyVariable: true,
			token:           noToken,
			runs:            map[string]*handRun{orgLayer: {stateList: orgState}},
			wantErr:         "1-org's GitHub provider needs a token",
			wantRan: []string{
				"1-org: tofu init -input=false -no-color -reconfigure [data 1-org.data]",
				"1-org: tofu state list [data 1-org.data]",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := destroyCheckout(t, tt.destroyVariable)
			runner := &handRunner{runs: tt.runs}
			var out bytes.Buffer
			err := Destroy(context.Background(), runner, &DestroyRequest{Dir: dir, GithubToken: tt.token, KeepNet: tt.keepNet}, &out)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("Destroy() = %v, want no error; printed:\n%s", err, out.String())
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("Destroy() = %v, want an error containing %q", err, tt.wantErr)
			}
			if tt.wantRan != nil && strings.Join(runner.ran, "\n") != strings.Join(tt.wantRan, "\n") {
				t.Errorf("commands ran:\n%s\nwant:\n%s", strings.Join(runner.ran, "\n"), strings.Join(tt.wantRan, "\n"))
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("printed:\n%s\nwant it to contain %q", out.String(), want)
				}
			}
			if tt.wantCopy {
				wantHeld := []string{".opentofu-version", ".terraform.lock.hcl", "backend_override.tf", "initialize.tf", "terraform.tfstate", "terraform.tfvars", "variables.tf"}
				if strings.Join(runner.copyHeld, " ") != strings.Join(wantHeld, " ") {
					t.Errorf("the copy held %v, want %v", runner.copyHeld, wantHeld)
				}
				if runner.copyState != pulledState {
					t.Errorf("the copy's state is %q, want the pulled %q", runner.copyState, pulledState)
				}
			}
		})
	}
}

func TestResourceType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		address string
		want    string
	}{
		{"github_repository.infrastructure", "github_repository"},
		{`github_repository.app["quill"]`, "github_repository"},
		{`github_repository_ruleset.branch["quill"]`, "github_repository_ruleset"},
		{`module.project["net"].module.project-factory.google_project.main`, "google_project"},
		{"module.boot_project.module.project-factory.google_resource_manager_lien.lien[0]", "google_resource_manager_lien"},
		{"google_folder.this[\"tst\"]", "google_folder"},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			t.Parallel()

			if got := resourceType(tt.address); got != tt.want {
				t.Errorf("resourceType(%q) = %q, want %q", tt.address, got, tt.want)
			}
		})
	}
}
