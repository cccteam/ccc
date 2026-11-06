package root_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/root"
	"github.com/cccteam/ccc/bedrock/internal/root/roottest"
)

// granted answers every permission asked about.
type granted struct{}

func (granted) OrganizationPermissions(_ context.Context, _ string, permissions []string) ([]string, error) {
	return permissions, nil
}

func (granted) BillingAccountPermissions(_ context.Context, _ string, permissions []string) ([]string, error) {
	return permissions, nil
}

func (granted) Close() error {
	return nil
}

// draft is the placement as the command drafts it from the flags, the discovery and the
// prompts, before the bootstrap fills it.
func draft() *org.Placement {
	return &org.Placement{
		Kind: org.RootKind, Prefix: "imr", OrganizationDomain: "imp.example", OrganizationID: "123456789012", DirectoryCustomerID: "C0a1b2c3d",
		BillingAccount: "012345-6789AB-CDEF01", BreakGlassAccount: "bedrock@imp.example", GithubOrganization: "imp-example", GithubDefaultBranch: "master",
		SourceRepo: "imr-root-infrastructure", Regions: []org.Region{{Name: "us-central1", Code: "uc1"}},
		Domains: map[string]org.Domain{"clientbay.example": {}}, KeyOperators: []string{"group:key-operators@imp.example"},
		BedrockVersion: "v0.5.0", BedrockSHA256: strings.Repeat("ab", 32), Labels: map[string]string{"bedrock-lab": "true"},
	}
}

// keyPair is an app's key: the PEM the bootstrap stores and the public key GitHub's
// stand-in verifies with.
func keyPair(t *testing.T) ([]byte, *rsa.PublicKey) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), &key.PublicKey
}

// registerApp puts an app into GitHub's stand-in, installed on the organization when
// installed is set, and answers its PEM.
func registerApp(t *testing.T, srv *githubtest.Server, m org.AppManifest, id int64, installed bool) []byte {
	t.Helper()

	keyPEM, public := keyPair(t)
	app := &githubtest.App{
		Key:    public,
		Record: github.App{ID: id, Slug: m.Name, Name: m.Name, Owner: github.Account{Login: "imp-example", Type: "Organization"}, Permissions: m.Permissions, PEM: string(keyPEM)},
	}
	if installed {
		app.Installations = []github.Installation{{ID: id + 1, AppID: id, Account: github.Account{Login: "imp-example", Type: "Organization"}, RepositorySelection: github.AllRepositories}}
	}
	srv.Apps[fmt.Sprint(id)] = app

	return keyPEM
}

// bootstrapWorld is the lab before the bootstrap: nothing on Google Cloud, the GitHub
// organization with the Bot Account as an owner and the Cloud Build app installed.
func bootstrapWorld(t *testing.T) (*roottest.Fake, *githubtest.Server, *root.Clients) {
	t.Helper()

	fake := roottest.New()
	srv := githubtest.New(t)
	srv.Login = "imp-bot"
	srv.Memberships["imp-example"] = "admin"
	srv.PublicApps["google-cloud-build"] = github.App{ID: 10529, Slug: "google-cloud-build", Name: "Google Cloud Build"}
	srv.OrgInstallations["imp-example"] = []github.OrgInstallation{{ID: 55, AppID: 10529, AppSlug: "google-cloud-build", RepositorySelection: github.AllRepositories}}
	clients := &root.Clients{
		GitHub:      func(token string) *github.Client { return github.New(srv.URL, token) },
		Permissions: func(context.Context) (org.PermissionTester, error) { return granted{}, nil },
		Sleep:       func(context.Context, time.Duration) error { return nil },
	}
	fake.Clients(clients)

	return fake, srv, clients
}

