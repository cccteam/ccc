package render

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	goversion "go/version"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"golang.org/x/mod/modfile"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/release"
	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/ci"
)

// fixtures is where the fixture applications and the placement live.
const fixtures = "../derive/testdata"

// deriveFixture derives the named fixture application under the named placement.
func deriveFixture(t *testing.T, name, placement string) *derive.Model {
	t.Helper()

	p, err := derive.ReadPlacement(filepath.Join(fixtures, placement))
	if err != nil {
		t.Fatalf("derive.ReadPlacement() error = %v", err)
	}
	a, err := app.Discover(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatalf("app.Discover() error = %v", err)
	}
	m, err := derive.Derive(a, p)
	if err != nil {
		t.Fatalf("derive.Derive() error = %v", err)
	}

	return m
}

// update rewrites the goldens from the render instead of comparing: go test ./internal/render
// -update, after a template change that is meant, in the same commit.
var update = flag.Bool("update", false, "rewrite the goldens under testdata from the render")

// TestRenderGolden proves the render reproduces the hand-written stack byte for byte:
// every file under testdata/<golden>, comments included, since the comments naming the
// source declarations are the point.
func TestRenderGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fixture   string
		placement string
		golden    string
	}{
		{name: "harbor, a Google directory auth", fixture: "harbor", placement: "placement.json", golden: "harbor"},
		{name: "beacon, a password auth", fixture: "beacon", placement: "placement-beacon.json", golden: "beacon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files, err := Render(deriveFixture(t, tt.fixture, tt.placement))
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			// The stack's files are the golden directory's; the files for the application
			// root are under its root subdirectory.
			goldenDir := filepath.Join("testdata", tt.golden)
			rendered := map[string]bool{}
			for _, f := range files {
				golden := goldenDir
				if f.Root {
					golden = filepath.Join(goldenDir, "root")
				}
				rendered[filepath.Join(golden, f.Path)] = true
				if *update {
					if err := os.MkdirAll(filepath.Dir(filepath.Join(golden, f.Path)), 0o750); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(golden, f.Path), f.Content, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(filepath.Join(golden, f.Path))
				if err != nil {
					t.Errorf("%s: rendered but not in %s: %v", f.Path, golden, err)

					continue
				}
				if !bytes.Equal(f.Content, want) {
					t.Errorf("%s differs from %s:\n%s", f.Path, golden, firstDiff(want, f.Content))
				}
			}
			for _, golden := range []string{goldenDir, filepath.Join(goldenDir, "root")} {
				entries, err := os.ReadDir(golden)
				if err != nil {
					t.Fatalf("os.ReadDir() error = %v", err)
				}
				for _, e := range entries {
					if !e.IsDir() && !rendered[filepath.Join(golden, e.Name())] {
						t.Errorf("%s is in %s but not rendered", e.Name(), golden)
					}
				}
			}
		})
	}
}

// firstDiff shows the first differing line of two texts, with a little context.
func firstDiff(want, got []byte) string {
	w := strings.Split(string(want), "\n")
	g := strings.Split(string(got), "\n")
	for i := range max(len(w), len(g)) {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return fmt.Sprintf("line %d\n  want: %q\n  got:  %q", i+1, wl, gl)
		}
	}

	return "(same lines, different bytes)"
}

func TestFormatted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		file    string
		content string
		want    string
	}{
		{
			name:    "a .tf file's assignments and trailing comments align as tofu fmt writes them",
			file:    "cloud-build.tf",
			content: "locals {\n  a = 1 # one\n  bbb = \"two\"   # two\n}\n",
			want:    "locals {\n  a   = 1     # one\n  bbb = \"two\" # two\n}\n",
		},
		{
			name:    "a .tfvars file too",
			file:    "terraform.tfvars",
			content: "a=1\n",
			want:    "a = 1\n",
		},
		{
			name:    "any other file as it is",
			file:    "cloudbuild.yaml",
			content: "steps:\n  - name:   x\n",
			want:    "steps:\n  - name:   x\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := string(Formatted(tt.file, []byte(tt.content))); got != tt.want {
				t.Errorf("Formatted() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPipelineFlowItems reads the rendered Cloud Build configurations and refuses a bare
// item of a flow sequence that YAML 1.1, which Cloud Build reads, takes for a boolean or
// null: on, off, yes, no, y, n, true, false and null, in any case, and ~. A step's args
// are strings, and "cannot unmarshal bool into Go value of type string" stopped a pipeline
// at its first step once over args: [deploy, maintenance, on]. Such a word is quoted.
func TestPipelineFlowItems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
	}{
		{name: "harbor's pipeline", file: "testdata/harbor/root/cloudbuild.yaml"},
		{name: "harbor's sweep", file: "testdata/harbor/root/cloudbuild-sweep.yaml"},
		{name: "beacon's pipeline", file: "testdata/beacon/root/cloudbuild.yaml"},
		{name: "beacon's sweep", file: "testdata/beacon/root/cloudbuild-sweep.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			for i, line := range strings.Split(string(content), "\n") {
				if word := bareYAMLBool(line); word != "" {
					t.Errorf("%s:%d: %q is a YAML 1.1 boolean or null in a flow sequence; quote it: %s", tt.file, i+1, word, strings.TrimSpace(line))
				}
			}
		})
	}
}

// TestPipelineOrder proves the rendered pipeline keeps the order a window release needs,
// start check, image, the job where the application has a job process, pre-flight, wait,
// maintenance, migrate, revision, traffic, record, reads the pull request's stack job's
// outputs before the image build where the placement declares build arguments (harbor)
// and after it otherwise (beacon), applies no Terraform for a pull request (none of the
// stack steps the pr-stack trigger holds, and no waitFor: the steps run in order), carries
// the release file's directory to the release check, renders the job steps only for an
// application with a job process, and keeps every step's own timeout under the whole-build
// ceiling of 24 hours.
func TestPipelineOrder(t *testing.T) {
	t.Parallel()

	jobSteps := []string{"CreateJobs", "SweepJobs"}
	timeouts := map[string]string{"ValidateRelease": "300s", "ReadPullRequestStack": "900s", "BuildImage": "1800s", "MaintenanceOnRestore": "3600s", "PlanEnvironmentStack": "7200s", "PreflightMigrations": "1200s", "WaitForWindow": "86400s", "MaintenanceOnWindow": "900s", "RunMigrations": "2400s", "DeployServiceNoTraffic": "1200s", "ShiftTraffic": "600s"}
	after := []string{"MaintenanceOnRestore", "PlanEnvironmentStack", "ApplyEnvironmentStack", "CreateJobs", "PreflightMigrations", "WaitForWindow", "MaintenanceOnWindow", "RunMigrations", "DeployServiceNoTraffic", "ShiftTraffic", "MaintenanceOff", "SweepJobs", "WriteDeploymentRecord"}
	tests := []struct {
		name  string
		file  string
		jobs  bool
		order []string
	}{
		{name: "harbor's pipeline, whose image takes the pull request's own build arguments", file: "testdata/harbor/root/cloudbuild.yaml", jobs: true, order: append([]string{"ValidateRelease", "CheckRelease", "ReadPullRequestStack", "BuildImage"}, after...)},
		{name: "beacon's pipeline, whose image takes none", file: "testdata/beacon/root/cloudbuild.yaml", order: append([]string{"ValidateRelease", "CheckRelease", "BuildImage", "ReadPullRequestStack"}, after...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			var ids []string
			seen := map[string]string{}
			for _, line := range strings.Split(string(content), "\n") {
				if id, ok := strings.CutPrefix(line, "  - id: "); ok {
					ids = append(ids, id)
				}
				if timeout, ok := strings.CutPrefix(line, "    timeout: "); ok && len(ids) > 0 {
					seen[ids[len(ids)-1]] = timeout
				}
				if strings.Contains(line, "waitFor") {
					t.Errorf("the pipeline branches (%q); its steps run in order", strings.TrimSpace(line))
				}
			}
			for _, id := range []string{"PlanEnvironments", "PlanPullRequestStack", "GuardPullRequestPlan", "ApplyPullRequestStack", "RefreshPullRequestStack"} {
				if slices.Contains(ids, id) {
					t.Errorf("step %s is rendered: the pull request's stack is the stack's own triggers' job, never this file's", id)
				}
			}
			want := tt.order
			if !tt.jobs {
				want = slices.DeleteFunc(slices.Clone(tt.order), func(id string) bool {
					return slices.Contains(jobSteps, id)
				})
				for _, id := range jobSteps {
					if slices.Contains(ids, id) {
						t.Errorf("step %s is rendered for an application without a job process", id)
					}
				}
			}
			at := 0
			for _, id := range want {
				i := slices.Index(ids[at:], id)
				if i < 0 {
					t.Errorf("step %s is missing or out of order after %v", id, ids[:at])

					continue
				}
				at += i + 1
			}
			for id, want := range timeouts {
				if seen[id] != want {
					t.Errorf("step %s has timeout %q, want %q", id, seen[id], want)
				}
			}
			if !strings.Contains(string(content), "args: [deploy, validate-release, --router-dir, pkg/router]") {
				t.Errorf("the release check is not told the router directory:\n%s", content)
			}
			if !strings.Contains(string(content), "\ntimeout: 86400s\n") {
				t.Errorf("the whole-build timeout is not the 24-hour ceiling:\n%s", content)
			}
		})
	}
}

