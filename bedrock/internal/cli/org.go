// org.go is the org command group: the organization foundation, rendered from the
// organization placement.

package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

const orgPlacementFile = "placement.json"

func newOrg(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "The organization foundation: the layers every application deploys into",
		Long: `org holds the commands that render and check an organization's infrastructure repository:
the six layers of the CCC provisioning model (0-bootstrap, 1-org, 2-shr, 2-spn, 2-net, 2-env)
and the layers workflow that plans them on a pull request and applies them on its merge,
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
README and its seeded terraform.tfvars, the layers workflow under .github/workflows, and at
the root the README, the journal, the ignore rules and the OpenTofu version. It then prints
the hand steps the model needs before the workflow can run: the seed (the terraform folder,
the boot project, the boot identity and the state bucket, as the bootstrap administrator), the
bootstrap apply on local state and its migration into the bucket, the billing grants and the
first apply of 1-org, all spelled out in 0-bootstrap/README.md; from then on the workflow
plans every layer on a pull request and applies it on the merge, as the layer's own identity.

The placement is read from --placement, or from placement.json in the directory. The .tf
files, the READMEs and the workflow are owned: org render rewrites them, and org check
compares them. Everything the seed decides (the boot project's suffix, the state bucket, the
folder) is REPLACEME in the seeded values until the seed has run.`,
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

// handSteps says what a person does before the workflow can run, and where the commands are.
func handSteps(p *org.Placement) string {
	return fmt.Sprintf(`
By hand, before the layers workflow can run (the commands are in 0-bootstrap/README.md):
  0. In the Workspace Admin console: one team group per environment, named in
     placement.json (teamGroups: a group's address each; production's is not the first
     environment's), whose members approve the environment's releases and ask for its
     entitlements; nothing else names a person. On GitHub, in a browser: the
     organization, this infrastructure repository (never
     managed by the layers; default branch %s), the release, deployer and infrastructure
     apps with their keys, installed on the organization (the release app's App ID goes
     into placement.json, githubReleaseAppId; the infrastructure app's App ID and the
     version of its key in the boot project's container, githubInfrastructureAppId and
     githubInfrastructureKeyVersion, once 0-bootstrap has made the container), the
     organization secrets, the Cloud Build app, and, if a team is to approve changes to
     the applications' check files, that team (githubInfrastructureTeam).
  1. Seed, as %s: the terraform folder at the organization root (%s), the boot
     project %s-boot-gbl-core-<suffix>, the boot identity %s-boot-gbl-tofu with its
     organization roles, and the state bucket %s-boot-gbl-state-<suffix>.
  2. Put the seed's values in place: boot_project_id and terraform_folder_id in
     0-bootstrap/terraform.tfvars, boot_project_id in 1-org and 2-env, and in
     placement.json the bucket (stateBucket) and the boot project's id and number
     (projects.boot, projectNumbers.boot); then bedrock org render, for the backend
     blocks, the bucket's grants and the workflow.
  3. Apply 0-bootstrap on local state, then migrate its state into the bucket.
  4. A billing administrator grants roles/billing.user on %s to the two identities.
  5. Apply 1-org, with GITHUB_TOKEN set to an organization owner's token, and record its
     project_ids and project_numbers in placement.json (projects, projectNumbers); then
     bedrock org render, so the workflow names every layer's identities. The apply leaves
     you Owner on each project it creates, as the seed left you Folder Admin and Folder
     Editor on the folder: a creator's grants, temporary, removed by hand once the
     workflow applies the layers (bedrock org check lists each person still holding
     roles/owner on an environment project).
  6. In the tst project's Cloud Build console, signed in to GitHub as the organization's
     machine account: the Cloud Build GitHub App's authorization (2-env/README.md, "The
     GitHub authorization, before the first application"); its installation id and the
     token secret's version go into 2-env/terraform.tfvars, applied through the workflow.
     bedrock org register refuses the first application until both are set.
From then on the layers workflow (%s) applies every layer, these two included: a pull
request plans the layers it touches as their plan identities and posts the plans, the
merge applies them as their apply identities, in layer order. The shared layers, 2-env per
environment and the applications' registrations go through it; a person applies by hand
for recovery alone (0-bootstrap/README.md, "Recovery, by hand").
`, p.GithubDefaultBranch, p.Operator, p.OrganizationID, p.Prefix, p.Prefix, p.Prefix, p.BillingAccount, org.WorkflowFile)
}

// workflowNotice says what the layers workflow still lacks in the placement, or nothing.
func workflowNotice(p *org.Placement) string {
	missing := p.WorkflowUnwired()
	if len(missing) == 0 {
		return ""
	}

	return fmt.Sprintf("The layers workflow cannot run every layer yet: record %s in placement.json and run bedrock org render.\n", strings.Join(missing, ", "))
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
.tf files and READMEs, the layers workflow, the root README and the OpenTofu version) from
the placement, and seeds the files that are absent. Run from the repository root, or name it
with --dir.`,
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
			fmt.Fprint(cmd.OutOrStdout(), workflowNotice(p))
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

func newOrgCheck(d deps) *cobra.Command {
	var (
		dir       string
		placement string
	)

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Compare the organization's committed layers with what the placement renders",
		Long: `check renders the organization's layers and the layers workflow afresh and compares every
owned file with the one in the repository. It exits 1 when any differs or is missing, listing
each with the first line that differs: the drift between the placement and the committed
infrastructure. Seeded files (each layer's terraform.tfvars, the journal, the ignore rules)
are a person's and are not compared. It then lists each person (a user: member) holding
roles/owner on an environment project the placement records: the grant a project's creator
receives, which the first apply of 1-org by hand leaves the bootstrap administrator with on
every project it creates, temporary by design and removed by hand once the layers workflow
applies the layers. That listing reads the projects' IAM policies with the run's Google
credentials (gcloud auth application-default login); without any it says so, and it never
fails the check.`,
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
			ownerReport(cmd.Context(), d, p, cmd.OutOrStdout())
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

// ownerReport lists each person holding roles/owner on an environment project the
// placement records: the grant a project's creator receives, which the first apply of
// 1-org by hand leaves the bootstrap administrator with on every project it creates, and
// which a hand step removes once the layers workflow applies the layers (1-org/README.md,
// Applying). The read needs Google credentials; without any the report says so and what
// it would have done. Nothing here fails the check: the drift between the placement and
// the repository is the check's verdict, and the grants are a person's to remove.
func ownerReport(ctx context.Context, d deps, p *org.Placement, out io.Writer) {
	const does = "org check lists each person (a user: member) holding roles/owner on an environment project, the creator's temporary grant, when it runs with Google credentials that read the projects' IAM policies (gcloud auth application-default login)"
	if d.policies == nil {
		fmt.Fprintf(out, "Owners not checked: no IAM policy reader is wired; %s.\n", does)

		return
	}
	reader, err := d.policies(ctx)
	if err != nil {
		fmt.Fprintf(out, "Owners not checked (%v): %s.\n", errors.Cause(err), does)

		return
	}
	defer reader.Close()
	owners, unrecorded, err := org.Owners(ctx, p, reader)
	if err != nil {
		fmt.Fprintf(out, "Owners not checked (%v): %s.\n", errors.Cause(err), does)

		return
	}
	if len(unrecorded) > 0 {
		fmt.Fprintf(out, "Owners not checked in %s: the placement records no project there (projects).\n", strings.Join(unrecorded, ", "))
	}
	if len(owners) == 0 {
		if len(unrecorded) < len(org.Environments) {
			fmt.Fprintln(out, "No person holds roles/owner on an environment project.")
		}

		return
	}
	for _, o := range owners {
		fmt.Fprintf(out, "%s (%s): %s holds roles/owner, the creator's grant from the first apply of 1-org by hand; it is temporary, removed once the layers workflow applies the layers (1-org/README.md, Applying).\n", o.Environment, o.Project, o.Member)
	}
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
applications, the layers' values are rendered from it (1-org's repositories and its
public-invoker grants, 2-env's list, 2-shr's pushers and pullers, 2-spn's database admins,
2-net's hostnames), and the pull requests the registration takes through the layers workflow
are printed, since one pass in layer order does not follow the order the grants need: first
1-org and 2-env (the repository, then the identities under it), then 2-env again from the
Actions tab (each environment grants the next environment's deploy identity read on its
records bucket, from state the first pass did not have), then 1-org's public-invoker grants
with 2-shr and 2-spn (the grants, on identities that exist now), then the application's own
stack per environment, then 2-net (the hostnames, onto backends that exist now). The files a
later pull request carries stay in the working tree until then. An application code is 1 to 6
lowercase alphanumeric characters starting with a letter, registered once. Run from the
repository root, or name it with --dir. Before 1-org has run, the placement records no
environment projects and the rendered values carry REPLACEME; record 1-org's project_ids in
placement.json (projects) and run org render. The Cloud Build GitHub App's browser
authorization comes before the first application: register refuses while 2-env's
terraform.tfvars leaves github_app_installation_id or github_oauth_token_secret_version
unset (2-env/README.md, "The GitHub authorization, before the first application"), since
the applications' triggers exist once 2-env holds the connection and nothing is built by
hand before them.`,
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
			unset, err := org.ConnectionUnset(dir)
			if err != nil {
				return err
			}
			if len(unset) > 0 {
				return errors.Newf("%s leaves %s unset: the Cloud Build GitHub App's browser authorization comes before the first application (2-env/README.md, \"The GitHub authorization, before the first application\"); set both and register again", org.EnvTfvars, strings.Join(unset, " and "))
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
			fmt.Fprintf(out, "Registered %s in %s; rendered %d owned file(s), the five applications.auto.tfvars and 1-org/public-invokers.auto.tfvars among them.\n", app, placement, written.Owned)
			if missing := p.ProjectsMissing(); len(missing) > 0 {
				fmt.Fprintf(out, "The placement records no project for %s: the rendered values carry REPLACEME there until 1-org's project_ids are recorded in placement.json (projects) and org render runs.\n", strings.Join(missing, ", "))
			}
			fmt.Fprint(out, workflowNotice(p))
			fmt.Fprint(out, applySequence(p, app))
			fmt.Fprint(out, applicationProjects(p, app))

			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the repository root)")

	return cmd
}

// applicationProjects is the block the application's placement records for the
// operations workflow (a restore or a rerun started from GitHub names the environment's
// identity provider and operations identity by the project's id and number), or what the
// organization's placement still lacks for it.
func applicationProjects(p *org.Placement, app string) string {
	block, missing := p.ApplicationProjects()
	var b strings.Builder
	if block != "" {
		fmt.Fprintf(&b, "\nRecord in %s's placement.json (infrastructure/placement.json), for the operations workflow that starts a restore or a rerun of an environment from GitHub (production's for the rerun alone):\n%s\n", app, block)
	}
	if len(missing) > 0 {
		fmt.Fprintf(&b, "\nThe operations workflow cannot be wired for %s yet: record 1-org's project_ids and project_numbers in placement.json (projects, projectNumbers), then run org register's print again with org render.\n", strings.Join(missing, ", "))
	}

	return b.String()
}

// applySequence says how a registered application reaches the layers through the layers
// workflow: the pull requests, in order, and the one run from the Actions tab.
func applySequence(p *org.Placement, app string) string {
	return fmt.Sprintf(`
Register through the layers workflow, as pull requests into %s (each merge applies the layers
it touches, in layer order); a file a later pull request carries stays in the working tree
until then:
  1. placement.json, 1-org/applications.auto.tfvars and 2-env/applications.auto.tfvars:
     %s's repository (private, squash the only merge, its rulesets and Environments; a
     repository made before this is imported first, 1-org/README.md), then, in tst, stg
     and prd, its identities, database, repository link and triggers.
  2. Run workflow (the Actions tab) with 2-env: tst and stg grant the next environment's
     deploy identity read on their records bucket, from state the first pass did not have.
  3. 1-org/public-invokers.auto.tfvars, 2-shr/applications.auto.tfvars and
     2-spn/applications.auto.tfvars: the public-invoker tag, the registry grants and the
     database admins, on identities that exist now.
  4. %s's own stack, rendered in its repository (bedrock render), applied per environment
     by its pipeline with the first release.
  5. 2-net/applications.auto.tfvars: the hostnames, onto the backends the stack created.
`, p.GithubDefaultBranch, app, app)
}

// orgPlacement reads the placement named, or the one at the repository root.
func orgPlacement(dir, placement string) (*org.Placement, error) {
	if placement == "" {
		placement = filepath.Join(dir, orgPlacementFile)
	}

	return org.ReadPlacement(placement)
}
