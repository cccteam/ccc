// placement.go holds the placement: the facts about the organization and its
// environments that no application declares and the stack needs anyway.

package derive

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/release"
)

// Placement is what the organization decided and the application cannot know: its
// naming prefix, its environments, its regions, its domains, and the few constants the
// stack restates.
type Placement struct {
	// Prefix is the organization's naming prefix: every resource name starts with
	// <prefix>-<env>.
	Prefix string `json:"prefix"`
	// Environments are the environments in promotion order; the first is where pull
	// requests deploy and the last is production.
	Environments []string `json:"environments"`
	// Regions are the Cloud Run regions; the first is primary.
	Regions []Region `json:"regions"`
	// AppsDomain is the domain the applications' hostnames hang under.
	AppsDomain string `json:"appsDomain"`
	// HostedDomain is the Google Workspace domain the directory auths restrict logins to.
	HostedDomain string `json:"hostedDomain"`
	// StateBucket is the state bucket every backend block names.
	StateBucket string `json:"stateBucket"`
	// PlaceholderImage is the image every service and job is created with, before the
	// first deploy.
	PlaceholderImage string `json:"placeholderImage"`
	// DefaultBranch is the branch pull requests target.
	DefaultBranch string `json:"defaultBranch"`
	// Repository is the application repository's name, for the source_repo label.
	Repository string `json:"repository"`
	// ReleaseApp is the slug of the GitHub App that cuts the releases (release-please
	// runs as it): the pipeline accepts a release tag only from a GitHub Release it
	// authored, and the repository's rules let it alone create one; not even an admin
	// may bypass them.
	ReleaseApp string `json:"releaseApp"`
	// BedrockVersion is the bedrock the pipeline and the infrastructure workflow run, one
	// of two kinds of pin. A release (v0.4.0: the tag bedrock/v0.4.0 of
	// github.com/cccteam/ccc) comes with BedrockSHA256, the SHA-256, in hex, of that
	// release's linux/amd64 binary, which both download and verify before running it. A
	// commit pin (v0.0.0-lab.1.0.20260928222237-58b211dce544: the pseudo-version the Go
	// module proxy gives a pushed commit of the bedrock module) comes with no checksum:
	// both build it with go install, and Go's checksum database verifies it. The one place
	// a bedrock version appears in the application: bedrock upgrade moves the pin, never a
	// hand edit in a rendered file. Both empty, the placement is unpinned, which render and
	// check refuse.
	BedrockVersion string `json:"bedrockVersion"`
	BedrockSHA256  string `json:"bedrockSha256"`
	// Labels are labels the organization puts on every resource, beside the ones the
	// stack derives.
	Labels map[string]string `json:"labels,omitempty"`
	// Seed lists the environments whose database takes the development seed
	// (schema/devseed, as data migrations tracked apart from the schema) at a release
	// build, after the schema migrations. Absent, none: a database holding data is
	// never seeded unless the placement says so. A pull-request environment is always
	// seeded, its database being new; production never.
	Seed []string `json:"seed,omitempty"`
	// Approvals lists the environments whose version trigger waits for a person's
	// approval in Cloud Build before a release runs there. Absent, every environment
	// but the first.
	Approvals []string `json:"approvals,omitempty"`
	// BuildMachine is the Cloud Build machine the pipeline's builds run on, by the name
	// cloudbuild.yaml's options.machineType takes (E2_HIGHCPU_8); absent, Cloud Build's
	// default. The machine is a build-level option: the whole run is on it, a window
	// release's wait included, and no step has a machine of its own. The image step is
	// what a larger machine shortens: its compile and its bundle build run fresh in every
	// environment, the registry's layer cache holding the downloads alone. The number of
	// builds a project runs at once on it is its CPU quota for Cloud Build's default pool
	// divided by the machine's vCPUs (Machines), a quota Cloud Build sets per project and
	// never raises, so nothing here reads it.
	BuildMachine string `json:"buildMachine,omitempty"`
	// MaxInstances is the most Cloud Run instances the service may run per region, by
	// environment name: a cap that bounds what the environment can cost when traffic
	// rises. An environment it does not name has no cap of the placement's, and Cloud
	// Run's own default applies there. Absent, none has a cap.
	MaxInstances map[string]int `json:"maxInstances,omitempty"`
	// OutlierDetection is the backend service's outlier detection thresholds, how the
	// load balancer takes a failing region out (outlier.go); a field left out, or the
	// whole block, takes its default.
	OutlierDetection *OutlierDetection `json:"outlierDetection,omitempty"`
	// Maintenance is each environment's maintenance window by environment name: the
	// time the environment may take a release that interrupts service (a breaking
	// release, whose oldest answered release is newer than the one the environment runs,
	// deploys behind the maintenance page inside it; under releases all, every release
	// waits for it). Written as "anytime" or as the client's windows (maintenance.go).
	// Every environment but production is anytime unless written; production has no
	// default, and a breaking release to it is refused until its setting is written.
	Maintenance map[string]MaintenanceWindow `json:"maintenance,omitempty"`
	// Projects are the environment projects by environment, as the organization's
	// apply chose them: the id and the number (bedrock org register writes them into the
	// first placement from the organization's, and org render prints the block). The
	// operations workflow, which starts a restore or a rerun of an environment from
	// GitHub, names the environment's workload identity provider and operations identity
	// by them; an environment without an entry is not wired for it. Production's serves
	// the rerun alone: it is never restored by a run.
	Projects map[string]Project `json:"projects,omitempty"`
}

