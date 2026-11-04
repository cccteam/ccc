// create.go is the creation pass of bedrock org apply in a root organization: after the
// layers, each hosting organization placement.json records without a folder yet is
// created (internal/create), in record order, as the creating identity, and handed off
// with a pull request. A run that creates nothing says so in a line; a creation that
// fails stops the run, and the next push finishes it, since every step of a creation
// looks before it makes.

package layers

import (
	"context"
	"fmt"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/create"
	"github.com/cccteam/ccc/bedrock/internal/deploy"
)

// createAll creates the pending hosting organizations of a root organization; nothing in
// a hosting organization.
func (r *run) createAll(ctx context.Context, store deploy.Store) error {
	if !r.p.IsRoot() {
		return nil
	}
	pending := r.p.Pending()
	if len(pending) == 0 {
		return nil
	}
	if r.clients.Google == nil || r.clients.Sleep == nil {
		return errors.New("no Google client as an identity is wired for the creation")
	}
	token, err := r.infrastructureToken(ctx)
	if err != nil {
		return errors.Wrapf(err, "the creation pushes the new repository and opens the hand-off as the infrastructure GitHub App")
	}
	clients := &create.Clients{Google: r.clients.Google, GitHub: r.clients.GitHub, Exec: r.clients.Exec, Store: store, Sleep: r.clients.Sleep}
	for _, record := range pending {
		req := &create.Request{Root: r.p, RootDir: r.req.Dir, Record: record, Token: token, Commit: r.subs[commitSub]}
		result, err := create.Run(ctx, clients, req, r.out)
		if err != nil {
			return errors.Wrapf(err, "the creation of %s failed; the log says why", record.Prefix)
		}
		if result.Already {
			continue
		}
		fmt.Fprintf(r.out, "%s is created; its hand-off pull request records the folder: %s.\n", record.Prefix, result.PullRequest)
	}

	return nil
}

// creationPreview says, on a pull request of a root organization, what its merge
// creates: each pending organization by prefix, boot project and repository.
func (r *run) creationPreview() string {
	if !r.p.IsRoot() {
		return ""
	}
	preview := ""
	for _, h := range r.p.Pending() {
		preview += fmt.Sprintf("The merge creates %s: folder %s at the organization node, boot project %s, repository %s/%s-infrastructure or as its record names it (%s/%s/placement.json), as %s, with the first applies of its two foundation layers and the push of its repository; then the hand-off pull request.\n",
			h.Prefix, h.Prefix, h.BootProject, r.p.GithubOrganization, h.Prefix, "organizations", h.Prefix, r.p.CreatingIdentity())
	}

	return preview
}