// triggerBlocks are the google_cloudbuild_trigger resources of a rendered cloud-build.tf,
// by name, parsed with HCL's own parser.
func triggerBlocks(t *testing.T, file string) map[string]*hclsyntax.Block {
	t.Helper()

	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	f, diags := hclsyntax.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatalf("hclsyntax.ParseConfig() = %v", diags)
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		t.Fatalf("body is %T", f.Body)
	}
	triggers := map[string]*hclsyntax.Block{}
	for _, b := range body.Blocks {
		if b.Type == "resource" && len(b.Labels) == 2 && b.Labels[0] == "google_cloudbuild_trigger" {
			triggers[b.Labels[1]] = b
		}
	}

	return triggers
}

// exprText is an attribute's expression as the source writes it.
func exprText(t *testing.T, block *hclsyntax.Block, name string) string {
	t.Helper()

	attr, ok := block.Body.Attributes[name]
	if !ok {
		return ""
	}
	src, err := os.ReadFile(attr.SrcRange.Filename)
	if err != nil {
		t.Fatal(err)
	}

	return string(attr.Expr.Range().SliceBytes(src))
}

// nested is the first block of the type under the block, or nil.
func nested(block *hclsyntax.Block, typ string) *hclsyntax.Block {
	for _, b := range block.Body.Blocks {
		if b.Type == typ {
			return b
		}
	}

	return nil
}

// TestTriggerSteps reads the stack's triggers from the golden stacks: the check-and-plan
// trigger and the pull-request stack trigger hold their steps in the trigger, the step
// ids the deploy package compares a build's against, run as the apply identity, on every
// push (comments disabled) and on the /gcbrun comment, with the build logs sent to Cloud
// Logging; the preview trigger keeps cloudbuild.yaml and the deploy identity; the sweep
// runs as the apply identity.
func TestTriggerSteps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		trigger string
		// steps are the step ids the trigger holds; empty for a trigger that runs a file.
		steps    []string
		filename string
		identity string
		// pullRequest is whether the trigger runs on pull requests; commentControl is
		// the comment control it names, empty for none (the API's "disabled").
		pullRequest    bool
		commentControl string
	}{
		{name: "the check-and-plan trigger names no comment control: the API's disabled", trigger: CheckTrigger, steps: CheckTriggerSteps, identity: "local.apply_identity_id", pullRequest: true},
		{name: "the pull-request stack trigger", trigger: "pr_stack", steps: StackTriggerSteps, identity: "local.apply_identity_id", pullRequest: true, commentControl: `"COMMENTS_ENABLED"`},
		{name: "the preview trigger keeps the pipeline file and the deploy identity", trigger: "pr", filename: `"cloudbuild.yaml"`, identity: "local.identities.deploy_identity_id", pullRequest: true, commentControl: `"COMMENTS_ENABLED"`},
		{name: "the version trigger", trigger: "version", filename: `"cloudbuild.yaml"`, identity: "local.identities.deploy_identity_id"},
		{name: "the sweep runs as the apply identity", trigger: "sweep", identity: "local.apply_identity_id"},
		{name: "the release trigger runs as the release identity, on pushes, with its steps held", trigger: "release", steps: ReleaseTriggerSteps, identity: "local.release_identity_id"},
	}
	for _, fixture := range []string{"harbor", "beacon"} {
		triggers := triggerBlocks(t, filepath.Join("testdata", fixture, "cloud-build.tf"))
		for _, tt := range tests {
			t.Run(fixture+": "+tt.name, func(t *testing.T) {
				t.Parallel()

				block, ok := triggers[tt.trigger]
				if !ok {
					t.Fatalf("no trigger %q among %v", tt.trigger, slices.Sorted(maps.Keys(triggers)))
				}
				if got := exprText(t, block, "service_account"); got != tt.identity {
					t.Errorf("service_account = %s, want %s", got, tt.identity)
				}
				if got := exprText(t, block, "filename"); got != tt.filename {
					t.Errorf("filename = %s, want %s", got, tt.filename)
				}
				if event := nested(block, "repository_event_config"); event != nil {
					if pr := nested(event, "pull_request"); (pr != nil) != tt.pullRequest {
						t.Errorf("pull_request block present %t, want %t", pr != nil, tt.pullRequest)
					} else if pr != nil {
						if got := exprText(t, pr, "comment_control"); got != tt.commentControl {
							t.Errorf("comment_control = %s, want %s", got, tt.commentControl)
						}
					}
				}
				build := nested(block, "build")
				if (build != nil) != (len(tt.steps) > 0) {
					t.Fatalf("build block present %t, want %t", build != nil, len(tt.steps) > 0)
				}
				if build == nil {
					return
				}
				var ids []string
				for _, step := range build.Body.Blocks {
					if step.Type == "step" {
						ids = append(ids, strings.Trim(exprText(t, step, "id"), `"`))
						if entry := exprText(t, step, "entrypoint"); entry != `"bash"` && entry != `"sh"` && entry != `"/builder/home/bedrock"` {
							t.Errorf("step %s runs %s, want bash, sh or the fetched bedrock", ids[len(ids)-1], entry)
						}
					}
				}
				if diff := cmp.Diff(tt.steps, ids); diff != "" {
					t.Errorf("steps (-want +got):\n%s", diff)
				}
				options := nested(build, "options")
				if options == nil {
					t.Fatal("no options block")
				}
				if got := exprText(t, options, "logging"); got != `"CLOUD_LOGGING_ONLY"` {
					t.Errorf("options.logging = %s, want CLOUD_LOGGING_ONLY", got)
				}
			})
		}
	}
}

// TestTriggerShell evaluates the two shell scripts the trigger-held jobs start with, as
// HCL reads them out of the golden stack: every dollar sign is doubled (Cloud Build reads
// $NAME as a substitution and $$ as a literal $, and ALLOW_LOOSE leaves an unknown $NAME
// in place, which the shell would then read as an empty variable), the pin is read from
// the checkout's placement at run time, never from the text, and the scripts parse once
// Cloud Build has turned $$ into $.
func TestTriggerShell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		local string
		want  []string
	}{
		{
			name:  "the fetch reads the pin from the placement and downloads or defers to the build",
			local: "fetch_bedrock",
			want: []string{
				`pin=$$(python3 -c 'import json; p = json.load(open("infrastructure/placement.json")); print(p["bedrockVersion"], p.get("bedrockSha256") or "-")')`,
				"\nif [ \"$$sha256\" = \"-\" ]; then\n  commit=$$(printf '%s' \"$$version\" | sed 's/.*-//')\n  branches=$$(mktemp -d)\n  if ! git clone --quiet --bare --no-tags --filter=tree:0 \"https://github.com/cccteam/ccc.git\" \"$$branches\"; then\n",
				"\n  for head in $$(git -C \"$$branches\" for-each-ref --format='%(refname:short)' refs/heads/); do\n    if git -C \"$$branches\" merge-base --is-ancestor \"$$commit\" \"refs/heads/$$head\" 2>/dev/null; then\n      on=$$head\n      break\n",
				"\n  if [ -z \"$$on\" ]; then\n    echo \"bedrock $$version pins commit $$commit, which no branch of https://github.com/cccteam/ccc.git reaches: the triggers build only a commit on a branch of github.com/cccteam/ccc, as bedrock upgrade accepts; move the pin to one, or to a release.\"\n    exit 1\n  fi\n  echo \"$$version\" > /builder/home/bedrock.pin\n  echo \"bedrock $$version is a commit pin on $$on: the next step builds it with go install.\"\n  exit 0\nfi\n",
				`-o /builder/home/bedrock "https://github.com/cccteam/ccc/releases/download/bedrock/$$version/bedrock-linux-amd64"`,
				`echo "$$sha256  /builder/home/bedrock" | sha256sum --check -`,
			},
		},
		{
			name:  "the build installs a commit pin and does nothing for a release",
			local: "build_bedrock_commit",
			want: []string{
				"if [ ! -f /builder/home/bedrock.pin ]; then\n  echo \"bedrock is a release pin, fetched by the step before: nothing to build.\"\n  exit 0\nfi\n",
				`go install -ldflags="-s -w" github.com/cccteam/ccc/bedrock@$$(cat /builder/home/bedrock.pin)`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			script := triggerScript(t, "harbor", tt.local)
			for _, want := range tt.want {
				if !strings.Contains(script, want) {
					t.Errorf("%s lacks %q:\n%s", tt.local, want, script)
				}
			}
			if strings.Contains(script, "$$$") || strings.Count(script, "$") != 2*strings.Count(script, "$$") {
				t.Errorf("%s has a dollar sign Cloud Build would read as a substitution:\n%s", tt.local, script)
			}
			if strings.HasPrefix(script, " ") || strings.HasPrefix(script, "\n") {
				t.Errorf("%s starts with whitespace: the heredoc's indentation is not stripped:\n%s", tt.local, script)
			}
			shell, err := exec.LookPath("bash")
			if err != nil {
				t.Skip("no bash to parse the script with")
			}
			cmd := exec.CommandContext(t.Context(), shell, "-n")
			cmd.Stdin = strings.NewReader(strings.ReplaceAll(script, "$$", "$"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("bash -n: %v\n%s", err, out)
			}
		})
	}
}

