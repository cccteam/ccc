// Package org renders an organization foundation: the six layers of the CCC provisioning
// model (the bootstrap, the organization, the shared registry, the shared Spanner
// instance, the network, and the environments) from an organization placement, the way
// the application stack is rendered from an application. The layers' .tf files and
// their READMEs are owned and rewritten on every render; each layer's terraform.tfvars,
// the journal and the ignore rules are seeded once and then a person's.
package org

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"
)

const (
	// regionCount is the number of regions the model runs in: a primary and a secondary.
	regionCount = 2
	// replaceMe marks a value only a seed step can supply.
	replaceMe = "REPLACEME"
)

var (
	prefixRE        = regexp.MustCompile(`^[a-z][a-z0-9]{1,3}$`)
	regionCodeRE    = regexp.MustCompile(`^[a-z][a-z0-9]{2}$`)
	applicationRE   = regexp.MustCompile(`^[a-z][a-z0-9]{0,5}$`)
	labelRE         = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
	projectNumberRE = regexp.MustCompile(`^\d+$`)
)

// Placement is what the organization decided before any layer exists: its naming
// prefix, its domains, its billing account, its regions, its GitHub organization, and the
// few constants the layers restate. Values the seed step chooses (the boot project's
// suffix, the state bucket) are not here until it has run.
type Placement struct {
	// Prefix is the organization's naming prefix: every resource name starts with
	// <prefix>-<env>.
	Prefix string `json:"prefix"`
	// OrganizationDomain is the Google Cloud organization's domain, which is also the
	// Workspace domain the directory sign-ins restrict logins to.
	OrganizationDomain string `json:"organizationDomain"`
	// OrganizationID is the organization's numeric ID, for the seed steps.
	OrganizationID string `json:"organizationId"`
	// BillingAccount is the billing account projects are linked to.
	BillingAccount string `json:"billingAccount"`
	// AppsDomain is the domain the applications' hostnames hang under, never the
	// identity domain.
	AppsDomain string `json:"appsDomain"`
	// GithubOrganization is the GitHub organization holding the application
	// repositories and this one.
	GithubOrganization string `json:"githubOrganization"`
	// SourceRepo is this repository's name, the source_repo label every resource carries.
	SourceRepo string `json:"sourceRepo"`
	// StateBucket is the seeded state bucket every backend block names; empty until the
	// seed has run, and REPLACEME is rendered in its place.
	StateBucket string `json:"stateBucket,omitempty"`
	// Operator is the bootstrap administrator's account, who seeds and applies by hand,
	// and the first secret operator.
	Operator string `json:"operator"`
	// Regions are the two Cloud Run regions, the primary first.
	Regions []Region `json:"regions"`
	// Spanner is the shared instance's configuration.
	Spanner Spanner `json:"spanner"`
	// ContactDomains are the domains Essential Contacts may belong to, each with its
	// leading @.
	ContactDomains []string `json:"contactDomains"`
	// Applications are the application codes registered in every environment, in the
	// order they were added; empty for an organization with none yet. bedrock org
	// register adds one; the layers' applications.auto.tfvars are rendered from it.
	Applications []string `json:"applications"`
	// Projects are the environment projects by environment (tst, stg, prd), the ids
	// 1-org's apply chose (its project_ids output): what the applications' identities
	// and backends are named under. Absent until 1-org has run; REPLACEME is rendered
	// in their place.
	Projects map[string]string `json:"projects,omitempty"`
	// ProjectNumbers are the environment projects' numbers by environment, from 1-org's
	// project_numbers output, recorded beside Projects. An application's placement
	// records its environments' ids and numbers together (bedrock org register prints
	// the block), for the operations workflow that starts a restore from GitHub.
	ProjectNumbers map[string]string `json:"projectNumbers,omitempty"`
	// Labels are the labels every project and bucket carries beyond the model's own.
	Labels map[string]string `json:"labels"`
}

// The model's environments, by name.
const (
	tstEnvironment = "tst"
	stgEnvironment = "stg"
	prdEnvironment = "prd"
)

// Environments are the model's environments in promotion order; pull requests deploy
// to the first, production is the last.
var Environments = []string{tstEnvironment, stgEnvironment, prdEnvironment}

// Region is one region: its name and the three-letter code resource names carry.
type Region struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

// Spanner is the shared instance's configuration.
type Spanner struct {
	// Config is the instance configuration, a multi-region one whose read-write replicas
	// are the two regions (nam10 for us-central1 and us-west3).
	Config string `json:"config"`
}

// ReadPlacement reads and validates a placement file.
func ReadPlacement(path string) (*Placement, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	var p Placement
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): %s", path)
	}
	if err := p.Validate(); err != nil {
		return nil, errors.Wrapf(err, "%s", path)
	}

	return &p, nil
}