// TestBootstrapBroughtApps: with the three apps brought, the bootstrap makes the folder,
// the project, the bucket and the four secrets, holds each app to its manifest, pins each
// key, finds the Cloud Build installation, makes and pushes the repository, makes the
// rest through the engine, takes its own Owner off, and leaves a clean check.
func TestBootstrapBroughtApps(t *testing.T) {
	t.Parallel()

	fake, srv, clients := bootstrapWorld(t)
	p := draft()
	brought := map[org.AppRole]root.BroughtApp{}
	for i, m := range p.AppManifests() {
		id := int64(5000 + i)
		keyPEM := registerApp(t, srv, m, id, true)
		brought[m.Role] = root.BroughtApp{ID: fmt.Sprint(id), Key: keyPEM}
	}
	dir := t.TempDir()
	var out bytes.Buffer
	res, err := root.Bootstrap(context.Background(), clients, &root.BootstrapRequest{Placement: p, Dir: dir, Token: []byte("test-token"), Brought: brought}, &out)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v\n%s", err, out.String())
	}
	project := res.Placement.Projects[org.RootProjectKey]
	if !strings.HasPrefix(project, "imr-root-gbl-core-") || res.Placement.StateBucket != "imr-root-gbl-state-"+strings.TrimPrefix(project, "imr-root-gbl-core-") || res.Placement.ProjectNumbers[org.RootProjectKey] == "" {
		t.Errorf("project %q, bucket %q, numbers %v", project, res.Placement.StateBucket, res.Placement.ProjectNumbers)
	}
	if res.Placement.GithubBotAccount != "imp-bot" || res.Placement.CloudBuildInstallationID != "55" || res.Placement.CloudBuildTokenSecretVersion != "projects/"+project+"/secrets/imr-root-gbl-github-token/versions/current" || res.Placement.GithubTokenVersion != "1" {
		t.Errorf("bot %q, installation %q, token %q pinned %q", res.Placement.GithubBotAccount, res.Placement.CloudBuildInstallationID, res.Placement.CloudBuildTokenSecretVersion, res.Placement.GithubTokenVersion)
	}
	if res.Placement.GithubInfrastructureAppID != "5000" || res.Placement.GithubDeployerAppID != "5001" || res.Placement.GithubReleaseAppID != "5002" || res.Placement.GithubReleaseAppSlug != "imp-example-release" {
		t.Errorf("apps %q %q %q %q", res.Placement.GithubInfrastructureAppID, res.Placement.GithubDeployerAppID, res.Placement.GithubReleaseAppID, res.Placement.GithubReleaseAppSlug)
	}
	for _, key := range org.Keys {
		if res.Placement.KeyVersion(key) != "1" {
			t.Errorf("%s pinned at %q, want 1", key, res.Placement.KeyVersion(key))
		}
	}
	if !res.Report.Clean() {
		t.Errorf("the check after the bootstrap is not clean: %v %v", whats(res.Report), res.Report.Findings)
	}
	for _, want := range []string{
		"The token is imp-bot's, an owner of imp-example: the Bot Account.",
		"Created the folder imr at the organization node: 900000000001.",
		"Created the project " + project + " under the folder.",
		"Put the Bot Account's token into imr-root-gbl-github-token as version 1, and pinned it: every Cloud Build connection authorizes with the alias current, which the apply points at the pinned version.",
		"secrets: moves the alias current of imr-root-gbl-github-token from no version to version 1",
		"the infrastructure app: App 5000 (imp-example-infrastructure), brought, holds what bedrock declares and is installed on imp-example.",
		"Google's Cloud Build GitHub App is installed on imp-example as installation 55",
		"Created the repository imp-example/imr-root-infrastructure, private.",
		"Renamed the branch main of imp-example/imr-root-infrastructure to master, the default branch placement.json names.",
		"Pushed the foundation to imp-example/imr-root-infrastructure at",
		"grants: grants serviceAccount:imr-create-gbl-tofu@" + project + ".iam.gserviceaccount.com roles/resourcemanager.folderCreator at the organization node",
		"Cloud Build: creates the trigger imr-root-uc1-apply",
		"Took bedrock@imp.example's Owner off " + project + " and its Folder Editor off folder 900000000001; it keeps Folder Admin on the folder.",
		"The root organization stands as placement.json says: nothing to change.",
		"Bootstrapped the imr root organization as bedrock@imp.example",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if !slices.Contains(fake.Calls, "GrantFolder(folders/900000000001, roles/resourcemanager.folderAdmin, user:bedrock@imp.example)") || !slices.Contains(fake.Calls, "RevokeProject(projects/"+project+", user:bedrock@imp.example, roles/owner)") || !slices.Contains(fake.Calls, "RevokeFolder(folders/900000000001, user:bedrock@imp.example, roles/resourcemanager.folderEditor)") {
		t.Errorf("the break-glass account's grants: %v", fake.Calls)
	}
	// The zones are made as the applier, with the Token Creator the run grants the
	// break-glass account on it and on the creating identity and takes off at its end.
	applier := res.Placement.ApplierIdentity()
	if !slices.Equal(fake.DNSIdentities, []string{applier}) {
		t.Errorf("Cloud DNS opened as %v, want the applier alone", fake.DNSIdentities)
	}
	for _, identity := range []string{applier, res.Placement.CreatingIdentity()} {
		if !slices.Contains(fake.Calls, "GrantServiceAccount(serviceAccounts/"+identity+", roles/iam.serviceAccountTokenCreator, user:bedrock@imp.example)") || !slices.Contains(fake.Calls, "RevokeServiceAccount(serviceAccounts/"+identity+", user:bedrock@imp.example, roles/iam.serviceAccountTokenCreator)") {
			t.Errorf("the bootstrap's Token Creator on %s was not granted and taken off: %v", identity, fake.Calls)
		}
	}
	if !slices.Contains(fake.Verifiers, applier) || !slices.Contains(fake.Verifiers, res.Placement.CreatingIdentity()) {
		t.Errorf("the identities verified: %v", fake.Verifiers)
	}
	if !slices.Contains(srv.Created, "imp-example/imr-root-infrastructure") {
		t.Errorf("the repository was not made: %v", srv.Created)
	}
	repo := srv.Repos["imp-example/imr-root-infrastructure"]
	if len(repo.Rulesets) != 1 || len(repo.Rulesets[0].BypassActors) != 1 || repo.Rulesets[0].BypassActors[0].ActorID != 5000 {
		t.Errorf("the repository's rulesets: %+v", repo.Rulesets)
	}
	placement, err := org.ReadPlacement(dir + "/placement.json")
	if err != nil {
		t.Fatalf("the rendered placement does not read: %v", err)
	}
	if placement.Projects[org.RootProjectKey] != project {
		t.Errorf("the rendered placement records project %q", placement.Projects[org.RootProjectKey])
	}
	for _, name := range []string{"README.md", "JOURNAL.md", ".gitignore"} {
		if !slices.ContainsFunc(srv.Calls, func(c string) bool { return c == "POST /repos/imp-example/imr-root-infrastructure/git/blobs" }) {
			t.Errorf("no blob was pushed for %s", name)
		}
	}

	// A second run finds everything and changes nothing.
	out.Reset()
	again, err := root.Bootstrap(context.Background(), clients, &root.BootstrapRequest{Placement: res.Placement, Dir: dir, Token: []byte("test-token"), Brought: brought}, &out)
	if err != nil {
		t.Fatalf("Bootstrap() again error = %v\n%s", err, out.String())
	}
	if !again.Report.Clean() || strings.Contains(out.String(), "Created the") || !strings.Contains(out.String(), "The project "+project+" exists.") {
		t.Errorf("the second run:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Rendered the imr root organization") || !strings.Contains(out.String(), "master of imp-example/imr-root-infrastructure carries the foundation already (") {
		t.Errorf("the second run rendered or pushed:\n%s", out.String())
	}
}

// TestBootstrapInAClone: a run again in a clone of the repository, on a branch a pull
// request is being made from, leaves the clone as the person has it: nothing rendered, the
// branch and the commit kept, the working tree clean, and no clone made over it.
func TestBootstrapInAClone(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on this computer")
	}
	fake, srv, clients := bootstrapWorld(t)
	_ = fake
	p := draft()
	brought := map[org.AppRole]root.BroughtApp{}
	for i, m := range p.AppManifests() {
		brought[m.Role] = root.BroughtApp{ID: fmt.Sprint(5000 + i), Key: registerApp(t, srv, m, int64(5000+i), true)}
	}
	dir := t.TempDir()
	var out bytes.Buffer
	res, err := root.Bootstrap(context.Background(), clients, &root.BootstrapRequest{Placement: p, Dir: dir, Token: []byte("test-token"), Brought: brought}, &out)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v\n%s", err, out.String())
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example")
		got, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, got)
		}

		return strings.TrimSpace(string(got))
	}
	git("init", "--quiet", "--initial-branch=master")
	git("remote", "add", "origin", "https://github.com/imp-example/imr-root-infrastructure.git")
	git("add", "--all")
	git("commit", "--quiet", "-m", "the foundation")
	git("checkout", "--quiet", "-b", "bedrock/pin-abc1234")
	head := git("rev-parse", "HEAD")
	clients.Clone = func(context.Context, string, string, string, string, string) error {
		t.Error("the run cloned over the clone")

		return nil
	}

	out.Reset()
	if _, err := root.Bootstrap(context.Background(), clients, &root.BootstrapRequest{Placement: res.Placement, Dir: dir, Token: []byte("test-token"), Brought: brought}, &out); err != nil {
		t.Fatalf("Bootstrap() again error = %v\n%s", err, out.String())
	}
	if want := dir + " is a clone of imp-example/imr-root-infrastructure already, on bedrock/pin-abc1234 at " + head[:7] + "."; !strings.Contains(out.String(), want) {
		t.Errorf("output lacks %q:\n%s", want, out.String())
	}
	if got := git("rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD moved from %s to %s", head, got)
	}
	if got := git("status", "--porcelain"); got != "" {
		t.Errorf("the run again left the working tree changed:\n%s", got)
	}
}