// triggerScript evaluates a script local of the fixture's golden cloud-build.tf as HCL
// reads it: the heredoc's text, its indentation stripped and its $$ still doubled.
func triggerScript(t *testing.T, fixture, local string) string {
	t.Helper()

	file := filepath.Join("testdata", fixture, "cloud-build.tf")
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	f, diags := hclsyntax.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatalf("hclsyntax.ParseConfig() = %v", diags)
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		t.Fatalf("body is %T", f.Body)
	}
	for _, b := range body.Blocks {
		if b.Type != "locals" {
			continue
		}
		attr, ok := b.Body.Attributes[local]
		if !ok {
			continue
		}
		value, diags := attr.Expr.Value(nil)
		if diags.HasErrors() {
			t.Fatalf("evaluating %s: %v", local, diags)
		}

		return value.AsString()
	}
	t.Fatalf("no locals block of %s holds %s", file, local)

	return ""
}

// TestTriggerFetchCommitPin runs the fetch script for a commit pin as the trigger does
// (its $$ turned into $, the home directory and the repository stood in for), over a
// checkout whose placement pins a commit: a commit on a branch of the repository is noted
// for the build step, with its branch named; a commit only a tag reaches, one the
// repository does not hold and a repository that cannot be read are refused, and nothing
// is noted.
func TestTriggerFetchCommitPin(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{"bash", "git", "python3"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s to run the step with", tool)
		}
	}
	repository, onBranch, onTag := testRepository(t)
	script := strings.ReplaceAll(triggerScript(t, "harbor", "fetch_bedrock"), "$$", "$")
	const stamp = "v0.0.0-lab.1.0.20260928222237-"
	tests := []struct {
		name       string
		version    string
		repository string
		wantOut    string
		wantErr    string
	}{
		{
			name:    "a commit on a branch: noted for the build step, with its branch",
			version: stamp + onBranch[:12],
			wantOut: "bedrock " + stamp + onBranch[:12] + " is a commit pin on main: the next step builds it with go install.\n",
		},
		{
			name:    "a commit only a tag reaches",
			version: stamp + onTag[:12],
			wantErr: "bedrock " + stamp + onTag[:12] + " pins commit " + onTag[:12] + ", which no branch of " + repository + " reaches: the triggers build only a commit on a branch of github.com/cccteam/ccc, as bedrock upgrade accepts; move the pin to one, or to a release.",
		},
		{
			name:    "a commit the repository does not hold",
			version: stamp + "0123456789ab",
			wantErr: "pins commit 0123456789ab, which no branch of " + repository + " reaches",
		},
		{
			name:       "a repository that cannot be read",
			version:    stamp + onBranch[:12],
			repository: "file://" + filepath.Join(t.TempDir(), "missing"),
			wantErr:    "could not read the branches of file://",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			checkout, home := t.TempDir(), t.TempDir()
			if err := os.MkdirAll(filepath.Join(checkout, "infrastructure"), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(checkout, "infrastructure", "placement.json"), []byte(`{"bedrockVersion": "`+tt.version+`"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			repo := tt.repository
			if repo == "" {
				repo = repository
			}
			// The script is the trigger step the render holds, run against a local git
			// repository to prove the commit-pin check; no input but the test's reaches it.
			// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
			cmd := exec.CommandContext(t.Context(), "bash", "-c", strings.ReplaceAll(strings.ReplaceAll(script, release.CloneURL(), repo), "/builder/home", home))
			cmd.Dir = checkout
			cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
			out, err := cmd.CombinedOutput()
			note, noteErr := os.ReadFile(filepath.Join(home, "bedrock.pin"))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(string(out), tt.wantErr) {
					t.Fatalf("the step failed = %v, want the refusal %q; output:\n%s", err, tt.wantErr, out)
				}
				if noteErr == nil {
					t.Errorf("the step noted %q after refusing", note)
				}

				return
			}
			if err != nil {
				t.Fatalf("the step failed: %v\n%s", err, out)
			}
			if string(out) != tt.wantOut {
				t.Errorf("the step printed %q, want %q", out, tt.wantOut)
			}
			if string(note) != tt.version+"\n" {
				t.Errorf("the note = %q (%v), want %q", note, noteErr, tt.version+"\n")
			}
		})
	}
}

// testRepository makes a repository with one commit on its one branch, main, and one only
// a tag reaches, and returns its clone URL and the two commits.
func testRepository(t *testing.T) (repository, onBranch, onTag string) {
	t.Helper()

	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()

		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "user.name=bedrock", "-c", "user.email=bedrock@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}

		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet", "--initial-branch=main")
	git("config", "uploadpack.allowFilter", "true")
	git("commit", "--quiet", "--allow-empty", "-m", "one")
	onBranch = git("rev-parse", "HEAD")
	git("checkout", "--quiet", "-b", "side")
	git("commit", "--quiet", "--allow-empty", "-m", "two")
	onTag = git("rev-parse", "HEAD")
	git("tag", "only-a-tag")
	git("checkout", "--quiet", "main")
	git("branch", "--quiet", "-D", "side")

	return "file://" + dir, onBranch, onTag
}

// bareYAMLBool is the first unquoted item of a flow sequence on the line that YAML 1.1
// reads as a boolean or null, or "".
func bareYAMLBool(line string) string {
	open := strings.Index(line, "[")
	if open < 0 || strings.HasPrefix(strings.TrimSpace(line), "#") {
		return ""
	}
	closing := strings.LastIndex(line, "]")
	if closing < open {
		return ""
	}
	for _, item := range strings.Split(line[open+1:closing], ",") {
		item = strings.TrimSpace(item)
		switch strings.ToLower(item) {
		case "y", "n", "yes", "no", "on", "off", "true", "false", "null", "~":
			return item
		}
	}

	return ""
}

// TestGoImage holds the Go image a commit pin is built in to bedrock: its Go is at least
// the go line of bedrock's go.mod, since the builds run with GOTOOLCHAIN=local and a newer
// go line would fail every pipeline pinned to that commit, and it is the image the seeded
// Dockerfile builds the application in, so bedrock names one Go digest.
func TestGoImage(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatalf("modfile.Parse() error = %v", err)
	}
	dockerfile, err := templates.ReadFile("templates/root/Dockerfile.tmpl")
	if err != nil {
		t.Fatalf("templates.ReadFile() error = %v", err)
	}
	tests := []struct {
		name string
		ok   bool
		why  string
	}{
		{
			name: "the image's Go is at least bedrock's go line",
			ok:   mod.Go != nil && goversion.Compare("go"+goImageGo, "go"+mod.Go.Version) >= 0,
			why:  fmt.Sprintf("goImage carries Go %s and bedrock's go.mod says go %v: pin a digest of an image with that Go and set goImageGo", goImageGo, mod.Go),
		},
		{
			name: "the seeded Dockerfile builds in the same image",
			ok:   bytes.Contains(dockerfile, []byte("FROM "+goImage+" AS build-env")),
			why:  "the seeded Dockerfile's build stage is not FROM " + goImage + ": move both to the same digest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !tt.ok {
				t.Error(tt.why)
			}
		})
	}
}

func TestWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		existing     map[string]string
		existingRoot map[string]string
		wantOwned    int
		wantPipeline []string
		wantSeeded   []string
		wantKept     []string
		// wantRemoved are the retired files found at the application root and removed.
		wantRemoved []string
		wantTfvars  string
		wantDocker  string
	}{
		{
			name:         "a retired file at the application root is removed",
			existingRoot: map[string]string{".github/workflows/release-please.yml": "name: release-please\n"},
			wantOwned:    2,
			wantPipeline: []string{"cloudbuild.yaml"},
			wantSeeded:   []string{"terraform.tfvars", "Dockerfile"},
			wantRemoved:  []string{".github/workflows/release-please.yml"},
			wantTfvars:   "seeded\n",
			wantDocker:   "image\n",
		},
		{
			name:         "empty directories get everything",
			wantOwned:    2,
			wantPipeline: []string{"cloudbuild.yaml"},
			wantSeeded:   []string{"terraform.tfvars", "Dockerfile"},
			wantTfvars:   "seeded\n",
			wantDocker:   "image\n",
		},
		{
			name:         "owned files are rewritten, seeded files kept, in both places",
			existing:     map[string]string{"locals.tf": "old\n", "terraform.tfvars": "mine\n"},
			existingRoot: map[string]string{"cloudbuild.yaml": "old pipeline\n", "Dockerfile": "my image\n"},
			wantOwned:    2,
			wantPipeline: []string{"cloudbuild.yaml"},
			wantKept:     []string{"terraform.tfvars", "Dockerfile"},
			wantTfvars:   "mine\n",
			wantDocker:   "my image\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "stack")
			appDir := t.TempDir()
			for name, content := range tt.existing {
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for name, content := range tt.existingRoot {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(appDir, name)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(appDir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			files := []File{
				{Path: "locals.tf", Tier: Owned, Content: []byte("new\n")},
				{Path: "README.md", Tier: Owned, Content: []byte("readme\n")},
				{Path: "terraform.tfvars", Tier: Seeded, Content: []byte("seeded\n")},
				{Path: "cloudbuild.yaml", Tier: Owned, Root: true, Content: []byte("pipeline\n")},
				{Path: "Dockerfile", Tier: Seeded, Root: true, Content: []byte("image\n")},
			}
			written, err := Write(files, dir, appDir)
			if err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			if written.Owned != tt.wantOwned {
				t.Errorf("Write() owned = %d, want %d", written.Owned, tt.wantOwned)
			}
			if strings.Join(written.Pipeline, ",") != strings.Join(tt.wantPipeline, ",") {
				t.Errorf("Write() pipeline = %v, want %v", written.Pipeline, tt.wantPipeline)
			}
			if strings.Join(written.Seeded, ",") != strings.Join(tt.wantSeeded, ",") {
				t.Errorf("Write() seeded = %v, want %v", written.Seeded, tt.wantSeeded)
			}
			if strings.Join(written.Kept, ",") != strings.Join(tt.wantKept, ",") {
				t.Errorf("Write() kept = %v, want %v", written.Kept, tt.wantKept)
			}
			if strings.Join(written.Removed, ",") != strings.Join(tt.wantRemoved, ",") {
				t.Errorf("Write() removed = %v, want %v", written.Removed, tt.wantRemoved)
			}
			for _, name := range Retired {
				if _, err := os.Stat(filepath.Join(appDir, filepath.FromSlash(name))); err == nil {
					t.Errorf("%s is still there after the write", name)
				}
			}
			for path, want := range map[string]string{
				filepath.Join(dir, "locals.tf"):          "new\n",
				filepath.Join(dir, "terraform.tfvars"):   tt.wantTfvars,
				filepath.Join(appDir, "cloudbuild.yaml"): "pipeline\n",
				filepath.Join(appDir, "Dockerfile"):      tt.wantDocker,
			} {
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("os.ReadFile(%s) error = %v", path, err)
				}
				if string(got) != want {
					t.Errorf("%s = %q, want %q", path, got, want)
				}
			}
		})
	}
}

// TestReleaseFiles: render seeds release-please's configuration and manifest at the
// application root, the configuration starting the releases at 0.1.0 (initial-version)
// with a feature on the minor below 1.0, the manifest at 0.0.0, and keeps either once the
// application has it.
func TestReleaseFiles(t *testing.T) {
	t.Parallel()

	files, err := Render(deriveFixture(t, "harbor", "placement.json"))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	rendered := map[string]File{}
	for _, f := range files {
		if f.Root {
			rendered[f.Path] = f
		}
	}
	var config map[string]any
	if err := json.Unmarshal(rendered[ReleasePleaseConfig].Content, &config); err != nil {
		t.Fatalf("%s does not read: %v", ReleasePleaseConfig, err)
	}
	var manifest map[string]string
	if err := json.Unmarshal(rendered[ReleasePleaseManifest].Content, &manifest); err != nil {
		t.Fatalf("%s does not read: %v", ReleasePleaseManifest, err)
	}
	// hidden is each changelog section's visibility by type; a type outside the map has
	// no section, and release-please would drop its merges.
	hidden := map[string]bool{}
	sections, _ := config["changelog-sections"].([]any)
	for _, s := range sections {
		section, _ := s.(map[string]any)
		typ, _ := section["type"].(string)
		h, _ := section["hidden"].(bool)
		hidden[typ] = h
	}
	covered := make([]string, 0, len(ci.TitleTypes))
	for _, typ := range ci.TitleTypes {
		if _, ok := hidden[typ]; ok {
			covered = append(covered, typ)
		}
	}
	tests := []struct {
		name string
		got  any
		want any
	}{
		{name: "every type the title check accepts has a section", got: strings.Join(covered, ","), want: strings.Join(ci.TitleTypes, ",")},
		{name: "an upgrade releases: its section is shown", got: hidden["upgrade"], want: false},
		{name: "an infrastructure change releases: its section is shown", got: hidden["infra"], want: false},
		{name: "a configuration change releases: its section is shown", got: hidden["config"], want: false},
		{name: "a cleanup is hidden, beside refactor", got: hidden["cleanup"] && hidden["refactor"], want: true},
		{name: "a feature is shown", got: hidden["feat"] || hidden["feature"], want: false},
		{name: "the configuration is seeded at the root", got: rendered[ReleasePleaseConfig].Tier == Seeded && rendered[ReleasePleaseConfig].Root, want: true},
		{name: "the manifest is seeded at the root", got: rendered[ReleasePleaseManifest].Tier == Seeded && rendered[ReleasePleaseManifest].Root, want: true},
		{name: "the first release is 0.1.0", got: config["initial-version"], want: "0.1.0"},
		{name: "a feature advances the minor below 1.0", got: config["bump-patch-for-minor-pre-major"], want: false},
		{name: "a breaking change advances the minor below 1.0", got: config["bump-minor-pre-major"], want: true},
		{name: "the tags are v<version>, as the pipeline reads them", got: config["include-v-in-tag"], want: true},
		{name: "one package, the repository", got: fmt.Sprint(config["packages"]), want: "map[.:map[]]"},
		{name: "the manifest starts at 0.0.0", got: manifest["."], want: "0.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}

	seeding := []struct {
		name       string
		existing   map[string]string
		wantSeeded []string
		wantKept   []string
	}{
		{name: "an application without them gets both", wantSeeded: []string{ReleasePleaseConfig, ReleasePleaseManifest}},
		{
			name:       "a manifest release-please moved is kept",
			existing:   map[string]string{ReleasePleaseManifest: "{\".\": \"0.3.1\"}\n"},
			wantSeeded: []string{ReleasePleaseConfig},
			wantKept:   []string{ReleasePleaseManifest},
		},
	}
	for _, tt := range seeding {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appDir := t.TempDir()
			for name, content := range tt.existing {
				if err := os.WriteFile(filepath.Join(appDir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			written, err := Write([]File{rendered[ReleasePleaseConfig], rendered[ReleasePleaseManifest]}, filepath.Join(t.TempDir(), "stack"), appDir)
			if err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			if strings.Join(written.Seeded, ",") != strings.Join(tt.wantSeeded, ",") {
				t.Errorf("Write() seeded = %v, want %v", written.Seeded, tt.wantSeeded)
			}
			if strings.Join(written.Kept, ",") != strings.Join(tt.wantKept, ",") {
				t.Errorf("Write() kept = %v, want %v", written.Kept, tt.wantKept)
			}
			for name, content := range tt.existing {
				if got, err := os.ReadFile(filepath.Join(appDir, name)); err != nil || string(got) != content {
					t.Errorf("%s = %q (%v), want %q kept", name, got, err, content)
				}
			}
		})
	}
}

// store is a derived file store at the level, for the file-store tests.
func store(variable, level, name string) derive.FileStore {
	resource, suffix := "files", "files"
	if name != "" {
		resource += "_" + strings.ReplaceAll(name, "-", "_")
		suffix += "-" + name
	}

	return derive.FileStore{Variable: &derive.Variable{Name: variable, Level: level, Struct: "dataConfig", Field: "Store"}, Name: name, Resource: resource, Suffix: suffix}
}

func TestNewFileStore(t *testing.T) {
	t.Parallel()

	data := &derive.Process{Name: "jobs", Dir: "cmd/jobs", Levels: []string{derive.LevelCore, derive.LevelData}}
	tests := []struct {
		name          string
		store         derive.FileStore
		jobs          *derive.Process
		wantValue     string
		wantLabel     string
		wantTitle     string
		wantJobsReads bool
	}{
		{
			name:          "the default store: the bucket's gs:// URL from its name, read by a job process that constructs its level",
			store:         store("APP_FILE_STORE", derive.LevelData, ""),
			jobs:          data,
			wantValue:     `"gs://${local.files_bucket_name}"`,
			wantLabel:     "the default file store",
			wantTitle:     "The default file store",
			wantJobsReads: true,
		},
		{
			name:          "a named store: the resource with underscores in the URL, the name in the label",
			store:         store("APP_FILE_STORE_DOCUMENTS", derive.LevelData, "documents"),
			jobs:          data,
			wantValue:     `"gs://${local.files_documents_bucket_name}"`,
			wantLabel:     "the documents file store",
			wantTitle:     "The documents file store",
			wantJobsReads: true,
		},
		{
			name:      "a two-word name",
			store:     store("APP_FILE_STORE_CLIENT_FILES", derive.LevelData, "client-files"),
			wantValue: `"gs://${local.files_client_files_bucket_name}"`,
			wantLabel: "the client-files file store",
			wantTitle: "The client-files file store",
		},
		{
			name:      "a store at a level the job process does not construct",
			store:     store("APP_FILE_STORE", derive.LevelSite, ""),
			jobs:      data,
			wantValue: `"gs://${local.files_bucket_name}"`,
			wantLabel: "the default file store",
			wantTitle: "The default file store",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := newFileStore(tt.store, tt.jobs)
			if got.Value != tt.wantValue || got.Label != tt.wantLabel || got.Title != tt.wantTitle || got.JobsReads != tt.wantJobsReads {
				t.Errorf("newFileStore() = value %s, label %q, title %q, jobs %t; want %s, %q, %q, %t", got.Value, got.Label, got.Title, got.JobsReads, tt.wantValue, tt.wantLabel, tt.wantTitle, tt.wantJobsReads)
			}
		})
	}
}

func TestFileStores(t *testing.T) {
	t.Parallel()

	data := &derive.Process{Name: "jobs", Dir: "cmd/jobs", Levels: []string{derive.LevelCore, derive.LevelData}}
	tests := []struct {
		name   string
		stores []derive.FileStore
		jobs   *derive.Process
		// wantEnv is the aligned block of every store, wantJobsEnv the job process's own
		// block (empty when it reads every store's level or none), wantJobs the stores it
		// reads, and wantAddresses the buckets for the pipeline's substitution.
		wantEnv       string
		wantJobsEnv   string
		wantJobs      []string
		wantAddresses string
	}{
		{name: "no store", stores: nil, jobs: data},
		{
			name:          "one store the job process reads: one block serves both",
			stores:        []derive.FileStore{store("APP_FILE_STORE", derive.LevelData, "")},
			jobs:          data,
			wantEnv:       `    APP_FILE_STORE = "gs://${local.files_bucket_name}"`,
			wantJobs:      []string{"APP_FILE_STORE"},
			wantAddresses: "google_storage_bucket.files[0]",
		},
		{
			name:          "two stores at the data level, aligned, in declaration order",
			stores:        []derive.FileStore{store("APP_FILE_STORE", derive.LevelData, ""), store("APP_FILE_STORE_DOCUMENTS", derive.LevelData, "documents")},
			jobs:          data,
			wantEnv:       "    APP_FILE_STORE           = \"gs://${local.files_bucket_name}\"\n    APP_FILE_STORE_DOCUMENTS = \"gs://${local.files_documents_bucket_name}\"",
			wantJobs:      []string{"APP_FILE_STORE", "APP_FILE_STORE_DOCUMENTS"},
			wantAddresses: "google_storage_bucket.files[0],google_storage_bucket.files_documents[0]",
		},
		{
			name:          "a store at a level the job process does not construct gets the job process a block of its own",
			stores:        []derive.FileStore{store("APP_FILE_STORE", derive.LevelData, ""), store("APP_FILE_STORE_DOCUMENTS", derive.LevelSite, "documents")},
			jobs:          data,
			wantEnv:       "    APP_FILE_STORE           = \"gs://${local.files_bucket_name}\"\n    APP_FILE_STORE_DOCUMENTS = \"gs://${local.files_documents_bucket_name}\"",
			wantJobsEnv:   `    APP_FILE_STORE = "gs://${local.files_bucket_name}"`,
			wantJobs:      []string{"APP_FILE_STORE"},
			wantAddresses: "google_storage_bucket.files[0],google_storage_bucket.files_documents[0]",
		},
		{
			name:          "no job process: every store is the site's alone",
			stores:        []derive.FileStore{store("APP_FILE_STORE", derive.LevelData, "")},
			wantEnv:       `    APP_FILE_STORE = "gs://${local.files_bucket_name}"`,
			wantAddresses: "google_storage_bucket.files[0]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v := &view{Model: &derive.Model{FileStores: tt.stores, Jobs: tt.jobs}}
			v.fileStores()
			if len(v.FileStores) != len(tt.stores) {
				t.Errorf("FileStores has %d, want %d", len(v.FileStores), len(tt.stores))
			}
			if v.FileStoreEnv != tt.wantEnv {
				t.Errorf("FileStoreEnv =\n%s\nwant\n%s", v.FileStoreEnv, tt.wantEnv)
			}
			if v.JobsFileStoreEnv != tt.wantJobsEnv {
				t.Errorf("JobsFileStoreEnv =\n%s\nwant\n%s", v.JobsFileStoreEnv, tt.wantJobsEnv)
			}
			var jobs []string
			for i := range v.JobsFileStores {
				jobs = append(jobs, v.JobsFileStores[i].Variable.Name)
			}
			if !slices.Equal(jobs, tt.wantJobs) {
				t.Errorf("JobsFileStores = %v, want %v", jobs, tt.wantJobs)
			}
			if v.FileStoreAddresses != tt.wantAddresses {
				t.Errorf("FileStoreAddresses = %q, want %q", v.FileStoreAddresses, tt.wantAddresses)
			}
		})
	}
}

// TestFileStorePolicy reads a file store's bucket policy as the stack declares it: the
// bucket's whole permission list, objectUser for the site and, when the job process
// constructs the store's level, the job, nobody else, set again with a replaced bucket,
// and no member resource on the bucket beside it.
// TestViewJobsTemplate finds the job process's template variable for the templates, as
// the served site declares it by embedding the job driver's settings, under the name
// derive reads off the driver's declaration; a model with a job process and no variable
// in the role is refused, since locals.tf would name the template to the service through
// nothing, and a model without a job process has none to find.
func TestViewJobsTemplate(t *testing.T) {
	t.Parallel()

	// withoutRole strips the template variable's role, as a model derive never produces
	// would carry it.
	withoutRole := func(m *derive.Model) {
		for i := range m.Variables {
			if m.Variables[i].Role == derive.RoleJobsTemplate {
				m.Variables[i].Role = derive.RoleNone
			}
		}
	}
	tests := []struct {
		name string
		// mutate changes the harbor model before the view is prepared.
		mutate func(m *derive.Model)
		// wantDeclaration and wantLevel are the variable the view found; both empty for
		// none.
		wantDeclaration string
		wantLevel       string
		wantErr         string
	}{
		{name: "harbor: the site level embeds the driver's settings", mutate: func(*derive.Model) {}, wantDeclaration: "siteConfig.Template", wantLevel: derive.LevelSite},
		{
			name:    "a job process with no variable in the role is refused",
			mutate:  withoutRole,
			wantErr: "no variable in the jobs-template role: the stack names the job process's template job to the service through it",
		},
		{
			name: "no job process: nothing to find",
			mutate: func(m *derive.Model) {
				withoutRole(m)
				m.Jobs = nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := deriveFixture(t, "harbor", "placement.json")
			tt.mutate(m)
			v, err := newView(m)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("newView() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("newView() error = %v", err)
			}
			if v.JobsTemplateVariable != derive.JobsTemplateVariable {
				t.Errorf("JobsTemplateVariable = %q, want derive's %q", v.JobsTemplateVariable, derive.JobsTemplateVariable)
			}
			if tt.wantDeclaration == "" {
				if v.JobsTemplate != nil {
					t.Errorf("JobsTemplate = %+v, want none", v.JobsTemplate)
				}

				return
			}
			if v.JobsTemplate == nil || v.JobsTemplate.Declaration() != tt.wantDeclaration || v.JobsTemplate.Level != tt.wantLevel || v.JobsTemplate.Name != v.JobsTemplateVariable {
				t.Errorf("JobsTemplate = %+v, want %s at the %s level named %s", v.JobsTemplate, tt.wantDeclaration, tt.wantLevel, v.JobsTemplateVariable)
			}
		})
	}
}

func TestFileStorePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// level is the store's level: the data level, which harbor's job process
		// constructs, or the site level, which it does not.
		level  string
		want   []string
		absent []string
	}{
		{
			name:  "a store the job process reads: the site and the job",
			level: derive.LevelData,
			want: []string{
				// The bucket and its policy exist where the stack owns its database; a
				// pull-request stack sharing the integration environment's database uses
				// its bucket, granting its own identities on it.
				"resource \"google_storage_bucket\" \"files\" {\n  count = local.own_database ? 1 : 0\n",
				"  name     = local.files_bucket_name\n  location = local.files_location\n",
				"  dynamic \"custom_placement_config\" {\n    for_each = local.files_regional ? [] : [1]\n",
				"  soft_delete_policy {\n    retention_duration_seconds = local.is_prd ? 604800 : 0\n  }\n",
				"data \"google_service_accounts\" \"files_previews\" {\n  count = local.own_database && !local.is_pr && var.environment == \"tst\" ? 1 : 0\n\n  project = local.project_id\n  prefix  = \"${local.app}-pr\"\n}\n",
				"data \"google_iam_policy\" \"files\" {\n  count = local.own_database ? 1 : 0\n\n  binding {\n    role    = \"roles/storage.objectUser\"\n    members = concat([local.app_member, local.jobs_member], local.files_preview_members)\n  }\n\n  # Cloud Storage's default grants to the project's basic roles, kept as it\n  # makes them.\n  binding {\n    role    = \"roles/storage.legacyBucketOwner\"\n    members = [\"projectOwner:${local.project_id}\", \"projectEditor:${local.project_id}\"]\n  }\n  binding {\n    role    = \"roles/storage.legacyObjectOwner\"\n    members = [\"projectOwner:${local.project_id}\", \"projectEditor:${local.project_id}\"]\n  }\n  binding {\n    role    = \"roles/storage.legacyBucketReader\"\n    members = [\"projectViewer:${local.project_id}\"]\n  }\n  binding {\n    role    = \"roles/storage.legacyObjectReader\"\n    members = [\"projectViewer:${local.project_id}\"]\n  }\n}\n",
				"and the project's\n# basic roles as Cloud Storage grants them on a new bucket",
				"resource \"google_storage_bucket_iam_policy\" \"files\" {\n  count = local.own_database ? 1 : 0\n\n  bucket      = google_storage_bucket.files[0].name\n  policy_data = data.google_iam_policy.files[0].policy_data\n\n  depends_on = [google_service_account.app, google_service_account.jobs]\n",
				"  lifecycle {\n    replace_triggered_by = [google_storage_bucket.files[0]]\n  }\n",
				"resource \"google_storage_bucket_iam_member\" \"files_preview\" {\n  for_each = local.own_database ? toset([]) : toset([\"app\", \"jobs\"])\n\n  bucket = local.files_bucket_name\n  role   = \"roles/storage.objectUser\"\n  member = each.key == \"app\" ? local.app_member : local.jobs_member\n}\n",
				"the one authoritative IAM resource the stack\n# declares",
			},
			absent: []string{"resource \"google_storage_bucket_iam_member\" \"files_app\"", "resource \"google_storage_bucket_iam_member\" \"files_jobs\""},
		},
		{
			name:  "a store at a level the job process does not construct: the site alone",
			level: derive.LevelSite,
			want: []string{
				"    members = concat([local.app_member], local.files_preview_members)\n",
				"  depends_on = [google_service_account.app]\n",
				"  for_each = local.own_database ? toset([]) : toset([\"app\"])\n\n  bucket = local.files_bucket_name\n  role   = \"roles/storage.objectUser\"\n  member = local.app_member\n}\n",
			},
			absent: []string{"resource \"google_storage_bucket_iam_member\" \"files_app\"", "local.jobs_member", "files_jobs"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := deriveFixture(t, "harbor", "placement.json")
			if len(m.FileStores) != 1 {
				t.Fatalf("harbor declares %d file stores, want 1", len(m.FileStores))
			}
			m.FileStores[0].Variable.Level = tt.level
			files, err := Render(m)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			var storage string
			for _, f := range files {
				if f.Path == "storage.tf" {
					storage = string(f.Content)
				}
			}
			if storage == "" {
				t.Fatal("storage.tf is not rendered")
			}
			for _, w := range tt.want {
				if !strings.Contains(storage, w) {
					t.Errorf("storage.tf lacks:\n%s", w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(storage, a) {
					t.Errorf("storage.tf still carries %q", a)
				}
			}
		})
	}
}

func TestBuildMachine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// buildMachine is the placement's value; empty leaves the machine to Cloud Build.
		buildMachine string
		wantPipeline []string
		wantReadme   []string
		// absentPipeline and absentReadme are what each file must not carry: the README
		// names options.machineType either way, saying there is none when it is unset.
		absentPipeline []string
		absentReadme   []string
	}{
		{
			name:         "no build machine: the options carry none and the README says so",
			buildMachine: "",
			wantPipeline: []string{"options:\n  logging: CLOUD_LOGGING_ONLY\n  # The bedrock deploy steps"},
			wantReadme: []string{
				"run on Cloud Build's default machine, the placement naming no\n  `buildMachine`",
				"no `options.machineType`,\n  the placement naming no `buildMachine`",
				"`E2_MEDIUM` (1 vCPU), `E2_STANDARD_2` (2 vCPUs), `E2_HIGHCPU_8` (8 vCPUs), and `E2_HIGHCPU_32` (32 vCPUs)",
			},
			absentPipeline: []string{"machineType"},
			absentReadme:   []string{"`options.machineType: "},
		},
		{
			name:         "a 32-vCPU machine: options.machineType and the README's arithmetic",
			buildMachine: "E2_HIGHCPU_32",
			wantPipeline: []string{"options:\n  logging: CLOUD_LOGGING_ONLY\n  # The placement's buildMachine.", "  machineType: E2_HIGHCPU_32\n  # The bedrock deploy steps"},
			wantReadme: []string{
				"run on `E2_HIGHCPU_32` (32 vCPUs), the placement's\n  `buildMachine`",
				"`options.machineType: E2_HIGHCPU_32`,\n  the machine the placement's `buildMachine` names",
				"sixteen builds of 8 vCPUs\n  at once, or four of 32",
			},
			absentReadme: []string{"Cloud Build's default machine"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := deriveFixture(t, "harbor", "placement.json")
			m.Placement.BuildMachine = tt.buildMachine
			files, err := Render(m)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			var pipeline, readme string
			for _, f := range files {
				switch {
				case f.Root && f.Path == "cloudbuild.yaml":
					pipeline = string(f.Content)
				case !f.Root && f.Path == "README.md":
					readme = string(f.Content)
				}
			}
			if pipeline == "" || readme == "" {
				t.Fatal("cloudbuild.yaml or README.md is not rendered")
			}
			for _, w := range tt.wantPipeline {
				if !strings.Contains(pipeline, w) {
					t.Errorf("cloudbuild.yaml lacks:\n%s", w)
				}
			}
			for _, w := range tt.wantReadme {
				if !strings.Contains(readme, w) {
					t.Errorf("README.md lacks:\n%s", w)
				}
			}
			for _, a := range tt.absentPipeline {
				if strings.Contains(pipeline, a) {
					t.Errorf("cloudbuild.yaml still carries %q", a)
				}
			}
			for _, a := range tt.absentReadme {
				if strings.Contains(readme, a) {
					t.Errorf("README.md still carries %q", a)
				}
			}
		})
	}
}

// TestMaxInstances pins the service's scaling for each shape of the placement's caps: no
// cap anywhere (no max_instance_count, no local), the same cap everywhere, and caps in
// some environments, where the others take Cloud Run's default through the lookup's null.
func TestMaxInstances(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		caps       map[string]int
		wantRun    []string
		wantLocals []string
		wantReadme string
		absent     []string
	}{
		{
			name: "no cap: Cloud Run's default, no scaling block, and no local",
			caps: nil,
			wantRun: []string{
				"    # Scale to zero, and to Cloud Run's default maximum instances per region:\n    # the placement caps no environment (maxInstances). No scaling block: Cloud\n    # Run's defaults are exactly that, and a block setting only a minimum of\n    # zero is dropped by the service, which every later plan would add again.\n",
			},
			wantReadme: "It scales from zero to Cloud Run's default maximum instances per region, the placement capping no environment (`maxInstances`).",
			absent:     []string{"max_instance_count", "max_instances", "scaling {"},
		},
		{
			name:       "the same cap in every environment",
			caps:       map[string]int{"tst": 2, "stg": 2, "prd": 2},
			wantRun:    []string{"    scaling {\n      min_instance_count = 0\n      max_instance_count = local.max_instances\n    }\n"},
			wantLocals: []string{"  max_instances = lookup({ tst = 2, stg = 2, prd = 2 }, var.environment, null)\n"},
			wantReadme: "It scales from zero to at most 2 instances per region in every environment, the placement's cap (`maxInstances`).",
		},
		{
			name:       "caps in some environments, the rest uncapped",
			caps:       map[string]int{"tst": 1, "prd": 20},
			wantRun:    []string{"      max_instance_count = local.max_instances\n"},
			wantLocals: []string{"  max_instances = lookup({ tst = 1, prd = 20 }, var.environment, null)\n"},
			wantReadme: "It scales from zero to at most 1 instance per region in tst, 20 in prd, the placement's caps (`maxInstances`), and to Cloud Run's default maximum in stg.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := deriveFixture(t, "harbor", "placement.json")
			m.Placement.MaxInstances = tt.caps
			files, err := Render(m)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			text := map[string]string{}
			for _, f := range files {
				if !f.Root {
					text[f.Path] = string(f.Content)
				}
			}
			for _, w := range tt.wantRun {
				if !strings.Contains(text["cloud-run.tf"], w) {
					t.Errorf("cloud-run.tf lacks:\n%s", w)
				}
			}
			for _, w := range tt.wantLocals {
				if !strings.Contains(text["locals.tf"], w) {
					t.Errorf("locals.tf lacks:\n%s", w)
				}
			}
			if !strings.Contains(text["README.md"], tt.wantReadme) {
				t.Errorf("README.md lacks:\n%s", tt.wantReadme)
			}
			for _, a := range tt.absent {
				for _, path := range []string{"cloud-run.tf", "locals.tf"} {
					if strings.Contains(text[path], a) {
						t.Errorf("%s carries %q", path, a)
					}
				}
			}
		})
	}
}

