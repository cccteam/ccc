// check.go is domain check: whether the apps domain resolves to the network layer's zone,
// and, where it does not, the records to add and the place to add them. What the zone
// holds comes from Cloud DNS; what the world sees comes from the resolver. Which shape the
// domain has (registered by the network layer, delegated at its apex from a registrar
// elsewhere, or delegated as a label of a domain served elsewhere) is not configured: it
// is what the answers show. A registered domain's name servers are read from the answers
// too, never trusted from the registration: after its zone is made again, a registration
// still names the old set until it is pointed at the zone in Cloud Domains.

package domain

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/net/publicsuffix"
	dns "google.golang.org/api/dns/v1"
	"google.golang.org/api/googleapi"
)

const (
	// typeA, typeAAAA, typeCNAME, typeMX and typeNS are the record types the check reads.
	typeA     = "A"
	typeAAAA  = "AAAA"
	typeCNAME = "CNAME"
	typeMX    = "MX"
	typeNS    = "NS"
	// authorizationPrefix starts the name of the record Certificate Manager's DNS
	// authorization asks for: _acme-challenge.<domain>., or _acme-challenge_<id>.<domain>.
	// for an authorization of the per-project kind.
	authorizationPrefix = "_acme-challenge"
	// netValues names the network layer's placement in a message.
	netValues = DefaultLayer + "/" + tfvarsFile
	// squarespacePath is where Squarespace Domains, the registrar of record behind Cloud
	// Domains and a common one besides, sets a domain's name servers; it has no API.
	squarespacePath = "At Squarespace Domains: the domain's DNS settings, Domain Nameservers, Use Custom Nameservers (two to thirteen servers); the change takes up to 48 hours to take effect."
	// serverMisbehaving is the Go resolver's text for an answer of SERVFAIL: the servers
	// a name is delegated to do not answer for it, as when its zone was made again on
	// other name servers and the delegation still names the old ones.
	serverMisbehaving = "server misbehaving"
)

// Zone is a managed zone as Cloud DNS describes it.
type Zone struct {
	// DNSName is the domain the zone serves, fully qualified (example.com.).
	DNSName string
	// NameServers are the name servers Cloud DNS assigned the zone, as it spells them.
	NameServers []string
}

// Record is one record set of a zone: its name and type, and its data, as Cloud DNS
// spells them (names fully qualified, with the trailing dot).
type Record struct {
	Name string
	Type string
	Data []string
}

// Line is the record as a line to paste: name, type and the first datum.
func (r Record) Line() string {
	data := ""
	if len(r.Data) > 0 {
		data = r.Data[0]
	}

	return r.Name + " " + r.Type + " " + data
}

// ZoneReader reads a managed zone and its record sets. NewCloudDNS is the real one, over
// the Cloud DNS API; tests pass a fake.
type ZoneReader interface {
	// ManagedZone is the named zone of the project, or nil when the project holds no
	// such zone.
	ManagedZone(ctx context.Context, project, zone string) (*Zone, error)
	// RecordSets are every record set of the zone.
	RecordSets(ctx context.Context, project, zone string) ([]Record, error)
	// Close releases the connection.
	Close() error
}

// ZoneReaderFunc opens a ZoneReader.
type ZoneReaderFunc func(ctx context.Context) (ZoneReader, error)

// Resolver answers the public questions the check asks. *net.Resolver is the real one;
// tests pass a fake.
type Resolver interface {
	LookupNS(ctx context.Context, name string) ([]*net.NS, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupCNAME(ctx context.Context, host string) (string, error)
}

// Lookups are what the check reads through: Cloud DNS for what the zone holds, opened by
// OpenZones, and Resolver for what the world sees.
type Lookups struct {
	OpenZones ZoneReaderFunc
	Resolver  Resolver
}

// DefaultLookups are the real ones: the Cloud DNS API with the run's Google credentials,
// and the system's resolver.
func DefaultLookups() *Lookups {
	return &Lookups{OpenZones: NewCloudDNS, Resolver: net.DefaultResolver}
}

// cloudDNS is the ZoneReader over the Cloud DNS API.
type cloudDNS struct {
	service *dns.Service
}

// NewCloudDNS opens the Cloud DNS API with Application Default Credentials; without any,
// the open fails and says so.
func NewCloudDNS(ctx context.Context) (ZoneReader, error) {
	service, err := dns.NewService(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "dns.NewService()")
	}

	return &cloudDNS{service: service}, nil
}

