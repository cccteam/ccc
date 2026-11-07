// keycheck.go is bedrock org secrets check: a version of a GitHub App's key, added to the
// root organization's secret, is proven with GitHub before the alias moves to it. A JWT
// signed with the version, issued by the App ID placement.json records, asks GitHub for the
// app itself; GitHub answers for the app's own key alone, so an answer proves the version
// and a refusal says the version is not the app's. Nothing moves: the alias stays where
// the placement pins it until bedrock secret pin and the pull request's apply.

package org

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/secret"
)

// KeyAccessFunc reads a secret version's payload by its resource name.
type KeyAccessFunc func(ctx context.Context, version string) ([]byte, error)

// AppIdentity is a GitHub App as GitHub describes it to the app itself.
type AppIdentity struct {
	ID   int64
	Slug string
	Name string
}

// AppIdentityFunc asks GitHub which app a key signs as, with a JWT issued by appID and
// signed with the key in PEM; GitHub refuses a key that is not the app's.
type AppIdentityFunc func(ctx context.Context, appID string, pemKey []byte) (*AppIdentity, error)

// KeyCheckRequest is what org secrets check was asked to prove.
type KeyCheckRequest struct {
	// Secret is the organization secret's name; Version the version's number.
	Secret  string
	Version string
	// Project and Container are where the version lives.
	Project   string
	Container string
}

// KeyCheckResult is what the check found.
type KeyCheckResult struct {
	Secret    string
	Version   string
	Project   string
	Container string
	// AppID is the App ID the placement records for the key's app, which the JWT was
	// issued by; App is GitHub's answer.
	AppID string
	App   *AppIdentity
	// Pinned is the version the alias points at as placement.json pins it, or empty.
	Pinned string
}

// CheckKey proves the version: it must be enabled, GitHub must answer a JWT signed with
// it as the app placement.json records, and the answer must be that app. A hosting
// organization is sent to the root organization that holds the keys; an app not
// recorded is refused, since there is nothing to sign as.
func CheckKey(ctx context.Context, open secret.ClientFunc, access KeyAccessFunc, identity AppIdentityFunc, p *Placement, req *KeyCheckRequest) (*KeyCheckResult, error) {
	if err := ValidateSecret(req.Secret); err != nil {
		return nil, err
	}
	if err := p.KeysElsewhere(); err != nil {
		return nil, err
	}
	if !secretVersionRE.MatchString(req.Version) {
		return nil, errors.Newf("version %q: a key's version is checked by its number, never %s", req.Version, secret.Latest)
	}
	appID := p.app(req.Secret)
	if appID == "" {
		return nil, errors.Newf("placement.json records no %s: record the app's App ID there, from its settings page, since the check signs as the app", appKey(req.Secret))
	}
	if err := verifySecret(ctx, open, &SecretPinRequest{Secret: req.Secret, Version: req.Version, Project: req.Project, Container: req.Container}); err != nil {
		return nil, err
	}
	name := "projects/" + req.Project + "/secrets/" + req.Container + "/versions/" + req.Version
	pemKey, err := access(ctx, name)
	if err != nil {
		return nil, errors.Wrapf(err, "reading version %s of %s in project %s", req.Version, req.Container, req.Project)
	}
	app, err := identity(ctx, appID, pemKey)
	if err != nil {
		return nil, errors.Wrapf(err, "GitHub refuses a token signed with version %s of %s as App %s (%s): the version is not that app's key, or %s records another app's id; nothing changes", req.Version, req.Container, appID, appKey(req.Secret), appKey(req.Secret))
	}
	if strconv.FormatInt(app.ID, 10) != appID {
		return nil, errors.Newf("a token signed with version %s of %s is App %d (%s), not App %s as %s records: nothing changes", req.Version, req.Container, app.ID, app.Slug, appID, appKey(req.Secret))
	}

	return &KeyCheckResult{Secret: req.Secret, Version: req.Version, Project: req.Project, Container: req.Container, AppID: appID, App: app, Pinned: *p.pinned(req.Secret)}, nil
}

// Write prints what was proven and what moves the alias.
func (r *KeyCheckResult) Write(w io.Writer) {
	fmt.Fprintf(w, "Version %s of %s in project %s is enabled, and GitHub accepts a token signed with it as %s: App %s (%s).\n", r.Version, r.Container, r.Project, secretWhat(r.Secret), r.AppID, r.App.Slug)
	switch r.Pinned {
	case r.Version:
		fmt.Fprintf(w, "The alias %s is pinned to version %s already (%s in placement.json); every reader names it.\n", KeyAlias, r.Version, pinKey(r.Secret))
	case "":
		fmt.Fprintf(w, "No version is pinned yet (%s in placement.json): the alias %s names none, and nothing reads version %s.\n", pinKey(r.Secret), KeyAlias, r.Version)
	default:
		fmt.Fprintf(w, "The alias %s is pinned to version %s (%s in placement.json); nothing reads version %s yet.\n", KeyAlias, r.Pinned, pinKey(r.Secret), r.Version)
	}
	if r.Pinned != r.Version {
		fmt.Fprintf(w, "Next: bedrock secret pin %s %s, then the pull request with the render; its apply moves the alias, and every reader in every organization switches. Delete the old key on GitHub once the next run has gone through clean.\n", r.Secret, r.Version)
	}
}
