package deploy

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuildImage(t *testing.T) {
	t.Parallel()

	const env = "export SKIP_DEPLOY=\"\"\nexport IMAGE=\"reg/quill\"\nexport IMAGE_TAG=\"v1.2.3-tst\"\nexport COMMIT_TAG=\"c9-tst\"\nexport VERSION=\"v1.2.3\"\n"
	tests := []struct {
		name      string
		env       string
		declared  string
		buildArgs string
		secrets   map[string]string
		metadata  string
		wantArgs  []string
		wantOut   []string
		wantErr   string
		wantBuilt bool
		// hooks takes the hooks program out of the image; wantHooks are the docker commands
		// that do it.
		hooks     bool
		wantHooks []string
	}{
		{name: "a torn-down environment builds nothing", env: "export SKIP_DEPLOY=\"true\"\n", wantOut: []string{tornDown}},
		{name: "a build to reuse is not rebuilt", env: env + "export REUSE_IMAGE=\"true\"\nexport IMAGE_DIGEST=\"sha256:old\"\n", wantOut: []string{"Reusing reg/quill@sha256:old"}},
		{
			name:      "the build takes its arguments and its secrets, and leaves the digest",
			env:       env,
			declared:  "NPM_TOKEN=projects/p/secrets/npm/versions/2",
			buildArgs: "_WIDGET_MODE=on\n# a hook's note\nFRONTEND_VERSION=4.1\n",
			secrets:   map[string]string{"projects/p/secrets/npm/versions/2": "s3cret"},
			metadata:  `{"containerimage.digest": "sha256:new"}`,
			wantArgs: []string{
				"--build-arg VERSION=v1.2.3", "--build-arg COMMIT=c9", "--build-arg _WIDGET_MODE=on", "--build-arg FRONTEND_VERSION=4.1",
				"--secret id=NPM_TOKEN,src=SECRETS/NPM_TOKEN", "--tag reg/quill:c9-tst", "--tag reg/quill:v1.2.3-tst", "--push .",
			},
			wantOut:   []string{"Build secret NPM_TOKEN: projects/p/secrets/npm/versions/2 (6 bytes)", "Built and pushed reg/quill@sha256:new"},
			wantBuilt: true,
		},
		{
			name:      "an application with a job process bakes its build's job into the image",
			env:       env + "export JOBS_JOB=\"us-central1=quill-jobs\"\n",
			metadata:  `{"containerimage.digest": "sha256:new"}`,
			wantArgs:  []string{"--build-arg VERSION=v1.2.3", "--build-arg COMMIT=c9", "--build-arg JOBS_JOB=projects/p/locations/us-central1/jobs/quill-jobs-v1-2-3", "--push ."},
			wantOut:   []string{"Built and pushed reg/quill@sha256:new"},
			wantBuilt: true,
		},
		{name: "a build secret the deploy identity cannot read is refused", env: env, declared: "NPM_TOKEN=projects/p/secrets/npm/versions/9", wantErr: "Build REJECTED: the build secret NPM_TOKEN (projects/p/secrets/npm/versions/9) could not be read"},
		{name: "a push without a digest is refused", env: env, metadata: `{}`, wantErr: "no image digest", wantBuilt: true},
		{
			name: "the hooks program is taken out of the built image", env: env, metadata: `{"containerimage.digest": "sha256:new"}`, hooks: true, wantBuilt: true,
			wantOut:   []string{"The hooks program is taken out of the image to HOOKS."},
			wantHooks: []string{"docker create reg/quill@sha256:new", "docker cp cid-1:/hooks HOOKS", "docker rm cid-1"},
		},
		{
			name: "and out of a reused one", env: env + "export REUSE_IMAGE=\"true\"\nexport IMAGE_DIGEST=\"sha256:old\"\n", hooks: true,
			wantHooks: []string{"docker create reg/quill@sha256:old", "docker cp cid-1:/hooks HOOKS", "docker rm cid-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: buildFor(t, map[string]string{commitSub: "c9", buildSecretsSub: tt.declared, projectSub: "p"}), BuildArgsFile: tt.buildArgs})
			secretDir := t.TempDir()
			var secretSeen string
			hooks := ""
			if tt.hooks {
				hooks = filepath.Join(t.TempDir(), "hooks")
			}
			run := &fakeRunner{outputs: map[string]string{"docker create": "cid-1\n"}, effect: func(c Command) error {
				if c.Args[0] != "buildx" {
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
			var out strings.Builder
			err := BuildImage(t.Context(), &Clients{Exec: run, Secrets: (&fakeSecrets{payloads: tt.secrets}).open}, w, secretDir, hooks, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("BuildImage() error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("BuildImage() error = %v\n%s", err, out.String())
			}
			var built bool
			var taken []string
			for _, line := range run.lines() {
				if strings.HasPrefix(line, "docker buildx") {
					built = true

					continue
				}
				if hooks != "" {
					line = strings.ReplaceAll(line, hooks, "HOOKS")
				}
				taken = append(taken, line)
			}
			said := out.String()
			if hooks != "" {
				said = strings.ReplaceAll(said, hooks, "HOOKS")
			}
			containsAll(t, said, tt.wantOut...)
			if built != tt.wantBuilt {
				t.Fatalf("ran %v, want a build %t", run.lines(), tt.wantBuilt)
			}
			if strings.Join(taken, "|") != strings.Join(tt.wantHooks, "|") {
				t.Errorf("took the hooks program with %q, want %q", taken, tt.wantHooks)
			}
			if !tt.wantBuilt || tt.wantErr != "" || tt.secrets == nil {
				return
			}
			line := strings.ReplaceAll(run.lines()[0], secretDir, "SECRETS")
			for _, want := range tt.wantArgs {
				if !strings.Contains(line, want) {
					t.Errorf("docker %s lacks %q", line, want)
				}
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
			if !slices.Contains(run.ran[0].Args, "buildx") || run.ran[0].Dir != string(w) {
				t.Errorf("ran %v in %s", run.ran[0].Args, run.ran[0].Dir)
			}
		})
	}
}
