// found.go is the founding pass of bedrock org apply in a root organization: after the
// layers, each hosting organization placement.json records without a folder yet is
// founded (internal/found), in record order, as the founding identity, and handed off
// with a pull request. A run that founds nothing says so in a line; a founding that
// fails stops the run, and the next push finishes it, since every step of a founding
// looks before it makes.

package layers

import (
	"context"
	"fmt"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/found"
)

// foundAll founds the pending hosting organizations of a root organization; nothing in
// a hosting organization.
func (r *run) foundAll(ctx context.Context, store deploy.Store) error {
	if !r.p.IsRoot() {
		return nil
	}
	pending := r.p.Pending()
	if len(pending) == 0 {
		return nil
	}
	if r.clients.Google == nil || r.clients.Sleep == nil {
		return errors.New("no Google client as an identity is wired for the founding")
	}
	token, err := r.infrastructureToken(ctx)
	if err != nil {
		return errors.Wrapf(err, "the founding pushes the new repository and opens the hand-off as the infrastructure GitHub App")
	}
	clients := &found.Clients{Google: r.clients.Google, GitHub: r.clients.GitHub, Exec: r.clients.Exec, Store: store, Sleep: r.clients.Sleep}
	for _, record := range pending {
		req := &found.Request{Root: r.p, RootDir: r.req.Dir, Record: record, Token: token, Commit: r.subs[commitSub]}
		result, err := found.Run(ctx, clients, req, r.out)
		if err != nil {
			return errors.Wrapf(err, "the founding of %s failed; the log says why", record.Prefix)
		}
		if result.Already {
			continue
		}
		fmt.Fprintf(r.out, "%s is founded; its hand-off pull request records the folder: %s.\n", record.Prefix, result.PullRequest)
	}

	return nil
}

// foundingPreview says, on a pull request of a root organization, what its merge
// founds: each pending organization by prefix, boot project and repository.
func (r *run) foundingPreview() string {
	if !r.p.IsRoot() {
		return ""
	}
	preview := ""
	for _, h := range r.p.Pending() {
		preview += fmt.Sprintf("The merge founds %s: folder %s at the organization node, boot project %s, repository %s/%s-infrastructure or as its record names it (%s/%s/placement.json), as %s, with the first applies of its two foundation layers and the push of its repository; then the hand-off pull request.\n",
			h.Prefix, h.Prefix, h.BootProject, r.p.GithubOrganization, h.Prefix, "organizations", h.Prefix, r.p.FoundingIdentity())
	}

	return preview
}