// ManagedZone reads the zone (managedZones.get); a zone the project lacks is nil.
func (c *cloudDNS) ManagedZone(ctx context.Context, project, zone string) (*Zone, error) {
	z, err := c.service.ManagedZones.Get(project, zone).Context(ctx).Do()
	if err != nil {
		var apiErr *googleapi.Error
		if errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound {
			return nil, nil
		}

		return nil, errors.Wrapf(err, "dns.ManagedZonesService.Get(): %s in %s", zone, project)
	}

	return &Zone{DNSName: z.DnsName, NameServers: z.NameServers}, nil
}

// RecordSets lists the zone's record sets (resourceRecordSets.list), every page.
func (c *cloudDNS) RecordSets(ctx context.Context, project, zone string) ([]Record, error) {
	var records []Record
	err := c.service.ResourceRecordSets.List(project, zone).Pages(ctx, func(page *dns.ResourceRecordSetsListResponse) error {
		for _, rr := range page.Rrsets {
			records = append(records, Record{Name: rr.Name, Type: rr.Type, Data: rr.Rrdatas})
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "dns.ResourceRecordSetsService.List(): %s in %s", zone, project)
	}

	return records, nil
}

// Close has nothing to release: the REST client holds no connection of its own.
func (*cloudDNS) Close() error {
	return nil
}

// CheckRequest is what domain check was asked to check.
type CheckRequest struct {
	// Domain is the apps domain, as the organization's placement names it.
	Domain string
	// Project is the network project, and Zone the name the network layer gives the
	// apps domain's zone there.
	Project string
	Zone    string
	// Dir is the infrastructure repository's root, whose network layer placement lists
	// the domains the layer registers.
	Dir string
	// ZoneReplacement reports zoneReplacement set in the organization's placement: the
	// zones rendered without prevent_destroy for a recreation, which the check says can
	// be cleared once the domain points at the zone.
	ZoneReplacement bool
}

// Delegation is what the answers show about the apps domain.
type Delegation int

const (
	// Delegated is a domain that answers the zone's name servers and no other.
	Delegated Delegation = iota
	// Registering is a domain the network layer registers (its registrations) for which
	// the zone's name servers do not answer yet.
	Registering
	// ApexUndelegated is a registrable domain (an apex, such as example.com) whose
	// registrar names other name servers, or none.
	ApexUndelegated
	// LabelUndelegated is a label of a domain served elsewhere (apps.example.com) for
	// which that domain's DNS provider holds no NS records pointing at the zone.
	LabelUndelegated
	// Refused is an apex, not delegated, that answers records that are not the zone's: a
	// domain with other DNS on it, which is never delegated whole.
	Refused
	// Repoint is a domain the network layer registers whose registration names name
	// servers that are not the zone's, as after the zone was made again on other ones:
	// it answers another set, or the servers it is delegated to do not answer for it.
	// The apply does not change the name servers of a registration that exists, so the
	// registration is pointed at the zone in Cloud Domains.
	Repoint
)

// Report is what the check found.
type Report struct {
	// Domain is the apps domain; Project and Zone are where its zone is.
	Domain  string
	Project string
	Zone    string
	// ZoneName is the domain as the zone spells it (fully qualified), and NameServers
	// the zone's name servers.
	ZoneName    string
	NameServers []string
	// Answered are the name servers the world sees for the domain; Lame says the
	// servers it is delegated to do not answer for it (Repoint), so none are seen.
	Answered []string
	Lame     bool
	// Delegation is what the answers show.
	Delegation Delegation
	// Parent is the domain served elsewhere that a label belongs to, and Label the
	// label (LabelUndelegated).
	Parent string
	Label  string
	// Foreign are the records the apex answers that are not the zone's (Refused), each
	// as type and data.
	Foreign []string
	// Authorization is the zone's authorization record, and AuthorizationResolves
	// whether the world sees it.
	Authorization         Record
	AuthorizationResolves bool
	// ZoneReplacement reports zoneReplacement set in the organization's placement.
	ZoneReplacement bool
}

// appsZone is what the zone holds that the check compares against.
type appsZone struct {
	zone          *Zone
	address       string
	authorization Record
}

// Check reads the apps domain's zone through Cloud DNS, resolves the domain's delegation
// and the authorization record, and reports what is in place and what is missing. It is
// refused when the domain is not a bare lowercase name, when the zone does not exist or
// serves another domain, and when the zone lacks a record the network layer's apply
// creates (the apex and wildcard addresses, the authorization record); an answer the
// resolver fails to give, other than "no such record", stops it too, except the servers'
// failure to answer for a domain the network layer registers, which is a registration
// to point at the zone (Repoint).
func Check(ctx context.Context, l *Lookups, req CheckRequest) (*Report, error) {
	if err := Validate(req.Domain); err != nil {
		return nil, err
	}
	registered, err := registeredHere(req.Dir, req.Domain)
	if err != nil {
		return nil, err
	}
	z, err := readZone(ctx, l.OpenZones, req)
	if err != nil {
		return nil, err
	}
	r := &Report{
		Domain: req.Domain, Project: req.Project, Zone: req.Zone,
		ZoneName: z.zone.DNSName, NameServers: z.zone.NameServers, Authorization: z.authorization,
		ZoneReplacement: req.ZoneReplacement,
	}
	r.Answered, err = nameServers(ctx, l.Resolver, req.Domain)
	r.Lame = registered && serverFailure(err)
	if err != nil && !r.Lame {
		return nil, err
	}
	switch {
	case r.Lame:
		r.Delegation = Repoint
	case sameNames(r.Answered, r.NameServers):
		r.Delegation = Delegated
	case registered && len(r.Answered) == 0:
		r.Delegation = Registering
	case registered:
		r.Delegation = Repoint
	default:
		if err := r.classify(ctx, l.Resolver, z.address); err != nil {
			return nil, err
		}
	}
	r.AuthorizationResolves, err = resolvesTo(ctx, l.Resolver, z.authorization)
	if err != nil && (r.Delegation == Delegated || !serverFailure(err)) {
		return nil, err
	}

	return r, nil
}

// serverFailure reports a lookup that the servers answered with a failure (SERVFAIL),
// which a domain whose delegation names servers that do not serve its zone gets.
func serverFailure(err error) bool {
	var dnsErr *net.DNSError

	return errors.As(err, &dnsErr) && !dnsErr.IsNotFound && !dnsErr.IsTimeout && dnsErr.Err == serverMisbehaving
}

// registeredHere reports whether the network layer's placement lists the domain among
// its registrations; a repository without that placement registers nothing.
func registeredHere(dir, name string) (bool, error) {
	file := filepath.Join(dir, DefaultLayer, tfvarsFile)
	src, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return false, errors.Wrap(err, "os.ReadFile()")
	}
	p, err := parsePlacement(src, file)
	if err != nil {
		return false, err
	}
	names, err := p.registrations()
	if err != nil {
		return false, err
	}

	return slices.Contains(names, name), nil
}

