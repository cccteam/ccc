package root_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/root"
	"github.com/cccteam/ccc/bedrock/internal/root/roottest"
)

// world is a root organization as the bootstrap leaves it before the engine's first run:
// the folder, the project, the bucket, the four secrets with their versions, the apps on
// GitHub with keys the secrets hold, and the repository.
type world struct {
	p       *org.Placement
	fake    *roottest.Fake
	srv     *githubtest.Server
	clients *root.Clients
}

func newWorld(t *testing.T) *world {
	t.Helper()

	p, err := org.ReadPlacement("../org/testdata/imr/placement.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := githubtest.New(t)
	fake := roottest.Standing(t, p, srv)
	clients := &root.Clients{GitHub: func(token string) *github.Client { return github.New(srv.URL, token) }}
	fake.Clients(clients)
	// The fixture records a hosting organization to create (rgb); the creation is the
	// create package's to test, and these runs create none.
	p.HostingOrganizations = slices.DeleteFunc(p.HostingOrganizations, func(h org.HostingOrganization) bool { return !h.Created() })

	return &world{p: p, fake: fake, srv: srv, clients: clients}
}

func (w *world) run(t *testing.T, mode root.Mode, bootstrap bool) (report *root.Report, printed string) {
	t.Helper()

	var out bytes.Buffer
	report, err := root.Run(context.Background(), w.clients, &root.Request{Placement: w.p, Mode: mode, Commit: "abc1234", GitHubToken: "test-token", Bootstrap: bootstrap}, &out)
	if err != nil {
		t.Fatalf("Run(%v) error = %v\n%s", mode, err, out.String())
	}

	return report, out.String()
}

func whats(r *root.Report) []string {
	var out []string
	for _, c := range r.Changes {
		out = append(out, c.Section+": "+c.What)
	}

	return out
}

func containsAll(t *testing.T, got []string, wants ...string) {
	t.Helper()

	for _, want := range wants {
		if !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, want) }) {
			t.Errorf("nothing says %q among:\n  %s", want, strings.Join(got, "\n  "))
		}
	}
}

