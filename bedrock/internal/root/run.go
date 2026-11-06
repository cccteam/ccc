// run.go is a trigger's run of the root organization: bedrock org plan on a pull request,
// which says what the merge would change and posts it, and bedrock org apply on a push,
// which makes it so under a lock and leaves a marker. Both read their build through the
// Cloud Build API, as the hosting organizations' runs do, and act on GitHub as the
// infrastructure app.

package root

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// RunRequest is what a trigger's run was started with: the build and the checkout.
type RunRequest struct {
	BuildID  string
	Project  string
	Location string
	Dir      string
}

// The substitutions a run reads off its build.
const (
	stepsSub    = "_BEDROCK_STEPS"
	prNumberSub = "_PR_NUMBER"
	commitSub   = "COMMIT_SHA"
)

// The comment the plan posts, found by its first line on a later push.
const planMarker = "<!-- bedrock org plan -->"

// RunClients are the run's seams beyond the engine's: Cloud Build for the build, Secret
// Manager for the apps' keys.
type RunClients struct {
	Builds  deploy.BuildsFunc
	Secrets deploy.SecretsFunc
}

// trigger is one run's reading of its build.
type trigger struct {
	p      *org.Placement
	subs   map[string]string
	commit string
}

// read reads the placement off the checkout and the build off Cloud Build, and checks
// the steps' shape.
func read(ctx context.Context, rc *RunClients, req *RunRequest, command string, out io.Writer) (*trigger, error) {
	for name, value := range map[string]string{"BUILD_ID": req.BuildID, "PROJECT_ID": req.Project, "LOCATION": req.Location} {
		if value == "" {
			return nil, errors.Newf("%s is not set: bedrock org %s runs from the %s trigger of the root organization, whose step passes the build's id, project and location to it", name, command, command)
		}
	}
	if rc == nil || rc.Builds == nil || rc.Secrets == nil {
		return nil, errors.New("no Cloud Build or Secret Manager client is wired")
	}
	p, err := org.ReadPlacement(filepath.Join(req.Dir, org.PlacementFile))
	if err != nil {
		return nil, err
	}
	if !p.IsRoot() {
		return nil, errors.Newf("the %s organization is a hosting organization: its triggers run the layers", p.Prefix)
	}
	builds, err := rc.Builds(ctx)
	if err != nil {
		return nil, err
	}
	data, err := builds.Get(ctx, req.Project, req.Location, req.BuildID)
	if err != nil {
		return nil, err
	}
	build, err := deploy.ParseBuild(data)
	if err != nil {
		return nil, err
	}
	t := &trigger{p: p, subs: build.Substitutions, commit: build.Substitutions[commitSub]}
	steps, ok := t.subs[stepsSub]
	if !ok {
		return nil, errors.Newf("no %s substitution: bedrock org %s runs from the %s trigger the bootstrap made, whose substitutions carry the steps' shape; this build did not start from it", stepsSub, command, command)
	}
	if shape := org.StepsShape(p); steps != shape {
		fmt.Fprintf(out, "This run's steps are of shape %s, and this bedrock makes shape %s for the placement's pin: the trigger's steps lag one apply behind the repository, and the next run of the trigger is the first on the shape this bedrock makes. This run goes on with what the steps pass.\n", steps, shape)
	}
	if t.commit == "" {
		return nil, errors.Newf("%s is not set: a trigger's run names the commit it builds", commitSub)
	}

	return t, nil
}

// AppToken mints an app's installation token on the repository from its key's alias, as
// the credentials read it (a trigger's runner; a person holding the key's read).
func AppToken(ctx context.Context, rc *RunClients, gh deploy.GitHubFunc, p *org.Placement, role org.AppRole) (string, error) {
	version := p.SecretVersionName(role.Key())
	if p.AppID(role) == "" || version == "" {
		return "", errors.Newf("placement.json records no %s or no pinned key for it: bedrock org bootstrap records both", role.Title())
	}
	secrets, err := rc.Secrets(ctx)
	if err != nil {
		return "", err
	}
	defer secrets.Close()
	pem, err := secrets.Access(ctx, version)
	if err != nil {
		return "", errors.Wrapf(err, "%s's key (%s)", role.Title(), version)
	}

	return deploy.AppInstallationToken(ctx, gh, p.AppID(role), pem, p.GithubOrganization, p.SourceRepo, role.Title())
}

