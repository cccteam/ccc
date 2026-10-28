package check

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// programStages is the seeded Dockerfile's shape in short around the shipped programs: a
// Go build stage declaring PROGRAMS after the binaries' build, and a runtime stage
// copying the programs in.
const programStages = "FROM golang AS build-env\nRUN go build -o /build/app .\nARG VERSION\nARG PROGRAMS\nRUN mkdir -p /programs\nFROM scratch\nCOPY --from=build-env /programs /programs\nENV APP_PROGRAMS=/programs\n"

// noProgramStages is a Dockerfile seeded before the shipped programs existed.
const noProgramStages = "FROM golang AS build-env\nRUN go build -o /build/app .\nFROM scratch\nCOPY --from=build-env /build /\n"

func TestScanPrograms(t *testing.T) {
	t.Parallel()

	corral := derive.ReleaseProgram{Name: "corral", Dir: "cmd/corral", Platforms: []string{"linux/amd64"}}
	tests := []struct {
		name       string
		dockerfile string
		noFile     bool
		programs   []derive.ReleaseProgram
		want       *ProgramsFinding
	}{
		{name: "no program shipped: a Dockerfile without the argument passes", dockerfile: noProgramStages},
		{name: "no program shipped: one with it passes too", dockerfile: programStages},
		{name: "a program shipped, and the Go stage declares the argument", dockerfile: programStages, programs: []derive.ReleaseProgram{corral}},
		{name: "a program shipped, and the committed golden declares it", dockerfile: readGolden(t, "root/Dockerfile"), programs: []derive.ReleaseProgram{corral}},
		{name: "a program shipped, and no stage declares the argument", dockerfile: noProgramStages, programs: []derive.ReleaseProgram{corral}, want: &ProgramsFinding{Programs: []derive.ReleaseProgram{corral}}},
		{name: "an ARG before the first FROM reaches the FROM lines alone, and is refused", dockerfile: "ARG PROGRAMS\n" + noProgramStages, programs: []derive.ReleaseProgram{corral}, want: &ProgramsFinding{Programs: []derive.ReleaseProgram{corral}}},
		{name: "a commented ARG declares nothing", dockerfile: strings.Replace(programStages, "ARG PROGRAMS\n", "# ARG PROGRAMS\n", 1), programs: []derive.ReleaseProgram{corral}, want: &ProgramsFinding{Programs: []derive.ReleaseProgram{corral}}},
		{name: "no Dockerfile: the unseeded finding covers it", noFile: true, programs: []derive.ReleaseProgram{corral}},
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
			got, err := scanPrograms(dir, &derive.Model{ShippedPrograms: tt.programs})
			if err != nil {
				t.Fatalf("scanPrograms() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("scanPrograms() (-want +got):\n%s", diff)
			}
		})
	}
}

// TestReport_Write_programs writes the refusal: the first line names the programs and
// why, and the lines to add follow it, indented, the compile then the copy.
func TestReport_Write_programs(t *testing.T) {
	t.Parallel()

	const indent = "           "
	tests := []struct {
		name     string
		programs []derive.ReleaseProgram
		wantHead string
	}{
		{
			name:     "one program for one platform",
			programs: []derive.ReleaseProgram{{Name: "corral", Dir: "cmd/corral", Platforms: []string{"linux/amd64"}}},
			wantHead: "  refused  Dockerfile declares no PROGRAMS build argument, and the release files ship cmd/corral (for linux/amd64): the pipeline passes the shipped programs to the image build as PROGRAMS, which a stage reads only once it declares it, so the image would carry no program and the outlet serve none; add these lines to the Go build stage after its go build, as the seeded Dockerfile has them:\n",
		},
		{
			name:     "two programs, one for two platforms",
			programs: []derive.ReleaseProgram{{Name: "beacon", Dir: "cmd/beacon", Platforms: []string{"linux/amd64"}}, {Name: "corral", Dir: "cmd/corral", Platforms: []string{"linux/amd64", "linux/arm64"}}},
			wantHead: "  refused  Dockerfile declares no PROGRAMS build argument, and the release files ship cmd/beacon (for linux/amd64) and cmd/corral (for linux/amd64 and linux/arm64): the pipeline passes",
		},
	}
	wantTail := indent + strings.ReplaceAll(programsBuildLines, "\n", "\n"+indent) + "\n" +
		indent + "and this line to the runtime stage after the binaries' copy, with APP_PROGRAMS=/programs added to its ENV line, the directory the server serves the programs from:\n" +
		indent + "COPY --from=build-env /programs /programs\n"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &Report{Dir: "infrastructure", AppDir: ".", Programs: &ProgramsFinding{Programs: tt.programs}}
			if r.Clean() {
				t.Error("Clean() = true with a programs finding")
			}
			var b bytes.Buffer
			r.Write(&b)
			got := b.String()
			if !strings.Contains(got, tt.wantHead) {
				t.Errorf("Write() = %q, want the line %q", got, tt.wantHead)
			}
			if !strings.HasSuffix(got, wantTail) {
				t.Errorf("Write() = %q, want the last lines %q", got, wantTail)
			}
		})
	}
}

// TestProgramsLinesSeeded holds the lines the check prints to the seeded Dockerfile's
// rendering: the committed golden carries the compile, the copy and the variable on its
// ENV line verbatim, so what the check tells an application to add is what the seed
// writes.
func TestProgramsLinesSeeded(t *testing.T) {
	t.Parallel()

	golden := readGolden(t, "root/Dockerfile")
	for _, want := range []string{programsBuildLines, programsRuntimeLines, "ENV APP_VERSION=\"${VERSION}\" \\\n    " + programsVariable} {
		if !strings.Contains(golden, want) {
			t.Errorf("the golden Dockerfile lacks:\n%s", want)
		}
	}
}
