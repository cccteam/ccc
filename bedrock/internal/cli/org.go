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

	"github.com/cccteam/ccc/bedrock/internal/create"
	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/domain"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/layers"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/release"
	"github.com/cccteam/ccc/bedrock/internal/render"
	rootorg "github.com/cccteam/ccc/bedrock/internal/root"
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
		Long: `org holds the commands of an organization's infrastructure repository, rendered from the
organization's placement, placement.json at the repository root, the way an application's
stack is rendered from its code. A root organization, the one thing made by hand, is made by
one command (bootstrap) as the break-glass account and kept true by its two Cloud Build
triggers, which compare the organization with the placement and make it so, no terraform
(plan on a pull request, apply on the merge); it holds the GitHub Apps' keys, the zones of
the domains delegated to it and the hosting organizations' records, and its apply creates
each hosting organization recorded (create). A hosting organization is the six layers of the
CCC provisioning model (0-bootstrap, 1-org, 2-shr, 2-spn, 2-net, 2-env), which its two
triggers in the boot project plan and apply (the same plan and apply). check compares the
committed files with what the placement renders, and preflight checks, before the bootstrap,
that the break-glass account holds the roles it needs.`,
	}
	cmd.AddCommand(newOrgBootstrap(d), newOrgNew(d), newOrgPreflight(d), newOrgRender(d), newOrgCheck(d), newOrgRegister(d), newOrgPlace(d), newOrgCreate(d), newOrgPlan(d), newOrgApply(d), newOrgTeardown(d), newOrgServe(d), newOrgHostnames(d), newOrgRetire(d), newOrgDestroy(d), newOrgSecrets(d))

	return cmd
}

