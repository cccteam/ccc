package check

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

func TestScanJobName(t *testing.T) {
	t.Parallel()

	jobs := &derive.Process{Name: "jobs", Dir: "cmd/jobs"}
	site := derive.Model{Jobs: jobs, Variables: []derive.Variable{{Name: "APP_JOBS_JOB", Role: derive.RoleJobsJob, Level: derive.LevelSite}}}
	const seeded = "ARG VERSION=dev\nARG JOBS_JOB=\nFROM static\nARG VERSION\nARG JOBS_JOB\nENV APP_VERSION=\"${VERSION}\" \\\n    APP_JOBS_JOB=\"${JOBS_JOB}\"\n"
	tests := []struct {
		name       string
		dockerfile string
		model      derive.Model
		want       []JobNameFinding
	}{
		{name: "the seeded Dockerfile carries the job", dockerfile: seeded, model: site},
		{name: "an ENV on one line, unbraced", dockerfile: "ARG JOBS_JOB\nENV APP_JOBS_JOB=$JOBS_JOB\n", model: site},
		{
			name:       "a Dockerfile without the argument",
			dockerfile: "FROM static\nENV APP_JOBS_JOB=\"${JOBS_JOB}\"\n",
			model:      site,
			want:       []JobNameFinding{{Var: "APP_JOBS_JOB", Missing: "ARG JOBS_JOB"}},
		},
		{
			name:       "a Dockerfile that never sets the variable",
			dockerfile: "ARG JOBS_JOB=\nFROM static\nARG JOBS_JOB\nENV APP_VERSION=\"${VERSION}\"\n",
			model:      site,
			want:       []JobNameFinding{{Var: "APP_JOBS_JOB", Missing: "ENV APP_JOBS_JOB=\"${JOBS_JOB}\""}},
		},
		{
			name:       "a commented line is not an instruction",
			dockerfile: "# ARG JOBS_JOB\n# ENV APP_JOBS_JOB=\"${JOBS_JOB}\"\n",
			model:      site,
			want:       []JobNameFinding{{Var: "APP_JOBS_JOB", Missing: "ARG JOBS_JOB"}, {Var: "APP_JOBS_JOB", Missing: "ENV APP_JOBS_JOB=\"${JOBS_JOB}\""}},
		},
		{name: "a site that declares no job variable needs nothing", dockerfile: "FROM static\n", model: derive.Model{Jobs: jobs}},
		{name: "an application without a job process needs nothing", dockerfile: "FROM static\n", model: derive.Model{Variables: site.Variables}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(appDir, dockerfileName), []byte(tt.dockerfile), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := scanJobName(appDir, &tt.model)
			if err != nil {
				t.Fatalf("scanJobName() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("scanJobName() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestScanJobNameWithoutDockerfile(t *testing.T) {
	t.Parallel()

	model := derive.Model{Jobs: &derive.Process{Name: "jobs", Dir: "cmd/jobs"}, Variables: []derive.Variable{{Name: "APP_JOBS_JOB", Role: derive.RoleJobsJob, Level: derive.LevelSite}}}
	got, err := scanJobName(t.TempDir(), &model)
	if err != nil || got != nil {
		t.Errorf("scanJobName() = %v, %v; want nothing without a Dockerfile", got, err)
	}
}
