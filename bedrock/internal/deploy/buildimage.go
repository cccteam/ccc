// buildimage.go is the image build.

package deploy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/check"
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
)

// The dependency caches. The seeded Dockerfile's two reserved stages, go-modules and
// web-packages, are built first, each exporting its layers to the registry under
// cache-<commit>-go and cache-<commit>-web; the full build then reads those caches and
// exports nothing. Only the downloads, whose content go.sum and bun.lock pin and the
// package managers verify, are ever served from a cache: a compiled or bundled layer is
// built fresh in every environment, and no per-build file of the pipeline leaves the
// worker. A tag is written once, by the first build of the commit that finds it absent,
// and never updated: the registry's tags are immutable.
const (
	cacheTagPrefix = "cache-"
	goCacheSuffix  = "-go"
	webCacheSuffix = "-web"
	// cacheOnlyOutput keeps a dependency stage's result in the builder alone: its layers
	// reach the registry through --cache-to, never as an image.
	cacheOnlyOutput = "type=cacheonly"
	// packageManifest is the file whose trustedDependencies entry names the packages
	// whose install scripts bun runs.
	packageManifest = "package.json"
)

// cacheStage is one reserved stage: its name, its cache tag's suffix and what it
// downloads.
type cacheStage struct {
	name, suffix, what string
}

// cacheStages are the two, in the order they are built.
var cacheStages = []cacheStage{
	{name: check.GoModulesStage, suffix: goCacheSuffix, what: "the Go module download"},
	{name: check.WebPackagesStage, suffix: webCacheSuffix, what: "the browser package install"},
}

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
		fmt.Fprintln(out, skipped(env))

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
	args := []string{dockerBuildx, dockerBuild, "--build-arg", "VERSION=" + env[versionFact], "--build-arg", "COMMIT=" + build.Substitutions[commitSub]}
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
	plan, err := planCache(ctx, clients, w, build, env[imageFact], out)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, plan.line())
	args = append(args, plan.fullArgs()...)
	args = append(args,
		"--tag", env[imageFact]+":"+env[commitTagFact],
		"--tag", env[imageFact]+":"+env[imageTagFact],
		"--metadata-file", metadata,
		// The build pushes a plain image, the manifest the metadata file's digest names
		// and Cloud Run deploys; with attestations on, the container driver would push an
		// image index with an attestation manifest beside it.
		"--provenance=false", "--sbom=false",
		"--file", "Dockerfile", "--push", ".")
	if err := runBuilds(ctx, clients, w, plan, args, out); err != nil {
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

// runBuilds creates the builder and runs the builds in it: each reserved stage the
// Dockerfile has, exporting its cache, then the full build with the arguments composed.
// Exporting a cache to the registry takes buildx's docker-container driver: the docker
// driver a Cloud Build step starts with builds and pushes but exports no cache ("Cache
// export is not supported for the docker driver"). A builder of that driver is created
// for this build and used; its BuildKit runs in a container beside the step's daemon,
// pulling and pushing with the step's registry credentials. The full build finds the
// stages the builds before it made in that same builder.
func runBuilds(ctx context.Context, clients *Clients, w Workspace, plan *cachePlan, full []string, out io.Writer) error {
	if err := clients.Exec.Run(ctx, Command{Dir: string(w), Name: dockerProgram, Args: []string{dockerBuildx, "create", "--driver", "docker-container", "--use"}}, out); err != nil {
		return err
	}
	for _, s := range plan.stages {
		if err := clients.Exec.Run(ctx, Command{Dir: string(w), Name: dockerProgram, Args: plan.stageArgs(s)}, out); err != nil {
			return err
		}
	}

	return clients.Exec.Run(ctx, Command{Dir: string(w), Name: dockerProgram, Args: full}, out)
}

// cacheSource is one commit whose caches the build may read, and what it is to the
// build.
type cacheSource struct {
	Commit string
	// Why says what the commit is: this commit; the live release, with its version and
	// build; the pull request's last build. A commit that is two of those says both.
	Why []string
}

// String is the source as the tests and the log name it.
func (s cacheSource) String() string {
	return s.Commit + " (" + strings.Join(s.Why, ", ") + ")"
}

// cachePlan is the build's layer cache: the reserved stages the Dockerfile has, the
// commits whose caches may be read, which of their tags the registry holds, and the
// stage built with no cache at all.
type cachePlan struct {
	image   string
	commit  string
	sources []cacheSource
	// stages are the reserved stages the Dockerfile has, in build order.
	stages []cacheStage
	// held says, by tag, whether the registry holds it.
	held map[string]bool
	// uncached is the package.json listing trustedDependencies, when one does, which
	// leaves web-packages out of the cache in both directions: the install scripts of
	// those packages run at install and may fetch or vary, so a stored install need not
	// equal a fresh one.
	uncached string
}

// planCache reads the Dockerfile for its reserved stages and each workspace's manifest
// for trusted install scripts, lists the commits whose caches the build may read, and
// asks the registry which of the candidate tags exist (one request per tag, as the
// release check asks after the image's tags), so that the build names only tags that
// exist and writes only tags that are absent: docker prints an ERROR line for an import
// it cannot find and for an export the registry's immutable tags refuse, and neither
// belongs in a build log.
func planCache(ctx context.Context, clients *Clients, w Workspace, build *Build, image string, out io.Writer) (*cachePlan, error) {
	src, err := os.ReadFile(filepath.Join(string(w), dockerfileName))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile(): Dockerfile")
	}
	stages := check.Stages(src)
	plan := &cachePlan{image: image, commit: build.Substitutions[commitSub], held: map[string]bool{}}
	for _, s := range cacheStages {
		stage := check.Named(stages, s.name)
		if stage == nil {
			fmt.Fprintf(out, "The Dockerfile has no %s stage, so %s is not cached; the seeded Dockerfile carries the stage.\n", s.name, s.what)

			continue
		}
		plan.stages = append(plan.stages, s)
		if s.name != check.WebPackagesStage {
			continue
		}
		manifest, err := trustedScripts(string(w), stage)
		if err != nil {
			return nil, err
		}
		if manifest != "" {
			plan.uncached = manifest
			fmt.Fprintf(out, "%s is built with no cache in or out: %s lists trustedDependencies, whose install scripts run at install and may fetch or vary, so a stored install need not equal a fresh one.\n", s.name, manifest)
		}
	}
	plan.sources, err = cacheSources(ctx, clients.Storage, build)
	if err != nil {
		return nil, err
	}
	registry, err := clients.Registry(ctx)
	if err != nil {
		return nil, err
	}
	commits := []string{plan.commit}
	for _, s := range plan.sources {
		if !slices.Contains(commits, s.Commit) {
			commits = append(commits, s.Commit)
		}
	}
	for _, s := range plan.stages {
		if !plan.cacheable(s) {
			continue
		}
		for _, commit := range commits {
			tag := plan.tag(commit, s)
			digest, err := registry.Digest(ctx, image, tag)
			if err != nil {
				return nil, err
			}
			plan.held[tag] = digest != ""
		}
	}

	return plan, nil
}

