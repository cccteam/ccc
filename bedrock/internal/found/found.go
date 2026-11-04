// Package found is how a root organization founds a hosting organization from its apply
// trigger's run (README.md, "Founding a hosting organization"): for each hosting
// organization placement.json records without a folder yet, the run, as the root
// organization's founding identity, does under a new folder what the hand seed and the
// first apply do for an organization bootstrapped by hand, pushes the rendered
// repository, takes its own creator grants off, and hands the organization off with a
// pull request that records its folder. Every step looks before it makes, so a run
// that stopped halfway finishes on the next push, and a founded organization is
// left alone: its marker in the root organization's state bucket says so.
package found

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/go-playground/errors/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// Clients are the founding's seams: Google as the founding identity, GitHub with the
// infrastructure app's token, the programs a step runs (tofu), the root organization's
// state bucket for the marker, and the wait between tries.
type Clients struct {
	Google GoogleAsFunc
	GitHub deploy.GitHubFunc
	Exec   deploy.Runner
	Store  deploy.Store
	Sleep  deploy.SleepFunc
}

// Request is one founding: the root organization's placement and checkout, the
// organization to found (a record without a folder), the infrastructure app's token
// for the repositories, and the commit the run builds.
type Request struct {
	Root    *org.Placement
	RootDir string
	Record  org.HostingOrganization
	Token   string
	Commit  string
}

// Marker is what the founding leaves in the root organization's state bucket once the
// organization is handed off (applied/found/<prefix>): what it made, and when.
type Marker struct {
	Prefix             string `json:"prefix"`
	OrganizationFolder string `json:"organizationFolder"`
	TerraformFolder    string `json:"terraformFolder"`
	BootProject        string `json:"bootProject"`
	BootProjectNumber  string `json:"bootProjectNumber"`
	StateBucket        string `json:"stateBucket"`
	Repository         string `json:"repository"`
	FoundedAt          string `json:"foundedAt"`
	FoundedFrom        string `json:"foundedFrom"`
}

// MarkerObject is the marker's object in the state bucket, under the applier's markers.
func MarkerObject(prefix string) string {
	return org.MarkerPrefix + "/found/" + prefix
}

// Result is what the founding did: the marker, and the hand-off pull request's URL.
type Result struct {
	Marker      Marker
	PullRequest string
	// Already says the organization was founded by an earlier run; this one only made
	// sure the hand-off pull request exists.
	Already bool
}

// The seed's values, as the hand seed names them (0-bootstrap/README.md, "Seed with
// gcloud"), which the founding makes the same way.
const (
	terraformFolderName = "terraform"
	// services are the APIs the boot project needs before its 0-bootstrap applies.
	bootProjectRoleCreator = "roles/resourcemanager.projectCreator"
	folderAdminRole        = "roles/resourcemanager.folderAdmin"
	folderEditorRole       = "roles/resourcemanager.folderEditor"
	ownerRole              = "roles/owner"
	// grantSettle is how long the founding waits for a new project's creator grant to
	// reach IAM before it gives up (the hand seed polls the same way).
	grantSettle = 3 * time.Minute
	grantPoll   = 10 * time.Second
	// handOffBranch is the root organization repository's branch the hand-off pull
	// request comes from, per organization.
	handOffBranch = "found/"
)

// services are the APIs the hand seed enables in a boot project.
var services = []string{
	"cloudbilling.googleapis.com", "cloudbuild.googleapis.com", "cloudresourcemanager.googleapis.com",
	"iam.googleapis.com", "iamcredentials.googleapis.com", "secretmanager.googleapis.com",
	"serviceusage.googleapis.com", "storage.googleapis.com",
}

// Run founds the organization the request names, or makes sure a founded one is handed
// off, saying each step on out.
func Run(ctx context.Context, clients *Clients, req *Request, out io.Writer) (*Result, error) {
	if err := clients.ready(); err != nil {
		return nil, err
	}
	f := &founding{clients: clients, req: req, out: out, gh: clients.GitHub(req.Token)}
	marker, err := f.readMarker(ctx)
	if err != nil {
		return nil, err
	}
	if marker != nil {
		fmt.Fprintf(out, "%s was founded on %s (folder %s, boot project %s): nothing to make.\n", req.Record.Prefix, marker.FoundedAt, marker.OrganizationFolder, marker.BootProject)
		h, err := f.record()
		if err != nil {
			return nil, err
		}
		h.ProjectNumbers = map[string]string{"boot": marker.BootProjectNumber}
		url, err := f.handOff(ctx, h, marker)
		if err != nil {
			return nil, err
		}

		return &Result{Marker: *marker, PullRequest: url, Already: true}, nil
	}

	return f.found(ctx)
}

