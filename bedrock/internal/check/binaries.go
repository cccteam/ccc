// binaries.go checks the image build against the jobs the stack deploys: a Cloud Run job
// runs a command the image must carry (/migrate for the migrate command, /jobs for the job
// process), and a Dockerfile that builds no such binary makes a job that fails at its
// first run, after the stack and the pipeline said nothing.

package check

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// dockerfileName is the image build at the application root.
const dockerfileName = "Dockerfile"

// BinaryFinding is a job whose command the Dockerfile does not build.
type BinaryFinding struct {
	// Process is the process's name (migrate, jobs), Dir its main package, and Binary
	// the path its Cloud Run job runs.
	Process string
	Dir     string
	Binary  string
}

// BundleFinding is a browser bundle the Dockerfile does not set the site's variable
// for: the site would serve the bundle from the code's default, a path the image does
// not hold.
type BundleFinding struct {
	// Var is the site's variable, Path the bundle it names (web/dist/console).
	Var  string
	Path string
}

// scanBundles reports every browser bundle the site declares (a site-level variable
// whose default is <workspace>/dist/<bundle>) that the Dockerfile's ENV does not set:
// the seeded Dockerfile builds each workspace and sets the variable to where it put the
// bundle, and a workspace added later needs the same by hand.
func scanBundles(m *derive.Model) []BundleFinding {
	var findings []BundleFinding
	for _, v := range m.ByLevel(derive.LevelSite) {
		if !v.HasDefault || v.Image || !derive.BundleRE.MatchString(v.Default) {
			continue
		}
		findings = append(findings, BundleFinding{Var: v.Name, Path: v.Default})
	}

	return findings
}

// scanBinaries reads the Dockerfile at the application root for the binary each job of
// the stack runs, /<process> (the seeded Dockerfile builds it with go build -o
// /build/<process> and copies /build into the runtime image), and reports every job
// whose binary no instruction names. A comment is not an instruction. An application
// without a Dockerfile has nothing to scan: the unseeded finding covers it.
func scanBinaries(appDir string, m *derive.Model) ([]BinaryFinding, error) {
	src, err := os.ReadFile(filepath.Join(appDir, dockerfileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile(): Dockerfile")
	}
	instructions := instructionLines(src)
	var findings []BinaryFinding
	for _, p := range []*derive.Process{m.Migrate, m.Jobs} {
		if p == nil {
			continue
		}
		binary := "/" + p.Name
		if regexp.MustCompile(regexp.QuoteMeta(binary) + `\b`).MatchString(instructions) {
			continue
		}
		findings = append(findings, BinaryFinding{Process: p.Name, Dir: p.Dir, Binary: binary})
	}

	return findings, nil
}

// instructionLines is the Dockerfile without its comment lines, one line per line.
func instructionLines(dockerfile []byte) string {
	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(dockerfile))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}
