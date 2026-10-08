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

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/ci"
	"github.com/cccteam/ccc/impulse/internal/names"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

func newRender() *cobra.Command {
	var (
		modulePath string
		name       string
		devRoot    string
	)

	cmd := &cobra.Command{
		Use:   "render [<candidate> <dir>]",
		Short: "Write the owned files from the code, or render one embedded skeleton into a directory (developer form)",
		Long: `render has two forms; the arguments decide which.

With no arguments, inside an application (a go.mod at the working directory), render writes
the files impulse owns from the application's code: today the CI workflows,
.github/workflows/ci.yml, with one browser job per workspace, the //impulse:ci line's
choices and the action and tool pins this impulse carries, .github/workflows/ci-cache.yml
beside it, and .github/workflows/security-scan.yml, the daily vulnerability check and image
scan of the default branch and the latest release. impulse check compares the committed
files with the same rendering and fails a hand edit, so this is the command that brings
them back into agreement after a change to the code, and the second step of moving the
impulse pin: go get -tool github.com/cccteam/ccc/impulse@<version>, then go tool impulse
render, then go tool impulse check.

With a candidate and a directory, render copies one of the embedded skeleton templates into
a new or empty directory under the module path --module names, rewriting every import and
go.mod to it. It is the primitive impulse new builds on, and the way the templates are
validated: render one, then build, test, and check the result. The placeholder auth (staff)
is kept; new renames it. The application is named after the module path's last segment
unless --name says otherwise.

With --dev-root, that form also writes a go.work that uses every framework module the
application requires directly and that has a checkout under that directory (laid out by
repository: <root>/ccc/resource, <root>/session, ...), so the application builds against
local framework work instead of the pins. Do not commit that go.work.`,
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch len(args) {
			case 0:
				return renderOwned(cmd.OutOrStdout(), ".")
			case 1:
				return errors.Newf("render takes no arguments inside an application (it writes the owned files from the code), or a candidate and a directory (impulse render <candidate> <dir> --module <path>); %q alone is neither", args[0])
			}
			if modulePath == "" {
				return errors.New("--module is required to render a candidate: the module path of the rendered application")
			}
			appName, err := appName(name, modulePath)
			if err != nil {
				return err
			}
			got, err := skeleton.Render(&skeleton.Options{Candidate: args[0], Dir: args[1], ModulePath: modulePath, Name: appName, DevRoot: devRoot})
			if err != nil {
				return err
			}

			port, emulator, firestore := templatePorts(args[1])
			goProcs, err := goProcesses(args[1])
			if err != nil {
				return err
			}
			web, err := webWorkspaces(args[1])
			if err != nil {
				return err
			}
			report := renderReport{
				candidate: args[0], dir: args[1], modulePath: modulePath, name: appName, devRoot: devRoot,
				rendered: got, port: port, emulator: emulator, firestore: firestore, goProcs: goProcs, web: web,
				styled: isTerminal(cmd.OutOrStdout()),
			}
			report.write(cmd.OutOrStdout())

			return nil
		},
	}

	cmd.Flags().StringVar(&modulePath, "module", "", "module path of the rendered application (required with a candidate)")
	cmd.Flags().StringVar(&name, "name", "", "the application's name: "+nameUse+" (default: the module path's last segment)")
	cmd.Flags().StringVar(&devRoot, "dev-root", "", "directory of cccteam checkouts to build against instead of the pins")

	return cmd
}

// renderOwned writes the files impulse owns from the code of the application at dir, and
// says for each whether it was written or already read as the code renders.
func renderOwned(w io.Writer, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, goModFile)); err != nil {
		return errors.Newf("no go.mod at %s: run render with no arguments from an application root, or name a candidate and a directory to render a skeleton (impulse render <candidate> <dir> --module <path>)", dir)
	}
	a, err := app.Discover(dir)
	if err != nil {
		return err
	}
	outcome, err := ci.Write(a)
	if err != nil {
		return err
	}
	states := make([]string, 0, len(outcome.Files))
	for _, f := range outcome.Files {
		state := "unchanged"
		if f.Written {
			state = "written"
		}
		states = append(states, f.File+" "+state)
	}
	fmt.Fprintf(w, "Rendered the owned files from the code: %s.\n", strings.Join(states, ", "))

	return nil
}

var (
	portLine      = regexp.MustCompile(`(?m)^export PORT=(\d+)`)
	emulatorLine  = regexp.MustCompile(`(?m)^export SPANNER_EMULATOR_PORT=(\d+)`)
	firestoreLine = regexp.MustCompile(`(?m)^export FIRESTORE_EMULATOR_PORT=(\d+)`)
	// packageManagerWord marks a Procfile command as a browser-app process.
	packageManagerWord = regexp.MustCompile(`(?:^|[\s;&|(])(?:npm|npx|bun|bunx|yarn|pnpm)\s`)
)