// PlanRun is bedrock org plan: it says what the pull request's merge would change in the
// root organization, in the log and in one comment on the pull request as the deployer
// app, and exits well whatever it found; a change that cannot be read is the error.
func PlanRun(ctx context.Context, clients *Clients, rc *RunClients, req *RunRequest, out io.Writer) error {
	t, err := read(ctx, rc, req, org.PlanCommand, out)
	if err != nil {
		return err
	}
	number, err := strconv.Atoi(t.subs[prNumberSub])
	if err != nil || number <= 0 {
		return errors.Newf("%s is %q: the plan trigger runs on a pull request, and Cloud Build names it there", prNumberSub, t.subs[prNumberSub])
	}
	infra, err := AppToken(ctx, rc, clients.GitHub, t.p, org.InfrastructureApp)
	if err != nil {
		return err
	}
	report, err := Run(ctx, clients, &Request{Placement: t.p, Dir: req.Dir, Mode: Plan, Commit: t.commit, GitHubToken: infra}, out)
	if err != nil {
		return err
	}
	report.Write(out, Plan)
	deployer, err := AppToken(ctx, rc, clients.GitHub, t.p, org.DeployerApp)
	if err != nil {
		fmt.Fprintf(out, "The report stays in this log: %v.\n", errors.Cause(err))

		return nil
	}
	gh := clients.GitHub(deployer)
	body := report.Comment(t.commit, t.p)
	existing, err := gh.IssueComments(ctx, t.p.GithubOrganization, t.p.SourceRepo, number)
	if err != nil {
		return err
	}
	for _, c := range existing {
		if strings.HasPrefix(c.Body, planMarker) {
			if err := gh.UpdateComment(ctx, t.p.GithubOrganization, t.p.SourceRepo, c.ID, body); err != nil {
				return err
			}
			fmt.Fprintf(out, "Updated the report on pull request #%d as the deployer app.\n", number)

			return nil
		}
	}
	if err := gh.CreateComment(ctx, t.p.GithubOrganization, t.p.SourceRepo, number, body); err != nil {
		return err
	}
	fmt.Fprintf(out, "Posted the report on pull request #%d as the deployer app.\n", number)

	return nil
}

// Comment is the plan's comment: the marker, the heading, what the merge changes and the
// findings, each on a line.
func (r *Report) Comment(commit string, p *org.Placement) string {
	var b strings.Builder
	b.WriteString(planMarker + "\n### what the merge changes in the " + p.Prefix + " root organization\n\nAt " + short(commit) + ":\n\n")
	pending := r.pendingCreations(p)
	if len(r.Changes) == 0 && len(pending) == 0 {
		b.WriteString("Nothing: the root organization stands as placement.json says.\n")
	}
	for _, c := range r.Changes {
		b.WriteString("- " + c.Section + ": " + c.What + "\n")
	}
	for _, p := range pending {
		b.WriteString("- creations: creates the hosting organization " + p + " as the creating identity (its folder, its boot project, the first applies, its repository, the hand-off pull request)\n")
	}
	if len(r.Findings) > 0 {
		b.WriteString("\nFindings, which the apply does not change:\n\n")
		for _, f := range r.Findings {
			b.WriteString("- " + f + "\n")
		}
	}
	if len(r.Notes) > 0 {
		b.WriteString("\n")
		for _, n := range r.Notes {
			b.WriteString("- note: " + n + "\n")
		}
	}

	return b.String()
}

// pendingCreations names the hosting organizations the apply would create.
func (r *Report) pendingCreations(p *org.Placement) []string {
	pending := p.Pending()
	names := make([]string, 0, len(pending))
	for _, h := range pending {
		names = append(names, h.Prefix)
	}

	return names
}

// lock is what the apply leaves in the lock object while it runs.
type lock struct {
	Build  string `json:"build"`
	Commit string `json:"commit"`
	Since  string `json:"since"`
}

// ApplyRun is bedrock org apply: under the lock in the state bucket, it makes the root
// organization as the pushed placement says and creates the hosting organizations
// recorded without a folder, then leaves the marker.
func ApplyRun(ctx context.Context, clients *Clients, rc *RunClients, req *RunRequest, out io.Writer) error {
	t, err := read(ctx, rc, req, org.ApplyCommand, out)
	if err != nil {
		return err
	}
	infra, err := AppToken(ctx, rc, clients.GitHub, t.p, org.InfrastructureApp)
	if err != nil {
		return err
	}
	// A creation's hand-off pull request is found and opened as the deployer app.
	deployer, err := AppToken(ctx, rc, clients.GitHub, t.p, org.DeployerApp)
	if err != nil {
		return err
	}
	locks, err := clients.Locks(ctx)
	if err != nil {
		return err
	}
	defer locks.Close()
	now := time.Now
	if clients.Now != nil {
		now = clients.Now
	}
	data, err := json.Marshal(lock{Build: req.BuildID, Commit: t.commit, Since: now().UTC().Format(time.RFC3339)})
	if err != nil {
		return errors.Wrap(err, "json.Marshal()")
	}
	held, err := locks.Lock(ctx, t.p.StateBucket, LockObject, data)
	if errors.Is(err, ErrLocked) {
		var other lock
		_ = json.Unmarshal(held, &other)

		return errors.Newf("another apply holds the lock gs://%s/%s (build %s at %s, since %s): wait for it; a run that died leaves the lock, and deleting the object frees it", t.p.StateBucket, LockObject, other.Build, short(other.Commit), other.Since)
	}
	if err != nil {
		return err
	}
	defer func() { _ = locks.Unlock(ctx, t.p.StateBucket, LockObject) }()
	report, err := Run(ctx, clients, &Request{Placement: t.p, Dir: req.Dir, Mode: Apply, Commit: t.commit, GitHubToken: infra, PullRequestToken: deployer}, out)
	if err != nil {
		return err
	}
	report.Write(out, Apply)
	for _, c := range report.Created {
		if c.Already {
			fmt.Fprintf(out, "%s was created by an earlier run; its hand-off pull request: %s\n", c.Prefix, c.PullRequest)
		} else {
			fmt.Fprintf(out, "Created %s; its hand-off pull request: %s\n", c.Prefix, c.PullRequest)
		}
	}

	return WriteMarker(ctx, clients, t.p, t.commit, report.Applied)
}

// GitHubClient is the GitHub client over a token, the engine's default.
func GitHubClient(token string) *github.Client {
	return github.New(github.DefaultBase, token)
}