// TestBootstrapBrowserFlow: without the apps brought, the bootstrap serves the page, each
// app is made from its manifest (GitHub's redirect with the code) and installed (seen by
// the app's own listing), and the keys go into the secrets.
func TestBootstrapBrowserFlow(t *testing.T) {
	t.Parallel()

	fake, srv, clients := bootstrapWorld(t)
	p := draft()
	// The browser: it reads the page, posts nothing to GitHub (GitHub's stand-in has no
	// manifest page), and instead follows the redirect GitHub would send, with a code the
	// stand-in converts; then it installs each app, which the stand-in records.
	opened := make(chan string, 1)
	browser := func(url string) error {
		opened <- url

		return nil
	}
	go func() {
		url := <-opened
		manifests := p.AppManifests()
		for i, m := range manifests {
			page := get(t, url+"/")
			// The app's own form: its state, then its manifest with the app's name.
			form := regexp.MustCompile(`state=([0-9a-f]+)"><input type="hidden" name="manifest" value="{&#34;name&#34;:&#34;` + regexp.QuoteMeta(m.Name) + `&#34;`).FindStringSubmatch(page)
			if form == nil {
				t.Errorf("the page has no form for %s:\n%s", m.Name, page)

				return
			}
			id := int64(6000 + i)
			registerApp(t, srv, m, id, false)
			srv.Conversions["code-"+m.Name] = &srv.Apps[fmt.Sprint(id)].Record
			get(t, url+"/made?code=code-"+m.Name+"&state="+form[1])
			// The install: the app's installation appears in its own listing.
			srv.Apps[fmt.Sprint(id)].Installations = []github.Installation{{ID: id + 1, AppID: id, Account: github.Account{Login: "imp-example", Type: "Organization"}, RepositorySelection: github.AllRepositories}}
			get(t, url+"/installed?installation_id="+fmt.Sprint(id+1)+"&setup_action=install")
		}
	}()
	dir := t.TempDir()
	var out bytes.Buffer
	res, err := root.Bootstrap(context.Background(), clients, &root.BootstrapRequest{Placement: p, Dir: dir, Token: []byte("test-token"), OpenURL: browser, FlowTimeout: time.Minute}, &out)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v\n%s", err, out.String())
	}
	if res.Placement.GithubInfrastructureAppID != "6000" || res.Placement.GithubDeployerAppID != "6001" || res.Placement.GithubReleaseAppID != "6002" {
		t.Errorf("apps %q %q %q", res.Placement.GithubInfrastructureAppID, res.Placement.GithubDeployerAppID, res.Placement.GithubReleaseAppID)
	}
	if !strings.Contains(out.String(), "In your browser, as an owner of the imp-example organization, make and install imp-example-infrastructure, imp-example-deployer, imp-example-release: http://127.0.0.1:") || !strings.Contains(out.String(), "the deployer app: made App 6001 (imp-example-deployer) and installed it on imp-example.") {
		t.Errorf("output:\n%s", out.String())
	}
	project := res.Placement.Projects[org.RootProjectKey]
	stored, err := fake.AccessSecretVersion(context.Background(), "projects/"+project+"/secrets/imr-root-gbl-github-deployer-key/versions/1")
	if err != nil || string(stored) != srv.Apps["6001"].Record.PEM {
		t.Errorf("the deployer key in Secret Manager = %q, %v", stored, err)
	}
	if !res.Report.Clean() {
		t.Errorf("the check after the bootstrap is not clean: %v %v", whats(res.Report), res.Report.Findings)
	}
}

