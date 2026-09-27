// Package cli is the command tree of the bedrock tool.
package cli

import (
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
	"github.com/cccteam/ccc/bedrock/internal/secret"
	"github.com/cccteam/ccc/bedrock/internal/where"
	"github.com/cccteam/ccc/impulse/app"
)

// placementFile is the placement's default name, read beside the stack.
const placementFile = "placement.json"

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
		Version:       version(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newRender(d))
	root.AddCommand(newCheck(d))
	root.AddCommand(newDomain(d))
	root.AddCommand(newSecret(d))
	root.AddCommand(newRepository(d))
	root.AddCommand(newHotfix(d))
	root.AddCommand(newDeploy(d))

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

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "(devel)"
	}

	return info.Main.Version
}

// exitError carries a process exit code out of a command without printing anything.
type exitError struct {
	code int
}

func (e exitError) Error() string {
	return fmt.Sprintf("exit %d", e.code)
}

// model reads the application and the placement and derives the model. The placement is
// the file named, or placement.json beside the stack.
func model(appDir, placement, stackDir string) (*derive.Model, error) {
	if placement == "" {
		placement = filepath.Join(stackDir, placementFile)
	}
	p, err := derive.ReadPlacement(placement)
	if err != nil {
		return nil, err
	}
	a, err := app.Discover(appDir)
	if err != nil {
		return nil, err
	}

	return derive.Derive(a, p)
}

// asExit reports whether the error carries an exit code, and sets it.
func asExit(err error, target *exitError) bool {
	return errors.As(err, target)
}
