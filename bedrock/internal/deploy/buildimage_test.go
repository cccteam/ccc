package deploy

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
)

// seededDockerfile is the seeded Dockerfile's shape in short: the two reserved stages,
// the bun binary's stage, and a compile stage.
const seededDockerfile = `FROM go AS go-modules
WORKDIR /go/src/app
COPY go.mod go.sum ./
RUN go mod download
FROM bun AS bun-binary
FROM node AS web-packages
COPY --from=bun-binary /usr/local/bin/bun /usr/local/bin/bun
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
FROM go AS build-env
COPY --from=go-modules /root/go/pkg/mod /root/go/pkg/mod
COPY . ./
RUN go build -o /build/app .
`

// argBaseDockerfile is the seeded shape with go-modules built on a stage of its own that
// declares a build argument the stack carries: the reserved stage sees PROJECT_ID through
// its FROM, and web-packages sees nothing.
const argBaseDockerfile = `FROM go AS go-base
ARG PROJECT_ID
RUN echo "$PROJECT_ID" > /etc/project
FROM go-base AS go-modules
WORKDIR /go/src/app
COPY go.mod go.sum ./
RUN go mod download
FROM bun AS bun-binary
FROM node AS web-packages
COPY --from=bun-binary /usr/local/bin/bun /usr/local/bin/bun
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
FROM node AS web-build-env
ARG FIREBASE_API_KEY
COPY --from=web-packages /src/web/node_modules ./node_modules
RUN bun run build
`

// stackArgsEnv is the environment file's build arguments of the stack, as resolve exports a
// release build's from its trigger (or deploy pr-stack apply appends a pull request's).
const stackArgsEnv = "export _BUILD_ARG_FIREBASE_API_KEY='AIzaTstKey'\nexport _BUILD_ARG_PROJECT_ID='tst-project'\n"

// recordJSON is one deployment record as the records bucket holds it.
func recordJSON(version, commit, build, status, timestamp string) string {
	return `{"app":"harbor","env":"tst","version":"` + version + `","commit":"` + commit + `","status":"` + status + `","build":"` + build + `","timestamp":"` + timestamp + `"}`
}

