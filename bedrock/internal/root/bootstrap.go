// bootstrap.go is bedrock org bootstrap: the one thing made by hand, a root
// organization, by one command run as the break-glass account. It looks before every
// step and makes what is missing, so a run that stopped halfway finishes on the next:
// the preflight, the Bot Account's token checked, the folder, the root project with its
// billing link and its APIs, the state bucket, the token secret, the three GitHub Apps
// (made in the browser from their manifests, or brought and held to them) with their
// keys in Secret Manager, the Cloud Build GitHub App's installation found, the repository
// made and pushed, the engine's apply (secrets, grants, zones, connection, triggers,
// rules), the break-glass account's working grant taken off, and the check.

package root

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cccteam/ccc/bedrock/internal/create"
	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/gitcmd"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// BroughtApp is a GitHub App the organization has already, in place of one the bootstrap
// makes: its App ID and its private key in PEM.
type BroughtApp struct {
	ID  string
	Key []byte
}

// BootstrapRequest is one bootstrap: the placement as drafted (what the person and the
// discovery decided; the bootstrap fills in the project, the bucket, the apps and the
// authorization), where the repository is rendered, the Bot Account's token, the apps
// brought, how the browser is opened, and how long the browser flow may take.
type BootstrapRequest struct {
	Placement *org.Placement
	Dir       string
	Token     []byte
	Brought   map[org.AppRole]BroughtApp
	// OpenURL opens the page in the person's browser; nil prints the address alone.
	OpenURL     func(url string) error
	FlowTimeout time.Duration
}

// BootstrapResult is what the bootstrap left: the placement as recorded, the commit
// pushed, whether the directory is a clone of the repository at it, and the check's
// report.
type BootstrapResult struct {
	Placement *org.Placement
	Commit    string
	Cloned    bool
	Report    *Report
}

// The roles the bootstrap grants by hand's measure and takes off.
const (
	folderAdminRole = "roles/resourcemanager.folderAdmin"
	ownerRole       = "roles/owner"
	// grantSettle is how long the bootstrap waits for a new project's creator grant to
	// reach IAM before it gives up; grantPoll how often it tries.
	grantSettle = 3 * time.Minute
	grantPoll   = 10 * time.Second
	// defaultFlowTimeout is how long the browser flow may take.
	defaultFlowTimeout = 45 * time.Minute
)

// bootstrap is one run.
type bootstrap struct {
	clients *Clients
	req     *BootstrapRequest
	p       *org.Placement
	out     io.Writer
	google  create.Google
	secrets Secrets
}

// Bootstrap runs the bootstrap and answers what it left.
func Bootstrap(ctx context.Context, clients *Clients, req *BootstrapRequest, out io.Writer) (*BootstrapResult, error) {
	b := &bootstrap{clients: clients, req: req, p: req.Placement, out: out}
	if err := b.check(); err != nil {
		return nil, err
	}
	if !org.Preflight(ctx, b.p, clients.Permissions, out) {
		return nil, errors.New("the preflight did not pass; nothing was made")
	}
	bot, err := b.botAccount(ctx)
	if err != nil {
		return nil, err
	}
	if b.google, err = clients.Google(ctx); err != nil {
		return nil, err
	}
	defer b.google.Close()
	if b.secrets, err = clients.Secrets(ctx); err != nil {
		return nil, err
	}
	defer b.secrets.Close()
	folder, err := b.seed(ctx)
	if err != nil {
		return nil, err
	}
	if err := b.save(); err != nil {
		return nil, err
	}
	if err := b.tokenSecret(ctx); err != nil {
		return nil, err
	}
	if err := b.save(); err != nil {
		return nil, err
	}
	if err := b.apps(ctx); err != nil {
		return nil, err
	}
	infraToken, err := b.installation(ctx)
	if err != nil {
		return nil, err
	}
	if err := b.save(); err != nil {
		return nil, err
	}
	commit, err := b.repository(ctx, infraToken)
	if err != nil {
		return nil, err
	}
	cloned := b.clone(ctx, commit, infraToken)
	fmt.Fprintf(out, "=== making the root organization as placement.json says ===\n")
	if _, err := Run(ctx, clients, &Request{Placement: b.p, Dir: req.Dir, Mode: Apply, Commit: commit, GitHubToken: infraToken, Bootstrap: true}, out); err != nil {
		return nil, err
	}
	if err := b.withdraw(ctx, folder); err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "=== checking ===\n")
	report, err := Run(ctx, clients, &Request{Placement: b.p, Dir: req.Dir, Mode: Check, Commit: commit, GitHubToken: infraToken}, out)
	if err != nil {
		return nil, err
	}
	report.Write(out, Check)
	fmt.Fprintf(out, "Bootstrapped the %s root organization as %s: folder %s, project %s, repository %s/%s at %s. The Bot Account is %s; the apps are %s.\n",
		b.p.Prefix, b.p.BreakGlassAccount, folder, b.p.Projects[org.RootProjectKey], b.p.GithubOrganization, b.p.SourceRepo, short(commit), bot, b.appIDs())

	return &BootstrapResult{Placement: b.p, Commit: commit, Cloned: cloned, Report: report}, nil
}