// TestEngineFromNothing: a check before anything is made lists everything the apply
// makes, with the outer grants as findings; the bootstrap's apply makes all of it; the
// check after it is clean.
func TestEngineFromNothing(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	report, _ := w.run(t, root.Check, false)
	got := whats(report)
	containsAll(t, got,
		"identities: creates the identity imr-root-gbl-planner@imr-root-gbl-core-c3d4.iam.gserviceaccount.com",
		"identities: creates the identity imr-create-gbl-tofu@imr-root-gbl-core-c3d4.iam.gserviceaccount.com",
		"roles: creates the custom role secretPolicyAdmin in imr-root-gbl-core-c3d4",
		"secrets: moves the alias current of imr-root-gbl-github-infrastructure-key from no version to version 1",
		"secrets: moves the alias current of imr-root-gbl-github-release-key from no version to version 2",
		"secrets: moves the alias current of imr-root-gbl-github-token from no version to version 1",
		"secrets: sets the policy of imr-root-gbl-github-token: secretPolicyAdmin to serviceAccount:imp-boot-gbl-tofu@imp-boot-gbl-core-a1b2.iam.gserviceaccount.com, serviceAccount:imr-create-gbl-tofu@imr-root-gbl-core-c3d4.iam.gserviceaccount.com, secretmanager.secretAccessor to serviceAccount:service-100000000011@gcp-sa-cloudbuild.iam.gserviceaccount.com",
		"grants: grants serviceAccount:imr-root-gbl-applier@imr-root-gbl-core-c3d4.iam.gserviceaccount.com roles/dns.admin on project imr-root-gbl-core-c3d4",
		"grants: grants serviceAccount:imr-root-gbl-planner@imr-root-gbl-core-c3d4.iam.gserviceaccount.com roles/browser on folder 100",
		"grants: grants serviceAccount:imr-root-gbl-applier@imr-root-gbl-core-c3d4.iam.gserviceaccount.com roles/iam.serviceAccountTokenCreator on the identity imr-create-gbl-tofu@imr-root-gbl-core-c3d4.iam.gserviceaccount.com",
		"zones: creates the zone imr-root-gbl-dns-clientbay-example for clientbay.example",
		"zones: hands harbor.clientbay.example onward to ns-cloud-c1.googledomains.com., ns-cloud-c2.googledomains.com., ns-cloud-c3.googledomains.com., ns-cloud-c4.googledomains.com.",
		"zones: creates the zone imr-root-gbl-dns-zones-imp-example for zones.imp.example",
		"Cloud Build: creates the connection imr-root-uc1-github in us-central1 from the recorded authorization",
		"Cloud Build: links the repository https://github.com/imp-example/imr-root-infrastructure.git under the connection as imr-root-uc1-infrastructure-repo",
		"Cloud Build: creates the trigger imr-root-uc1-plan",
		"Cloud Build: creates the trigger imr-root-uc1-apply",
		"GitHub: sets the merge settings of imp-example/imr-root-infrastructure",
		"GitHub: rules the default branch of imp-example/imr-root-infrastructure: pull requests alone with 1 approving review(s), squash merges, the plan trigger's build required, the infrastructure app bypassing",
		"GitHub: sets the organization variable CI_LARGE_RUNNER to ci-large",
		"GitHub: makes the runner group applications, open to every repository",
		"GitHub: makes the larger runner ci-large (8-core, at most 10 at once) in the group applications",
	)
	containsAll(t, report.Findings,
		"the root organization lacks a grant the bootstrap makes: grants serviceAccount:imr-create-gbl-tofu@imr-root-gbl-core-c3d4.iam.gserviceaccount.com roles/resourcemanager.folderCreator at the organization node; an Organization Administrator makes it",
		"grants serviceAccount:imr-create-gbl-tofu@imr-root-gbl-core-c3d4.iam.gserviceaccount.com roles/billing.admin on billing account 012345-6789AB-CDEF01; a Billing Account Administrator of it makes it",
		"grants serviceAccount:imr-root-gbl-applier@imr-root-gbl-core-c3d4.iam.gserviceaccount.com roles/storage.objectUser on the markers and the lock in gs://imr-root-gbl-state-c3d4",
	)
	if len(report.Findings) != 3 {
		t.Errorf("findings = %q, want the three outer grants alone", report.Findings)
	}
	if len(w.fake.Calls) == 0 || slices.ContainsFunc(w.fake.Calls, func(c string) bool {
		return strings.HasPrefix(c, "Create") || strings.HasPrefix(c, "Set") || strings.HasPrefix(c, "Grant")
	}) {
		t.Errorf("a check made something: %v", w.fake.Calls)
	}

	applied, out := w.run(t, root.Apply, true)
	if applied.Applied != len(applied.Changes) || applied.Applied < 20 {
		t.Errorf("Apply made %d of %d changes:\n%s", applied.Applied, len(applied.Changes), out)
	}
	if len(applied.Findings) != 0 {
		t.Errorf("the bootstrap's apply has findings: %q", applied.Findings)
	}
	if !strings.Contains(out, "grants: grants serviceAccount:imr-create-gbl-tofu@imr-root-gbl-core-c3d4.iam.gserviceaccount.com roles/billing.admin on billing account 012345-6789AB-CDEF01") {
		t.Errorf("the bootstrap did not grant the creating identity billing administration:\n%s", out)
	}

	clean, out := w.run(t, root.Check, false)
	if !clean.Clean() {
		t.Errorf("the check after the apply is not clean:\n%s\nchanges: %v\nfindings: %v", out, whats(clean), clean.Findings)
	}
	var buf bytes.Buffer
	clean.Write(&buf, root.Check)
	if want := "The root organization stands as placement.json says: nothing to change.\n"; !strings.Contains(buf.String(), want) {
		t.Errorf("Write() = %q, want %q", buf.String(), want)
	}
	zones, _ := w.fake.Zones(context.Background(), w.p.Projects[org.RootProjectKey])
	if len(zones) != 3 {
		t.Errorf("zones = %d, want 3", len(zones))
	}
	records, _ := w.fake.RecordSets(context.Background(), w.p.Projects[org.RootProjectKey], "imr-root-gbl-dns-clientbay-example")
	var ns []string
	for _, r := range records {
		if r.Type == "NS" && r.Name != "clientbay.example." {
			ns = append(ns, r.Name)
		}
	}
	if want := []string{"beacon.clientbay.example.", "harbor.clientbay.example."}; !slices.Equal(ns, want) {
		t.Errorf("delegations = %v, want %v", ns, want)
	}
}

