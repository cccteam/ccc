// org.go is the org command group: the organization foundation, rendered from the
// organization placement.

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/domain"
	"github.com/cccteam/ccc/bedrock/internal/layers"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/release"
	"github.com/cccteam/ccc/bedrock/internal/where"
)

// The org command group's name, its placement file and the two commands the triggers
// run.
const (
	orgCommand       = "org"
	orgPlacementFile = "placement.json"
	planCommand      = "plan"
	applyCommand     = "apply"
)

func newOrg(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   orgCommand,
		Short: "The organization foundation: the layers every application deploys into",
		Long: `org holds the commands that render and check an organization's infrastructure repository:
the six layers of the CCC provisioning model (0-bootstrap, 1-org, 2-shr, 2-spn, 2-net, 2-env)
and the two Cloud Build triggers in the boot project that plan them on a pull request and apply
them on the merge, rendered from the organization placement, placement.json at the repository
root, the way an application's stack is rendered from its code; the two commands those
triggers run (plan, apply); and the check, before the seed, that the bootstrap administrator
holds the roles the seed and the first applies need (preflight).`,
	}
	cmd.AddCommand(newOrgNew(d), newOrgPreflight(d), newOrgRender(d), newOrgCheck(d), newOrgRegister(d), newOrgPlan(d), newOrgApply(d))

	return cmd
}

func newOrgNew(d deps) *cobra.Command {
	var placement string

	cmd := &cobra.Command{
		Use:   "new <dir>",
		Short: "Render an organization foundation for an organization that has none",
		Long: `new renders an organization's infrastructure repository into a new or empty directory from
its placement: the six layers of the CCC provisioning model, each with its .tf files, its
README and its seeded terraform.tfvars, and at the root the README, the journal, the ignore
rules and the OpenTofu version. 0-bootstrap carries the two Cloud Build triggers, in the boot
project, that plan the layers on a pull request and apply them on the merge, running bedrock
org plan and bedrock org apply. It then prints the hand steps the model needs before the
triggers can run, each opening with the place it happens: the team and role groups, the
GitHub organization with its apps and their keys, org
preflight's check of the bootstrap administrator's roles and the seed (the terraform folder,
the boot project, the boot identity and the state bucket, as the bootstrap administrator), the
Cloud Build GitHub authorization in the boot project, the bootstrap apply on local state and
its migration into the bucket, the billing grants and the spend budget, the first apply of
1-org and the consent screen, spelled out in 0-bootstrap/README.md and the root README; from
then on the triggers plan every layer on a pull request and apply it on the merge, as the
layer's own identity.

The placement is read from --placement, or from placement.json in the directory. The .tf
files and the READMEs are owned: org render rewrites them, and org check compares them.
Everything the seed decides (the boot project's suffix, the state bucket, the folder) is
REPLACEME in the seeded values until the seed has run. The triggers' runs fetch the bedrock
the placement pins (bedrockVersion, and for a release bedrockSha256), so new records the
bedrock running it as the pin when that is a release or a commit installed with go install;
a build from a checkout is nobody's pin, and bedrock upgrade records one later.`,
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
			out := cmd.OutOrStdout()
			if err := d.pinOrg(cmd.Context(), p, placement, out); err != nil {
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
			fmt.Fprintf(out, "Rendered the %s foundation into %s: %d owned file(s) in %s; seeded %s.\n",
				p.Prefix, dir, written.Owned, strings.Join(org.Layers, ", "), strings.Join(written.Seeded, ", "))
			if len(written.Kept) > 0 {
				fmt.Fprintf(out, "Kept %s as it was.\n", strings.Join(written.Kept, ", "))
			}
			fmt.Fprint(out, handSteps(p))

			return nil
		},
	}
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the directory)")

	return cmd
}

// pinOrg records the bedrock running org new as the placement's pin when it is one a
// placement can hold (a release, with its pipeline binary's checksum, or an installed
// commit), writing the placement; a build from a checkout pins nothing, and the notice
// says what records a pin. A placement that pins another bedrock than the one running is
// refused, as render is.
func (d deps) pinOrg(ctx context.Context, p *org.Placement, placement string, out io.Writer) error {
	if p.Pinned() {
		return d.orgPinned(p, placement)
	}
	if !d.running().heldToPin() {
		fmt.Fprintf(out, "This bedrock is %s, which no placement can pin: %s pins no bedrock yet, and the triggers' runs need one; bedrock upgrade in the repository records a release or an installed commit and renders with it.\n", d.running().version, placement)

		return nil
	}
	version, sum, err := d.firstPin(ctx)
	if err != nil {
		return err
	}
	p.BedrockVersion, p.BedrockSHA256 = version, sum
	if err := p.Write(placement); err != nil {
		return err
	}
	fmt.Fprintf(out, "Pinned bedrock %s in %s: the triggers' runs fetch it.\n", version, placement)

	return nil
}

