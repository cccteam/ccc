// Package cli is the command tree of the bedrock tool.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/domain"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/prompt"
	"github.com/cccteam/ccc/bedrock/internal/release"
	"github.com/cccteam/ccc/bedrock/internal/secret"
	"github.com/cccteam/ccc/bedrock/internal/where"
	"github.com/cccteam/ccc/impulse/app"
)

// placementFile is the placement's default name, read beside the stack.
const placementFile = "placement.json"

// develVersion is the version of a build Go could not stamp: nobody's release.
const develVersion = "(devel)"

// Main runs the tool with the arguments and returns the process exit code.
func Main(args []string) int {
	root := newRoot()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var exit exitError
		if asExit(err, &exit) {
			return exit.code
		}
		// The cause carries the message written for the user; the chain above it holds
		// source positions meant for a developer of the tool.
		fmt.Fprintln(os.Stderr, "bedrock:", errors.Cause(err))

		return 2
	}

	return 0
}

// deps are the seams the command tree reaches the world through: the clients it opens,
// where the discovery of the repository starts, and whether a person is at the terminal
// to be asked. newRoot wires the real ones; tests pass fakes.
type deps struct {
	// domains opens Cloud Domains, secrets Secret Manager, and projects Cloud Resource
	// Manager.
	domains  domain.ClientFunc
	secrets  secret.ClientFunc
	projects where.ProjectClientFunc
	// github opens the GitHub API client with the token found for the account.
	github github.ClientFunc
	// cwd is where the repository is looked for when --dir is not given; empty means
	// the process's working directory.
	cwd string
	// interactive reports whether standard input is a terminal a person can be asked
	// on.
	interactive func() bool
	// readSecret asks at the terminal for a value nobody should see and reads it
	// without echo, after printing the question on the writer.
	readSecret func(w io.Writer, question string) ([]byte, error)
	// deploy holds what the deploy sequence's commands open: Cloud Storage for the
	// records, Cloud Build for a build's own description and its GitHub token.
	deploy *deploy.Clients
	// releases is where bedrock's own releases and commits are read from, for upgrade;
	// version is the running bedrock, its version and how it was built, which render and
	// check hold against the placement's pin (nil: (devel), nobody's release or commit);
	// cacheDir is where upgrade keeps the bedrock it fetched or installed (empty: the
	// user's cache directory); install builds a commit pin with go install into a
	// directory, for upgrade (nil: the real go install).
	releases func() *release.Source
	version  func() build
	cacheDir string
	install  func(ctx context.Context, version, dir string) error
}

func newRoot() *cobra.Command {
	return newRootWith(deps{
		domains:     domain.NewCloudDomains,
		secrets:     secret.NewSecretManager,
		projects:    where.NewProjects,
		github:      github.Open,
		interactive: stdinIsTerminal,
		readSecret:  readHidden,
		deploy:      deploy.DefaultClients(),
		releases:    release.GitHub,
		version:     version,
	})
}

// newRootWith builds the command tree over the seams, so tests can pass fakes. The
// completion command cobra adds by default (bedrock completion <shell>) stays on: the
// positional arguments of secret pin complete through the same discovery the command
// runs.
func newRootWith(d deps) *cobra.Command {
	root := &cobra.Command{
		Use:   "bedrock",
		Short: "The infrastructure companion of impulse",
		Long: `bedrock derives what an Impulse application needs from the application's own code, through
impulse's reader, and renders the application stack that provisions it. It keeps no record of
its own: the code and a placement are the inputs, the stack is the output.`,
		Version:       d.running().version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newRender(d))
	root.AddCommand(newCheck(d))
	root.AddCommand(newUpgrade(d))
	root.AddCommand(newDomain(d))
	root.AddCommand(newSecret(d))
	root.AddCommand(newHotfix(d))
	root.AddCommand(newRestore(d))
	root.AddCommand(newDeploy(d))
	root.AddCommand(newOrg(d))
	root.AddCommand(newMigration(d))

	return root
}

// stdinIsTerminal reports whether standard input is a terminal: a character device, not
// a pipe or a file.
func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}

