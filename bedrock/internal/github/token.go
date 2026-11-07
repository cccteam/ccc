package github

import (
	"context"
	"strings"

	"github.com/go-playground/errors/v5"
)

// appIssued are the prefixes GitHub puts on the tokens its apps are issued (GitHub's token
// formats, docs.github.com/en/authentication/keeping-your-account-and-data-secure/about-authentication-to-github),
// each with what the token is. A personal access token starts with ghp_ (classic) or
// github_pat_ (fine-grained) and is not among them.
var appIssued = []struct{ prefix, kind string }{
	{"ghu_", "a GitHub App's user access token, which a GitHub App issued to the account that authorized it (Cloud Build's console leaves one in a secret of the connection it makes)"},
	{"ghs_", "a GitHub App's installation access token, which GitHub issued to an installation of the app for an hour"},
	{"gho_", "an OAuth app's access token, which GitHub issued to the account that authorized the app"},
	{"ghr_", "a GitHub App's refresh token, which GitHub issued to renew a user access token"},
}

// IssuedByApp says what a token that an app was issued is, by its prefix, or answers ""
// for any other token (a personal access token among them).
func IssuedByApp(token string) string {
	for _, t := range appIssued {
		if strings.HasPrefix(token, t.prefix) {
			return t.kind
		}
	}

	return ""
}

// BotAccount reads whose the token is and that the account owns the organization, through
// a client opened with it, and answers the login: the GitHub Bot Account, whose token every
// Cloud Build connection authorizes with. A token an app was issued is refused first, by its
// prefix, as not a personal access token, whatever account it names; so are a token of no
// member and a member's that is not an owner's.
func BotAccount(ctx context.Context, open func(token string) *Client, token, organization string) (string, error) {
	if kind := IssuedByApp(token); kind != "" {
		return "", errors.Newf("the token passed is %s, not a personal access token: the Bot Account's token is a classic personal access token with the repo, read:user and read:org scopes, made in the Bot Account's settings (Developer settings, Personal access tokens, Tokens (classic))", kind)
	}
	gh := open(token)
	login, err := gh.User(ctx)
	if err != nil {
		return "", errors.Wrap(err, "GitHub, asked whose the token is")
	}
	role, err := gh.OrgMembership(ctx, organization)
	if err != nil {
		if NotFound(err) {
			return "", errors.Newf("%s, whose token this is, is not a member of the %s organization: the Bot Account is an owner of it, and its token is the one to pass", login, organization)
		}

		return "", errors.Wrapf(err, "GitHub, asked about %s's membership of %s", login, organization)
	}
	if role != "admin" {
		return "", errors.Newf("%s, whose token this is, is a %s of the %s organization, not an owner: the Bot Account is an owner, and Cloud Build's connection authorizes as it", login, role, organization)
	}

	return login, nil
}
