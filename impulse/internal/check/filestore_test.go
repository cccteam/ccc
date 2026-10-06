package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/resource/filestore"
)

// The files the file-store cases build on: a data level reading the variable, a resource
// recording a file, a method taking an upload, and the environment template under test.
const (
	fileStoreConfig = `package config

type dataConfig struct {
	FileStore string ` + "`env:\"APP_FILE_STORE\"`" + `
}
`
	namedStoreConfig = `package config

type dataConfig struct {
	Documents string ` + "`env:\"APP_FILE_STORE_DOCUMENTS\"`" + `
}
`
	photoResource = `package resources

import "github.com/cccteam/ccc/resource"

// Photo is a stored picture.
//
// @resource
type Photo struct {
	ID string ` + "`spanner:\"Id\"`" + `
	// @file
	Key resource.Key[resource.Store] ` + "`spanner:\"Key\"`" + `
}
`
	uploadMethod = `package rpc

// AttachPhoto takes a picture.
//
// @rpc
// @upload(max: 5MB)
type AttachPhoto struct{}
`
)

func TestFileStore(t *testing.T) {
	t.Parallel()

	withProgram := func(files map[string]string) map[string]string {
		files["cmd/generate/main.go"] = program("pkg/resources", `generation.GenerateHandlers("app"),`, `generation.GenerateRoutes("pkg/router", "api"),`, `generation.WithRPC("pkg/rpc"),`)

		return files
	}
	tests := []struct {
		name          string
		files         map[string]string
		fix           bool
		wantStatus    Status
		wantSummary   string
		wantDetails   []string
		wantGitignore string
	}{
		{
			name:        "no store and nothing recording files",
			files:       withProgram(map[string]string{"pkg/resources/doc.go": "package resources\n"}),
			wantStatus:  Skip,
			wantSummary: "no file store variable, and no resource records files",
		},
		{
			name:        "files recorded without a store",
			files:       withProgram(map[string]string{"pkg/resources/photo.go": photoResource, "pkg/rpc/attach.go": uploadMethod}),
			wantStatus:  Fail,
			wantSummary: "2 file annotation(s) without a file store",
			wantDetails: []string{
				"pkg/resources/photo.go:10: declares @file, and no file store is wired (no env tag APP_FILE_STORE): run impulse add files",
				"pkg/rpc/attach.go:6: declares @upload, and no file store is wired (no env tag APP_FILE_STORE): run impulse add files",
			},
		},
		{
			name:        "a directory store, gitignored",
			files:       map[string]string{"pkg/config/data.go": fileStoreConfig, ".envrc.template": "export APP_FILE_STORE=file://uploads\n", ".gitignore": ".envrc\nuploads/\n"},
			wantStatus:  Pass,
			wantSummary: "1 file store variable(s) name stores the framework opens",
		},
		{
			name:        "a bucket store needs no gitignore",
			files:       map[string]string{"pkg/config/data.go": fileStoreConfig, ".envrc.template": "export APP_FILE_STORE=gs://beacon-files\n"},
			wantStatus:  Pass,
			wantSummary: "1 file store variable(s) name stores the framework opens",
		},
		{
			name:        "a named store in memory",
			files:       map[string]string{"pkg/config/data.go": namedStoreConfig, ".envrc.template": "export APP_FILE_STORE_DOCUMENTS=\"mem://\"\n"},
			wantStatus:  Pass,
			wantSummary: "1 file store variable(s) name stores the framework opens",
		},
		{
			name:        "a variable the template lacks is the env-template check's",
			files:       map[string]string{"pkg/config/data.go": fileStoreConfig, ".envrc.template": "export APP_SERVICE_NAME=beacon\n"},
			wantStatus:  Pass,
			wantSummary: "1 file store variable(s) name stores the framework opens",
		},
		{
			name:        "a scheme the framework does not open",
			files:       map[string]string{"pkg/config/data.go": fileStoreConfig, ".envrc.template": "export APP_FILE_STORE=s3://beacon-files\n"},
			wantStatus:  Fail,
			wantSummary: "1 file store problem(s)",
			wantDetails: []string{".envrc.template: APP_FILE_STORE=s3://beacon-files has the scheme s3, which the framework's store (resource/filestore) does not open; a store is file://<dir>, gs://<bucket> or mem://"},
		},
		{
			name:        "no scheme",
			files:       map[string]string{"pkg/config/data.go": fileStoreConfig, ".envrc.template": "export APP_FILE_STORE=uploads\n"},
			wantStatus:  Fail,
			wantSummary: "1 file store problem(s)",
			wantDetails: []string{".envrc.template: APP_FILE_STORE=uploads has no scheme; a store is file://<dir>, gs://<bucket> or mem://"},
		},
		{
			name:        "an empty value",
			files:       map[string]string{"pkg/config/data.go": fileStoreConfig, ".envrc.template": "export APP_FILE_STORE=\n"},
			wantStatus:  Fail,
			wantSummary: "1 file store problem(s)",
			wantDetails: []string{".envrc.template: APP_FILE_STORE names no store; development keeps files in a directory, file://uploads"},
		},
		{
			name:        "a directory store not gitignored",
			files:       map[string]string{"pkg/config/data.go": fileStoreConfig, ".envrc.template": "export APP_FILE_STORE=file://uploads/\n", ".gitignore": ".envrc\n"},
			wantStatus:  Fail,
			wantSummary: "1 file store problem(s)",
			wantDetails: []string{".envrc.template: APP_FILE_STORE names the directory file://uploads/, which .gitignore does not ignore, so the development files would be committed (--fix adds uploads/)"},
		},
		{
			name:          "a directory store not gitignored, fixed",
			files:         map[string]string{"pkg/config/data.go": fileStoreConfig, ".envrc.template": "export APP_FILE_STORE=file://uploads\n"},
			fix:           true,
			wantStatus:    Pass,
			wantSummary:   "1 directory store(s) added to .gitignore",
			wantDetails:   []string{".envrc.template: APP_FILE_STORE names the directory file://uploads, which .gitignore does not ignore, so the development files would be committed (--fix adds uploads/)"},
			wantGitignore: "uploads/\n",
		},
		{
			name:        "a directory outside the tree is not the repository's to ignore",
			files:       map[string]string{"pkg/config/data.go": fileStoreConfig, ".envrc.template": "export APP_FILE_STORE=file:///var/beacon/uploads\n"},
			wantStatus:  Pass,
			wantSummary: "1 file store variable(s) name stores the framework opens",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			files := map[string]string{"go.mod": "module example.com/harbor\n\ngo 1.26.6\n"}
			for rel, content := range tt.files {
				files[rel] = content
			}
			for rel, content := range files {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := fileStore{}.Run(context.Background(), &Env{App: a, Fix: tt.fix})
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v (%s)", got.Status, tt.wantStatus, got.Summary)
			}
			if got.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", got.Summary, tt.wantSummary)
			}
			if diff := cmp.Diff(tt.wantDetails, got.Details); diff != "" {
				t.Errorf("Details mismatch (-want +got):\n%s", diff)
			}
			if tt.wantGitignore != "" {
				data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
				if err != nil {
					t.Fatalf("reading .gitignore: %v", err)
				}
				if string(data) != tt.wantGitignore {
					t.Errorf(".gitignore = %q, want %q", data, tt.wantGitignore)
				}
			}
		})
	}
}

