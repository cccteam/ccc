// Package org renders an organization foundation: the six layers of the CCC provisioning
// model (the bootstrap, the organization, the shared registry, the shared Spanner
// instance, the network, and the environments) from an organization placement, the way
// the application stack is rendered from an application. The layers' .tf files and
// their READMEs are owned and rewritten on every render; each layer's terraform.tfvars,
// the journal and the ignore rules are seeded once and then a person's.
package org

import (
	"encoding/json"
	"os"
	"regexp"
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
	prefixRE      = regexp.MustCompile(`^[a-z][a-z0-9]{1,3}$`)
	regionCodeRE  = regexp.MustCompile(`^[a-z][a-z0-9]{2}$`)
	applicationRE = regexp.MustCompile(`^[a-z][a-z0-9]{0,5}$`)
	labelRE       = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
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
	// order they were added; empty for an organization with none yet.
	Applications []string `json:"applications"`
	// Labels are the labels every project and bucket carries beyond the model's own.
	Labels map[string]string `json:"labels"`
}

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
