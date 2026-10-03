// domain.go is the domain command: the registrations a stack makes through its placement.

package cli

import (
	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/domain"
	"github.com/cccteam/ccc/bedrock/internal/prompt"
)

func newDomain(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domain",
		Short: "Register domains through the placement",
		Long: `domain holds the commands that put a domain registration into the network layer's
placement, where the layer's domains.tf registers it through Cloud Domains.`,
	}
	cmd.AddCommand(newDomainAdd(d))

	return cmd
}

func newDomainAdd(d deps) *cobra.Command {
	var (
		dir     string
		layer   string
		project string
	)

	cmd := &cobra.Command{
		Use:   "add [domain]",
		Short: "Add a domain registration to the placement",
		Long: `add asks Cloud Domains whether the domain can be registered, at what yearly price and
with which notices, and writes the answer into <layer>/terraform.tfvars under registrations.
Nothing is bought here: the pull request's plan shows the purchase, and the apply registers
the domain and points it at the layer's zone.

Run from anywhere inside the repository, it finds the infrastructure root (the repository
root, or its infrastructure directory; --dir overrides). A domain left out is asked for at
the terminal; when standard input is not a terminal, it is refused.

The call is made with Application Default Credentials and billed to --project, else to the
boot project named by boot_project_id in the layer's terraform.tfvars, else to the one in
2-env/terraform.tfvars. It is refused when the domain is not a bare lowercase name, when the
placement already lists it, when registrant_contact.email in the placement is empty (the
registrar's verification mail goes to that mailbox), when the domain is not available, and
when its price is not in whole US dollars.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			infraRoot, _, err := d.infrastructureRoot(dir)
			if err != nil {
				return err
			}
			name, err := domainArgument(args, d.asker(cmd))
			if err != nil {
				return err
			}
			r, err := domain.Add(cmd.Context(), d.domains, domain.Request{Domain: name, Dir: infraRoot, Layer: layer, Project: project})
			if err != nil {
				return err
			}
			r.Write(cmd.OutOrStdout())

			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "the infrastructure root (default: found from the working directory)")
	cmd.Flags().StringVar(&layer, "layer", domain.DefaultLayer, "the layer whose placement takes the registration")
	cmd.Flags().StringVar(&project, "project", "", "the project the Cloud Domains call is billed to (default: boot_project_id from the placement)")
	_ = cmd.MarkFlagDirname("dir")

	return cmd
}

// domainArgument is the domain: the one given, else the one typed at the prompt, else,
// with no terminal to ask on, refused.
func domainArgument(args []string, ask *prompt.Prompter) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	if ask == nil {
		return "", errors.New("no domain given and no terminal to ask on: pass a bare lowercase domain, such as example.com")
	}

	return ask.Line("Which domain? (a bare lowercase name, such as example.com)")
}
