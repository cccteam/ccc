package create_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cccteam/ccc/bedrock/internal/create"
	"github.com/cccteam/ccc/bedrock/internal/create/createtest"
	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

const (
	rootFixture   = "../org/testdata/imr/placement.json"
	owner         = "imp-example"
	rootRepo      = "imr-root-infrastructure"
	baseSHA       = "base1"
	rootCommit    = "push7"
	creatingEmail = "imr-create-gbl-tofu@imr-boot-gbl-core-c3d4.iam.gserviceaccount.com"
	bootIdentity  = "serviceAccount:rgb-boot-gbl-tofu@rgb-boot-gbl-core-e5f6.iam.gserviceaccount.com"
	policyAdmin   = "projects/imr-boot-gbl-core-c3d4/roles/secretPolicyAdmin"
)

// record is the hosting organization rgb as a person records it in the root
// organization's repository, filled.
func record(t *testing.T, root *org.Placement) *org.Placement {
	t.Helper()
	h := &org.Placement{
		Prefix: "rgb", AppsDomain: "apps.rgb.example", Spanner: org.Spanner{Config: "nam10"},
		Regions:        []org.Region{{Name: "us-central1", Code: "uc1"}, {Name: "us-west3", Code: "uw3"}},
		TeamGroups:     map[string]string{"tst": "team-tst@imp.example", "stg": "team-stg@imp.example", "prd": "team-prd@imp.example"},
		ContactDomains: []string{"@imp.example"},
		Projects:       map[string]string{"boot": "rgb-boot-gbl-core-e5f6"},
		Labels:         map[string]string{"bedrock-lab": "true"},
	}
	if err := org.Fill(root, h); err != nil {
		t.Fatalf("Fill() error = %v", err)
	}

	return h
}

// checkout is the root organization's repository: its placement and the record of rgb.
func checkout(t *testing.T) (string, *org.Placement) {
	t.Helper()
	root, err := org.ReadPlacement(rootFixture)
	if err != nil {
		t.Fatalf("ReadPlacement() error = %v", err)
	}
	dir := t.TempDir()
	if err := root.Write(filepath.Join(dir, org.PlacementFile)); err != nil {
		t.Fatal(err)
	}
	h := record(t, root)
	path := org.RecordPath(dir, "rgb")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := h.Write(path); err != nil {
		t.Fatal(err)
	}

	return dir, root
}

// fakeRunner records the programs run and answers success.
type fakeRunner struct {
	mu          sync.Mutex
	ran         []deploy.Command
	outputsRead []deploy.Command
}

func (r *fakeRunner) Run(_ context.Context, c deploy.Command, _ io.Writer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ran = append(r.ran, c)

	return nil
}

// Output answers tofu output -json with 1-org's outputs: six projects and four folders.
func (r *fakeRunner) Output(_ context.Context, c deploy.Command, _ io.Writer) ([]byte, error) {
	if c.Args[0] != "output" {
		return nil, nil
	}
	r.mu.Lock()
	r.outputsRead = append(r.outputsRead, c)
	r.mu.Unlock()

	data, err := json.Marshal(orgOutputs())
	if err != nil {
		return nil, fmt.Errorf("json.Marshal(): %w", err)
	}

	return data, nil
}

// orgOutputs are 1-org's outputs as tofu output -json prints them.
func orgOutputs() map[string]any {
	ids, numbers := map[string]string{}, map[string]string{}
	for i, key := range []string{"shr", "net", "spn", "tst", "stg", "prd"} {
		ids[key] = "rgb-" + key + "-gbl-core-e5f6"
		numbers[key] = "10000000010" + strconv.Itoa(i+1)
	}
	folders := map[string]string{"shared": "900000000011", "tst": "900000000012", "stg": "900000000013", "prd": "900000000014"}
	value := func(v any) map[string]any { return map[string]any{"value": v} }

	return map[string]any{"project_ids": value(ids), "project_numbers": value(numbers), "folder_ids": value(folders)}
}

