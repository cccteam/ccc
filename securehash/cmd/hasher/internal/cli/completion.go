package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
)

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate or install shell completion scripts",
		Long: `Cobra emits completion scripts for bash, zsh, fish, and powershell on stdout
via "hasher completion <shell>". The "install" subcommand below detects $SHELL,
generates the right script, and writes it to the conventional location for that
shell.`,
	}

	cmd.AddCommand(newCompletionInstallCmd())

	return cmd
}

func newCompletionInstallCmd() *cobra.Command {
	var (
		shell    string
		printOut bool
		force    bool
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the completion script for the current shell",
		Long: `Install detects $SHELL (or accepts --shell <name>) and writes the completion
script to the conventional XDG location:

  bash         $XDG_DATA_HOME/bash-completion/completions/hasher
  zsh          $XDG_DATA_HOME/zsh/site-functions/_hasher
  fish         $XDG_CONFIG_HOME/fish/completions/hasher.fish
  powershell   prints the script with install instructions

Use --print to write the script to stdout instead.`,
		Example: `  # Auto-detect shell, write to XDG location
  hasher completion install

  # Explicit shell, dump to stdout
  hasher completion install --shell zsh --print > _hasher`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			detected, err := resolveShell(shell)
			if err != nil {
				return err
			}

			var buf bytes.Buffer
			if err := writeCompletion(cmd.Root(), detected, &buf); err != nil {
				return err
			}

			if printOut || detected == "powershell" {
				if detected == "powershell" && !printOut {
					fmt.Fprintln(cmd.ErrOrStderr(),
						"powershell has no canonical install path; printing to stdout. "+
							"Append the output to $PROFILE to enable completion.")
				}
				_, err := cmd.OutOrStdout().Write(buf.Bytes())

				return err
			}

			path, err := completionInstallPath(detected)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return errors.Wrapf(err, "create %s", filepath.Dir(path))
			}
			if !force {
				if _, statErr := os.Stat(path); statErr == nil {
					return errors.Newf("%s already exists (use --force to overwrite)", path)
				}
			}
			if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
				return errors.Wrapf(err, "write %s", path)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "installed %s completion: %s\n", detected, path)
			if detected == "zsh" {
				fmt.Fprintln(cmd.OutOrStdout(),
					"note: ensure $XDG_DATA_HOME/zsh/site-functions is in your fpath, e.g. add to ~/.zshrc:")
				fmt.Fprintln(cmd.OutOrStdout(),
					"  fpath=(\"${XDG_DATA_HOME:-$HOME/.local/share}/zsh/site-functions\" $fpath)")
				fmt.Fprintln(cmd.OutOrStdout(), "  autoload -U compinit && compinit")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&shell, "shell", "", "shell name (bash, zsh, fish, powershell); default: auto-detect from $SHELL")
	cmd.Flags().BoolVar(&printOut, "print", false, "print the script to stdout instead of installing")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing completion file")

	return cmd
}

func resolveShell(explicit string) (string, error) {
	if explicit != "" {
		return normalizeShell(explicit)
	}
	if env := os.Getenv("SHELL"); env != "" {
		return normalizeShell(filepath.Base(env))
	}

	return "", errors.New("could not detect shell from $SHELL; pass --shell")
}

func normalizeShell(s string) (string, error) {
	switch strings.ToLower(s) {
	case "bash":
		return "bash", nil
	case "zsh":
		return "zsh", nil
	case "fish":
		return "fish", nil
	case "powershell", "pwsh":
		return "powershell", nil
	default:
		return "", errors.Newf("unsupported shell %q (want bash, zsh, fish, or powershell)", s)
	}
}

func writeCompletion(root *cobra.Command, shell string, w *bytes.Buffer) error {
	switch shell {
	case "bash":
		return root.GenBashCompletionV2(w, true)
	case "zsh":
		return root.GenZshCompletion(w)
	case "fish":
		return root.GenFishCompletion(w, true)
	case "powershell":
		return root.GenPowerShellCompletionWithDesc(w)
	}

	return errors.Newf("unsupported shell %q", shell)
}

func completionInstallPath(shell string) (string, error) {
	switch shell {
	case "bash":
		return filepath.Join(xdgDataHome(), "bash-completion", "completions", "hasher"), nil
	case "zsh":
		return filepath.Join(xdgDataHome(), "zsh", "site-functions", "_hasher"), nil
	case "fish":
		return filepath.Join(xdgConfigHome(), "fish", "completions", "hasher.fish"), nil
	}

	return "", errors.Newf("no install path for shell %q", shell)
}

func xdgDataHome() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share")
	}

	return ".local/share"
}

func xdgConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config")
	}

	return ".config"
}