// trustedScripts is the package.json the web-packages stage copies that lists
// trustedDependencies, or empty when none does. The manifests are read inside the
// checkout alone (an os.Root), since the Dockerfile names them; one the checkout lacks is
// left to the build, whose copy of it fails naming the file.
func trustedScripts(root string, stage *check.Stage) (string, error) {
	checkout, err := os.OpenRoot(root)
	if err != nil {
		return "", errors.Wrap(err, "os.OpenRoot()")
	}
	defer checkout.Close()
	for _, in := range stage.Instructions {
		from, sources := in.Copies()
		if from != "" {
			continue
		}
		for _, source := range sources {
			if path.Base(source) != packageManifest {
				continue
			}
			data, err := checkout.ReadFile(filepath.FromSlash(source))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return "", errors.Wrapf(err, "os.Root.ReadFile(): %s", source)
			}
			var manifest struct {
				TrustedDependencies []string `json:"trustedDependencies"`
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				return "", errors.Wrapf(err, "json.Unmarshal(): %s", source)
			}
			if len(manifest.TrustedDependencies) > 0 {
				return source, nil
			}
		}
	}

	return "", nil
}

// tag is the cache tag of a commit's stage.
func (p *cachePlan) tag(commit string, s cacheStage) string {
	return cacheTagPrefix + commit + s.suffix
}

// cacheable reports whether the stage reads and writes a cache at all.
func (p *cachePlan) cacheable(s cacheStage) bool {
	return s.name != check.WebPackagesStage || p.uncached == ""
}

// reads lists the stage's tags the registry holds among the sources, in source order.
func (p *cachePlan) reads(s cacheStage) []string {
	var tags []string
	for _, source := range p.sources {
		if tag := p.tag(source.Commit, s); p.held[tag] && !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}

	return tags
}

// writes reports whether the stage's cache for this commit is exported: when the stage
// is cached and the registry lacks the tag.
func (p *cachePlan) writes(s cacheStage) bool {
	return p.cacheable(s) && !p.held[p.tag(p.commit, s)]
}

// stageArgs are the docker arguments of one reserved stage's build: the stage as the
// target, the caches it reads, its own cache exported when absent, every layer of the
// stage (mode=max), and no image.
func (p *cachePlan) stageArgs(s cacheStage) []string {
	args := []string{dockerBuildx, dockerBuild, "--target", s.name}
	if !p.cacheable(s) {
		args = append(args, "--no-cache")
	}
	for _, tag := range p.reads(s) {
		args = append(args, "--cache-from", "type=registry,ref="+p.image+":"+tag)
	}
	if p.writes(s) {
		args = append(args, "--cache-to", "type=registry,ref="+p.image+":"+p.tag(p.commit, s)+",mode=max")
	}

	return append(args, "--output", cacheOnlyOutput, "--file", dockerfileName, ".")
}

