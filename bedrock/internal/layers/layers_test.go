package layers

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// errorf is a run's own failure in tests.
func errorf(text string) error {
	return errors.New(text)
}

// TestLayersOf maps the files a change touches to the layers it changes, in layer order:
// a layer's .tf and .tfvars files and its lock file, every layer on the OpenTofu version,
// and nothing on a README, a file outside the layers or one under a layer's subdirectory.
func TestLayersOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		paths []string
		want  string
	}{
		{name: "nothing changed"},
		{name: "a layer's .tf file", paths: []string{"2-net/dns.tf"}, want: "2-net"},
		{name: "a layer's values", paths: []string{"2-env/terraform.tfvars"}, want: "2-env"},
		{name: "a layer's application values", paths: []string{"1-org/applications.auto.tfvars"}, want: "1-org"},
		{name: "a layer's lock file", paths: []string{"2-shr/.terraform.lock.hcl"}, want: "2-shr"},
		{name: "several layers, in layer order whatever the files' order", paths: []string{"2-env/locals.tf", "0-bootstrap/github.tf", "2-spn/instance.tf"}, want: "0-bootstrap,2-spn,2-env"},
		{name: "a README changes nothing", paths: []string{"1-org/README.md", "README.md"}},
		{name: "the placement alone changes nothing: render carries it into the layers", paths: []string{"placement.json"}},
		{name: "a file under a layer's subdirectory changes nothing", paths: []string{"2-env/.terraform/modules/x.tf"}},
		{name: "a file outside the layers changes nothing", paths: []string{"scripts/x.tf", ".github/workflows/layers.yml"}},
		{name: "the OpenTofu version changes every layer", paths: []string{".opentofu-version"}, want: "0-bootstrap,1-org,2-shr,2-spn,2-net,2-env"},
		{name: "a layer twice is one layer", paths: []string{"1-org/a.tf", "1-org/b.tf"}, want: "1-org"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := strings.Join(layersOf(tt.paths), ","); got != tt.want {
				t.Errorf("layersOf(%v) = %q, want %q", tt.paths, got, tt.want)
			}
		})
	}
}

// TestJoined joins words as a sentence does.
func TestJoined(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		words []string
		want  string
	}{
		{name: "none", want: ""},
		{name: "one", words: []string{"a"}, want: "a"},
		{name: "two", words: []string{"a", "b"}, want: "a and b"},
		{name: "three", words: []string{"a", "b", "c"}, want: "a, b and c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := joined(tt.words); got != tt.want {
				t.Errorf("joined(%v) = %q, want %q", tt.words, got, tt.want)
			}
		})
	}
}

// TestCommentBody writes a plan's comment: the marker line a later push finds it by, the
// heading, the identity and the commit with the verdict, and the plan folded under it,
// cut when long; a plan that did not run says so in the plan's place.
func TestCommentBody(t *testing.T) {
	t.Parallel()

	run := org.LayerRun{Name: "2-env tst", Plan: "acme-tst-gbl-plan@acme-tst-gbl-core-1a2b.iam.gserviceaccount.com"}
	tests := []struct {
		name    string
		result  *planResult
		summary string
		want    []string
		absent  []string
	}{
		{
			name:    "a plan that ran",
			result:  &planResult{run: run, ran: true, ok: true, text: succeededPlan},
			summary: "Plan: 1 to add, 0 to change, 0 to destroy.",
			want: []string{
				"<!-- layers: 2-env tst -->\n### plan of 2-env tst\n\nAs `acme-tst-gbl-plan@acme-tst-gbl-core-1a2b.iam.gserviceaccount.com` at abc123: Plan: 1 to add, 0 to change, 0 to destroy.\n\n<details><summary>the plan</summary>\n\n````text\nPlan: 1 to add, 0 to change, 0 to destroy.\n````\n\n</details>\n",
			},
		},
		{
			name:    "a plan that did not run",
			result:  &planResult{run: run},
			summary: planNotRun,
			want:    []string{"at abc123: The plan did not run; the log says why.\n", "````text\ntofu plan 2-env tst left no plan; its log says why.\n````\n"},
		},
		{
			name:    "a long plan is cut",
			result:  &planResult{run: run, ran: true, ok: true, text: strings.Repeat("x", commentCut+10)},
			summary: noSummary,
			want:    []string{"````text\n" + strings.Repeat("x", commentCut) + "\n... cut at 60000 characters; the run's log has all of it\n````\n"},
			absent:  []string{strings.Repeat("x", commentCut+1)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := commentBody(tt.result, tt.summary, "abc123")
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("the comment lacks:\n%s\ngot:\n%s", w, got)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("the comment still carries %q", a[:20])
				}
			}
			if !strings.HasPrefix(got, markerOf(run.Name)) {
				t.Errorf("the comment does not start with its marker: %q", got[:40])
			}
		})
	}
}

