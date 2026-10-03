// talkback.go is the pull-request build's last word.

package deploy

import (
	"context"
	"fmt"
	"io"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

// TalkBack says on the pull request what the build did, as the deployer app: a GitHub
// deployment named after the pull request's environment (<app>-pr<N>) carrying its URL,
// which the pull request's sidebar shows and "View deployment" opens, and a comment with
// the release and the database mode. A teardown (/gcbrun down) marks every deployment of
// the environment inactive instead. A tag build, or an environment without a deployer
// app yet, says nothing.
func TalkBack(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	pr, err := speaker(ctx, clients, build, env, false)
	if err != nil {
		return err
	}
	if pr == nil {
		fmt.Fprintln(out, "No talk-back: not a pull-request build, or no deployer app in this environment yet.")

		return nil
	}
	subs := build.Substitutions
	environment := subs[appSub] + "-pr" + subs[prNumberSub]
	logURL := fmt.Sprintf("https://console.cloud.google.com/cloud-build/builds;region=%s/%s?project=%s", env[locationFact], build.ID, env[projectFact])
	var body string
	if env[downFact] == trueValue {
		ids, err := pr.gh.Deployments(ctx, pr.owner, pr.repo, environment)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := pr.gh.CreateDeploymentStatus(ctx, pr.owner, pr.repo, id, &github.DeploymentStatus{State: "inactive", LogURL: logURL, Description: "Destroyed by /gcbrun down"}); err != nil {
				return err
			}
		}
		body = fmt.Sprintf("The environment %s is destroyed (/gcbrun down, build %s).", environment, build.ID)
	} else {
		url := "https://" + env[prHostnameFact] + "/"
		id, err := pr.gh.CreateDeployment(ctx, pr.owner, pr.repo, &github.DeploymentRequest{
			Ref: subs[commitSub], Environment: environment, Description: "Cloud Build " + build.ID,
			TransientEnvironment: true, RequiredContexts: []string{},
		})
		if err != nil {
			return err
		}
		if err := pr.gh.CreateDeploymentStatus(ctx, pr.owner, pr.repo, id, &github.DeploymentStatus{
			State: "success", EnvironmentURL: url, LogURL: logURL, AutoInactive: true, Description: "Deployed " + env[releaseFact],
		}); err != nil {
			return err
		}
		body = fmt.Sprintf("Deployed %s to %s with %s (build %s).", env[releaseFact], url, databaseMode(env, subs[envSub]), build.ID)
	}
	if err := pr.comment(ctx, body, out); err != nil {
		return err
	}
	fmt.Fprintf(out, "Talked back on pull request %d: %s\n", pr.number, body)

	return nil
}

// databaseMode says which database the pull request's environment runs against.
func databaseMode(env map[string]string, environment string) string {
	switch {
	case env[sharedDBFact] == trueValue:
		return environment + "'s database (shared-db)"
	case env[reloadDBFact] == trueValue:
		return "its own database, recreated this build (" + env[reloadReasonFact] + ")"
	default:
		return "its own database"
	}
}
