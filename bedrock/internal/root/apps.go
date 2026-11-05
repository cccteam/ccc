// apps.go holds a GitHub App to its declaration with its key: the key signs as the app,
// GitHub answers the app's own record and where it is installed, and the manifest says
// what differs.

package root

import (
	"context"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// VerifyApp asks GitHub, as the app the key signs as, for the app's record and its
// installations, and answers the manifest's findings against them: none when the app is
// as declared. A key that does not sign as the App ID is GitHub's refusal, an error.
func VerifyApp(ctx context.Context, gh deploy.GitHubFunc, m org.AppManifest, appID string, pem []byte, organization string) ([]string, error) {
	app, err := deploy.AppIdentity(ctx, gh, appID, pem)
	if err != nil {
		return nil, err
	}
	installations, err := deploy.AppInstallations(ctx, gh, appID, pem)
	if err != nil {
		return nil, err
	}

	return m.Verify(app, installations, organization), nil
}