// templatePorts reads the server and emulator ports the rendered application's
// development environment template names, or empty strings when it has none. A taken
// port is the first thing a fresh application trips on, and the failure hides in a
// process pane, so the ports are worth stating up front.
func templatePorts(dir string) (port, emulator, firestore string) {
	data, err := os.ReadFile(filepath.Join(dir, ".envrc.template"))
	if err != nil {
		return "", "", ""
	}
	if m := portLine.FindSubmatch(data); m != nil {
		port = string(m[1])
	}
	if m := emulatorLine.FindSubmatch(data); m != nil {
		emulator = string(m[1])
	}
	if m := firestoreLine.FindSubmatch(data); m != nil {
		firestore = string(m[1])
	}

	return port, emulator, firestore
}

// renderReport is what the render command tells the user: what it did in a few lines,
// then the numbered steps to a running application. The steps are the part a reader
// acts on, so they carry the emphasis when the output is a terminal.
type renderReport struct {
	// headline replaces the "Rendered ..." line when set (impulse new); gitNote reports
	// the first commit; options adds the step that names the options to add next.
	headline, gitNote string
	options           bool
	// pin is the version impulse new pinned the impulse tool at, when it pinned one.
	pin string

	candidate, dir, modulePath, devRoot string
	// name is the application's name when the rendering set one.
	name     string
	rendered *skeleton.Rendered
	// port, emulator and firestore are the server's, the Spanner emulator's and the
	// Firestore emulator's ports from the environment template; firestore is empty for
	// a template that starts no Firestore emulator.
	port, emulator, firestore string
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
	// Path is where the dev server serves the project, its servePath with a trailing
	// slash: / for a project at the root, /console/ for one under a mount path.
	Path string
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
	if r.name != "" {
		fmt.Fprintf(w, "Named %s: the web package, APP_SERVICE_NAME, and the development database carry it.\n", r.name)
	}
	if r.pin != "" {
		fmt.Fprintf(w, "Pinned the impulse tool in go.mod at %s, the impulse that created the application, so CI's go tool impulse check runs the same code.\n", r.pin)
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
	r.writePorts(w, bold)

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
				urls = append(urls, fmt.Sprintf("%s at http://127.0.0.1:%d%s", p.Name, p.Port, p.Path))
			}
		}
	}
	if len(urls) > 0 {
		note("Everything, with ng serve for the %s.", strings.Join(urls, " and the "))
	}
	r.optionsStep(next, note)
}

// writePorts states the development ports the environment template names: the server's
// and the Spanner emulator's, and the Firestore emulator's when the template starts one.
// Nothing is written for a template that names no server port.
func (r *renderReport) writePorts(w io.Writer, bold func(string) string) {
	switch {
	case r.port != "" && r.firestore != "":
		fmt.Fprintf(w, "\n%s the server listens on :%s, the Spanner emulator on :%s and the Firestore emulator on :%s.\n", bold("Ports:"), r.port, r.emulator, r.firestore)
		fmt.Fprintf(w, "       All three are set in .envrc.template; change them there if one is taken.\n")
	case r.port != "":
		fmt.Fprintf(w, "\n%s the server listens on :%s and the Spanner emulator on :%s.\n", bold("Ports:"), r.port, r.emulator)
		fmt.Fprintf(w, "       Both are set in .envrc.template; change them there if either is taken.\n")
	}
}

// optionsStep names the options an application takes on afterwards, when the report is
// for a new application.
func (r *renderReport) optionsStep(next, note func(format string, args ...any)) {
	if !r.options {
		return
	}
	next("impulse add tenancy | impulse add outlet <name> --prefix <p> --auth <auth> | impulse add auth <name>")
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
		for i := range projects {
			p := &projects[i]
			ws.Projects = append(ws.Projects, webProject{Name: p.Name, Port: p.DevPort, Path: servedPath(p.ServePath)})
		}
		workspaces = append(workspaces, ws)
	}
	sort.Slice(workspaces, func(i, j int) bool { return workspaces[i].Dir < workspaces[j].Dir })

	return workspaces, nil
}

// servedPath is the browser path a dev server answers at: its servePath with a trailing
// slash, / when it names none.
func servedPath(servePath string) string {
	trimmed := strings.Trim(servePath, "/")
	if trimmed == "" {
		return "/"
	}

	return "/" + trimmed + "/"
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

// nameUse says what the application's name reaches, for the flag help of new and render.
const nameUse = "the web package (<name>-web), APP_SERVICE_NAME, and the development database"

// appName settles the application's name: the --name flag when given, else the module
// path's last segment, which is what a module is named after. A derived name that is not
// one (example.com/acme/beacon.service) asks for the flag rather than guessing.
func appName(flag, modulePath string) (string, error) {
	if flag != "" {
		return flag, names.ValidateApp(flag)
	}
	name := names.App(modulePath)
	if err := names.ValidateApp(name); err != nil {
		return "", errors.Newf("the module path %s does not end in an application name (%q is not one: %s); pass --name", modulePath, name, names.AppGuidance)
	}

	return name, nil
}
