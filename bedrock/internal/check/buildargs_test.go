package check

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// argStages is a Dockerfile with a global ARG, a browser stage declaring one argument and
// a runtime stage declaring another with a default.
const argStages = `ARG GLOBAL_ONLY
FROM node AS web-build-env
ARG VERSION
ARG FIREBASE_API_KEY
RUN bun run build
FROM scratch
ARG HOSTNAME_ARG=localhost PROJECT_ID
COPY --from=web-build-env /dist /dist
`

func TestScanBuildArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dockerfile string
		noFile     bool
		args       map[string]string
		want       []BuildArgumentFinding
	}{
		{name: "no build argument declared: nothing to scan", dockerfile: "FROM scratch\n"},
		{name: "every declared argument has its ARG, one with a default and two on a line", dockerfile: argStages, args: map[string]string{"FIREBASE_API_KEY": "firebaseApiKey", "PROJECT_ID": "projectId", "HOSTNAME_ARG": "hostname"}},
		{
			name:       "an argument no stage declares is refused, with the value it takes",
			dockerfile: argStages,
			args:       map[string]string{"FIREBASE_API_KEY": "firebaseApiKey", "FIRESTORE_DB": "firestoreDatabase"},
			want:       []BuildArgumentFinding{{Name: "FIRESTORE_DB", Value: "firestoreDatabase"}},
		},
		{
			name:       "an ARG before the first FROM reaches the FROM lines alone, and is refused",
			dockerfile: argStages,
			args:       map[string]string{"GLOBAL_ONLY": "environment"},
			want:       []BuildArgumentFinding{{Name: "GLOBAL_ONLY", Value: "environment"}},
		},
		{
			name:       "a commented ARG declares nothing",
			dockerfile: "FROM node AS web\n# ARG ENVIRONMENT\nRUN bun run build\n",
			args:       map[string]string{"ENVIRONMENT": "environment"},
			want:       []BuildArgumentFinding{{Name: "ENVIRONMENT", Value: "environment"}},
		},
		{name: "no Dockerfile: the unseeded finding covers it", noFile: true, args: map[string]string{"PROJECT_ID": "projectId"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if !tt.noFile {
				if err := os.WriteFile(filepath.Join(dir, dockerfileName), []byte(tt.dockerfile), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := scanBuildArguments(dir, &derive.Placement{BuildArguments: tt.args})
			if err != nil {
				t.Fatalf("scanBuildArguments() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("scanBuildArguments() (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReport_Write_buildArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		finding BuildArgumentFinding
		want    string
	}{
		{
			name:    "the Firebase key",
			finding: BuildArgumentFinding{Name: "FIREBASE_API_KEY", Value: "firebaseApiKey"},
			want:    "  refused  placement.json declares build argument FIREBASE_API_KEY (firebaseApiKey), which the Dockerfile does not declare: add the line \"ARG FIREBASE_API_KEY\" to the stage that builds with it (a browser build stage, before its build), since a build argument reaches only the stages that declare it\n",
		},
		{
			name:    "the hostname",
			finding: BuildArgumentFinding{Name: "SITE_HOST", Value: "hostname"},
			want:    "  refused  placement.json declares build argument SITE_HOST (hostname), which the Dockerfile does not declare: add the line \"ARG SITE_HOST\" to the stage that builds with it (a browser build stage, before its build), since a build argument reaches only the stages that declare it\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &Report{Dir: "infrastructure", AppDir: ".", BuildArguments: []BuildArgumentFinding{tt.finding}}
			if r.Clean() {
				t.Error("Clean() = true with a build-argument finding")
			}
			var b bytes.Buffer
			r.Write(&b)
			if got := b.String(); !bytes.HasSuffix([]byte(got), []byte(tt.want)) {
				t.Errorf("Write() = %q, want a last line %q", got, tt.want)
			}
		})
	}
}