// check refuses a draft that lacks what the bootstrap cannot find out.
func (b *bootstrap) check() error {
	p := b.p
	if !p.IsRoot() {
		return errors.New("the placement is not a root organization's (kind root)")
	}
	missing := map[string]bool{
		"prefix": p.Prefix == "", "organizationId": p.OrganizationID == "", "billingAccount": p.BillingAccount == "", "breakGlassAccount": p.BreakGlassAccount == "",
		"githubOrganization": p.GithubOrganization == "", "regions": len(p.Regions) == 0, "sourceRepo": p.SourceRepo == "", "githubDefaultBranch": p.GithubDefaultBranch == "",
	}
	var names []string
	for name, is := range missing {
		if is {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	if len(names) > 0 {
		return errors.Newf("the bootstrap's placement lacks %s", strings.Join(names, ", "))
	}
	if len(b.req.Token) == 0 {
		return errors.New("no token of the Bot Account: GITHUB_TOKEN, or the prompt")
	}
	b.req.Token = bytes.TrimSpace(b.req.Token)
	if b.clients.GitHub == nil || b.clients.Permissions == nil || b.clients.Google == nil || b.clients.Secrets == nil {
		return errors.New("the bootstrap's clients are not all wired")
	}

	return nil
}

// save writes the placement as recorded so far into the directory, so a run that stops
// here goes on from it next time (the project's suffix, the apps, the token's version are
// chosen once).
func (b *bootstrap) save() error {
	if err := os.MkdirAll(b.req.Dir, 0o750); err != nil {
		return errors.Wrapf(err, "os.MkdirAll(): %s", b.req.Dir)
	}

	return b.p.Write(filepath.Join(b.req.Dir, org.PlacementFile))
}

// botAccount reads whose the token is and that the account owns the organization, and
// records the login. A token an app was issued (GitHub's prefixes say so) is refused
// first: it is not a personal access token, whatever account it names.
func (b *bootstrap) botAccount(ctx context.Context) (string, error) {
	login, err := github.BotAccount(ctx, b.clients.GitHub, string(b.req.Token), b.p.GithubOrganization)
	if err != nil {
		return "", err
	}
	b.p.GithubBotAccount = login
	fmt.Fprintf(b.out, "The token is %s's, an owner of %s: the Bot Account.\n", login, b.p.GithubOrganization)

	return login, nil
}

// seed makes the folder, the project with its billing link and its APIs, and the bucket,
// and records them; it answers the folder's id.
func (b *bootstrap) seed(ctx context.Context) (string, error) {
	p := b.p
	fmt.Fprintf(b.out, "=== the %s root organization's folder, project and bucket ===\n", p.Prefix)
	parent := "organizations/" + p.OrganizationID
	folder, err := b.google.FindFolder(ctx, parent, p.Prefix)
	if err != nil {
		return "", err
	}
	if folder == "" {
		if folder, err = b.google.CreateFolder(ctx, parent, p.Prefix); err != nil {
			return "", err
		}
		fmt.Fprintf(b.out, "Created the folder %s at the organization node: %s.\n", p.Prefix, folder)
	} else {
		fmt.Fprintf(b.out, "The folder %s exists at the organization node: %s.\n", p.Prefix, folder)
	}
	if err := b.google.GrantFolder(ctx, folder, folderAdminRole, "user:"+p.BreakGlassAccount); err != nil {
		return "", err
	}
	if p.Projects == nil {
		p.Projects = map[string]string{}
	}
	if p.Projects[org.RootProjectKey] == "" {
		// A run that stopped before the placement was saved left its project under the
		// folder; the next run takes it up rather than making a second one.
		found, err := b.google.FindProject(ctx, folder, p.Prefix+"-"+org.RootProjectKey+"-gbl-core-")
		if err != nil {
			return "", err
		}
		if found != "" {
			p.Projects[org.RootProjectKey] = found
			fmt.Fprintf(b.out, "Found the root project %s under the folder, which an earlier run made; recorded.\n", found)
		} else {
			suffix, err := org.RandomSuffix()
			if err != nil {
				return "", err
			}
			p.Projects[org.RootProjectKey] = p.Prefix + "-" + org.RootProjectKey + "-gbl-core-" + suffix
		}
	}
	project := p.Projects[org.RootProjectKey]
	exists, err := b.google.ProjectExists(ctx, project)
	if err != nil {
		return "", err
	}
	if !exists {
		if err := b.google.CreateProject(ctx, project, p.Prefix+"-"+org.RootProjectKey+"-gbl-core", folder, p.ProjectLabels(org.RootProjectKey, org.RootProjectKey)); err != nil {
			return "", err
		}
		fmt.Fprintf(b.out, "Created the project %s under the folder.\n", project)
	} else {
		fmt.Fprintf(b.out, "The project %s exists.\n", project)
	}
	if err := b.settled(ctx, func() error { return b.google.LinkBilling(ctx, project, p.BillingAccount) }); err != nil {
		return "", err
	}
	if err := b.google.EnableServices(ctx, project, Services); err != nil {
		return "", err
	}
	number, err := b.google.ProjectNumber(ctx, project)
	if err != nil {
		return "", err
	}
	if p.ProjectNumbers == nil {
		p.ProjectNumbers = map[string]string{}
	}
	p.ProjectNumbers[org.RootProjectKey] = number
	fmt.Fprintf(b.out, "Linked %s (%s) to billing account %s and enabled its %d APIs.\n", project, number, p.BillingAccount, len(Services))
	if p.StateBucket == "" {
		p.StateBucket = p.Prefix + "-" + org.RootProjectKey + "-gbl-state-" + project[strings.LastIndex(project, "-")+1:]
	}
	if exists, err := b.google.BucketExists(ctx, p.StateBucket); err != nil {
		return "", err
	} else if !exists {
		if err := b.google.CreateBucket(ctx, project, p.StateBucket, p.Primary().Name, p.ProjectLabels(org.RootProjectKey, org.RootProjectKey)); err != nil {
			return "", err
		}
		fmt.Fprintf(b.out, "Created the state bucket %s in %s.\n", p.StateBucket, p.Primary().Name)
	} else {
		fmt.Fprintf(b.out, "The state bucket %s exists.\n", p.StateBucket)
	}

	return folder, nil
}

// settled runs the call until it is not refused for a permission the creator holds but
// IAM has not seen yet, up to grantSettle.
func (b *bootstrap) settled(ctx context.Context, call func() error) error {
	deadline := time.Now().Add(grantSettle)
	for {
		err := call()
		if err == nil || status.Code(errors.Cause(err)) != codes.PermissionDenied || time.Now().After(deadline) {
			return err
		}
		fmt.Fprintln(b.out, "The creator's grant has not reached IAM yet; trying again in a moment.")
		if err := b.sleep(ctx, grantPoll); err != nil {
			return err
		}
	}
}

func (b *bootstrap) sleep(ctx context.Context, d time.Duration) error {
	if b.clients.Sleep != nil {
		return b.clients.Sleep(ctx, d)
	}

	return sleep(ctx, d)
}

// secret makes the secret in the root project unless it is there, and answers its name.
func (b *bootstrap) secret(ctx context.Context, id string) (string, error) {
	name := "projects/" + b.p.Projects[org.RootProjectKey] + "/secrets/" + id
	var got *Secret
	err := b.untilServing(ctx, "Secret Manager", func() error {
		var err error
		got, err = b.secrets.Secret(ctx, name)

		return err
	})
	if err != nil {
		return "", err
	}
	if got == nil {
		if err := b.secrets.CreateSecret(ctx, b.p.Projects[org.RootProjectKey], id, b.p.ProjectLabels(org.RootProjectKey, org.RootProjectKey)); err != nil {
			return "", err
		}
		fmt.Fprintf(b.out, "Created the secret %s.\n", id)
	}

	return name, nil
}

// An API enabled moments ago answers its first calls with PermissionDenied and the reason
// SERVICE_DISABLED for a minute or two while the enable propagates (Secret Manager in the
// lab, 2026-11-05); the bootstrap tries such a call again, apiWait apart, up to apiTries.
const (
	apiWait  = 15 * time.Second
	apiTries = 12
)

// untilServing runs call, and again while Google answers that the API is not enabled yet,
// up to apiTries times apiWait apart; any other error, and the last, is answered as is.
func (b *bootstrap) untilServing(ctx context.Context, api string, call func() error) error {
	for try := 1; ; try++ {
		err := call()
		if err == nil || !serviceDisabled(err) || try == apiTries {
			return err
		}
		fmt.Fprintf(b.out, "Google answers that the %s API is not enabled yet, moments after the project's APIs were enabled; trying again in %s.\n", api, apiWait)
		if werr := b.clients.Sleep(ctx, apiWait); werr != nil {
			return werr
		}
	}
}

// serviceDisabled reports whether the error is Google's answer that the API is disabled or
// has not been used in the project: PermissionDenied, saying so in its message (the reason
// is SERVICE_DISABLED).
func serviceDisabled(err error) bool {
	st, ok := status.FromError(errors.Cause(err))
	if !ok || st.Code() != codes.PermissionDenied {
		return false
	}

	return strings.Contains(st.Message(), "has not been used in project") || strings.Contains(st.Message(), "it is disabled")
}

// tokenSecret puts the Bot Account's token into its secret, pins the version
// (githubTokenVersion) and records the alias every connection names
// (cloudBuildTokenSecretVersion), unless the placement records them already (a run after a
// stop). The apply that follows points the alias at the pinned version, before it makes
// the connection.
func (b *bootstrap) tokenSecret(ctx context.Context) error {
	name, err := b.secret(ctx, b.p.TokenSecretID())
	if err != nil {
		return err
	}
	if b.p.CloudBuildTokenSecretVersion != "" {
		fmt.Fprintf(b.out, "The Bot Account's token is in %s, version %s pinned (recorded).\n", b.p.CloudBuildTokenSecretVersion, b.p.GithubTokenVersion)

		return nil
	}
	version, err := b.secrets.AddSecretVersion(ctx, name, b.req.Token)
	if err != nil {
		return err
	}
	if err := b.p.PinKeyVersion(org.Token, version); err != nil {
		return err
	}
	b.p.CloudBuildTokenSecretVersion = name + "/versions/" + org.KeyAlias
	fmt.Fprintf(b.out, "Put the Bot Account's token into %s as version %s, and pinned it: every Cloud Build connection authorizes with the alias %s, which the apply points at the pinned version.\n", b.p.TokenSecretID(), version, org.KeyAlias)

	return nil
}

// apps brings each app: a brought one is held to its manifest and recorded; the rest are
// made in the browser. Each key goes into its secret, and the version is pinned.
func (b *bootstrap) apps(ctx context.Context) error {
	fmt.Fprintf(b.out, "=== the GitHub Apps ===\n")
	var toMake []org.AppManifest
	for _, m := range b.p.AppManifests() {
		if b.p.AppID(m.Role) != "" && b.p.KeyVersion(m.Role.Key()) != "" {
			fmt.Fprintf(b.out, "%s is App %s, its key pinned at version %s (recorded).\n", m.Role.Title(), b.p.AppID(m.Role), b.p.KeyVersion(m.Role.Key()))

			continue
		}
		brought, ok := b.req.Brought[m.Role]
		if !ok {
			toMake = append(toMake, m)

			continue
		}
		findings, err := VerifyApp(ctx, b.clients.GitHub, m, brought.ID, brought.Key, b.p.GithubOrganization)
		if err != nil {
			return errors.Wrapf(err, "%s, brought as App %s", m.Role.Title(), brought.ID)
		}
		if len(findings) > 0 {
			return errors.Newf("%s, brought as App %s, is not as bedrock declares it: %s", m.Role.Title(), brought.ID, strings.Join(findings, "; "))
		}
		app, err := deploy.AppIdentity(ctx, b.clients.GitHub, brought.ID, brought.Key)
		if err != nil {
			return err
		}
		if err := b.recordApp(ctx, m, app, brought.Key); err != nil {
			return err
		}
		fmt.Fprintf(b.out, "%s: App %s (%s), brought, holds what bedrock declares and is installed on %s.\n", m.Role.Title(), brought.ID, app.Slug, b.p.GithubOrganization)
	}
	if len(toMake) == 0 {
		return nil
	}
	flow, err := StartFlow(ctx, toMake, b.p.GithubOrganization, "https://github.com/"+b.p.GithubOrganization+"/"+b.p.SourceRepo, b.clients.GitHub("").ConvertManifest, b.clients.GitHub)
	if err != nil {
		return err
	}
	defer flow.Close()
	var names []string
	for _, m := range toMake {
		names = append(names, m.Name)
	}
	fmt.Fprintf(b.out, "In your browser, as an owner of the %s organization, make and install %s: %s\n(Create, then Install for all repositories, for each; the page refreshes by itself. The keys go from GitHub into Secret Manager and never touch this computer's disk.)\n", b.p.GithubOrganization, strings.Join(names, ", "), flow.URL())
	if b.req.OpenURL != nil {
		if err := b.req.OpenURL(flow.URL()); err != nil {
			fmt.Fprintf(b.out, "The browser did not open by itself (%v); open the address above.\n", errors.Cause(err))
		}
	}
	timeout := b.req.FlowTimeout
	if timeout == 0 {
		timeout = defaultFlowTimeout
	}
	flowCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	apps, err := flow.Wait(flowCtx)
	if err != nil {
		return errors.Wrapf(err, "still pending: %s", flow.Pending())
	}
	for _, m := range toMake {
		app := apps[m.Role]
		if err := b.recordApp(ctx, m, app, []byte(app.PEM)); err != nil {
			return err
		}
		fmt.Fprintf(b.out, "%s: made App %d (%s) and installed it on %s.\n", m.Role.Title(), app.ID, app.Slug, b.p.GithubOrganization)
	}

	return nil
}

// clone makes the directory a clone of the repository at the pushed commit, when the
// clients know how and it is not one already, and reports whether it is one; a clone that
// fails is said, with the clone to make by hand, and stops nothing.
func (b *bootstrap) clone(ctx context.Context, commit, token string) bool {
	p := b.p
	url := "https://github.com/" + p.GithubOrganization + "/" + p.SourceRepo + ".git"
	if head, err := gitcmd.Output(ctx, b.req.Dir, "rev-parse", "HEAD"); err == nil && strings.TrimSpace(head) == commit {
		fmt.Fprintf(b.out, "%s is a clone of %s/%s at %s already.\n", b.req.Dir, p.GithubOrganization, p.SourceRepo, short(commit))

		return true
	}
	if b.clients.Clone == nil {
		fmt.Fprintf(b.out, "%s holds the files pushed; clone the repository to go on from it: git clone %s\n", b.req.Dir, url)

		return false
	}
	if err := b.clients.Clone(ctx, b.req.Dir, url, p.GithubDefaultBranch, commit, token); err != nil {
		fmt.Fprintf(b.out, "%s holds the files pushed, and could not be made a clone (%v); clone the repository to go on from it: git clone %s\n", b.req.Dir, errors.Cause(err), url)

		return false
	}
	fmt.Fprintf(b.out, "%s is a clone of %s/%s at %s, on %s.\n", b.req.Dir, p.GithubOrganization, p.SourceRepo, short(commit), p.GithubDefaultBranch)

	return true
}

// recordApp puts the key into its secret, pins the version and records the App ID (and
// the release app's slug).
func (b *bootstrap) recordApp(ctx context.Context, m org.AppManifest, app *github.App, key []byte) error {
	name, err := b.secret(ctx, b.p.SecretContainer(m.Role.Key()))
	if err != nil {
		return err
	}
	version, err := b.secrets.AddSecretVersion(ctx, name, key)
	if err != nil {
		return err
	}
	if err := b.p.PinKeyVersion(m.Role.Key(), version); err != nil {
		return err
	}
	b.p.SetApp(m.Role, app)
	fmt.Fprintf(b.out, "Put %s's key into %s as version %s, and pinned it.\n", m.Role.Title(), b.p.SecretContainer(m.Role.Key()), version)

	return nil
}

// installation finds the Google Cloud Build GitHub App's installation on the organization
// through the infrastructure app, records it, and answers the infrastructure app's
// installation token for the repository.
func (b *bootstrap) installation(ctx context.Context) (string, error) {
	pem, err := b.secrets.AccessSecretVersion(ctx, b.p.PinnedVersionName(org.InfrastructureKey))
	if err != nil {
		return "", err
	}
	token, _, err := deploy.AppOrgInstallationToken(ctx, b.clients.GitHub, b.p.GithubInfrastructureAppID, pem, b.p.GithubOrganization, org.InfrastructureApp.Title())
	if err != nil {
		return "", err
	}
	installations, err := b.clients.GitHub(token).OrgInstallations(ctx, b.p.GithubOrganization)
	if err != nil {
		return "", errors.Wrapf(err, "listing the apps installed on %s as the infrastructure app", b.p.GithubOrganization)
	}
	i := slices.IndexFunc(installations, func(in github.OrgInstallation) bool { return in.AppSlug == cloudBuildAppSlug })
	if i < 0 {
		return "", errors.Newf("Google's Cloud Build GitHub App (github.com/apps/%s) is not installed on the %s organization: install it there as the Bot Account (%s), for all repositories, and run bedrock org bootstrap again", cloudBuildAppSlug, b.p.GithubOrganization, b.p.GithubBotAccount)
	}
	b.p.CloudBuildInstallationID = strconv.FormatInt(installations[i].ID, 10)
	fmt.Fprintf(b.out, "Google's Cloud Build GitHub App is installed on %s as installation %s; every connection names it.\n", b.p.GithubOrganization, b.p.CloudBuildInstallationID)

	return token, nil
}

// repository makes the repository unless it is there, renders the organization into the
// directory with its placement, and pushes the render as the first commit.
func (b *bootstrap) repository(ctx context.Context, token string) (string, error) {
	p := b.p
	if err := p.Validate(); err != nil {
		return "", errors.Wrap(err, "the placement as the bootstrap recorded it")
	}
	fmt.Fprintf(b.out, "=== the repository %s/%s ===\n", p.GithubOrganization, p.SourceRepo)
	gh := b.clients.GitHub(token)
	if _, err := gh.Repository(ctx, p.GithubOrganization, p.SourceRepo); err != nil {
		if github.Moved(err) {
			fmt.Fprintf(b.out, "The name %s/%s forwards to a repository renamed away from it; a new repository takes the name.\n", p.GithubOrganization, p.SourceRepo)
		} else if !github.NotFound(err) {
			return "", errors.Wrapf(err, "reading %s/%s", p.GithubOrganization, p.SourceRepo)
		}
		if _, err := gh.CreateRepository(ctx, p.GithubOrganization, github.RepositoryRequest{
			Name: p.SourceRepo, Description: "The infrastructure of the " + p.Prefix + " root organization (bedrock)", Private: true, AutoInit: true,
		}); err != nil {
			return "", errors.Wrapf(err, "creating %s/%s", p.GithubOrganization, p.SourceRepo)
		}
		fmt.Fprintf(b.out, "Created the repository %s/%s, private.\n", p.GithubOrganization, p.SourceRepo)
	}
	if err := create.DefaultBranch(ctx, gh, p.GithubOrganization, p.SourceRepo, p.GithubDefaultBranch, b.sleep, b.out); err != nil {
		return "", err
	}
	files, err := org.Render(p)
	if err != nil {
		return "", err
	}
	written, err := org.Write(p, files, b.req.Dir)
	if err != nil {
		return "", err
	}
	if err := p.Write(filepath.Join(b.req.Dir, org.PlacementFile)); err != nil {
		return "", err
	}
	fmt.Fprintf(b.out, "Rendered the %s root organization into %s: %d owned file(s) and placement.json.\n", p.Prefix, b.req.Dir, written.Owned)
	message := "chore: the " + p.Prefix + " root organization, bootstrapped by bedrock org bootstrap\n\nRendered from placement.json. The apply trigger makes the organization as the placement says on every push to " + p.GithubDefaultBranch + ", and creates the hosting organizations recorded without a folder."

	return create.Push(ctx, gh, b.req.Dir, p.GithubOrganization, p.SourceRepo, p.GithubDefaultBranch, message, b.out)
}

// withdraw takes off the break-glass account what Google gave it as the creator: Owner
// on the root project, and Folder Editor on the folder. Its Folder Admin on the folder
// stays, its key for a recovery by hand.
func (b *bootstrap) withdraw(ctx context.Context, folder string) error {
	account := "user:" + b.p.BreakGlassAccount
	if err := b.google.RevokeProject(ctx, b.p.Projects[org.RootProjectKey], account, []string{ownerRole}); err != nil {
		return err
	}
	if err := b.google.RevokeFolder(ctx, folder, account, []string{org.FolderEditorRole}); err != nil {
		return err
	}
	fmt.Fprintf(b.out, "Took %s's Owner off %s and its Folder Editor off folder %s; it keeps Folder Admin on the folder.\n", b.p.BreakGlassAccount, b.p.Projects[org.RootProjectKey], folder)

	return nil
}

// appIDs names the three apps' ids for the last line.
func (b *bootstrap) appIDs() string {
	parts := make([]string, 0, len(org.AppRoles))
	for _, role := range org.AppRoles {
		parts = append(parts, string(role)+" "+b.p.AppID(role))
	}

	return strings.Join(parts, ", ")
}

// short is the first seven characters of a commit, as git prints them.
func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}

	return sha
}