// infrastructureRoot is the directory holding the layers: --dir as given, else the one
// found from the working directory (the repository root, or its infrastructure
// directory). appDir is the application's source directory when the layout says where
// it is (the repository root, with the infrastructure in its infrastructure directory),
// else empty.
func (d deps) infrastructureRoot(dir string) (infraRoot, appDir string, err error) {
	if dir != "" {
		return dir, applicationDirOf(dir), nil
	}
	start := d.cwd
	if start == "" {
		start, err = os.Getwd()
		if err != nil {
			return "", "", errors.Wrap(err, "os.Getwd()")
		}
	}
	repoRoot, err := where.RepoRoot(start)
	if err != nil {
		return "", "", err
	}
	infraRoot, err = where.InfrastructureRoot(repoRoot)
	if err != nil {
		return "", "", err
	}

	return infraRoot, where.ApplicationDir(repoRoot, infraRoot), nil
}

// applicationDirOf is the application's source directory for an infrastructure root
// given by hand: known when the root sits in a repository whose layout says where the
// code is, else empty.
func applicationDirOf(infraRoot string) string {
	repoRoot, err := where.RepoRoot(infraRoot)
	if err != nil {
		return ""
	}

	return where.ApplicationDir(repoRoot, infraRoot)
}

// stack is where render and check work. The stack directory is the flag (--out, --dir)
// as given, else the one application's layer under the infrastructure root found from
// the working directory: the application repository's infrastructure directory, or the
// one layer under 3-app. The application's source directory is --app as given, else the
// one the layout around the working directory knows (the repository root, with the
// stack in its infrastructure directory), else the working directory itself.
func (d deps) stack(appFlag, dirFlag string) (appDir, stackDir string, err error) {
	if appFlag != "" && dirFlag != "" {
		return appFlag, dirFlag, nil
	}
	infraRoot, layoutAppDir, err := d.infrastructureRoot("")
	stackDir = dirFlag
	if stackDir == "" {
		if err != nil {
			return "", "", err
		}
		application, err := where.SingleApplication(infraRoot)
		if err != nil {
			return "", "", err
		}
		stackDir = where.LayerDir(infraRoot, application)
	}
	appDir = appFlag
	if appDir == "" {
		appDir = layoutAppDir
	}
	if appDir == "" {
		appDir = "."
	}

	return appDir, stackDir, nil
}

// asker is the prompter that asks a person on the command's terminal, or nil when
// standard input is not a terminal.
func (d deps) asker(cmd *cobra.Command) *prompt.Prompter {
	if !d.interactive() {
		return nil
	}

	return prompt.New(cmd.InOrStdin(), cmd.OutOrStdout())
}

// argument is the positional argument at index i: the one given, else the one chosen at
// the prompt from the choices, else, with no terminal to ask on, refused with the
// choices listed. what names the argument in that refusal and question asks for it.
func argument(args []string, i int, ask *prompt.Prompter, what, question string, choices func() ([]prompt.Choice, error)) (string, error) {
	if i < len(args) {
		return args[i], nil
	}
	list, err := choices()
	if err != nil {
		return "", err
	}
	if ask == nil {
		return "", errors.Newf("no %s given and no terminal to ask on: pass one of %s", what, choiceList(list))
	}

	return ask.Choose(question, list)
}

// choiceList names the choices in a message: each value, with its note in parentheses
// when it has one, comma separated.
func choiceList(choices []prompt.Choice) string {
	names := make([]string, 0, len(choices))
	for _, c := range choices {
		name := c.Value
		if c.Note != "" {
			name += " (" + c.Note + ")"
		}
		names = append(names, name)
	}

	return strings.Join(names, ", ")
}

// plain makes choices of values with no note, for a list of names.
func plain(names []string, err error) ([]prompt.Choice, error) {
	if err != nil {
		return nil, err
	}
	choices := make([]prompt.Choice, 0, len(names))
	for _, name := range names {
		choices = append(choices, prompt.Choice{Value: name})
	}

	return choices, nil
}

// releaseVersion is the version a release build stamps at link time
// (-X github.com/cccteam/ccc/bedrock/internal/cli.releaseVersion=v0.4.0, in ccc's release
// workflow, which then checks what --version prints). Go stamps the same version from the
// tag itself, bedrock/v0.4.0, on a build from a clean checkout at that tag, so the stamp
// repeats what the build info says; it is kept so a release binary states its version
// however it was built. Empty in any other build.
var releaseVersion string

