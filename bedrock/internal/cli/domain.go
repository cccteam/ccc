// domain.go is the domain command: the registrations a stack makes through its placement,
// and the check that the apps domain resolves to the network layer's zone.

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
		Short: "Register domains through the placement, and check the apps domain's delegation",
		Long: `domain holds the commands that put a domain registration into the network layer's
placement, where the layer's domains.tf registers it through Cloud Domains, and the check
that the apps domain resolves to the network layer's zone.`,
	}
	cmd.AddCommand(newDomainAdd(d), newDomainCheck(d))

	return cmd
}

func newDomainCheck(d deps) *cobra.Command {
	var (
		dir       string
		placement string
	)

	cmd := &cobra.Command{
		Use:   checkCommand,
		Short: "Check that the apps domain resolves to the network layer's zone",
		Long: `check reads the apps domain's zone in the network project (2-net's dns.tf: its name servers,
the apex and wildcard addresses, and the record that proves the domain to Certificate
Manager) through the Cloud DNS API, resolves the domain's delegation as the world sees it,
and says what is in place and what is missing, naming where to act. It runs from the
infrastructure repository's root, or names it with --dir, and reads the organization's
placement (appsDomain, and projects.net, which 1-org's project_ids fill in).

The shape of the domain is found, not configured. When the domain answers the zone's name
servers it is delegated, whether 2-net registered it or a registrar elsewhere points at the
zone. When 2-net registers it (registrations in 2-net/terraform.tfvars) and the registration
is not active yet, the check says so and names the registrant's verification mail. When the
registration names name servers that are not the zone's (the domain answers another set, or
the servers it is delegated to do not answer for it), as after the zone was made again, the
check prints the step in Cloud Domains that points the registration at the zone, with its
gcloud command: the apply never changes the name servers of a registration that exists. A domain
that can be registered (example.com) gets the registrar step, with the zone's name servers
to set there; a label of a domain served elsewhere (apps.example.com) gets the NS records to
add at the DNS provider that serves that domain. A registrable domain that is not delegated
and already answers records that are not the zone's (a website, mail) is refused: such a
domain is never delegated whole, and the check names a label of it to use instead. The
check also resolves the authorization record and says when the certificate is still waiting
on it. Every record is printed on its own line, as it is pasted.

The zone is read with the run's Google credentials (gcloud auth application-default login),
which need to read the network project's zones (roles/dns.reader). It exits 1 when anything
is missing or refused.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := orgPlacement(dir, placement)
			if err != nil {
				return err
			}
			project, zone := p.AppsZone()
			if project == "" {
				return errors.New("placement.json records no network project (projects.net): record 1-org's project_ids in placement.json (projects), then check again")
			}
			r, err := domain.Check(cmd.Context(), d.lookups, domain.CheckRequest{Domain: p.AppsDomain, Project: project, Zone: zone, Dir: dir})
			if err != nil {
				return err
			}
			r.Write(cmd.OutOrStdout())
			if !r.Passed() {
				return exitError{code: 1}
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the repository root)")

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
