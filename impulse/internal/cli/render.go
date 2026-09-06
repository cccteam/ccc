package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

func newRender() *cobra.Command {
	var (
		modulePath string
		devRoot    string
	)

	cmd := &cobra.Command{
		Use:   "render <candidate> <dir>",
		Short: "Render one embedded skeleton into a directory (developer command)",
		Long: `render copies one of the embedded skeleton templates into a new or empty directory
under the module path you name, rewriting every import and go.mod to it. It is the
primitive impulse new builds on, and the way the templates are validated: render one,
then build, test, and check the result. The placeholder auth (staff) is kept; new renames it.

With --dev-root, render also writes a go.work that uses every framework module the
application requires directly and that has a checkout under that directory (laid out by
repository: <root>/ccc/resource, <root>/session, ...), so the application builds against
local framework work instead of the pins. Do not commit that go.work.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			got, err := skeleton.Render(&skeleton.Options{Candidate: args[0], Dir: args[1], ModulePath: modulePath, DevRoot: devRoot})
			if err != nil {
				return err
			}

			port, emulator := templatePorts(args[1])
			goProcs, err := goProcesses(args[1])
			if err != nil {
				return err
			}
			web, err := webWorkspaces(args[1])
			if err != nil {
				return err
			}
			report := renderReport{
				candidate: args[0], dir: args[1], modulePath: modulePath, devRoot: devRoot,
				rendered: got, port: port, emulator: emulator, goProcs: goProcs, web: web,
				styled: isTerminal(cmd.OutOrStdout()),
			}
			report.write(cmd.OutOrStdout())

			return nil
		},
	}

	cmd.Flags().StringVar(&modulePath, "module", "", "module path of the rendered application (required)")
	cmd.Flags().StringVar(&devRoot, "dev-root", "", "directory of cccteam checkouts to build against instead of the pins")
	_ = cmd.MarkFlagRequired("module")

	return cmd
}

var (
	portLine     = regexp.MustCompile(`(?m)^export PORT=(\d+)`)
	emulatorLine = regexp.MustCompile(`(?m)^export SPANNER_EMULATOR_PORT=(\d+)`)
	// packageManagerWord marks a Procfile command as a browser-app process.
	packageManagerWord = regexp.MustCompile(`(?:^|[\s;&|(])(?:npm|npx|bun|bunx|yarn|pnpm)\s`)
)

// templatePorts reads the server and emulator ports the rendered application's
// development environment template names, or empty strings when it has none. A taken
// port is the first thing a fresh application trips on, and the failure hides in a
// process pane, so the ports are worth stating up front.
func templatePorts(dir string) (port, emulator string) {
	data, err := os.ReadFile(filepath.Join(dir, ".envrc.template"))
	if err != nil {
		return "", ""
	}
	if m := portLine.FindSubmatch(data); m != nil {
		port = string(m[1])
	}
	if m := emulatorLine.FindSubmatch(data); m != nil {
		emulator = string(m[1])
	}

	return port, emulator
}

// renderReport is what the render command tells the user: what it did in a few lines,
// then the numbered steps to a running application. The steps are the part a reader
// acts on, so they carry the emphasis when the output is a terminal.
type renderReport struct {
	// headline replaces the "Rendered ..." line when set (impulse new); gitNote reports
	// the first commit; options adds the step that names the options to add next.
	headline, gitNote string
	options           bool

	candidate, dir, modulePath, devRoot string
	rendered                            *skeleton.Rendered
	port, emulator                      string
	// goProcs are the Procfile processes that run without the browser apps installed.
	goProcs []string
	// web lists the browser workspaces and the ng serve ports of their projects.
	web []webWorkspace
	// styled turns on ANSI bold for the heading and step numbers.
	styled bool
}

// webWorkspace is one Angular workspace in the rendered tree.
type webWorkspace struct {
	// Dir is the workspace directory relative to the application root.
	Dir string
	// Projects maps each project to its development ng serve port, 0 when unset.
	Projects []webProject
}

type webProject struct {
	Name string
	Port int
}

func (r *renderReport) write(w io.Writer) {
	bold := func(s string) string {
		if !r.styled {
			return s
		}

		return "\x1b[1m" + s + "\x1b[0m"
	}

	if r.headline != "" {
		fmt.Fprintln(w, r.headline)
	} else {
		fmt.Fprintf(w, "Rendered %s into %s as %s (%d files).\n", r.candidate, r.dir, r.modulePath, r.rendered.Files)
	}
	if r.gitNote != "" {
		fmt.Fprintln(w, r.gitNote)
	}
	if r.rendered.Workspace != "" {
		fmt.Fprintf(w, "Wrote go.work using %d local framework checkout(s) under %s.\n", len(r.rendered.DevUsed), r.devRoot)
		if len(r.rendered.DevMissing) > 0 {
			fmt.Fprintf(w, "No checkout for %s; those pins stay in force.\n", strings.Join(r.rendered.DevMissing, ", "))
		}
	}
	if r.port != "" {
		fmt.Fprintf(w, "\n%s the server listens on :%s and the Spanner emulator on :%s.\n", bold("Ports:"), r.port, r.emulator)
		fmt.Fprintf(w, "       Both are set in .envrc.template; change them there if either is taken.\n")
	}

	fmt.Fprintf(w, "\n%s\n", bold("Next steps"))
	step := 0
	next := func(format string, args ...any) {
		step++
		fmt.Fprintf(w, "  %s %s\n", bold(fmt.Sprintf("%d.", step)), fmt.Sprintf(format, args...))
	}
	note := func(format string, args ...any) {
		fmt.Fprintf(w, "     %s\n", fmt.Sprintf(format, args...))
	}

	next("cd %s", r.dir)
	next("cp .envrc.template .envrc && direnv allow")
	if len(r.web) == 0 || len(r.goProcs) == 0 {
		next("overmind start")
		note("The first run compiles and bootstraps before it listens. Wait for \"Starting Server\",")
		note("then sign in as admin with the password \"password\".")
		r.optionsStep(next, note)

		return
	}

	next("overmind start -l %s", strings.Join(r.goProcs, ","))
	note("The Go side alone. The first run compiles and bootstraps before it listens; wait for")
	note("\"Starting Server\", then sign in against the API as admin with the password \"password\".")
	for _, ws := range r.web {
		next("(cd %s && ./ccclib.sh local)", ws.Dir)
	}
	note("Once, for the browser apps: publishes ccc-lib to the local yalc store, links it, and runs")
	note("bun install. ccclib.sh expects the ccc-lib checkout beside this application; set CCC_LIB otherwise.")
	next("overmind start")
	var urls []string
	for _, ws := range r.web {
		for _, p := range ws.Projects {
			if p.Port != 0 {
				urls = append(urls, fmt.Sprintf("%s at http://127.0.0.1:%d", p.Name, p.Port))
			}
		}
	}
	if len(urls) > 0 {
		note("Everything, with ng serve for the %s.", strings.Join(urls, " and the "))
	}
	r.optionsStep(next, note)
}

// optionsStep names the options an application takes on afterwards, when the report is
// for a new application.
func (r *renderReport) optionsStep(next, note func(format string, args ...any)) {
	if !r.options {
		return
	}
	next("impulse add tenancy | impulse add outlet <name> --prefix <p> --sessions | impulse add auth <name>")
	note("Options are added one at a time from a clean tree; each ends in a handoff brief for the")
	note("wiring the tool cannot do and a check that says when it is done.")
}

// goProcesses lists the Procfile processes that run no package manager: the emulator and
// the Go servers, which run before any browser workspace is installed.
func goProcesses(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "Procfile"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}

	var procs []string
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, command, ok := strings.Cut(line, ":")
		if !ok || packageManagerWord.MatchString(command) {
			continue
		}
		procs = append(procs, strings.TrimSpace(name))
	}

	return procs, nil
}

// webWorkspaces lists the rendered tree's Angular workspaces with each project's
// development ng serve port, read from angular.json.
func webWorkspaces(dir string) ([]webWorkspace, error) {
	a, err := app.Discover(dir)
	if err != nil {
		return nil, errors.Wrap(err, "app.Discover()")
	}

	workspaces := make([]webWorkspace, 0, len(a.WebApps))
	for _, w := range a.WebApps {
		projects, err := a.ReadAngular(w.Dir)
		if err != nil {
			return nil, err
		}
		ws := webWorkspace{Dir: w.Dir}
		for _, p := range projects {
			ws.Projects = append(ws.Projects, webProject{Name: p.Name, Port: p.DevPort})
		}
		workspaces = append(workspaces, ws)
	}
	sort.Slice(workspaces, func(i, j int) bool { return workspaces[i].Dir < workspaces[j].Dir })

	return workspaces, nil
}

// isTerminal reports whether w is a character device such as a terminal, where ANSI
// styling renders. NO_COLOR in the environment turns styling off regardless.
func isTerminal(w io.Writer) bool {
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}