// TestOutlierThresholds renders the backend service's outlier detection from the
// placement: today's thresholds and their comment when it sets none, and the placement's
// numbers, with a comment naming where they come from, when it sets them, in the backend
// and in the README alike.
func TestOutlierThresholds(t *testing.T) {
	t.Parallel()

	n := func(v int) *int {
		return &v
	}
	tests := []struct {
		name       string
		set        *derive.OutlierDetection
		wantLB     []string
		wantReadme string
		absent     []string
	}{
		{
			name: "none set: today's thresholds",
			wantLB: []string{
				"  # Five consecutive errors in a one-second window eject the region for\n  # thirty seconds, never more than half the backends at once; enforcing 100\n  # means every detection counts.\n  outlier_detection {\n" +
					"    consecutive_errors           = 5\n    enforcing_consecutive_errors = 100\n    max_ejection_percent         = 50\n\n    interval {\n      seconds = 1\n    }\n\n    base_ejection_time {\n      seconds = 30\n    }\n  }\n\n  log_config {\n",
			},
			wantReadme: "outlier detection on (5 consecutive errors in a 1-second interval eject a\n  backend for 30 seconds, at most 50% ejected, enforced at 100), request",
			absent:     []string{"placement.json's (outlierDetection)"},
		},
		{
			name: "every threshold set",
			set:  &derive.OutlierDetection{ConsecutiveErrors: n(3), EnforcingConsecutiveErrors: n(80), MaxEjectionPercent: n(100), IntervalSeconds: n(2), BaseEjectionSeconds: n(60)},
			wantLB: []string{
				"  # The thresholds are placement.json's (outlierDetection): 3 consecutive\n  # errors in a 2-second window eject the region for 60 seconds, never\n  # more than 100 percent of the backends at once; enforcing 80 means that\n  # percent of the detections eject.\n  outlier_detection {\n" +
					"    consecutive_errors           = 3\n    enforcing_consecutive_errors = 80\n    max_ejection_percent         = 100\n\n    interval {\n      seconds = 2\n    }\n\n    base_ejection_time {\n      seconds = 60\n    }\n  }\n",
			},
			wantReadme: "outlier detection on (3 consecutive errors in a 2-second interval eject a\n  backend for 60 seconds, at most 100% ejected, enforced at 80), request",
			absent:     []string{"Five consecutive errors"},
		},
		{
			name:       "one threshold set, the rest today's",
			set:        &derive.OutlierDetection{BaseEjectionSeconds: n(120)},
			wantLB:     []string{"    consecutive_errors           = 5\n", "      seconds = 120\n", "eject the region for 120 seconds"},
			wantReadme: "backend for 120 seconds, at most 50% ejected, enforced at 100)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := deriveFixture(t, "harbor", "placement.json")
			m.Placement.OutlierDetection = tt.set
			files, err := Render(m)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			text := map[string]string{}
			for _, f := range files {
				if !f.Root {
					text[f.Path] = string(f.Content)
				}
			}
			for _, w := range tt.wantLB {
				if !strings.Contains(text["load-balancer.tf"], w) {
					t.Errorf("load-balancer.tf lacks:\n%s", w)
				}
			}
			if !strings.Contains(text["README.md"], tt.wantReadme) {
				t.Errorf("README.md lacks:\n%s", tt.wantReadme)
			}
			for _, a := range tt.absent {
				if strings.Contains(text["load-balancer.tf"], a) {
					t.Errorf("load-balancer.tf carries %q", a)
				}
			}
		})
	}
}