// get fetches a page of the flow, following no redirect.
func get(t *testing.T, url string) string {
	t.Helper()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Errorf("GET %s: %v", url, err)

		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Errorf("GET %s: %v", url, err)

		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	return string(body)
}

// TestBootstrapNotServingYet: Secret Manager, enabled moments before, answers the first
// call with PermissionDenied and SERVICE_DISABLED; the bootstrap tries again and goes on.
func TestBootstrapNotServingYet(t *testing.T) {
	t.Parallel()

	fake, srv, clients := bootstrapWorld(t)
	p := draft()
	brought := map[org.AppRole]root.BroughtApp{}
	for i, m := range p.AppManifests() {
		brought[m.Role] = root.BroughtApp{ID: fmt.Sprint(5000 + i), Key: registerApp(t, srv, m, int64(5000+i), true)}
	}
	fake.RefuseOnce["Secret"] = status.Error(codes.PermissionDenied, "Secret Manager API has not been used in project imr-root-gbl-core-0000 before or it is disabled. Enable it by visiting https://console.developers.google.com/apis/api/secretmanager.googleapis.com/overview?project=imr-root-gbl-core-0000 then retry.")
	var out bytes.Buffer
	if _, err := root.Bootstrap(context.Background(), clients, &root.BootstrapRequest{Placement: p, Dir: t.TempDir(), Token: []byte("test-token"), Brought: brought}, &out); err != nil {
		t.Fatalf("Bootstrap() error = %v\n%s", err, out.String())
	}
	if want := "Google answers that the Secret Manager API is not enabled yet, moments after the project's APIs were enabled; trying again in 15s."; !strings.Contains(out.String(), want) {
		t.Errorf("output lacks %q:\n%s", want, out.String())
	}
}

