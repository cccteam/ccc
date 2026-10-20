// org.go is the org command group: the organization foundation, rendered from the
// organization placement.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
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
	cmd.AddCommand(newOrgNew(d), newOrgPreflight(d), newOrgRender(d), newOrgCheck(d), newOrgRegister(d), newOrgPlan(d), newOrgApply(d), newOrgTeardown(d), newOrgServe(d), newOrgDestroy(d))

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
		next      bool
	)

	cmd := &cobra.Command{
		Use:   "register <code> <application checkout>",
		Short: "Register an application in the foundation and write its first placement",
		Long: `register adds an application to the organization: its code, the organization's naming
decision, and its repository's name, read from the checkout's go.mod module path (a module
under github.com/<organization> is its repository; a vanity import path is refused), go into
placement.json's applications, the layers' values are rendered from them (1-org's
repositories and its public-invoker grants, 2-env's list, 2-shr's repository and its pushers
and pullers, 2-spn's database admins, 2-net's hostnames), and the pull requests the
registration takes through the Cloud Build triggers are printed, since one pass in layer
order does not follow the order the grants need: first 1-org and 2-env (the repository, then
the identities under it),
then 2-env again by starting the apply trigger by hand (each environment grants the next
environment's deploy identity read on its records bucket, from state the first pass did not
have), then 1-org's public-invoker grants with 2-shr and 2-spn (the grants, on identities
that exist now), then the application's own stack per environment, then 2-net (the
hostnames, onto backends that exist now). placement.json records how far the registration
has gone (registration: identities, then grants, then nothing once complete), the layers
render what that step may apply, and register <app> --next moves to the next step: to
grants once 2-env's state in every environment holds the application's identities, to
complete once its stack has a state in every environment, each read from the state bucket,
and refused before. So every pull request of the sequence carries every rendered file, and
bedrock org check passes on each. An application code is 1 to 6 lowercase letters and
digits starting with a letter, the segment every resource name of the application carries,
registered once; the repository's name is bound by no such rule. Run from the repository
root, or name it with --dir.

It also makes the application's first placement.json, the file bedrock render reads in the
application's repository (infrastructure/placement.json), from this placement and nothing
asked: the prefix, the environments, the regions, the domains, the state bucket, the default
branch, the release app's slug, the labels and the environment projects' ids and numbers,
with Cloud Run's sample image to create the services with, the application's code and its
repository's name, the first environment's database seeded, and the bedrock running
register as the pin (a release with its pipeline binary's checksum from the release's
checksums.txt, or a commit installed with go install; a build from a checkout is nobody's
pin and is refused). The second argument is the application's checkout (typically a sibling
of this repository), whose go.mod names the repository: register writes the file there and
refuses to overwrite one that exists. The approvals, the maintenance windows, the build
machine and the instance caps take their defaults; production's maintenance window is the
team's to write before its first breaking release.

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
			if next {
				if len(args) == 2 {
					return errors.New("--next moves a registration on and writes no first placement: leave the checkout out")
				}

				return d.registerNext(cmd.Context(), cmd.OutOrStdout(), p, placement, dir, app)
			}
			if len(args) != 2 {
				return errors.Newf("register %s needs the application's checkout as its second argument: its go.mod names the repository, and its first placement is written there", app)
			}

			return d.registerFirst(cmd.Context(), cmd.OutOrStdout(), p, placement, dir, args)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the repository root)")
	cmd.Flags().BoolVar(&next, "next", false, "move the application's registration to its next step: grants, then complete")

	return cmd
}

// registerFirst registers the application at its identities: the placement written and
// rendered, the sequence printed, and the application's first placement written into
// the checkout named second, whose module path names the repository.
func (d deps) registerFirst(ctx context.Context, out io.Writer, p *org.Placement, placement, dir string, args []string) error {
	app := args[0]
	if err := derive.ValidateApplicationCode(app); err != nil {
		return err
	}
	repository, err := checkoutRepository(args[1], p.GithubOrganization)
	if err != nil {
		return err
	}
	if err := p.Register(app, repository); err != nil {
		return err
	}
	if unset := p.ConnectionUnset(); len(unset) > 0 {
		return errors.Newf("placement.json leaves %s unset: the Cloud Build GitHub App's browser authorization in the boot project comes before the first application (0-bootstrap/README.md, \"The Cloud Build GitHub authorization\"); record both and register again", strings.Join(unset, " and "))
	}
	first, err := d.firstPlacement(ctx, p, app, repository)
	if err != nil {
		return err
	}
	target, err := firstPlacementFile(args[1])
	if err != nil {
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
	fmt.Fprintf(out, "Registered %s in %s at its identities; rendered %d owned file(s), the five applications.auto.tfvars and 1-org/public-invokers.auto.tfvars among them.\n", app, placement, written.Owned)
	fmt.Fprint(out, triggerNotice(p))
	fmt.Fprint(out, applySequence(p, app))

	return writeFirstPlacement(out, first, app, target)
}

// The state objects register --next reads from the state bucket before it moves a
// registration on: a layer's or a stack's state at its prefix.
const (
	stateObject    = "default.tfstate"
	envStatePrefix = "2-env/"
	appStatePrefix = "3-app/"
	// applicationsOutput is 2-env's output of the applications' identities, keyed by
	// application.
	applicationsOutput = "applications"
)

// registerNext moves the application's registration to its next step, once the state the
// step needs is there, writes the placement, renders, and prints what the pull request
// carries and what follows its apply.
func (d deps) registerNext(ctx context.Context, out io.Writer, p *org.Placement, placement, dir, app string) error {
	if err := d.registrationReady(ctx, p, app); err != nil {
		return err
	}
	step, err := p.Advance(app)
	if err != nil {
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
	if step == org.RegistrationGrants {
		fmt.Fprintf(out, "Moved %s's registration to its grants in %s; rendered %d owned file(s).\n", app, placement, written.Owned)
		fmt.Fprintf(out, `
The pull request now carries 1-org/public-invokers.auto.tfvars, 2-shr/applications.auto.tfvars
and 2-spn/applications.auto.tfvars: the public-invoker tag, the registry grants and the
database admins, on identities that exist now. After its merge and apply: %s's own stack,
applied for the first time in each environment through its operations workflow (Actions,
operations, apply; its infrastructure/README.md, Applying), then
bedrock org register %s --next.
`, app, app)

		return nil
	}
	fmt.Fprintf(out, "Completed %s's registration in %s; rendered %d owned file(s).\n", app, placement, written.Owned)
	fmt.Fprint(out, `
The pull request now carries 2-net/applications.auto.tfvars and 1-org/applications.auto.tfvars:
the hostnames, onto the backends the stack created, and the stack's check trigger as a
required check of the repository, which the first apply made. After its merge and apply:
bedrock domain check, which passes when the apps domain resolves to its zone.
`)

	return nil
}

// registrationReady refuses the registration's next step while the state it needs is not
// there: the grants need the application's identities in 2-env's state in every
// environment (the apply run after the first pull request), and the completion needs the
// stack's state in every environment (its first applies by hand). An application the
// placement does not list or has registered in full is refused by Advance.
func (d deps) registrationReady(ctx context.Context, p *org.Placement, app string) error {
	step := p.Step(app)
	if step == "" || !p.Registered(app) {
		return nil
	}
	store, err := d.deploy.Storage(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	for _, env := range org.Environments {
		switch step {
		case org.RegistrationIdentities:
			if err := identitiesInState(ctx, store, p.StateBucket, env, app); err != nil {
				return err
			}
		case org.RegistrationGrants:
			if err := stackInState(ctx, store, p.StateBucket, env, app); err != nil {
				return err
			}
		}
	}

	return nil
}

// tofuState is the part of a state file the registration reads: its outputs by name.
type tofuState struct {
	Outputs map[string]struct {
		Value json.RawMessage `json:"value"`
	} `json:"outputs"`
}

// readTofuState reads one state object; absent, it answers no outputs and no error.
func readTofuState(ctx context.Context, store deploy.Store, bucket, object string) (*tofuState, error) {
	data, err := store.Read(ctx, bucket, object)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &tofuState{}, nil
		}

		return nil, errors.Wrapf(err, "reading gs://%s/%s", bucket, object)
	}
	var state tofuState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): gs://%s/%s", bucket, object)
	}

	return &state, nil
}

// identitiesInState refuses while 2-env's state in the environment lacks the
// application's identities.
func identitiesInState(ctx context.Context, store deploy.Store, bucket, env, app string) error {
	state, err := readTofuState(ctx, store, bucket, envStatePrefix+env+"/"+stateObject)
	if err != nil {
		return err
	}
	var applications map[string]json.RawMessage
	if raw, ok := state.Outputs[applicationsOutput]; ok {
		if err := json.Unmarshal(raw.Value, &applications); err != nil {
			return errors.Wrapf(err, "json.Unmarshal(): 2-env %s's %s output", env, applicationsOutput)
		}
	}
	if _, ok := applications[app]; !ok {
		return errors.Newf("%s's identities are not in 2-env %s's state yet: merge the registration's pull request and start the apply trigger with 2-env (the sequence's step 2) before moving on", app, env)
	}

	return nil
}

// stackInState refuses while the application's stack has no state with outputs in the
// environment.
func stackInState(ctx context.Context, store deploy.Store, bucket, env, app string) error {
	state, err := readTofuState(ctx, store, bucket, appStatePrefix+app+"/"+env+"/"+stateObject)
	if err != nil {
		return err
	}
	if len(state.Outputs) == 0 {
		return errors.Newf("%s's stack has no state in %s yet (%s%s/%s): its first apply there is by hand (the sequence's step 4) before moving on", app, env, appStatePrefix, app, env)
	}

	return nil
}

// firstPlacement is the application's first placement: every field from the
// organization's placement, the pin the bedrock running register.
func (d deps) firstPlacement(ctx context.Context, p *org.Placement, app, repository string) (*derive.Placement, error) {
	version, sum, err := d.firstPin(ctx)
	if err != nil {
		return nil, err
	}

	return p.ApplicationPlacement(app, repository, version, sum)
}

// checkoutRepository is the application's repository name, read from the module path
// of the go.mod at the checkout's root: a module under github.com/<organization> is its
// repository, github.com/<organization>/<repository>, and a module anywhere else (a
// vanity import path, another organization, a directory inside a repository) is refused.
func checkoutRepository(checkout, organization string) (string, error) {
	info, err := os.Stat(checkout)
	if err != nil || !info.IsDir() {
		return "", errors.Newf("no directory at %s: the second argument is the application's checkout, whose go.mod names its repository and where register writes its first placement (infrastructure/placement.json)", checkout)
	}
	file := filepath.Join(checkout, "go.mod")
	data, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.Newf("no go.mod at %s: the application's module at the checkout's root names its repository", checkout)
		}

		return "", errors.Wrap(err, "os.ReadFile()")
	}
	f, err := modfile.ParseLax(file, data, nil)
	if err != nil {
		return "", errors.Wrap(err, "modfile.ParseLax()")
	}
	if f.Module == nil {
		return "", errors.Newf("%s has no module directive to name the repository", file)
	}
	module := f.Module.Mod.Path
	parts := strings.Split(module, "/")
	if len(parts) != 3 || parts[0] != "github.com" || !strings.EqualFold(parts[1], organization) {
		return "", errors.Newf("the module path %q (%s) is not a repository of the %s GitHub organization (github.com/%s/<repository>): a vanity import path is not supported", module, file, organization, organization)
	}

	return parts[2], nil
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
		return "", errors.Newf("%s exists: register writes an application's first placement and never overwrites one", file)
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
		fmt.Fprintf(out, "\nWrote %s, %s's first placement, pinned to bedrock %s: commit it in the application's repository, where bedrock render writes the stack beside it and refuses a migrations directory that breaks the sequence rule, so a generator's mistake never reaches the default branch. %s\n", file, app, first.BedrockVersion, defaults)

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
layers it touches, in layer order). placement.json records how far the registration has gone
(registration), the layers render what that step may apply, and every pull request carries
every rendered file:
  1. This render, %s at its identities: its repository (private, squash the only merge,
     its rulesets and Environments; a repository made before this is imported first,
     1-org/README.md) and its registry repository, then, in tst, stg and prd, its
     identities, database, repository link and triggers.
  2. The apply trigger by hand with 2-env, in a terminal:
     %s
     tst and stg grant the next environment's deploy identity read on their records
     bucket, from state the first pass did not have.
  3. bedrock org register %s --next, then the pull request it renders: the public-invoker
     tag (1-org), the registry grants (2-shr) and the database admins (2-spn), on
     identities that exist now.
  4. %s's own stack, rendered in its repository (bedrock render) from its first
     placement.json (below), applied for the first time in each environment through its
     operations workflow (Actions, operations, apply, with the environment: the stack
     trigger 2-env made there applies it as the apply identity); every later apply is
     its pipeline's, with each release.
  5. bedrock org register %s --next once more, then the pull request it renders: the
     hostnames (2-net), onto the backends the stack created, and the stack's check trigger
     as a required check of the repository (1-org), which the first apply made; until then
     the repository's pull requests merge on the CI jobs alone.
  6. After 2-net's apply: bedrock domain check, which passes when the apps domain resolves to its zone.
`, p.GithubDefaultBranch, app, triggerRun(p, ""), app, app, app)
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
lock. A layer with no state yet (never applied: its plan identity reads the bucket and finds
no state object) is not planned, and passes as planned after its first apply. Once every plan
has finished each gets its verdict: its Plan: line when it succeeded; passed, as planned after
the layers before it apply, when it failed only because it reads outputs their plans create,
since the merge applies those first; failed otherwise, a plan that did not run included. One comment per layer goes on the pull request as the deployer
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

