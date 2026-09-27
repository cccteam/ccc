package deploy

import (
	"context"
	"fmt"
	"io"

	"github.com/go-playground/errors/v5"
)

// The facts the release check adds to the environment file: the digest the later steps
// deploy, and whether the image build stands aside.
const (
	reuseImageFact = "REUSE_IMAGE"
)

// CheckRelease decides whether the image build runs. One registry serves every
// environment and the image tags carry the environment (<release>-<env>, <commit>-<env>),
// so the release tag's digest and the commit tag's digest tell the story: neither exists,
// the build runs; the commit is built and the release tag is not, the release name is
// added to that build and nothing is rebuilt (a tag moved, or a pull-request release
// whose commit was fast-forwarded to the default branch); both exist and agree, the
// build is reused; the release tag names another build, the run is refused, since a
// release names one build per environment.
func CheckRelease(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if env[skipDeploy] == trueValue {
		fmt.Fprintln(out, tornDown)

		return nil
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	image, imageTag, commitTag := env[imageFact], env[imageTagFact], env[commitTagFact]
	if image == "" || imageTag == "" || commitTag == "" {
		return errors.Newf("%s names no image or tags (IMAGE, IMAGE_TAG, COMMIT_TAG): the resolve step writes them", EnvironmentFile)
	}
	registry, err := clients.Registry(ctx)
	if err != nil {
		return err
	}
	existing, err := registry.Digest(ctx, image, imageTag)
	if err != nil {
		return err
	}
	byCommit, err := registry.Digest(ctx, image, commitTag)
	if err != nil {
		return err
	}
	commit, environment := build.Substitutions[commitSub], build.Substitutions[envSub]
	if existing == "" {
		if byCommit != "" {
			fmt.Fprintf(out, "Commit %s is already built for %s as %s@%s; naming it %s and reusing it.\n", commit, environment, image, byCommit, imageTag)
			if err := registry.AddTag(ctx, image, imageTag, byCommit); err != nil {
				return err
			}

			return w.Append(map[string]string{digestFact: byCommit, reuseImageFact: trueValue})
		}
		fmt.Fprintf(out, "Release %s is not in the registry for %s yet; BuildImage builds it.\n", env[releaseFact], environment)

		return w.Append(map[string]string{reuseImageFact: ""})
	}
	none := byCommit
	if none == "" {
		none = "none"
	}
	fmt.Fprintf(out, "Release tag digest: %s; commit tag digest: %s\n", existing, none)
	if existing != byCommit {
		return errors.Newf("%s%s:%s already exists and was not built from commit %s; a release names one build per environment.", rejected, image, imageTag, commit)
	}
	fmt.Fprintf(out, "Reusing %s:%s@%s, built from this commit by an earlier run.\n", image, imageTag, existing)

	return w.Append(map[string]string{digestFact: existing, reuseImageFact: trueValue})
}