func (c *Clients) ready() error {
	seams := []struct {
		name string
		nil  bool
	}{
		{"Google client as an identity", c.Google == nil},
		{"GitHub client", c.GitHub == nil},
		{"program runner", c.Exec == nil},
		{"Cloud Storage client", c.Store == nil},
		{"sleep", c.Sleep == nil},
	}
	for _, seam := range seams {
		if seam.nil {
			return errors.Newf("no %s is wired for the founding", seam.name)
		}
	}

	return nil
}

// founding is one run's state.
type founding struct {
	clients *Clients
	req     *Request
	out     io.Writer
	gh      *github.Client
}

// record reads the organization's founding record and fills it from the root
// organization's placement, so that it is complete.
func (f *founding) record() (*org.Placement, error) {
	h, err := org.ReadFounding(f.req.RootDir, f.req.Record.Prefix)
	if err != nil {
		return nil, err
	}
	if err := org.Fill(f.req.Root, h); err != nil {
		return nil, err
	}
	if h.Projects["boot"] != f.req.Record.BootProject {
		return nil, errors.Newf("%s records boot project %s and placement.json's hostingOrganizations entry %s: bedrock org found %s brings them together", org.FoundingPlacementPath(f.req.RootDir, h.Prefix), h.Projects["boot"], f.req.Record.BootProject, h.Prefix)
	}

	return h, nil
}

func (f *founding) readMarker(ctx context.Context) (*Marker, error) {
	data, err := f.clients.Store.Read(ctx, f.req.Root.StateBucket, MarkerObject(f.req.Record.Prefix))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): the marker gs://%s/%s", f.req.Root.StateBucket, MarkerObject(f.req.Record.Prefix))
	}

	return &m, nil
}

// found makes the organization: the seed, then the foundation on it.
func (f *founding) found(ctx context.Context) (*Result, error) {
	h, err := f.record()
	if err != nil {
		return nil, err
	}
	g, err := f.clients.Google(ctx, f.req.Root.FoundingIdentity())
	if err != nil {
		return nil, err
	}
	defer g.Close()
	fmt.Fprintf(f.out, "=== founding %s as %s ===\n", h.Prefix, f.req.Root.FoundingIdentity())
	seed, number, err := f.seed(ctx, g, h)
	if err != nil {
		return nil, err
	}
	h.ProjectNumbers = map[string]string{"boot": number}

	return f.establish(ctx, g, h, seed, number)
}

// seed does what the hand seed does: the organization's folder at the organization node
// (with Project Creator for the founding identity itself, since a folder's administrator
// creates no project), the terraform folder, the boot project with its billing link and
// its APIs, the boot identity, the state bucket, the boot identity's roles and the
// break-glass account's Folder Admin on the folder, and the boot identity's policy
// administration of the shared secrets. It answers the folders and the project's number.
func (f *founding) seed(ctx context.Context, g Google, h *org.Placement) (*org.Seed, string, error) {
	organizationFolder, err := f.folder(ctx, g, "organizations/"+h.OrganizationID, h.Prefix)
	if err != nil {
		return nil, "", err
	}
	if err := g.GrantFolder(ctx, organizationFolder, bootProjectRoleCreator, f.req.Root.FoundingIdentityMember()); err != nil {
		return nil, "", err
	}
	terraformFolder, err := f.folder(ctx, g, "folders/"+organizationFolder, terraformFolderName)
	if err != nil {
		return nil, "", err
	}
	number, err := f.project(ctx, g, h, terraformFolder)
	if err != nil {
		return nil, "", err
	}
	bootIdentity := h.Prefix + "-boot-gbl-tofu@" + h.Projects["boot"] + ".iam.gserviceaccount.com"
	if err := f.serviceAccount(ctx, g, h.Projects["boot"], h.Prefix, bootIdentity); err != nil {
		return nil, "", err
	}
	if err := f.bucket(ctx, g, h); err != nil {
		return nil, "", err
	}
	for _, role := range org.BootLayerRoles {
		if err := g.GrantFolder(ctx, organizationFolder, role, "serviceAccount:"+bootIdentity); err != nil {
			return nil, "", err
		}
	}
	if err := g.GrantFolder(ctx, organizationFolder, folderAdminRole, "user:"+h.BreakGlassAccount); err != nil {
		return nil, "", err
	}
	fmt.Fprintf(f.out, "Granted the boot identity its %d roles and the break-glass account Folder Admin on folder %s.\n", len(org.BootLayerRoles), organizationFolder)
	for _, secret := range f.req.Root.SharedSecrets() {
		if err := g.GrantSecret(ctx, secret, "projects/"+f.req.Root.Projects["boot"]+"/roles/"+org.SecretPolicyAdminRole, "serviceAccount:"+bootIdentity); err != nil {
			return nil, "", err
		}
	}
	fmt.Fprintf(f.out, "Granted the boot identity the policy administration of the %d shared secrets.\n", len(f.req.Root.SharedSecrets()))

	return &org.Seed{OrganizationFolder: organizationFolder, TerraformFolder: terraformFolder}, number, nil
}

