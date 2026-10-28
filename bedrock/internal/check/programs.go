// programs.go checks the Dockerfile compiles the shipped programs the release files
// declare: the command programs a generator program declares on a machine outlet, which
// the pipeline passes to the image build as the PROGRAMS build argument and the seeded
// Dockerfile's loop compiles for their platforms. A build argument reaches a stage's
// instructions only once the stage declares it with ARG, so a Dockerfile seeded before
// the loop existed, which declares it nowhere, builds an image that carries no program,
// and the outlet that declares one serves nothing.

package check

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// programsArgument is the build argument the pipeline passes the shipped programs in.
const programsArgument = "PROGRAMS"

// The seeded Dockerfile's lines for the shipped programs (Dockerfile.tmpl, whose rendering
// the goldens pin): programsBuildLines is their compile, in the Go build stage after the
// binaries' build; programsRuntimeLines their copy into the runtime image; and
// programsVariable what the runtime stage's ENV line adds, the directory the server
// serves them from. The check prints them for an application that ships a program from a
// Dockerfile without them.
const (
	programsBuildLines = `ARG VERSION
ARG PROGRAMS
RUN mkdir -p /programs && for program in $PROGRAMS; do \
        dir="${program%%=*}"; \
        (IFS=,; for platform in ${program#*=}; do \
            GOOS="${platform%/*}" GOARCH="${platform#*/}" go build -trimpath \
                -ldflags="-s -w -X github.com/cccteam/ccc/resource/shipped.Release=${VERSION}" \
                -o "/programs/${dir##*/}-${platform%/*}-${platform#*/}" "./$dir" || exit 1; \
        done) || exit 1; \
    done`
	programsRuntimeLines = "COPY --from=build-env /programs /programs"
	programsVariable     = "APP_PROGRAMS=/programs"
)

// ProgramsFinding is the shipped programs the release files declare where no stage of
// the Dockerfile declares the PROGRAMS build argument.
type ProgramsFinding struct {
	// Programs are the application's shipped programs, by directory.
	Programs []derive.ReleaseProgram
}

// scanPrograms reads the Dockerfile at the application root for an ARG of the PROGRAMS
// build argument in any stage, when the release files ship a program, and reports the
// programs when no stage declares it. An ARG before the first FROM does not count: it
// reaches the FROM lines alone. An application that ships none passes whatever the
// Dockerfile declares, and one without a Dockerfile has nothing to scan: the unseeded
// finding covers it.
func scanPrograms(appDir string, m *derive.Model) (*ProgramsFinding, error) {
	if len(m.ShippedPrograms) == 0 {
		return nil, nil
	}
	src, err := os.ReadFile(filepath.Join(appDir, dockerfileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile(): Dockerfile")
	}
	for _, s := range Stages(src) {
		for _, name := range s.Args() {
			if name == programsArgument {
				return nil, nil
			}
		}
	}

	return &ProgramsFinding{Programs: m.ShippedPrograms}, nil
}

// Refusal is the refusal's text, for the report's refused line: the first line names the
// Dockerfile as given, the programs the release files ship and why the image would carry
// none, and the lines after it are the ones to add, the compile and then the copy.
func (f *ProgramsFinding) Refusal(dockerfile string) []string {
	first := fmt.Sprintf("%s declares no %s build argument, and the release files ship %s: the pipeline passes the shipped programs to the image build as %s, which a stage reads only once it declares it, so the image would carry no program and the outlet serve none; add these lines to the Go build stage after its go build, as the seeded Dockerfile has them:", dockerfile, programsArgument, f.sentence(), programsArgument)
	lines := append([]string{first}, strings.Split(programsBuildLines, "\n")...)

	return append(lines, "and this line to the runtime stage after the binaries' copy, with "+programsVariable+" added to its ENV line, the directory the server serves the programs from:", programsRuntimeLines)
}

// sentence names the programs with their platforms: "cmd/corral (for linux/amd64 and
// linux/arm64) and cmd/beacon (for linux/amd64)".
func (f *ProgramsFinding) sentence() string {
	parts := make([]string, 0, len(f.Programs))
	for i := range f.Programs {
		parts = append(parts, f.Programs[i].Dir+" (for "+joinAnd(f.Programs[i].Platforms)+")")
	}

	return joinAnd(parts)
}