// Region is one Cloud Run region with the code that names its regional resources.
type Region struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

// Project is an environment project: its id and its number.
type Project struct {
	ID     string `json:"id"`
	Number string `json:"number"`
}

// Machine is one of the machines Cloud Build's default pool runs a build on, by the
// name options.machineType takes, with its vCPUs: what a project's default-pool CPU
// quota is divided by for the number of builds it runs at once.
type Machine struct {
	Name string
	CPUs int
}

// The machines a placement's buildMachine may name, as options.machineType spells them.
const (
	MachineE2Medium    = "E2_MEDIUM"
	MachineE2Standard2 = "E2_STANDARD_2"
	MachineE2HighCPU8  = "E2_HIGHCPU_8"
	MachineE2HighCPU32 = "E2_HIGHCPU_32"
)

// Machines are the machines a placement's buildMachine may name, in size order: the E2
// machines of the Cloud Build API's MachineType enum. The enum's two N1 machines are
// deprecated there and left out, so a placement never names a machine Cloud Build is
// retiring; the enum's UNSPECIFIED, the default, is what an absent value means.
func Machines() []Machine {
	return []Machine{
		{Name: MachineE2Medium, CPUs: 1},
		{Name: MachineE2Standard2, CPUs: 2},
		{Name: MachineE2HighCPU8, CPUs: 8},
		{Name: MachineE2HighCPU32, CPUs: 32},
	}
}

// PlaceholderImage is the image an application's first placement names for every service
// and job to be created with before the first deploy (placeholderImage): Cloud Run's
// public sample, which serves on the port Cloud Run gives it and reads nothing.
const PlaceholderImage = "us-docker.pkg.dev/cloudrun/container/hello"

var (
	prefixRE = regexp.MustCompile(`^[a-z][a-z0-9]{0,7}$`)
	envRE    = regexp.MustCompile(`^[a-z][a-z0-9]{1,7}$`)
	codeRE   = regexp.MustCompile(`^[a-z][a-z0-9]{1,3}$`)
	sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
	numberRE = regexp.MustCompile(`^\d+$`)
)

// The two restores a run makes: an empty database the migrations and the seed then
// fill, and production's most recent backup, for the environment on production's
// instance whose database is not seeded.
const (
	RestoreEmpty  = "empty"
	RestoreBackup = "production-backup"
)

// ReadPlacement reads a placement from its JSON file.
func ReadPlacement(file string) (*Placement, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.Newf("no placement at %s: pass --placement", file)
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	p := &Placement{}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(p); err != nil {
		return nil, errors.Wrapf(err, "placement %s", file)
	}
	if err := p.Validate(); err != nil {
		return nil, errors.Wrapf(err, "placement %s", file)
	}

	return p, nil
}

