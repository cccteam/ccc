package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/cccteam/ccc/securehash"
	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

// Exit codes for `hasher verify`. Match (0) is the absence of an ExitError.
const (
	exitMismatch  = 1
	exitVerifyErr = 2
)

func newVerifyCmd(v *viper.Viper) *cobra.Command {
	var (
		password string
		hashFlag string
		hashFile string
	)

	cmd := &cobra.Command{
		Use:   "verify [hash]",
		Short: "Verify a password against a hash",
		Long: `Verify checks whether a password matches a previously generated hash.

The hash may be supplied as a positional argument, with --hash, with --hash-file,
or piped on stdin (when no positional or file is given).

The algorithm is inferred from the hash prefix; --algo is only used to decide
whether to report "rehash recommended" against the current default parameters.

When the hash is piped on stdin, the password must be supplied via --password.
When the hash is given another way, the password may be piped on stdin.

Exit codes:
  0  match
  1  no match
  2  verify error (e.g. malformed hash, unreadable input)`,
		Example: `  # Verify with prompted password
  hasher verify '$3$1$1$Wj…'

  # Hash from file, password piped
  printf '%s' 'hunter2' | hasher verify --hash-file ./creds/admin.hash

  # Hash piped, password via flag
  cat admin.hash | hasher verify --password 'hunter2'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := newHasher(v)
			if err != nil {
				return &ExitError{Code: exitVerifyErr, Message: err.Error()}
			}

			hashBytes, hashConsumedStdin, err := readHash(args, hashFlag, hashFile)
			if err != nil {
				return &ExitError{Code: exitVerifyErr, Message: err.Error()}
			}

			parsed := &securehash.Hash{}
			if err := parsed.UnmarshalText(bytes.TrimSpace(hashBytes)); err != nil {
				return &ExitError{Code: exitVerifyErr, Message: errors.Wrap(err, "parse hash").Error()}
			}

			ps := defaultPasswordSource()
			ps.flagValue = password
			if hashConsumedStdin {
				if password == "" {
					return &ExitError{Code: exitVerifyErr, Message: "hash piped on stdin: password must be supplied via --password"}
				}
				ps.stdin = nil
			}
			pw, err := ps.readPassword()
			if err != nil {
				return &ExitError{Code: exitVerifyErr, Message: err.Error()}
			}

			needsUpgrade, err := h.Compare(parsed, string(pw))
			out := cmd.OutOrStdout()
			outputFmt := v.GetString("output")

			if err != nil {
				if outputFmt == outputJSON {
					_ = json.NewEncoder(out).Encode(verifyJSON{Match: false})
				} else {
					fmt.Fprintln(out, "no match")
				}

				return &ExitError{Code: exitMismatch}
			}

			if outputFmt == outputJSON {
				if encErr := json.NewEncoder(out).Encode(verifyJSON{Match: true, RehashRecommended: needsUpgrade}); encErr != nil {
					return &ExitError{Code: exitVerifyErr, Message: encErr.Error()}
				}
			} else if needsUpgrade {
				fmt.Fprintln(out, "match (rehash recommended)")
			} else {
				fmt.Fprintln(out, "match")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&password, "password", "p", "",
		"password (insecure: visible in shell history)")
	cmd.Flags().StringVar(&hashFlag, "hash", "", "hash string")
	cmd.Flags().StringVar(&hashFile, "hash-file", "", "path to a file containing the hash")

	return cmd
}

type verifyJSON struct {
	Match             bool `json:"match"`
	RehashRecommended bool `json:"rehashRecommended"`
}

// readHash returns (bytes, consumedStdin, error). Precedence: positional arg
// > --hash > --hash-file > piped stdin. If stdin is a TTY and no other source
// was supplied, this is an error: prompting for a hash makes no sense.
func readHash(args []string, hashFlag, hashFile string) ([]byte, bool, error) {
	if len(args) == 1 {
		return []byte(args[0]), false, nil
	}
	if hashFlag != "" {
		return []byte(hashFlag), false, nil
	}
	if hashFile != "" {
		b, err := os.ReadFile(hashFile)
		if err != nil {
			return nil, false, errors.Wrapf(err, "read %s", hashFile)
		}

		return b, false, nil
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, false, errors.New("no hash provided (use a positional arg, --hash, --hash-file, or pipe one on stdin)")
	}

	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, false, errors.Wrap(err, "read hash from stdin")
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, false, errors.New("empty hash on stdin")
	}

	return b, true, nil
}