// TestFileStoreSchemes pins the check's schemes to the ones resource/filestore opens.
func TestFileStoreSchemes(t *testing.T) {
	t.Parallel()

	if diff := cmp.Diff([]string{filestore.SchemeDir, filestore.SchemeBucket, filestore.SchemeMem}, fileStoreSchemes); diff != "" {
		t.Errorf("schemes mismatch (-filestore +check):\n%s", diff)
	}
	if dirScheme != filestore.SchemeDir {
		t.Errorf("dirScheme = %q, want %q", dirScheme, filestore.SchemeDir)
	}
}

func TestTemplateValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
		want     string
		wantOK   bool
	}{
		{name: "exported", template: "export APP_FILE_STORE=file://uploads\n", want: "file://uploads", wantOK: true},
		{name: "bare", template: "APP_FILE_STORE=gs://b\n", want: "gs://b", wantOK: true},
		{name: "quoted", template: "export APP_FILE_STORE='mem://'\n", want: "mem://", wantOK: true},
		{name: "commented out", template: "# export APP_FILE_STORE=file://uploads\n", wantOK: false},
		{name: "another variable's prefix", template: "export APP_FILE_STORE_DOCUMENTS=mem://\n", wantOK: false},
		{name: "empty", template: "export APP_FILE_STORE=\n", want: "", wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := templateValue(tt.template, "APP_FILE_STORE")
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("templateValue() = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