func TestBuildImage(t *testing.T) {
	t.Parallel()

	const env = "export SKIP_DEPLOY=\"\"\nexport IMAGE=\"reg/quill\"\nexport IMAGE_TAG=\"v1.2.3-tst\"\nexport COMMIT_TAG=\"c9-tst\"\nexport VERSION=\"v1.2.3\"\n"
	const (
		goStage  = "docker buildx build --target go-modules"
		webStage = "docker buildx build --target web-packages"
		tail     = " --output type=cacheonly --file Dockerfile ."
		fromC9Go = " --cache-from type=registry,ref=reg/quill:cache-c9-go"
		fromC9W  = " --cache-from type=registry,ref=reg/quill:cache-c9-web"
		fromC8Go = " --cache-from type=registry,ref=reg/quill:cache-c8-go"
		fromC8W  = " --cache-from type=registry,ref=reg/quill:cache-c8-web"
		fromC7Go = " --cache-from type=registry,ref=reg/quill:cache-c7-go"
		fromC7W  = " --cache-from type=registry,ref=reg/quill:cache-c7-web"
		toC9Go   = " --cache-to type=registry,ref=reg/quill:cache-c9-go,mode=max"
		toC9W    = " --cache-to type=registry,ref=reg/quill:cache-c9-web,mode=max"
		// toC9GoTst is go-modules' export under the digest of tst's PROJECT_ID.
		toC9GoTst = " --cache-to type=registry,ref=reg/quill:cache-c9-go-5930b08fead0,mode=max"
	)
	// firstBuild is the stage builds of a commit's first build: both caches written, none read.
	firstBuild := []string{goStage + toC9Go + tail, webStage + toC9W + tail}
	tagged := map[string]string{"_RECORDS_BUCKET": "records", "_APP": "harbor", "_ENV": "tst"}
	pulled := map[string]string{"_RECORDS_BUCKET": "records", "_APP": "harbor", "_ENV": "tst", prNumberSub: "7"}
	live := map[string]string{"gs://records/harbor/tst/v1.2.2/b-0.json": recordJSON("v1.2.2", "c8", "b-0", Live, "2026-09-27T05:00:00Z")}
	tests := []struct {
		name string
		env  string
		// declared are the build secrets resolve left (BUILD_SECRETS); the trigger's
		// _BUILD_SECRETS names another, which the step never reads.
		declared  string
		buildArgs string
		secrets   map[string]string
		metadata  string
		// dockerfile replaces the seeded one; files are more files of the checkout.
		dockerfile string
		files      map[string]string
		// subs are the build's substitutions beyond the commit, records the bucket's
		// objects, held the tags the registry holds.
		subs    map[string]string
		records map[string]string
		held    []string
		// wantStages are the stage builds in order, as the log shows them (the first
		// build's when nil); wantArgs and wantNoArgs are what the full build's line
		// carries and lacks.
		wantStages []string
		wantArgs   []string
		wantNoArgs []string
		wantOut    []string
		// wantNotOut is what the log must not carry: a build argument's value among it.
		wantNotOut []string
		wantErr    string
		wantBuilt  bool
		// hooks takes the hooks program out of the image beside the migrate command, which
		// every build takes; wantTaken are the docker commands that do it; noMigrate leaves
		// the image without the migrate command.
		hooks     bool
		noMigrate bool
		wantTaken []string
	}{
		{name: "a torn-down environment builds nothing", env: "export SKIP_DEPLOY=\"true\"\n", wantOut: []string{tornDown}},
		{
			name: "a build to reuse is not rebuilt, and the migrate command is taken out of it", env: env + "export REUSE_IMAGE=\"true\"\nexport IMAGE_DIGEST=\"sha256:old\"\n",
			wantOut:   []string{"Reusing reg/quill@sha256:old", "The migrate command is taken out of the image to MIGRATE."},
			wantTaken: []string{"docker create reg/quill@sha256:old", "docker cp cid-1:/migrate MIGRATE", "docker rm cid-1"},
		},
		{
			name:      "the build takes its arguments and its secrets, writes the commit's first caches, and leaves the digest",
			env:       env,
			declared:  "NPM_TOKEN=projects/p/secrets/npm/versions/2",
			buildArgs: "_WIDGET_MODE=on\n# a hook's note\nFRONTEND_VERSION=4.1\n",
			secrets:   map[string]string{"projects/p/secrets/npm/versions/2": "s3cret"},
			metadata:  `{"containerimage.digest": "sha256:new"}`,
			wantArgs: []string{
				"--build-arg VERSION=v1.2.3", "--build-arg COMMIT=c9", "--build-arg _WIDGET_MODE=on", "--build-arg FRONTEND_VERSION=4.1",
				"--secret id=NPM_TOKEN,src=SECRETS/NPM_TOKEN", "--tag reg/quill:c9-tst", "--tag reg/quill:v1.2.3-tst", "--push .",
			},
			wantNoArgs: []string{"--cache-from", "--cache-to", "--no-cache", "--target"},
			wantOut:    []string{"Build secret NPM_TOKEN: projects/p/secrets/npm/versions/2 (6 bytes)", "Layer cache: read nothing; written cache-c9-go, cache-c9-web.", "Built and pushed reg/quill@sha256:new"},
			wantBuilt:  true,
			wantTaken:  []string{"docker create reg/quill@sha256:new", "docker cp cid-1:/migrate MIGRATE", "docker rm cid-1"},
		},
		{
			name:       "the stack's build arguments are passed and named, never their values, and a dependency stage that sees none is cached as ever",
			env:        env + stackArgsEnv,
			metadata:   `{"containerimage.digest": "sha256:new"}`,
			buildArgs:  "_WIDGET_MODE=on\n",
			wantArgs:   []string{"--build-arg COMMIT=c9 --build-arg FIREBASE_API_KEY=AIzaTstKey --build-arg PROJECT_ID=tst-project --build-arg _WIDGET_MODE=on"},
			wantOut:    []string{"Build arguments from the stack (placement.json, buildArguments): FIREBASE_API_KEY, PROJECT_ID.", "Layer cache: read nothing; written cache-c9-go, cache-c9-web."},
			wantNotOut: []string{"AIzaTstKey", "tst-project", " sees the build arguments"},
			wantBuilt:  true,
		},
		{
			name:       "a dependency stage built on a stage that declares a build argument is passed it and caches under a digest of its value",
			env:        env + stackArgsEnv,
			metadata:   `{"containerimage.digest": "sha256:new"}`,
			dockerfile: argBaseDockerfile,
			wantStages: []string{goStage + " --build-arg PROJECT_ID=tst-project" + toC9GoTst + tail, webStage + toC9W + tail},
			wantArgs:   []string{"--build-arg FIREBASE_API_KEY=AIzaTstKey --build-arg PROJECT_ID=tst-project"},
			wantOut: []string{
				"go-modules sees the build arguments PROJECT_ID (its FROM line, or a stage it is built on, reads them): its build is passed them, and its cache tags carry a digest of their values (5930b08fead0), so each environment's values read and write a cache of their own.",
				"Layer cache: read nothing; written cache-c9-go-5930b08fead0, cache-c9-web.",
			},
			wantNotOut: []string{"AIzaTstKey", "web-packages sees"},
			wantBuilt:  true,
		},
		{
			name:       "another environment's values never serve the stage: its digest's tags are not read, and this environment's own are written",
			env:        env + stackArgsEnv,
			metadata:   `{"containerimage.digest": "sha256:new"}`,
			dockerfile: argBaseDockerfile,
			subs:       tagged,
			records:    live,
			held:       []string{"cache-c9-go-7e95161484f2", "cache-c8-go-7e95161484f2", "cache-c8-go-5930b08fead0", "cache-c9-web"},
			wantStages: []string{goStage + " --build-arg PROJECT_ID=tst-project --cache-from type=registry,ref=reg/quill:cache-c8-go-5930b08fead0" + toC9GoTst + tail, webStage + fromC9W + tail},
			wantArgs:   []string{"--cache-from type=registry,ref=reg/quill:cache-c8-go-5930b08fead0 --cache-from type=registry,ref=reg/quill:cache-c9-web --tag"},
			wantNoArgs: []string{"7e95161484f2", "--cache-to"},
			wantOut:    []string{"Layer cache: read cache-c9-web (this commit), cache-c8-go-5930b08fead0 (the live release v1.2.2, build b-0); written cache-c9-go-5930b08fead0; the registry holds cache-c9-web."},
			wantBuilt:  true,
		},
		{
			name:       "an application with a job process passes no job to the image: the framework names the job of its build from the stack's template and the version",
			env:        env + "export JOBS_JOB=\"us-central1=quill-jobs\"\n",
			metadata:   `{"containerimage.digest": "sha256:new"}`,
			wantArgs:   []string{"--build-arg VERSION=v1.2.3", "--build-arg COMMIT=c9", "--push ."},
			wantNoArgs: []string{"JOBS_JOB"},
			wantOut:    []string{"Built and pushed reg/quill@sha256:new"},
			wantBuilt:  true,
			wantTaken:  []string{"docker create reg/quill@sha256:new", "docker cp cid-1:/migrate MIGRATE", "docker rm cid-1"},
		},
		{
			name:       "a tag build after another environment's reads this commit's caches and the live release's, and writes nothing",
			env:        env,
			metadata:   `{"containerimage.digest": "sha256:new"}`,
			subs:       tagged,
			records:    live,
			held:       []string{"cache-c9-go", "cache-c9-web", "cache-c8-go", "cache-c8-web"},
			wantStages: []string{goStage + fromC9Go + fromC8Go + tail, webStage + fromC9W + fromC8W + tail},
			wantArgs:   []string{fromC9Go + fromC8Go + fromC9W + fromC8W + " --tag"},
			wantNoArgs: []string{"--cache-to"},
			wantOut:    []string{"Layer cache: read cache-c9-go, cache-c9-web (this commit), cache-c8-go, cache-c8-web (the live release v1.2.2, build b-0); not written, the registry holds them."},
			wantBuilt:  true,
		},
		{
			name:       "a candidate tag the registry lacks is not named",
			env:        env,
			metadata:   `{"containerimage.digest": "sha256:new"}`,
			subs:       tagged,
			records:    live,
			held:       []string{"cache-c8-go"},
			wantStages: []string{goStage + fromC8Go + toC9Go + tail, webStage + toC9W + tail},
			wantArgs:   []string{fromC8Go + " --tag"},
			wantNoArgs: []string{"cache-c8-web", "--cache-to"},
			wantOut:    []string{"Layer cache: read cache-c8-go (the live release v1.2.2, build b-0); written cache-c9-go, cache-c9-web."},
			wantBuilt:  true,
		},
		{
			name:       "a live release with nothing held is named",
			env:        env,
			metadata:   `{"containerimage.digest": "sha256:new"}`,
			subs:       tagged,
			records:    live,
			wantOut:    []string{"Layer cache: read nothing; nothing held for the live release v1.2.2, build b-0 (c8); written cache-c9-go, cache-c9-web."},
			wantNoArgs: []string{"--cache-from"},
			wantBuilt:  true,
		},
		{
			name:       "one of this commit's tags held, the other is written",
			env:        env,
			metadata:   `{"containerimage.digest": "sha256:new"}`,
			held:       []string{"cache-c9-go"},
			wantStages: []string{goStage + fromC9Go + tail, webStage + toC9W + tail},
			wantArgs:   []string{fromC9Go + " --tag"},
			wantOut:    []string{"Layer cache: read cache-c9-go (this commit); written cache-c9-web; the registry holds cache-c9-go."},
			wantBuilt:  true,
		},
		{
			name:       "a pull-request build reads its last build's caches and the live release's, never this commit's by name",
			env:        env,
			metadata:   `{"containerimage.digest": "sha256:new"}`,
			subs:       pulled,
			records:    map[string]string{"gs://records/harbor/tst/pr7-c7/b-3.json": recordJSON("pr7@c7", "c7", "b-3", Live, "2026-09-28T05:00:00Z"), "gs://records/harbor/tst/v1.2.2/b-0.json": live["gs://records/harbor/tst/v1.2.2/b-0.json"]},
			held:       []string{"cache-c7-go", "cache-c7-web", "cache-c8-go", "cache-c8-web"},
			wantStages: []string{goStage + fromC7Go + fromC8Go + toC9Go + tail, webStage + fromC7W + fromC8W + toC9W + tail},
			wantArgs:   []string{fromC7Go + fromC8Go + fromC7W + fromC8W + " --tag"},
			wantOut:    []string{"Layer cache: read cache-c7-go, cache-c7-web (the pull request's last build, build b-3), cache-c8-go, cache-c8-web (the live release v1.2.2, build b-0); written cache-c9-go, cache-c9-web."},
			wantBuilt:  true,
		},
		{
			name:       "a pull request's first build reads the live release's alone, and a rebuilt commit's held tags are not written",
			env:        env,
			metadata:   `{"containerimage.digest": "sha256:new"}`,
			subs:       pulled,
			records:    live,
			held:       []string{"cache-c8-go", "cache-c8-web", "cache-c9-go", "cache-c9-web"},
			wantStages: []string{goStage + fromC8Go + tail, webStage + fromC8W + tail},
			wantNoArgs: []string{"--cache-to", "cache-c9"},
			wantOut:    []string{"Layer cache: read cache-c8-go, cache-c8-web (the live release v1.2.2, build b-0); not written, the registry holds cache-c9-go, cache-c9-web."},
			wantBuilt:  true,
		},
		{
			name:       "trusted install scripts leave web-packages out of the cache in both directions",
			env:        env,
			metadata:   `{"containerimage.digest": "sha256:new"}`,
			files:      map[string]string{"web/package.json": `{"name": "web", "trustedDependencies": ["esbuild"]}`},
			held:       []string{"cache-c9-web"},
			wantStages: []string{goStage + toC9Go + tail, webStage + " --no-cache" + tail},
			wantNoArgs: []string{"--cache-from", "--no-cache"},
			wantOut:    []string{"web-packages is built with no cache in or out: web/package.json lists trustedDependencies, whose install scripts run at install and may fetch or vary, so a stored install need not equal a fresh one.", "Layer cache: read nothing; written cache-c9-go."},
			wantBuilt:  true,
		},
		{
			name:       "a Dockerfile without the reserved stages is built once, with no cache",
			env:        env,
			metadata:   `{"containerimage.digest": "sha256:new"}`,
			dockerfile: "FROM go AS build-env\nCOPY . ./\nRUN go build -o /build/app .\n",
			held:       []string{"cache-c9-go"},
			wantStages: []string{},
			wantNoArgs: []string{"--cache-from", "--cache-to"},
			wantOut:    []string{"The Dockerfile has no go-modules stage, so the Go module download is not cached; the seeded Dockerfile carries the stage.", "The Dockerfile has no web-packages stage, so the browser package install is not cached", "Layer cache: read nothing; nothing to write."},
			wantBuilt:  true,
		},
		{name: "a build secret the deploy identity cannot read is refused", env: env, declared: "NPM_TOKEN=projects/p/secrets/npm/versions/9", wantErr: "Build REJECTED: the build secret NPM_TOKEN (projects/p/secrets/npm/versions/9) could not be read"},
		{name: "a push without a digest is refused", env: env, metadata: `{}`, wantErr: "no image digest", wantBuilt: true},
		{
			name: "the hooks program is taken out of the built image beside the migrate command", env: env, metadata: `{"containerimage.digest": "sha256:new"}`, hooks: true, wantBuilt: true,
			wantOut:   []string{"The migrate command is taken out of the image to MIGRATE.", "The hooks program is taken out of the image to HOOKS."},
			wantTaken: []string{"docker create reg/quill@sha256:new", "docker cp cid-1:/migrate MIGRATE", "docker cp cid-1:/hooks HOOKS", "docker rm cid-1"},
		},
		{
			name: "and out of a reused one", env: env + "export REUSE_IMAGE=\"true\"\nexport IMAGE_DIGEST=\"sha256:old\"\n", hooks: true,
			wantTaken: []string{"docker create reg/quill@sha256:old", "docker cp cid-1:/migrate MIGRATE", "docker cp cid-1:/hooks HOOKS", "docker rm cid-1"},
		},
		{
			name: "an image without the migrate command is refused, the container removed", env: env, metadata: `{"containerimage.digest": "sha256:new"}`, noMigrate: true, wantBuilt: true,
			wantErr:   "the image carries no migrate command at /migrate (the Dockerfile builds it: go build -o /build/migrate ./cmd/deployment/migrate)",
			wantTaken: []string{"docker create reg/quill@sha256:new", "docker cp cid-1:/migrate MIGRATE", "docker rm cid-1"},
		},
	}
	// triggerBuildList is the trigger's list of build secrets, as its stack's last apply
	// set it: the step reads the facts' list (BUILD_SECRETS) and never this one.
	const triggerBuildList = "NPM_OLD=projects/p/secrets/old/versions/1"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subs := map[string]string{commitSub: "c9", buildSecretsSub: triggerBuildList, projectSub: "p"}
			maps.Copy(subs, tt.subs)
			dockerfile := tt.dockerfile
			if dockerfile == "" {
				dockerfile = seededDockerfile
			}
			environment := tt.env
			if tt.declared != "" {
				environment += "export " + buildSecretsFact + "=" + doubleQuote(tt.declared) + "\n"
			}
			files := map[string]string{EnvironmentFile: environment, BuildFile: buildFor(t, subs), BuildArgsFile: tt.buildArgs, dockerfileName: dockerfile}
			maps.Copy(files, tt.files)
			w := workspaceFiles(t, files)
			secretDir := t.TempDir()
			var secretSeen string
			home := t.TempDir()
			migrate, hooks := filepath.Join(home, "migrate"), ""
			if tt.hooks {
				hooks = filepath.Join(home, "hooks")
			}
			run := &fakeRunner{outputs: map[string]string{"docker create": "cid-1\n"}, effect: func(c Command) error {
				if tt.noMigrate && c.Args[0] == "cp" && strings.HasSuffix(c.Args[1], ":/migrate") {
					return errors.New("docker failed: exit status 1")
				}
				if c.Args[0] != "buildx" || c.Args[1] != "build" || slices.Contains(c.Args, "--target") {
					return nil
				}
				for _, arg := range c.Args {
					if src, ok := strings.CutPrefix(arg, "id=NPM_TOKEN,src="); ok {
						data, _ := os.ReadFile(src)
						secretSeen = string(data)
					}
				}

				return os.WriteFile(filepath.Join(string(w), MetadataFile), []byte(tt.metadata), 0o600)
			}}
			registry := &fakeRegistry{digests: map[string]string{}}
			for _, tag := range tt.held {
				registry.digests["reg/quill:"+tag] = "sha256:" + tag
			}
			store := &memoryStore{objects: tt.records}
			clients := &Clients{Exec: run, Secrets: (&fakeSecrets{payloads: tt.secrets}).open, Registry: registry.open, Storage: store.open}
			var out strings.Builder
			err := BuildImage(t.Context(), clients, w, secretDir, migrate, hooks, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("BuildImage() error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("BuildImage() error = %v\n%s", err, out.String())
			}
			var built, builder bool
			var taken, stages []string
			buildLine := ""
			for _, line := range run.lines() {
				switch {
				case strings.HasPrefix(line, "docker buildx create --driver docker-container --use"):
					builder = true

					continue
				case strings.HasPrefix(line, "docker buildx build --target "):
					stages = append(stages, line)

					continue
				case strings.HasPrefix(line, "docker buildx build "):
					built = true
					buildLine = line

					continue
				}
				taken = append(taken, placeholders(line, migrate, hooks))
			}
			containsAll(t, placeholders(out.String(), migrate, hooks), tt.wantOut...)
			for _, unwanted := range tt.wantNotOut {
				if strings.Contains(out.String(), unwanted) {
					t.Errorf("output carries %q:\n%s", unwanted, out.String())
				}
			}
			if built != tt.wantBuilt || builder != tt.wantBuilt {
				t.Fatalf("ran %v, want a build %t with a docker-container builder created before it", run.lines(), tt.wantBuilt)
			}
			// Every built image has its migrate command taken out; a case that says nothing
			// else about the take-out wants that alone.
			wantTaken := tt.wantTaken
			if wantTaken == nil && tt.wantBuilt && tt.wantErr == "" {
				wantTaken = []string{"docker create reg/quill@sha256:new", "docker cp cid-1:/migrate MIGRATE", "docker rm cid-1"}
			}
			if strings.Join(taken, "|") != strings.Join(wantTaken, "|") {
				t.Errorf("took the programs out with %q, want %q", taken, wantTaken)
			}
			if !tt.wantBuilt {
				return
			}
			wantStages := tt.wantStages
			if wantStages == nil {
				wantStages = firstBuild
			}
			if !slices.Equal(stages, wantStages) {
				t.Errorf("stage builds:\n%s\nwant:\n%s", strings.Join(stages, "\n"), strings.Join(wantStages, "\n"))
			}
			for _, c := range run.ran {
				if slices.Contains(c.Args, dockerBuildx) && c.Dir != string(w) {
					t.Errorf("ran %v in %s, want the builder and the builds in the workspace", c.Args, c.Dir)
				}
			}
			line := strings.ReplaceAll(buildLine, secretDir, "SECRETS")
			for _, want := range append(tt.wantArgs, "--provenance=false", "--sbom=false", "--push") {
				if !strings.Contains(line, want) {
					t.Errorf("docker %s lacks %q", line, want)
				}
			}
			for _, unwanted := range tt.wantNoArgs {
				if strings.Contains(line, unwanted) {
					t.Errorf("docker %s carries %q", line, unwanted)
				}
			}
			if tt.wantErr != "" || tt.secrets == nil {
				return
			}
			if secretSeen != "s3cret" {
				t.Errorf("the build saw the secret as %q", secretSeen)
			}
			if left, _ := os.ReadDir(secretDir); len(left) != 0 {
				t.Errorf("secrets left after the build: %v", left)
			}
			env, _ := w.Environment()
			if env[digestFact] != "sha256:new" {
				t.Errorf("IMAGE_DIGEST = %q", env[digestFact])
			}
		})
	}
}