// orgPinned refuses a placement pinned to another bedrock than the release or installed
// commit running the command, as the application's render and check do: the committed
// files say which bedrock they come from through the pin, and bedrock upgrade moves it.
// A placement that pins nothing yet is let through: the triggers' runs are what need
// the pin, and the notice says so.
func (d deps) orgPinned(p *org.Placement, placement string) error {
	running := d.running()
	if !p.Pinned() || !running.heldToPin() || running.version == p.BedrockVersion {
		return nil
	}
	install := release.Page(p.BedrockVersion)
	if p.CommitPin() {
		install = "go install " + release.Module + "@" + p.BedrockVersion
	}

	return errors.Newf("%s pins bedrock %s and this is bedrock %s: install the pinned one (%s) or move the pin (bedrock upgrade)", placement, p.BedrockVersion, running.version, install)
}

// handSteps says what a person does before the triggers can run, and where the commands
// are. Each step opens with the place it happens; the bootstrap roles are org preflight's
// table, one line each, so the steps name what the check checks.
func handSteps(p *org.Placement) string {
	var roles strings.Builder
	for _, role := range org.BootstrapRoles {
		fmt.Fprintf(&roles, "       - %s (%s), on the %s\n", role.Title, role.Role, role.On)
	}

	return fmt.Sprintf(`
By hand, before the Cloud Build triggers can run (the commands are in 0-bootstrap/README.md):
  0. In the Workspace Admin console: one team group per environment, named in
     placement.json (teamGroups: a group's address each; production's is not the first
     environment's), whose members approve the environment's releases and ask for its
     entitlements; nothing else names a person. For each application, before its
     first sign-in, its role groups (<group prefix><role>@%s), each created with
     "Who can view members" set so that its members can: the sign-in reads a person's
     groups with the person's own token, and Google leaves out a group whose member
     list the person may not view.
     On GitHub, as an organization owner: the organization, its machine account
     (%s, an owner of the organization, named in placement.json as
     githubMachineAccount), this infrastructure repository (default branch %s; 1-org
     rules that branch and nothing else of it), the release, deployer and
     infrastructure apps, each installed on all repositories, with their private keys
     (README.md, "The two GitHub Apps", and 0-bootstrap/README.md, "The infrastructure
     GitHub App"; the release
     app's App ID and slug go into placement.json, githubReleaseAppId and
     githubReleaseAppSlug, the deployer app's App ID into placement.json,
     githubDeployerAppId, and the infrastructure app's into placement.json,
     githubInfrastructureAppId) and, if a team is to approve changes to the applications'
     check files, that team (githubInfrastructureTeam).
     In a terminal, in this repository, the apps' keys, never by hand, once 0-bootstrap
     has made their three containers in the boot project: bedrock secret add
     github-infrastructure-key and bedrock secret pin github-infrastructure-key
     <version>; the same with github-deployer-key and github-release-key. No key reaches
     GitHub: the release trigger of each application's stack mints the release app's
     token from the pinned version in Cloud Build.
  1. In a terminal, as %s, after gcloud auth application-default login:
     bedrock org preflight, which must find each of these roles held:
%s     then the seed: the terraform folder at the organization root (%s), the boot
     project %s-boot-gbl-core-<suffix>, the boot identity %s-boot-gbl-tofu with its
     organization roles, and the state bucket %s-boot-gbl-state-<suffix>.
  2. In this repository, the seed's values: boot_project_id and terraform_folder_id in
     0-bootstrap/terraform.tfvars, boot_project_id in 1-org and 2-env, and in
     placement.json the bucket (stateBucket) and the boot project's id and number
     (projects.boot, projectNumbers.boot); then bedrock org render, for the backend
     blocks, the bucket's grants and the triggers.
  3. In the boot project's Cloud Build console, as %s (the bootstrap
     administrator, Owner on the boot project from the seed), signed in to GitHub as the
     organization's machine account (%s): the Cloud Build GitHub App's authorization
     (0-bootstrap/README.md, "The Cloud Build GitHub authorization"), a connection named
     %s in %s; its installation id and the token secret's version go
     into placement.json (cloudBuildInstallationId, cloudBuildTokenSecretVersion). Then,
     in a terminal, in this repository: bedrock upgrade, which pins the bedrock the
     triggers run (bedrockVersion) and renders. bedrock org register refuses the first
     application until the two values are set.
  4. In a terminal, in 0-bootstrap: the apply on local state, the connection imported
     into it first, then the migration of its state into the bucket.
  5. In a terminal, as a billing administrator of %s: roles/billing.user on
     it for the two identities. In the Billing console, as the same administrator: the
     spend budget on the account (0-bootstrap/README.md, step 5); nothing renders it.
  6. In a terminal, in 1-org: the apply, with GITHUB_TOKEN set to an organization
     owner's token; then its project_ids and project_numbers recorded in placement.json
     (projects, projectNumbers), and bedrock org render, so the triggers name every
     layer's identities. The apply leaves you Owner on each project it creates, as the
     seed left you Folder Admin and Folder Editor on the folder: a creator's grants,
     temporary, removed by hand once the apply trigger applies the layers (bedrock org
     check lists each person still holding roles/owner on an environment project).
  7. In each environment project's Google Cloud console, before the first application's
     OAuth client is made there: the consent screen (APIs & Services, OAuth consent
     screen), with the audience Internal, the organization's own users.
From then on the two Cloud Build triggers in the boot project (%s and %s)
apply every layer, these two included: a pull request plans the layers it touches as their
plan identities and posts the plans on it, the merge applies them as their apply
identities, in layer order. The shared layers, 2-env per environment and the applications'
registrations go through them; a person applies by hand for recovery alone
(0-bootstrap/README.md, "Recovery, by hand").
After the first apply of 2-net: bedrock domain check prints what the apps domain still needs, and where.
`, p.OrganizationDomain, p.GithubMachineAccount, p.GithubDefaultBranch, p.Operator, roles.String(), p.OrganizationID, p.Prefix, p.Prefix, p.Prefix,
		p.Operator, p.GithubMachineAccount, p.ConnectionName(), p.Primary().Name, p.BillingAccount, p.PlanTriggerName(), p.ApplyTriggerName())
}

