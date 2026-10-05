package check

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
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
		name   string
		mutate func(t *testing.T, dir string)
		// code changes the derived model, as a change to the application's code would;
		// nil leaves it as the fixture derives.
		code         func(m *derive.Model)
		wantClean    bool
		wantFindings []Finding
		wantUnseeded []string
		wantRefused  []Authoritative
		// wantReleaseFiles are release-please's files the report refuses as missing.
		wantReleaseFiles []string
		wantOutput       []string
	}{
		{
			name:      "the committed stack matches, its file store's bucket policy admitted, with the maintenance warnings the fixture earns",
			mutate:    func(*testing.T, string) {},
			wantClean: true,
			wantOutput: []string{
				"23 owned file(s) match the code",
				"warning  prd has no maintenance setting (placement.json \"maintenance\": {\"prd\": ...}): a breaking release to prd is refused at the start of its run until one is written; \"anytime\" is a setting, and so are the client's windows",
				"warning  no release file at pkg/router/zz_gen_release.json: no outlet declares an oldest answered release, so no release is breaking and the maintenance window never holds a run; the resource generator writes it beside the generated router (go generate ./...)",
			},
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
			wantOutput:   []string{"1 of 23 owned file(s) differ from the code", "differs  cloudbuild.yaml:1 (at the application root)"},
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
			wantOutput:   []string{"1 of 23 owned file(s) differ", "differs  locals.tf:5", "code:        app = \"harbor\"", "committed:   app = \"haven\""},
		},
		{
			name: "a scheduler job edited in the committed stack differs",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				path := filepath.Join(dir, "scheduler.tf")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.Replace(data, []byte(`schedule  = "0 7 * * 1-5"`), []byte(`schedule  = "0 9 * * *"`), 1)
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantFindings: []Finding{{Path: "scheduler.tf", Line: 29, Want: `      schedule  = "0 7 * * 1-5"`, Got: `      schedule  = "0 9 * * *"`}},
			wantOutput:   []string{"1 of 23 owned file(s) differ", "differs  scheduler.tf:29"},
		},
		{
			name:   "a schedule the code changed differs from the committed job",
			mutate: func(*testing.T, string) {},
			code: func(m *derive.Model) {
				m.Scheduled = []derive.ScheduledRoute{{Path: "/_scheduled/send-daily-digest", Schedule: "0 6 * * 1-5", TimeZone: "America/New_York"}}
			},
			wantFindings: []Finding{
				{Path: "README.md", Line: 235, Want: "  | `POST /_scheduled/send-daily-digest` | `0 6 * * 1-5` | America/New_York |", Got: "  | `POST /_scheduled/send-daily-digest` | `0 7 * * 1-5` | America/New_York |"},
				{Path: "scheduler.tf", Line: 29, Want: `      schedule  = "0 6 * * 1-5"`, Got: `      schedule  = "0 7 * * 1-5"`},
			},
			wantOutput: []string{"2 of 23 owned file(s) differ", "differs  scheduler.tf:29"},
		},
		{
			name:   "a route the code adds has no job in the committed stack",
			mutate: func(*testing.T, string) {},
			code: func(m *derive.Model) {
				m.Scheduled = append([]derive.ScheduledRoute{{Path: "/_scheduled/close-stale-holds", Schedule: "*/15 * * * *", TimeZone: "UTC"}}, m.Scheduled...)
			},
			wantFindings: []Finding{
				{Path: "README.md", Line: 235, Want: "  | `POST /_scheduled/close-stale-holds` | `*/15 * * * *` | UTC |", Got: "  | `POST /_scheduled/send-daily-digest` | `0 7 * * 1-5` | America/New_York |"},
				{Path: "scheduler.tf", Line: 27, Want: `    "close-stale-holds" = {`, Got: `    "send-daily-digest" = {`},
			},
			wantOutput: []string{"2 of 23 owned file(s) differ", "differs  scheduler.tf:27"},
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
			name: "release-please's configuration missing is refused, not an unseeded line",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.Remove(filepath.Join(dir, "root", "release-please-config.json")); err != nil {
					t.Fatal(err)
				}
			},
			wantReleaseFiles: []string{"release-please-config.json"},
			wantOutput:       []string{"refused  release-please-config.json is missing at the application root: the release workflow reads it, and without it no release is cut and nothing reaches an environment; bedrock render seeds it when absent"},
		},
		{
			name: "release-please's manifest missing is refused",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.Remove(filepath.Join(dir, "root", ".release-please-manifest.json")); err != nil {
					t.Fatal(err)
				}
			},
			wantReleaseFiles: []string{".release-please-manifest.json"},
			wantOutput:       []string{"refused  .release-please-manifest.json is missing at the application root"},
		},
		{
			name: "release-please's files edited are the application's",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.WriteFile(filepath.Join(dir, "root", ".release-please-manifest.json"), []byte("{\".\": \"0.3.1\"}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantClean: true,
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
			wantOutput:  []string{"23 owned file(s) match the code", "refused  custom.tf:2 google_project_iam_binding.owners", "(a file store's bucket policy, storage.tf's, is the one admitted)"},
		},
		{
			name: "a binding on the file store's bucket is refused; the policy alone is admitted",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				custom := "resource \"google_storage_bucket_iam_binding\" \"files\" {\n  bucket  = google_storage_bucket.files.name\n  role    = \"roles/storage.objectViewer\"\n  members = []\n}\n"
				if err := os.WriteFile(filepath.Join(dir, "custom.tf"), []byte(custom), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantRefused: []Authoritative{{Path: "custom.tf", Line: 1, Address: "google_storage_bucket_iam_binding.files"}},
			wantOutput:  []string{"refused  custom.tf:1 google_storage_bucket_iam_binding.files"},
		},
		{
			name: "a policy on a bucket that is not a file store's is refused",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				custom := "resource \"google_storage_bucket_iam_policy\" \"records\" {\n  bucket      = \"records\"\n  policy_data = \"{}\"\n}\n"
				if err := os.WriteFile(filepath.Join(dir, "custom.tf"), []byte(custom), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantRefused: []Authoritative{{Path: "custom.tf", Line: 1, Address: "google_storage_bucket_iam_policy.records"}},
			wantOutput:  []string{"refused  custom.tf:1 google_storage_bucket_iam_policy.records"},
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
		{
			name: "a reserved stage of the Dockerfile holding more than its install is refused",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				path := filepath.Join(dir, "root", "Dockerfile")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.Replace(data, []byte("RUN go mod download\n"), []byte("COPY . ./\nRUN go mod download\n"), 1)
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantOutput: []string{"23 owned file(s) match the code", "refused  Dockerfile:68 stage go-modules copies .; the stage copies only go.mod or go.sum (\"COPY . ./\"): the image build exports this stage's layers to the registry's cache"},
		},
		{
			name: "a build argument the placement declares that the Dockerfile does not is refused",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				path := filepath.Join(dir, "root", "Dockerfile")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.ReplaceAll(data, []byte("ARG PROJECT_ID\n"), nil)
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantOutput: []string{"refused  placement.json declares build argument PROJECT_ID (projectId), which the Dockerfile does not declare: add the line \"ARG PROJECT_ID\" to the stage that builds with it"},
		},
		{
			name: "a Dockerfile without the reserved stages is warned about, not refused",
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.WriteFile(filepath.Join(dir, "root", "Dockerfile"), []byte("FROM golang AS build-env\nARG FIREBASE_API_KEY PROJECT_ID\nRUN go build -o /build/app . && go build -o /build/migrate ./cmd/deployment/migrate && go build -o /build/jobs ./cmd/jobs\nFROM scratch\nARG JOBS_JOB\nENV APP_VERSION=1 APP_CONSOLE_DIST=/c APP_PORTAL_DIST=/p APP_JOBS_JOB=\"${JOBS_JOB}\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantClean:  true,
			wantOutput: []string{"warning  Dockerfile has no go-modules stage: the image build caches nothing for the Go module download", "warning  Dockerfile has no web-packages stage: the image build caches nothing for the browser package install"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := copyGolden(t)
			tt.mutate(t, dir)
			m := harborModel(t)
			if tt.code != nil {
				tt.code(m)
			}
			report, err := Run(m, dir, filepath.Join(dir, "root"))
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
			if strings.Join(report.ReleaseFiles, ",") != strings.Join(tt.wantReleaseFiles, ",") {
				t.Errorf("ReleaseFiles = %v, want %v", report.ReleaseFiles, tt.wantReleaseFiles)
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

// TestRunLatest: check names each secret an environment's secret_versions lets track
// latest, environment by environment in promotion order, and says so when none does; both
// leave the check clean. A secret_versions that is not written out stops the check.
func TestRunLatest(t *testing.T) {
	t.Parallel()

	const none = "latest   no secret tracks latest: every entry of secret_versions in terraform.tfvars pins a version number"
	tests := []struct {
		name       string
		versions   string
		wantLatest []LatestSecret
		wantOutput []string
		absent     []string
		wantErr    string
	}{
		{
			name:       "the committed stack pins nothing, so nothing tracks latest",
			wantOutput: []string{none},
		},
		{
			name:       "every secret pinned to a version number",
			versions:   "secret_versions = {\n  tst = { APP_STRIPE_KEY = \"3\" }\n  stg = { APP_STRIPE_KEY = 2 }\n  prd = { APP_STRIPE_KEY = \"2\" }\n}\n",
			wantOutput: []string{none},
		},
		{
			name:       "secrets tracking latest, named per environment in promotion order",
			versions:   "secret_versions = {\n  prd = { APP_STRIPE_KEY = \"latest\", APP_MAPS_KEY = \"latest\" }\n  stg = { APP_STRIPE_KEY = \"2\" }\n  tst = { APP_STRIPE_KEY = \"latest\" }\n}\n",
			wantLatest: []LatestSecret{{Environment: "tst", Variable: "APP_STRIPE_KEY"}, {Environment: "prd", Variable: "APP_MAPS_KEY"}, {Environment: "prd", Variable: "APP_STRIPE_KEY"}},
			wantOutput: []string{
				"latest   APP_STRIPE_KEY tracks latest in tst (secret_versions.tst in terraform.tfvars): the environment runs whatever version is added next, with no release; pinning a version number is the default, and latest the exception for a secret that has to follow its source",
				"latest   APP_MAPS_KEY tracks latest in prd (secret_versions.prd in terraform.tfvars)",
				"latest   APP_STRIPE_KEY tracks latest in prd (secret_versions.prd in terraform.tfvars)",
			},
			absent: []string{none, "in stg (secret_versions.stg"},
		},
		{
			name:     "a secret_versions that is not written out stops the check",
			versions: "secret_versions = var.pins\n",
			wantErr:  "secret_versions in",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := copyGolden(t)
			if tt.versions != "" {
				path := filepath.Join(dir, "terraform.tfvars")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.Replace(data, []byte("secret_versions = {\n  tst = {}\n  stg = {}\n  prd = {}\n}\n"), []byte(tt.versions), 1)
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			report, err := Run(harborModel(t), dir, filepath.Join(dir, "root"))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Run() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if !report.Clean() {
				t.Errorf("Clean() = false, want true: a secret tracking latest is information, not drift")
			}
			if !slices.Equal(report.Latest, tt.wantLatest) {
				t.Errorf("Latest = %+v, want %+v", report.Latest, tt.wantLatest)
			}
			var out bytes.Buffer
			report.Write(&out)
			for _, want := range tt.wantOutput {
				if !strings.Contains(out.String(), want) {
					t.Errorf("Write() output lacks %q:\n%s", want, out.String())
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(out.String(), a) {
					t.Errorf("Write() output carries %q:\n%s", a, out.String())
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
