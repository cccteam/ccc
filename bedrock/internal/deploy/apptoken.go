// apptoken.go lends the pipeline's GitHub App sign-in to the organization's commands:
// bedrock org plan posts the layers' plans as the deployer app and mints 1-org's GitHub
// token from the infrastructure app's key, with the same JWT and installation token the
// pipeline's talk-back makes.

package deploy

import (
	"context"
	"time"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

// AppInstallationToken mints a GitHub App's installation token on the repository from
// the app's private key in PEM: a JWT signed with the key asks GitHub for the app's
// installation on the repository and then for a token good for an hour. what names the
// app in a refusal (the deployer app, the infrastructure app).
func AppInstallationToken(ctx context.Context, gh GitHubFunc, appID string, pemKey []byte, owner, repo, what string) (string, error) {
	jwt, err := appJWT(appID, pemKey, time.Now())
	if err != nil {
		return "", errors.Wrapf(err, "%s's key", what)
	}
	app := gh(jwt)
	installation, err := app.RepositoryInstallation(ctx, owner, repo)
	if err != nil {
		return "", errors.Wrapf(err, "%s %s's installation on %s/%s", what, appID, owner, repo)
	}
	token, err := app.InstallationToken(ctx, installation)
	if err != nil {
		return "", errors.Wrapf(err, "a token for %s %s's installation %d", what, appID, installation)
	}

	return token, nil
}

// AppIdentity asks GitHub which app a key signs as: a JWT issued by appID and signed with
// the key in PEM fetches the app's own record, which GitHub gives for the app's own key
// alone. A key that is not the app's, or an App ID that is another app's, is refused by
// GitHub. bedrock org secrets check proves a key's version with it.
func AppIdentity(ctx context.Context, gh GitHubFunc, appID string, pemKey []byte) (*github.App, error) {
	jwt, err := appJWT(appID, pemKey, time.Now())
	if err != nil {
		return nil, errors.Wrap(err, "the key")
	}
	app, err := gh(jwt).App(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "GitHub, asked for the app a token signed with the key is (App %s)", appID)
	}

	return app, nil
}

// ParseBuild reads a build as Cloud Build's API describes it: its id and substitutions.
func ParseBuild(data []byte) (*Build, error) {
	return parseBuild(data)
}