func newOrgPreflight(d deps) *cobra.Command {
	var (
		dir       string
		placement string
	)

	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Check, before the seed, that these credentials hold the roles the seed and the first applies need",
		Long: `preflight asks Google which of the permissions the seed and the first applies of 0-bootstrap
and 1-org need the caller holds, with the run's Application Default Credentials (gcloud auth
application-default login, as the bootstrap administrator): on the organization the
placement names (organizationId, through Cloud Resource Manager's testIamPermissions) and on
its billing account (billingAccount, through Cloud Billing's). It prints one line per role,
holds or missing, naming the permissions tested: Folder Creator, Project Creator,
Organization Administrator, Organization Policy Administrator, Organization Role
Administrator and Tag Administrator at the organization, and Billing Account User on the
billing account. It exits 1 when any is missing, saying who grants it where, and when the
permissions could not be checked (no credentials, or an API that refused to answer). Run
from the repository root, or name it with --dir.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := orgPlacement(dir, placement)
			if err != nil {
				return err
			}
			if !org.Preflight(cmd.Context(), p, d.permissionTester(), cmd.OutOrStdout()) {
				return exitError{code: 1}
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the repository root)")

	return cmd
}

// permissionTester is what opens org preflight's permission tests, or, when none is wired,
// an open that says so.
func (d deps) permissionTester() org.PermissionTesterFunc {
	if d.org == nil || d.org.permissions == nil {
		return func(context.Context) (org.PermissionTester, error) {
			return nil, errors.New("no permission tester is wired")
		}
	}

	return d.org.permissions
}

// triggerNotice says what the triggers still lack in the placement, or nothing: the
// projects whose layers they cannot run yet, the pin their runs fetch, and the GitHub
// authorization 0-bootstrap renders them from.
func triggerNotice(p *org.Placement) string {
	var b strings.Builder
	if missing := p.UnrecordedProjects(); len(missing) > 0 {
		fmt.Fprintf(&b, "The triggers cannot run every layer yet: record %s in placement.json and run bedrock org render.\n", strings.Join(missing, ", "))
	}
	if !p.Pinned() {
		b.WriteString("placement.json pins no bedrock (bedrockVersion), and the triggers' runs fetch the pinned one: run bedrock upgrade in this repository, which records the pin and renders.\n")
	}
	if unset := p.ConnectionUnset(); len(unset) > 0 {
		fmt.Fprintf(&b, "0-bootstrap renders the Cloud Build connection and the triggers once placement.json records %s, from the GitHub authorization in the boot project (0-bootstrap/README.md, \"The Cloud Build GitHub authorization\").\n", strings.Join(unset, " and "))
	}

	return b.String()
}

func newOrgRender(d deps) *cobra.Command {
	var (
		dir       string
		placement string
	)

	cmd := &cobra.Command{
		Use:   renderCommand,
		Short: "Rewrite the organization's owned files from the placement",
		Long: `render rewrites the owned files of the organization's infrastructure repository (the layers'
.tf files and READMEs, the root README and the OpenTofu version) from the placement, and seeds
the files that are absent. It refuses a placement pinned to another bedrock than the release
or installed commit running it (bedrock upgrade moves the pin). Run from the repository root,
or name it with --dir.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := orgPlacement(dir, placement)
			if err != nil {
				return err
			}
			if err := d.orgPinned(p, orgPlacementPath(dir, placement)); err != nil {
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
			fmt.Fprint(cmd.OutOrStdout(), triggerNotice(p))
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
		Use:   checkCommand,
		Short: "Compare the organization's committed layers with what the placement renders",
		Long: `check renders the organization's layers afresh and compares every owned file with the one in
the repository. It exits 1 when any differs or is missing, listing each with the first line
that differs: the drift between the placement and the committed infrastructure. Seeded files
(each layer's terraform.tfvars, the journal, the ignore rules) are a person's and are not
compared. It also lists, and fails on, a file in a layer it does not render: a .tf file added
by hand, which would run as the layer's identity, or an override file (override.tf,
*_override.tf), which OpenTofu merges into the layer. It refuses a placement pinned to another bedrock than the release or installed commit running
it (bedrock upgrade moves the pin). The plan trigger runs it before each plan, and the apply
trigger before each apply, so nothing a pull request adds to a layer by hand runs there.

Before anything else it reports each domain 2-net registers (registrations in
2-net/terraform.tfvars) as Cloud Domains holds it in the network project: first every
registration whose registrant mailbox is not verified yet, naming the mailbox and the date
the registrar's verification mail must be followed by (fifteen days after the registration,
or the domain is suspended), then each registration's state and expiry date, an expiry
within thirty days and any other issue the registrar raises. That report reads the
registrations with the run's Google credentials (gcloud auth application-default login,
roles/domains.viewer on the network project); without any, or when Cloud Domains cannot be
reached, it says so, and it never fails the check. While zoneReplacement is set in the
placement (2-net's zones rendered without prevent_destroy, for making a zone again), it says
so and what is left to do, also without failing the check. It then lists each person (a user: member) holding
roles/owner on an environment project the placement records: the grant a project's creator
receives, which the first apply of 1-org by hand leaves the bootstrap administrator with on
every project it creates, temporary by design and removed by hand once the apply trigger
applies the layers. That listing reads the projects' IAM policies with the run's Google
credentials (gcloud auth application-default login); without any it says so, and it never
fails the check. Last it lists each API key in an environment project that carries no API
restriction, which answers every API in the project that accepts an API key: Firebase's
browser key, which initializing Identity Platform creates and bedrock org apply restricts
after each apply of 2-env, when Firebase has made it again since, or any other.
That listing reads the projects' API keys with the same credentials, says so without any,
and never fails the check either.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := orgPlacement(dir, placement)
			if err != nil {
				return err
			}
			if err := d.orgPinned(p, orgPlacementPath(dir, placement)); err != nil {
				return err
			}
			r, err := org.Check(p, dir)
			if err != nil {
				return err
			}
			registrationReport(cmd.Context(), d, p, dir, cmd.OutOrStdout())
			fmt.Fprint(cmd.OutOrStdout(), zoneReplacementNotice(p))
			for _, f := range r.Findings {
				switch {
				case f.Reason != "":
					fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", f.Path, f.Reason)
				case f.Missing:
					fmt.Fprintf(cmd.OutOrStdout(), "%s: missing\n", f.Path)
				default:
					fmt.Fprintf(cmd.OutOrStdout(), "%s: line %d differs\n  want: %s\n  got:  %s\n", f.Path, f.Line, f.Want, f.Got)
				}
			}
			for _, path := range r.Unseeded {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: not seeded yet (org render writes it)\n", path)
			}
			ownerReport(cmd.Context(), d, p, cmd.OutOrStdout())
			keyReport(cmd.Context(), d, p, cmd.OutOrStdout())
			if !r.Clean() {
				fmt.Fprintf(cmd.OutOrStdout(), "%d finding(s) against the %d owned file(s) the placement renders\n", len(r.Findings), r.Checked)

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

// zoneReplacementNotice says, while zoneReplacement is set in the placement, that 2-net's
// zones are rendered without prevent_destroy and what is left to do, so the guard is not
// left off by forgetting; nothing when it is not set. It is information: the value is a
// person's decision, and the check does not fail on it.
func zoneReplacementNotice(p *org.Placement) string {
	if !p.ZoneReplacement {
		return ""
	}

	return "zoneReplacement is set in placement.json: 2-net's apps zone and parked zones are rendered without prevent_destroy, so a plan may destroy them and create them again on other name servers. Make the change that recreates the zone (a new appsDomain, then bedrock org render), point what pointed at the old name servers at the new ones as bedrock domain check prints, then clear zoneReplacement and run bedrock org render to write the rule back; bedrock domain check says when the domain points at the zone (2-net/README.md, \"Making a zone again\").\n"
}

// registrationReport reads each domain 2-net registers (registrations in its
// terraform.tfvars) through Cloud Domains and prints its state and expiry date, a
// registration waiting on the registrant mailbox's verification first: the registrar's
// verification mail goes to that mailbox, and a domain whose link is not followed within
// fifteen days is suspended. The read needs Google credentials that read the network
// project's registrations; without any the report says so and what it would have done.
// Nothing here fails the check, as with the owners and the keys.
func registrationReport(ctx context.Context, d deps, p *org.Placement, dir string, out io.Writer) {
	const does = "org check reports each domain 2-net registers (registrations in 2-net/terraform.tfvars), its state and expiry date, a registrant mailbox waiting on its verification first, when it runs with Google credentials that read the network project's Cloud Domains registrations (roles/domains.viewer; gcloud auth application-default login)"
	var open domain.RegistrationReaderFunc
	if d.org != nil {
		open = d.org.registrations
	}
	project, _ := p.AppsZone()
	r, err := domain.Registrations(ctx, open, domain.RegistrationsRequest{Dir: dir, Project: project, Now: time.Now()})
	if err != nil {
		fmt.Fprintf(out, "Registrations not checked (%v): %s.\n", errors.Cause(err), does)

		return
	}
	r.Write(out)
}

// ownerReport lists each person holding roles/owner on an environment project the
// placement records: the grant a project's creator receives, which the first apply of
// 1-org by hand leaves the bootstrap administrator with on every project it creates, and
// which a hand step removes once the apply trigger applies the layers (1-org/README.md,
// Applying). The read needs Google credentials; without any the report says so and what
// it would have done. Nothing here fails the check: the drift between the placement and
// the repository is the check's verdict, and the grants are a person's to remove.
func ownerReport(ctx context.Context, d deps, p *org.Placement, out io.Writer) {
	const does = "org check lists each person (a user: member) holding roles/owner on an environment project, the creator's temporary grant, when it runs with Google credentials that read the projects' IAM policies (gcloud auth application-default login)"
	if d.org == nil || d.org.policies == nil {
		fmt.Fprintf(out, "Owners not checked: no IAM policy reader is wired; %s.\n", does)

		return
	}
	reader, err := d.org.policies(ctx)
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
		fmt.Fprintf(out, "%s (%s): %s holds roles/owner, the creator's grant from the first apply of 1-org by hand; it is temporary, removed once the apply trigger applies the layers (1-org/README.md, Applying).\n", o.Environment, o.Project, o.Member)
	}
}

// keyReport lists each API key in an environment project that carries no API
// restriction: such a key answers every API in the project that accepts an API key. The
// one expected is Firebase's browser key, which initializing Identity Platform creates
// (2-env's identity-platform.tf) and bedrock org apply restricts to the sign-in APIs
// after each apply of 2-env, when Firebase has made it again since; any other is a
// person's to restrict or delete. The read needs Google credentials; without any the
// report says so and what it would have done. Nothing here fails the check, as with the
// owners: the repository's drift is the check's verdict, and a key is live state no
// commit changes.
func keyReport(ctx context.Context, d deps, p *org.Placement, out io.Writer) {
	const does = "org check lists each API key in an environment project that carries no API restriction, when it runs with Google credentials that list the projects' API keys (gcloud auth application-default login)"
	if d.org == nil || d.org.keys == nil {
		fmt.Fprintf(out, "API keys not checked: no API key lister is wired; %s.\n", does)

		return
	}
	lister, err := d.org.keys(ctx)
	if err != nil {
		fmt.Fprintf(out, "API keys not checked (%v): %s.\n", errors.Cause(err), does)

		return
	}
	defer lister.Close()
	keys, unrecorded, err := org.UnrestrictedKeys(ctx, p, lister)
	if err != nil {
		fmt.Fprintf(out, "API keys not checked (%v): %s.\n", errors.Cause(err), does)

		return
	}
	if len(unrecorded) > 0 {
		fmt.Fprintf(out, "API keys not checked in %s: the placement records no project there (projects).\n", strings.Join(unrecorded, ", "))
	}
	if len(keys) == 0 {
		if len(unrecorded) < len(org.Environments) {
			fmt.Fprintln(out, "Every API key in the environment projects carries an API restriction.")
		}

		return
	}
	for _, k := range keys {
		if k.Key.DisplayName == org.FirebaseBrowserKey {
			fmt.Fprintf(out, "%s (%s): the API key %q (%s) carries no API restriction, so it answers every API in the project that accepts an API key; bedrock org apply restricts it to %s after each apply of 2-env: start the apply trigger for that environment (%s) to restrict it now (2-env/README.md, Identity Platform).%s\n", k.Environment, k.Project, k.Key.DisplayName, k.Key.Name, strings.Join(org.SignInAPIs, " and "), triggerRun(p, k.Environment), triggerUnrendered(p))

			continue
		}
		fmt.Fprintf(out, "%s (%s): the API key %q (%s) carries no API restriction, so it answers every API in the project that accepts an API key; no layer declares it: restrict it to the APIs it is for, or delete it.\n", k.Environment, k.Project, k.Key.DisplayName, k.Key.Name)
	}
}

func newOrgRegister(d deps) *cobra.Command {
	var (
		dir       string
		placement string
	)

	cmd := &cobra.Command{
		Use:   "register <app> [<application checkout>]",
		Short: "Register an application in the foundation and write its first placement",
		Long: `register adds an application to the organization: its code goes into placement.json's
applications, the layers' values are rendered from it (1-org's repositories and its
public-invoker grants, 2-env's list, 2-shr's pushers and pullers, 2-spn's database admins,
2-net's hostnames), and the pull requests the registration takes through the Cloud Build
triggers are printed, since one pass in layer order does not follow the order the grants
need: first 1-org and 2-env (the repository, then the identities under it), then 2-env again
by starting the apply trigger by hand (each environment grants the next environment's deploy
identity read on its records bucket, from state the first pass did not have), then 1-org's public-invoker grants
with 2-shr and 2-spn (the grants, on identities that exist now), then the application's own
stack per environment, then 2-net (the hostnames, onto backends that exist now). The files a
later pull request carries stay in the working tree until then. An application code is 1 to 6
lowercase alphanumeric characters starting with a letter, registered once. Run from the
repository root, or name it with --dir.

It also makes the application's first placement.json, the file bedrock render reads in the
application's repository (infrastructure/placement.json), from this placement and nothing
asked: the prefix, the environments, the regions, the domains, the state bucket, the default
branch, the release app's slug, the labels and the environment projects' ids and numbers,
with Cloud Run's sample image to create the services with, the application's code as its
repository's name, the first environment's database seeded, and the bedrock running
register as the pin (a release with its pipeline binary's checksum from the release's
checksums.txt, or a commit installed with go install; a build from a checkout is nobody's
pin and is refused). Given the application's checkout as the second argument (typically a
sibling of this repository), register writes the file there and refuses to overwrite one
that exists; without it, register prints the file. The approvals, the maintenance windows,
the build machine and the instance caps take their defaults; production's maintenance
window is the team's to write before its first breaking release.

