// root.go is the root kind of organization: a bedrock organization with no environments
// that holds DNS zones alone, one per Google Cloud organization that hosts applications.
// A customer delegates a shared domain to it once (mydomain.com, or a label such as
// apps.mydomain.com), and each application's name under that domain is handed onward, as
// one NS record set, to the zone the application's hosting organization serves it from, so
// a customer's second application lands in another hosting organization with nothing
// moving at the customer's registrar. Its layers are the bootstrap every organization gets
// and 1-dns in place of the organization and environment layers; its zones live in the
// boot project.

package org

import (
	"maps"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// The kinds of organization a placement describes (Placement.Kind): hosting, the
// default, which holds applications in its environments; and zones, which holds DNS zones
// alone and hands labels of its domains onward to the hosting organizations' zones.
const (
	HostingKind = "hosting"
	RootKind    = "root"
)

// Kinds are the kinds a placement may name.
var Kinds = []string{HostingKind, RootKind}

// dnsLayer is the one layer of a root organization after the bootstrap: the zones and
// their NS record sets, in the boot project, and the rule on this repository's default
// branch.
const dnsLayer = "1-dns"

// The identity names of the second layer of each kind, as 0-bootstrap names the layer's
// apply and plan identities (<prefix>-<name>-gbl-tofu, <prefix>-<name>-gbl-plan).
const (
	orgIdentity = "org"
	dnsIdentity = "dns"
)

// RootLayers are a root organization's layers, in apply order.
var RootLayers = []string{bootstrapLayer, dnsLayer}

// rootProjectKeys are a root organization's projects by key: the boot project alone,
// where its zones live.
var rootProjectKeys = []string{bootProject}

// nameServerCount is how many name servers Cloud DNS assigns a zone, and how many a
// label handed onward names.
const nameServerCount = 4

// Domain is one domain a root organization holds a zone for: the labels of it handed
// onward, by label, each to the zone a hosting organization serves the name from. A domain
// with no label yet holds its zone alone, so the customer's delegation is made once, at
// the first application or before it.
type Domain struct {
	// Labels are the names under the domain handed onward, by label (acme for
	// acme.mydomain.com; a label may carry a dot, as in east.acme, but no label is above
	// or below another), each with the hosting organization and the four name servers of
	// the application's zone there.
	Labels map[string]Delegation `json:"labels,omitempty"`
}

// Delegation is where a label is handed: the hosting organization that serves the name
// (by its prefix) and the four name servers of the zone it serves it from, as Cloud DNS
// spells them (ns-cloud-a1.googledomains.com., with the trailing dot), read from that
// zone once its apply has made it, since Cloud DNS assigns them and nothing chooses them
// in advance.
type Delegation struct {
	Organization string   `json:"organization"`
	NameServers  []string `json:"nameServers"`
}

// Root reports whether the placement is a root organization's.
func (p *Placement) Root() bool {
	return p.Kind == RootKind
}

// Layers are the organization's layers in apply order, by its kind: the six layers of
// the hosting model, or the bootstrap and 1-dns.
func (p *Placement) Layers() []string {
	if p.Root() {
		return RootLayers
	}

	return HostingLayers
}

// SecondLayer is the layer after the bootstrap, whose identity 0-bootstrap creates and
// whose GitHub provider takes the infrastructure GitHub App's token: 1-org, or 1-dns for
// a root organization.
func (p *Placement) SecondLayer() string {
	if p.Root() {
		return dnsLayer
	}

	return orgLayer
}

// SecondIdentity is the name 0-bootstrap gives the second layer's identities
// (<prefix>-org-gbl-tofu, <prefix>-dns-gbl-tofu).
func (p *Placement) SecondIdentity() string {
	if p.Root() {
		return dnsIdentity
	}

	return orgIdentity
}

// ProjectKeys are the organization's projects by key, in layer order, by its kind: the
// boot project, the shared projects and the environment projects, or the boot project
// alone.
func (p *Placement) ProjectKeys() []string {
	if p.Root() {
		return rootProjectKeys
	}

	return HostingProjectKeys
}

// RerunExample is the substitutions a person starts the apply trigger by hand with, as
// the READMEs and the triggers' comments show them: 2-env for one environment in the
// hosting model, 1-dns for a root organization.
func (p *Placement) RerunExample() string {
	if p.Root() {
		return "_LAYER=" + dnsLayer
	}

	return "_LAYER=" + envLayer + ",_ENVIRONMENT=" + Environments[0]
}

// DomainNames lists the domains a root organization holds zones for, in name order.
func (p *Placement) DomainNames() []string {
	return slices.Sorted(maps.Keys(p.Domains))
}

// LabelNames lists the labels of the domain handed onward, in name order.
func (p *Placement) LabelNames(domain string) []string {
	return slices.Sorted(maps.Keys(p.Domains[domain].Labels))
}

// ZoneProject is where a root organization's zones are: the boot project, empty until
// the placement records the seed's value (projects.boot).
func (p *Placement) ZoneProject() string {
	return p.Projects[bootProject]
}

// ZoneName is the name 1-dns gives a domain's zone in the boot project:
// <prefix>-boot-gbl-dns-<the domain with hyphens for its dots>, the shape 2-net gives a
// vanity domain's zone in the network project.
func (p *Placement) ZoneName(domain string) string {
	return p.Prefix + "-" + bootProject + "-gbl-dns-" + strings.ReplaceAll(domain, ".", "-")
}

// validateKind refuses a kind that is neither of the two.
func (p *Placement) validateKind() error {
	if p.Kind != "" && !slices.Contains(Kinds, p.Kind) {
		return errors.Newf("kind %q is not one of %s: a hosting organization holds applications in its environments (the default, left out), a root organization holds DNS zones alone", p.Kind, strings.Join(Kinds, ", "))
	}

	return nil
}

// validateRoot holds a root organization's placement to what its two layers render
// from: the fields every organization has, one region (the boot project's, where the
// connection, the triggers and the state bucket are), the domains and their labels, and
// none of the fields that describe applications or environments, which it has neither of.
func (p *Placement) validateRoot() error {
	for name, value := range map[string]string{
		"appsDomain": p.AppsDomain, "spanner.config": p.Spanner.Config, "githubReleaseAppSlug": p.GithubReleaseAppSlug,
		"githubInfrastructureTeam": p.GithubInfrastructureTeam, "ciLargeRunner": p.CiLargeRunner, "ciLargeRunnerSize": p.CiLargeRunnerSize,
	} {
		if value != "" {
			return errors.Newf("%s is set, and a root organization has no use for it: it holds DNS zones alone, no applications and no environments", name)
		}
	}
	for name, set := range map[string]bool{
		"vanityDomains": len(p.VanityDomains) > 0, "teamGroups": len(p.TeamGroups) > 0, "entitlementDurations": len(p.EntitlementDurations) > 0,
		"outlierDetection": p.OutlierDetection != nil, "cloudArmor": p.CloudArmor != nil, "ciLargeRunnerMaximum": p.CiLargeRunnerMaximum != 0,
		"applications": len(p.Applications) > 0, "registration": len(p.Registration) > 0, "teardown": p.Teardown,
		"outOfService": len(p.OutOfService) > 0, "hostnames": len(p.Hostnames) > 0,
	} {
		if set {
			return errors.Newf("%s is set, and a root organization has no use for it: it holds DNS zones alone, no applications and no environments", name)
		}
	}
	if len(p.Regions) != 1 {
		return errors.Newf("regions has %d entries; a root organization runs in one, where its connection, its triggers and its state bucket are", len(p.Regions))
	}
	if err := p.validateKeyHolding(); err != nil {
		return err
	}

	return p.validateDomains()
}

// validateDomains refuses a domain that is not a bare lowercase domain, the
// organization's domain (whose mail is on it; a label of it is fine) or a name above it,
// one under another listed (one zone serves a domain and everything under it), one whose
// zone's name would not fit, and a label handed onward that breaks the label rule, is
// above or below another, names no hosting organization or this one, or names other than
// four name servers as Cloud DNS spells them.
func (p *Placement) validateDomains() error {
	domains := p.DomainNames()
	for i, d := range domains {
		if !derive.HostnameOK(d) {
			return errors.Newf("domains names %q, which is not a bare lowercase domain such as example.com", d)
		}
		switch {
		case d == p.OrganizationDomain:
			return errors.Newf("domains names %q, which is the organization's domain, whose mail is on it: a root organization holds a label of it (apps.%s) or another domain, and the identity domain stays at its registrar's DNS", d, d)
		case strings.HasSuffix(p.OrganizationDomain, "."+d):
			return errors.Newf("domains names %q, which is above the organization's domain %s: one zone would serve both", d, p.OrganizationDomain)
		}
		for _, earlier := range domains[:i] {
			if strings.HasSuffix(d, "."+earlier) || strings.HasSuffix(earlier, "."+d) {
				return errors.Newf("domains lists %q and %q, one under the other: the zone of a domain serves everything under it, so the one above is enough", earlier, d)
			}
		}
		if name := p.ZoneName(d); len(name) > resourceNameLimit {
			return errors.Newf("domains names %q, whose zone %s would be %d characters; Cloud DNS takes at most %d in a zone's name, so a domain here is at most %d characters", d, name, len(name), resourceNameLimit, resourceNameLimit-(len(name)-len(d)))
		}
		if err := p.validateLabels(d); err != nil {
			return err
		}
	}

	return nil
}

// validateLabels holds the labels of one domain to the rules: each label a DNS label or
// labels joined with dots, none above or below another, each handed to a hosting
// organization by its prefix that is not this organization, on exactly four name servers,
// each a lowercase host name with the trailing dot Cloud DNS writes, none twice.
func (p *Placement) validateLabels(domain string) error {
	labels := p.LabelNames(domain)
	for i, label := range labels {
		where := "domains." + domain + ".labels." + label
		for part := range strings.SplitSeq(label, ".") {
			if !derive.HostnameLabelOK(part) {
				return errors.Newf("%s: label %q is not %s (joined with dots for a name more than one label below the domain)", where, label, derive.HostnameLabelRule)
			}
		}
		for _, earlier := range labels[:i] {
			if strings.HasSuffix(label, "."+earlier) || strings.HasSuffix(earlier, "."+label) {
				return errors.Newf("domains.%s lists the labels %q and %q, one under the other: a label handed onward takes everything under it with it, so the one above is enough", domain, earlier, label)
			}
		}
		if err := p.validateDelegation(where, p.Domains[domain].Labels[label]); err != nil {
			return err
		}
	}

	return nil
}

// validateDelegation holds one label's delegation to the rules.
func (p *Placement) validateDelegation(where string, d Delegation) error {
	switch {
	case !prefixRE.MatchString(d.Organization):
		return errors.Newf("%s names organization %q, which is not a bedrock organization's prefix (2 to 4 lowercase alphanumeric characters starting with a letter): the hosting organization whose zone serves the name", where, d.Organization)
	case d.Organization == p.Prefix:
		return errors.Newf("%s names organization %s, which is this organization: a root organization hosts no application, and a label is handed to the hosting organization whose zone serves the name", where, d.Organization)
	case len(d.NameServers) != nameServerCount:
		return errors.Newf("%s names %d name servers, not %d: the four name servers Cloud DNS assigned the application's zone in the hosting organization, as bedrock domain check prints them there", where, len(d.NameServers), nameServerCount)
	}
	for i, ns := range d.NameServers {
		if !strings.HasSuffix(ns, ".") || !derive.HostnameOK(strings.TrimSuffix(ns, ".")) {
			return errors.Newf("%s names the name server %q, which is not a lowercase host name with the trailing dot Cloud DNS writes (ns-cloud-a1.googledomains.com.)", where, ns)
		}
		if slices.Contains(d.NameServers[:i], ns) {
			return errors.Newf("%s names the name server %s twice", where, ns)
		}
	}

	return nil
}