// readZone reads the zone and the three records the check compares against: the apex
// and wildcard addresses and the authorization record.
func readZone(ctx context.Context, open ZoneReaderFunc, req CheckRequest) (*appsZone, error) {
	reader, err := open(ctx)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	zone, err := reader.ManagedZone(ctx, req.Project, req.Zone)
	if err != nil {
		return nil, err
	}
	if zone == nil {
		return nil, errors.Newf("no zone %s in %s: the apply of %s creates it; apply %s, then check again", req.Zone, req.Project, DefaultLayer, DefaultLayer)
	}
	if !sameName(zone.DNSName, req.Domain) {
		return nil, errors.Newf("the zone %s in %s serves %s, not the apps domain %s: set apps_domain in %s to appsDomain in placement.json and apply %s, then check again", req.Zone, req.Project, zone.DNSName, req.Domain, netValues, DefaultLayer)
	}
	records, err := reader.RecordSets(ctx, req.Project, req.Zone)
	if err != nil {
		return nil, err
	}
	z := &appsZone{zone: zone}
	var wildcard bool
	for _, rec := range records {
		switch {
		case rec.Type == typeA && sameName(rec.Name, req.Domain) && len(rec.Data) > 0:
			z.address = rec.Data[0]
		case rec.Type == typeA && sameName(rec.Name, "*."+req.Domain):
			wildcard = true
		case rec.Type == typeCNAME && strings.HasPrefix(rec.Name, authorizationPrefix) && len(rec.Data) > 0:
			z.authorization = rec
		}
	}
	var lacks []string
	if z.address == "" {
		lacks = append(lacks, "the A record of "+req.Domain)
	}
	if !wildcard {
		lacks = append(lacks, "the A record of *."+req.Domain)
	}
	if z.authorization.Name == "" {
		lacks = append(lacks, "the certificate's authorization record ("+authorizationPrefix+")")
	}
	if len(lacks) > 0 {
		return nil, errors.Newf("the zone %s in %s lacks %s: the apply of %s creates them; apply %s, then check again", req.Zone, req.Project, strings.Join(lacks, ", "), DefaultLayer, DefaultLayer)
	}

	return z, nil
}