// The destroy command, its flag that stands in for the prefix typed, and the journal
// the command writes its line to.
const (
	destroyCommand = "destroy"
	yesFlag        = "yes"
	journalFile    = "JOURNAL.md"
	netKey         = "net"
)

// projectContents says what each shared project of the organization holds, by the key
// the placement records it under, for the destroy's warning; an environment's is said
// from its name.
var projectContents = map[string]string{
	"boot": "the layer identities, the state bucket with every layer's and every application's state and records, the Cloud Build connection and triggers, the GitHub Apps' keys",
	"shr":  "the image registry with every application's images",
	"spn":  "the shared Spanner instance with every application's stg and prd databases",
	netKey: "the load balancer, its address and certificate, the apps domain's zone",
	"log":  "the audit logs",
}

func newOrgDestroy(d deps) *cobra.Command {
	var dir string
	var yes bool

	cmd := &cobra.Command{
		Use:   destroyCommand,
		Short: "Delete the organization's projects, and with them everything the layers and the applications made",
		Long: `destroy deletes every project of the organization, by a person at their own computer with their
own sign-in (gcloud auth application-default login as the bootstrap administrator, with the roles
0-bootstrap/README.md step 1 lists; GITHUB_TOKEN, or gh's token, for 1-org's GitHub provider:
the infrastructure GitHub App's installation token, or an organization owner's). It says what
goes, project by project, and asks for the prefix typed back before anything runs (--yes stands
in for it where there is no terminal, as in a disposable organization's scripts). Then it
destroys 1-org, the GitHub repositories taken out of its state first so that they stay with
their code, pull requests and releases (the rulesets, the operations environments, the runner
group and the organization variable go), an apply with destroy=true recording the projects'
deletion policy, that no API is disabled first, and the folders' protection lifted, and the destroy tried again after a
pause while it fails (a folder refuses for a while after its last project's deletion); then
0-bootstrap the same way, on a local copy of its state, since the state bucket is in the boot
project and goes with it. Google keeps a deleted project for 30 days, in which the console can
restore it; its id never returns after. A layer whose state holds nothing is skipped. When 2-net
registers domains (registrations in 2-net/terraform.tfvars) the network project is kept, with
its folder and the organization's, since a registration cannot move to another project: that
project is a person's to deal with (1-org/README.md, "Removing a project"). It refuses a layer
rendered by an earlier bedrock, which declares no destroy variable. It ends with a line in
JOURNAL.md and what starting over takes.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return d.destroyOrg(cmd, dir, yes)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")
	cmd.Flags().BoolVar(&yes, yesFlag, false, "go on without the prefix typed back, where there is no terminal to type it at")

	return cmd
}

// destroyOrg is the destroy: the warning, the prefix typed back, the two layers, the
// journal line and what is next.
func (d deps) destroyOrg(cmd *cobra.Command, dir string, yes bool) error {
	out := cmd.OutOrStdout()
	p, err := orgPlacement(dir, "")
	if err != nil {
		return err
	}
	registered, err := domain.RegisteredDomains(dir)
	if err != nil {
		return err
	}
	keepNet := len(registered) > 0
	fmt.Fprint(out, destroyWarning(p, registered))
	if !yes {
		if !d.interactive() {
			return errors.Newf("bedrock org %s asks for the prefix typed back at the terminal, and this run has none: --%s stands in for it", destroyCommand, yesFlag)
		}
		typed, err := d.asker(cmd).Line(fmt.Sprintf("Type the prefix (%s) to go on; anything else stops:", p.Prefix))
		if err != nil {
			return err
		}
		if typed != p.Prefix {
			fmt.Fprintln(out, "Stopped: nothing was deleted.")

			return nil
		}
	}
	var exec deploy.Runner
	if d.deploy != nil {
		exec = d.deploy.Exec
	}
	req := &layers.DestroyRequest{Dir: dir, GithubToken: d.githubToken, KeepNet: keepNet, Pause: layers.DestroyPause}
	if err := layers.Destroy(cmd.Context(), exec, req, out); err != nil {
		return err
	}
	if err := appendJournal(dir, destroyJournalLine(p, keepNet, time.Now().UTC())); err != nil {
		return err
	}
	fmt.Fprint(out, destroyEpilogue(p, keepNet))

	return nil
}

// destroyWarning says what the destroy deletes: each project the placement records
// with what it holds, what else goes, what stays, and the grace Google gives.
func destroyWarning(p *org.Placement, registered []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "bedrock org destroy deletes every project of the %s organization, and with them everything the layers and the applications made:\n\n", p.Prefix)
	for _, key := range destroyedProjectKeys(p) {
		if len(registered) > 0 && key == netKey {
			continue
		}
		fmt.Fprintf(&b, "  %s (%s): %s\n", p.Projects[key], key, projectContentsOf(key))
	}
	b.WriteString("\nAlso gone: the folders, the custom roles and the tag, the org policies, the entitlements, the audit log sink, and on GitHub the rulesets, the operations environments, the runner group and the organization variable. The repositories stay, with their code, pull requests and releases.\n")
	if len(registered) > 0 {
		fmt.Fprintf(&b, "\nKept: the network project %s, with its folder and the organization's, since 2-net registers %s there (registrations in 2-net/terraform.tfvars) and a registration cannot move to another project; it is a person's to deal with (1-org/README.md, \"Removing a project\").\n", p.Projects[netKey], strings.Join(registered, ", "))
	}
	b.WriteString("\nGoogle keeps a deleted project for 30 days, in which the console can restore it; its id never returns after. Nothing here is undone by bedrock.\n\n")

	return b.String()
}

// destroyedProjectKeys is every project key the placement records, the known ones in
// their order and the rest after them.
func destroyedProjectKeys(p *org.Placement) []string {
	var keys []string
	for _, key := range org.ProjectKeys {
		if _, ok := p.Projects[key]; ok {
			keys = append(keys, key)
		}
	}
	var rest []string
	for key := range p.Projects {
		if !slices.Contains(keys, key) {
			rest = append(rest, key)
		}
	}
	slices.Sort(rest)

	return append(keys, rest...)
}

// projectContentsOf says what a project holds: a shared project's from the table, an
// environment's from its name (the first environment, tst, holds its own Spanner
// instance; the others' databases are on the shared one), or that it is the project
// under its key.
func projectContentsOf(key string) string {
	if contents, ok := projectContents[key]; ok {
		return contents
	}
	if slices.Contains(org.Environments, key) {
		contents := fmt.Sprintf("every application's %s services, jobs, secrets, records and files", key)
		if key == org.Environments[0] {
			contents += fmt.Sprintf(", and the %s Spanner instance", key)
		}

		return contents
	}

	return "the " + key + " project's contents"
}

// destroyJournalLine is the line the destroy adds to the journal.
func destroyJournalLine(p *org.Placement, keepNet bool, now time.Time) string {
	var ids []string
	for _, key := range destroyedProjectKeys(p) {
		if keepNet && key == netKey {
			continue
		}
		ids = append(ids, p.Projects[key])
	}
	line := fmt.Sprintf("- %s: bedrock org destroy deleted the organization's projects (%s): Google holds them for 30 days, their ids never return. The GitHub repositories stay.", now.Format("2006-01-02 15:04Z"), strings.Join(ids, ", "))
	if keepNet {
		line += fmt.Sprintf(" The network project %s was kept with its registrations, for a person.", p.Projects[netKey])
	}

	return line + "\n"
}

// appendJournal adds the line to the repository's journal, creating it when absent.
func appendJournal(dir, line string) error {
	f, err := os.OpenFile(filepath.Join(dir, journalFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.Wrap(err, "os.OpenFile(): the journal")
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return errors.Wrap(err, "os.File.WriteString(): the journal")
	}

	return nil
}

// destroyEpilogue says what the destroy left and what starting over takes.
func destroyEpilogue(p *org.Placement, keepNet bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nDeleted the %s organization's projects; a line went into %s. Google holds them for 30 days (their ids never return); the GitHub repositories are as they were, without the rulesets, environments and runners 1-org made.\n", p.Prefix, journalFile)
	if keepNet {
		fmt.Fprintf(&b, "The network project %s stays with its registrations, for a person.\n", p.Projects[netKey])
	}
	b.WriteString("To start over: a new seed (0-bootstrap/README.md, \"First-time setup\"), bedrock org render on its values, and the hand steps again (bedrock org new prints them).\n")

	return b.String()
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
		c.Builds, c.Store, c.StoreAs, c.Secrets, c.GitHub, c.Exec = d.deploy.Builds, d.deploy.Storage, d.deploy.StorageAs, d.deploy.Secrets, d.deploy.GitHub, d.deploy.Exec
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
