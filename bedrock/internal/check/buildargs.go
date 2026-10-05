// buildargs.go checks the build arguments the placement declares (buildArguments) against
// the Dockerfile: the pipeline passes each to the image build, and a build argument
// reaches a stage's instructions only once the stage declares it with ARG, so one the
// Dockerfile never declares is a value the build is handed and drops without a word.

package check

import (
	"os"
	"path/filepath"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// BuildArgumentFinding is a build argument the placement declares that no stage of the
// Dockerfile declares with ARG.
type BuildArgumentFinding struct {
	// Name is the argument's name, Value the catalog's value it carries.
	Name  string
	Value string
}

// scanBuildArguments reads the Dockerfile at the application root for an ARG of each build
// argument the placement declares, in any stage, and reports each no stage declares. An
// ARG before the first FROM does not count: it reaches the FROM lines alone. An
// application without a Dockerfile has nothing to scan: the unseeded finding covers it.
func scanBuildArguments(appDir string, p *derive.Placement) ([]BuildArgumentFinding, error) {
	names := p.BuildArgumentNames()
	if len(names) == 0 {
		return nil, nil
	}
	src, err := os.ReadFile(filepath.Join(appDir, dockerfileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile(): Dockerfile")
	}
	declared := map[string]bool{}
	for _, s := range Stages(src) {
		for _, name := range s.Args() {
			declared[name] = true
		}
	}
	var findings []BuildArgumentFinding
	for _, name := range names {
		if !declared[name] {
			findings = append(findings, BuildArgumentFinding{Name: name, Value: p.BuildArguments[name]})
		}
	}

	return findings, nil
}
