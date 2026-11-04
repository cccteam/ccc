// orgsecrets.go is bedrock org secrets, the root organization's commands on the GitHub
// Apps' keys it holds for every organization: check, which proves a version of a key with
// GitHub before bedrock secret pin and the pull request's apply move the alias to it
// (0-bootstrap/README.md, "Rotating a GitHub App's key").

package cli

import (
	"context"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

func newOrgSecrets(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "The GitHub Apps' keys a root organization holds for every organization",
		Long: `secrets is the root organization's view of the three secrets it holds for every
organization in the Google Cloud organization: the infrastructure, deployer and release GitHub
Apps' private keys, one secret per app in its boot project, each read through its alias
current by every pipeline in every organization. A key is added as a version with bedrock
secret add, proven with secrets check, and pinned with bedrock secret pin, whose pull request's
apply moves the alias.`,
	}
	cmd.AddCommand(newOrgSecretsCheck(d))

	return cmd
}

func newOrgSecretsCheck(d deps) *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   "check <secret> <version>",
		Short: "Prove a version of a GitHub App's key with GitHub before the alias moves to it",
		Long: `check proves a version of a GitHub App's key before anything reads it: the version must be
enabled in Secret Manager, and a token signed with it, issued by the App ID placement.json
records for the app (githubInfrastructureAppId, githubDeployerAppId, githubReleaseAppId),
must be accepted by GitHub as that app, which GitHub does for the app's own key alone. It
reads the version with the run's Google credentials (gcloud auth application-default login,
as a key operator placement.json names) and prints the app GitHub answered with, the version
the alias current is pinned to, and the pin to make. Nothing moves: the alias stays where
placement.json pins it until bedrock secret pin and the pull request's apply. A version that
is not the app's key, or an App ID that is another app's, is refused with GitHub's answer. It
runs in the root organization's infrastructure repository; a hosting organization's names
the root organization instead.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, p, err := d.orgRepository(dir)
			if err != nil {
				return err
			}
			if p == nil {
				return errors.New("not in an organization's infrastructure repository (no placement.json naming an organization at its root): run secrets check at the root organization's root, or pass --dir")
			}
			if err := p.KeysElsewhere(); err != nil {
				return err
			}
			project, container, err := orgSecretPlace(p, &pinFlags{}, args[0])
			if err != nil {
				return err
			}
			r, err := org.CheckKey(cmd.Context(), d.secrets, d.keyAccess, d.appIdentity, p, &org.KeyCheckRequest{Secret: args[0], Version: args[1], Project: project, Container: container})
			if err != nil {
				return err
			}
			r.Write(cmd.OutOrStdout())

			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "the repository root (default: found from the working directory)")

	return cmd
}

// keyAccess reads a key's version through the deploy clients' Secret Manager.
func (d deps) keyAccess(ctx context.Context, version string) ([]byte, error) {
	if d.deploy == nil || d.deploy.Secrets == nil {
		return nil, errors.New("no Secret Manager client is wired for the read")
	}
	secrets, err := d.deploy.Secrets(ctx)
	if err != nil {
		return nil, err
	}
	defer secrets.Close()

	return secrets.Access(ctx, version)
}

// appIdentity asks GitHub which app a key signs as, through the deploy clients' GitHub.
func (d deps) appIdentity(ctx context.Context, appID string, pemKey []byte) (*org.AppIdentity, error) {
	if d.deploy == nil || d.deploy.GitHub == nil {
		return nil, errors.New("no GitHub client is wired for the check")
	}
	app, err := deploy.AppIdentity(ctx, d.deploy.GitHub, appID, pemKey)
	if err != nil {
		return nil, err
	}

	return &org.AppIdentity{ID: app.ID, Slug: app.Slug, Name: app.Name}, nil
}