// fakeStore is the root organization's state bucket.
type fakeStore struct {
	mu      sync.Mutex
	objects map[string]string
}

func (s *fakeStore) Write(_ context.Context, _, object string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[object] = string(data)

	return nil
}

func (s *fakeStore) List(context.Context, string, string) ([]string, error) {
	return nil, nil
}

func (s *fakeStore) Read(_ context.Context, _, object string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[object]
	if !ok {
		return nil, os.ErrNotExist
	}

	return []byte(data), nil
}

func (s *fakeStore) Delete(context.Context, string, string) error {
	return nil
}

func (*fakeStore) Close() error {
	return nil
}

type flow struct {
	google *createtest.FakeGoogle
	runner *fakeRunner
	store  *fakeStore
	srv    *githubtest.Server
	repo   *githubtest.Repo
}

func newFlow(t *testing.T) *flow {
	t.Helper()
	srv := githubtest.New(t)
	repo := srv.AddRepo(owner, rootRepo, &githubtest.Repo{
		Refs:     map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: baseSHA}},
		Ancestry: map[string][]string{baseSHA: {baseSHA}},
		Trees:    map[string]string{baseSHA: "tree0"},
		Files:    map[string]string{"tree0:placement.json": "{}", "tree0:README.md": "# imr\n"},
	})

	return &flow{google: createtest.New(), runner: &fakeRunner{}, store: &fakeStore{objects: map[string]string{}}, srv: srv, repo: repo}
}

func (f *flow) clients() *create.Clients {
	return &create.Clients{
		Google: f.google.Open,
		GitHub: func(token string) *github.Client { return github.New(f.srv.URL, token) },
		Exec:   f.runner,
		Store:  f.store,
		Sleep:  func(context.Context, time.Duration) error { return nil },
	}
}

func request(root *org.Placement, dir string) *create.Request {
	return &create.Request{Root: root, RootDir: dir, Record: *root.HostingOrganization("rgb"), Token: "test-token", Commit: rootCommit}
}

