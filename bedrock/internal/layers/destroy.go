// destroy.go is bedrock org destroy: a person deletes the organization's projects from
// their own computer, with their own sign-in, by destroying 1-org and then 0-bootstrap.

package layers

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/render"
)

// What the destroy runs and sets, and what it keeps.
const (
	BootstrapLayer = "0-bootstrap"
	destroyCommand = "destroy"
	// autoApprove is the flag the destroy's two tofu runs take: the prefix typed back
	// at the command is the approval.
	autoApprove = "-auto-approve"
	// destroyVar is the variable the layers that make projects and folders declare for
	// this command alone (0-bootstrap and 1-org): on its own
	// runs the projects' deletion policy becomes DELETE and the folders' deletion
	// protection comes off, recorded by an apply before the destroy, since the provider
	// reads both from the state.
	destroyVar = "destroy"
	// repositoryType is the resource type of the GitHub repositories 1-org declares, taken
	// out of the state before the destroy so that the repositories outlive the
	// organization; everything else 1-org made on GitHub goes. The public-invoker tag
	// keys and values, and the values' bindings, are taken out the same way: they live in
	// the environment projects and go with them, and Google refuses to delete a value
	// while a Cloud Run service in a project being deleted still binds it, a hold a
	// deleted project keeps for its thirty days, which no number of tries outlasts.
	repositoryType = "github_repository"
	tagKeyType     = "google_tags_tag_key"
	tagValueType   = "google_tags_tag_value"
	tagBindingType = "google_tags_tag_value_iam_member"
	// netProjectAddress is the network project in 1-org's state, which the destroy
	// excludes when the layer registers domains there (they cannot move to another
	// project); organizationFolderAddress is the organization's folder in 0-bootstrap's
	// state, kept with it since the network project's folder stays inside.
	netProjectAddress         = `module.project["net"]`
	organizationFolderAddress = "google_folder.organization"
	// netLayer is the network layer, destroyed on its own inside the network project when
	// the project is kept for its registrations, since the project's deletion would have
	// taken it; registrationType is the resource type of the registrations 2-net declares,
	// taken out of the state before that destroy: a registration cannot move to another
	// project and bedrock never deletes one, so it stays in the kept project, a person's to
	// transfer out or give up.
	netLayer         = "2-net"
	registrationType = "google_clouddomains_registration"
	// What the apply before the destroy targets, by their state addresses: every project
	// (the project-factory module's, under its key), every service of a project (the
	// module's too, its disabling on destroy) and every folder.
	projectSuffix = ".google_project.main"
	serviceMark   = ".google_project_service."
	folderPrefix  = "google_folder."
	// variablesFile is where a layer declares its variables; a checkout rendered by an
	// earlier bedrock declares no destroy variable, and the command refuses it.
	variablesFile = "variables.tf"
	// backendOverrideFile moves 0-bootstrap's state to the working copy before the destroy:
	// the state bucket is in the boot project and goes with it, so the destroy runs on a
	// local copy of the state, in a copy of the layer whose override file replaces the
	// bucket backend (an override's backend block takes precedence over the base's).
	backendOverrideFile = "backend_override.tf"
	backendOverride     = "terraform {\n  backend \"local\" {}\n}\n"
	stateFile           = "terraform.tfstate"
	// destroyAttempts is how many times each layer's destroy is tried: a folder refuses
	// for a while after its last project's deletion, and a tag value while a binding in
	// a project being deleted still holds it, and the next attempt gets past both.
	destroyAttempts = 6
	// DestroyPause is how long the command waits between two attempts.
	DestroyPause = 20 * time.Second
)

// layerFiles are the files a layer's working copy takes, by pattern: the configuration,
// the values and the provider lock. The OpenTofu version pin at the repository root goes
// with them, since a version manager reads it from the directory it runs in.
var layerFiles = []string{"*.tf", "*.tfvars", "*.tfvars.json", lockFile}

