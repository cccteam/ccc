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
)

// What the destroy runs and sets, and what it keeps.
const (
	bootstrapLayer = "0-bootstrap"
	destroyCommand = "destroy"
	// autoApprove is the flag the destroy's two tofu runs take: the prefix typed back
	// at the command is the approval.
	autoApprove = "-auto-approve"
	// destroyVar is the variable both layers declare for this command alone: on its own
	// runs the projects' deletion policy becomes DELETE and the folders' deletion
	// protection comes off, recorded by an apply before the destroy, since the provider
	// reads both from the state.
	destroyVar = "destroy"
	// repositoryType is the resource type of the GitHub repositories 1-org declares, taken
	// out of the state before the destroy so that the repositories outlive the
	// organization; everything else 1-org made on GitHub goes.
	repositoryType = "github_repository"
	// netProjectAddress is the network project in 1-org's state, which the destroy
	// excludes when the layer registers domains there (they cannot move to another
	// project); organizationFolderAddress is the organization's folder in 0-bootstrap's
	// state, kept with it since the network project's folder stays inside.
	netProjectAddress         = `module.project["net"]`
	organizationFolderAddress = "google_folder.organization"
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

// DestroyRequest is what org destroy runs with: the checkout, where the GitHub token for
// 1-org's provider comes from (asked for only when 1-org's state holds something), whether
// the network project is kept (the layer registers domains there), and the pause between
// two attempts of a destroy.
type DestroyRequest struct {
	Dir         string
	GithubToken func(ctx context.Context) (string, error)
	KeepNet     bool
	Pause       time.Duration
}

// Destroy deletes the organization's projects, as the person running the command: 1-org
// first, its GitHub repositories taken out of the state, then 0-bootstrap on a local copy
// of its state. In each layer an apply with destroy=true records the projects' DELETE
// policy and lifts the folders' protection, then the destroy runs, tried again after a
// pause while it fails. A layer whose state holds nothing is skipped. With KeepNet the
// network project is excluded from 1-org's destroy and the organization's folder from
// 0-bootstrap's, and both stay for a person.
func Destroy(ctx context.Context, exec deploy.Runner, req *DestroyRequest, out io.Writer) error {
	if exec == nil {
		return errors.New("no program runner is wired")
	}
	for _, layer := range []string{orgLayer, bootstrapLayer} {
		if err := checkDestroyVariable(req.Dir, layer); err != nil {
			return err
		}
	}
	work, err := os.MkdirTemp("", "bedrock-destroy-")
	if err != nil {
		return errors.Wrap(err, "os.MkdirTemp()")
	}
	defer os.RemoveAll(work)
	if err := destroyOrgLayer(ctx, exec, req, work, out); err != nil {
		return err
	}

	return destroyBootstrapLayer(ctx, exec, req, work, out)
}

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
// repositories taken out of the state first, the network project excluded when kept.
func destroyOrgLayer(ctx context.Context, exec deploy.Runner, req *DestroyRequest, work string, out io.Writer) error {
	h := &handTofu{exec: exec, layer: orgLayer, dir: filepath.Join(req.Dir, orgLayer), data: filepath.Join(work, orgLayer+".data"), out: out}
	fmt.Fprintf(out, "=== destroy of %s ===\n", orgLayer)
	if err := h.init(ctx); err != nil {
		return err
	}
	addresses, err := h.stateList(ctx)
	if err != nil {
		return err
	}
	if len(addresses) == 0 {
		fmt.Fprintf(out, "%s's state holds nothing: nothing to destroy there.\n", orgLayer)

		return nil
	}
	token, err := req.GithubToken(ctx)
	if err != nil {
		return errors.Wrapf(err, "%s's GitHub provider needs a token for the organization's runner group, variable, rulesets and environments: the infrastructure GitHub App's installation token, or an organization owner's", orgLayer)
	}
	h.env = append(h.env, githubEnv+"="+token)
	for _, address := range addresses {
		if resourceType(address) == repositoryType {
			if err := h.run(ctx, "state", "rm", address); err != nil {
				return errors.Wrapf(err, "taking %s out of %s's state failed; the log says why", address, orgLayer)
			}
			fmt.Fprintf(out, "Took %s out of %s's state: the repository stays.\n", address, orgLayer)
		}
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

// destroyBootstrapLayer destroys 0-bootstrap on a working copy of the layer that holds a
// local copy of its state, pulled from the bucket first: the bucket is in the boot project
// and goes with it.
func destroyBootstrapLayer(ctx context.Context, exec deploy.Runner, req *DestroyRequest, work string, out io.Writer) error {
	fmt.Fprintf(out, "=== destroy of %s ===\n", bootstrapLayer)
	bucket := &handTofu{exec: exec, layer: bootstrapLayer, dir: filepath.Join(req.Dir, bootstrapLayer), data: filepath.Join(work, bootstrapLayer+".data"), out: out}
	if err := bucket.init(ctx); err != nil {
		return err
	}
	state, err := bucket.output(ctx, "state", "pull")
	if err != nil {
		return errors.Wrapf(err, "pulling %s's state failed; the log says why", bootstrapLayer)
	}
	local := &handTofu{exec: exec, layer: bootstrapLayer, dir: filepath.Join(work, bootstrapLayer), out: out}
	if err := copyLayer(req.Dir, bootstrapLayer, local.dir, state); err != nil {
		return err
	}
	fmt.Fprintf(out, "Copied %s's state and files to %s: the state bucket goes with the boot project, so the destroy runs on the copy.\n", bootstrapLayer, local.dir)
	if err := local.init(ctx); err != nil {
		return err
	}
	addresses, err := local.stateList(ctx)
	if err != nil {
		return err
	}
	if len(addresses) == 0 {
		fmt.Fprintf(out, "%s's state holds nothing: nothing to destroy there.\n", bootstrapLayer)

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

// handTofu drives tofu in one layer's directory as the person running the command: no
// identity is impersonated, the data directory is under the command's working files so
// that the checkout's own init stays as it was (empty for a copy, which keeps its own),
// and the environment carries the GitHub token for 1-org.
type handTofu struct {
	exec  deploy.Runner
	layer string
	dir   string
	data  string
	env   []string
	out   io.Writer
}

// command is one tofu command in the layer's directory.
func (h *handTofu) command(args ...string) deploy.Command {
	env := slices.Clone(h.env)
	if h.data != "" {
		env = append(env, dataDirEnv+"="+h.data)
	}

	return deploy.Command{Dir: h.dir, Env: env, Name: "tofu", Args: args}
}

// run runs one tofu command with its output on the command's.
func (h *handTofu) run(ctx context.Context, args ...string) error {
	return h.exec.Run(ctx, h.command(args...), h.out)
}

// output runs one tofu command and answers what it printed.
func (h *handTofu) output(ctx context.Context, args ...string) ([]byte, error) {
	return h.exec.Output(ctx, h.command(args...), h.out)
}

// init initializes the layer at its own backend, again when the data directory holds an
// earlier init of another shape.
func (h *handTofu) init(ctx context.Context) error {
	if err := h.run(ctx, "init", noInput, noColor, "-reconfigure"); err != nil {
		return errors.Wrapf(err, "the init of %s failed; the log says why", h.layer)
	}

	return nil
}

// stateList is every address in the layer's state.
func (h *handTofu) stateList(ctx context.Context) ([]string, error) {
	printed, err := h.output(ctx, "state", "list")
	if err != nil {
		return nil, errors.Wrapf(err, "listing %s's state failed; the log says why", h.layer)
	}
	var addresses []string
	for _, line := range strings.Split(string(printed), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			addresses = append(addresses, line)
		}
	}

	return addresses, nil
}

// unprotect applies destroy=true to every project, every project service and every
// folder in the state but the excluded ones: the provider reads a project's deletion
// policy, a service's disabling on destroy and a folder's deletion protection from the
// state, so the destroy needs them recorded first. Nothing else is targeted, so the
// apply changes nothing else.
func (h *handTofu) unprotect(ctx context.Context, addresses, excluded []string) error {
	args := []string{applyCommand, noInput, noColor, autoApprove, "-var", destroyVar + "=true"}
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
	args = append(args, destroyCommand, noInput, noColor, autoApprove, "-var", destroyVar+"=true")
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