// placeholders puts the names MIGRATE and HOOKS where the text names the programs' paths.
func placeholders(text, migrate, hooks string) string {
	text = strings.ReplaceAll(text, migrate, "MIGRATE")
	if hooks != "" {
		text = strings.ReplaceAll(text, hooks, "HOOKS")
	}

	return text
}

func TestCacheSources(t *testing.T) {
	t.Parallel()

	tagged := map[string]string{commitSub: "c9", "_RECORDS_BUCKET": "records", "_APP": "harbor", "_ENV": "tst"}
	pulled := map[string]string{commitSub: "c9", "_RECORDS_BUCKET": "records", "_APP": "harbor", "_ENV": "tst", prNumberSub: "7"}
	tests := []struct {
		name    string
		subs    map[string]string
		records map[string]string
		want    []string
	}{
		{name: "a build without a records bucket reads its own commit's cache alone", subs: map[string]string{commitSub: "c9"}, want: []string{"c9 (this commit)"}},
		{name: "a tag build in an environment without a record reads its own commit's cache alone", subs: tagged, want: []string{"c9 (this commit)"}},
		{
			name:    "a tag build reads its own commit's cache, then the live release's",
			subs:    tagged,
			records: map[string]string{"gs://records/harbor/tst/v1.2.2/b-0.json": recordJSON("v1.2.2", "c8", "b-0", Live, "2026-09-27T05:00:00Z")},
			want:    []string{"c9 (this commit)", "c8 (the live release v1.2.2, build b-0)"},
		},
		{
			name: "a pull-request build reads its last build's cache before the live release's, and never names its own commit",
			subs: pulled,
			records: map[string]string{
				"gs://records/harbor/tst/pr7-c7/b-3.json": recordJSON("pr7@c7", "c7", "b-3", Live, "2026-09-28T05:00:00Z"),
				"gs://records/harbor/tst/v1.2.2/b-0.json": recordJSON("v1.2.2", "c8", "b-0", Live, "2026-09-27T05:00:00Z"),
				"gs://records/harbor/tst/pr8-c6/b-4.json": recordJSON("pr8@c6", "c6", "b-4", Live, "2026-09-29T05:00:00Z"),
				"gs://records/harbor/tst/v1.2.1/b-9.json": recordJSON("v1.2.1", "c5", "b-9", Preview, "2026-09-29T06:00:00Z"),
			},
			want: []string{"c7 (the pull request's last build, build b-3)", "c8 (the live release v1.2.2, build b-0)"},
		},
		{
			name:    "a pull request's first build reads the live release's alone",
			subs:    pulled,
			records: map[string]string{"gs://records/harbor/tst/v1.2.2/b-0.json": recordJSON("v1.2.2", "c8", "b-0", Live, "2026-09-27T05:00:00Z")},
			want:    []string{"c8 (the live release v1.2.2, build b-0)"},
		},
		{
			name:    "another pull request's live record is not the environment's release",
			subs:    tagged,
			records: map[string]string{"gs://records/harbor/tst/pr8-c6/b-4.json": recordJSON("pr8@c6", "c6", "b-4", Live, "2026-09-29T05:00:00Z")},
			want:    []string{"c9 (this commit)"},
		},
		{
			name:    "a commit that is this commit and the live release is listed once, with both reasons",
			subs:    tagged,
			records: map[string]string{"gs://records/harbor/tst/v1.2.3/b-1.json": recordJSON("v1.2.3", "c9", "b-1", Live, "2026-09-27T05:00:00Z")},
			want:    []string{"c9 (this commit, the live release v1.2.3, build b-1)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &memoryStore{objects: tt.records}
			sources, err := cacheSources(t.Context(), store.open, &Build{ID: "b-1", Substitutions: tt.subs})
			if err != nil {
				t.Fatalf("cacheSources() error = %v", err)
			}
			got := make([]string, 0, len(sources))
			for _, s := range sources {
				got = append(got, s.String())
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("cacheSources() = %v, want %v", got, tt.want)
			}
		})
	}
}