// TestEngineReadsNothingAboveTheFolder: a trigger's run holds no read of the organization
// node's or the billing account's IAM policy; it notes the two grants it did not compare
// and is clean otherwise. The bootstrap refused the same read stops, as does a run refused
// for another reason.
func TestEngineReadsNothingAboveTheFolder(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	if _, out := w.run(t, root.Apply, true); strings.Contains(out, "finding") {
		t.Fatalf("the first apply:\n%s", out)
	}
	w.fake.Refuse["OrganizationPolicy"] = status.Error(codes.PermissionDenied, "Permission 'resourcemanager.organizations.getIamPolicy' denied on resource '//cloudresourcemanager.googleapis.com/organizations/123456789012' (or it may not exist).")
	w.fake.Refuse["BillingPolicy"] = status.Error(codes.PermissionDenied, "The caller does not have permission")
	report, _ := w.run(t, root.Check, false)
	if !report.Clean() {
		t.Errorf("not clean: changes %v, findings %q", whats(report), report.Findings)
	}
	want := []string{
		"the creating identity's Folder Creator at the organization node was not compared: this run holds no read of the organization node's IAM policy, as the triggers' runners hold their roles under the organization's folder alone; the bootstrap made the grant and compares it, and so does bedrock org check run by an Organization Administrator",
		"the creating identity's Billing Account Administrator on billing account 012345-6789AB-CDEF01 was not compared: this run holds no read of the billing account's IAM policy, as the triggers' runners hold their roles under the organization's folder alone; the bootstrap made the grant and compares it, and so does bedrock org check run by a Billing Account Administrator of it",
	}
	if !slices.Equal(report.Notes, want) {
		t.Errorf("notes = %q, want %q", report.Notes, want)
	}
	var buf bytes.Buffer
	report.Write(&buf, root.Check)
	if !strings.Contains(buf.String(), "note: "+want[0]+"\n") || !strings.HasSuffix(buf.String(), "The root organization stands as placement.json says: nothing to change.\n") {
		t.Errorf("Write() = %q", buf.String())
	}
	var out bytes.Buffer
	if _, err := root.Run(context.Background(), w.clients, &root.Request{Placement: w.p, Mode: root.Check, Commit: "abc1234", GitHubToken: "test-token", Bootstrap: true}, &out); err == nil || !strings.Contains(err.Error(), "resourcemanager.organizations.getIamPolicy") {
		t.Errorf("the bootstrap refused the read: err = %v", err)
	}
	w.fake.Refuse["OrganizationPolicy"] = status.Error(codes.Unavailable, "try again")
	if _, err := root.Run(context.Background(), w.clients, &root.Request{Placement: w.p, Mode: root.Check, Commit: "abc1234", GitHubToken: "test-token"}, &out); err == nil || !strings.Contains(err.Error(), "try again") {
		t.Errorf("a refusal for another reason: err = %v", err)
	}
}

