// start.go checks how the image starts against how the stack's services start it. In
// the sites layout each site's Cloud Run service passes its own binary as the
// container's arguments (cloud-run.tf), which replace the image's default command (CMD)
// and follow an entrypoint (ENTRYPOINT), so the image sets no entrypoint and names the
// first site's binary as its default command. The Dockerfile is seeded once and then
// the application's, so an image seeded before that contract keeps its entrypoint:
// every site's service then runs the entrypoint with its own binary as an argument, and
// every one serves the first site, with nothing refusing it. The flat image keeps /app
// as its entrypoint, and its service sets neither command nor arguments.

package check

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// The instruction keywords the start is read from, as Dockerfile writes them.
const (
	keywordEntrypoint = "ENTRYPOINT"
	keywordCmd        = "CMD"
)

// multiSiteStart is how a sites-layout image ends, as the seeded Dockerfile writes it
// (Dockerfile.tmpl, whose rendering the lantern golden pins): the comment and the CMD
// naming the first site's binary. The check prints it for an image that breaks the
// contract.
const multiSiteStart = `# No ENTRYPOINT: the default command is the first site's binary, and each site's Cloud Run
# service names its own binary as the container's arguments, which replace it (cloud-run.tf).
# The placeholder image the stack creates the services with keeps its own entrypoint, so
# the first apply's revisions start before the pipeline's first deploy.
CMD [%q]`

// ImageStart is how the image starts, read from the ENTRYPOINT and CMD of the
// Dockerfile's last stage, the one the image is: a line of the report when the start
// follows the layout's contract, a refusal when a sites-layout image breaks it
// (Problem set).
type ImageStart struct {
	// Found says what the Dockerfile sets, as written: `ENTRYPOINT ["/app"]`,
	// `CMD ["/console"]`, the two joined, or that it sets neither.
	Found string
	// Multi reports the sites layout, whose contract the start is held to.
	Multi bool
	// Binary is the binary the image starts on its own, and in the sites layout Site
	// the site it serves; both empty when Problem is set, and Site in the flat layout.
	Binary string
	Site   string
	// Problem is what is wrong with a sites-layout image's start, empty when it
	// passes.
	Problem string
	// Lines are the lines a sites-layout image ends with, the comment and the CMD
	// naming the first site's binary, as the seeded Dockerfile has them; empty in the
	// flat layout.
	Lines string
}

