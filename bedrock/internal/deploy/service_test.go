package deploy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// serviceDoc is a service as the API answers it, the parts the steps touch: serving its
// revision prev at 100 percent, at rest.
func serviceDoc(name string) map[string]any {
	prev := shortName(name) + "-00007-prev"

	return map[string]any{
		keyName:  name,
		"labels": map[string]any{"terraform": "true"},
		"template": map[string]any{
			"labels":     map[string]any{"terraform": "true"},
			"containers": []any{map[string]any{"image": "reg/harbor@sha256:old", "ports": []any{map[string]any{"containerPort": float64(8080)}}}},
		},
		"traffic":               []any{map[string]any{keyType: targetLatest, keyPercent: fullTraffic}},
		"trafficStatuses":       []any{map[string]any{keyType: targetLatest, keyRevision: prev, keyPercent: fullTraffic}},
		"terminalCondition":     map[string]any{keyType: "Ready", "state": conditionSucceeded},
		"latestReadyRevision":   name + "/revisions/" + prev,
		"latestCreatedRevision": name + "/revisions/" + prev,
	}
}

func TestDeploy(t *testing.T) {
	t.Parallel()

	const (
		central     = "projects/tst-project/locations/us-central1/services/harbor-app"
		west        = "projects/tst-project/locations/us-west3/services/harbor-app"
		environment = "export SKIP_DEPLOY=\"\"\nexport SERVICES=\"us-central1=harbor-app,us-west3=harbor-app\"\nexport IMAGE=\"reg/harbor\"\nexport IMAGE_DIGEST=\"sha256:abc\"\nexport REVISION_TAG=\"\"\n"
		build       = `{"id": "b-1", "substitutions": {"_PROJECT": "tst-project", "_ENV": "tst", "COMMIT_SHA": "deadbeef", "REPO_NAME": "harbor", "_PR_NUMBER": "7"}}`
	)
	pinned := []any{map[string]any{keyType: targetRevision, keyRevision: "harbor-app-00007-prev", keyPercent: fullTraffic}}
	tests := []struct {
		name string
		env  string
		// broken makes the first service inconsistent before the deploy.
		broken        bool
		wantOut       []string
		wantRevisions string
		// wantTraffic is the traffic the first service was deployed with; wantFields the
		// update masks of its patches, in order.
		wantTraffic []any
		wantFields  [][]string
		wantErr     string
	}{
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			wantOut: []string{tornDown},
		},
		{
			name:          "every region gets a revision without traffic",
			env:           environment,
			wantOut:       []string{"HEALTH CHECK PASSED: service [harbor-app] is consistent.", "Revision [harbor-app-00008-new] deployed to [harbor-app] in [us-central1]", "Revision [harbor-app-00008-new] deployed to [harbor-app] in [us-west3]"},
			wantRevisions: "us-central1,harbor-app,harbor-app-00008-new\nus-west3,harbor-app,harbor-app-00008-new\n",
			wantTraffic:   pinned,
			wantFields:    [][]string{nil},
		},
		{
			name:          "an inconsistent service is repaired first",
			env:           environment,
			broken:        true,
			wantOut:       []string{"PROBLEM: service [harbor-app] is in an inconsistent state; moving traffic back to [harbor-app-00007-prev]...", "FIX VERIFIED: traffic for [harbor-app] is consistent again.", "Revision [harbor-app-00008-new] deployed"},
			wantRevisions: "us-central1,harbor-app,harbor-app-00008-new\nus-west3,harbor-app,harbor-app-00008-new\n",
			wantTraffic:   pinned,
			wantFields:    [][]string{{"traffic"}, nil},
		},
		{
			name:          "a revision tag names the new revision",
			env:           strings.Replace(environment, `REVISION_TAG=""`, `REVISION_TAG="pr7"`, 1),
			wantRevisions: "us-central1,harbor-app,harbor-app-00008-new\nus-west3,harbor-app,harbor-app-00008-new\n",
			wantTraffic:   append(append([]any{}, pinned...), map[string]any{keyType: targetLatest, keyPercent: float64(0), keyTag: "pr7"}),
			wantFields:    [][]string{nil},
		},
		{
			name:    "a workspace without the digest is refused",
			env:     strings.Replace(environment, "export IMAGE_DIGEST=\"sha256:abc\"\n", "", 1),
			wantErr: "environment.sh names no image digest (IMAGE, IMAGE_DIGEST): the image build writes it",
		},
		{
			name:    "a service that is not region=name is refused",
			env:     strings.Replace(environment, "us-west3=harbor-app", "harbor-app", 1),
			wantErr: `SERVICES "harbor-app" is not region=name`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			run := newFakeRun(map[string]map[string]any{central: serviceDoc(central), west: serviceDoc(west)})
			if tt.broken {
				doc := run.resources[central]
				doc["terminalCondition"] = map[string]any{keyType: "Ready", "state": "CONDITION_FAILED"}
				doc["traffic"] = []any{map[string]any{keyType: targetLatest, keyPercent: fullTraffic}}
				doc["latestCreatedRevision"] = central + "/revisions/harbor-app-00007-broken"
			}
			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: build})
			var out strings.Builder
			err := Deploy(t.Context(), &Clients{Run: run.open}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Deploy() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out.String())
				}

				return
			}
			if err != nil {
				t.Fatalf("Deploy() error = %v; output:\n%s", err, out.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if tt.wantRevisions == "" {
				return
			}
			revisions, err := w.Revisions()
			if err != nil {
				t.Fatal(err)
			}
			var lines strings.Builder
			for _, r := range revisions {
				fmt.Fprintf(&lines, "%s,%s,%s\n", r.Region, r.Service, r.Revision)
			}
			if lines.String() != tt.wantRevisions {
				t.Errorf("revisions.txt = %q, want %q", lines.String(), tt.wantRevisions)
			}
			last := run.patches[central][len(run.patches[central])-1]
			if diff := cmp.Diff(tt.wantTraffic, last["traffic"]); diff != "" {
				t.Errorf("traffic mismatch (-want +got):\n%s", diff)
			}
			template, _ := last["template"].(map[string]any)
			container, _ := firstContainer(template)
			if container["image"] != "reg/harbor@sha256:abc" {
				t.Errorf("image = %v, want reg/harbor@sha256:abc", container["image"])
			}
			if text(last, "labels.gcb-build-id") != "b-1" || text(template, "labels.pr-number") != "7" || text(last, "labels.terraform") != "true" {
				t.Errorf("labels = %v / %v", last["labels"], template["labels"])
			}
			if diff := cmp.Diff(tt.wantFields, run.fieldsOf[central]); diff != "" {
				t.Errorf("update masks mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestShiftTraffic(t *testing.T) {
	t.Parallel()

	const (
		central     = "projects/tst-project/locations/us-central1/services/harbor-app"
		environment = "export SKIP_DEPLOY=\"\"\nexport SHIFT_TRAFFIC=\"true\"\n"
		build       = `{"id": "b-1", "substitutions": {"_PROJECT": "tst-project"}}`
		revisions   = "us-central1,harbor-app,harbor-app-00008-new\n"
	)
	tests := []struct {
		name  string
		env   string
		files map[string]string
		// tagged adds a tagged target on an older revision before the shift.
		tagged      bool
		wantOut     []string
		wantTraffic []any
		wantErr     string
	}{
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			files:   map[string]string{RevisionsFile: revisions},
			wantOut: []string{tornDown},
		},
		{
			name:        "every region moves to its new revision",
			env:         environment,
			files:       map[string]string{RevisionsFile: revisions},
			wantOut:     []string{"--- Shifting traffic in [us-central1] to [harbor-app-00008-new] ---"},
			wantTraffic: []any{map[string]any{keyType: targetRevision, keyRevision: "harbor-app-00008-new", keyPercent: fullTraffic}},
		},
		{
			name:        "tags on other revisions stay",
			env:         environment,
			files:       map[string]string{RevisionsFile: revisions},
			tagged:      true,
			wantTraffic: []any{map[string]any{keyType: targetRevision, keyRevision: "harbor-app-00008-new", keyPercent: fullTraffic}, map[string]any{keyType: targetRevision, keyRevision: "harbor-app-00005-old", keyPercent: float64(0), keyTag: "pr3"}},
		},
		{
			name:    "a pull-request revision keeps its tag only",
			env:     strings.Replace(environment, `SHIFT_TRAFFIC="true"`, `SHIFT_TRAFFIC=""`, 1),
			files:   map[string]string{RevisionsFile: revisions},
			wantOut: []string{"Leaving traffic unchanged: a pull-request revision is served under its tag only."},
		},
		{
			name:    "no revision deployed is refused",
			env:     environment,
			files:   map[string]string{},
			wantErr: "revisions.txt is missing or empty; no revision was deployed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			run := newFakeRun(map[string]map[string]any{central: serviceDoc(central)})
			if tt.tagged {
				run.resources[central]["traffic"] = []any{
					map[string]any{keyType: targetRevision, keyRevision: "harbor-app-00007-prev", keyPercent: fullTraffic},
					map[string]any{keyType: targetRevision, keyRevision: "harbor-app-00005-old", keyPercent: float64(0), keyTag: "pr3"},
				}
			}
			files := map[string]string{EnvironmentFile: tt.env, BuildFile: build}
			for name, content := range tt.files {
				files[name] = content
			}
			w := workspaceFiles(t, files)
			var out strings.Builder
			err := ShiftTraffic(t.Context(), &Clients{Run: run.open}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ShiftTraffic() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("ShiftTraffic() error = %v; output:\n%s", err, out.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if tt.wantTraffic == nil {
				if len(run.patches[central]) != 0 {
					t.Errorf("the service was patched: %v", run.patches[central])
				}

				return
			}
			last := run.patches[central][len(run.patches[central])-1]
			if diff := cmp.Diff(tt.wantTraffic, last["traffic"]); diff != "" {
				t.Errorf("traffic mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff([][]string{{"traffic"}}, run.fieldsOf[central]); diff != "" {
				t.Errorf("update masks mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