// TestRun creates rgb from the root organization imr: the seed by API (both folders, the
// boot project with its billing link and APIs, the boot identity, the bucket, the grants
// on the folder and on the four shared secrets), the repository with the rendered
// foundation pushed on its initial commit (the seeded tfvars carrying the folder ids and
// the boot project), the first apply as the creating identity against the new bucket,
// the creating identity's withdrawal, the marker and the hand-off pull request; and a
// second run makes nothing and opens nothing.
func TestRun(t *testing.T) {
	t.Parallel()

	f := newFlow(t)
	dir, root := checkout(t)
	var out bytes.Buffer
	result, err := create.Run(t.Context(), f.clients(), request(root, dir), &out)
	if err != nil {
		t.Fatalf("Run() error = %v; output:\n%s", err, out.String())
	}
	orgFolder := f.google.Folders["organizations/123456789012|rgb"]
	if orgFolder == "" {
		t.Fatalf("no folder rgb made under the organization; made: %v", f.google.Made())
	}
	tfFolder := f.google.Folders["folders/"+orgFolder+"|terraform"]
	wantMade := []string{
		"account rgb-boot-gbl-tofu@rgb-boot-gbl-core-e5f6.iam.gserviceaccount.com",
		"bucket rgb-boot-gbl-state-e5f6",
		"folder folders/" + orgFolder + "|terraform=" + tfFolder,
		"folder organizations/123456789012|rgb=" + orgFolder,
		"project rgb-boot-gbl-core-e5f6 (100000000001, billing 012345-6789AB-CDEF01, 8 APIs)",
	}
	if got := f.google.Made(); !slices.Equal(got, wantMade) {
		t.Errorf("made %v, want %v", got, wantMade)
	}
	if want := []string{creatingEmail}; !slices.Equal(f.google.Identities, want) {
		t.Errorf("Google opened as %v, want %v", f.google.Identities, want)
	}
	wantGrants := []string{
		"folders/" + orgFolder + " roles/resourcemanager.projectCreator serviceAccount:" + creatingEmail,
		"folders/" + orgFolder + " roles/orgpolicy.policyAdmin serviceAccount:" + creatingEmail,
		"folders/" + orgFolder + " roles/resourcemanager.tagAdmin serviceAccount:" + creatingEmail,
		"folders/" + orgFolder + " roles/cloudbuild.builds.editor " + bootIdentity,
		"folders/" + orgFolder + " roles/serviceusage.serviceUsageAdmin " + bootIdentity,
		"folders/" + orgFolder + " roles/resourcemanager.folderAdmin user:seed@imp.example",
		"projects/imr-boot-gbl-core-c3d4/secrets/imr-boot-gbl-github-infrastructure-key " + policyAdmin + " " + bootIdentity,
		"projects/imr-boot-gbl-core-c3d4/secrets/imr-boot-gbl-github-release-key " + policyAdmin + " " + bootIdentity,
		"projects/imr-boot-gbl-core-c3d4/locations/us-central1/secrets/github-github-oauthtoken-fedcba " + policyAdmin + " " + bootIdentity,
	}
	for _, want := range wantGrants {
		if !slices.Contains(f.google.Grants, want) {
			t.Errorf("grants lack %q:\n%s", want, strings.Join(f.google.Grants, "\n"))
		}
	}
	if n := len(f.google.Grants); n != 3+len(org.BootLayerRoles)+1+4 {
		t.Errorf("%d grants, want %d (the three roles for the first applies, the boot roles, Folder Admin, four secrets)", n, 3+len(org.BootLayerRoles)+1+4)
	}
	creator := " serviceAccount:" + creatingEmail + " roles/resourcemanager.folderAdmin+roles/resourcemanager.folderEditor"
	wantRevoked := []string{
		"folders/900000000014" + creator, "folders/900000000011" + creator, "folders/900000000013" + creator, "folders/900000000012" + creator,
		"projects/rgb-shr-gbl-core-e5f6 serviceAccount:" + creatingEmail + " roles/owner",
		"projects/rgb-net-gbl-core-e5f6 serviceAccount:" + creatingEmail + " roles/owner",
		"projects/rgb-spn-gbl-core-e5f6 serviceAccount:" + creatingEmail + " roles/owner",
		"projects/rgb-tst-gbl-core-e5f6 serviceAccount:" + creatingEmail + " roles/owner",
		"projects/rgb-stg-gbl-core-e5f6 serviceAccount:" + creatingEmail + " roles/owner",
		"projects/rgb-prd-gbl-core-e5f6 serviceAccount:" + creatingEmail + " roles/owner",
		"folders/" + tfFolder + creator,
		"projects/rgb-boot-gbl-core-e5f6 serviceAccount:" + creatingEmail + " roles/owner",
		"folders/" + orgFolder + creator + "+roles/resourcemanager.projectCreator+roles/orgpolicy.policyAdmin+roles/resourcemanager.tagAdmin",
	}
	if !slices.Equal(f.google.Revoked, wantRevoked) {
		t.Errorf("revoked:\n%s\nwant:\n%s", strings.Join(f.google.Revoked, "\n"), strings.Join(wantRevoked, "\n"))
	}
	// The first applies, as the creating identity, in the rendered 0-bootstrap and 1-org,
	// the second with the infrastructure app's token, and 1-org's outputs read after it.
	if len(f.runner.ran) != 4 {
		t.Fatalf("%d programs ran, want tofu init and apply twice: %v", len(f.runner.ran), f.runner.ran)
	}
	init, apply := f.runner.ran[0], f.runner.ran[1]
	if want := "tofu init -input=false -no-color -backend-config=prefix=0-bootstrap -backend-config=impersonate_service_account=" + creatingEmail; init.String() != want {
		t.Errorf("init = %q, want %q", init.String(), want)
	}
	orgInit, orgApply := f.runner.ran[2], f.runner.ran[3]
	if want := "tofu init -input=false -no-color -backend-config=prefix=1-org -backend-config=impersonate_service_account=" + creatingEmail; orgInit.String() != want || filepath.Base(orgInit.Dir) != "1-org" {
		t.Errorf("1-org init = %q in %s, want %q", orgInit.String(), orgInit.Dir, want)
	}
	if filepath.Base(orgApply.Dir) != "1-org" || !slices.Contains(orgApply.Env, "GITHUB_TOKEN=test-token") || !slices.Contains(orgApply.Env, "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT="+creatingEmail) {
		t.Errorf("1-org apply = %q in %s with %v", orgApply.String(), orgApply.Dir, orgApply.Env)
	}
	if len(f.runner.outputsRead) != 1 || f.runner.outputsRead[0].String() != "tofu output -no-color -json" || filepath.Base(f.runner.outputsRead[0].Dir) != "1-org" {
		t.Errorf("outputs read: %v", f.runner.outputsRead)
	}
	if want := "tofu apply -input=false -no-color -auto-approve"; apply.String() != want || filepath.Base(apply.Dir) != "0-bootstrap" || !slices.Contains(apply.Env, "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT="+creatingEmail) {
		t.Errorf("apply = %q in %s with %v", apply.String(), apply.Dir, apply.Env)
	}
	// The repository and its first push.
	if want := []string{owner + "/rgb-infrastructure"}; !slices.Equal(f.srv.Created, want) {
		t.Errorf("repositories created %v, want %v", f.srv.Created, want)
	}
	hosting := f.srv.Repos[owner+"/rgb-infrastructure"]
	tree := hosting.Trees[hosting.Refs["refs/heads/master"].SHA]
	wantFiles := map[string][]string{
		"placement.json":               {`"prefix": "rgb"`, `"boot": "100000000001"`, `"organization": "imr"`, `"stateBucket": "rgb-boot-gbl-state-e5f6"`, `"net": "rgb-net-gbl-core-e5f6"`, `"prd": "100000000106"`},
		"0-bootstrap/cloud-build.tf":   {"100000000104"},
		"0-bootstrap/terraform.tfvars": {`boot_project_id        = "rgb-boot-gbl-core-e5f6"`, `organization_folder_id = "` + orgFolder + `"`, `terraform_folder_id    = "` + tfFolder + `"`},
		"0-bootstrap/initialize.tf":    {`bucket = "rgb-boot-gbl-state-e5f6"`},
		"1-org/terraform.tfvars":       {`boot_project_id     = "rgb-boot-gbl-core-e5f6"`},
		"README.md":                    {"# rgb"},
	}
	for path, wants := range wantFiles {
		content, ok := hosting.Files[tree+":"+path]
		if !ok {
			t.Errorf("the push lacks %s", path)

			continue
		}
		for _, want := range wants {
			if !strings.Contains(content, want) {
				t.Errorf("%s lacks %q", path, want)
			}
		}
	}
	if _, kept := hosting.Files[tree+":README.md"]; !kept {
		t.Error("the initial README is gone from the tree")
	}
	// The marker and the hand-off.
	var marker create.Marker
	if err := json.Unmarshal([]byte(f.store.objects["applied/created/rgb"]), &marker); err != nil {
		t.Fatalf("no marker: %v; objects %v", err, f.store.objects)
	}
	if marker.OrganizationFolder != orgFolder || marker.BootProject != "rgb-boot-gbl-core-e5f6" || marker.Repository != owner+"/rgb-infrastructure" || marker.CreatedFrom != rootCommit {
		t.Errorf("marker = %+v", marker)
	}
	if marker.Projects["tst"] != "rgb-tst-gbl-core-e5f6" || marker.ProjectNumbers["tst"] != "100000000104" || len(marker.Projects) != 7 {
		t.Errorf("marker's projects = %v, %v", marker.Projects, marker.ProjectNumbers)
	}
	if result.PullRequest != "https://github.com/"+owner+"/"+rootRepo+"/pull/1" || result.Already {
		t.Errorf("result = %+v", result)
	}
	if len(f.repo.PullRequests) != 1 || f.repo.PullRequests[0].Head.Ref != "create/rgb" {
		t.Fatalf("pull requests %v", f.repo.PullRequests)
	}
	handOffTree := f.repo.Trees[f.repo.Refs["refs/heads/create/rgb"].SHA]
	if !strings.Contains(f.repo.Files[handOffTree+":placement.json"], `"folder": "`+orgFolder+`"`) {
		t.Errorf("the hand-off's placement.json lacks the folder:\n%s", f.repo.Files[handOffTree+":placement.json"])
	}
	created := f.repo.Files[handOffTree+":organizations/rgb/placement.json"]
	if !strings.Contains(created, `"boot": "100000000001"`) || !strings.Contains(created, `"spn": "rgb-spn-gbl-core-e5f6"`) {
		t.Errorf("the hand-off's record lacks the projects:\n%s", created)
	}
	if body := f.srv.Bodies[0]; !strings.Contains(body, "folder `"+orgFolder+"`") || !strings.Contains(body, "the creating identity's own grants taken off") || !strings.Contains(body, "of its `1-org`") {
		t.Errorf("the hand-off's body:\n%s", body)
	}
	wantOut := []string{
		"=== creation rgb as " + creatingEmail + " ===\n",
		"Created folder rgb under organizations/123456789012: " + orgFolder + ".\n",
		"Created project rgb-boot-gbl-core-e5f6 under folder " + tfFolder + ".\n",
		"Granted the boot identity its 9 roles and the break-glass account Folder Admin on folder " + orgFolder + ".\n",
		"Granted the boot identity the policy administration of the 4 shared secrets.\n",
		"Created repository " + owner + "/rgb-infrastructure, private, default branch master.\n",
		"=== first apply of rgb's 0-bootstrap as " + creatingEmail + ", state in gs://rgb-boot-gbl-state-e5f6/0-bootstrap ===\n",
		"=== first apply of rgb's 1-org as " + creatingEmail + ", state in gs://rgb-boot-gbl-state-e5f6/1-org ===\n",
		"Applied rgb's 1-org and recorded its 6 projects in the placement (projects, projectNumbers); it made 4 folders.\n",
		"Pushed the foundation to " + owner + "/rgb-infrastructure at ",
		"Took the creating identity's grants off the 4 folders and 6 projects 1-org made, folder " + tfFolder + ", project rgb-boot-gbl-core-e5f6 and folder " + orgFolder + ".\n",
		"Recorded the creation of rgb in gs://imr-boot-gbl-state-c3d4/applied/created/rgb.\n",
		"Opened the hand-off pull request of rgb: https://github.com/" + owner + "/" + rootRepo + "/pull/1.\n",
	}
	for _, want := range wantOut {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}

	// A second run: created already, the hand-off open, nothing made.
	calls, grants := len(f.google.Calls), len(f.google.Grants)
	out.Reset()
	again, err := create.Run(t.Context(), f.clients(), request(root, dir), &out)
	if err != nil {
		t.Fatalf("Run() again error = %v; output:\n%s", err, out.String())
	}
	if !again.Already || again.PullRequest != result.PullRequest || len(f.google.Calls) != calls || len(f.google.Grants) != grants || len(f.repo.PullRequests) != 1 || len(f.runner.ran) != 4 {
		t.Errorf("the second run made something: result %+v, %d calls (was %d), %d pull requests, %d programs", again, len(f.google.Calls), calls, len(f.repo.PullRequests), len(f.runner.ran))
	}
	if !strings.Contains(out.String(), "rgb was created on "+marker.CreatedAt+" (folder "+orgFolder+", boot project rgb-boot-gbl-core-e5f6): nothing to make.\n") {
		t.Errorf("output:\n%s", out.String())
	}
}

