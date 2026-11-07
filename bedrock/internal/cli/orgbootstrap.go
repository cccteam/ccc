// orgbootstrap.go is bedrock org bootstrap: the one thing made by hand, the root
// organization, by one command run as the break-glass account (README.md, "Bootstrapped
// by one command"). From it every hosting organization is created by pull request.

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/prompt"
	"github.com/cccteam/ccc/bedrock/internal/release"
	rootorg "github.com/cccteam/ccc/bedrock/internal/root"
)

// The command, and the region the root project is in when none is named; the Bot
// Account's token is read from the variable every GitHub token is (github.TokenEnv).
const (
	bootstrapCommand = "bootstrap"
	defaultRegion    = "us-central1"
)

// bootstrapFlags is what the command drafts the placement from.
type bootstrapFlags struct {
	prefix, organization, billingAccount, githubOrganization, defaultBranch, region, regionCode, appPrefix string
	domains, keyOperators                                                                                  []string
	labels                                                                                                 map[string]string
	approvingReviews                                                                                       int
	brought                                                                                                map[org.AppRole]*broughtFlags
	noBrowser                                                                                              bool
}

// broughtFlags name a GitHub App the organization has already: its App ID and the file
// holding its private key.
type broughtFlags struct {
	app, key string
}

func newOrgBootstrap(d deps) *cobra.Command {
	f := &bootstrapFlags{brought: map[org.AppRole]*broughtFlags{}}
	cmd := &cobra.Command{
		Use:   bootstrapCommand + " <dir>",
		Short: "Make the root organization, the one thing made by hand, as the break-glass account",
		Long: `bootstrap makes a root organization: the one thing made by hand, by this one command, run once
at a terminal with the break-glass account's credentials (gcloud auth application-default login).
From it every hosting organization is created by pull request (bedrock org create in the
repository it makes). It looks before each step and makes what is missing, so a run that
stopped finishes on the next run with the same directory.

What it needs: the break-glass account holding Organization Administrator and Folder Creator
at the organization node and Billing Account Administrator on the billing account (org
preflight checks them first; README.md, "Bootstrapped by one command"); the GitHub
organization, with the GitHub Bot Account an owner of it and Google's Cloud Build GitHub App
installed on it for all repositories; and the Bot Account's token, a classic personal access
token with the repo, read:user and read:org scopes, in GITHUB_TOKEN or typed at the prompt
(it goes into Secret Manager, where every Cloud Build connection authorizes with it, and
nowhere else).

What it makes, in order: the organization's folder at the organization node (the break-glass
account's Folder Admin on it, its key for a recovery by hand); the root project under it,
<prefix>-root-gbl-core-<suffix>, linked to the billing account with its APIs enabled, and the
state bucket; the token's secret; the three GitHub Apps, infrastructure, deployer and release,
made in your browser from the manifests bedrock declares (a page this command serves: Create,
then Install for all repositories, for each) or brought (--infrastructure-app with
--infrastructure-key, and the others) and held to the same declaration, each key going from
GitHub into its secret, its version pinned; the Cloud Build installation found through the
infrastructure app; the repository <prefix>-root-infrastructure, private, with placement.json
and the rendered README as its first commit; then everything the apply keeps true, through
the same engine the apply trigger runs: the runner identities and the creating identity with
their roles, the secrets' aliases and policies, the creating identity's Folder Creator at the
organization node and Billing Account Administrator on the billing account, the zones of the
domains (--domain) with DNSSEC, the Cloud Build connection, the repository link and the two
triggers, the repository's settings and ruleset with the infrastructure app as its bypass
actor; last its own Owner off the root project, and the check. No terraform: the root
organization is bedrock's own bookkeeping, kept true by API on every merge.

The discovery decides what the credentials show: whose they are (the break-glass account),
the one organization and the one open billing account they see, or --organization (its
numeric id or its domain) and --billing-account among several; --prefix and
--github-organization are asked for at the terminal when not given. The directory is where
the repository is rendered: a new or empty one, or the one of a run that stopped, whose
placement.json then names everything and no naming flag is taken. The bedrock running pins
itself in the placement (a release, or a commit installed with go install), since the
triggers' runs fetch it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.orgBootstrap(cmd, args[0], f)
		},
	}
	cmd.Flags().StringVar(&f.prefix, "prefix", "", "the prefix the organization's names start with (2 to 4 lowercase characters, a letter first); asked for at the terminal when left out")
	cmd.Flags().StringVar(&f.organization, "organization", "", "the Google Cloud organization, by numeric id or domain, when the credentials see several")
	cmd.Flags().StringVar(&f.billingAccount, "billing-account", "", "the billing account (XXXXXX-XXXXXX-XXXXXX) when the credentials see several")
	cmd.Flags().StringVar(&f.githubOrganization, "github-organization", "", "the GitHub organization's login; asked for at the terminal when left out")
	cmd.Flags().StringVar(&f.defaultBranch, "default-branch", "master", "the default branch of the repository, and of every repository under the organization")
	cmd.Flags().StringVar(&f.region, "region", defaultRegion, "the region the root project's connection, triggers and state bucket are in")
	cmd.Flags().StringVar(&f.regionCode, "region-code", "", "the three-character code bedrock's names carry for the region (us-central1: uc1), when it is not the initials and the number")
	cmd.Flags().StringSliceVar(&f.domains, "domain", nil, "a domain delegated to the organization, whose zone it holds and hands names under onward (repeatable)")
	cmd.Flags().StringSliceVar(&f.keyOperators, "key-operator", nil, "a member (user:..., group:...) who may add a version of a GitHub App's key for its rotation (repeatable)")
	cmd.Flags().StringToStringVar(&f.labels, "label", nil, "a label on the folder, the project and the bucket, key=value (repeatable)")
	cmd.Flags().IntVar(&f.approvingReviews, "approving-reviews", 1, "the approving reviews a pull request needs before it merges, here and on every repository under the organization; 0 for none")
	cmd.Flags().StringVar(&f.appPrefix, "app-prefix", "", "the word the three GitHub Apps' names start with, when the organization's login would make a name longer than GitHub's 34 characters")
	cmd.Flags().BoolVar(&f.noBrowser, "no-browser", false, "print the GitHub Apps page's address instead of opening the browser")
	for _, role := range org.AppRoles {
		b := &broughtFlags{}
		f.brought[role] = b
		cmd.Flags().StringVar(&b.app, string(role)+"-app", "", "the App ID of the "+role.Title()+" the organization has already, in place of one made here")
		cmd.Flags().StringVar(&b.key, string(role)+"-key", "", "the file holding the private key (PEM) of the "+role.Title()+" brought with --"+string(role)+"-app")
	}

	return cmd
}

// orgBootstrap is the bootstrap: the draft, the token, the brought apps, the run, and what
// comes next.
func (d deps) orgBootstrap(cmd *cobra.Command, dir string, f *bootstrapFlags) error {
	ctx, out := cmd.Context(), cmd.OutOrStdout()
	clients := d.rootOrg()
	if clients == nil {
		return errors.New("no root organization clients are wired")
	}
	p, err := d.bootstrapDraft(ctx, cmd, dir, f, out)
	if err != nil {
		return err
	}
	token, err := d.botToken(out)
	if err != nil {
		return err
	}
	brought, err := readBrought(f.brought)
	if err != nil {
		return err
	}
	req := &rootorg.BootstrapRequest{Placement: p, Dir: dir, Token: token, Brought: brought}
	if open := d.org.openURL; !f.noBrowser && open != nil {
		req.OpenURL = func(url string) error {
			return open(ctx, url)
		}
	}
	res, err := rootorg.Bootstrap(ctx, clients, req, out)
	if err != nil {
		return err
	}
	fmt.Fprint(out, bootstrapEpilogue(res.Placement, dir, res.Cloned))

	return nil
}

// bootstrapDraft is the placement the bootstrap starts from: the one in the directory,
// from a run that stopped (which names everything, so no naming flag is taken), or a new
// one from the flags, the discovery of whose the credentials are and what they see, and
// the prompts for what neither gives.
func (d deps) bootstrapDraft(ctx context.Context, cmd *cobra.Command, dir string, f *bootstrapFlags, out io.Writer) (*org.Placement, error) {
	path := filepath.Join(dir, orgPlacementFile)
	if _, err := os.Stat(path); err == nil {
		return resumedDraft(path, f, out)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.Wrapf(err, "os.Stat(): %s", path)
	}
	version, sum, err := d.bootstrapPin(ctx)
	if err != nil {
		return nil, err
	}
	disc, err := d.rootOrg().Discovery(ctx)
	if err != nil {
		return nil, err
	}
	defer disc.Close()
	account, err := disc.Account(ctx)
	if err != nil {
		return nil, err
	}
	ask := d.asker(cmd)
	organization, err := chooseOrganization(ctx, disc, f.organization, ask)
	if err != nil {
		return nil, err
	}
	billing, err := chooseBillingAccount(ctx, disc, f.billingAccount, ask)
	if err != nil {
		return nil, err
	}
	prefix, err := named(f.prefix, "prefix", "The prefix the root organization's names start with (2 to 4 lowercase characters, a letter first):", ask)
	if err != nil {
		return nil, err
	}
	githubOrganization, err := named(f.githubOrganization, "github-organization", "The GitHub organization's login (the Bot Account is an owner of it):", ask)
	if err != nil {
		return nil, err
	}
	code := f.regionCode
	if code == "" {
		if code, err = org.RegionCode(f.region); err != nil {
			return nil, err
		}
	}
	p := &org.Placement{
		Kind: org.RootKind, Prefix: prefix, OrganizationDomain: organization.Domain, OrganizationID: organization.ID, DirectoryCustomerID: organization.DirectoryCustomerID,
		BillingAccount: billing, BreakGlassAccount: account, GithubOrganization: githubOrganization, GithubDefaultBranch: f.defaultBranch, GithubAppPrefix: f.appPrefix,
		SourceRepo: prefix + "-root-infrastructure", Regions: []org.Region{{Name: f.region, Code: code}}, KeyOperators: f.keyOperators, Labels: f.labels,
		BedrockVersion: version, BedrockSHA256: sum,
	}
	if f.approvingReviews != 1 {
		reviews := f.approvingReviews
		p.ApprovingReviews = &reviews
	}
	for _, domain := range f.domains {
		if p.Domains == nil {
			p.Domains = map[string]org.Domain{}
		}
		p.Domains[domain] = org.Domain{}
	}
	if err := p.ValidateDraft(); err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "Drafted the %s root organization: Google Cloud organization %s (%s), billing account %s, GitHub organization %s, region %s (%s), bedrock %s, as %s.\n",
		p.Prefix, organization.Domain, organization.ID, billing, githubOrganization, f.region, code, version, account)

	return p, nil
}

// resumedDraft is the placement a run that stopped left in the directory: it names
// everything, and a flag that names something is refused, since the record decides.
func resumedDraft(path string, f *bootstrapFlags, out io.Writer) (*org.Placement, error) {
	p, err := org.ReadPlacementUnchecked(path)
	if err != nil {
		return nil, err
	}
	if !p.IsRoot() {
		return nil, errors.Newf("%s is a hosting organization's placement (kind): a hosting organization is created by its root organization (bedrock org create there); bedrock org bootstrap makes a root organization, in a new or empty directory or one of its own earlier run", path)
	}
	var named []string
	for flag, set := range map[string]bool{
		"prefix": f.prefix != "", "organization": f.organization != "", "billing-account": f.billingAccount != "", "github-organization": f.githubOrganization != "",
		"domain": len(f.domains) > 0, "key-operator": len(f.keyOperators) > 0, "label": len(f.labels) > 0, "app-prefix": f.appPrefix != "", "region-code": f.regionCode != "",
	} {
		if set {
			named = append(named, "--"+flag)
		}
	}
	if len(named) > 0 {
		return nil, errors.Newf("%s records the %s root organization from an earlier run, and names everything: run again without %s, or start in an empty directory; to change what it records, edit the file", path, p.Prefix, strings.Join(named, ", "))
	}
	fmt.Fprintf(out, "%s records the %s root organization from an earlier run: the bootstrap finishes what that run left.\n", path, p.Prefix)

	return p, nil
}

// bootstrapPin is the bedrock running, as the placement pins it: a release with its
// checksum, or an installed commit; a build from a checkout pins nothing, and the triggers'
// runs need a pin, so it is refused.
func (d deps) bootstrapPin(ctx context.Context) (version, sum string, err error) {
	running := d.running()
	if !running.heldToPin() {
		return "", "", errors.Newf("this bedrock is %s, which no placement can pin, and the root organization's triggers fetch the bedrock placement.json pins: run bedrock org bootstrap with a released bedrock (a binary from its GitHub Release) or one installed at a pushed commit (go install %s@<commit>)", running.version, release.Module)
	}

	return d.firstPin(ctx)
}

// chooseOrganization is the Google Cloud organization: the one the credentials see, or
// the one --organization names among several (by id or domain), or the one chosen at the
// terminal.
func chooseOrganization(ctx context.Context, disc rootorg.Discovery, flag string, ask *prompt.Prompter) (rootorg.Organization, error) {
	organizations, err := disc.Organizations(ctx)
	if err != nil {
		return rootorg.Organization{}, err
	}
	if len(organizations) == 0 {
		return rootorg.Organization{}, errors.New("these credentials see no Google Cloud organization: sign in as the break-glass account (gcloud auth application-default login), which holds Organization Administrator at the organization node")
	}
	choices := make([]prompt.Choice, 0, len(organizations))
	for _, o := range organizations {
		if flag != "" && (o.ID == flag || o.Domain == flag) {
			return o, nil
		}
		choices = append(choices, prompt.Choice{Value: o.ID, Note: o.Domain})
	}
	if flag != "" {
		return rootorg.Organization{}, errors.Newf("--organization %s is none these credentials see: %s", flag, choiceList(choices))
	}
	if len(organizations) == 1 {
		return organizations[0], nil
	}
	if ask == nil {
		return rootorg.Organization{}, errors.Newf("these credentials see %d Google Cloud organizations and no terminal is here to choose on: pass --organization, one of %s", len(organizations), choiceList(choices))
	}
	id, err := ask.Choose("The Google Cloud organization:", choices)
	if err != nil {
		return rootorg.Organization{}, err
	}
	for _, o := range organizations {
		if o.ID == id {
			return o, nil
		}
	}

	return rootorg.Organization{}, errors.Newf("no organization %s", id)
}

// chooseBillingAccount is the billing account: the one open account the credentials see,
// or the one --billing-account names, or the one chosen at the terminal.
func chooseBillingAccount(ctx context.Context, disc rootorg.Discovery, flag string, ask *prompt.Prompter) (string, error) {
	accounts, err := disc.BillingAccounts(ctx)
	if err != nil {
		return "", err
	}
	var choices []prompt.Choice
	for _, a := range accounts {
		if flag != "" && a.ID == flag {
			return a.ID, nil
		}
		if a.Open {
			choices = append(choices, prompt.Choice{Value: a.ID, Note: a.DisplayName})
		}
	}
	switch {
	case flag != "":
		return "", errors.Newf("--billing-account %s is none these credentials see: %s. A billing account of another organization is seen once the break-glass account holds Billing Account Administrator on it", flag, choiceList(choices))
	case len(choices) == 0:
		return "", errors.New("these credentials see no open billing account: the break-glass account holds Billing Account Administrator on the one the organization's projects link to, granted by a billing administrator of the organization that owns it")
	case len(choices) == 1:
		return choices[0].Value, nil
	case ask == nil:
		return "", errors.Newf("these credentials see %d open billing accounts and no terminal is here to choose on: pass --billing-account, one of %s", len(choices), choiceList(choices))
	}

	return ask.Choose("The billing account the organization's projects link to:", choices)
}

// named is a value the flag gives, else the one typed at the terminal, else refused.
func named(flag, name, question string, ask *prompt.Prompter) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if ask == nil {
		return "", errors.Newf("no --%s given and no terminal to ask on", name)
	}
	value, err := ask.Line(question)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", errors.Newf("nothing was typed for --%s", name)
	}

	return strings.TrimSpace(value), nil
}

// botToken is the Bot Account's token: GITHUB_TOKEN, or typed without echo at the
// terminal.
func (d deps) botToken(out io.Writer) ([]byte, error) {
	if token := os.Getenv(github.TokenEnv); token != "" {
		return []byte(token), nil
	}
	if !d.interactive() || d.readSecret == nil {
		return nil, errors.Newf("no token of the GitHub Bot Account: set %s to a classic personal access token of the Bot Account (an owner of the GitHub organization; scopes repo, read:user and read:org), or run at a terminal, which asks for it", github.TokenEnv)
	}
	token, err := d.readSecret(out, "The GitHub Bot Account's token (a classic personal access token; it goes into Secret Manager and nowhere else): ")
	if err != nil {
		return nil, err
	}
	token = []byte(strings.TrimSpace(string(token)))
	if len(token) == 0 {
		return nil, errors.New("no token was typed")
	}

	return token, nil
}

// readBrought reads the apps brought: each flag pair's id and key file.
func readBrought(flags map[org.AppRole]*broughtFlags) (map[org.AppRole]rootorg.BroughtApp, error) {
	brought := map[org.AppRole]rootorg.BroughtApp{}
	for _, role := range org.AppRoles {
		f := flags[role]
		if f == nil || (f.app == "" && f.key == "") {
			continue
		}
		if f.app == "" || f.key == "" {
			return nil, errors.Newf("--%s-app and --%s-key go together: the App ID of %s and the file holding its private key", role, role, role.Title())
		}
		key, err := os.ReadFile(f.key)
		if err != nil {
			return nil, errors.Wrapf(err, "os.ReadFile(): %s's key", role.Title())
		}
		brought[role] = rootorg.BroughtApp{ID: f.app, Key: key}
	}

	return brought, nil
}

// bootstrapEpilogue says what follows the bootstrap, each where it happens; cloned says
// whether the directory is a clone of the repository (at the pushed commit, or on whatever
// branch a run again found it), else the clone is the first step.
func bootstrapEpilogue(p *org.Placement, dir string, cloned bool) string {
	where := fmt.Sprintf("%s is a clone of %s/%s", dir, p.GithubOrganization, p.SourceRepo)
	if !cloned {
		where = fmt.Sprintf("clone %s/%s (git clone https://github.com/%s/%s.git), which holds what %s holds, at its first commit", p.GithubOrganization, p.SourceRepo, p.GithubOrganization, p.SourceRepo, dir)
	}

	return fmt.Sprintf(`
Next, each where it happens:
  1. In a terminal: %s. Every change to the root
     organization goes from here by pull request (placement.json, then bedrock org render): the
     plan trigger posts what the merge changes, and the apply trigger makes it on the merge
     (README.md, "How the root organization is kept").
  2. At each domain's registrar: point the delegation at its zone's name servers, which
     bedrock domain check prints here for each domain in placement.json (domains), and says
     when the world sees them.
  3. In a terminal, here: bedrock org create <prefix> for the first hosting organization, then
     the pull request; the merge creates it as the creating identity (README.md, "Creating a
     hosting organization").
The break-glass account keeps Folder Admin on the folder %s alone, its key for a recovery by
hand; nothing else is done by hand.
`, where, p.Prefix)
}
