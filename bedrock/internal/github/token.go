package github

import "strings"

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
