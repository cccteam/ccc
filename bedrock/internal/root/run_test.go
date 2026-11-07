package root_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/root"
)

// fakeBuilds is Cloud Build in the run tests: one build, described by its JSON.
type fakeBuilds struct {
	build string
}

func (b *fakeBuilds) open(context.Context) (deploy.Builds, error) {
	return b, nil
}

func (b *fakeBuilds) Get(context.Context, string, string, string) ([]byte, error) {
	return []byte(b.build), nil
}

func (b *fakeBuilds) ReadToken(context.Context, string) (string, error) {
	return "", errors.New("no read token in this test")
}

// applied is the world after the bootstrap's apply, with the placement written to a
// checkout and the run's clients: what a trigger's run starts from.
func applied(t *testing.T) (*world, *root.RunClients, string) {
	t.Helper()

	w := newWorld(t)
	w.run(t, root.Apply, true)
	dir := withRecord(t, t.TempDir())
	if err := w.p.Write(filepath.Join(dir, org.PlacementFile)); err != nil {
		t.Fatal(err)
	}
	rc := &root.RunClients{Secrets: func(context.Context) (deploy.Secrets, error) { return w.fake, nil }}

	return w, rc, dir
}

// build is a trigger's build, with the substitutions the steps pass.
func build(p *org.Placement, pr string) string {
	subs := map[string]string{"_BEDROCK_STEPS": org.StepsShape(p), "COMMIT_SHA": "0123456789abcdef0123456789abcdef01234567"}
	if pr != "" {
		subs["_PR_NUMBER"] = pr
	}
	data, _ := json.Marshal(map[string]any{"id": "b-1", "substitutions": subs})

	return string(data)
}