// TestBootstrapTakesUpTheProject: a run that stopped after making the folder and the
// project, before the placement was saved, left both; the next run takes the project up
// instead of making a second one, and records it.
func TestBootstrapTakesUpTheProject(t *testing.T) {
	t.Parallel()

	fake, srv, clients := bootstrapWorld(t)
	p := draft()
	brought := map[org.AppRole]root.BroughtApp{}
	for i, m := range p.AppManifests() {
		brought[m.Role] = root.BroughtApp{ID: fmt.Sprint(5000 + i), Key: registerApp(t, srv, m, int64(5000+i), true)}
	}
	ctx := context.Background()
	folder, err := fake.CreateFolder(ctx, "organizations/"+p.OrganizationID, p.Prefix)
	if err != nil {
		t.Fatal(err)
	}
	if err := fake.CreateProject(ctx, "imr-root-gbl-core-54b1", "imr-root-gbl-core", folder, nil); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	res, err := root.Bootstrap(ctx, clients, &root.BootstrapRequest{Placement: p, Dir: t.TempDir(), Token: []byte("test-token"), Brought: brought}, &out)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v\n%s", err, out.String())
	}
	if res.Placement.Projects[org.RootProjectKey] != "imr-root-gbl-core-54b1" || res.Placement.StateBucket != "imr-root-gbl-state-54b1" {
		t.Errorf("project %q, bucket %q: want the project made before", res.Placement.Projects[org.RootProjectKey], res.Placement.StateBucket)
	}
	for _, want := range []string{
		"The folder imr exists at the organization node: " + folder + ".",
		"Found the root project imr-root-gbl-core-54b1 under the folder, which an earlier run made; recorded.",
		"The project imr-root-gbl-core-54b1 exists.",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

// TestBootstrapIdentityNotSeenYet: the secret's policy names an identity made moments
// before, and Secret Manager answers InvalidArgument, does not exist; the engine tries
// again and goes on.
func TestBootstrapIdentityNotSeenYet(t *testing.T) {
	t.Parallel()

	fake, srv, clients := bootstrapWorld(t)
	p := draft()
	brought := map[org.AppRole]root.BroughtApp{}
	for i, m := range p.AppManifests() {
		brought[m.Role] = root.BroughtApp{ID: fmt.Sprint(5000 + i), Key: registerApp(t, srv, m, int64(5000+i), true)}
	}
	fake.RefuseOnce["SetSecretPolicy"] = status.Error(codes.InvalidArgument, "Service account imr-root-gbl-planner@imr-root-gbl-core-0000.iam.gserviceaccount.com does not exist.")
	var out bytes.Buffer
	if _, err := root.Bootstrap(context.Background(), clients, &root.BootstrapRequest{Placement: p, Dir: t.TempDir(), Token: []byte("test-token"), Brought: brought}, &out); err != nil {
		t.Fatalf("Bootstrap() error = %v\n%s", err, out.String())
	}
	if want := "(what this names was made moments ago and is not seen yet; trying again in 10s)"; !strings.Contains(out.String(), want) {
		t.Errorf("output lacks %q:\n%s", want, out.String())
	}
}

// TestBootstrapRefusals: a token an app was issued, a token of a member who is not an
// owner, an app brought that is not as declared, and the Cloud Build app not installed
// are each refused with what to do, before or after what was made (which a run again
// finds).
func TestBootstrapRefusals(t *testing.T) {
	t.Parallel()

	t.Run("the token is a GitHub App's, not a personal access token", func(t *testing.T) {
		t.Parallel()

		_, _, clients := bootstrapWorld(t)
		_, err := root.Bootstrap(context.Background(), clients, &root.BootstrapRequest{Placement: draft(), Dir: t.TempDir(), Token: []byte("ghu_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijkl")}, &bytes.Buffer{})
		if want := "the token passed is a GitHub App's user access token, which a GitHub App issued to the account that authorized it (Cloud Build's console leaves one in a secret of the connection it makes), not a personal access token: the Bot Account's token is a classic personal access token with the repo, read:user and read:org scopes"; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want %q", err, want)
		}
	})
	t.Run("the token is a member's, not an owner's", func(t *testing.T) {
		t.Parallel()

		_, srv, clients := bootstrapWorld(t)
		srv.Memberships["imp-example"] = "member"
		_, err := root.Bootstrap(context.Background(), clients, &root.BootstrapRequest{Placement: draft(), Dir: t.TempDir(), Token: []byte("test-token")}, &bytes.Buffer{})
		if want := "imp-bot, whose token this is, is a member of the imp-example organization, not an owner"; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want %q", err, want)
		}
	})
	t.Run("a brought app lacks a permission", func(t *testing.T) {
		t.Parallel()

		_, srv, clients := bootstrapWorld(t)
		p := draft()
		brought := map[org.AppRole]root.BroughtApp{}
		for i, m := range p.AppManifests() {
			id := int64(7000 + i)
			brought[m.Role] = root.BroughtApp{ID: fmt.Sprint(id), Key: registerApp(t, srv, m, id, true)}
		}
		delete(srv.Apps["7001"].Record.Permissions, org.PermissionChecks)
		_, err := root.Bootstrap(context.Background(), clients, &root.BootstrapRequest{Placement: p, Dir: t.TempDir(), Token: []byte("test-token"), Brought: brought}, &bytes.Buffer{})
		if want := "the deployer app, brought as App 7001, is not as bedrock declares it: the deployer app lacks the Checks permission (write)"; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want %q", err, want)
		}
	})
	t.Run("the Cloud Build app is not installed", func(t *testing.T) {
		t.Parallel()

		_, srv, clients := bootstrapWorld(t)
		srv.OrgInstallations["imp-example"] = nil
		p := draft()
		brought := map[org.AppRole]root.BroughtApp{}
		for i, m := range p.AppManifests() {
			id := int64(8000 + i)
			brought[m.Role] = root.BroughtApp{ID: fmt.Sprint(id), Key: registerApp(t, srv, m, id, true)}
		}
		var out bytes.Buffer
		_, err := root.Bootstrap(context.Background(), clients, &root.BootstrapRequest{Placement: p, Dir: t.TempDir(), Token: []byte("test-token"), Brought: brought}, &out)
		if want := "Google's Cloud Build GitHub App (github.com/apps/google-cloud-build) is not installed on the imp-example organization: install it there as the Bot Account (imp-bot), for all repositories, and run bedrock org bootstrap again"; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want %q", err, want)
		}
	})
}