// TestEngineDrift: what a person or a change moved is listed and put back: an alias
// moved by hand, a delegation's name servers changed, a role granted beyond the list, a
// label taken out of the placement, the ruleset's bypass actor removed and the runner's
// variable changed; a check lists them, an apply repairs them, the check after is clean.
func TestEngineDrift(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	if _, out := w.run(t, root.Apply, true); strings.Contains(out, "finding") {
		t.Fatalf("the first apply:\n%s", out)
	}
	ctx := context.Background()
	project := w.p.Projects[org.RootProjectKey]
	if err := w.fake.SetSecretAliases(ctx, "projects/"+project+"/secrets/imr-root-gbl-github-release-key", map[string]string{org.KeyAlias: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := w.fake.ChangeRecordSets(ctx, project, "imr-root-gbl-dns-clientbay-example",
		[]root.RecordSet{{Name: "harbor.clientbay.example.", Type: "NS", TTL: 3600, Data: []string{"ns-cloud-x1.googledomains.com.", "ns-cloud-x2.googledomains.com.", "ns-cloud-x3.googledomains.com.", "ns-cloud-x4.googledomains.com."}}},
		[]root.RecordSet{{Name: "harbor.clientbay.example.", Type: "NS"}}); err != nil {
		t.Fatal(err)
	}
	w.fake.SeedGrant("projects/"+project, "roles/owner", "serviceAccount:"+w.p.PlannerIdentity())
	delete(w.p.Domains["clientbay.example"].Labels, "beacon")
	gh := github.New(w.srv.URL, "test-token")
	rulesets, _ := gh.Rulesets(ctx, w.p.GithubOrganization, w.p.SourceRepo)
	rs, _ := gh.Ruleset(ctx, w.p.GithubOrganization, w.p.SourceRepo, rulesets[0].ID)
	rs.BypassActors = []github.BypassActor{}
	if err := gh.UpdateRuleset(ctx, w.p.GithubOrganization, w.p.SourceRepo, rs.ID, rs); err != nil {
		t.Fatal(err)
	}
	if err := gh.UpdateOrgVariable(ctx, w.p.GithubOrganization, github.Variable{Name: "CI_LARGE_RUNNER", Value: "ci-tiny", Visibility: "all"}); err != nil {
		t.Fatal(err)
	}

	report, _ := w.run(t, root.Check, false)
	got := whats(report)
	containsAll(t, got,
		"secrets: moves the alias current of imr-root-gbl-github-release-key from version 1 to version 2",
		"zones: in the zone imr-root-gbl-dns-clientbay-example: hands harbor.clientbay.example onward to ns-cloud-c1.googledomains.com., ns-cloud-c2.googledomains.com., ns-cloud-c3.googledomains.com., ns-cloud-c4.googledomains.com.; takes back beacon.clientbay.example",
		"grants: takes roles/owner off serviceAccount:imr-root-gbl-planner@imr-root-gbl-core-c3d4.iam.gserviceaccount.com on project imr-root-gbl-core-c3d4",
		"GitHub: brings the default branch's ruleset of imp-example/imr-root-infrastructure to the placement: bypass actors",
		"GitHub: sets the organization variable CI_LARGE_RUNNER to ci-large (was ci-tiny)",
	)
	if len(got) != 5 {
		t.Errorf("changes = %d, want 5:\n  %s", len(got), strings.Join(got, "\n  "))
	}
	if len(report.Findings) != 0 {
		t.Errorf("findings = %q", report.Findings)
	}
	if _, out := w.run(t, root.Apply, false); strings.Contains(out, "finding") {
		t.Errorf("the repair:\n%s", out)
	}
	if clean, out := w.run(t, root.Check, false); !clean.Clean() {
		t.Errorf("the check after the repair is not clean:\n%s\n%v\n%v", out, whats(clean), clean.Findings)
	}
}

// TestEngineFindings: a zone whose domain the placement no longer lists, an app that
// lacks a declared permission and an alias pointing at a version nothing pins are
// findings, not changes; the apply never deletes a zone.
func TestEngineFindings(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	w.run(t, root.Apply, true)
	ctx := context.Background()
	project := w.p.Projects[org.RootProjectKey]
	delete(w.p.Domains, "zones.imp.example")
	deployer := w.srv.Apps[w.p.GithubDeployerAppID]
	delete(deployer.Record.Permissions, org.PermissionDeployments)
	deployer.Record.Permissions[org.PermissionContents] = org.LevelWrite
	w.p.GithubReleaseKeyVersion = ""

	report, _ := w.run(t, root.Check, false)
	containsAll(t, report.Findings,
		"the zone imr-root-gbl-dns-zones-imp-example serves zones.imp.example, which placement.json no longer lists (domains): the apply deletes no zone; once nothing delegates the domain to it, delete it by hand (gcloud dns managed-zones delete imr-root-gbl-dns-zones-imp-example --project imr-root-gbl-core-c3d4)",
		"the deployer app lacks the Deployments permission (write)",
		"the deployer app holds the Contents permission (write), which it does not need: take it off on the app's settings page",
		"the alias current of imr-root-gbl-github-release-key points at version 2, and placement.json pins no version for it",
	)
	if len(report.Changes) != 0 {
		t.Errorf("changes = %v, want none", whats(report))
	}
	containsAll(t, report.Notes, "the release app is not checked: placement.json records no App ID or no pinned key version for it (github-release-key)")
	w.run(t, root.Apply, false)
	zones, _ := w.fake.Zones(ctx, project)
	if len(zones) != 3 {
		t.Errorf("the apply deleted a zone: %d left", len(zones))
	}
}

// TestEngineRefusals: a hosting organization's placement, and a root organization
// without its project recorded, are refused before anything is read.
func TestEngineRefusals(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	hosting, err := org.ReadPlacement("../org/testdata/imp/placement.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Run(context.Background(), w.clients, &root.Request{Placement: hosting, Mode: root.Check}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "the imp organization is a hosting organization: the engine runs a root organization") {
		t.Errorf("a hosting placement: error = %v", err)
	}
	unrecorded := *w.p
	unrecorded.Projects = map[string]string{}
	if _, err := root.Run(context.Background(), w.clients, &root.Request{Placement: &unrecorded, Mode: root.Check}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "placement.json records no root project (projects.root): bedrock org bootstrap records it") {
		t.Errorf("no project: error = %v", err)
	}
}

// TestReportComment: the plan's comment says Nothing only when the merge changes nothing
// and creates nothing; a pending creation is listed on its own.
func TestReportComment(t *testing.T) {
	t.Parallel()

	p, err := org.ReadPlacement("../org/testdata/imr/placement.json")
	if err != nil {
		t.Fatal(err)
	}
	report := &root.Report{}
	comment := report.Comment("0123456789abcdef", p)
	if strings.Contains(comment, "Nothing:") || !strings.Contains(comment, "- creations: creates the hosting organization rgb as the creating identity") {
		t.Errorf("with a pending creation:\n%s", comment)
	}
	p.HostingOrganizations = slices.DeleteFunc(p.HostingOrganizations, func(h org.HostingOrganization) bool { return !h.Created() })
	comment = report.Comment("0123456789abcdef", p)
	if !strings.Contains(comment, "Nothing: the root organization stands as placement.json says.") || strings.Contains(comment, "creations:") {
		t.Errorf("with nothing pending:\n%s", comment)
	}
}
