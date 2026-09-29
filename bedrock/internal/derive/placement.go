// placement.go holds the placement: the facts about the organization and its
// environments that no application declares and the stack needs anyway.

package derive

import (
	"bytes"
	"encoding/json"
	"os"
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
	// authored, and the repository's rules let it alone create one.
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
}

// Region is one Cloud Run region with the code that names its regional resources.
type Region struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

var (
	prefixRE = regexp.MustCompile(`^[a-z][a-z0-9]{0,7}$`)
	envRE    = regexp.MustCompile(`^[a-z][a-z0-9]{1,7}$`)
	codeRE   = regexp.MustCompile(`^[a-z][a-z0-9]{1,3}$`)
	sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
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

// WritePlacement writes the placement to its JSON file, in the fields' order, as
// bedrock upgrade rewrites it after moving the pin.
func WritePlacement(file string, p *Placement) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return errors.Wrap(err, "json.MarshalIndent()")
	}
	if err := os.WriteFile(file, append(data, '\n'), 0o644); err != nil {
		return errors.Wrapf(err, "os.WriteFile(): %s", file)
	}

	return nil
}

// SeedEnvironments are the environments whose database is seeded at a release build:
// the placement's seed list, none by default.
func (p *Placement) SeedEnvironments() []string {
	return p.Seed
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
