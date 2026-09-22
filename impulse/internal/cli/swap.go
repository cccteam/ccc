package cli

import (
	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/impulse/internal/handoff"
	transition_ "github.com/cccteam/ccc/impulse/internal/transition"
)

func newSwap() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "swap",
		Short: "Change an option's value in place: files and seam edits, then a handoff for the wiring",
		Long: `swap changes the value of an option the application already has, the way add enables
one: the deterministic half is made and staged, impulse check runs, and the failing checks
are handed to an agent with a brief at ` + handoff.File + `.`,
	}
	cmd.AddCommand(newSwapAuth())

	return cmd
}

func newSwapAuth() *cobra.Command {
	var (
		f          transitionFlags
		oidcAzure  bool
		oidcGoogle bool
		authority  string
		carryRoles bool
	)

	cmd := &cobra.Command{
		Use:   "auth <name>",
		Short: "Move an auth to a directory: its people sign in over OpenID Connect instead of a password",
		Long: `auth changes how an existing auth's people sign in. The auth keeps its name, its permission
store, its roles file, and the surfaces bound to it; its package is rewritten from the
reference OIDC auth (--oidc-azure or --oidc-google), one migration drops its session tables
and creates them in the new shape, the data level's construction gains the directory
registration (APP_<NAME>_OIDC_* variables), the Procfile builds with the session library's
skipAuth tag, and the App's and router's handler types and the login route are swapped where
they stand in the base's shape. --authority says who owns role membership afterwards and is
asked when not given, as for add auth.

Everyone in the auth signs in again. Its role assignments are dropped, since they were keyed
by password usernames the directory need not present; --carry-roles keeps them when the
usernames were already the names the directory presents. The bootstrap, the login page, the
harness login helper, and the removal of password user management are handed to the agent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var flavor string
			switch {
			case oidcAzure && oidcGoogle:
				return errors.New("--oidc-azure and --oidc-google are two flavors; pick one")
			case oidcAzure:
				flavor = transition_.FlavorOIDCAzure
			case oidcGoogle:
				flavor = transition_.FlavorOIDCGoogle
			default:
				return errors.New("swap auth needs the flavor to move to: --oidc-azure or --oidc-google")
			}
			if authority == "" {
				answer, err := askAuthority(cmd)
				if err != nil {
					return err
				}
				authority = answer
			}

			return runTransition(cmd, &f, transition_.AuthFlavor{Name: args[0], Flavor: flavor, Authority: authority, CarryRoles: carryRoles}, transition_.ReferenceCandidate)
		},
	}
	f.bind(cmd)
	cmd.Flags().BoolVar(&oidcAzure, "oidc-azure", false, "move the auth to the organization's Azure directory")
	cmd.Flags().BoolVar(&oidcGoogle, "oidc-google", false, "move the auth to the organization's Google Workspace directory")
	cmd.Flags().StringVar(&authority, "authority", "", "who owns role membership afterwards: directory (role claims synchronized at every login) or application (roles assigned in the application); asked when not given")
	cmd.Flags().BoolVar(&carryRoles, "carry-roles", false, "keep the auth's role assignments, keyed by the old usernames, instead of dropping them")

	return cmd
}