// TestRunResumes creates an organization a run left half made: the folders, the project,
// the identity and the repository exist, so none is made again, and the rest follows.
func TestRunResumes(t *testing.T) {
	t.Parallel()

	f := newFlow(t)
	dir, root := checkout(t)
	f.google.Folders["organizations/123456789012|rgb"] = "900000000007"
	f.google.Folders["folders/900000000007|terraform"] = "900000000008"
	f.google.Projects["rgb-boot-gbl-core-e5f6"], f.google.Numbers["rgb-boot-gbl-core-e5f6"] = true, "100000000042"
	f.google.Accounts["rgb-boot-gbl-tofu@rgb-boot-gbl-core-e5f6.iam.gserviceaccount.com"] = true
	f.google.Buckets["rgb-boot-gbl-state-e5f6"] = true
	f.srv.AddRepo(owner, "rgb-infrastructure", &githubtest.Repo{
		Refs: map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: "init1"}}, Ancestry: map[string][]string{"init1": {"init1"}},
		Trees: map[string]string{"init1": "treeinit"}, Files: map[string]string{"treeinit:README.md": "# rgb-infrastructure\n"},
	})
	var out bytes.Buffer
	result, err := create.Run(t.Context(), f.clients(), request(root, dir), &out)
	if err != nil {
		t.Fatalf("Run() error = %v; output:\n%s", err, out.String())
	}
	for _, call := range f.google.Calls {
		if strings.HasPrefix(call, "Create") {
			t.Errorf("made again: %s", call)
		}
	}
	if len(f.srv.Created) != 0 {
		t.Errorf("repositories created: %v", f.srv.Created)
	}
	wantOut := []string{
		"Folder rgb exists under organizations/123456789012: 900000000007.\n",
		"Project rgb-boot-gbl-core-e5f6 exists.\n",
		"Boot identity rgb-boot-gbl-tofu@rgb-boot-gbl-core-e5f6.iam.gserviceaccount.com exists.\n",
		"State bucket rgb-boot-gbl-state-e5f6 exists.\n",
		"Repository " + owner + "/rgb-infrastructure exists.\n",
		"Pushed the foundation to " + owner + "/rgb-infrastructure at ",
	}
	for _, want := range wantOut {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if result.Marker.OrganizationFolder != "900000000007" || result.Marker.BootProjectNumber != "100000000042" {
		t.Errorf("marker = %+v", result.Marker)
	}
}

