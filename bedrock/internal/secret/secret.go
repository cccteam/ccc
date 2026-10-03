// Package secret pins the version of a secret an environment runs. The application layer's
// secret-manager.tf creates one container per secret the code declares, an operator adds
// the value as a version, and the layer's terraform.tfvars pins, per environment and per
// variable, which version the environment mounts (secret_versions). The command writes
// that pin, asking Secret Manager first, when it is told which project to ask, that the
// version exists and is enabled; nothing is rolled out here. The pull request's plan for
// the environment shows the change to the service's configuration (Cloud Run's revision
// template) and nothing elsewhere, and the pin promotes as a release: the tag build that
// carries the commit applies the stack in that environment, deploys and moves traffic.
package secret

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// layerRoot is the directory the application layers live under: the layer of an
	// application is <layerRoot>/<app>.
	layerRoot = "3-app"
	// tfvarsFile is a layer's placement file.
	tfvarsFile = "terraform.tfvars"
	// Latest is the version alias for the most recently added version. The layer allows
	// it only for a secret the placement marks as tracking it.
	Latest = "latest"
	// Enabled is the state Secret Manager reports for a version that can be accessed.
	Enabled = "ENABLED"
	// variablePrefix is the prefix of every environment variable the application reads;
	// the secret container's name ends with the rest of the variable in kebab case.
	variablePrefix = "APP_"
)

// The environments, in promotion order.
const (
	tst = "tst"
	stg = "stg"
	prd = "prd"
)

// Environments are the environments a placement is keyed by, in promotion order.
var Environments = []string{tst, stg, prd}