// DestroyRequest is what org destroy runs with: the checkout and its placement, where the
// GitHub token for the GitHub provider comes from (asked for only when 1-org's state
// holds something), whether the network project is kept (the layer registers domains
// there; 2-net is then destroyed inside it, on a copy rendered from the placement), and
// the pause between two attempts of a destroy. A root organization has no layers: bedrock
// org destroy deletes it by API (internal/root).
type DestroyRequest struct {
	Dir         string
	Placement   *org.Placement
	GithubToken func(ctx context.Context) (string, error)
	KeepNet     bool
	Pause       time.Duration
}

// Destroy deletes the organization's projects, as the person running the command: 1-org
// first, its GitHub repositories and its tags taken out of the state, then 0-bootstrap on
// a local copy of its state. In each layer an apply with destroy=true records the
// projects' DELETE policy and lifts the folders' protection, then the destroy runs, tried
// again after a pause while it fails. A layer whose state holds nothing is skipped. With
// KeepNet the network project is excluded from 1-org's destroy and the organization's
// folder from 0-bootstrap's, and both stay for a person.
func Destroy(ctx context.Context, exec deploy.Runner, req *DestroyRequest, out io.Writer) error {
	if exec == nil {
		return errors.New("no program runner is wired")
	}
	for _, layer := range []string{orgLayer, BootstrapLayer} {
		if err := checkDestroyVariable(req.Dir, layer); err != nil {
			return err
		}
	}
	work, err := os.MkdirTemp("", "bedrock-destroy-")
	if err != nil {
		return errors.Wrap(err, "os.MkdirTemp()")
	}
	defer os.RemoveAll(work)
	if req.KeepNet {
		if err := destroyNetLayer(ctx, exec, req, work, out); err != nil {
			return err
		}
	}
	if err := destroyOrgLayer(ctx, exec, req, work, out); err != nil {
		return err
	}

	return destroyBootstrapLayer(ctx, exec, req, work, out)
}

// destroyNetLayer destroys 2-net inside the network project kept for its registrations,
// so the project holds the registrations and nothing else after: its zones and records,
// the load balancer and its address, the certificates all go, as they would have with the
// project. It runs on a working copy of the layer rendered from the placement with
// zoneReplacement set, which writes the zones without prevent_destroy (2-net/README.md,
// "Making a zone again"), against the layer's state in the bucket, with the registrations
// taken out of the state first: bedrock never deletes a registration, and the domains
// registered there stop resolving until a zone stands elsewhere and each registration is
// pointed at it.
func destroyNetLayer(ctx context.Context, exec deploy.Runner, req *DestroyRequest, work string, out io.Writer) error {
	fmt.Fprintf(out, "=== destroy of %s, inside the network project kept for its registrations ===\n", netLayer)
	if req.Placement == nil {
		return errors.Newf("no placement is wired: %s is rendered from it for its destroy", netLayer)
	}
	dir := filepath.Join(work, netLayer)
	if err := renderNetForDestroy(req.Placement, req.Dir, dir); err != nil {
		return err
	}
	h := &handTofu{exec: exec, layer: netLayer, dir: dir, data: filepath.Join(work, netLayer+".data"), out: out}
	if err := h.init(ctx); err != nil {
		return err
	}
	addresses, err := h.stateList(ctx)
	if err != nil {
		return err
	}
	if len(addresses) == 0 {
		fmt.Fprintf(out, "%s's state holds nothing: nothing to destroy there.\n", netLayer)

		return nil
	}
	for _, address := range addresses {
		if resourceType(address) != registrationType {
			continue
		}
		if err := h.run(ctx, "state", "rm", address); err != nil {
			return errors.Wrapf(err, "taking %s out of %s's state failed; the log says why", address, netLayer)
		}
		fmt.Fprintf(out, "Took %s out of %s's state: the registration stays in the kept project, a person's to transfer out or give up.\n", address, netLayer)
	}
	if err := h.destroy(ctx, nil, req.Pause); err != nil {
		return err
	}
	fmt.Fprintf(out, "The network project holds the registrations and nothing else now: %s's zones and records, load balancer and certificates are gone, and the domains registered there stop resolving until a zone stands elsewhere and each registration is pointed at it.\n", netLayer)

	return nil
}