// fullArgs are the full build's cache arguments: every tag the stages read, and no
// export.
func (p *cachePlan) fullArgs() []string {
	var args []string
	for _, s := range p.stages {
		for _, tag := range p.reads(s) {
			args = append(args, "--cache-from", "type=registry,ref="+p.image+":"+tag)
		}
	}

	return args
}

// line says what the cache did: the tags read, by the commit they belong to and what it
// is to the build; the sources the registry holds nothing for; and whether this commit's
// tags were written or were held already.
func (p *cachePlan) line() string {
	var groups, missing []string
	thisRead := false
	for _, source := range p.sources {
		var tags []string
		for _, s := range p.stages {
			if tag := p.tag(source.Commit, s); p.held[tag] && p.cacheable(s) {
				tags = append(tags, tag)
			}
		}
		switch {
		case len(tags) > 0:
			groups = append(groups, strings.Join(tags, ", ")+" ("+strings.Join(source.Why, ", ")+")")
			thisRead = thisRead || source.Commit == p.commit
		case source.Commit != p.commit:
			missing = append(missing, strings.Join(source.Why, ", ")+" ("+source.Commit+")")
		}
	}
	read := "read nothing"
	if len(groups) > 0 {
		read = "read " + strings.Join(groups, ", ")
	}
	if len(missing) > 0 {
		read += "; nothing held for " + strings.Join(missing, ", ")
	}
	var written, held []string
	for _, s := range p.stages {
		switch tag := p.tag(p.commit, s); {
		case !p.cacheable(s):
		case p.writes(s):
			written = append(written, tag)
		default:
			held = append(held, tag)
		}
	}
	var wrote string
	switch {
	case len(written) == 0 && len(held) == 0:
		wrote = "nothing to write"
	case len(written) == 0 && thisRead:
		wrote = "not written, the registry holds them"
	case len(written) == 0:
		wrote = "not written, the registry holds " + strings.Join(held, ", ")
	case len(held) == 0:
		wrote = "written " + strings.Join(written, ", ")
	default:
		wrote = "written " + strings.Join(written, ", ") + "; the registry holds " + strings.Join(held, ", ")
	}

	return "Layer cache: " + read + "; " + wrote + "."
}

// cacheSources lists the commits whose caches the build may read, in the order they are
// named. A tag build reads this commit's (another environment built this commit already)
// and the commit the environment runs live (the previous release's downloads, most of
// which an ordinary change keeps). A pull-request build reads its own pull request's
// last build's and the live release's. A tag build never reads a pull-request build's
// cache: a pull-request build runs a contributor's branch and hooks as a registry
// writer, and a release commit's tag cannot be written ahead of it, since a squash
// commit's hash is unknowable until it exists. The records are read through the records
// bucket; a build without one reads this commit's cache alone.
func cacheSources(ctx context.Context, open StoreFunc, build *Build) ([]cacheSource, error) {
	commit := build.Substitutions[commitSub]
	var sources []cacheSource
	add := func(c, why string) {
		if c == "" {
			return
		}
		for i := range sources {
			if sources[i].Commit == c {
				sources[i].Why = append(sources[i].Why, why)

				return
			}
		}
		sources = append(sources, cacheSource{Commit: c, Why: []string{why}})
	}
	bucket, app, env := build.Substitutions[recordsBucket], build.Substitutions[appSub], build.Substitutions[envSub]
	pr := build.Substitutions[prNumberSub]
	if bucket == "" || app == "" || env == "" || pr == "" {
		add(commit, "this commit")
	}
	if bucket == "" || app == "" || env == "" {
		return sources, nil
	}
	store, err := open(ctx)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	if pr != "" {
		last, err := newestRecordWhere(ctx, store, bucket, app+"/"+env+"/pr"+pr+"-", func(*Record) bool {
			return true
		})
		if err != nil {
			return nil, err
		}
		if last != nil {
			add(last.Commit, "the pull request's last build, build "+last.Build)
		}
	}
	live, err := newestLiveRelease(ctx, store, bucket, app, env)
	if err != nil {
		return nil, err
	}
	if live != nil {
		add(live.Commit, "the live release "+live.Version+", build "+live.Build)
	}

	return sources, nil
}

// hooksInImage is where the image carries the hooks program, and dockerProgram the program
// the image build drives.
const (
	hooksInImage  = "/hooks"
	dockerProgram = "docker"
	// dockerBuildx is docker's buildx plugin, which creates the builder and builds
	// (dockerBuild).
	dockerBuildx = "buildx"
	dockerBuild  = "build"
	// dockerCreate makes a container from an image without starting it, to copy a file out.
	dockerCreate = "create"
	// dockerfileName is the image build at the application root.
	dockerfileName = "Dockerfile"
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