// classify tells the shapes of a domain that does not answer the zone's name servers
// apart: a label of a domain served elsewhere, an apex that carries other DNS (refused),
// or an apex with nothing else on it.
func (r *Report) classify(ctx context.Context, resolver Resolver, address string) error {
	apex, err := publicsuffix.EffectiveTLDPlusOne(r.Domain)
	if err != nil {
		return errors.Newf("%s is not a domain that can be registered or a label of one: %v", r.Domain, err)
	}
	if apex != r.Domain {
		r.Delegation = LabelUndelegated
		r.Label, r.Parent, _ = strings.Cut(r.Domain, ".")

		return nil
	}
	hosts, err := resolver.LookupHost(ctx, r.Domain)
	if err := answered(err, "the addresses of "+r.Domain); err != nil {
		return err
	}
	for _, h := range hosts {
		if h == address {
			continue
		}
		typ := typeA
		if ip := net.ParseIP(h); ip != nil && ip.To4() == nil {
			typ = typeAAAA
		}
		r.Foreign = append(r.Foreign, typ+" "+h)
	}
	mx, err := resolver.LookupMX(ctx, r.Domain)
	if err := answered(err, "the mail servers of "+r.Domain); err != nil {
		return err
	}
	for _, m := range mx {
		r.Foreign = append(r.Foreign, fmt.Sprintf("%s %d %s", typeMX, m.Pref, m.Host))
	}
	r.Delegation = ApexUndelegated
	if len(r.Foreign) > 0 {
		r.Delegation = Refused
	}

	return nil
}

// nameServers are the name servers the world sees for the domain; none when it has none.
func nameServers(ctx context.Context, resolver Resolver, name string) ([]string, error) {
	ns, err := resolver.LookupNS(ctx, name)
	if err := answered(err, "the name servers of "+name); err != nil {
		return nil, err
	}
	hosts := make([]string, 0, len(ns))
	for _, n := range ns {
		hosts = append(hosts, n.Host)
	}

	return hosts, nil
}