// renderNetForDestroy renders 2-net into the directory from the placement with
// zoneReplacement set, so its zones carry no prevent_destroy there and nothing else
// differs from the checkout, and brings the layer's own files from the checkout beside
// the render (the values and the provider lock) with the OpenTofu version pin; the
// backend is the bucket's, as in the checkout, so the copy runs on the layer's state.
func renderNetForDestroy(p *org.Placement, checkout, into string) error {
	unguarded := *p
	unguarded.ZoneReplacement = true
	files, err := org.Render(&unguarded)
	if err != nil {
		return errors.Wrapf(err, "rendering %s for its destroy", netLayer)
	}
	if err := os.MkdirAll(into, 0o750); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}
	prefix := netLayer + "/"
	for _, f := range files {
		name, ok := strings.CutPrefix(f.Path, prefix)
		if !ok || f.Tier != render.Owned || strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(into, name), f.Content, 0o600); err != nil {
			return errors.Wrapf(err, "os.WriteFile(): %s", f.Path)
		}
	}
	entries, err := os.ReadDir(filepath.Join(checkout, netLayer))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Wrap(err, "os.ReadDir()")
	}
	for _, e := range entries {
		if e.IsDir() || !layerFile(e.Name()) || strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		if err := copyFile(filepath.Join(checkout, netLayer, e.Name()), into, e.Name()); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(checkout, tofuVersionFile)); err == nil {
		if err := copyFile(filepath.Join(checkout, tofuVersionFile), into, tofuVersionFile); err != nil {
			return err
		}
	}

	return nil
}

// keptOut reports whether a resource of 1-org's state is taken out of it before the
// destroy, with what becomes of it: the GitHub repositories outlive the organization,
// and the tag keys, values and bindings go with the environment projects that hold them.
func keptOut(address string) (string, bool) {
	switch resourceType(address) {
	case repositoryType:
		return "the repository stays", true
	case tagKeyType, tagValueType, tagBindingType:
		return "it goes with the project that holds it", true
	}

	return "", false
}

// tagsNotice is said once when a tag resource was taken out of the state: the run cannot
// tell from the address whether the key or value sits in an environment project, where
// it goes with the project, or at the organization node, from a render before the tags
// moved into the environment projects, where it goes with nothing, so it says what to
// look for and what to do if one is there.
const tagsNotice = "If a tag key or value stands at the organization node (gcloud resource-manager tags keys list --parent organizations/<id>), from a render before the tags moved into the environment projects, it stays behind: delete it by hand (gcloud resource-manager tags values delete, then keys delete) once the projects it was bound to are purged, 30 days after their deletion; a key in an environment project goes with the project.\n"

// checkDestroyVariable refuses a layer that declares no destroy variable: a render of an
// earlier bedrock, whose projects and folders the apply before the destroy could not
// unprotect.
func checkDestroyVariable(dir, layer string) error {
	data, err := os.ReadFile(filepath.Join(dir, layer, variablesFile))
	if err != nil {
		return errors.Wrapf(err, "os.ReadFile(): %s's variables", layer)
	}
	if !strings.Contains(string(data), `variable "`+destroyVar+`"`) {
		return errors.Newf("%s/%s declares no %s variable, which bedrock org destroy sets to record the projects' deletion policy and lift the folders' protection: run bedrock org render with this bedrock first (the variable defaults to false, so no apply is needed before the destroy)", layer, variablesFile, destroyVar)
	}

	return nil
}

