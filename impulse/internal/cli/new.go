package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/cccteam/ccc/impulse/internal/names"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

func newNew() *cobra.Command {
	var (
		modulePath string
		authName   string
		devRoot    string
		skipGit    bool
	)

	cmd := &cobra.Command{
		Use:   "new <dir>",
		Short: "Create an Impulse application: the base skeleton under your module path, its first auth named",
		Long: `new renders the base skeleton (flat layout, one auth, nothing else on) into a new or empty
directory under the module path you name, and names the application's first auth: the
population that signs in, as a lowercase plural (staff, members, partners, devices). The
auth is a package, pkg/auth/<name>, and its name is also the prefix of its tables, its
cookie, and the stem of its roles file, so it is asked for when --auth is not given, and
there is no default: a default word would land in every application whose author skipped
the question.

The rendered tree is committed as the application's first commit (--skip-git leaves it
uncommitted), so impulse add can start from a clean tree. With --dev-root, new also writes
a go.work that uses the framework checkouts under that directory (laid out by repository:
<root>/ccc/resource, <root>/session, ...); it is ignored by git and never committed.

Options are added afterwards, each as one reviewable change: impulse add tenancy, impulse
add outlet <name>, impulse add auth <name>.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reserved, err := skeleton.Reserved(skeleton.Base)
			if err != nil {
				return err
			}
			if authName == "" {
				authName, err = askAuthName(cmd)
				if err != nil {
					return err
				}
			}
			if err := names.ValidateAuth(authName, reserved); err != nil {
				return err
			}

			dir := args[0]
			got, err := skeleton.Render(&skeleton.Options{Candidate: skeleton.Base, Dir: dir, ModulePath: modulePath, DevRoot: devRoot, Auth: authName})
			if err != nil {
				return err
			}
			gitNote := ""
			if !skipGit {
				gitNote = initRepo(cmd.Context(), dir, modulePath, authName)
			}

			port, emulator := templatePorts(dir)
			goProcs, err := goProcesses(dir)
			if err != nil {
				return err
			}
			web, err := webWorkspaces(dir)
			if err != nil {
				return err
			}
			report := renderReport{
				headline: fmt.Sprintf("Created %s at %s with the %s auth (%d files).", modulePath, dir, authName, got.Files),
				gitNote:  gitNote, options: true,
				candidate: skeleton.Base, dir: dir, modulePath: modulePath, devRoot: devRoot,
				rendered: got, port: port, emulator: emulator, goProcs: goProcs, web: web,
				styled: isTerminal(cmd.OutOrStdout()),
			}
			report.write(cmd.OutOrStdout())

			return nil
		},
	}

	cmd.Flags().StringVar(&modulePath, "module", "", "module path of the application (required)")
	cmd.Flags().StringVar(&authName, "auth", "", "the first auth's name: "+names.Guidance+" (asked when not given; no default)")
	cmd.Flags().StringVar(&devRoot, "dev-root", "", "directory of cccteam checkouts to build against instead of the pins")
	cmd.Flags().BoolVar(&skipGit, "skip-git", false, "do not initialize a git repository and make the first commit")
	_ = cmd.MarkFlagRequired("module")

	return cmd
}

// authQuestion is what a person at the terminal is asked when --auth is not given.
const authQuestion = `Name the application's first auth: the population that signs in, plural, lowercase, one
word (staff, members, partners, devices). It names the package pkg/auth/<name>, the
tables, the cookie, and the roles file. There is no default.
Auth name: `

// askAuthName asks for the first auth's name when the flag did not give it and a person is
// at the terminal. Without a terminal the flag is required.
func askAuthName(cmd *cobra.Command) (string, error) {
	if !stdinIsTerminal() {
		return "", errors.New("--auth is required: name the application's first auth, " + names.Guidance + ". There is no default")
	}
	fmt.Fprint(cmd.ErrOrStderr(), authQuestion)
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.Wrap(err, "reading the answer")
	}

	return strings.TrimSpace(line), nil
}

// stdinIsTerminal reports whether a person is at the terminal to answer a question.
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// initRepo makes the rendered tree a git repository with its first commit, so impulse add
// starts from a clean tree. It returns a note for the report when a step fails: a missing
// git or an unset identity is the person's to sort out, not a reason to undo the render.
func initRepo(ctx context.Context, dir, modulePath, authName string) string {
	steps := [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"commit", "-q", "-m", fmt.Sprintf("impulse new: %s with the %s auth", modulePath, authName)},
	}
	for _, step := range steps {
		cmd := exec.CommandContext(ctx, "git", step...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Sprintf("git %s failed (%v): %s. Commit the tree yourself before impulse add.", step[0], err, strings.TrimSpace(string(out)))
		}
	}

	return "Committed the tree as the application's first commit."
}