The placement must record 1-org's project ids and numbers for every environment (projects,
projectNumbers) and the seed's state bucket, which the application's placement names. The
Cloud Build GitHub App's browser authorization comes before the first application: register
refuses while placement.json leaves cloudBuildInstallationId or cloudBuildTokenSecretVersion
unset (0-bootstrap/README.md, "The Cloud Build GitHub authorization"), since the
applications' triggers exist once 2-env holds the connection and nothing is built by hand
before them. A refusal changes nothing.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := args[0]
			if placement == "" {
				placement = filepath.Join(dir, orgPlacementFile)
			}
			p, err := org.ReadPlacement(placement)
			if err != nil {
				return err
			}
			if err := d.orgPinned(p, placement); err != nil {
				return err
			}
			if err := p.Register(app); err != nil {
				return err
			}
			if unset := p.ConnectionUnset(); len(unset) > 0 {
				return errors.Newf("placement.json leaves %s unset: the Cloud Build GitHub App's browser authorization in the boot project comes before the first application (0-bootstrap/README.md, \"The Cloud Build GitHub authorization\"); record both and register again", strings.Join(unset, " and "))
			}
			first, err := d.firstPlacement(cmd.Context(), p, app)
			if err != nil {
				return err
			}
			target := ""
			if len(args) == 2 {
				if target, err = firstPlacementFile(args[1]); err != nil {
					return err
				}
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
			fmt.Fprint(out, triggerNotice(p))
			fmt.Fprint(out, applySequence(p, app))

			return writeFirstPlacement(out, first, app, target)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the repository root)")

	return cmd
}

