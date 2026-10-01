// buildimage.go is the image build.

package deploy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/secret"
)

// The image build's inputs and what it leaves.
const (
	// MetadataFile is what docker buildx writes about the image it pushed; its
	// containerimage.digest is the image's digest.
	MetadataFile = "image-metadata.json"
	// buildSecretsSub lists the declared build secrets: NAME=<secret version>, comma
	// separated.
	buildSecretsSub = "_BUILD_SECRETS"
	// cacheTagPrefix is the registry tag under which a build exports its layer cache,
	// followed by the commit: cache-<commit>. The layers are content-addressed, so a cache
	// changes nothing in what a build produces; it spares the work whose inputs are
	// unchanged (the dependency downloads, the browser build of an untouched web tree,
	// and for the environments after the first the whole image).
	cacheTagPrefix = "cache-"
)

// BuildImage builds the application's image from the checkout's Dockerfile and pushes
// it under its two tags (<release>-<env> and <commit>-<env>), unless the release check
// found this commit's build to reuse. The build arguments are VERSION and COMMIT, for an
// application with a job process JOBS_JOB (the resource name of the job this build makes
// for its revision, which the Dockerfile sets as the site's APP_JOBS_JOB, so the image
// names the job of its own build), the declared substitutions and what a hook before the
// build added (the build arguments file, NAME=value lines). Each declared build secret is read as the deploy identity by
// its pinned version into secretDir (memory-backed in Cloud Build, gone with the step,
// never in the workspace) and passed to docker as a BuildKit secret the Dockerfile
// mounts; it is never a build argument, which the image would keep. The digest the push
// answered goes to the environment file (IMAGE_DIGEST).
//
// With hooks set, the hooks program the image carries (/hooks) is taken out of the image,
// built or reused, and left at hooks for the hook steps after it.
func BuildImage(ctx context.Context, clients *Clients, w Workspace, secretDir, hooks string, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if env[skipDeploy] == trueValue {
		fmt.Fprintln(out, tornDown)

		return nil
	}
	if env[reuseImageFact] == trueValue {
		fmt.Fprintf(out, "Reusing %s@%s: the release check found this release already built from this commit.\n", env[imageFact], env[digestFact])

		return takeHooks(ctx, clients, env[imageFact]+"@"+env[digestFact], hooks, out)
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	buildArgs, err := w.BuildArgs()
	if err != nil {
		return err
	}
	args := []string{"buildx", "build", "--build-arg", "VERSION=" + env[versionFact], "--build-arg", "COMMIT=" + build.Substitutions[commitSub]}
	if env[jobsJobFact] != "" {
		_, job, err := buildJob(build.Substitutions[projectSub], env, jobsJobFact)
		if err != nil {
			return err
		}
		args = append(args, "--build-arg", "JOBS_JOB="+job)
	}
	for _, arg := range buildArgs {
		args = append(args, "--build-arg", arg)
	}
	secretArgs, cleanup, err := buildSecrets(ctx, clients, build.Substitutions[buildSecretsSub], secretDir, out)
	defer cleanup()
	if err != nil {
		return err
	}
	metadata := filepath.Join(string(w), MetadataFile)
	args = append(args, secretArgs...)
	sources, err := cacheSources(ctx, clients.Storage, build)
	if err != nil {
		return err
	}
	for _, commit := range sources {
		args = append(args, "--cache-from", "type=registry,ref="+env[imageFact]+":"+cacheTagPrefix+commit)
	}
	exported := env[imageFact] + ":" + cacheTagPrefix + build.Substitutions[commitSub]
	args = append(args, "--cache-to", "type=registry,ref="+exported+",mode=max,ignore-error=true")
	fmt.Fprintf(out, "Layer cache: read from %s, written to %s (a tag the registry already holds is left as it is).\n", cacheTagPrefix+strings.Join(sources, ", "+cacheTagPrefix), exported)
	args = append(args,
		"--tag", env[imageFact]+":"+env[commitTagFact],
		"--tag", env[imageFact]+":"+env[imageTagFact],
		"--metadata-file", metadata,
		"--file", "Dockerfile", "--push", ".")
	if err := clients.Exec.Run(ctx, Command{Dir: string(w), Name: dockerProgram, Args: args}, out); err != nil {
		return err
	}
	digest, err := imageDigest(metadata)
	if err != nil {
		return err
	}
	if err := w.Append(map[string]string{digestFact: digest}); err != nil {
		return err
	}
	fmt.Fprintf(out, "Built and pushed %s@%s\n", env[imageFact], digest)

	return takeHooks(ctx, clients, env[imageFact]+"@"+digest, hooks, out)
}

// cacheSources lists the commits whose layer caches the build reads, this commit's first:
// its own (another environment built this commit already, or the pull request's earlier
// build pushed it), the commit the environment runs live (the previous release's layers,
// most of which an ordinary change keeps), and in a pull-request build the pull request's
// last build. A cache the registry lacks is skipped by docker. The records are read
// through the records bucket; a build without one reads only its own commit's cache.
func cacheSources(ctx context.Context, open StoreFunc, build *Build) ([]string, error) {
	commit := build.Substitutions[commitSub]
	sources := []string{commit}
	add := func(c string) {
		if c != "" && !slices.Contains(sources, c) {
			sources = append(sources, c)
		}
	}
	bucket, app, env := build.Substitutions[recordsBucket], build.Substitutions[appSub], build.Substitutions[envSub]
	if bucket == "" || app == "" || env == "" {
		return sources, nil
	}
	store, err := open(ctx)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	if pr := build.Substitutions[prNumberSub]; pr != "" {
		last, err := newestRecordWhere(ctx, store, bucket, app+"/"+env+"/pr"+pr+"-", func(*Record) bool {
			return true
		})
		if err != nil {
			return nil, err
		}
		if last != nil {
			add(last.Commit)
		}
	}
	live, err := newestLiveRelease(ctx, store, bucket, app, env)
	if err != nil {
		return nil, err
	}
	if live != nil {
		add(live.Commit)
	}

	return sources, nil
}

// hooksInImage is where the image carries the hooks program, and dockerProgram the program
// the image build drives.
const (
	hooksInImage  = "/hooks"
	dockerProgram = "docker"
	// dockerCreate makes a container from an image without starting it, to copy a file out.
	dockerCreate = "create"
)

// takeHooks copies the hooks program out of the image to dst, when dst is set: a
// container is created from the image (pulled when this worker lacks it, with the step's
// registry credentials), the program copied out, the container removed. It never runs.
func takeHooks(ctx context.Context, clients *Clients, image, dst string, out io.Writer) error {
	if dst == "" {
		return nil
	}
	created, err := clients.Exec.Output(ctx, Command{Name: dockerProgram, Args: []string{dockerCreate, image}}, out)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(string(created))
	copyErr := clients.Exec.Run(ctx, Command{Name: dockerProgram, Args: []string{"cp", id + ":" + hooksInImage, dst}}, out)
	if err := clients.Exec.Run(ctx, Command{Name: dockerProgram, Args: []string{"rm", id}}, io.Discard); err != nil && copyErr == nil {
		return err
	}
	if copyErr != nil {
		return errors.Newf("the image carries no hooks program at %s (the Dockerfile builds it: go build -o /build/hooks ./cmd/deployment/hooks): %v", hooksInImage, copyErr)
	}
	fmt.Fprintf(out, "The hooks program is taken out of the image to %s.\n", dst)

	return nil
}

// buildSecrets reads each declared build secret into a file of its own under dir (0600)
// and answers docker's --secret arguments, with what removes the files.
func buildSecrets(ctx context.Context, clients *Clients, declared, dir string, out io.Writer) (args []string, cleanup func(), err error) {
	var files []string
	cleanup = func() {
		for _, f := range files {
			_ = os.Remove(f)
		}
	}
	if declared == "" {
		return nil, cleanup, nil
	}
	secrets, err := clients.Secrets(ctx)
	if err != nil {
		return nil, cleanup, err
	}
	defer secrets.Close()
	for _, entry := range strings.Split(declared, ",") {
		name, version, ok := strings.Cut(entry, "=")
		if !ok || secret.ValidateBuildSecret(name) != nil {
			return nil, cleanup, errors.Newf("%s entry %q is not NAME=<secret version>", buildSecretsSub, entry)
		}
		payload, err := secrets.Access(ctx, version)
		if err != nil {
			return nil, cleanup, errors.Newf("%sthe build secret %s (%s) could not be read as the deploy identity; the stack grants it accessor on the container, and the version must exist and be enabled: %v", rejected, name, version, errors.Cause(err))
		}
		file := filepath.Join(dir, name)
		if err := os.WriteFile(file, payload, 0o600); err != nil {
			return nil, cleanup, errors.Wrapf(err, "os.WriteFile(): build secret %s", name)
		}
		files = append(files, file)
		args = append(args, "--secret", "id="+name+",src="+file)
		fmt.Fprintf(out, "Build secret %s: %s (%d bytes)\n", name, version, len(payload))
	}

	return args, cleanup, nil
}

// imageDigest is the digest docker buildx recorded for the pushed image.
func imageDigest(metadata string) (string, error) {
	data, err := os.ReadFile(metadata)
	if err != nil {
		return "", errors.Wrap(err, "os.ReadFile()")
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return "", errors.Wrapf(err, "json.Unmarshal(): %s", MetadataFile)
	}
	digest, _ := m["containerimage.digest"].(string)
	if digest == "" {
		return "", errors.Newf("no image digest (containerimage.digest) in %s", MetadataFile)
	}

	return digest, nil
}

// BuildArgs reads the build arguments file: one NAME=value per line, the declared
// substitutions resolve wrote and what a hook before the build appended. Blank lines and
// lines starting with # are skipped; the value runs to the end of the line.
func (w Workspace) BuildArgs() ([]string, error) {
	f, err := os.Open(filepath.Join(string(w), BuildArgsFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "os.Open()")
	}
	defer f.Close()
	var args []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if name, _, ok := strings.Cut(line, "="); !ok || name == "" {
			return nil, errors.Newf("%s: %q is not NAME=value", BuildArgsFile, line)
		}
		args = append(args, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.Wrap(err, "bufio.Scanner.Scan()")
	}

	return args, nil
}
