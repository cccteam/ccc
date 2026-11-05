// tokencheck.go is bedrock org secrets check for the GitHub Bot Account's token: a version
// added to the root organization's token secret is proven with GitHub before the alias
// moves to it. The version is read and GitHub is asked whose it is and that the account
// owns the organization; a token an app was issued is refused by its prefix first, as the
// bootstrap refuses it. Nothing moves: the alias stays where the placement pins it until
// bedrock secret pin and the pull request's apply.

package org

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/secret"
)

// TokenIdentityFunc asks GitHub whose the token is and that the account owns the
// organization, answering the login; a token that is not a personal access token, not a
// member's or not an owner's is refused with why.
type TokenIdentityFunc func(ctx context.Context, token, organization string) (string, error)

// TokenCheckResult is what the token's check found.
type TokenCheckResult struct {
	Version   string
	Project   string
	Container string
	// Login is the account GitHub says the token is, the Bot Account's; Organization the
	// GitHub organization it owns.
	Login        string
	Organization string
	// Pinned is the version the alias points at as placement.json pins it, or empty.
	Pinned string
}

// CheckToken proves the version of the Bot Account's token: it must be enabled, and GitHub
// must say it is the Bot Account's (githubBotAccount) and the account an owner of the
// organization. A hosting organization is sent to the root organization that holds it.
func CheckToken(ctx context.Context, open secret.ClientFunc, access KeyAccessFunc, identity TokenIdentityFunc, p *Placement, req *KeyCheckRequest) (*TokenCheckResult, error) {
	if req.Secret != Token {
		return nil, errors.Newf("%q is not the Bot Account's token secret (%s)", req.Secret, Token)
	}
	if err := p.KeysElsewhere(); err != nil {
		return nil, err
	}
	if !secretVersionRE.MatchString(req.Version) {
		return nil, errors.Newf("version %q: the token's version is checked by its number, never %s", req.Version, secret.Latest)
	}
	if err := verifySecret(ctx, open, &SecretPinRequest{Secret: req.Secret, Version: req.Version, Project: req.Project, Container: req.Container}); err != nil {
		return nil, err
	}
	name := "projects/" + req.Project + "/secrets/" + req.Container + "/versions/" + req.Version
	token, err := access(ctx, name)
	if err != nil {
		return nil, errors.Wrapf(err, "reading version %s of %s in project %s", req.Version, req.Container, req.Project)
	}
	login, err := identity(ctx, strings.TrimSpace(string(token)), p.GithubOrganization)
	if err != nil {
		// The command prints the cause alone, so the version is named in it.
		return nil, errors.Newf("version %s of %s is not the Bot Account's token: %s; nothing changes", req.Version, req.Container, errors.Cause(err))
	}
	if login != p.GithubBotAccount {
		return nil, errors.Newf("version %s of %s is %s's token, not the Bot Account %s's (githubBotAccount in placement.json): nothing changes", req.Version, req.Container, login, p.GithubBotAccount)
	}

	return &TokenCheckResult{Version: req.Version, Project: req.Project, Container: req.Container, Login: login, Organization: p.GithubOrganization, Pinned: p.GithubTokenVersion}, nil
}

// Write prints what was proven and what moves the alias.
func (r *TokenCheckResult) Write(w io.Writer) {
	fmt.Fprintf(w, "Version %s of %s in project %s is enabled, and GitHub says it is %s's, an owner of %s: the Bot Account's token.\n", r.Version, r.Container, r.Project, r.Login, r.Organization)
	switch r.Pinned {
	case r.Version:
		fmt.Fprintf(w, "The alias %s is pinned to version %s already (%s in placement.json); every Cloud Build connection names it.\n", KeyAlias, r.Version, pinKey(Token))
	case "":
		fmt.Fprintf(w, "No version is pinned yet (%s in placement.json): the alias %s names none, and nothing reads version %s.\n", pinKey(Token), KeyAlias, r.Version)
	default:
		fmt.Fprintf(w, "The alias %s is pinned to version %s (%s in placement.json); nothing reads version %s yet.\n", KeyAlias, r.Pinned, pinKey(Token), r.Version)
	}
	if r.Pinned != r.Version {
		fmt.Fprintf(w, "Next: bedrock secret pin %s %s, then the pull request with the render; its apply moves the alias, and every Cloud Build connection in every organization reads it. %s\n", Token, r.Version, retireOld(Token))
	}
}
