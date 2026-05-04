package cli

import (
	"encoding/json"
	"fmt"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newHashCmd(v *viper.Viper) *cobra.Command {
	var (
		password  string
		noConfirm bool
	)

	cmd := &cobra.Command{
		Use:   "hash",
		Short: "Hash a password",
		Long: `Hash reads a password and prints its hashed form.

The password is taken from --password if set, otherwise from a piped stdin,
otherwise from a hidden TTY prompt (confirmed by default).`,
		Example: `  # Interactive prompt (recommended)
  hasher hash

  # Pipe from stdin
  printf '%s' 'hunter2' | hasher hash --algo bcrypt

  # JSON output
  hasher hash --output json <<< 'hunter2'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := newHasher(v)
			if err != nil {
				return err
			}

			ps := defaultPasswordSource()
			ps.flagValue = password
			ps.confirm = !noConfirm
			pw, err := ps.readPassword()
			if err != nil {
				return err
			}

			result, err := h.Hash(string(pw))
			if err != nil {
				return errors.Wrap(err, "hash")
			}

			text, err := result.MarshalText()
			if err != nil {
				return errors.Wrap(err, "marshal hash")
			}

			out := cmd.OutOrStdout()
			switch v.GetString("output") {
			case outputJSON:
				return json.NewEncoder(out).Encode(struct {
					Algorithm string `json:"algorithm"`
					Hash      string `json:"hash"`
				}{Algorithm: result.KeyType(), Hash: string(text)})
			default:
				_, err := fmt.Fprintln(out, string(text))

				return err
			}
		},
	}

	cmd.Flags().StringVarP(&password, "password", "p", "",
		"password (insecure: visible in shell history)")
	cmd.Flags().BoolVar(&noConfirm, "no-confirm", false,
		"skip the confirmation prompt at TTY")

	return cmd
}
