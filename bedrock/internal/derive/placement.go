// placement.go holds the placement: the facts about the organization and its
// environments that no application declares and the stack needs anyway.

package derive

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"
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
	// authored, and the repository's rules let it alone, with the admins, create one.
	ReleaseApp string `json:"releaseApp"`
	// Labels are labels the organization puts on every resource, beside the ones the
	// stack derives.
	Labels map[string]string `json:"labels"`
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
	if err := json.Unmarshal(data, p); err != nil {
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
	for name, value := range map[string]string{
		"appsDomain": p.AppsDomain, "hostedDomain": p.HostedDomain, "stateBucket": p.StateBucket,
		"placeholderImage": p.PlaceholderImage, "defaultBranch": p.DefaultBranch, "repository": p.Repository,
		"releaseApp": p.ReleaseApp,
	} {
		if strings.TrimSpace(value) == "" {
			return errors.Newf("%s is empty", name)
		}
	}

	return nil
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