// firstPlacement is the application's first placement: every field from the
// organization's placement, the pin the bedrock running register.
func (d deps) firstPlacement(ctx context.Context, p *org.Placement, app string) (*derive.Placement, error) {
	version, sum, err := d.firstPin(ctx)
	if err != nil {
		return nil, err
	}

	return p.ApplicationPlacement(app, version, sum)
}

// firstPin is the bedrock pin an application's first placement carries: the bedrock
// running register, so the application renders with the bedrock that registered it. A
// release carries its pipeline binary's checksum, read from the release's checksums.txt
// as bedrock upgrade reads it; a commit installed with go install carries none. Any other
// build, one from a checkout or (devel), is nobody's pin and is refused.
func (d deps) firstPin(ctx context.Context) (version, sum string, err error) {
	running := d.running()
	if !running.heldToPin() {
		return "", "", errors.Newf("this bedrock is %s, which no placement can pin, and an application's first placement pins the bedrock that writes it: register with a released bedrock (a binary from its GitHub Release) or one installed at a pushed commit (go install %s@<commit>)", running.version, release.Module)
	}
	if release.IsCommitPin(running.version) {
		return running.version, "", nil
	}
	_, sum, err = releaseChecksums(ctx, d.releases(), running.version)
	if err != nil {
		return "", "", err
	}

	return running.version, sum, nil
}