// TestBuildArguments pins what a placement's build arguments render: each as a trigger
// substitution _BUILD_ARG_<NAME> in cloud-build.tf's substitutions map, from the stack's
// own resource or local, which both triggers and the substitutions output carry; the
// pipeline's image build waiting for a pull request's stack; the seeded Dockerfile
// declaring each in the browser stages; and the README naming them. A placement with none
// (beacon's) renders none of it.
func TestBuildArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fixture   string
		placement string
		// args replaces the placement's build arguments when set.
		args         map[string]string
		wantStack    []string
		wantPipeline []string
		wantDocker   []string
		wantReadme   []string
		absent       []string
	}{
		{
			name:      "harbor's two: the Firebase web API key and the project",
			fixture:   "harbor",
			placement: "placement.json",
			wantStack: []string{
				"    _BUILD_ARG_FIREBASE_API_KEY = nonsensitive(google_apikeys_key.firebase.key_string) # firebaseApiKey\n",
				"    _BUILD_ARG_PROJECT_ID       = local.project_id                                     # projectId\n  }\n",
				"substitutions = merge(local.substitutions, local.custom_substitutions, { _PR_NUMBER = \"\" })",
				"substitutions      = merge(local.substitutions, local.custom_substitutions, { _SEED = \"true\" })",
			},
			wantPipeline: []string{"    args: [deploy, pr-outputs]\n", "values the step before read from that stack (FIREBASE_API_KEY and PROJECT_ID)."},
			wantDocker:   []string{"ARG VERSION\n# The values the stack makes that placement.json's buildArguments declares, which the\n# build script reads the same way: FIREBASE_API_KEY and PROJECT_ID.\nARG FIREBASE_API_KEY\nARG PROJECT_ID\nENV CI=true\n"},
			wantReadme:   []string{"(`_BUILD_ARG_FIREBASE_API_KEY` and `_BUILD_ARG_PROJECT_ID`)", "This application declares\n  `FIREBASE_API_KEY` (`firebaseApiKey`) and `PROJECT_ID` (`projectId`).", "  - `hostname`: the service's canonical hostname in the environment.\n"},
		},
		{
			name:      "the database, the environment and the hostname, from the stack's own",
			fixture:   "harbor",
			placement: "placement.json",
			args:      map[string]string{"FIRESTORE_DB": "firestoreDatabase", "APP_ENV": "environment", "SITE_HOST": "hostname"},
			wantStack: []string{
				"    _BUILD_ARG_APP_ENV      = var.environment                          # environment\n",
				"    _BUILD_ARG_FIRESTORE_DB = google_firestore_database.firestore.name # firestoreDatabase\n",
				"    _BUILD_ARG_SITE_HOST    = local.hostnames[0]                       # hostname\n",
			},
			wantPipeline: []string{"values the step before read from that stack ("},
			wantDocker:   []string{"ARG APP_ENV\nARG FIRESTORE_DB\nARG SITE_HOST\nENV CI=true\n"},
			absent:       []string{"_BUILD_ARG_FIREBASE_API_KEY", "_BUILD_ARG_PROJECT_ID"},
		},
		{
			name:         "beacon's none: no substitution, the stack read after the image, no ARG",
			fixture:      "beacon",
			placement:    "placement-beacon.json",
			wantPipeline: []string{"    args: [deploy, pr-outputs]\n"},
			wantReadme:   []string{"(none: the placement declares no build argument)", "This application declares none."},
			absent:       []string{"_BUILD_ARG_FIREBASE", "\nARG FIREBASE_API_KEY\n", "the step before read from that stack", "waitFor"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := deriveFixture(t, tt.fixture, tt.placement)
			if tt.args != nil {
				m.Placement.BuildArguments = tt.args
			}
			files, err := Render(m)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			text := map[string]string{}
			for _, f := range files {
				key := f.Path
				if f.Root {
					key = "root/" + f.Path
				}
				text[key] = string(f.Content)
			}
			for file, wants := range map[string][]string{"cloud-build.tf": tt.wantStack, "root/cloudbuild.yaml": tt.wantPipeline, "root/Dockerfile": tt.wantDocker, "README.md": tt.wantReadme} {
				for _, w := range wants {
					if !strings.Contains(text[file], w) {
						t.Errorf("%s lacks:\n%s", file, w)
					}
				}
			}
			for _, a := range tt.absent {
				for _, file := range []string{"cloud-build.tf", "root/cloudbuild.yaml", "root/Dockerfile"} {
					if strings.Contains(text[file], a) {
						t.Errorf("%s carries %q", file, a)
					}
				}
			}
		})
	}
}

