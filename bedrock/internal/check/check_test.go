package check

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/impulse/app"
)

const (
	fixtures = "../derive/testdata"
	golden   = "../render/testdata/harbor"
)

// harborModel derives the harbor fixture under the test placement.
func harborModel(t *testing.T) *derive.Model {
	t.Helper()

	p, err := derive.ReadPlacement(filepath.Join(fixtures, "placement.json"))
	if err != nil {
		t.Fatalf("derive.ReadPlacement() error = %v", err)
	}
	a, err := app.Discover(filepath.Join(fixtures, "harbor"))
	if err != nil {
		t.Fatalf("app.Discover() error = %v", err)
	}
	m, err := derive.Derive(a, p)
	if err != nil {
		t.Fatalf("derive.Derive() error = %v", err)
	}

	return m
}

// copyGolden copies the committed stack into a fresh directory the test can edit.
func copyGolden(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "stack")
	if err := os.CopyFS(dir, os.DirFS(golden)); err != nil {
		t.Fatalf("os.CopyFS() error = %v", err)
	}

	return dir
}

func TestRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		mutate       func(t *testing.T, dir string)
		wantClean    bool
		wantFindings []Finding
		wantUnseeded []string
		wantRefused  []Authoritative
		wantOutput   []string
	}{
		{
			name:       "the committed stack matches",
			mutate:     func(*testing.T, string) {},
			wantClean:  true,
			wantOutput: []string{"19 owned file(s) match the code"},
		},
		{
			name: "an edited pipeline file at the application root differs",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.WriteFile(filepath.Join(dir, "root", "cloudbuild.yaml"), []byte("# edited\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantFindings: []Finding{{Path: "cloudbuild.yaml", Root: true, Line: 1, Want: "# harbor's deploy pipeline: the Cloud Build steps that take one commit into one environment,", Got: "# edited"}},
			wantOutput:   []string{"1 of 19 owned file(s) differ from the code", "differs  cloudbuild.yaml:1 (at the application root)"},
		},
		{
			name: "an edited owned file differs at its first changed line",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				path := filepath.Join(dir, "locals.tf")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.Replace(data, []byte(`app = "harbor"`), []byte(`app = "haven"`), 1)
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantFindings: []Finding{{Path: "locals.tf", Line: 5, Want: `  app = "harbor"`, Got: `  app = "haven"`}},
			wantOutput:   []string{"1 of 19 owned file(s) differ", "differs  locals.tf:5", "code:        app = \"harbor\"", "committed:   app = \"haven\""},
		},
		{
			name: "a missing owned file",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.Remove(filepath.Join(dir, "spanner.tf")); err != nil {
					t.Fatal(err)
				}
			},
			wantFindings: []Finding{{Path: "spanner.tf", Missing: true}},
			wantOutput:   []string{"missing  spanner.tf"},
		},
		{
			name: "a missing seeded file is not drift",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.Remove(filepath.Join(dir, "terraform.tfvars")); err != nil {
					t.Fatal(err)
				}
			},
			wantClean:    true,
			wantUnseeded: []string{"terraform.tfvars"},
			wantOutput:   []string{"unseeded terraform.tfvars"},
		},
		{
			name: "an authoritative IAM resource anywhere in the stack is refused",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				custom := "# a person's file\nresource \"google_project_iam_binding\" \"owners\" {\n  project = \"p\"\n  role    = \"roles/owner\"\n  members = []\n}\n"
				if err := os.WriteFile(filepath.Join(dir, "custom.tf"), []byte(custom), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantRefused: []Authoritative{{Path: "custom.tf", Line: 2, Address: "google_project_iam_binding.owners"}},
			wantOutput:  []string{"19 owned file(s) match the code", "refused  custom.tf:2 google_project_iam_binding.owners"},
		},
		{
			name: "an edited seeded file is a person's",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.WriteFile(filepath.Join(dir, "terraform.tfvars"), []byte("state_bucket = \"mine\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantClean: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := copyGolden(t)
			tt.mutate(t, dir)
			report, err := Run(harborModel(t), dir, filepath.Join(dir, "root"))
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if report.Clean() != tt.wantClean {
				t.Errorf("Clean() = %v, want %v (findings %+v)", report.Clean(), tt.wantClean, report.Findings)
			}
			if len(report.Findings) != len(tt.wantFindings) {
				t.Fatalf("Findings = %+v, want %+v", report.Findings, tt.wantFindings)
			}
			for i, want := range tt.wantFindings {
				if report.Findings[i] != want {
					t.Errorf("Findings[%d] = %+v, want %+v", i, report.Findings[i], want)
				}
			}
			if strings.Join(report.Unseeded, ",") != strings.Join(tt.wantUnseeded, ",") {
				t.Errorf("Unseeded = %v, want %v", report.Unseeded, tt.wantUnseeded)
			}
			if len(report.Authoritative) != len(tt.wantRefused) {
				t.Fatalf("Authoritative = %+v, want %+v", report.Authoritative, tt.wantRefused)
			}
			for i, want := range tt.wantRefused {
				if report.Authoritative[i] != want {
					t.Errorf("Authoritative[%d] = %+v, want %+v", i, report.Authoritative[i], want)
				}
			}
			var out bytes.Buffer
			report.Write(&out)
			for _, want := range tt.wantOutput {
				if !strings.Contains(out.String(), want) {
					t.Errorf("Write() output lacks %q:\n%s", want, out.String())
				}
			}
		})
	}
}

func TestFirstDifference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		want     string
		got      string
		wantLine int
		wantW    string
		wantG    string
		wantSame bool
	}{
		{name: "same", want: "a\nb\n", got: "a\nb\n", wantSame: true},
		{name: "second line", want: "a\nb\n", got: "a\nc\n", wantLine: 2, wantW: "b", wantG: "c"},
		{name: "the committed text is longer", want: "a\n", got: "a\nb\n", wantLine: 2, wantW: "", wantG: "b"},
		{name: "the rendered text is longer", want: "a\nb\n", got: "a\n", wantLine: 2, wantW: "b", wantG: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			line, w, g, same := firstDifference([]byte(tt.want), []byte(tt.got))
			if line != tt.wantLine || w != tt.wantW || g != tt.wantG || same != tt.wantSame {
				t.Errorf("firstDifference() = %d, %q, %q, %v; want %d, %q, %q, %v", line, w, g, same, tt.wantLine, tt.wantW, tt.wantG, tt.wantSame)
			}
		})
	}
}