// firstPlacementFile is where register writes the application's first placement in its
// checkout: infrastructure/placement.json, where bedrock render reads it. The checkout
// is a directory, and a placement already there is never overwritten.
func firstPlacementFile(checkout string) (string, error) {
	info, err := os.Stat(checkout)
	if err != nil || !info.IsDir() {
		return "", errors.Newf("no directory at %s: the second argument is the application's checkout, where register writes its first placement (infrastructure/placement.json)", checkout)
	}
	file := where.PlacementFile(checkout)
	switch _, err := os.Stat(file); {
	case err == nil:
		return "", errors.Newf("%s exists: register writes an application's first placement and never overwrites one; leave the checkout out to print the file instead", file)
	case !errors.Is(err, os.ErrNotExist):
		return "", errors.Wrap(err, "os.Stat()")
	}

	return file, nil
}

// writeFirstPlacement writes the application's first placement to the file, or prints it
// when there is none, last, so it is copied whole.
func writeFirstPlacement(out io.Writer, first *derive.Placement, app, file string) error {
	defaults := "Approvals, maintenance windows, the build machine and instance caps are left to their defaults; production's maintenance window is the team's to write before its first breaking release."
	if file != "" {
		if err := derive.CreatePlacement(file, first); err != nil {
			return err
		}
		fmt.Fprintf(out, "\nWrote %s, %s's first placement, pinned to bedrock %s: commit it in the application's repository, where bedrock render writes the stack beside it. %s\n", file, app, first.BedrockVersion, defaults)

		return nil
	}
	data, err := derive.MarshalPlacement(first)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\n%s's first placement, pinned to bedrock %s: commit it in the application's repository as infrastructure/placement.json, where bedrock render reads it (bedrock org register %s <checkout> writes it there instead). %s\n%s", app, first.BedrockVersion, app, defaults, data)

	return nil
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
		fmt.Fprintf(&b, "\nThe operations workflow cannot be wired for %s yet: record 1-org's project_ids and project_numbers in placement.json (projects, projectNumbers) and run org render again, which prints the block; org register writes it into a new application's first placement.\n", strings.Join(missing, ", "))
	}

	return b.String()
}