// Validate checks the placement's shape.
func (p *Placement) Validate() error {
	if p == nil {
		return errors.New("no placement")
	}
	if !prefixRE.MatchString(p.Prefix) {
		return errors.Newf("prefix %q: a lowercase word of at most eight characters", p.Prefix)
	}
	if len(p.Environments) == 0 {
		return errors.New("at least one environment")
	}
	for _, env := range p.Environments {
		if !envRE.MatchString(env) {
			return errors.Newf("environment %q: a lowercase word of two to eight characters", env)
		}
	}
	if len(p.Regions) == 0 {
		return errors.New("at least one region")
	}
	for _, r := range p.Regions {
		if r.Name == "" || !codeRE.MatchString(r.Code) {
			return errors.Newf("region %q with code %q: a region name and a code of two to four lowercase characters", r.Name, r.Code)
		}
	}
	for _, env := range p.Approvals {
		if !slices.Contains(p.Environments, env) {
			return errors.Newf("approvals names %q, which is not one of the environments (%s)", env, strings.Join(p.Environments, ", "))
		}
	}
	for _, env := range p.Seed {
		if !slices.Contains(p.Environments, env) {
			return errors.Newf("seed names %q, which is not one of the environments (%s)", env, strings.Join(p.Environments, ", "))
		}
		if env == p.Production() {
			return errors.Newf("seed names %q, the production environment, which is never seeded", env)
		}
	}
	for env, project := range p.Projects {
		if !slices.Contains(p.Environments, env) {
			return errors.Newf("projects names %q, which is not one of the environments (%s)", env, strings.Join(p.Environments, ", "))
		}
		if strings.TrimSpace(project.ID) == "" {
			return errors.Newf("projects.%s.id is empty: the environment project's id", env)
		}
		if !numberRE.MatchString(project.Number) {
			return errors.Newf("projects.%s.number %q is not a project number (digits)", env, project.Number)
		}
	}
	if err := p.validateMaintenance(); err != nil {
		return err
	}
	if err := p.validateBuildMachine(); err != nil {
		return err
	}
	if err := p.validateMaxInstances(); err != nil {
		return err
	}
	if err := p.OutlierDetection.Validate(); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"appsDomain": p.AppsDomain, "hostedDomain": p.HostedDomain, "stateBucket": p.StateBucket,
		"placeholderImage": p.PlaceholderImage, "defaultBranch": p.DefaultBranch, "repository": p.Repository,
		"releaseApp": p.ReleaseApp,
	} {
		if strings.TrimSpace(value) == "" {
			return errors.Newf("%s is empty", name)
		}
	}

	return p.validatePin()
}

// validateBuildMachine checks that buildMachine, when written, is one of Cloud Build's
// machine names, so that a misspelling is refused here and not by the first build.
func (p *Placement) validateBuildMachine() error {
	if p.BuildMachine == "" {
		return nil
	}
	if _, ok := p.Machine(); ok {
		return nil
	}
	names := make([]string, 0, len(Machines()))
	for _, m := range Machines() {
		names = append(names, m.Name)
	}

	return errors.Newf("buildMachine %q is not one of Cloud Build's machines (%s); absent, the builds run on Cloud Build's default", p.BuildMachine, strings.Join(names, ", "))
}

// validateMaxInstances checks that each cap names one of the environments and lets the
// service run at least one instance.
func (p *Placement) validateMaxInstances() error {
	for _, env := range slices.Sorted(maps.Keys(p.MaxInstances)) {
		if !slices.Contains(p.Environments, env) {
			return errors.Newf("maxInstances names %q, which is not one of the environments (%s)", env, strings.Join(p.Environments, ", "))
		}
		if n := p.MaxInstances[env]; n < 1 {
			return errors.Newf("maxInstances.%s is %d: a cap lets the service run at least one instance; leave the environment out for no cap (Cloud Run's default)", env, n)
		}
	}

	return nil
}

// validatePin checks the bedrock pin: none (both fields empty), a release with the
// SHA-256 of its pipeline binary, or a commit pin with no checksum.
func (p *Placement) validatePin() error {
	switch {
	case p.BedrockVersion == "" && p.BedrockSHA256 == "":
		return nil
	case p.BedrockVersion == "":
		return errors.New("bedrockSha256 without a bedrockVersion: bedrock upgrade sets the pin")
	case release.IsVersion(p.BedrockVersion):
		if p.BedrockSHA256 == "" {
			return errors.Newf("bedrockVersion %s is a release, and a release pin carries its bedrockSha256: bedrock upgrade sets both together", p.BedrockVersion)
		}
		if !sha256RE.MatchString(p.BedrockSHA256) {
			return errors.Newf("bedrockSha256 %q: the SHA-256 of the release's %s, 64 hex digits from its %s", p.BedrockSHA256, release.PipelineAsset(), release.ChecksumsFile)
		}

		return nil
	case release.IsCommitPin(p.BedrockVersion):
		if p.BedrockSHA256 != "" {
			return errors.Newf("bedrockVersion %s is a commit pin, and a commit pin carries no bedrockSha256: Go's checksum database verifies it", p.BedrockVersion)
		}

		return nil
	default:
		return errors.Newf("bedrockVersion %q: a release version, v0.4.0 (the tag bedrock/v0.4.0 without its prefix), or a commit pin, the pseudo-version the Go module proxy gives a pushed commit (bedrock upgrade <commit> writes it)", p.BedrockVersion)
	}
}