// destroyOrgLayer destroys 1-org in the checkout: its state in the bucket, its GitHub
// repositories and its tags taken out of the state first, the network project excluded
// when kept.
func destroyOrgLayer(ctx context.Context, exec deploy.Runner, req *DestroyRequest, work string, out io.Writer) error {
	layer := orgLayer
	h := &handTofu{exec: exec, layer: layer, dir: filepath.Join(req.Dir, layer), data: filepath.Join(work, layer+".data"), out: out, declaresDestroy: true}
	fmt.Fprintf(out, "=== destroy of %s ===\n", layer)
	if err := h.init(ctx); err != nil {
		return err
	}
	addresses, err := h.stateList(ctx)
	if err != nil {
		return err
	}
	if len(addresses) == 0 {
		fmt.Fprintf(out, "%s's state holds nothing: nothing to destroy there.\n", layer)

		return nil
	}
	token, err := req.GithubToken(ctx)
	if err != nil {
		return errors.Wrapf(err, "%s's GitHub provider needs a token for the organization's runner group, variable, rulesets and environments: the infrastructure GitHub App's installation token, or an organization owner's", layer)
	}
	h.env = append(h.env, githubEnv+"="+token)
	tags := false
	for _, address := range addresses {
		becomes, ok := keptOut(address)
		if !ok {
			continue
		}
		if err := h.run(ctx, "state", "rm", address); err != nil {
			return errors.Wrapf(err, "taking %s out of %s's state failed; the log says why", address, layer)
		}
		fmt.Fprintf(out, "Took %s out of %s's state: %s.\n", address, layer, becomes)
		tags = tags || resourceType(address) != repositoryType
	}
	if tags {
		fmt.Fprint(out, tagsNotice)
	}
	var excluded []string
	if req.KeepNet {
		excluded = append(excluded, netProjectAddress)
	}
	if err := h.unprotect(ctx, addresses, excluded); err != nil {
		return err
	}

	return h.destroy(ctx, excluded, req.Pause)
}

// destroyVariable is the destroy variable set to true, for a layer that declares it, and
// nothing for one that does not.
func (h *handTofu) destroyVariable() []string {
	if !h.declaresDestroy {
		return nil
	}

	return []string{"-var", destroyVar + "=true"}
}

// destroyBootstrapLayer destroys 0-bootstrap on a working copy of the layer that holds a
// local copy of its state, pulled from the bucket first: the bucket is in the boot project
// and goes with it.
func destroyBootstrapLayer(ctx context.Context, exec deploy.Runner, req *DestroyRequest, work string, out io.Writer) error {
	fmt.Fprintf(out, "=== destroy of %s ===\n", BootstrapLayer)
	bucket := &handTofu{exec: exec, layer: BootstrapLayer, dir: filepath.Join(req.Dir, BootstrapLayer), data: filepath.Join(work, BootstrapLayer+".data"), out: out, declaresDestroy: true}
	if err := bucket.init(ctx); err != nil {
		return err
	}
	state, err := bucket.output(ctx, "state", "pull")
	if err != nil {
		return errors.Wrapf(err, "pulling %s's state failed; the log says why", BootstrapLayer)
	}
	local := &handTofu{exec: exec, layer: BootstrapLayer, dir: filepath.Join(work, BootstrapLayer), out: out, declaresDestroy: true}
	if err := copyLayer(req.Dir, BootstrapLayer, local.dir, state); err != nil {
		return err
	}
	fmt.Fprintf(out, "Copied %s's state and files to %s: the state bucket goes with the boot project, so the destroy runs on the copy.\n", BootstrapLayer, local.dir)
	if err := local.init(ctx); err != nil {
		return err
	}
	addresses, err := local.stateList(ctx)
	if err != nil {
		return err
	}
	if len(addresses) == 0 {
		fmt.Fprintf(out, "%s's state holds nothing: nothing to destroy there.\n", BootstrapLayer)

		return nil
	}
	var excluded []string
	if req.KeepNet {
		excluded = append(excluded, organizationFolderAddress)
	}
	if err := local.unprotect(ctx, addresses, excluded); err != nil {
		return err
	}

	return local.destroy(ctx, excluded, req.Pause)
}

// copyLayer copies the layer's files and the repository's OpenTofu version pin into the
// directory, with the state and the override file that makes the state local.
func copyLayer(dir, layer, into string, state []byte) error {
	if err := os.MkdirAll(into, 0o750); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}
	entries, err := os.ReadDir(filepath.Join(dir, layer))
	if err != nil {
		return errors.Wrap(err, "os.ReadDir()")
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && layerFile(e.Name()) {
			names = append(names, e.Name())
		}
	}
	for _, name := range names {
		if err := copyFile(filepath.Join(dir, layer, name), into, name); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, tofuVersionFile)); err == nil {
		if err := copyFile(filepath.Join(dir, tofuVersionFile), into, tofuVersionFile); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(into, backendOverrideFile), []byte(backendOverride), 0o600); err != nil {
		return errors.Wrap(err, "os.WriteFile(): the backend override")
	}
	if err := os.WriteFile(filepath.Join(into, stateFile), state, 0o600); err != nil {
		return errors.Wrap(err, "os.WriteFile(): the state")
	}

	return nil
}