// TestRunRefusals: a record whose boot project is not the listing's, and a seam that
// refuses a step, each stop the creation with the step named, and nothing after it runs.
func TestRunRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(f *flow, root *org.Placement)
		wantErr string
	}{
		{
			name: "the listing and the record disagree on the boot project",
			prepare: func(_ *flow, root *org.Placement) {
				root.HostingOrganization("rgb").BootProject = "rgb-boot-gbl-core-0000"
			},
			wantErr: "records boot project rgb-boot-gbl-core-e5f6 and placement.json's hostingOrganizations entry rgb-boot-gbl-core-0000: bedrock org create rgb brings them together",
		},
		{
			name: "a folder that cannot be made",
			prepare: func(f *flow, _ *org.Placement) {
				f.google.Refuse["CreateFolder"] = errRefused
			},
			wantErr: "refused by the stand-in",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFlow(t)
			dir, root := checkout(t)
			tt.prepare(f, root)
			var out bytes.Buffer
			_, err := create.Run(t.Context(), f.clients(), request(root, dir), &out)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Run() error = %v, want %q", err, tt.wantErr)
			}
			if len(f.runner.ran) != 0 || len(f.srv.Created) != 0 || len(f.store.objects) != 0 {
				t.Errorf("the creation went on after the refusal: %v, %v, %v", f.runner.ran, f.srv.Created, f.store.objects)
			}
		})
	}
}

var errRefused = errorString("refused by the stand-in")

type errorString string

func (e errorString) Error() string {
	return string(e)
}
