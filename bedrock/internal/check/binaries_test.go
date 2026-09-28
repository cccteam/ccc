package check

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

func TestScanBinaries(t *testing.T) {
	t.Parallel()

	migrate := &derive.Process{Name: "migrate", Dir: "cmd/deployment/migrate"}
	jobs := &derive.Process{Name: "jobs", Dir: "cmd/jobs"}
	const both = "FROM golang AS build-env\nRUN go build -o /build/app . && \\\n    go build -o /build/migrate ./cmd/deployment/migrate && \\\n    go build -o /build/jobs ./cmd/jobs\nFROM scratch\nCOPY --from=build-env /build /\n"
	tests := []struct {
		name       string
		dockerfile string
		noFile     bool
		model      derive.Model
		want       []BinaryFinding
	}{
		{
			name:       "a Dockerfile building every job's binary passes",
			dockerfile: both,
			model:      derive.Model{Migrate: migrate, Jobs: jobs},
		},
		{
			name:       "a job process the Dockerfile does not build is found",
			dockerfile: "FROM golang AS build-env\nRUN go build -o /build/app . && go build -o /build/migrate ./cmd/deployment/migrate\nFROM scratch\nCOPY --from=build-env /build /\n",
			model:      derive.Model{Migrate: migrate, Jobs: jobs},
			want:       []BinaryFinding{{Process: "jobs", Dir: "cmd/jobs", Binary: "/jobs"}},
		},
		{
			name:       "a mention in a comment is not a build",
			dockerfile: "# the job process (/jobs)\nFROM golang AS build-env\nRUN go build -o /build/migrate ./cmd/deployment/migrate\n",
			model:      derive.Model{Migrate: migrate, Jobs: jobs},
			want:       []BinaryFinding{{Process: "jobs", Dir: "cmd/jobs", Binary: "/jobs"}},
		},
		{
			name:       "a longer name is not the binary",
			dockerfile: "FROM golang AS build-env\nRUN go build -o /build/migrate ./cmd/deployment/migrate && go build -o /build/jobsworth ./cmd/jobsworth\n",
			model:      derive.Model{Migrate: migrate, Jobs: jobs},
			want:       []BinaryFinding{{Process: "jobs", Dir: "cmd/jobs", Binary: "/jobs"}},
		},
		{
			name:       "without a job process only the migrate command is looked for",
			dockerfile: "FROM golang AS build-env\nRUN go build -o /build/migrate ./cmd/deployment/migrate\n",
			model:      derive.Model{Migrate: migrate},
		},
		{
			name:       "a migrate command the Dockerfile does not build is found",
			dockerfile: "FROM golang AS build-env\nRUN go build -o /build/app .\n",
			model:      derive.Model{Migrate: migrate},
			want:       []BinaryFinding{{Process: "migrate", Dir: "cmd/deployment/migrate", Binary: "/migrate"}},
		},
		{
			name:   "no Dockerfile, nothing to scan",
			noFile: true,
			model:  derive.Model{Migrate: migrate, Jobs: jobs},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appDir := t.TempDir()
			if !tt.noFile {
				if err := os.WriteFile(filepath.Join(appDir, "Dockerfile"), []byte(tt.dockerfile), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := scanBinaries(appDir, &tt.model)
			if err != nil {
				t.Fatalf("scanBinaries() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("scanBinaries() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
