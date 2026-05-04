package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func newManCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "man",
		Short: "Generate or install man pages",
		Long: `man emits roff-formatted man pages for hasher and each subcommand.
Pages are generated on demand from the live cobra command tree, so they
always reflect the current binary's flags and help text.`,
	}

	cmd.AddCommand(newManGenerateCmd(), newManInstallCmd())

	return cmd
}

func newManGenerateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "generate <dir>",
		Short: "Write man pages into <dir>",
		Long: `Write hasher.1 and one .1 file per subcommand into <dir>.
The directory is created if it does not exist.`,
		Example: `  hasher man generate ./man`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return errors.Wrapf(err, "create %s", dir)
			}

			return generateManTree(cmd.Root(), dir)
		},
	}
}

func newManInstallCmd() *cobra.Command {
	var system bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install man pages to the user's man path",
		Long: `Install writes hasher's man pages to:

  $XDG_DATA_HOME/man/man1/   (default)
  /usr/local/share/man/man1/ (with --system; usually requires root)

After installing, you may need to run mandb (or the equivalent on your system)
for "man hasher" to find them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := filepath.Join(xdgDataHome(), "man", "man1")
			if system {
				dir = "/usr/local/share/man/man1"
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return errors.Wrapf(err, "create %s", dir)
			}
			if err := generateManTree(cmd.Root(), dir); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "installed man pages to %s\n", dir)
			fmt.Fprintln(cmd.OutOrStdout(), "you may need to run: mandb")

			return nil
		},
	}

	cmd.Flags().BoolVar(&system, "system", false, "install to /usr/local/share/man/man1 (requires root)")

	return cmd
}

func generateManTree(root *cobra.Command, dir string) error {
	header := &doc.GenManHeader{
		Title:   "HASHER",
		Section: "1",
		Source:  "ccc",
		Manual:  "User Commands",
	}
	if err := doc.GenManTree(root, header, dir); err != nil {
		return errors.Wrap(err, "generate man tree")
	}

	return nil
}
