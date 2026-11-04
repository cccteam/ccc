package layers

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/bedrock/internal/found"
	"github.com/cccteam/ccc/bedrock/internal/found/foundtest"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// noSleep waits for nothing between the founding's tries.
func noSleep(context.Context, time.Duration) error {
	return nil
}

const (
	rootFixture = "../org/testdata/imr/placement.json"
	rootRepo    = "imr-root-infrastructure"
)

// rootCheckout is the root organization's repository with the record of rgb, pending.
func rootCheckout(t *testing.T) (string, *org.Placement) {
	t.Helper()
	root, err := org.ReadPlacement(rootFixture)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := root.Write(filepath.Join(dir, org.PlacementFile)); err != nil {
		t.Fatal(err)
	}
	h := &org.Placement{
		Prefix: "rgb", AppsDomain: "apps.rgb.example", Spanner: org.Spanner{Config: "nam10"},
		Regions:        []org.Region{{Name: "us-central1", Code: "uc1"}, {Name: "us-west3", Code: "uw3"}},
		TeamGroups:     map[string]string{"tst": "team-tst@imp.example", "stg": "team-stg@imp.example", "prd": "team-prd@imp.example"},
		ContactDomains: []string{"@imp.example"},
		Projects:       map[string]string{"boot": "rgb-boot-gbl-core-e5f6"},
		Labels:         map[string]string{"bedrock-lab": "true"},
	}
	if err := org.Fill(root, h); err != nil {
		t.Fatal(err)
	}
	path := org.FoundingPlacementPath(dir, "rgb")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := h.Write(path); err != nil {
		t.Fatal(err)
	}

	return dir, root
}

// TestApplyFounds: a push to a root organization whose layers are applied at the commit
// already founds the pending organization in the same run, as the founding identity
// with the infrastructure app's token, and says so; a run in a hosting organization, and
// a root run with nothing pending, found nothing.
func TestApplyFounds(t *testing.T) {
	t.Parallel()

	f := newFlow(t, pushSubs(), infraAppID, map[string]tofuRun{})
	f.srv.AddRepo(owner, rootRepo, &githubtest.Repo{
		Refs:     map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: pushSHA}},
		Ancestry: map[string][]string{pushSHA: {pushSHA, baseSHA}, baseSHA: {baseSHA}},
		Trees:    map[string]string{pushSHA: "treeroot"},
		Files:    map[string]string{"treeroot:placement.json": "{}"},
	})
	google := foundtest.New()
	f.store.objects = map[string]string{"applied/0-bootstrap": pushSHA, "applied/1-dns": pushSHA}
	dir, root := rootCheckout(t)
	clients := f.clients()
	clients.Google, clients.Sleep = google.Open, noSleep
	var out bytes.Buffer
	req := &Request{BuildID: "build-1", Project: "imr-boot-gbl-core-c3d4", Location: "us-central1", Dir: dir}
	if err := Apply(t.Context(), clients, req, &out); err != nil {
		t.Fatalf("Apply() error = %v; output:\n%s", err, out.String())
	}
	wantOut := []string{
		"No layer changes since the last applies: nothing to apply.\n",
		"=== founding rgb as " + root.FoundingIdentity() + " ===\n",
		"Created repository imp-example/rgb-infrastructure, private, default branch master.\n",
		"rgb is founded; its hand-off pull request records the folder: https://github.com/imp-example/imr-root-infrastructure/pull/1.\n",
	}
	for _, want := range wantOut {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if len(google.Identities) != 1 || google.Identities[0] != root.FoundingIdentity() {
		t.Errorf("Google opened as %v", google.Identities)
	}
	var marker found.Marker
	if err := json.Unmarshal([]byte(f.store.objects["applied/found/rgb"]), &marker); err != nil || marker.FoundedFrom != pushSHA {
		t.Errorf("marker: %v, %+v", err, marker)
	}
	// The infrastructure app's key was read for the token.
	if !strings.Contains(strings.Join(f.srv.Calls, "\n"), "POST /app/installations/77/access_tokens") {
		t.Errorf("no installation token minted:\n%s", strings.Join(f.srv.Calls, "\n"))
	}

	t.Run("nothing pending", func(t *testing.T) {
		t.Parallel()

		g := newFlow(t, pushSubs(), infraAppID, map[string]tofuRun{})
		g.srv.AddRepo(owner, rootRepo, &githubtest.Repo{Refs: map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: pushSHA}}, Ancestry: map[string][]string{pushSHA: {pushSHA}}})
		g.store.objects = map[string]string{"applied/0-bootstrap": pushSHA, "applied/1-dns": pushSHA}
		google := foundtest.New()
		dir, _ := rootCheckout(t)
		placement, _ := org.ReadPlacement(filepath.Join(dir, org.PlacementFile))
		placement.HostingOrganization("rgb").Folder = "900000000009"
		if err := placement.Write(filepath.Join(dir, org.PlacementFile)); err != nil {
			t.Fatal(err)
		}
		clients := g.clients()
		clients.Google, clients.Sleep = google.Open, noSleep
		var out bytes.Buffer
		if err := Apply(t.Context(), clients, &Request{BuildID: "build-1", Project: "imr-boot-gbl-core-c3d4", Location: "us-central1", Dir: dir}, &out); err != nil {
			t.Fatalf("Apply() error = %v; output:\n%s", err, out.String())
		}
		if len(google.Calls) != 0 || strings.Contains(out.String(), "founding") {
			t.Errorf("a run with nothing pending founded: %v\n%s", google.Calls, out.String())
		}
	})
}

// TestPlanPreviewsFounding: a pull request of a root organization says what its merge
// founds, before the plans.
func TestPlanPreviewsFounding(t *testing.T) {
	t.Parallel()

	f := newFlow(t, pullRequestSubs(), deployerAppID, map[string]tofuRun{})
	f.srv.AddRepo(owner, rootRepo, &githubtest.Repo{
		Refs:     map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: baseSHA}},
		Ancestry: map[string][]string{headSHA: {headSHA, baseSHA}, baseSHA: {baseSHA}},
		Changed:  map[string][]string{headSHA: {"organizations/rgb/placement.json", "placement.json"}},
	})
	dir, root := rootCheckout(t)
	var out bytes.Buffer
	err := Plan(t.Context(), f.clients(), &Request{BuildID: "build-1", Project: "imr-boot-gbl-core-c3d4", Location: "us-central1", Dir: dir}, &out)
	if err != nil {
		t.Fatalf("Plan() error = %v; output:\n%s", err, out.String())
	}
	want := "The merge founds rgb: folder rgb at the organization node, boot project rgb-boot-gbl-core-e5f6, repository imp-example/rgb-infrastructure or as its record names it (organizations/rgb/placement.json), as " + root.FoundingIdentity() + "; then the hand-off pull request.\n"
	if !strings.Contains(out.String(), want) || !strings.Contains(out.String(), "No layer changes in this pull request: nothing to plan.") {
		t.Errorf("output lacks the preview:\n%s", out.String())
	}
}