// Pinned reports whether the placement pins bedrock: a release or a commit.
func (p *Placement) Pinned() bool {
	return p.BedrockVersion != ""
}

// MarshalPlacement is the placement as its JSON file holds it: the fields in their
// order, indented the way a person reads it, ending in a newline.
func MarshalPlacement(p *Placement) ([]byte, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, errors.Wrap(err, "json.MarshalIndent()")
	}

	return append(data, '\n'), nil
}

// WritePlacement writes the placement to its JSON file, in the fields' order, as
// bedrock upgrade rewrites it after moving the pin.
func WritePlacement(file string, p *Placement) error {
	data, err := MarshalPlacement(p)
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return errors.Wrapf(err, "os.WriteFile(): %s", file)
	}

	return nil
}

// CreatePlacement writes an application's first placement to its JSON file, creating the
// file's directory when absent, and refuses a file that exists: a placement is the
// application's from its first write, and nothing overwrites it.
func CreatePlacement(file string, p *Placement) error {
	switch _, err := os.Lstat(file); {
	case err == nil:
		return errors.Newf("%s exists: an application's first placement is written once and never overwritten", file)
	case !errors.Is(err, os.ErrNotExist):
		return errors.Wrap(err, "os.Lstat()")
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}

	return WritePlacement(file, p)
}

// SeedEnvironments are the environments whose database is seeded at a release build:
// the placement's seed list, none by default.
func (p *Placement) SeedEnvironments() []string {
	return p.Seed
}

// Restorable lists the environments a run may restore: every one but production.
func (p *Placement) Restorable() []string {
	var envs []string
	for _, env := range p.Environments {
		if env != p.Production() {
			envs = append(envs, env)
		}
	}

	return envs
}

// RestoreKind is what a restore run replaces env's database with: production's most
// recent backup for an environment on production's instance (every one above the
// first) whose database is not seeded, an empty database otherwise, which the
// migrations and the seed then fill.
func (p *Placement) RestoreKind(env string) string {
	if env != p.Integration() && !slices.Contains(p.Seed, env) {
		return RestoreBackup
	}

	return RestoreEmpty
}

// Machine is the build machine the placement names and whether it names one; absent,
// the pipeline leaves the machine to Cloud Build's default.
func (p *Placement) Machine() (Machine, bool) {
	for _, m := range Machines() {
		if m.Name == p.BuildMachine {
			return m, true
		}
	}

	return Machine{}, false
}

// Outlier is the outlier detection thresholds the backend service is rendered with: the
// placement's, with the defaults where it sets none.
func (p *Placement) Outlier() Outlier {
	return p.OutlierDetection.Resolved()
}

// MaxInstanceCount is the most instances the service may run per region in env, and
// whether the placement caps it there; uncapped, Cloud Run's default applies.
func (p *Placement) MaxInstanceCount(env string) (int, bool) {
	n, ok := p.MaxInstances[env]

	return n, ok
}

// Project is the environment's project as the placement records it, and whether it
// records one.
func (p *Placement) Project(env string) (Project, bool) {
	project, ok := p.Projects[env]

	return project, ok
}

// ApprovalEnvironments are the environments a release waits for approval in: Approvals
// as given, else every environment but the first.
func (p *Placement) ApprovalEnvironments() []string {
	if p.Approvals != nil {
		return p.Approvals
	}

	return p.Environments[1:]
}

// Previous is the environment before env in the promotion order, whose live
// deployment record a release needs before it runs in env; empty for the first.
func (p *Placement) Previous(env string) string {
	for i, e := range p.Environments {
		if e == env && i > 0 {
			return p.Environments[i-1]
		}
	}

	return ""
}

// ReleaseActor is the login the release app's releases carry: <slug>[bot].
func (p *Placement) ReleaseActor() string {
	return p.ReleaseApp + "[bot]"
}

// Integration is the environment pull requests deploy to: the first.
func (p *Placement) Integration() string {
	return p.Environments[0]
}

// Production is the last environment.
func (p *Placement) Production() string {
	return p.Environments[len(p.Environments)-1]
}

// Primary is the primary region.
func (p *Placement) Primary() Region {
	return p.Regions[0]
}

// environments lists the environments with the hostnames the application serves in
// each: <app>.<domain> in production, <app>-<env>.<domain> below it.
func (p *Placement) environments(app string) []Environment {
	envs := make([]Environment, 0, len(p.Environments))
	for _, env := range p.Environments {
		host := app + "-" + env + "." + p.AppsDomain
		if env == p.Production() {
			host = app + "." + p.AppsDomain
		}
		envs = append(envs, Environment{Name: env, Hostnames: []string{host}})
	}

	return envs
}