// establish builds the foundation on the seed: the rendered repository, the repository
// on GitHub, the first apply of 0-bootstrap, the push, the founding identity's
// withdrawal, the marker and the hand-off.
func (f *founding) establish(ctx context.Context, g Google, h *org.Placement, seed *org.Seed, number string) (*Result, error) {
	dir, err := f.render(h, seed)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	repository, err := f.repository(ctx, h)
	if err != nil {
		return nil, err
	}
	if err := f.firstApply(ctx, dir, h); err != nil {
		return nil, err
	}
	commit, err := f.push(ctx, dir, h)
	if err != nil {
		return nil, err
	}
	if err := f.withdraw(ctx, g, seed.OrganizationFolder, seed.TerraformFolder, h.Projects["boot"]); err != nil {
		return nil, err
	}
	marker := Marker{
		Prefix: h.Prefix, OrganizationFolder: seed.OrganizationFolder, TerraformFolder: seed.TerraformFolder, BootProject: h.Projects["boot"], BootProjectNumber: number,
		StateBucket: h.StateBucket, Repository: repository, FoundedAt: time.Now().UTC().Format(time.RFC3339), FoundedFrom: f.req.Commit,
	}
	if err := f.writeMarker(ctx, marker); err != nil {
		return nil, err
	}
	fmt.Fprintf(f.out, "Founded %s: folder %s, boot project %s (%s), repository %s at %s; the founding identity holds nothing under the folder now.\n", h.Prefix, seed.OrganizationFolder, h.Projects["boot"], number, repository, short(commit))
	url, err := f.handOff(ctx, h, &marker)
	if err != nil {
		return nil, err
	}

	return &Result{Marker: marker, PullRequest: url}, nil
}

// folder finds or makes the folder named under the parent.
func (f *founding) folder(ctx context.Context, g Google, parent, name string) (string, error) {
	id, err := g.FindFolder(ctx, parent, name)
	if err != nil {
		return "", err
	}
	if id != "" {
		fmt.Fprintf(f.out, "Folder %s exists under %s: %s.\n", name, parent, id)

		return id, nil
	}
	id, err = g.CreateFolder(ctx, parent, name)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(f.out, "Created folder %s under %s: %s.\n", name, parent, id)

	return id, nil
}

// project finds or makes the boot project under the terraform folder, links it to the
// billing account and enables its APIs, and answers its number. A project just made is
// not readable by its creator at once; the founding waits for the grant.
func (f *founding) project(ctx context.Context, g Google, h *org.Placement, terraformFolder string) (string, error) {
	id := h.Projects["boot"]
	exists, err := g.ProjectExists(ctx, id)
	if err != nil {
		return "", err
	}
	if !exists {
		if err := g.CreateProject(ctx, id, h.Prefix+"-boot-gbl-core", terraformFolder, h.ProjectLabels("0-bootstrap", "boot")); err != nil {
			return "", err
		}
		fmt.Fprintf(f.out, "Created project %s under folder %s.\n", id, terraformFolder)
	} else {
		fmt.Fprintf(f.out, "Project %s exists.\n", id)
	}
	if err := f.settled(ctx, func() error {
		return g.LinkBilling(ctx, id, h.BillingAccount)
	}); err != nil {
		return "", err
	}
	if err := g.EnableServices(ctx, id, services); err != nil {
		return "", err
	}
	fmt.Fprintf(f.out, "Linked %s to billing account %s and enabled its %d APIs.\n", id, h.BillingAccount, len(services))

	return g.ProjectNumber(ctx, id)
}

