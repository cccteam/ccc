// root.go is the root kind of organization: the bedrock organization at the top of a
// Google Cloud organization that holds what every hosting organization there shares, and
// creates the hosting organizations. It has no layers and no terraform. bedrock org
// bootstrap makes it once, by API, as the break-glass account: its folder at the
// organization node, its root project with the three GitHub Apps' keys and the GitHub
// Bot Account's token as secrets, the Cloud Build connection and the two triggers, the
// state bucket, a zone per domain delegated to it, and its repository on GitHub with
// the default branch ruled. From then on its apply trigger keeps the record true (the
// aliases the pins name, the zones, the labels handed onward, the runner group) and
// creates each hosting organization the record lists without a folder yet, from a pull
// request, as the creating identity.

package org

import (
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// The kinds of organization a placement describes (Placement.Kind): hosting, the
// default, which holds applications in its environments; and root, which holds the
// GitHub Apps' keys, the zones of the domains delegated to it and the records of the
// hosting organizations it creates.
const (
	HostingKind = "hosting"
	RootKind    = "root"
)

// Kinds are the kinds a placement may name.
var Kinds = []string{HostingKind, RootKind}

// orgIdentity is the name 0-bootstrap gives a hosting organization's second layer's
// identities (<prefix>-org-gbl-tofu, <prefix>-org-gbl-plan).
const orgIdentity = "org"

// RootProjectKey is the key of a root organization's one project in placement.json
// (projects.root): the root project, which holds the secrets, the zones, the connection,
// the triggers and the state bucket.
const RootProjectKey = "root"

// rootProjectKeys are a root organization's projects by key: the root project alone.
var rootProjectKeys = []string{RootProjectKey}

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
	// Records are the record sets the zone carries beside the labels' NS records and the
	// owners' verification tokens, declared by the person who knows what the domain
	// needs there: a verification TXT at the apex of a domain delegated whole, the mail
	// records of a Workspace domain (MX, SPF, DKIM, DMARC), a CAA record. The apply
	// writes each as declared and rewrites one the zone holds differently; a record
	// taken out stays until a person deletes it, as the apply deletes no zone.
	Records []Record `json:"records,omitempty"`
}

// Record is one record set a zone carries for the person who declared it: its name
// under the domain ("@" for the apex, else a label, or labels joined with dots), its
// type, its time to live in seconds (300 unless given) and its values as Cloud DNS
// takes them, except that a TXT value is quoted by the apply when it is not already.
type Record struct {
	Name string   `json:"name"`
	Type string   `json:"type"`
	TTL  int64    `json:"ttl,omitempty"`
	Data []string `json:"data"`
}

// RecordApex names the domain itself in a record (records[].name).
const RecordApex = "@"

