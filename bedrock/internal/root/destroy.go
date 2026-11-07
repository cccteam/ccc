// destroy.go is bedrock org destroy for a root organization: a person deletes the root
// project, and with it the secrets, the zones, the connection, the triggers, the
// identities and the state bucket, takes the creating identity's grants off the
// organization node and the billing account, and deletes the folder. The repository
// stays on GitHub, with its code and pull requests, and the apps stay with their
// installations: a bootstrap again brings them as brought apps, with their keys, or
// makes new ones.

package root

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// DestroyRequest is one destroy: the placement (the folder is found by its name), and
// the GitHub token the repository's ruleset is brought to the destroyed state with (the
// person's, since the Bot Account's goes with the project); nil leaves the ruleset alone.
type DestroyRequest struct {
	Placement   *org.Placement
	GithubToken func(context.Context) (string, error)
}

// CanDestroy is why the root organization cannot be destroyed yet, or nil: a hosting
// organization recorded as created (hostingOrganizations with a folder) is destroyed
// first, in its own repository, and its entry taken out.
func CanDestroy(p *org.Placement) error {
	if !p.IsRoot() {
		return errors.Newf("the %s organization is a hosting organization: bedrock org destroy destroys it through its layers", p.Prefix)
	}
	var created []string
	for _, h := range p.CreatedHostingOrganizations() {
		created = append(created, h.Prefix)
	}
	if len(created) > 0 {
		return errors.Newf("placement.json records %s as created hosting organizations (hostingOrganizations with a folder): destroy each in its own repository first (bedrock org destroy there), then take its entry out and destroy the root organization", strings.Join(created, ", "))
	}

	return nil
}

// Destroy deletes the root organization.
func Destroy(ctx context.Context, clients *Clients, req *DestroyRequest, out io.Writer) error {
	p := req.Placement
	if err := CanDestroy(p); err != nil {
		return err
	}
	google, err := clients.Google(ctx)
	if err != nil {
		return err
	}
	defer google.Close()
	iam, err := clients.IAM(ctx)
	if err != nil {
		return err
	}
	defer iam.Close()
	d := NewDesired(p)
	for _, g := range d.OrganizationGrants() {
		if err := iam.RevokeOrganization(ctx, p.OrganizationID, g.Member, []string{g.Role}); err != nil {
			return err
		}
	}
	for _, g := range d.BillingGrants() {
		if err := iam.RevokeBilling(ctx, p.BillingAccount, g.Member, []string{g.Role}); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "Took the creating identity's and the runners' grants off the organization node, and the creating identity's off billing account %s.\n", p.BillingAccount)
	project := p.Projects[org.RootProjectKey]
	if project != "" {
		exists, err := google.ProjectExists(ctx, project)
		if err != nil {
			return err
		}
		if exists {
			if err := iam.DeleteProject(ctx, project); err != nil {
				return err
			}
			fmt.Fprintf(out, "Deleted the project %s, with the secrets, the zones, the connection, the triggers, the identities and the state bucket: Google holds it for 30 days, and its id never returns.\n", project)
		} else {
			fmt.Fprintf(out, "The project %s is gone already.\n", project)
		}
	}
	folder, err := google.FindFolder(ctx, "organizations/"+p.OrganizationID, p.Prefix)
	if err != nil {
		return err
	}
	if folder != "" {
		if err := iam.DeleteFolder(ctx, folder); err != nil {
			return errors.Wrapf(err, "the folder %s refuses to go while a project under it is being deleted; run bedrock org destroy again in a little while", folder)
		}
		fmt.Fprintf(out, "Deleted the folder %s (%s).\n", p.Prefix, folder)
	}

	return dropPlanCheck(ctx, clients, req, out)
}

// dropPlanCheck takes the plan trigger's check out of the repository's default-branch
// ruleset, since the trigger is gone with the project: the journal line's pull request
// then merges on the review alone, until a bootstrap again requires the check. It looks
// before it changes, so a destroy run again finds nothing to do.
func dropPlanCheck(ctx context.Context, clients *Clients, req *DestroyRequest, out io.Writer) error {
	if req.GithubToken == nil || clients.GitHub == nil {
		return nil
	}
	p := req.Placement
	owner, repo := p.GithubOrganization, p.SourceRepo
	token, err := req.GithubToken(ctx)
	if err != nil {
		return errors.Wrapf(err, "the ruleset of %s/%s still requires the plan trigger's check, which is gone with the project; taking it out needs a GitHub token (GITHUB_TOKEN, or gh's)", owner, repo)
	}
	gh := clients.GitHub(token)
	listed, err := gh.Rulesets(ctx, owner, repo)
	if err != nil {
		return errors.Wrapf(err, "listing the rulesets of %s/%s", owner, repo)
	}
	i := slices.IndexFunc(listed, func(rs github.Ruleset) bool { return rs.Name == rulesetName })
	if i < 0 {
		fmt.Fprintf(out, "The repository %s/%s has no ruleset %q: no check to take out.\n", owner, repo, rulesetName)

		return nil
	}
	rs, err := gh.Ruleset(ctx, owner, repo, listed[i].ID)
	if err != nil {
		return errors.Wrapf(err, "reading the ruleset %s of %s/%s", rulesetName, owner, repo)
	}
	j := slices.IndexFunc(rs.Rules, func(r github.Rule) bool { return r.Type == ruleRequiredChecks })
	if j < 0 {
		fmt.Fprintf(out, "The ruleset %q of %s/%s requires no check already: the journal line's pull request merges on the review alone.\n", rulesetName, owner, repo)

		return nil
	}
	rs.Rules = slices.Delete(rs.Rules, j, j+1)
	if err := gh.UpdateRuleset(ctx, owner, repo, rs.ID, rs); err != nil {
		return errors.Wrapf(err, "taking the plan trigger's check out of the ruleset %s of %s/%s", rulesetName, owner, repo)
	}
	fmt.Fprintf(out, "Took the plan trigger's check out of the ruleset %q of %s/%s: the trigger is gone with the project, and the journal line's pull request merges on the review alone until a bootstrap again requires the check.\n", rulesetName, owner, repo)

	return nil
}