// layerFile reports whether a file of a layer's directory is one the working copy takes.
func layerFile(name string) bool {
	for _, pattern := range layerFiles {
		if ok, _ := filepath.Match(pattern, name); ok {
			return true
		}
	}

	return false
}

// copyFile copies the file into the directory under the name, inside that directory
// whatever the name spells.
func copyFile(from, into, name string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return errors.Wrap(err, "os.ReadFile()")
	}
	root, err := os.OpenRoot(into)
	if err != nil {
		return errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()
	f, err := root.Create(filepath.Base(name))
	if err != nil {
		return errors.Wrap(err, "os.Root.Create()")
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()

		return errors.Wrap(err, "os.File.Write()")
	}
	if err := f.Close(); err != nil {
		return errors.Wrap(err, "os.File.Close()")
	}

	return nil
}

// resourceType is the type of the resource a state address names: the part before the
// name, after the modules.
func resourceType(address string) string {
	if i := strings.LastIndex(address, "module."); i >= 0 {
		rest := address[i:]
		if _, after, ok := strings.Cut(rest[len("module."):], "."); ok {
			address = after
		}
	}
	typ, _, _ := strings.Cut(address, ".")

	return typ
}

// unprotect applies destroy=true to every project, every project service and every
// folder in the state but the excluded ones: the provider reads a project's deletion
// policy, a service's disabling on destroy and a folder's deletion protection from the
// state, so the destroy needs them recorded first. Nothing else is targeted, so the
// apply changes nothing else.
func (h *handTofu) unprotect(ctx context.Context, addresses, excluded []string) error {
	args := append([]string{applyCommand, noInput, noColor, autoApprove}, h.destroyVariable()...)
	var projects, services, folders int
	for _, address := range addresses {
		switch {
		case strings.HasSuffix(address, projectSuffix):
			projects++
		case strings.Contains(address, serviceMark):
			services++
		case strings.HasPrefix(address, folderPrefix):
			folders++
		default:
			continue
		}
		if slices.ContainsFunc(excluded, func(e string) bool { return strings.HasPrefix(address, e) }) {
			continue
		}
		args = append(args, "-target="+address)
	}
	if projects+services+folders == 0 {
		return nil
	}
	fmt.Fprintf(h.out, "Recording on %s that the destroy deletes %d project(s) with %d service(s) and %d folder(s), disabling no API first.\n", h.layer, projects, services, folders)
	if err := h.run(ctx, args...); err != nil {
		return errors.Wrapf(err, "the apply recording %s's deletion policies failed; the log says why", h.layer)
	}

	return nil
}

// destroy destroys the layer but the excluded addresses, tried again after the pause
// while it fails, up to the attempts.
func (h *handTofu) destroy(ctx context.Context, excluded []string, pause time.Duration) error {
	args := make([]string, 0, 6+len(excluded))
	args = append(args, destroyCommand, noInput, noColor, autoApprove)
	args = append(args, h.destroyVariable()...)
	for _, address := range excluded {
		args = append(args, "-exclude="+address)
	}
	for attempt := 1; ; attempt++ {
		err := h.run(ctx, args...)
		if err == nil {
			fmt.Fprintf(h.out, "Destroyed %s.\n", h.layer)

			return nil
		}
		if attempt == destroyAttempts {
			return errors.Wrapf(err, "the destroy of %s failed %d times; the log says why", h.layer, attempt)
		}
		fmt.Fprintf(h.out, "The destroy of %s failed (attempt %d of %d): a folder refuses for a while after its last project's deletion, and a tag value while a binding in a project being deleted holds it; trying again in %s.\n", h.layer, attempt, destroyAttempts, pause)
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "waiting to try the destroy again")
		case <-time.After(pause):
		}
	}
}