func newOrgNew(d deps) *cobra.Command {
	var placement string

	cmd := &cobra.Command{
		Use:   "new <dir>",
		Short: "Render an organization foundation for an organization that has none",
		Long: `new renders a hosting organization's infrastructure repository into a new or empty directory
from its placement: the six layers of the CCC provisioning model, each with its .tf files, its
README and its seeded terraform.tfvars, and at the root the README, the journal, the ignore
rules and the OpenTofu version. 0-bootstrap carries the two Cloud Build triggers, in the boot
project, that plan the layers on a pull request and apply them on the merge, running bedrock
org plan and bedrock org apply. It then prints the hand steps, each opening with the place
it happens: a hosting organization is created by its root organization (bedrock org create
there, then the merge), so its steps are what comes before the record (the team groups) and
after the creation (the spend budget, the consent screen, the role groups). From then on the
triggers plan every layer on a pull request and apply it on the merge, as the layer's own
identity. A root organization is not rendered here: bedrock org bootstrap makes it, placement
and files, in one run.

The placement is read from --placement, or from placement.json in the directory. The .tf
files and the READMEs are owned: org render rewrites them, and org check compares them.
Everything the creation decides (the boot project's suffix, the state bucket, the folders) is
REPLACEME in the seeded values until bedrock org create and the creation record it. The
triggers' runs fetch the bedrock
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
			if p.IsRoot() {
				return errors.Newf("%s is a root organization's placement (kind), and a root organization is made by bedrock org bootstrap, which writes its record and renders its files; org new renders a hosting organization's foundation, which bedrock org create records in the root organization's repository", placement)
			}
			out := cmd.OutOrStdout()
			if err := d.pinOrg(cmd.Context(), p, placement, out); err != nil {
				return err
			}
			files, err := org.Render(p)
			if err != nil {
				return err
			}
			written, err := org.Write(p, files, dir)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Rendered the %s foundation into %s: %d owned file(s) in %s; seeded %s.\n",
				p.Prefix, dir, written.Owned, strings.Join(p.Layers(), ", "), strings.Join(written.Seeded, ", "))
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

// handSteps says what a person does before and after a hosting organization's creation,
// and where. Each step opens with the place it happens. A root organization has no hand
// steps here: bedrock org bootstrap makes it and says what follows.
func handSteps(p *org.Placement) string {
	return fmt.Sprintf(`
A hosting organization is created by its root organization (%s, root.organization in
placement.json), from that organization's repository, by pull request; nothing here is seeded
or applied by hand (README.md, "Created by the root organization"). By hand, each where it
happens:
  0. In the Workspace Admin console, before the record: one team group per environment, named
     in placement.json (teamGroups: a group's address each; production's is not the first
     environment's), whose members approve the environment's releases and ask for its
     entitlements; nothing else names a person. For each application, before its first
     sign-in, its role groups (<group prefix><role>@%s), each created with "Who can view
     members" set so that its members can: the sign-in reads a person's groups with the
     person's own token, and Google leaves out a group whose member list the person may not
     view.
     On GitHub, nothing: the organization %s, its Bot Account (%s), the release, deployer
     and infrastructure apps with their keys and the Cloud Build authorization are the root
     organization's, recorded in its placement.json, and this placement names the same
     (githubOrganization, githubBotAccount, the three App IDs, cloudBuildInstallationId,
     cloudBuildTokenSecretVersion, root), filled in by the creation. The apps' keys live in
     the root organization's root project %s, one secret per app, and every run here reads
     each secret's alias current, which that organization pins (its README.md, "The GitHub
     Apps' keys"). No key reaches GitHub.
  1. In a terminal, in the root organization's repository: bedrock org create %s, which
     writes the record (organizations/%s/placement.json) for what is this organization's own
     to decide (appsDomain, spanner.config, two regions with the primary the root
     organization's, teamGroups, contactDomains, entitlementDurations, labels,
     vanityDomains); run again, it fills in what the root organization supplies, chooses the
     boot project's suffix and lists the organization in that placement.json
     (hostingOrganizations); then the pull request, whose plan says what the merge creates.
  2. The merge, as the root organization's creating identity: the organization's folder at
     the organization root (%s), the terraform folder under it, the boot project
     %s-boot-gbl-core-<suffix>, the boot identity %s-boot-gbl-tofu with its roles on the
     organization's folder and on the boot project, the break-glass account's (%s) Folder Admin on it and the state
     bucket %s-boot-gbl-state-<suffix>; the first applies of 0-bootstrap and 1-org against
     the bucket (the connection from the recorded authorization, the repository link and the
     two triggers; the folders, the projects and their identities, recorded in
     placement.json as projects and projectNumbers); the repository %s/%s with the rendered
     foundation as its first push, which its apply trigger runs on, every layer as its own
     identity; then the creating identity's grants off everything it made, and a pull
     request in the root organization's repository that records the folder. Nothing of the
     organization's sits at the organization node, and nobody applies by hand.
  3. In the Billing console, as a billing administrator of %s: the spend budget on the
     account (0-bootstrap/README.md, "The spend budget"); nothing renders it. The boot and
     org identities (%s-boot-gbl-tofu, %s-org-gbl-tofu) hold Billing Account User on it
     already, granted by the creation, for the projects 1-org links.
  4. In each environment project's Google Cloud console, before the first application's
     OAuth client is made there: the consent screen (APIs & Services, OAuth consent
     screen), with the audience Internal, the organization's own users.
From then on the two Cloud Build triggers in the boot project (%s and %s)
apply every layer: a pull request plans the layers it touches as their plan identities and
posts the plans on it, the merge applies them as their apply identities, in layer order. The
shared layers, 2-env per environment and the applications' registrations go through them; a
person applies by hand for recovery alone, as the break-glass account with the Folder Admin
the creation gave it (0-bootstrap/README.md, "Recovery, by hand"). bedrock org check lists
any grant on the organization's folder to a principal outside the organization.
After the first apply of 2-net: bedrock domain check prints what the apps domain, and each vanity domain, still needs, and where.
`, p.RootOrganization(), p.OrganizationDomain, p.GithubOrganization, p.GithubBotAccount, p.RootProject(), p.Prefix, p.Prefix,
		p.OrganizationID, p.Prefix, p.Prefix, p.BreakGlassAccount, p.Prefix, p.GithubOrganization, p.SourceRepo,
		p.BillingAccount, p.Prefix, p.Prefix, p.PlanTriggerName(), p.ApplyTriggerName())
}

// hostingOnly refuses a command that acts on applications or environments in a root
// organization, which has neither.
func hostingOnly(p *org.Placement, command string) error {
	if !p.IsRoot() {
		return nil
	}

	return errors.Newf("the %s organization is a root organization (kind in placement.json), which holds the GitHub Apps' keys, the zones of the domains delegated to it and the hosting organizations' records, no applications and no environments: bedrock org %s has nothing to act on here; the names under its domains are handed onward in placement.json (domains), and bedrock domain check reads them", p.Prefix, command)
}

// rootOnly refuses a hosting organization for the commands that are a root organization's:
// its root organization creates it, so nothing is bootstrapped by hand for it.
func rootOnly(p *org.Placement, command string) error {
	if p.IsRoot() {
		return nil
	}

	return errors.Newf("the %s organization is a hosting organization (kind in placement.json, left out), which its root organization creates from its record as the creating identity bedrock org bootstrap granted: bedrock org %s is a root organization's command, run in its repository; nothing is bootstrapped by hand for a hosting organization", p.Prefix, command)
}

func newOrgPreflight(d deps) *cobra.Command {
	var (
		dir       string
		placement string
	)

	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Check that these credentials hold the roles bedrock org bootstrap needs",
		Long: `preflight asks Google which of the permissions bedrock org bootstrap needs the caller holds,
with the run's Application Default Credentials (gcloud auth application-default login, as the
break-glass account): on the organization the placement names (organizationId, through Cloud
Resource Manager's testIamPermissions) and on its billing account (billingAccount, through
Cloud Billing's). It prints one line per role, holds or missing, naming the permissions
tested: Organization Administrator and Folder Creator at the organization node, Billing
Account Administrator on the billing account (README.md, "Bootstrapped by one command"). The
bootstrap runs the same check before it touches anything; this command repeats it in a root
organization's repository, before a destroy or when a grant is in doubt. A hosting
organization has no preflight: its root organization creates it. It exits 1 when any role is
missing, saying who grants it where, and when the permissions could not be checked (no
credentials, or an API that refused to answer). Run from the repository root, or name it
with --dir.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := orgPlacement(dir, placement)
			if err != nil {
				return err
			}
			if err := rootOnly(p, "preflight"); err != nil {
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
		fmt.Fprintf(&b, "0-bootstrap renders the Cloud Build connection and the triggers once placement.json records %s, the root organization's Cloud Build authorization, which the creation copies from its placement (0-bootstrap/README.md, \"Created by the root organization\").\n", strings.Join(unset, " and "))
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
			written, err := org.Write(p, files, dir)
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
so and what is left to do, also without failing the check. While the placement declares
more databases on a Spanner instance than it holds (the registered applications, one
each in every environment on the instance, the shared one or an environment's own,
against ten for every hundred processing units the layers declare), or the applications'
databases take all of the integration environment's instance and leave no pull-request
stack room for a database of its own, it says so, without failing the check: an
application bedrock destroy took down still stands in the placement, and the check gates
every plan and apply; bedrock org register refuses the registration that would exceed an
instance. While an application is out of service in every environment (outOfService), it
says that bedrock org retire <app> is the step that lets its entry go once its stacks are
destroyed, without failing the check. It then lists each grant a hand apply left a person
with, which the person removes once the triggers apply again (0-bootstrap/README.md,
"Recovery, by hand"): each person holding roles/owner on a project the placement records, which Google
gives a project's creator; each person holding Folder Admin or Folder Editor on a folder
under the organization's, which Google gives a folder's creator; and each role the
break-glass account holds on a project or on the organization's folder beyond the one it
keeps, Folder Admin on the organization's folder, its key for a recovery by hand. That
listing reads the projects' and folders' IAM policies with the run's Google credentials
(gcloud auth application-default login); without any it says so, and it never fails the
check. Last it lists each API key in an environment project that carries no API
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

			return d.writeOrgCheck(cmd, p, dir, r)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the repository root)")

	return cmd
}

// writeOrgCheck prints what org check found: the registrations, the notices, each
// finding, the leftover grants and API key listings, and the verdict. The registrations,
// the capacity and retire notices and the key listing are a hosting organization's
// alone: a root organization registers no domain and has no Spanner instance, no
// application and no environment project; its folder and boot project are read for
// leftover grants like a hosting organization's.
func (d deps) writeOrgCheck(cmd *cobra.Command, p *org.Placement, dir string, r *org.Report) error {
	out := cmd.OutOrStdout()
	if !p.IsRoot() {
		registrationReport(cmd.Context(), d, p, dir, out)
	}
	fmt.Fprint(out, zoneReplacementNotice(p))
	if !p.IsRoot() {
		fmt.Fprint(out, capacityNotice(p, dir))
		fmt.Fprint(out, retireNotice(p))
	}
	for _, f := range r.Findings {
		switch {
		case f.Reason != "":
			fmt.Fprintf(out, "%s: %s\n", f.Path, f.Reason)
		case f.Missing:
			fmt.Fprintf(out, "%s: missing\n", f.Path)
		default:
			fmt.Fprintf(out, "%s: line %d differs\n  want: %s\n  got:  %s\n", f.Path, f.Line, f.Want, f.Got)
		}
	}
	for _, path := range r.Unseeded {
		fmt.Fprintf(out, "%s: not seeded yet (org render writes it)\n", path)
	}
	leftoverReport(cmd.Context(), d, p, out)
	if p.IsRoot() {
		rootEngineReport(cmd.Context(), d, p, dir, out)
	} else {
		keyReport(cmd.Context(), d, p, out)
	}
	if !r.Clean() {
		fmt.Fprintf(out, "%d finding(s) against the %d owned file(s) the placement renders\n", len(r.Findings), r.Checked)

		return exitError{code: 1}
	}
	fmt.Fprintf(out, "%s: %d owned file(s) match the placement\n", dir, r.Checked)

	return nil
}

// rootEngineReport compares a root organization with Google Cloud and GitHub through the
// engine the triggers run, and prints what the apply would change, the findings and the
// notes. It is information, like the leftover grants: the check gates the triggers' runs
// on the render alone, and the plan says the same on the pull request. Without the
// clients, or when the comparison fails, it says so.
func rootEngineReport(ctx context.Context, d deps, p *org.Placement, dir string, out io.Writer) {
	clients := d.rootOrg()
	if clients == nil {
		fmt.Fprintln(out, "The root organization was not compared with Google Cloud: no clients are wired.")

		return
	}
	token, _ := d.rootGitHubToken(ctx, p)
	report, err := rootorg.Run(ctx, clients, &rootorg.Request{Placement: p, Dir: dir, Mode: rootorg.Check, GitHubToken: token}, out)
	if err != nil {
		fmt.Fprintf(out, "The root organization was not compared with Google Cloud: %v\n", errors.Cause(err))

		return
	}
	report.Write(out, rootorg.Check)
}

// rootGitHubToken is the token a root organization's check reads GitHub with: the
// infrastructure app's, minted from its key when the credentials read it (a trigger's
// runner), else the account's own (GITHUB_TOKEN, or gh's), else none, with why.
func (d deps) rootGitHubToken(ctx context.Context, p *org.Placement) (token, why string) {
	var reasons []string
	if d.deploy != nil && d.deploy.Secrets != nil {
		token, err := rootorg.AppToken(ctx, &rootorg.RunClients{Secrets: d.deploy.Secrets}, d.rootOrg().GitHub, p, org.InfrastructureApp)
		if err == nil {
			return token, ""
		}
		reasons = append(reasons, fmt.Sprintf("the infrastructure app's key: %v", errors.Cause(err)))
	}
	if d.githubToken != nil {
		token, err := d.githubToken(ctx)
		if err == nil && token != "" {
			return token, ""
		}
		if err != nil {
			reasons = append(reasons, errors.Cause(err).Error())
		}
	}

	return "", strings.Join(reasons, "; ")
}

// capacityNotice says, while the placement declares more databases on a Spanner instance
// than it holds (org.InstanceCapacities: the shared instance and each environment's own),
// what is declared and what the instance holds, and while the applications' databases
// take all of the integration environment's instance, that no pull-request stack has room
// for a database of its own there; nothing while every instance has room, and the
// reading's error when a seeded values file does not parse. It is information: an
// application bedrock destroy took down still stands in the placement and counts, and the
// check gates every plan and apply, so the check does not fail on it; bedrock org
// register refuses the registration that would exceed an instance.
func capacityNotice(p *org.Placement, dir string) string {
	capacities, err := p.InstanceCapacities(dir)
	if err != nil {
		return "Spanner capacity not counted: " + err.Error() + "\n"
	}
	var notice strings.Builder
	for _, c := range capacities {
		if c.Exceeded() {
			notice.WriteString("Spanner capacity: " + c.Problem() + "\n")
		}
	}
	if c, ok := org.IntegrationInstance(capacities); ok && !c.Exceeded() && c.Room() <= 0 {
		notice.WriteString("Spanner capacity: " + c.PullRequestRoom(org.Environments[0]) + "\n")
	}

	return notice.String()
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
	project, _ := p.AppsZoneName()
	r, err := domain.Registrations(ctx, open, domain.RegistrationsRequest{Dir: dir, Project: project, Now: time.Now()})
	if err != nil {
		fmt.Fprintf(out, "Registrations not checked (%v): %s.\n", errors.Cause(err), does)

		return
	}
	r.Write(out)
}

// leftoverReport lists each grant a hand apply left a person with: Owner on a
// project, which Google gives a project's creator; Folder Admin and Folder Editor on a
// folder under the organization's, which Google gives a folder's creator; and the roles
// the break-glass account holds on a project or on the organization's folder beyond the
// one it keeps there, Folder Admin, its key for a recovery by hand. The person removes
// them once the apply trigger applies the layers again (0-bootstrap/README.md, "Recovery,
// by hand"). It lists as well each grant on the organization's folder to a
// principal outside the organization, which nothing of the organization's makes: the
// creating identity of a root organization takes its own off at the creation's end. The
// read needs Google credentials; without any the report says so and what it would have
// done. Nothing here fails the check: the drift between the placement and the
// repository is the check's verdict, and the grants are a person's to remove.
func leftoverReport(ctx context.Context, d deps, p *org.Placement, out io.Writer) {
	const does = "org check lists each grant a hand apply left a person with (a person's roles/owner on a project, a person's Folder Admin or Folder Editor on a folder under the organization's, and the break-glass account's roles beyond its Folder Admin on the organization's folder) and each grant on the organization's folder to a principal outside the organization (neither the break-glass account nor an identity in one of its projects), when it runs with Google credentials that read the projects' and folders' IAM policies (gcloud auth application-default login)"
	if d.org == nil || d.org.policies == nil {
		fmt.Fprintf(out, "Leftover grants not checked: no IAM policy reader is wired; %s.\n", does)

		return
	}
	reader, err := d.org.policies(ctx)
	if err != nil {
		fmt.Fprintf(out, "Leftover grants not checked (%v): %s.\n", errors.Cause(err), does)

		return
	}
	defer reader.Close()
	leftovers, unrecorded, folderFound, err := org.Leftovers(ctx, p, reader)
	if err != nil {
		fmt.Fprintf(out, "Leftover grants not checked (%v): %s.\n", errors.Cause(err), does)

		return
	}
	if len(unrecorded) > 0 {
		fmt.Fprintf(out, "Leftover grants not checked in %s: the placement records no project there (projects).\n", strings.Join(unrecorded, ", "))
	}
	if !folderFound {
		fmt.Fprintf(out, "Leftover grants not checked on the folders: no folder named %s under organization %s (the creation makes it).\n", p.Prefix, p.OrganizationID)
	}
	if len(leftovers) == 0 {
		if folderFound || len(unrecorded) < len(org.Environments) {
			fmt.Fprintln(out, "No grant of a hand apply is left: nobody holds roles/owner on a project, nobody Folder Admin or Folder Editor on a folder under the organization's, the break-glass account holds Folder Admin on the organization's folder alone, and nothing on that folder is granted to a principal outside the organization.")
		}

		return
	}
	for _, l := range leftovers {
		if l.Outside {
			fmt.Fprintf(out, "%s: %s holds %s, and is outside this organization (not the break-glass account, not an identity in one of its projects); nothing of the organization's grants to an outsider on its folder, and a root organization's creating identity takes its own grants off at the creation's end, so this one is to remove, or to explain in JOURNAL.md.\n", l.Place(), l.Member, l.Role)

			continue
		}
		fmt.Fprintf(out, "%s: %s holds %s, %s; it is temporary, %s.\n", l.Place(), l.Member, l.Role, leftoverWhy(p, l), leftoverRemoval(p))
	}
}

// leftoverRemoval says what takes a leftover grant off: for a root organization the
// bootstrap run again, which takes off at its end what Google gave it as the creator;
// for a hosting organization the apply trigger's first applies of the layers.
func leftoverRemoval(p *org.Placement) string {
	if p.IsRoot() {
		return "removed by bedrock org bootstrap run again in a clone, which takes it off at its end"
	}

	return `removed once the apply trigger applies the layers (0-bootstrap/README.md, "Recovery, by hand")`
}

// leftoverWhy says where a leftover grant came from: in a root organization Google's
// grant to the bootstrap as the creator or the bootstrap's own working grant; in a
// hosting organization a creator's grant, or the seed's grant to the break-glass account
// for the first apply of the second layer.
func leftoverWhy(p *org.Placement, l org.Leftover) string {
	switch {
	case p.IsRoot() && (l.Role == org.OwnerRole || l.Role == org.FolderEditorRole):
		return "Google's grant to the creator, from the bootstrap"
	case p.IsRoot():
		return "the bootstrap's working grant"
	case l.Role == org.OwnerRole, l.On == org.OnFolder && (l.Role == org.FolderAdminRole || l.Role == org.FolderEditorRole):
		return "the creator's grant from the first applies by hand"
	case l.On == org.OnFolder && l.Key == p.Prefix:
		return "a grant for a hand apply of 1-org"
	default:
		return "a grant of a hand apply"
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

// registerLong is bedrock org register's help.
const registerLong = `register adds an application to the organization: its code, the organization's naming
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

The application is placed in every environment unless --environments names a prefix of
the promotion order (tst; tst,stg): a proof application that stays in the first
environment, say. Registration creates its things in those environments alone, the layers
render for it there alone, the capacity count counts the databases it has there, and its
release line ends at the last of them; its entry in placement.json records the list
(environments). bedrock org place changes it later, in either direction.

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
projectNumbers) and the creation's state bucket, which the application's placement names.
The root organization's Cloud Build authorization, which the creation copies into the
placement, comes before the first application: register refuses while placement.json leaves
cloudBuildInstallationId or cloudBuildTokenSecretVersion unset (0-bootstrap/README.md,
"Created by the root organization"), since the
applications' triggers exist once 2-env holds the connection and nothing is built by hand
before them. It also refuses a registration past a Spanner instance: the registered
applications, one database each in every environment on the instance (the shared one
holds stg's and prd's by default and the integration environment holds its own;
2-env/terraform.tfvars, spanner_instances, decides), counted against the databases the
instance holds at its size (processing_units in 2-spn/terraform.tfvars for the shared
instance, the environment's entry in spanner_instances for its own, 100 when unset; ten
for every hundred below a node, Google's rule), with the sentence naming the setting that
grows it and bedrock destroy of an application no longer needed, which frees its
databases. On the instance that holds the integration environment's databases every pull
request's stack holds a database of its own too (unless it shares the environment's), so
the registration says what the applications take there and what is left for them. A
refusal changes nothing.`

func newOrgRegister(d deps) *cobra.Command {
	var (
		dir          string
		placement    string
		next         bool
		environments []string
	)

	cmd := &cobra.Command{
		Use:   "register <code> <application checkout>",
		Short: "Register an application in the foundation and write its first placement",
		Long:  registerLong,
		Args:  cobra.RangeArgs(1, 2),
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
			if err := hostingOnly(p, "register"); err != nil {
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

			return d.registerFirst(cmd.Context(), cmd.OutOrStdout(), p, placement, dir, args, environments)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the repository root)")
	cmd.Flags().BoolVar(&next, "next", false, "move the application's registration to its next step: grants, then complete")
	cmd.Flags().StringSliceVar(&environments, "environments", nil, "the environments the application is placed in, a prefix of the promotion order (tst; tst,stg); every environment when left out")

	return cmd
}

// registerFirst registers the application at its identities: the placement written and
// rendered, the sequence printed, and the application's first placement written into
// the checkout named second, whose module path names the repository.
func (d deps) registerFirst(ctx context.Context, out io.Writer, p *org.Placement, placement, dir string, args, environments []string) error {
	app := args[0]
	if err := derive.ValidateApplicationCode(app); err != nil {
		return err
	}
	repository, err := checkoutRepository(args[1], p.GithubOrganization)
	if err != nil {
		return err
	}
	if err := p.Register(app, repository, environments); err != nil {
		return err
	}
	exists, err := d.repositoryExists(ctx, p.GithubOrganization, repository)
	if err != nil {
		return err
	}
	if exists {
		if err := p.Adopt(app); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s/%s exists on GitHub already: 1-org adopts the repository on its first apply (adopted in placement.json) instead of making it; a ruleset or Environment made on it by hand is imported by hand first (1-org/README.md).\n", p.GithubOrganization, repository)
	}
	if unset := p.ConnectionUnset(); len(unset) > 0 {
		return errors.Newf("placement.json leaves %s unset: the root organization's Cloud Build authorization, which the creation copies into this placement, comes before the first application (0-bootstrap/README.md, \"Created by the root organization\"); record both and register again", strings.Join(unset, " and "))
	}
	capacities, err := p.InstanceCapacities(dir)
	if err != nil {
		return err
	}
	var exceeded []string
	for _, c := range capacities {
		if c.Exceeded() {
			exceeded = append(exceeded, c.Problem())
		}
	}
	if len(exceeded) > 0 {
		return errors.Newf("register %s: %s", app, strings.Join(exceeded, "; "))
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
	written, err := org.Write(p, files, dir)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Registered %s under the code %s in %s at its identities, placed in %s; rendered %d owned file(s), the five applications.auto.tfvars and 1-org/public-invokers.auto.tfvars among them.\n", repository, app, placement, strings.Join(p.PlacedEnvironments(app), ", "), written.Owned)
	if c, ok := org.IntegrationInstance(capacities); ok {
		fmt.Fprintf(out, "%s.\n", c.PullRequestRoom(org.Environments[0]))
	}
	fmt.Fprint(out, triggerNotice(p))
	fmt.Fprint(out, applySequence(p, app, repository))

	return writeFirstPlacement(out, first, app, repository, target)
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
// carries and what follows its apply. The move to the grants reads GitHub first, the
// repository's workflow list and whether anything is pushed to it, since what follows
// that pull request is the stack's first apply, and how it starts depends on whether
// GitHub lists the operations workflow yet and, while it does not, on whether the first
// push is still to come; a repository that cannot be read moves nothing. The output names
// the application by its repository and keeps the code to the commands.
func (d deps) registerNext(ctx context.Context, out io.Writer, p *org.Placement, placement, dir, app string) error {
	repository := p.Repository(app)
	if err := d.registrationReady(ctx, p, app, repository); err != nil {
		return err
	}
	var start firstApply
	if p.Step(app) == org.RegistrationIdentities {
		var err error
		if start, err = d.firstApplyStart(ctx, p.GithubOrganization, repository); err != nil {
			return err
		}
	}
	under := p.UnderWay(app)
	step, err := p.Advance(app)
	if err != nil {
		return err
	}
	if step == "" {
		// The completion records the hostnames the stack serves in every environment
		// the step completes, read from its state, so 2-net's hosts render from what is
		// served and never from a rule; a hostname another application serves refuses
		// the step.
		if _, err := d.recordHostnames(ctx, p, app, repository, under); err != nil {
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
	written, err := org.Write(p, files, dir)
	if err != nil {
		return err
	}
	if step == org.RegistrationGrants {
		fmt.Fprintf(out, "Moved %s's registration to its grants in %s; rendered %d owned file(s).\n", repository, placement, written.Owned)
		fmt.Fprintf(out, `
The pull request now carries 1-org/public-invokers.auto.tfvars, 2-shr/applications.auto.tfvars
and 2-spn/applications.auto.tfvars: the public-invoker tag, the registry grants and the
database admins, on identities that exist now. After its merge and apply: %s's own stack,
applied for the first time in each environment%s`, repository, firstApplyInstruction(p, app, repository, start))

		return nil
	}
	fmt.Fprintf(out, "Completed %s's registration in %s in %s, its hostnames recorded from its stack's state (%s); rendered %d owned file(s).\n", repository, strings.Join(under, ", "), placement, hostnamesSummary(p, app), written.Owned)
	fmt.Fprint(out, `
The pull request now carries 2-net/applications.auto.tfvars and 1-org/applications.auto.tfvars:
the hostnames, onto the backends the stack created, and the stack's check trigger as a
required check of the repository, which the first apply made. After its merge and apply:
bedrock domain check, which passes when the apps domain and each vanity domain resolve to
their zones. A label or a vanity hostname the application changes later in its placement
(hostnames) is recorded again with bedrock org hostnames <app>.
`)

	return nil
}

// operationsWorkflowPath is the operations workflow's path in the repository, as
// GitHub's workflow list names it.
const operationsWorkflowPath = ".github/workflows/" + render.OperationsWorkflow

// firstApply is how the application's stack is applied for the first time, read from
// GitHub when the registration moves to its grants.
type firstApply int

const (
	// firstApplyWorkflow is the operations workflow's apply action: GitHub lists the
	// workflow.
	firstApplyWorkflow firstApply = iota
	// firstApplyAfterMerge is the operations workflow's apply action once the push that
	// merges the stack rendered at this bedrock lists the workflow: the repository has
	// commits and GitHub lists no operations workflow yet.
	firstApplyAfterMerge
	// firstApplyAfterPush is the application's first push, which lists the workflow, then
	// its apply action: the repository is empty.
	firstApplyAfterPush
)

// firstApplyStart reads from GitHub how the stack's first apply starts, which the
// repository's workflow list and whether anything is pushed to it decide: the operations
// workflow's apply action when GitHub lists the workflow; the same action once the push
// that merges the stack rendered at this bedrock lists it, when the repository has commits
// and GitHub lists no workflow yet (the workflow runs its registered job on the push that
// adds or changes its file, which lists it; the push that creates a repository does not
// always list the files it carries on its own); and the application's first push first
// when the repository is empty. A list or a repository that cannot be read is a refusal,
// so the instruction printed is never a guess.
func (d deps) firstApplyStart(ctx context.Context, owner, repository string) (firstApply, error) {
	client, err := d.github(ctx)
	if err != nil {
		return 0, err
	}
	listed, err := operationsWorkflowListed(ctx, client, owner, repository)
	if err != nil {
		return 0, err
	}
	if listed {
		return firstApplyWorkflow, nil
	}
	empty, err := client.Empty(ctx, owner, repository)
	if err != nil {
		return 0, errors.Wrapf(err, "reading whether %s/%s is empty, which decides how the stack's first apply starts; nothing moved", owner, repository)
	}
	if empty {
		return firstApplyAfterPush, nil
	}

	return firstApplyAfterMerge, nil
}

// operationsWorkflowListed reports whether GitHub lists the repository's operations
// workflow.
func operationsWorkflowListed(ctx context.Context, client *github.Client, owner, repository string) (bool, error) {
	workflows, err := client.Workflows(ctx, owner, repository)
	if err != nil {
		return false, errors.Wrapf(err, "reading %s/%s's workflows, which decide how the stack's first apply starts; nothing moved", owner, repository)
	}
	for i := range workflows {
		if workflows[i].Path == operationsWorkflowPath {
			return true, nil
		}
	}

	return false, nil
}

// firstApplyInstruction says how the application's stack is applied for the first time in
// each environment, continuing the sentence "applied for the first time in each
// environment": through the operations workflow's apply action when GitHub lists the
// workflow; through the same action once the push that merges the rendered stack lists
// it, when the repository has commits and GitHub lists no workflow yet; and after the
// application's first push, which lists it, while the repository is empty. It ends with
// the register --next that follows.
func firstApplyInstruction(p *org.Placement, app, repository string, start firstApply) string {
	var b strings.Builder
	switch start {
	case firstApplyWorkflow:
		fmt.Fprintf(&b, " through its operations workflow, which GitHub lists: %s. Then\n", workflowApply(p, app, repository))
	case firstApplyAfterMerge:
		fmt.Fprintf(&b, `. GitHub does not list its operations workflow yet: the push to %s that adds or
changes the workflow's file (the merge of the stack rendered at this bedrock) runs the
workflow's registered job, which lists it, and the first apply then starts through the
workflow's apply action: %s. Then
`, p.GithubDefaultBranch, workflowApply(p, app, repository))
	case firstApplyAfterPush:
		fmt.Fprintf(&b, `. The repository is empty: the application's first push comes first, to %s, the default
branch the stack trigger builds, with its stack rendered there (bedrock render) from its
first placement.json. That push runs the operations workflow's registered job, which lists
the workflow, and the stack's first apply then starts through the workflow's apply action:
%s. Then
`, p.GithubDefaultBranch, workflowApply(p, app, repository))
	}
	fmt.Fprintf(&b, "bedrock org register %s --next.\n", app)

	return b.String()
}

// workflowApply is the operations workflow's apply action, from Actions or with gh, per
// environment under way in order: how the stack's first apply starts once GitHub lists
// the workflow.
func workflowApply(p *org.Placement, app, repository string) string {
	return fmt.Sprintf(`Actions,
operations, Run workflow, with the action apply and the environment, %s in that order (or
gh workflow run %s --repo %s/%s -f action=apply -f environment=<env>; its
infrastructure/README.md, Applying)`, strings.Join(p.UnderWay(app), ", "), render.OperationsWorkflow, p.GithubOrganization, repository)
}

// registrationReady refuses the registration's next step while the state it needs is not
// there: the grants need the application's identities in 2-env's state in every
// environment under way (the apply run after the first pull request), and the completion
// needs the stack's state in every environment under way (its first applies, as the
// grants step's output said). An application the placement does not list or has
// registered in full is refused by Advance. The refusals name the application by its
// repository.
func (d deps) registrationReady(ctx context.Context, p *org.Placement, app, repository string) error {
	step := p.Step(app)
	if step == "" || !p.Registered(app) {
		return nil
	}
	store, err := d.deploy.Storage(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	for _, env := range p.UnderWay(app) {
		switch step {
		case org.RegistrationIdentities:
			if err := identitiesInState(ctx, store, p.StateBucket, env, app, repository); err != nil {
				return err
			}
		case org.RegistrationGrants:
			if err := stackInState(ctx, store, p.StateBucket, env, app, repository); err != nil {
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
func identitiesInState(ctx context.Context, store deploy.Store, bucket, env, app, repository string) error {
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
		return errors.Newf("%s's identities are not in 2-env %s's state yet: merge the registration's pull request and start the apply trigger with 2-env (the sequence's step 2) before moving on", repository, env)
	}

	return nil
}

// recordHostnames reads the hostnames the application's stack serves in the environments
// given from its state (the net_hosts output) and records them in the placement,
// answering whether the record changed. A state without the output, or with none,
// refuses: the organization records what is served and invents nothing.
func (d deps) recordHostnames(ctx context.Context, p *org.Placement, app, repository string, envs []string) (bool, error) {
	store, err := d.deploy.Storage(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = store.Close() }()
	byEnv := make(map[string]map[string]string, len(envs))
	for _, env := range envs {
		hosts, err := stackHosts(ctx, store, p.StateBucket, env, app, repository)
		if err != nil {
			return false, err
		}
		byEnv[env] = hosts
	}

	return p.RecordHostnames(app, byEnv)
}

// hostsOutput is the stack's output listing the entries 2-net's hosts take for the
// environment: every site's hostnames and their -next twins, each mapped to the backend
// service serving it.
const hostsOutput = "net_hosts"

// stackHosts reads the hosts the application's stack serves in the environment from its
// state's net_hosts output.
func stackHosts(ctx context.Context, store deploy.Store, bucket, env, app, repository string) (map[string]string, error) {
	state, err := readTofuState(ctx, store, bucket, appStatePrefix+app+"/"+env+"/"+stateObject)
	if err != nil {
		return nil, err
	}
	raw, ok := state.Outputs[hostsOutput]
	if !ok {
		return nil, errors.Newf("%s's stack state in %s (%s%s/%s) has no %s output: apply the stack there (the operations workflow's apply action) before recording its hostnames", repository, env, appStatePrefix, app, env, hostsOutput)
	}
	var hosts map[string]string
	if err := json.Unmarshal(raw.Value, &hosts); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): %s's %s output in %s", repository, hostsOutput, env)
	}
	if len(hosts) == 0 {
		return nil, errors.Newf("%s's stack serves no hostname in %s: its state's %s output is empty", repository, env, hostsOutput)
	}

	return hosts, nil
}

// hostnamesSummary restates the recorded hostnames in one line, env: hosts, for the
// environments the application is placed in.
func hostnamesSummary(p *org.Placement, app string) string {
	placed := p.PlacedEnvironments(app)
	parts := make([]string, 0, len(placed))
	for _, env := range placed {
		parts = append(parts, env+": "+strings.Join(p.ApplicationHostnames(app, env), ", "))
	}

	return strings.Join(parts, "; ")
}

// stackInState refuses while the application's stack has no state with outputs in the
// environment.
func stackInState(ctx context.Context, store deploy.Store, bucket, env, app, repository string) error {
	state, err := readTofuState(ctx, store, bucket, appStatePrefix+app+"/"+env+"/"+stateObject)
	if err != nil {
		return err
	}
	if len(state.Outputs) == 0 {
		return errors.Newf("%s's stack has no state in %s yet (%s%s/%s): its first apply there comes first (the sequence's step 4, as the grants step's output said) before moving on", repository, env, appStatePrefix, app, env)
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
func writeFirstPlacement(out io.Writer, first *derive.Placement, app, repository, file string) error {
	defaults := "Approvals, maintenance windows, the build machine and instance caps are left to their defaults; production's maintenance window is the team's to write before its first breaking release."
	if file != "" {
		if err := derive.CreatePlacement(file, first); err != nil {
			return err
		}
		fmt.Fprintf(out, "\nWrote %s, %s's first placement, pinned to bedrock %s: commit it in the application's repository, where bedrock render writes the stack beside it and refuses a migrations directory that breaks the sequence rule, so a generator's mistake never reaches the default branch. %s\n", file, repository, first.BedrockVersion, defaults)

		return nil
	}
	data, err := derive.MarshalPlacement(first)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\n%s's first placement, pinned to bedrock %s: commit it in the application's repository as infrastructure/placement.json, where bedrock render reads it (bedrock org register %s <checkout> writes it there instead). %s\n%s", repository, first.BedrockVersion, app, defaults, data)

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
// Build triggers: the pull requests, in order, and the one run of the apply trigger by
// hand. It names the application by its repository and keeps the code to the commands.
func applySequence(p *org.Placement, app, repository string) string {
	return fmt.Sprintf(`
Register through the Cloud Build triggers, as pull requests into %[1]s (each merge applies the
layers it touches, in layer order). placement.json records how far the registration has gone
(registration), the layers render what that step may apply, and every pull request carries
every rendered file:
  1. This render, %[4]s at its identities: its repository (private, squash the only merge,
     its rulesets and Environments; a repository that exists already is adopted, not
     made) and its registry repository, then, in %[5]s, its
     identities, database, repository link and triggers.
  2. The apply trigger by hand with 2-env, in a terminal:
     %[3]s
     each environment but the last of them grants the next environment's deploy identity
     read on its records bucket, from state the first pass did not have.
  3. bedrock org register %[2]s --next, then the pull request it renders: the public-invoker
     tag (1-org), the registry grants (2-shr) and the database admins (2-spn), on
     identities that exist now. Its output says how step 4 starts.
  4. %[4]s's own stack, rendered in its repository (bedrock render) from its first
     placement.json (below) and pushed to %[1]s, applied for the first time in each
     environment through its operations workflow (Actions, operations, apply, with the
     environment: the stack trigger 2-env made there applies it as the apply identity),
     which the push that adds or changes its file lists, since the workflow runs its
     registered job on that push; step 3 prints the way that applies. Every later apply
     is its pipeline's, with each release.
  5. bedrock org register %[2]s --next once more, then the pull request it renders: the
     hostnames (2-net), onto the backends the stack created, and the stack's check trigger
     as a required check of the repository (1-org), which the first apply made; until then
     the repository's pull requests merge on the CI jobs alone.
  6. After 2-net's apply: bedrock domain check, which passes when the apps domain and each vanity domain resolve to their zones.
`, p.GithubDefaultBranch, app, triggerRun(p, ""), repository, strings.Join(p.PlacedEnvironments(app), ", "))
}

// repositoryExists asks GitHub whether the organization's repository exists, which decides
// whether 1-org adopts it or makes it: found is true, not found or a name that forwards to
// a renamed repository false, and a refusal to read is an error. Without a GitHub client
// wired (the tests), nothing is asked and the repository is taken as new.
func (d deps) repositoryExists(ctx context.Context, owner, repository string) (bool, error) {
	if d.github == nil {
		return false, nil
	}
	gh, err := d.github(ctx)
	if err != nil {
		return false, errors.Wrapf(err, "reading whether %s/%s exists on GitHub, which decides whether 1-org adopts the repository or makes it", owner, repository)
	}
	if _, err := gh.Repository(ctx, owner, repository); err != nil {
		if github.NotFound(err) || github.Moved(err) {
			return false, nil
		}

		return false, errors.Wrapf(err, "reading %s/%s on GitHub", owner, repository)
	}

	return true, nil
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
log while placement.json records no deployer app (githubDeployerAppId) or the key's alias
names no version yet. 1-org's plan gets the infrastructure GitHub App's token as
GITHUB_TOKEN, minted from the app's key, read by its alias in the root organization's boot
project. _LAYER names one layer to plan whatever the pull request changes, and _ENVIRONMENT one
environment of 2-env. It exits 1 when any plan failed. It refuses a build that did not start
from the trigger (no _BEDROCK_STEPS substitution) and a layer whose identity is under a
project placement.json does not record yet (REPLACEME), and says so when it runs under a
step list an earlier bedrock rendered: the trigger's steps lag one apply behind the
repository, and a release keeps working under the previous release's steps.

In a root organization's repository (kind root in placement.json) plan is the plan trigger's
run through the engine the bootstrap made the organization with: it compares the organization
(the APIs, the identities and their roles, the secrets' aliases and policies, the grants, the
zones and their record sets, the connection and the triggers, the repository's settings and
ruleset) with the pull request's placement.json, says what the merge would change and which
hosting organizations it would create, and posts that in one comment on the pull request as
the deployer app, updated on a later push. Nothing is changed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if root, err := isRootRepo(dir); err != nil {
				return err
			} else if root {
				return d.rootRun(cmd.Context(), dir, org.PlanCommand, cmd.OutOrStdout())
			}

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
app's key, read by its alias in the root organization's root project. It refuses a build that did
not start from the trigger (no _BEDROCK_STEPS substitution) and a layer whose identity is
under a project placement.json does not record yet (REPLACEME), and says so when it runs
under a step list an earlier bedrock rendered: the trigger's steps lag one apply behind the
repository, and a release keeps working under the previous release's steps.

In a root organization's repository (kind root in placement.json) apply is the apply trigger's
run through the engine: under a lock in the state bucket (locks/root) it makes the organization
as the pushed placement.json says, section by section, creates each hosting organization
recorded without a folder as the creating identity (its folder, boot project, first applies,
repository and hand-off pull request), and leaves the marker (applied/root). The outer grants
the bootstrap made (the creating identity's at the organization node and on the billing
account) and a zone a domain no longer names are reported, never changed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if root, err := isRootRepo(dir); err != nil {
				return err
			} else if root {
				return d.rootRun(cmd.Context(), dir, org.ApplyCommand, cmd.OutOrStdout())
			}

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
	netKey: "the load balancer, its address and certificates, the apps domain's zone and the vanity domains' zones",
	"log":  "the audit logs",
}

func newOrgDestroy(d deps) *cobra.Command {
	var dir string
	var yes bool

	cmd := &cobra.Command{
		Use:   destroyCommand,
		Short: "Delete the organization's projects, and with them everything the layers and the applications made",
		Long: `destroy deletes every project of the organization, by a person at their own computer with their
own sign-in (gcloud auth application-default login as the break-glass account, holding Owner on
the organization's folder for the run, which its Folder Admin there lets it grant itself;
GITHUB_TOKEN, or gh's token, for 1-org's GitHub provider:
the infrastructure GitHub App's installation token, or an organization owner's). It says what
goes, project by project, and asks for the prefix typed back before anything runs (--yes stands
in for it where there is no terminal, as in a disposable organization's scripts).

A root organization (kind root in placement.json) goes by API, as it was made: it is refused
while placement.json records a hosting organization as created (each is destroyed first, in
its own repository, and its entry taken out); then the creating identity's grants come off the
organization node and the billing account, the root project is deleted, and with it the
secrets, the zones, the connection, the triggers, the identities and the state bucket, and the
folder is deleted. The repository stays on GitHub with its code and pull requests, the three
GitHub Apps stay installed, and a bootstrap again brings them with their keys or makes new
ones.

For a hosting organization it
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
	var registered []string
	if p.IsRoot() {
		if err := rootorg.CanDestroy(p); err != nil {
			return err
		}
	} else {
		registered, err = domain.RegisteredDomains(dir)
		if err != nil {
			return err
		}
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
	if p.IsRoot() {
		clients := d.rootOrg()
		if clients == nil {
			return errors.New("no root organization clients are wired")
		}
		if err := rootorg.Destroy(cmd.Context(), clients, &rootorg.DestroyRequest{Placement: p}, out); err != nil {
			return err
		}
	} else {
		var exec deploy.Runner
		if d.deploy != nil {
			exec = d.deploy.Exec
		}
		req := &layers.DestroyRequest{Dir: dir, GithubToken: d.githubToken, KeepNet: keepNet, Pause: layers.DestroyPause}
		if err := layers.Destroy(cmd.Context(), exec, req, out); err != nil {
			return err
		}
	}
	if err := appendJournal(dir, destroyJournalLine(p, keepNet, time.Now().UTC())); err != nil {
		return err
	}
	fmt.Fprint(out, destroyEpilogue(p, keepNet))

	return nil
}

// rootProjectContents says what a root organization's root project holds, for the
// destroy's warning.
const rootProjectContents = "the runner identities and the creating identity, the state bucket with the hosting organizations' creation records, the Cloud Build connection and triggers, the GitHub Apps' keys and the Bot Account's token, and the zones with their record sets: every domain delegated to this organization then points at servers that answer nothing"

// destroyWarning says what the destroy deletes: each project the placement records
// with what it holds, what else goes, what stays, and the grace Google gives.
func destroyWarning(p *org.Placement, registered []string) string {
	var b strings.Builder
	if p.IsRoot() {
		fmt.Fprintf(&b, "bedrock org destroy deletes the root project of the %s root organization, and with it everything the bootstrap and the applies made:\n\n", p.Prefix)
		fmt.Fprintf(&b, "  %s (%s): %s\n", p.RootProject(), org.RootProjectKey, rootProjectContents)
		b.WriteString("\nAlso gone: the folder, and the creating identity's grants at the organization node and on the billing account. The repository stays on GitHub with its code, pull requests, settings and ruleset, and the three GitHub Apps stay installed.\n")
		b.WriteString("\nGoogle keeps a deleted project for 30 days, in which the console can restore it; its id never returns after. Nothing here is undone by bedrock.\n\n")

		return b.String()
	}
	fmt.Fprintf(&b, "bedrock org destroy deletes every project of the %s organization, and with them everything the layers and the applications made:\n\n", p.Prefix)
	for _, key := range destroyedProjectKeys(p) {
		if len(registered) > 0 && key == netKey {
			continue
		}
		fmt.Fprintf(&b, "  %s (%s): %s\n", p.Projects[key], key, projectContentsOf(key))
	}
	b.WriteString("\nAlso gone: the folders, the custom roles and the tags, the org policies, the entitlements, the audit log sink, and on GitHub the rulesets, the operations environments, the runner group and the organization variable. The repositories stay, with their code, pull requests and releases.\n")
	if len(registered) > 0 {
		fmt.Fprintf(&b, "\nKept: the network project %s, with its folder and the organization's, since 2-net registers %s there (registrations in 2-net/terraform.tfvars) and a registration cannot move to another project; it is a person's to deal with (1-org/README.md, \"Removing a project\").\n", p.Projects[netKey], strings.Join(registered, ", "))
	}
	b.WriteString("\nGoogle keeps a deleted project for 30 days, in which the console can restore it; its id never returns after. The Spanner instance and every backup on it (the release backups, the schedules' and the final backups bedrock destroy took) go with the Spanner project and come back with it in those days, and are gone after. Nothing here is undone by bedrock.\n\n")

	return b.String()
}

// destroyedProjectKeys is every project key the placement records, the known ones in
// their order and the rest after them.
func destroyedProjectKeys(p *org.Placement) []string {
	var keys []string
	for _, key := range p.ProjectKeys() {
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
	if p.IsRoot() {
		return fmt.Sprintf("- %s: bedrock org destroy deleted the root organization's root project (%s) and folder, with its zones: Google holds the project for 30 days, its id never returns. The GitHub repository and the apps stay.\n", now.Format("2006-01-02 15:04Z"), p.RootProject())
	}
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
	if p.IsRoot() {
		fmt.Fprintf(&b, "\nDeleted the %s root organization's root project and folder; a line went into %s. Google holds the project for 30 days (its id never returns); the GitHub repository is as it was, with its settings and ruleset, and the three GitHub Apps stay installed, their keys in nobody's hands until a bootstrap stores them again.\n", p.Prefix, journalFile)
		b.WriteString("To start over: bedrock org bootstrap <dir> again, in an empty directory, with the apps brought (--infrastructure-app and --infrastructure-key, and the others) or made anew; every domain delegated here is then delegated again, to the new zones' name servers.\n")

		return b.String()
	}
	fmt.Fprintf(&b, "\nDeleted the %s organization's projects; a line went into %s. Google holds them for 30 days (their ids never return); the GitHub repositories are as they were, without the rulesets, environments and runners 1-org made.\n", p.Prefix, journalFile)
	if keepNet {
		fmt.Fprintf(&b, "The network project %s stays with its registrations, for a person.\n", p.Projects[netKey])
	}
	b.WriteString("To start over: the organization's entry out of the root organization's placement.json (hostingOrganizations), then bedrock org create there again, which chooses a new boot project suffix, and its pull request (0-bootstrap/README.md, \"Destroying the organization\").\n")

	return b.String()
}

// layerRequest is the run's request: the build the trigger's step names in the
// environment, and the checkout.
func layerRequest(dir string) *layers.Request {
	return &layers.Request{BuildID: os.Getenv("BUILD_ID"), Project: os.Getenv("PROJECT_ID"), Location: os.Getenv("LOCATION"), Dir: dir}
}

// layerClients are what org plan and org apply reach the world through in a hosting
// organization: the deploy sequence's seams (Cloud Build, Cloud Storage, Secret Manager,
// GitHub, the programs a step runs) and the API Keys API as an impersonated identity.
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

// isRootRepo reports whether the repository's placement is a root organization's; a
// placement that does not read is the run's error.
func isRootRepo(dir string) (bool, error) {
	p, err := orgPlacement(dir, "")
	if err != nil {
		return false, err
	}

	return p.IsRoot(), nil
}

// rootRun is org plan or org apply in a root organization's repository: the trigger's run
// through the engine, reading the build the step names in the environment; the apply gets
// the creation's seams for the hosting organizations it creates.
func (d deps) rootRun(ctx context.Context, dir, command string, out io.Writer) error {
	wired := d.rootOrg()
	if wired == nil {
		return errors.New("no root organization clients are wired")
	}
	rc := &rootorg.RunClients{}
	if d.deploy != nil {
		rc.Builds, rc.Secrets = d.deploy.Builds, d.deploy.Secrets
	}
	req := &rootorg.RunRequest{BuildID: os.Getenv("BUILD_ID"), Project: os.Getenv("PROJECT_ID"), Location: os.Getenv("LOCATION"), Dir: dir}
	if command == org.PlanCommand {
		return rootorg.PlanRun(ctx, wired, rc, req, out)
	}
	clients := *wired
	if clients.Create == nil && d.deploy != nil && d.deploy.Storage != nil {
		store, err := d.deploy.Storage(ctx)
		if err != nil {
			return err
		}
		defer store.Close()
		clients.Create = &create.Clients{Google: d.org.googleAs, GitHub: d.deploy.GitHub, Exec: d.deploy.Exec, Store: store, Sleep: d.deploy.Sleep}
	}

	return rootorg.ApplyRun(ctx, &clients, rc, req, out)
}

// orgPlacement reads the placement named, or the one at the repository root.
func orgPlacement(dir, placement string) (*org.Placement, error) {
	if placement == "" {
		placement = filepath.Join(dir, orgPlacementFile)
	}

	return org.ReadPlacement(placement)
}