// appRE matches an application name: lowercase letters, digits and inner hyphens, 1 to 6
// characters, as the stack's resource names allow.
var appRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,4}[a-z0-9])?$`)

// variableRE matches an environment variable in upper snake case under the APP_ prefix.
var variableRE = regexp.MustCompile(`^APP_[A-Z0-9]+(_[A-Z0-9]+)*$`)

// versionRE matches a version pin, a positive integer or latest: the shape the layer's
// secret_versions variable validates.
var versionRE = regexp.MustCompile(`^([1-9]\d*|latest)$`)

// ValidateApp checks that app is an application name: lowercase letters, digits and inner
// hyphens, 1 to 6 characters.
func ValidateApp(app string) error {
	if !appRE.MatchString(app) {
		return errors.Newf("app %q: lowercase letters, digits and inner hyphens, 1 to 6 characters, such as quill", app)
	}

	return nil
}

// ValidateEnv checks that env is one of the environments.
func ValidateEnv(env string) error {
	if !slices.Contains(Environments, env) {
		return errors.Newf("environment %q: one of %s", env, strings.Join(Environments, ", "))
	}

	return nil
}

// ValidateVariable checks that variable is an environment variable in upper snake case
// under the APP_ prefix.
func ValidateVariable(variable string) error {
	if !variableRE.MatchString(variable) {
		return errors.Newf("variable %q: an environment variable in upper snake case under the %s prefix, such as %sCOOKIE_KEY", variable, variablePrefix, variablePrefix)
	}

	return nil
}

// buildSecretRE matches a build-time secret's name: upper snake case, no prefix required
// (the name is the BuildKit secret id the Dockerfile mounts, KENDO_UI_LICENSE).
var buildSecretRE = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)

// ValidateBuildSecret checks that name is a build-time secret's name: upper snake case,
// with or without the APP_ prefix.
func ValidateBuildSecret(name string) error {
	if !buildSecretRE.MatchString(name) {
		return errors.Newf("build secret %q: a name in upper snake case, such as KENDO_UI_LICENSE", name)
	}

	return nil
}

// validateName checks a variable's name: a build-time secret's shape when build is true,
// else a runtime secret variable's.
func validateName(name string, build bool) error {
	if build {
		return ValidateBuildSecret(name)
	}

	return ValidateVariable(name)
}

// ValidateVersion checks that version is a positive integer or the word latest.
func ValidateVersion(version string) error {
	if !versionRE.MatchString(version) {
		return errors.Newf("version %q: a positive integer or the word %s", version, Latest)
	}

	return nil
}

// DefaultLayer is the layer whose placement holds the application's pins.
func DefaultLayer(app string) string {
	return filepath.Join(layerRoot, app)
}

// Version is what Secret Manager says about one version of a secret.
type Version struct {
	// Name is the version's resource name, projects/<project>/secrets/<secret>/versions/<n>;
	// asked for latest, it names the version latest resolved to.
	Name string
	// State is ENABLED when the version can be accessed, else DISABLED or DESTROYED.
	State string
}

// Client asks Secret Manager about the secrets of a project: one version, the containers
// carrying a label, the versions of one container. The command opens one per call through
// a ClientFunc; tests pass a fake.
type Client interface {
	// GetSecretVersion asks about the version named
	// projects/<project>/secrets/<secret>/versions/<version>.
	GetSecretVersion(ctx context.Context, name string) (*Version, error)
	// ListSecrets lists the names (the secret IDs, not the resource names) of the
	// containers in the project that match the filter, in the API's order.
	ListSecrets(ctx context.Context, project, filter string) ([]string, error)
	// ListSecretVersions lists every version of the container in the project, with its
	// state, in the API's order.
	ListSecretVersions(ctx context.Context, project, container string) ([]Version, error)
	// GetSecret asks whether the container named projects/<project>/secrets/<secret>
	// exists: its labels and true when it does, nil and false when Secret Manager knows
	// no such container.
	GetSecret(ctx context.Context, name string) (labels map[string]string, exists bool, err error)
	// CreateSecret creates the container in the project, with automatic replication and
	// the labels.
	CreateSecret(ctx context.Context, project, id string, labels map[string]string) error
	// AddSecretVersion adds the payload as a new version of the container named
	// projects/<project>/secrets/<secret> and returns the version's number.
	AddSecretVersion(ctx context.Context, name string, payload []byte) (string, error)
}

// ClientFunc opens a Client whose calls are billed to the project. NewSecretManager is
// the real one.
type ClientFunc func(ctx context.Context, project string) (Client, error)

// secretManager is the Client over the Secret Manager API.
type secretManager struct {
	client *secretmanager.Client
}

// NewSecretManager opens the Secret Manager client with Application Default Credentials,
// billing its calls to the project (the quota project).
func NewSecretManager(ctx context.Context, project string) (Client, error) {
	client, err := secretmanager.NewClient(ctx, option.WithQuotaProject(project))
	if err != nil {
		return nil, errors.Wrap(err, "secretmanager.NewClient()")
	}

	return &secretManager{client: client}, nil
}

// GetSecretVersion asks the API about the named version.
func (c *secretManager) GetSecretVersion(ctx context.Context, name string) (*Version, error) {
	v, err := c.client.GetSecretVersion(ctx, &secretmanagerpb.GetSecretVersionRequest{Name: name})
	if err != nil {
		return nil, errors.Wrapf(err, "secretmanager.Client.GetSecretVersion(): %s", name)
	}

	return &Version{Name: v.GetName(), State: v.GetState().String()}, nil
}

// ListSecrets asks the API for the containers in the project matching the filter.
func (c *secretManager) ListSecrets(ctx context.Context, project, filter string) ([]string, error) {
	var names []string
	it := c.client.ListSecrets(ctx, &secretmanagerpb.ListSecretsRequest{Parent: "projects/" + project, Filter: filter})
	for {
		s, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return names, nil
		}
		if err != nil {
			return nil, errors.Wrapf(err, "secretmanager.Client.ListSecrets(): projects/%s with filter %q", project, filter)
		}
		names = append(names, path.Base(s.GetName()))
	}
}

// ListSecretVersions asks the API for every version of the container in the project.
func (c *secretManager) ListSecretVersions(ctx context.Context, project, container string) ([]Version, error) {
	parent := "projects/" + project + "/secrets/" + container
	var versions []Version
	it := c.client.ListSecretVersions(ctx, &secretmanagerpb.ListSecretVersionsRequest{Parent: parent})
	for {
		v, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return versions, nil
		}
		if err != nil {
			return nil, errors.Wrapf(err, "secretmanager.Client.ListSecretVersions(): %s", parent)
		}
		versions = append(versions, Version{Name: v.GetName(), State: v.GetState().String()})
	}
}

// GetSecret asks the API about the named container; a container it does not know is
// reported as absent, not as an error.
func (c *secretManager) GetSecret(ctx context.Context, name string) (labels map[string]string, exists bool, err error) {
	s, err := c.client.GetSecret(ctx, &secretmanagerpb.GetSecretRequest{Name: name})
	if status.Code(err) == codes.NotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.Wrapf(err, "secretmanager.Client.GetSecret(): %s", name)
	}

	return s.GetLabels(), true, nil
}

// CreateSecret creates the container in the project with automatic replication, as the
// stack's secret-manager.tf creates one.
func (c *secretManager) CreateSecret(ctx context.Context, project, id string, labels map[string]string) error {
	_, err := c.client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
		Parent:   "projects/" + project,
		SecretId: id,
		Secret: &secretmanagerpb.Secret{
			Replication: &secretmanagerpb.Replication{
				Replication: &secretmanagerpb.Replication_Automatic_{Automatic: &secretmanagerpb.Replication_Automatic{}},
			},
			Labels: labels,
		},
	})
	if err != nil {
		return errors.Wrapf(err, "secretmanager.Client.CreateSecret(): projects/%s/secrets/%s", project, id)
	}

	return nil
}

// AddSecretVersion adds the payload as a new version of the named container.
func (c *secretManager) AddSecretVersion(ctx context.Context, name string, payload []byte) (string, error) {
	v, err := c.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent:  name,
		Payload: &secretmanagerpb.SecretPayload{Data: payload},
	})
	if err != nil {
		return "", errors.Wrapf(err, "secretmanager.Client.AddSecretVersion(): %s", name)
	}

	return path.Base(v.GetName()), nil
}

// Close releases the API connection.
func (c *secretManager) Close() error {
	if err := c.client.Close(); err != nil {
		return errors.Wrap(err, "secretmanager.Client.Close()")
	}

	return nil
}

// Request is what secret pin was asked to do.
type Request struct {
	// App is the application whose secret is pinned.
	App string
	// Env is the environment the pin is for.
	Env string
	// Variable is the environment variable the secret feeds.
	Variable string
	// Version is the version to pin: a positive integer or latest.
	Version string
	// Dir is the infrastructure repository's root.
	Dir string
	// Layer is the layer whose placement holds the pins; empty means DefaultLayer(App).
	Layer string
	// Project is the environment project the version is verified in; empty skips the
	// verification.
	Project string
	// Container is the secret container's name in the project, needed with Project.
	Container string
	// Build is true for a build-time secret (the stack's var.build_secrets), whose pin
	// lives under build_secrets rather than secret_versions.
	Build bool
}

// key is the placement map the pin lives under.
func (req *Request) key() string {
	if req.Build {
		return buildKey
	}

	return versionsKey
}

// validate checks the four arguments.
func (req *Request) validate() error {
	if err := ValidateApp(req.App); err != nil {
		return err
	}
	if err := ValidateEnv(req.Env); err != nil {
		return err
	}
	if err := validateName(req.Variable, req.Build); err != nil {
		return err
	}

	return ValidateVersion(req.Version)
}

// layer is the layer whose placement holds the pins.
func (req *Request) layer() string {
	if req.Layer != "" {
		return req.Layer
	}

	return DefaultLayer(req.App)
}

// container is the secret container's name, when a project to verify in was given: the
// name is needed then, and it is refused when missing.
func (req *Request) container() (string, error) {
	if req.Project == "" {
		return "", nil
	}
	if req.Container == "" {
		return "", errors.Newf("verifying through project %s needs the secret container's name: pass --container <name>", req.Project)
	}

	return req.Container, nil
}

// ContainerSuffix is how a secret container's name ends for the variable: the variable
// without its APP_ prefix, lowercase, with hyphens for underscores, as the stack's
// locals.tf derives it (APP_COOKIE_KEY ends a container's name as -cookie-key).
func ContainerSuffix(variable string) string {
	return "-" + strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(variable, variablePrefix), "_", "-"))
}

// Result is what secret pin found and did.
type Result struct {
	// App, Env, Variable and Version are the pin, as asked.
	App      string
	Env      string
	Variable string
	Version  string
	// File is the placement file holding the pin.
	File string
	// Unchanged is true when the placement already pinned the version and was left as it
	// was.
	Unchanged bool
	// Verified is true when Secret Manager confirmed the version exists and is enabled;
	// Project and Container then say where it was asked about.
	Verified  bool
	Project   string
	Container string
	// Resolved is the version number Secret Manager reported, which for latest is the
	// version the alias resolved to when asked.
	Resolved string
	// Key is the placement map the pin went under: secret_versions, or build_secrets for
	// a build-time secret. Empty reads as secret_versions.
	Key string
}

// key is the placement map the pin went under.
func (r *Result) key() string {
	if r.Key == "" {
		return versionsKey
	}

	return r.Key
}

// Pin writes the version into the layer's placement under secret_versions.<env>.<VARIABLE>,
// adding the entry when absent and replacing its value when present. It refuses, before
// touching anything, arguments of the wrong shape, a project to verify in without the
// container's name, a placement without a secret_versions map or without the environment
// in it, and, when a project is given, a version Secret Manager does not report as
// enabled. A variable already pinned to the version leaves the file as it is.
func Pin(ctx context.Context, open ClientFunc, req *Request) (*Result, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	container, err := req.container()
	if err != nil {
		return nil, err
	}
	layerDir := filepath.Join(req.Dir, req.layer())
	file := filepath.Join(layerDir, tfvarsFile)
	src, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.Newf("no placement at %s: --dir is the infrastructure repository's root and --layer the application layer whose placement holds the pins", file)
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	p, err := parsePlacement(src, file)
	if err != nil {
		return nil, err
	}
	current, pinned, err := p.pinned(req.key(), req.Env, req.Variable)
	if err != nil {
		return nil, err
	}
	r := &Result{App: req.App, Env: req.Env, Variable: req.Variable, Version: req.Version, File: file, Key: req.key()}
	if pinned && current == req.Version {
		r.Unchanged = true

		return r, nil
	}
	if req.Project != "" {
		resolved, err := verify(ctx, open, req.Project, container, req.Version)
		if err != nil {
			return nil, err
		}
		r.Verified, r.Project, r.Container, r.Resolved = true, req.Project, container, resolved
	}
	out, err := setVersion(src, file, req.key(), req.Env, req.Variable, req.Version)
	if err != nil {
		return nil, err
	}
	if err := writeBack(layerDir, out); err != nil {
		return nil, err
	}

	return r, nil
}

// verify opens a client for the project and asks that the version of the secret in the
// container exists and is enabled. It returns the version number the answer names, which
// for latest is the version the alias resolved to.
func verify(ctx context.Context, open ClientFunc, project, container, version string) (string, error) {
	client, err := open(ctx, project)
	if err != nil {
		return "", err
	}
	if c, ok := client.(io.Closer); ok {
		defer c.Close()
	}
	name := "projects/" + project + "/secrets/" + container + "/versions/" + version
	v, err := client.GetSecretVersion(ctx, name)
	if err != nil {
		return "", err
	}
	if v.State != Enabled {
		return "", errors.Newf("%s of %s in project %s is %s, not %s: the environment could not mount it", versionWord(version), container, project, v.State, Enabled)
	}
	resolved := path.Base(v.Name)
	if resolved == "." || resolved == "/" {
		resolved = version
	}

	return resolved, nil
}

// writeBack replaces the placement's content, keeping its mode. The write goes through an
// os.Root at the layer directory, as render's writes do.
func writeBack(layerDir string, data []byte) error {
	root, err := os.OpenRoot(layerDir)
	if err != nil {
		return errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()

	info, err := root.Stat(tfvarsFile)
	if err != nil {
		return errors.Wrap(err, "os.Root.Stat()")
	}
	if err := root.WriteFile(tfvarsFile, data, info.Mode().Perm()); err != nil {
		return errors.Wrap(err, "os.Root.WriteFile()")
	}

	return nil
}

// Write prints what was done and what to do next, the way a person reads it.
func (r *Result) Write(w io.Writer) {
	where := fmt.Sprintf("%s.%s in %s", r.key(), r.Env, r.File)
	if r.Unchanged {
		fmt.Fprintf(w, "%s is already pinned to %s for %s in %s (%s); nothing to change.\n", r.Variable, versionWord(r.Version), r.App, r.Env, where)

		return
	}
	fmt.Fprintf(w, "Pinned %s to %s for %s in %s: %s.\n", r.Variable, versionWord(r.Version), r.App, r.Env, where)
	if r.Verified {
		fmt.Fprintf(w, "Secret Manager confirms %s of %s in project %s is enabled", versionWord(r.Version), r.Container, r.Project)
		if r.Version == Latest && r.Resolved != "" && r.Resolved != Latest {
			fmt.Fprintf(w, " (latest is version %s today)", r.Resolved)
		}
		fmt.Fprintln(w, ".")
	} else {
		fmt.Fprintln(w, "The version was not verified: without --project, Secret Manager is not asked whether it exists and is enabled.")
	}
	if r.key() == buildKey {
		fmt.Fprintf(w, "Next: commit the change to the values file with a releasable type (fix(%s): …) and open the pull request; its plans show the change to the triggers' substitutions in %s (the image build reads the new version) and nothing in the other environments. The release that carries the commit applies it in %s, builds with the new version, deploys and moves traffic.\n", r.Env, r.Env, r.Env)

		return
	}
	fmt.Fprintf(w, "Next: commit the change to the values file with a releasable type (fix(%s): …) and open the pull request; its plans show the change to the service's configuration (Cloud Run's revision template) in %s and nothing in the other environments. The release that carries the commit applies it in %s, deploys and moves traffic.\n", r.Env, r.Env, r.Env)
}

// versionWord names a version in a sentence: "version 3", or "latest".
func versionWord(version string) string {
	if version == Latest {
		return Latest
	}

	return "version " + version
}