// resolvesTo reports whether the record's name resolves, through its CNAME, to the
// record's data.
func resolvesTo(ctx context.Context, resolver Resolver, rec Record) (bool, error) {
	target, err := resolver.LookupCNAME(ctx, rec.Name)
	if err := answered(err, "the record "+rec.Name); err != nil {
		return false, err
	}

	return target != "" && len(rec.Data) > 0 && sameName(target, rec.Data[0]), nil
}

// answered passes a lookup that answered, and one that found no such record; any other
// failure stops the check with what was asked.
func answered(err error, what string) error {
	if err == nil {
		return nil
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return nil
	}

	return errors.Wrapf(err, "resolving %s", what)
}

// sameName reports whether two domain names are one, whatever their case and trailing
// dot.
func sameName(a, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(b, "."))
}

// sameNames reports whether two lists name the same hosts, in any order: none missing
// from either, none besides.
func sameNames(a, b []string) bool {
	if len(a) == 0 {
		return false
	}

	return slices.Equal(hostSet(a), hostSet(b))
}

// hostSet is the names lowercased, without the trailing dot, sorted and each once.
func hostSet(names []string) []string {
	set := make([]string, 0, len(names))
	for _, n := range names {
		set = append(set, strings.ToLower(strings.TrimSuffix(n, ".")))
	}
	slices.Sort(set)

	return slices.Compact(set)
}

// Passed reports whether nothing is missing: the domain answers the zone's name servers
// and the authorization record resolves.
func (r *Report) Passed() bool {
	return r.Delegation == Delegated && r.AuthorizationResolves
}

// Write prints what the check found and, for what is missing, the place to act and the
// records to add there, and, when zoneReplacement is set and nothing is missing, that it
// can be cleared; each record on its own line exactly as it is pasted: the name
// servers for a registrar as bare host names (a registrar takes host names; the trailing
// dot is a zone file's convention), the NS and CNAME records as zone-file lines, fully
// qualified with the trailing dot.
func (r *Report) Write(w io.Writer) {
	switch r.Delegation {
	case Delegated:
		fmt.Fprintf(w, "%s is delegated to the zone %s in %s: it answers the zone's name servers (%s).\n", r.Domain, r.Zone, r.Project, strings.Join(r.NameServers, ", "))
	case Registering:
		fmt.Fprintf(w, "%s does not answer the zone's name servers yet (%s).\n", r.Domain, r.answeredNow())
		fmt.Fprintf(w, "%s registers it through Cloud Domains (registrations in %s): the apply that registers it gives it the zone's name servers, and they answer once the registration is active.\n", DefaultLayer, netValues)
		fmt.Fprintf(w, "In the registrant's mailbox (registrant_contact in %s): open the registrar's verification mail and follow its link within fifteen days of the registration, or the domain is suspended.\n", netValues)
	case ApexUndelegated:
		fmt.Fprintf(w, "%s does not answer the zone's name servers (%s).\n", r.Domain, r.answeredNow())
		fmt.Fprintf(w, "At the registrar where %s is registered: set its name servers to the zone's %d below, in place of the ones it has. %s\n", r.Domain, len(r.NameServers), squarespacePath)
		for _, ns := range r.NameServers {
			fmt.Fprintln(w, strings.TrimSuffix(ns, "."))
		}
		if len(r.Answered) == 0 {
			fmt.Fprintf(w, "%s answers no name servers at all: if it is not registered yet, bedrock domain add registers it through Cloud Domains in %s instead, with no registrar step.\n", r.Domain, DefaultLayer)
		}
	case Repoint:
		fmt.Fprintf(w, "%s does not answer the zone's name servers (%s).\n", r.Domain, r.answeredNow())
		fmt.Fprintf(w, "%s registers it through Cloud Domains (registrations in %s), and its registration names name servers that are not the zone's, as after the zone was made again: the apply does not change the name servers of a registration that exists (%s/README.md, \"Making a zone again\").\n", DefaultLayer, netValues, DefaultLayer)
		fmt.Fprintf(w, "In Cloud Domains, in the network project %s: point the registration of %s at the zone %s with the command below, or in the console (the Cloud Domains page, %s, Edit DNS details, Cloud DNS, the zone %s, Save). It takes the Cloud Domains Admin role (roles/domains.admin) on the project; the change takes up to 48 hours to be seen everywhere.\n", r.Project, r.Domain, r.Zone, r.Domain, r.Zone)
		fmt.Fprintf(w, "gcloud domains registrations configure dns %s --cloud-dns-zone=%s --project=%s\n", r.Domain, r.Zone, r.Project)
	case LabelUndelegated:
		fmt.Fprintf(w, "%s does not answer the zone's name servers (%s).\n", r.Domain, r.answeredNow())
		fmt.Fprintf(w, "At the DNS provider that serves %s: add NS records for %s pointing at the zone's name servers, in place of any it has, one record per line below (name, type, value). The other records of %s stay as they are.\n", r.Parent, r.Label, r.Parent)
		for _, ns := range r.NameServers {
			fmt.Fprintln(w, r.ZoneName+" "+typeNS+" "+ns)
		}
	case Refused:
		fmt.Fprintf(w, "%s does not answer the zone's name servers (%s), and it answers records that are not the zone's:\n", r.Domain, r.answeredNow())
		for _, f := range r.Foreign {
			fmt.Fprintln(w, f)
		}
		fmt.Fprintln(w, "Refused: a domain that carries other DNS (a website, mail) is never delegated whole, and application hostnames directly under it would each need their own records at its DNS provider, with a wildcard on it for the pull-request environments.")
		fmt.Fprintf(w, "Serve the applications under a label of it instead, such as apps.%s: set appsDomain in placement.json and apps_domain in %s to that name, run bedrock org render, apply %s, and run bedrock domain check again for the NS records to add at the DNS provider that serves %s.\n", r.Domain, netValues, DefaultLayer, r.Domain)

		return
	}
	r.writeAuthorization(w)
	if r.ZoneReplacement && r.Passed() {
		fmt.Fprintln(w, ZoneReplacementCleared)
	}
}