// TestFindComment finds the comment a run's marker starts, among the others.
func TestFindComment(t *testing.T) {
	t.Parallel()

	comments := []github.Comment{
		{ID: 1, Body: "a person's comment"},
		{ID: 2, Body: markerOf("1-org") + "\n### plan of 1-org\n"},
		{ID: 3, Body: markerOf("2-env tst") + "\n### plan of 2-env tst\n"},
	}
	tests := []struct {
		name   string
		marker string
		wantID int64
		wantOK bool
	}{
		{name: "the first run's", marker: markerOf("1-org"), wantID: 2, wantOK: true},
		{name: "the second run's", marker: markerOf("2-env tst"), wantID: 3, wantOK: true},
		{name: "a run without one", marker: markerOf("2-env stg")},
		{name: "a marker that is a prefix of another's is not it", marker: markerOf("2-env")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			id, ok := findComment(comments, tt.marker)
			if id != tt.wantID || ok != tt.wantOK {
				t.Errorf("findComment() = %d, %t; want %d, %t", id, ok, tt.wantID, tt.wantOK)
			}
		})
	}
}

// TestCheckSteps refuses a build without the steps' shape, and says when the shape is
// another bedrock's.
func TestCheckSteps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		subs    map[string]string
		wantErr string
		wantOut string
	}{
		{name: "this bedrock's shape", subs: map[string]string{stepsSub: org.StepsVersion}},
		{
			name:    "another shape",
			subs:    map[string]string{stepsSub: "0"},
			wantOut: "This run's steps are of shape 0, and this bedrock renders shape " + org.StepsVersion + ": the trigger's steps lag one apply behind the repository, and the next run of the trigger is the first on the shape this bedrock renders (0-bootstrap/README.md, \"The Cloud Build triggers\"). This run goes on with what the steps pass.\n",
		},
		{
			name:    "no shape: not the trigger's build",
			subs:    map[string]string{},
			wantErr: "no _BEDROCK_STEPS substitution: bedrock org plan runs from the plan trigger 0-bootstrap renders (0-bootstrap/cloud-build.tf), whose substitutions carry the steps' shape; this build did not start from it",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			r := &run{out: &out, subs: tt.subs}
			err := r.checkSteps(planCommand)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("checkSteps() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("checkSteps() error = %v", err)
			}
			if out.String() != tt.wantOut {
				t.Errorf("checkSteps() printed %q, want %q", out.String(), tt.wantOut)
			}
		})
	}
}

// TestRequestCheck refuses a request the trigger's step did not make.
func TestRequestCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     Request
		wantErr string
	}{
		{name: "whole", req: Request{BuildID: "b", Project: "p", Location: "l"}},
		{name: "no build", req: Request{Project: "p", Location: "l"}, wantErr: "BUILD_ID is not set: bedrock org apply runs from the apply trigger 0-bootstrap renders (0-bootstrap/cloud-build.tf), whose step passes the build's id, project and location to it"},
		{name: "no project", req: Request{BuildID: "b", Location: "l"}, wantErr: "PROJECT_ID is not set"},
		{name: "no location", req: Request{BuildID: "b", Project: "p"}, wantErr: "LOCATION is not set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.req.check(applyCommand)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("check() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("check() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// TestLastSummary is the last summary line an apply's output carries.
func TestLastSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "an apply", text: "Plan: 1 to add, 0 to change, 0 to destroy.\n...\nApply complete! Resources: 1 added, 0 changed, 0 destroyed.\n", want: "Apply complete! Resources: 1 added, 0 changed, 0 destroyed."},
		{name: "no changes", text: "No changes. Your infrastructure matches the configuration.\n", want: "No changes. Your infrastructure matches the configuration."},
		{name: "an error", text: "Plan: 1 to add, 0 to change, 0 to destroy.\nError: creating project: quota\n", want: "Error: creating project: quota"},
		{name: "nothing", text: "OpenTofu has been successfully initialized!\n", want: noSummary},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := lastSummary(tt.text); got != tt.want {
				t.Errorf("lastSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}
