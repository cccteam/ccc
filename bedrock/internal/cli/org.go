// org.go is the org command group: the organization foundation, rendered from the
// organization placement.

package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

const orgPlacementFile = "placement.json"

func newOrg(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "The organization foundation: the layers every application deploys into",
		Long: `org holds the commands that render and check an organization's infrastructure repository:
the six layers of the CCC provisioning model (0-bootstrap, 1-org, 2-shr, 2-spn, 2-net, 2-env),
rendered from the organization placement, placement.json at the repository root, the way an
application's stack is rendered from its code.`,
	}
	cmd.AddCommand(newOrgNew(d), newOrgRender(d), newOrgCheck(d), newOrgRegister(d))

	return cmd
}

func newOrgNew(_ deps) *cobra.Command {
	var placement string

	cmd := &cobra.Command{
		Use:   "new <dir>",
		Short: "Render an organization foundation for an organization that has none",
		Long: `new renders an organization's infrastructure repository into a new or empty directory from
its placement: the six layers of the CCC provisioning model, each with its .tf files, its
README and its seeded terraform.tfvars, and at the root the README, the journal, the ignore
rules and the OpenTofu version. It then prints the hand steps the model needs before the first
apply: the seed (the terraform folder, the boot project, the boot identity and the state
bucket, as the bootstrap administrator), the bootstrap apply on local state and its migration
into the bucket, and the billing grants, all spelled out in 0-bootstrap/README.md.

The placement is read from --placement, or from placement.json in the directory. The .tf
files and the READMEs are owned: org render rewrites them, and org check compares them.
Everything the seed decides (the boot project's suffix, the state bucket, the folder) is
REPLACEME in the seeded values until the seed has run.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			if placement == "" {
				placement = filepath.Join(dir, orgPlacementFile)
			}
			p, err := org.ReadPlacement(placement)
			if err != nil {
				return err
			}
			files, err := org.Render(p)
			if err != nil {
				return err
			}
			written, err := org.Write(files, dir)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rendered the %s foundation into %s: %d owned file(s) in %s; seeded %s.\n",
				p.Prefix, dir, written.Owned, strings.Join(org.Layers, ", "), strings.Join(written.Seeded, ", "))
			if len(written.Kept) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Kept %s as it was.\n", strings.Join(written.Kept, ", "))
			}
			fmt.Fprint(cmd.OutOrStdout(), handSteps(p))

			return nil
		},
	}
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the directory)")

	return cmd
}

// handSteps says what a person does before the first apply, and where the commands are.
func handSteps(p *org.Placement) string {
	return fmt.Sprintf(`
By hand, before the first apply (the commands are in 0-bootstrap/README.md):
  1. Seed, as %s: the terraform folder at the organization root (%s), the boot
     project %s-boot-gbl-core-<suffix>, the boot identity %s-boot-gbl-tofu with its
     organization roles, and the state bucket %s-boot-gbl-state-<suffix>.
  2. Put the seed's values in place: boot_project_id and terraform_folder_id in
     0-bootstrap/terraform.tfvars, boot_project_id in 1-org and 2-env, the bucket in
     placement.json (stateBucket), and run bedrock org render for the backend blocks.
  3. Apply 0-bootstrap on local state, then migrate its state into the bucket.
  4. A billing administrator grants roles/billing.user on %s to the two identities.
Then 1-org, the three shared layers, 2-env per environment, and the applications.
`, p.Operator, p.OrganizationID, p.Prefix, p.Prefix, p.Prefix, p.BillingAccount)
}

func newOrgRender(_ deps) *cobra.Command {
	var (
		dir       string
		placement string
	)

	cmd := &cobra.Command{
		Use:   renderCommand,
		Short: "Rewrite the organization's owned files from the placement",
		Long: `render rewrites the owned files of the organization's infrastructure repository (the layers'
.tf files and READMEs, the root README and the OpenTofu version) from the placement, and
seeds the files that are absent. Run from the repository root, or name it with --dir.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := orgPlacement(dir, placement)
			if err != nil {
				return err
			}
			files, err := org.Render(p)
			if err != nil {
				return err
			}
			written, err := org.Write(files, dir)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rendered the %s foundation into %s: %d owned file(s) written.\n", p.Prefix, dir, written.Owned)
			if len(written.Seeded) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Seeded %s.\n", strings.Join(written.Seeded, ", "))
			}
			if len(p.Applications) > 0 {
				fmt.Fprint(cmd.OutOrStdout(), applicationProjects(p, "each application"))
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the repository root)")

	return cmd
}