// ZoneReplacementCleared is what the check says when zoneReplacement is set in the
// organization's placement and the domain passes: the recreation the value was set for
// is done, and the rule that refuses the next one is to be written back.
const ZoneReplacementCleared = "zoneReplacement is set in placement.json and the domain points at this zone: clear it and run bedrock org render."

// writeAuthorization says whether the certificate is waiting on the authorization record
// and, when it is, prints the record.
func (r *Report) writeAuthorization(w io.Writer) {
	if r.AuthorizationResolves {
		fmt.Fprintf(w, "The authorization record resolves: the certificate for %s and *.%s is not waiting on it.\n", r.Domain, r.Domain)

		return
	}
	switch r.Delegation {
	case Delegated:
		fmt.Fprintln(w, "The certificate is still waiting on the authorization record below: the zone holds it and it does not resolve yet (a delegation just made can take up to 48 hours to be seen everywhere).")
	case Registering:
		fmt.Fprintln(w, "The certificate is still waiting on the authorization record below, which the zone holds and which resolves once the registration is active.")
	case Repoint:
		fmt.Fprintln(w, "The certificate is still waiting on the authorization record below, which the zone holds and which resolves once the registration points at the zone.")
	case ApexUndelegated, LabelUndelegated, Refused:
		host := r.Domain
		if r.Delegation == LabelUndelegated {
			host = r.Parent
		}
		fmt.Fprintf(w, "The certificate is still waiting on the authorization record below, which resolves once the delegation is in place. To have the certificate issued sooner, add it now at the DNS provider that serves %s (name, type, value):\n", host)
	}
	fmt.Fprintln(w, r.Authorization.Line())
}

// answeredNow says which name servers the domain answers today, for a message.
func (r *Report) answeredNow() string {
	if r.Lame {
		return "the name servers it is delegated to do not answer for it"
	}
	if len(r.Answered) == 0 {
		return "it answers none"
	}

	return "it answers " + strings.Join(r.Answered, ", ")
}