// applySequence says how a registered application reaches the layers through the Cloud
// Build triggers: the pull requests, in order, and the one run of the apply trigger by hand.
func applySequence(p *org.Placement, app string) string {
	return fmt.Sprintf(`
Register through the Cloud Build triggers, as pull requests into %s (each merge applies the
layers it touches, in layer order); a file a later pull request carries stays in the working
tree until then:
  1. placement.json, 1-org/applications.auto.tfvars and 2-env/applications.auto.tfvars:
     %s's repository (private, squash the only merge, its rulesets and Environments; a
     repository made before this is imported first, 1-org/README.md), then, in tst, stg
     and prd, its identities, database, repository link and triggers.
  2. The apply trigger by hand with 2-env, in a terminal:
     %s
     tst and stg grant the next environment's deploy identity read on their records
     bucket, from state the first pass did not have.
  3. 1-org/public-invokers.auto.tfvars, 2-shr/applications.auto.tfvars and
     2-spn/applications.auto.tfvars: the public-invoker tag, the registry grants and the
     database admins, on identities that exist now.
  4. %s's own stack, rendered in its repository (bedrock render) from its first
     placement.json (below): its first apply in each environment is by hand, as the
     stack's README says under Applying (the triggers are part of the stack, so no
     build runs before it); every later apply is its pipeline's, with each release.
  5. 2-net/applications.auto.tfvars: the hostnames, onto the backends the stack created.
  6. After 2-net's apply: bedrock domain check, which passes when the apps domain resolves to its zone.
`, p.GithubDefaultBranch, app, triggerRun(p, ""), app)
}

// triggerUnrendered says, while the placement records no Cloud Build authorization, that
// the triggers exist once it does and 0-bootstrap has applied; empty otherwise.
func triggerUnrendered(p *org.Placement) string {
	if p.Connected() {
		return ""
	}

	return " The trigger exists once placement.json records the Cloud Build GitHub authorization (cloudBuildInstallationId, cloudBuildTokenSecretVersion) and 0-bootstrap has applied."
}

// triggerRun is the command that starts the apply trigger by hand for 2-env: one
// environment, or without one the three in order.
func triggerRun(p *org.Placement, env string) string {
	substitutions := "_LAYER=2-env"
	if env != "" {
		substitutions += ",_ENVIRONMENT=" + env
	}

	return fmt.Sprintf("gcloud builds triggers run %s --region=%s --project=%s --branch=%s --substitutions=%s", p.ApplyTriggerName(), p.Primary().Name, p.BootProject(), p.GithubDefaultBranch, substitutions)
}

// orgPlacementPath is the placement's path as the messages name it: the file named, or
// placement.json at the repository root.
func orgPlacementPath(dir, placement string) string {
	if placement != "" {
		return placement
	}

	return filepath.Join(dir, orgPlacementFile)
}