func newOrgCheck(_ deps) *cobra.Command {
	var (
		dir       string
		placement string
	)

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Compare the organization's committed layers with what the placement renders",
		Long: `check renders the organization's layers afresh and compares every owned file with the one in
the repository. It exits 1 when any differs or is missing, listing each with the first line
that differs: the drift between the placement and the committed infrastructure. Seeded
files (each layer's terraform.tfvars, the journal, the ignore rules) are a person's and are
not compared.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := orgPlacement(dir, placement)
			if err != nil {
				return err
			}
			r, err := org.Check(p, dir)
			if err != nil {
				return err
			}
			for _, f := range r.Findings {
				if f.Missing {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: missing\n", f.Path)

					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: line %d differs\n  want: %s\n  got:  %s\n", f.Path, f.Line, f.Want, f.Got)
			}
			for _, path := range r.Unseeded {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: not seeded yet (org render writes it)\n", path)
			}
			if !r.Clean() {
				fmt.Fprintf(cmd.OutOrStdout(), "%d of %d owned file(s) differ from what the placement renders\n", len(r.Findings), r.Checked)

				return exitError{code: 1}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %d owned file(s) match the placement\n", dir, r.Checked)

			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the repository root)")

	return cmd
}

func newOrgRegister(_ deps) *cobra.Command {
	var (
		dir       string
		placement string
	)

	cmd := &cobra.Command{
		Use:   "register <app>",
		Short: "Register an application in the foundation",
		Long: `register adds an application to the organization: its code goes into placement.json's
applications, the layers' applications.auto.tfvars are rendered from it (2-env's list, 2-shr's
pushers and pullers, 2-spn's database admins, 2-net's hostnames), and the apply sequence is
printed: 2-env for every environment, then for every environment but the last again (each
grants the next environment's deploy identity read on its records bucket, from state the first
pass did not have), then 2-shr and 2-spn (the grants, on identities that exist now), then the
application's own stack per environment, then 2-net (the hostnames, onto backends that exist
now). An application code is 1 to 6 lowercase alphanumeric characters starting with a letter,
registered once. Run from the repository root, or name it with --dir. Before 1-org has run, the
placement records no environment projects and the rendered values carry REPLACEME; record
1-org's project_ids in placement.json (projects) and run org render.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := args[0]
			if placement == "" {
				placement = filepath.Join(dir, orgPlacementFile)
			}
			p, err := org.ReadPlacement(placement)
			if err != nil {
				return err
			}
			if err := p.Register(app); err != nil {
				return err
			}
			if err := p.Write(placement); err != nil {
				return err
			}
			files, err := org.Render(p)
			if err != nil {
				return err
			}
			written, err := org.Write(files, dir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Registered %s in %s; rendered %d owned file(s), the four applications.auto.tfvars among them.\n", app, placement, written.Owned)
			if missing := p.ProjectsMissing(); len(missing) > 0 {
				fmt.Fprintf(out, "The placement records no project for %s: the rendered values carry REPLACEME there until 1-org's project_ids are recorded in placement.json (projects) and org render runs.\n", strings.Join(missing, ", "))
			}
			fmt.Fprint(out, applySequence(app))
			fmt.Fprint(out, applicationProjects(p, app))

			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the repository root)")

	return cmd
}

// applicationProjects is the block the application's placement records for the
// operations workflow (a restore started from GitHub names the environment's identity
// provider and operations identity by the project's id and number), or what the
// organization's placement still lacks for it.
func applicationProjects(p *org.Placement, app string) string {
	block, missing := p.ApplicationProjects()
	var b strings.Builder
	if block != "" {
		fmt.Fprintf(&b, "\nRecord in %s's placement.json (infrastructure/placement.json), for the operations workflow that starts a restore of an environment from GitHub:\n%s\n", app, block)
	}
	if len(missing) > 0 {
		fmt.Fprintf(&b, "\nThe operations workflow cannot be wired for %s yet: record 1-org's project_ids and project_numbers in placement.json (projects, projectNumbers), then run org register's print again with org render.\n", strings.Join(missing, ", "))
	}

	return b.String()
}

// applySequence says what to apply after an application was registered, in order.
func applySequence(app string) string {
	return fmt.Sprintf(`
Apply, in order (each layer from its directory; 2-env per environment, -var environment=<env>):
  1. 2-env for tst, stg and prd: %s's identities, database, repository link and triggers.
  2. 2-env for tst and stg again: each environment grants the next environment's deploy
     identity read on its records bucket, from state the first pass did not have.
  3. 2-shr and 2-spn: the registry grants and the database admins, on identities that exist now.
  4. %s's own stack, rendered in its repository (bedrock render), applied per environment.
  5. 2-net: the hostnames, onto the backends the stack created.
`, app, app)
}

// orgPlacement reads the placement named, or the one at the repository root.
func orgPlacement(dir, placement string) (*org.Placement, error) {
	if placement == "" {
		placement = filepath.Join(dir, orgPlacementFile)
	}

	return org.ReadPlacement(placement)
}