// recordLabelRE is one part of a declared record's name: a DNS label, which may open
// with the underscore of a service name (_dmarc, _domainkey, _acme-challenge).
var recordLabelRE = regexp.MustCompile(`^_?[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// recordTypes are the types a declared record may have: what a domain needs to prove
// itself, to send and receive mail, and to answer an address. NS is not among them:
// the zone's own NS records are Cloud DNS's, and the others are the labels handed
// onward (labels).
var recordTypes = []string{"A", "AAAA", "CAA", recordTypeCNAME, "MX", "SRV", "TXT"}

// recordTypeCNAME is the one record type DNS keeps off the apex and holds to one value.
const recordTypeCNAME = "CNAME"

// Delegation is where a label is handed: the hosting organization that serves the name
// (by its prefix) and the four name servers of the zone it serves it from, as Cloud DNS
// spells them (ns-cloud-a1.googledomains.com., with the trailing dot), read from that
// zone once its apply has made it, since Cloud DNS assigns them and nothing chooses them
// in advance.
type Delegation struct {
	Organization string   `json:"organization"`
	NameServers  []string `json:"nameServers"`
}

// IsRoot reports whether the placement is a root organization's.
func (p *Placement) IsRoot() bool {
	return p.Kind == RootKind
}

// Layers are the organization's layers in apply order: the six layers of the hosting
// model; a root organization has none, since bedrock keeps it by API.
func (p *Placement) Layers() []string {
	if p.IsRoot() {
		return nil
	}

	return HostingLayers
}

// ProjectKeys are the organization's projects by key, in layer order, by its kind: the
// boot project, the shared projects and the environment projects, or the root project
// alone.
func (p *Placement) ProjectKeys() []string {
	if p.IsRoot() {
		return rootProjectKeys
	}

	return HostingProjectKeys
}

// RerunExample is the substitutions a person starts the apply trigger by hand with, as
// the READMEs and the triggers' comments show them: 2-env for one environment in the
// hosting model. A root organization's apply trigger takes none: it applies the whole
// record every run.
func (p *Placement) RerunExample() string {
	if p.IsRoot() {
		return ""
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

// ZoneProject is where a root organization's zones are: the root project, empty until
// the bootstrap records it (projects.root).
func (p *Placement) ZoneProject() string {
	return p.Projects[RootProjectKey]
}

// ZoneName is the name a root organization gives a domain's zone in the root project:
// <prefix>-root-gbl-dns-<the domain with hyphens for its dots>, the shape 2-net gives a
// vanity domain's zone in the network project.
func (p *Placement) ZoneName(domain string) string {
	return p.Prefix + "-" + RootProjectKey + "-gbl-dns-" + strings.ReplaceAll(domain, ".", "-")
}

// validateKind refuses a kind that is neither of the two.
func (p *Placement) validateKind() error {
	if p.Kind != "" && !slices.Contains(Kinds, p.Kind) {
		return errors.Newf("kind %q is not one of %s: a hosting organization holds applications in its environments (the default, left out), a root organization holds the GitHub Apps' keys, the zones of the domains delegated to it and the hosting organizations' records", p.Kind, strings.Join(Kinds, ", "))
	}

	return nil
}

// billingAccountRE is a billing account's id as Cloud Billing spells it.
var billingAccountRE = regexp.MustCompile(`^[0-9A-F]{6}-[0-9A-F]{6}-[0-9A-F]{6}$`)

// ValidateDraft holds the placement bedrock org bootstrap drafts, from its flags, the
// discovery and the prompts, to the rules before anything is made: what Validate holds a
// finished placement to, less what the bootstrap itself records (the project and the
// bucket, the apps and their keys, the Bot Account and the Cloud Build authorization).
func (p *Placement) ValidateDraft() error {
	if !prefixRE.MatchString(p.Prefix) {
		return errors.Newf("prefix %q is not 2 to 4 lowercase alphanumeric characters starting with a letter", p.Prefix)
	}
	if !p.IsRoot() {
		return errors.New("the bootstrap drafts a root organization (kind root)")
	}
	for name, value := range map[string]string{
		"organizationDomain": p.OrganizationDomain, "organizationId": p.OrganizationID, "billingAccount": p.BillingAccount,
		"githubOrganization": p.GithubOrganization, "sourceRepo": p.SourceRepo, "githubDefaultBranch": p.GithubDefaultBranch, "breakGlassAccount": p.BreakGlassAccount,
	} {
		if strings.TrimSpace(value) == "" {
			return errors.Newf("%s is empty", name)
		}
	}
	if !projectNumberRE.MatchString(p.OrganizationID) {
		return errors.Newf("organizationId %q is not an organization's numeric id", p.OrganizationID)
	}
	if !billingAccountRE.MatchString(p.BillingAccount) {
		return errors.Newf("billingAccount %q is not a billing account's id (XXXXXX-XXXXXX-XXXXXX, uppercase hexadecimal)", p.BillingAccount)
	}
	if !githubLoginRE.MatchString(p.GithubOrganization) {
		return errors.Newf("githubOrganization %q is not a GitHub organization's login", p.GithubOrganization)
	}
	if len(p.Regions) != 1 || p.Regions[0].Name == "" || !regionCodeRE.MatchString(p.Regions[0].Code) {
		return errors.New("regions needs one entry, the region the connection, the triggers and the state bucket are in, with its name and a three-character code (us-central1, uc1)")
	}
	for _, validate := range []func() error{p.validateDirectoryCustomer, p.validateAppNames, p.validateApprovingReviews, p.validateKeyHolding, p.validateCiLargeRunner, p.validateDomains} {
		if err := validate(); err != nil {
			return err
		}
	}
	for k, v := range p.Labels {
		if !labelRE.MatchString(k) || v == "" {
			return errors.Newf("label %q = %q is not a lowercase key with a value", k, v)
		}
	}

	return nil
}

// validateRoot holds a root organization's placement to what the bootstrap and the
// apply keep: the fields every organization has, one region (the root project's, where
// the connection, the triggers and the state bucket are), the domains and their labels,
// and none of the fields that describe applications or environments, which it has
// neither of.
func (p *Placement) validateRoot() error {
	for name, value := range map[string]string{
		"appsDomain": p.AppsDomain, "spanner.config": p.Spanner.Config, "githubInfrastructureTeam": p.GithubInfrastructureTeam,
	} {
		if value != "" {
			return errors.Newf("%s is set, and a root organization has no use for it: it holds no application and no environment", name)
		}
	}
	for name, set := range map[string]bool{
		"vanityDomains": len(p.VanityDomains) > 0, "teamGroups": len(p.TeamGroups) > 0, "entitlementDurations": len(p.EntitlementDurations) > 0,
		"outlierDetection": p.OutlierDetection != nil, "cloudArmor": p.CloudArmor != nil,
		"applications": len(p.Applications) > 0, "registration": len(p.Registration) > 0, "teardown": p.Teardown,
		"outOfService": len(p.OutOfService) > 0, "hostnames": len(p.Hostnames) > 0,
	} {
		if set {
			return errors.Newf("%s is set, and a root organization has no use for it: it holds no application and no environment", name)
		}
	}
	if p.ZoneReplacement {
		return errors.New("zoneReplacement is set, and a root organization has no use for it: its apply never deletes a zone (a domain taken out of domains leaves its zone standing, reported as a finding); to make a zone again, delete it by hand in the root project (gcloud dns managed-zones delete) and the next apply makes it, on new name servers the domain's registrar is then pointed at")
	}
	if len(p.Regions) != 1 {
		return errors.Newf("regions has %d entries; a root organization runs in one, where its connection, its triggers and its state bucket are", len(p.Regions))
	}
	if err := p.validateKeyHolding(); err != nil {
		return err
	}
	if err := p.validateCiLargeRunner(); err != nil {
		return err
	}

	return p.validateDomains()
}

// validateDomains refuses a domain that is not a bare lowercase domain, one under another
// listed (one zone serves a domain and everything under it), one whose zone's name would
// not fit, a label handed onward that breaks the label rule, is above or below another,
// names no hosting organization or this one, or names other than four name servers as
// Cloud DNS spells them, and a record that breaks the record rule. The organization's
// own domain is a domain like any other here: whether it is delegated whole or by a
// label is the live domain check's call, from what its apex answers, not the placement's.
func (p *Placement) validateDomains() error {
	domains := p.DomainNames()
	for i, d := range domains {
		if !derive.HostnameOK(d) {
			return errors.Newf("domains names %q, which is not a bare lowercase domain such as example.com", d)
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
		if err := p.validateRecords(d); err != nil {
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

// validateRecords holds the records one domain declares to the rules: a name that is
// the apex (@) or a label (or labels joined with dots) that is not handed onward nor
// under one, since the hosting organization's zone answers for those; a type among the
// ones a person declares (never NS); a time to live that is not negative; at least one
// value; no CNAME at the apex and none with more than one value; and no name and type
// twice, since several values go in one record's data.
func (p *Placement) validateRecords(domain string) error {
	seen := map[string]bool{}
	for i, r := range p.Domains[domain].Records {
		if r.Name != RecordApex {
			for part := range strings.SplitSeq(r.Name, ".") {
				if !recordLabelRE.MatchString(part) {
					return errors.Newf("domains.%s.records[%d]: name %q is not %q for the apex or a DNS label of lowercase letters, digits and hyphens, with a leading underscore for a service name such as _dmarc (joined with dots for a name more than one label below the domain)", domain, i, r.Name, RecordApex)
				}
			}
			for _, label := range p.LabelNames(domain) {
				if r.Name == label || strings.HasSuffix(r.Name, "."+label) {
					return errors.Newf("domains.%s.records[%d]: name %q is handed onward (labels.%s): the hosting organization's zone answers for it and everything under it", domain, i, r.Name, label)
				}
			}
		}
		switch {
		case !slices.Contains(recordTypes, r.Type):
			return errors.Newf("domains.%s.records[%d]: type %q is not one of %s (NS records are the labels handed onward, and the zone's own)", domain, i, r.Type, strings.Join(recordTypes, ", "))
		case r.TTL < 0:
			return errors.Newf("domains.%s.records[%d]: ttl %d is negative; leave it out for 300 seconds", domain, i, r.TTL)
		case len(r.Data) == 0:
			return errors.Newf("domains.%s.records[%d]: data is empty; a record set carries at least one value", domain, i)
		case r.Type == recordTypeCNAME && r.Name == RecordApex:
			return errors.Newf("domains.%s.records[%d]: a CNAME at the apex is not allowed by DNS; the apex holds the zone's own records", domain, i)
		case r.Type == recordTypeCNAME && len(r.Data) > 1:
			return errors.Newf("domains.%s.records[%d]: a CNAME carries one value, not %d", domain, i, len(r.Data))
		case seen[r.Name+"/"+r.Type]:
			return errors.Newf("domains.%s.records[%d]: %s %s is declared twice; several values go in one record's data", domain, i, r.Name, r.Type)
		}
		seen[r.Name+"/"+r.Type] = true
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

// RegionCode is the three-character code bedrock's names carry for a region, from the
// region's name: the initial of each word and the number (us-central1: uc1; us-west3:
// uw3; europe-west4: ew4; northamerica-northeast1: nn1). A name of another shape is
// refused.
func RegionCode(name string) (string, error) {
	i := strings.IndexFunc(name, func(r rune) bool { return r >= '0' && r <= '9' })
	if i <= 0 {
		return "", errors.Newf("%q is not a region name of the shape <area>-<direction><number> (us-central1)", name)
	}
	words := strings.Split(name[:i], "-")
	if len(words) != 2 || words[0] == "" || words[1] == "" {
		return "", errors.Newf("%q is not a region name of the shape <area>-<direction><number> (us-central1)", name)
	}
	code := words[0][:1] + words[1][:1] + name[i:]
	if !regionCodeRE.MatchString(code) {
		return "", errors.Newf("%q gives the code %q, not three characters (us-central1 gives uc1): choose the code by hand in placement.json", name, code)
	}

	return code, nil
}