func newOrgPlan(d deps) *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   planCommand,
		Short: "Plan the layers a pull request changes, from the plan trigger's run",
		Long: `plan is what the plan trigger 0-bootstrap renders runs on a pull request into the default
branch, after bedrock org check. It reads the build through the Cloud Build API (BUILD_ID,
PROJECT_ID and LOCATION, which the trigger's step passes): the pull request (_PR_NUMBER), its
base (_BASE_BRANCH) and its commit (COMMIT_SHA); reads the files the pull request changes
against its base through the repository's Cloud Build connection; and plans every layer
whose .tf, .tfvars or lock file changed, and every layer when .opentofu-version changed, side
by side, each in its own directory as the layer's plan identity (impersonated by the plan
trigger's runner; 2-env once per environment, with its own state prefix), without the state
lock. Once every plan has finished each gets its verdict: its Plan: line when it succeeded;
passed, as planned after the layers before it apply, when it failed only because it reads
outputs their plans create, since the merge applies those first; failed otherwise, a plan
that did not run included. One comment per layer goes on the pull request as the deployer
GitHub App, found by its first line and updated on a later push; the plans stay in the run's
log while placement.json records no deployer app (githubDeployerAppId,
githubDeployerKeyVersion). 1-org's plan gets the infrastructure GitHub App's token as
GITHUB_TOKEN, minted from the app's key in the boot project at the version placement.json
pins. _LAYER names one layer to plan whatever the pull request changes, and _ENVIRONMENT one
environment of 2-env. It exits 1 when any plan failed. It refuses a build that did not start
from the trigger (no _BEDROCK_STEPS substitution) and a layer whose identity is under a
project placement.json does not record yet (REPLACEME), and says so when it runs under a
step list an earlier bedrock rendered: the trigger's steps lag one apply behind the
repository, and a release keeps working under the previous release's steps.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return layers.Plan(cmd.Context(), d.layerClients(), layerRequest(dir), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")

	return cmd
}

func newOrgApply(d deps) *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   applyCommand,
		Short: "Apply the layers changed since their last apply, from the apply trigger's run",
		Long: `apply is what the apply trigger 0-bootstrap renders runs on a push to the default branch,
after bedrock org check, and what a person starts by hand for one layer (gcloud builds
triggers run <apply trigger> --substitutions=_LAYER=<layer>, and for 2-env
_ENVIRONMENT=<env>; 0-bootstrap/README.md, "The Cloud Build triggers"). It reads the build
through the Cloud Build API (BUILD_ID, PROJECT_ID and LOCATION, which the trigger's step
passes): the commit (COMMIT_SHA), _LAYER and _ENVIRONMENT. Without _LAYER it applies every
layer changed since its last successful apply: a marker object per layer in the state bucket
(applied/<layer>; applied/2-env/<env>) holds the commit of that apply, the files changed
between it and this commit are read through the repository's Cloud Build connection, and a
layer with no marker is applied. With _LAYER it applies that layer alone, changed or not
(_ENVIRONMENT one environment of 2-env; without it, the three in order). The layers apply in
layer order, one at a time, each in its own directory as the layer's apply identity
(impersonated by the apply trigger's runner): tofu init at the layer's state prefix, a plan,
the apply of that plan, then the marker. It stops at the first failure, the markers of the
layers applied before it written. After each apply of 2-env it restricts Firebase's browser
key in the environment project to the sign-in APIs (2-env/README.md, Identity Platform).
1-org's apply gets the infrastructure GitHub App's token as GITHUB_TOKEN, minted from the
app's key in the boot project at the version placement.json pins. It refuses a build that did
not start from the trigger (no _BEDROCK_STEPS substitution) and a layer whose identity is
under a project placement.json does not record yet (REPLACEME), and says so when it runs
under a step list an earlier bedrock rendered: the trigger's steps lag one apply behind the
repository, and a release keeps working under the previous release's steps.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return layers.Apply(cmd.Context(), d.layerClients(), layerRequest(dir), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")

	return cmd
}

// layerRequest is the run's request: the build the trigger's step names in the
// environment, and the checkout.
func layerRequest(dir string) *layers.Request {
	return &layers.Request{BuildID: os.Getenv("BUILD_ID"), Project: os.Getenv("PROJECT_ID"), Location: os.Getenv("LOCATION"), Dir: dir}
}

// layerClients are what org plan and org apply reach the world through: the deploy
// sequence's seams (Cloud Build, Cloud Storage, Secret Manager, GitHub, the programs a
// step runs) and the API Keys API as an impersonated identity.
func (d deps) layerClients() *layers.Clients {
	c := &layers.Clients{}
	if d.deploy != nil {
		c.Builds, c.Store, c.Secrets, c.GitHub, c.Exec = d.deploy.Builds, d.deploy.Storage, d.deploy.Secrets, d.deploy.GitHub, d.deploy.Exec
	}
	if d.org != nil {
		c.Keys = d.org.keysAs
	}

	return c
}

// orgPlacement reads the placement named, or the one at the repository root.
func orgPlacement(dir, placement string) (*org.Placement, error) {
	if placement == "" {
		placement = filepath.Join(dir, orgPlacementFile)
	}

	return org.ReadPlacement(placement)
}