// TestBuildValueExprs holds the stack's expressions to the catalog: every value a
// placement may name has the expression the stack reads it from, and nothing else does.
func TestBuildValueExprs(t *testing.T) {
	t.Parallel()

	tests := derive.BuildValues()
	if len(tests) != len(buildValueExprs) {
		t.Errorf("the catalog has %d values and the stack %d expressions", len(tests), len(buildValueExprs))
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			if buildValueExprs[tt.Name] == "" {
				t.Errorf("no expression for %s", tt.Name)
			}
		})
	}
}

func TestAligned(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		pairs [][2]string
		sep   []string
		want  string
	}{
		{
			name:  "values in one column",
			pairs: [][2]string{{"a", "1"}, {"long", "2"}},
			want:  "  a    = 1\n  long = 2",
		},
		{
			name:  "a separator of its own",
			pairs: [][2]string{{"name-app", "site"}, {"name-migrate", "job"}},
			sep:   []string{"  "},
			want:  "  name-app      site\n  name-migrate  job",
		},
		{name: "nothing", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := aligned("  ", tt.pairs, tt.sep...); got != tt.want {
				t.Errorf("aligned() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJoinWith(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		items []string
		word  string
		want  string
	}{
		{name: "none", word: "or", want: ""},
		{name: "one", items: []string{"tst"}, word: "or", want: "tst"},
		{name: "two", items: []string{"stg", "prd"}, word: "and", want: "stg and prd"},
		{name: "three", items: []string{"tst", "stg", "prd"}, word: "or", want: "tst, stg, or prd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := joinWith(tt.items, tt.word); got != tt.want {
				t.Errorf("joinWith() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCommonPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		names []string
		want  string
	}{
		{name: "cut at the underscore", names: []string{"GOOGLE_CLOUD_SPANNER_PROJECT", "GOOGLE_CLOUD_SPANNER_INSTANCE_ID"}, want: "GOOGLE_CLOUD_SPANNER"},
		{name: "nothing shared", names: []string{"A_B", "C_D"}, want: ""},
		{name: "one name", names: []string{"APP_PORT"}, want: "APP"},
		{name: "no names", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := commonPrefix(tt.names...); got != tt.want {
				t.Errorf("commonPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSubstitutionNames(t *testing.T) {
	t.Parallel()

	names, err := SubstitutionNames()
	if err != nil {
		t.Fatalf("SubstitutionNames() error = %v", err)
	}
	for _, want := range []string{"_ENV", "_APP", "_PROJECT", "_SERVICES", "_MIGRATE_ENV", "_SEED", "_DEPLOYER_KEY_SECRET", "_BUILD_SECRETS"} {
		if !slices.Contains(names, want) {
			t.Errorf("SubstitutionNames() = %v, want it to contain %s", names, want)
		}
	}
	if slices.Contains(names, "_PR_NUMBER") {
		t.Errorf("SubstitutionNames() = %v: _PR_NUMBER is the trigger's, not the map's", names)
	}
}

// TestMigrationGrants proves the rendered stacks grant the deploy identity, which runs the
// migrate command on the build worker, database admin on the application's own database
// the way they pin its project roles, and the Firestore user role only where the migrate
// command constructs that level; that no migrate identity, template migrate job or job
// output remains and the pipeline's contract carries the command's variables in place of
// the job; that the pipeline hands the migration steps the version variable; and that the
// operations workflow reads the build's own log by step name under static job names.
func TestMigrationGrants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		dir           string
		wantFirestore bool
	}{
		{name: "harbor, whose migrate command reads Firestore", dir: "testdata/harbor", wantFirestore: true},
		{name: "beacon, whose migrate command does not", dir: "testdata/beacon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{}
			for _, name := range []string{"spanner.tf", "firestore.tf", "service-accounts.tf", "cloud-run.tf", "cloud-build.tf", "outputs.tf", "logging.tf", "locals.tf", "README.md", "root/cloudbuild.yaml", "root/.github/workflows/operations.yml"} {
				content, err := os.ReadFile(filepath.Join(tt.dir, name))
				if err != nil {
					t.Fatalf("ReadFile() error = %v", err)
				}
				files[name] = string(content)
			}
			present := map[string][]string{
				"spanner.tf":                            {`resource "google_spanner_database_iam_member" "deploy_admin" {`, "  role     = \"roles/spanner.databaseAdmin\"\n  member   = local.identities.deploy_identity_member"},
				"service-accounts.tf":                   {`resource "google_project_iam_member" "deploy_metrics" {`, "  count = local.is_pr ? 0 : 1\n", "  role    = \"roles/monitoring.metricWriter\"\n  member  = local.identities.deploy_identity_member"},
				"cloud-build.tf":                        {"_MIGRATE_ENV             = jsonencode(local.migrate_env)", "_MIGRATE_DATABASES       = jsonencode(local.migrate_databases)"},
				"locals.tf":                             {"migrate_env = merge(local.core_env, local.data_env", "migrate_databases = concat(\n    [\"projects/${local.instance.project}/instances/${local.instance.name}/databases/${local.database_name}\"],"},
				"logging.tf":                            {`resource.labels.build_trigger_id`, `"operations_reads_migrate_logs"`},
				"README.md":                             {"`roles/spanner.databaseAdmin` on the database only, for DDL", "`roles/monitoring.metricWriter` on the project for the Spanner\n  client's metrics", "The first apply after a render with this bedrock removes them"},
				"root/cloudbuild.yaml":                  {"args: [deploy, migrate, --preflight, --version-variable, APP_VERSION]", "args: [deploy, migrate, --version-variable, APP_VERSION]"},
				"root/.github/workflows/operations.yml": {`labels.build_step=~\"(Preflight|Run)Migrations\"`, "    name: restore the environment\n", "    name: run the release again\n", "    name: tear the environment down or destroy it\n", "    name: the migrations' operation\n"},
			}
			absent := map[string][]string{
				"spanner.tf":                            {"migrate_admin", "local.migrate_member"},
				"service-accounts.tf":                   {`"google_service_account" "migrate"`, "deploy_uses_migrate", "local.migrate_member"},
				"cloud-run.tf":                          {`"google_cloud_run_v2_job" "migrate"`},
				"cloud-build.tf":                        {"_MIGRATE_JOB", "_MIGRATE_LOGS"},
				"outputs.tf":                            {`output "migrate_job"`, "google_service_account.migrate"},
				"locals.tf":                             {"migrate_account", "job_env"},
				"logging.tf":                            {"deploy_reads_migrate_logs", "cloud_run_job"},
				"README.md":                             {"the migrate job", "Migrate job", "`_MIGRATE_JOB`", "`_MIGRATE_LOGS`"},
				"root/.github/workflows/operations.yml": {"MIGRATE_JOB", "inputs.environment }} to", "cloud_run_job", "migrate job"},
			}
			firestore := []string{`resource "google_project_iam_member" "firestore_deploy" {`, "  member  = local.identities.deploy_identity_member"}
			firestoreDatabase := `["projects/${local.project_id}/databases/${google_firestore_database.firestore.name}"],`
			if tt.wantFirestore {
				present["firestore.tf"] = firestore
				present["locals.tf"] = append(present["locals.tf"], firestoreDatabase)
			} else {
				absent["firestore.tf"] = firestore
				absent["locals.tf"] = []string{firestoreDatabase}
			}
			absent["firestore.tf"] = append(absent["firestore.tf"], "firestore_migrate", "local.migrate_member")
			for name, wants := range present {
				for _, want := range wants {
					if !strings.Contains(files[name], want) {
						t.Errorf("%s lacks %q", name, want)
					}
				}
			}
			for name, wants := range absent {
				for _, want := range wants {
					if strings.Contains(files[name], want) {
						t.Errorf("%s still carries %q", name, want)
					}
				}
			}
		})
	}
}

// TestReleaseBackupsAndRetention pins what the placement's release backups and Spanner
// retention render: the rollback trigger in the environments whose release builds keep
// a backup as of their cut (production unless the placement lists others), with the
// three substitutions a rollback carries, and the retention map the database reads its
// version retention period from (seven days unless the placement says otherwise).
func TestReleaseBackupsAndRetention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		backups       []string
		retention     map[string]string
		wantTriggers  []string
		wantLocals    []string
		wantSubs      []string
		absentTrigger []string
	}{
		{
			name:         "nothing set: production keeps release backups, every environment seven days",
			wantTriggers: []string{"resource \"google_cloudbuild_trigger\" \"rollback\" {\n  count = local.identities.repository_id == null || local.is_pr ? 0 : 1\n", "  name     = \"${local.name}-${local.primary_region_code}-${local.app}-rollback\"\n  disabled = true\n", "# The rollback trigger, in every environment"},
			wantLocals:   []string{"spanner_retention = { tst = \"7d\", stg = \"7d\", prd = \"7d\" }"},
			wantSubs:     []string{"    _ROLLBACK                = \"\"", "    _REASON                  = \"\"", "    _MAINTENANCE             = \"\""},
		},
		{
			name:         "two environments keep release backups and one keeps thirty-six hours",
			backups:      []string{"tst", "prd"},
			retention:    map[string]string{"tst": "36h"},
			wantTriggers: []string{"local.is_pr ? 0 : 1\n", "# The rollback trigger, in every environment"},
			wantLocals:   []string{"spanner_retention = { tst = \"36h\", stg = \"7d\", prd = \"7d\" }"},
		},
		{
			name:          "no environment keeps release backups",
			backups:       []string{},
			wantTriggers:  []string{"local.is_pr ? 0 : 1\n", "# The rollback trigger, in every environment"},
			absentTrigger: []string{"in prd alone", "_ROLLBACK_FROM"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := deriveFixture(t, "harbor", "placement.json")
			m.Placement.ReleaseBackups = tt.backups
			m.Placement.SpannerRetention = tt.retention
			files, err := Render(m)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			text := map[string]string{}
			for _, f := range files {
				if !f.Root {
					text[f.Path] = string(f.Content)
				}
			}
			for _, w := range tt.wantTriggers {
				if !strings.Contains(text["cloud-build.tf"], w) {
					t.Errorf("cloud-build.tf lacks:\n%s", w)
				}
			}
			for _, w := range tt.wantSubs {
				if !strings.Contains(text["cloud-build.tf"], w) {
					t.Errorf("cloud-build.tf lacks the substitution:\n%s", w)
				}
			}
			for _, w := range tt.wantLocals {
				if !strings.Contains(text["locals.tf"], w) {
					t.Errorf("locals.tf lacks:\n%s", w)
				}
			}
			for _, a := range tt.absentTrigger {
				if strings.Contains(text["cloud-build.tf"], a) {
					t.Errorf("cloud-build.tf carries %q", a)
				}
			}
		})
	}
}
