// Package cli implements the hasher command.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cccteam/ccc/securehash"
	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	algoBcrypt = "bcrypt"
	algoArgon2 = "argon2"

	outputText = "text"
	outputJSON = "json"

	envPrefix = "HASHER"
)

// version is set at link time via -ldflags '-X .../cli.version=...'.
var version = "dev"

// ExitError carries a process exit code out of a cobra RunE.
// The verify subcommand uses code 1 for "no match" so callers can
// branch on it in scripts; main.go translates this into os.Exit.
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("exit %d", e.Code)
	}

	return e.Message
}

// NewRootCmd constructs the top-level `hasher` command and all subcommands.
// Each call returns a fresh tree so tests can run commands in isolation.
func NewRootCmd() *cobra.Command {
	v := viper.New()

	root := &cobra.Command{
		Use:   "hasher",
		Short: "Generate and verify password hashes (bcrypt / argon2)",
		Long: `hasher generates and verifies password hashes using the
github.com/cccteam/ccc/securehash library. It supports bcrypt and argon2id with
the library's recommended default parameters.

PASSWORD INPUT
  Passwords may be supplied via --password, piped on stdin, or entered at a
  hidden TTY prompt. Avoid --password on shared hosts: the value is recorded
  in shell history and visible in the process list.

ENVIRONMENT
  HASHER_ALGORITHM    default algorithm: bcrypt or argon2 (default: argon2)
  HASHER_OUTPUT       default output format: text or json (default: text)
  HASHER_CONFIG       path to a YAML config file

FILES
  $XDG_CONFIG_HOME/hasher/hasher.yaml
  $HOME/.config/hasher/hasher.yaml
    First file found is loaded. Keys: algorithm, output.

EXIT STATUS
  0   success
  1   verify mismatch, or general failure
  2   verify error (e.g. malformed hash, unreadable input)`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}

	root.PersistentFlags().String("algo", algoArgon2, "hash algorithm: bcrypt or argon2")
	root.PersistentFlags().String("output", outputText, "output format: text or json")
	root.PersistentFlags().String("config", "", "config file (default: $XDG_CONFIG_HOME/hasher/hasher.yaml)")

	if err := v.BindPFlag("algorithm", root.PersistentFlags().Lookup("algo")); err != nil {
		panic(err)
	}
	if err := v.BindPFlag("output", root.PersistentFlags().Lookup("output")); err != nil {
		panic(err)
	}
	if err := v.BindPFlag("config", root.PersistentFlags().Lookup("config")); err != nil {
		panic(err)
	}

	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	cobra.OnInitialize(func() { initConfig(v) })

	root.AddCommand(
		newHashCmd(v),
		newVerifyCmd(v),
		newCompletionCmd(),
		newManCmd(),
	)

	return root
}

func initConfig(v *viper.Viper) {
	if cfg := v.GetString("config"); cfg != "" {
		v.SetConfigFile(cfg)
	} else {
		v.SetConfigName("hasher")
		v.SetConfigType("yaml")
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			v.AddConfigPath(filepath.Join(xdg, "hasher"))
		}
		if home, err := os.UserHomeDir(); err == nil {
			v.AddConfigPath(filepath.Join(home, ".config", "hasher"))
		}
		v.AddConfigPath(".")
	}

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			fmt.Fprintln(os.Stderr, "warning: failed to read config:", err)
		}
	}
}

// newHasher builds a *securehash.SecureHasher from a viper-resolved algo.
// Only the library's default parameter sets are exposed.
func newHasher(v *viper.Viper) (*securehash.SecureHasher, error) {
	switch algo := strings.ToLower(v.GetString("algorithm")); algo {
	case algoArgon2, "":
		return securehash.New(securehash.Argon2()), nil
	case algoBcrypt:
		return securehash.New(securehash.Bcrypt()), nil
	default:
		return nil, errors.Newf("unknown algorithm %q (want %q or %q)", algo, algoBcrypt, algoArgon2)
	}
}