// Validate refuses a placement a layer could not be rendered from.
func (p *Placement) Validate() error {
	if !prefixRE.MatchString(p.Prefix) {
		return errors.Newf("prefix %q is not 2 to 4 lowercase alphanumeric characters starting with a letter", p.Prefix)
	}
	for name, value := range map[string]string{
		"organizationDomain": p.OrganizationDomain, "organizationId": p.OrganizationID, "billingAccount": p.BillingAccount,
		"appsDomain": p.AppsDomain, "githubOrganization": p.GithubOrganization, "sourceRepo": p.SourceRepo,
		"operator": p.Operator, "spanner.config": p.Spanner.Config,
	} {
		if strings.TrimSpace(value) == "" {
			return errors.Newf("%s is empty", name)
		}
	}
	if len(p.Regions) != regionCount {
		return errors.Newf("regions has %d entries; the model runs in two, the primary first", len(p.Regions))
	}
	for _, r := range p.Regions {
		if r.Name == "" || !regionCodeRE.MatchString(r.Code) {
			return errors.Newf("region %q needs a name and a three-character code", r.Name)
		}
	}
	for env, id := range p.Projects {
		if !slices.Contains(Environments, env) {
			return errors.Newf("projects names %q, which is not one of %s", env, strings.Join(Environments, ", "))
		}
		if strings.TrimSpace(id) == "" {
			return errors.Newf("projects.%s is empty", env)
		}
	}
	for env, number := range p.ProjectNumbers {
		if !slices.Contains(Environments, env) {
			return errors.Newf("projectNumbers names %q, which is not one of %s", env, strings.Join(Environments, ", "))
		}
		if !projectNumberRE.MatchString(number) {
			return errors.Newf("projectNumbers.%s %q is not a project number (digits)", env, number)
		}
	}
	for _, d := range p.ContactDomains {
		if !strings.HasPrefix(d, "@") || len(d) < 3 {
			return errors.Newf("contact domain %q is not @<domain>", d)
		}
	}
	for _, a := range p.Applications {
		if !applicationRE.MatchString(a) {
			return errors.Newf("application %q is not 1 to 6 lowercase alphanumeric characters starting with a letter", a)
		}
	}
	for k, v := range p.Labels {
		if !labelRE.MatchString(k) || v == "" {
			return errors.Newf("label %q = %q is not a lowercase key with a value", k, v)
		}
	}

	return nil
}

// Primary is the primary region.
func (p *Placement) Primary() Region {
	return p.Regions[0]
}

// Secondary is the secondary region.
func (p *Placement) Secondary() Region {
	return p.Regions[1]
}

// Bucket is the state bucket every backend block names: the seeded one, or the
// REPLACEME form the seed step's README says to substitute.
func (p *Placement) Bucket() string {
	if p.StateBucket != "" {
		return p.StateBucket
	}

	return p.Prefix + "-boot-gbl-state-" + replaceMe
}

// labelKeys lists the placement's label keys, sorted.
func (p *Placement) labelKeys() []string {
	keys := make([]string, 0, len(p.Labels))
	for k := range p.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}

// Project is the environment's project id, or the REPLACEME form until 1-org has run
// and the placement records it.
func (p *Placement) Project(env string) string {
	if id, ok := p.Projects[env]; ok {
		return id
	}

	return p.Prefix + "-" + env + "-gbl-core-" + replaceMe
}

// ProjectsMissing names the environments whose project the placement does not record.
func (p *Placement) ProjectsMissing() []string {
	var missing []string
	for _, env := range Environments {
		if _, ok := p.Projects[env]; !ok {
			missing = append(missing, env)
		}
	}

	return missing
}

// ApplicationProjects is the block an application's placement records for its
// environments' projects (id and number, as 1-org's project_ids and project_numbers
// outputs name them), for the environments this placement records both of, and the
// environments it does not. Production is left out: it is never restored by a run.
func (p *Placement) ApplicationProjects() (block string, missing []string) {
	var lines []string
	for _, env := range Environments {
		if env == prdEnvironment {
			continue
		}
		id, number := p.Projects[env], p.ProjectNumbers[env]
		if id == "" || number == "" {
			missing = append(missing, env)

			continue
		}
		lines = append(lines, fmt.Sprintf("    %q: {\"id\": %q, \"number\": %q}", env, id, number))
	}
	if len(lines) == 0 {
		return "", missing
	}

	return "  \"projects\": {\n" + strings.Join(lines, ",\n") + "\n  }", missing
}

// Register adds an application to the placement: a code of one to six lowercase
// alphanumeric characters starting with a letter, not registered yet.
func (p *Placement) Register(app string) error {
	if !applicationRE.MatchString(app) {
		return errors.Newf("application %q is not 1 to 6 lowercase alphanumeric characters starting with a letter", app)
	}
	if slices.Contains(p.Applications, app) {
		return errors.Newf("application %q is registered already", app)
	}
	p.Applications = append(p.Applications, app)

	return nil
}

// Write puts the placement back in its file, indented the way a person reads it.
func (p *Placement) Write(path string) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return errors.Wrap(err, "json.MarshalIndent()")
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return errors.Wrapf(err, "os.WriteFile(): %s", path)
	}

	return nil
}