// settled runs the call until it is not refused for a permission the creator holds but
// IAM has not seen yet, up to grantSettle.
func (f *founding) settled(ctx context.Context, call func() error) error {
	deadline := time.Now().Add(grantSettle)
	for {
		err := call()
		if err == nil || status.Code(errors.Cause(err)) != codes.PermissionDenied || time.Now().After(deadline) {
			return err
		}
		fmt.Fprintln(f.out, "The creator's grant has not reached IAM yet; trying again in a moment.")
		if err := f.clients.Sleep(ctx, grantPoll); err != nil {
			return err
		}
	}
}

func (f *founding) serviceAccount(ctx context.Context, g Google, project, prefix, email string) error {
	exists, err := g.ServiceAccountExists(ctx, email)
	if err != nil {
		return err
	}
	if exists {
		fmt.Fprintf(f.out, "Boot identity %s exists.\n", email)

		return nil
	}
	if err := g.CreateServiceAccount(ctx, project, prefix+"-boot-gbl-tofu", "OpenTofu SA - "+prefix+"-boot-gbl", "Layer identity for "+prefix+"-boot-gbl. Applies the 0-bootstrap layer from the infrastructure repository's apply trigger."); err != nil {
		return err
	}
	fmt.Fprintf(f.out, "Created the boot identity %s.\n", email)

	return nil
}

func (f *founding) bucket(ctx context.Context, g Google, h *org.Placement) error {
	exists, err := g.BucketExists(ctx, h.StateBucket)
	if err != nil {
		return err
	}
	if exists {
		fmt.Fprintf(f.out, "State bucket %s exists.\n", h.StateBucket)

		return nil
	}
	if err := g.CreateBucket(ctx, h.Projects["boot"], h.StateBucket, h.Primary().Name, h.ProjectLabels("0-bootstrap", "boot")); err != nil {
		return err
	}
	fmt.Fprintf(f.out, "Created the state bucket %s in %s.\n", h.StateBucket, h.Primary().Name)

	return nil
}

// render renders the new organization's repository into a directory of its own, the
// seeded tfvars carrying the folders and the project the founding made.
func (f *founding) render(h *org.Placement, seed *org.Seed) (string, error) {
	files, err := org.RenderSeeded(h, seed)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "bedrock-found-"+h.Prefix+"-")
	if err != nil {
		return "", errors.Wrap(err, "os.MkdirTemp()")
	}
	if _, err := org.Write(h, files, dir); err != nil {
		return "", err
	}
	if err := h.Write(filepath.Join(dir, org.PlacementFile)); err != nil {
		return "", err
	}

	return dir, nil
}

// withdraw takes the founding identity's creator grants off: Folder Admin and Folder
// Editor on the terraform folder, Owner on the boot project, and on the organization's
// folder the same two with the Project Creator it granted itself, last, so that each
// removal is still its to make.
func (f *founding) withdraw(ctx context.Context, g Google, organizationFolder, terraformFolder, project string) error {
	member := f.req.Root.FoundingIdentityMember()
	if err := g.RevokeFolder(ctx, terraformFolder, member, []string{folderAdminRole, folderEditorRole}); err != nil {
		return err
	}
	if err := g.RevokeProject(ctx, project, member, []string{ownerRole}); err != nil {
		return err
	}
	if err := g.RevokeFolder(ctx, organizationFolder, member, []string{folderAdminRole, folderEditorRole, bootProjectRoleCreator}); err != nil {
		return err
	}
	fmt.Fprintf(f.out, "Took the founding identity's grants off folder %s, folder %s and project %s.\n", organizationFolder, terraformFolder, project)

	return nil
}

func (f *founding) writeMarker(ctx context.Context, m Marker) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return errors.Wrap(err, "json.MarshalIndent()")
	}
	if err := f.clients.Store.Write(ctx, f.req.Root.StateBucket, MarkerObject(m.Prefix), append(data, '\n')); err != nil {
		return err
	}
	fmt.Fprintf(f.out, "Recorded the founding of %s in gs://%s/%s.\n", m.Prefix, f.req.Root.StateBucket, MarkerObject(m.Prefix))

	return nil
}

// short is the first seven characters of a commit, as git prints them.
func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}

	return sha
}