// buildKind is how the running bedrock was built, which decides the pins it may render.
type buildKind int

const (
	// buildDevel is a build Go could not stamp, (devel): nobody's release or commit.
	buildDevel buildKind = iota
	// buildCheckout is go build in a checkout: Go stamps the commit's pseudo-version (with
	// +dirty over uncommitted changes), or the release's version at a commit a release
	// tag names. The build info carries the vcs settings and no module sum.
	buildCheckout
	// buildInstalled is go install at a version, through the module proxy: the build info
	// carries the module's sum.
	buildInstalled
	// buildStamped is a release build of ccc's release workflow, stamped at link time.
	buildStamped
)

// build is the running bedrock: its version and how it was built.
type build struct {
	version string
	kind    buildKind
}

// buildOf reads the running bedrock off the version stamped at link time and the build
// info Go recorded (nil when the binary carries none).
func buildOf(stamped string, info *debug.BuildInfo) build {
	if stamped != "" {
		return build{version: stamped, kind: buildStamped}
	}
	if info == nil || info.Main.Version == "" || info.Main.Version == develVersion {
		return build{version: develVersion, kind: buildDevel}
	}
	if info.Main.Sum != "" {
		return build{version: info.Main.Version, kind: buildInstalled}
	}

	return build{version: info.Main.Version, kind: buildCheckout}
}

// version is the running bedrock, as this binary was built. ReadBuildInfo answers nil
// for a binary that carries no build info.
func version() build {
	info, _ := debug.ReadBuildInfo()

	return buildOf(releaseVersion, info)
}

// heldToPin reports whether the build must be the one the placement pins to render
// against it: a release, however it was built, and a commit installed with go install
// are someone's pin; a build from a checkout at any other commit, dirty or clean, and
// (devel) are nobody's and render any pin.
func (b build) heldToPin() bool {
	return release.IsVersion(b.version) || (b.kind == buildInstalled && release.IsCommitPin(b.version))
}

// running is the running bedrock through the seam; a test without one is (devel).
func (d deps) running() build {
	if d.version == nil {
		return build{version: develVersion, kind: buildDevel}
	}

	return d.version()
}

// exitError carries a process exit code out of a command without printing anything.
type exitError struct {
	code int
}

func (e exitError) Error() string {
	return fmt.Sprintf("exit %d", e.code)
}

// model reads the application and the placement and derives the model. The placement is
// the file named, or placement.json beside the stack, and it must pin the bedrock
// running: the rendered files say which bedrock they come from through that pin.
func (d deps) model(appDir, placement, stackDir string) (*derive.Model, error) {
	if placement == "" {
		placement = filepath.Join(stackDir, placementFile)
	}
	p, err := derive.ReadPlacement(placement)
	if err != nil {
		return nil, err
	}
	if err := d.pinned(p, placement); err != nil {
		return nil, err
	}
	a, err := app.Discover(appDir)
	if err != nil {
		return nil, err
	}

	return derive.Derive(a, p)
}

// pinned refuses a placement that pins no bedrock, and one that pins another bedrock
// than the one running when that one is held to its pin (a release, or a commit installed
// with go install): the pipeline and the infrastructure check run the pinned one, so the
// committed files must come from it. A build from a checkout, at any commit, and (devel)
// are nobody's pin and render any pin.
func (d deps) pinned(p *derive.Placement, placement string) error {
	if !p.Pinned() {
		return errors.Newf("%s pins no bedrock (bedrockVersion): run bedrock upgrade", placement)
	}
	running := d.running()
	if !running.heldToPin() || running.version == p.BedrockVersion {
		return nil
	}
	install := release.Page(p.BedrockVersion)
	if release.IsCommitPin(p.BedrockVersion) {
		install = "go install " + release.Module + "@" + p.BedrockVersion
	}

	return errors.Newf("%s pins bedrock %s and this is bedrock %s: install the pinned one (%s) or move the pin (bedrock upgrade)", placement, p.BedrockVersion, running.version, install)
}

// asExit reports whether the error carries an exit code, and sets it.
func asExit(err error, target *exitError) bool {
	return errors.As(err, target)
}
