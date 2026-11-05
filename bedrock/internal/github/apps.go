// apps.go is the GitHub Apps' own API: what an app is to itself, where it is installed,
// and the conversion that makes an app from a manifest, which bedrock org bootstrap makes
// the three apps with. The first two take the app's JWT, signed with its private key; the
// conversion takes no credential, since the code GitHub hands the browser is the whole
// of it.

package github

import (
	"context"
	"net/http"
	"net/url"
)

// App is a GitHub App as GitHub describes it to the app itself (GET /app), or as the
// conversion of a manifest hands it over: then with the private key, the client secret
// and the webhook secret, which GitHub gives that once and never again.
type App struct {
	ID       int64   `json:"id"`
	Slug     string  `json:"slug"`
	Name     string  `json:"name"`
	ClientID string  `json:"client_id"`
	HTMLURL  string  `json:"html_url"`
	Owner    Account `json:"owner"`
	// Permissions are the app's, by GitHub's name of each (contents, administration,
	// organization_self_hosted_runners) to its level (read, write, admin).
	Permissions map[string]string `json:"permissions"`
	// Events are the webhook events the app subscribes to; none for an app without a
	// webhook.
	Events        []string `json:"events"`
	PEM           string   `json:"pem,omitempty"`
	ClientSecret  string   `json:"client_secret,omitempty"`
	WebhookSecret string   `json:"webhook_secret,omitempty"`
}

// Account is a user or an organization on GitHub, by its login.
type Account struct {
	Login string `json:"login"`
	// Type is User or Organization.
	Type string `json:"type"`
}

// Installation is one installation of an app: on which account, and on all of its
// repositories or a selection of them.
type Installation struct {
	ID      int64   `json:"id"`
	AppID   int64   `json:"app_id"`
	Account Account `json:"account"`
	// RepositorySelection is all or selected.
	RepositorySelection string `json:"repository_selection"`
}

// AllRepositories is an installation's repository selection when it covers every
// repository of the account, the ones made after it included.
const AllRepositories = "all"

// App is the app the client's JWT signs as: GitHub's answer to the app itself, which it
// gives for a JWT signed with the app's own key alone. The client's token is the app's
// JWT, not an installation token.
func (c *Client) App(ctx context.Context) (*App, error) {
	var out App
	if err := c.do(ctx, http.MethodGet, "/app", nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}

// AppInstallations lists where the app the client's JWT signs as is installed. One page
// holds a hundred, and an app bedrock acts as is installed on one organization.
func (c *Client) AppInstallations(ctx context.Context) ([]Installation, error) {
	var out []Installation
	if err := c.do(ctx, http.MethodGet, "/app/installations?per_page=100", nil, &out); err != nil {
		return nil, err
	}

	return out, nil
}

// ConvertManifest makes the app a manifest describes, from the code GitHub sent the
// browser to the manifest's redirect URL once a person submitted the form (the code is
// good for an hour, once). The answer carries the app's private key and client secret;
// the client needs no token for it.
func (c *Client) ConvertManifest(ctx context.Context, code string) (*App, error) {
	var out App
	if err := c.do(ctx, http.MethodPost, "/app-manifests/"+url.PathEscape(code)+"/conversions", struct{}{}, &out); err != nil {
		return nil, err
	}

	return &out, nil
}

// Manifest is a GitHub App manifest: what a browser posts to GitHub, as the manifest
// field of a form, for a person to make the app with one click. GitHub then sends the
// browser to the redirect URL with a code, which ConvertManifest turns into the app.
type Manifest struct {
	// Name is the app's name, unique across GitHub, at most 34 characters.
	Name string `json:"name"`
	// URL is the app's homepage.
	URL            string         `json:"url"`
	HookAttributes HookAttributes `json:"hook_attributes"`
	// RedirectURL is where GitHub sends the browser, with the code, once the app is
	// made.
	RedirectURL string `json:"redirect_url"`
	// SetupURL is where GitHub sends the browser once the app is installed, with the
	// installation's id; none leaves the browser on GitHub.
	SetupURL      string `json:"setup_url,omitempty"`
	SetupOnUpdate bool   `json:"setup_on_update"`
	Description   string `json:"description"`
	// Public says whether any account may install the app; false keeps it to the
	// organization that owns it.
	Public bool `json:"public"`
	// DefaultEvents are the webhook events; none for an app without a webhook.
	DefaultEvents []string `json:"default_events"`
	// DefaultPermissions are the app's permissions, by GitHub's name of each to its
	// level.
	DefaultPermissions map[string]string `json:"default_permissions"`
}

// HookAttributes is the manifest's webhook: off (Active false) for an app that is only
// ever a credential.
type HookAttributes struct {
	URL    string `json:"url,omitempty"`
	Active bool   `json:"active"`
}

// ManifestFormURL is where the browser posts a manifest to make an app owned by the
// organization: GitHub's page for a new app of the organization, carrying the state the
// redirect gives back, so the receiver knows the code is its own.
func ManifestFormURL(organization, state string) string {
	return "https://github.com/organizations/" + url.PathEscape(organization) + "/settings/apps/new?state=" + url.QueryEscape(state)
}