// ScanStart reads the Dockerfile at the application root for how the image starts and
// holds it to the layout's contract: in the sites layout no ENTRYPOINT and a CMD in the
// exec form naming one site's binary (the first site's is the default the seeded
// Dockerfile writes); in the flat layout whatever the image sets, described and never
// refused. It answers nil without a Dockerfile (the unseeded finding covers it) and for
// a flat image that sets neither instruction. It is bedrock check's reading, and the one
// bedrock render repeats for the Dockerfile it keeps.
func ScanStart(appDir string, m *derive.Model) (*ImageStart, error) {
	src, err := os.ReadFile(filepath.Join(appDir, dockerfileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile(): Dockerfile")
	}
	entrypoint, cmd := startInstructions(Stages(src))
	start := &ImageStart{Found: foundStart(entrypoint, cmd), Multi: m.MultiSite()}
	if !start.Multi {
		if entrypoint == nil && cmd == nil {
			return nil, nil
		}
		start.Binary = startBinary(entrypoint, cmd)

		return start, nil
	}
	binaries := make([]string, 0, len(m.Sites))
	for i := range m.Sites {
		binaries = append(binaries, "/"+m.Sites[i].Process.Name)
	}
	start.Lines = fmt.Sprintf(multiSiteStart, binaries[0])
	start.Problem = startProblem(entrypoint, cmd, binaries)
	if start.Problem != "" {
		return start, nil
	}
	start.Binary = execForm(cmd.Args)[0]
	start.Site = strings.TrimPrefix(start.Binary, "/")

	return start, nil
}

// startProblem says what is wrong with a sites-layout image's start, or nothing when it
// follows the contract: no ENTRYPOINT, and a CMD in the exec form naming one of the
// sites' binaries alone.
func startProblem(entrypoint, cmd *Instruction, binaries []string) string {
	switch {
	case entrypoint != nil:
		program := startBinary(entrypoint, nil)
		if program == "" {
			program = "the entrypoint"
		}

		return fmt.Sprintf("an entrypoint runs with the container's arguments after it, so each site's service would start %s with its own binary as an argument, never its own binary", program)
	case cmd == nil:
		return "the image has no default command, and runs no site on its own"
	}
	fields := execForm(cmd.Args)
	switch {
	case fields == nil:
		return "a CMD in the shell form runs through /bin/sh, which the image does not hold; the exec form, a JSON array, names the binary alone"
	case len(fields) != 1 || !slices.Contains(binaries, fields[0]):
		return fmt.Sprintf("%s is not one of the sites' binaries (%s), so the image's default command runs no site", strings.Join(fields, " "), strings.Join(binaries, ", "))
	default:
		return ""
	}
}

// Refusal is the refusal's text, for the report's refused line and bedrock render's
// note: the first line names the Dockerfile as given, says what it sets and why the
// stack's services cannot start with it, and the lines after it are the ones to end the
// image with.
func (s *ImageStart) Refusal(dockerfile string) []string {
	first := fmt.Sprintf("%s sets %s: %s. The stack's services pass each site's binary as the container's arguments (cloud-run.tf), which replace a default command and follow an entrypoint, so the image sets no ENTRYPOINT and names the first site's binary as its CMD; end the image with these lines in place of what it sets, as the seeded Dockerfile has them:", dockerfile, s.Found, s.Problem)

	return append([]string{first}, strings.Split(s.Lines, "\n")...)
}

// Passed is the report's line for a start that follows the contract: in the sites
// layout the image sets no entrypoint and its default command is a site's binary; in
// the flat layout the image starts the site as it says, and its service runs it as
// written.
func (s *ImageStart) Passed() string {
	if s.Multi {
		return fmt.Sprintf("Dockerfile sets no ENTRYPOINT, and its default command is %s, the %s site's binary: each site's service passes its own binary as the container's arguments (cloud-run.tf), which replace it", s.Binary, s.Site)
	}

	return fmt.Sprintf("Dockerfile starts the site with %s, which its service runs as written (no command or arguments of its own)", s.Found)
}

// startInstructions are the last ENTRYPOINT and the last CMD of the Dockerfile's last
// stage, the one the image is (an earlier stage's are not the image's); nil for one
// the stage does not set.
func startInstructions(stages []Stage) (entrypoint, cmd *Instruction) {
	if len(stages) == 0 {
		return nil, nil
	}
	last := &stages[len(stages)-1]
	for i := range last.Instructions {
		in := &last.Instructions[i]
		switch in.Keyword {
		case keywordEntrypoint:
			entrypoint = in
		case keywordCmd:
			cmd = in
		}
	}

	return entrypoint, cmd
}

// foundStart says what the image sets, as written: the instruction, the two joined
// with "and", or that it sets neither.
func foundStart(entrypoint, cmd *Instruction) string {
	var found []string
	for _, in := range []*Instruction{entrypoint, cmd} {
		if in != nil {
			found = append(found, in.Keyword+" "+in.Args)
		}
	}
	if len(found) == 0 {
		return "neither an ENTRYPOINT nor a CMD"
	}

	return strings.Join(found, " and ")
}

// startBinary is the program the image starts on its own: the entrypoint's first
// word, else the default command's, in either form.
func startBinary(entrypoint, cmd *Instruction) string {
	for _, in := range []*Instruction{entrypoint, cmd} {
		if in == nil {
			continue
		}
		if words := fields(in.Args); len(words) > 0 {
			return words[0]
		}
	}

	return ""
}

// execForm reads an instruction's arguments in the exec form, a JSON array of words;
// nil for the shell form.
func execForm(args string) []string {
	trimmed := strings.TrimSpace(args)
	if !strings.HasPrefix(trimmed, "[") {
		return nil
	}
	var words []string
	if err := json.Unmarshal([]byte(trimmed), &words); err != nil {
		return nil
	}

	return words
}