// TestPlanRun: the plan trigger's run reads the pull request's placement, says what the
// merge would change, and posts it once on the pull request as the deployer app, updating
// the same comment on a later push.
func TestPlanRun(t *testing.T) {
	t.Parallel()

	w, rc, dir := applied(t)
	// The pull request adds a domain.
	w.p.Domains["harbor.example"] = org.Domain{}
	if err := w.p.Write(filepath.Join(dir, org.PlacementFile)); err != nil {
		t.Fatal(err)
	}
	rc.Builds = (&fakeBuilds{build: build(w.p, "7")}).open
	req := &root.RunRequest{BuildID: "b-1", Project: w.p.Projects[org.RootProjectKey], Location: "us-central1", Dir: dir}
	var out bytes.Buffer
	if err := root.PlanRun(context.Background(), w.clients, rc, req, &out); err != nil {
		t.Fatalf("PlanRun() error = %v\n%s", err, out.String())
	}
	for _, want := range []string{
		"zones: the apply creates the zone imr-root-gbl-dns-harbor-example for harbor.example",
		"Posted the report on pull request #7 as the deployer app.",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	repo := w.srv.Repos[w.p.GithubOrganization+"/"+w.p.SourceRepo]
	comments := repo.Comments[7]
	if len(comments) != 1 || !strings.HasPrefix(comments[0].Body, "<!-- bedrock org plan -->\n### what the merge changes in the imr root organization") || !strings.Contains(comments[0].Body, "- zones: creates the zone imr-root-gbl-dns-harbor-example for harbor.example\n") {
		t.Fatalf("the pull request's comments: %+v", comments)
	}
	if strings.Contains(comments[0].Body, "Nothing:") {
		t.Errorf("the comment says nothing changes:\n%s", comments[0].Body)
	}

	// A later push: the same comment, updated.
	out.Reset()
	if err := root.PlanRun(context.Background(), w.clients, rc, req, &out); err != nil {
		t.Fatalf("PlanRun() again error = %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Updated the report on pull request #7 as the deployer app.") || len(repo.Comments[7]) != 1 {
		t.Errorf("the second run: %d comment(s)\n%s", len(repo.Comments[7]), out.String())
	}
}

// TestPlanRunRefusals: a build that did not start from the trigger, one without the pull
// request, and a hosting organization's checkout are refused.
func TestPlanRunRefusals(t *testing.T) {
	t.Parallel()

	w, rc, dir := applied(t)
	req := &root.RunRequest{BuildID: "b-1", Project: w.p.Projects[org.RootProjectKey], Location: "us-central1", Dir: dir}
	tests := []struct {
		name    string
		build   string
		req     *root.RunRequest
		wantErr string
	}{
		{name: "no steps substitution", build: `{"id":"b-1","substitutions":{"COMMIT_SHA":"abc","_PR_NUMBER":"7"}}`, req: req, wantErr: "no _BEDROCK_STEPS substitution: bedrock org plan runs from the plan trigger the bootstrap made"},
		{name: "no pull request", build: build(w.p, ""), req: req, wantErr: `_PR_NUMBER is "": the plan trigger runs on a pull request`},
		{name: "no build named", build: build(w.p, "7"), req: &root.RunRequest{Dir: dir}, wantErr: "is not set: bedrock org plan runs from the plan trigger of the root organization"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rc := &root.RunClients{Builds: (&fakeBuilds{build: tt.build}).open, Secrets: rc.Secrets}
			err := root.PlanRun(context.Background(), w.clients, rc, tt.req, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("PlanRun() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// TestApplyRun: the apply trigger's run takes the lock, makes the pushed placement so,
// leaves the marker and frees the lock; a lock held by another run is refused with what
// it says.
func TestApplyRun(t *testing.T) {
	t.Parallel()

	w, rc, dir := applied(t)
	w.p.Domains["harbor.example"] = org.Domain{}
	if err := w.p.Write(filepath.Join(dir, org.PlacementFile)); err != nil {
		t.Fatal(err)
	}
	rc.Builds = (&fakeBuilds{build: build(w.p, "")}).open
	req := &root.RunRequest{BuildID: "b-1", Project: w.p.Projects[org.RootProjectKey], Location: "us-central1", Dir: dir}
	var out bytes.Buffer
	if err := root.ApplyRun(context.Background(), w.clients, rc, req, &out); err != nil {
		t.Fatalf("ApplyRun() error = %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "zones: creates the zone imr-root-gbl-dns-harbor-example for harbor.example") {
		t.Errorf("output:\n%s", out.String())
	}
	if _, ok := w.fake.ZoneMap[w.p.Projects[org.RootProjectKey]]["imr-root-gbl-dns-harbor-example"]; !ok {
		t.Errorf("the zone was not made: %v", w.fake.ZoneMap)
	}
	if len(w.fake.Locked) != 0 {
		t.Errorf("the lock is still held: %v", w.fake.Locked)
	}
	marker, err := w.fake.Read(context.Background(), w.p.StateBucket, root.MarkerObject)
	if err != nil || !strings.Contains(string(marker), `"commit": "0123456789abcdef0123456789abcdef01234567"`) {
		t.Errorf("the marker: %s, %v", marker, err)
	}
	lockKey := w.p.StateBucket + "/" + root.LockObject
	if !slices.Contains(w.fake.Calls, "Lock("+w.p.StateBucket+", "+root.LockObject+")") || !slices.Contains(w.fake.Calls, "Unlock("+w.p.StateBucket+", "+root.LockObject+")") {
		t.Errorf("the lock was not taken and freed: %v", w.fake.Calls)
	}

	// Another run holds the lock.
	w.fake.Locked[lockKey] = []byte(`{"build":"b-0","commit":"fedcba9876543210","since":"2026-11-05T01:02:03Z"}`)
	err = root.ApplyRun(context.Background(), w.clients, rc, req, &bytes.Buffer{})
	if want := "another apply holds the lock gs://" + w.p.StateBucket + "/" + root.LockObject + " (build b-0 at fedcba9, since 2026-11-05T01:02:03Z): wait for it"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("ApplyRun() under a lock error = %v, want %q", err, want)
	}
}
