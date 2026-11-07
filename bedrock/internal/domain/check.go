// check.go is domain check: whether a domain of the organization's, the apps domain or
// a vanity domain, resolves to its zone in the network layer, and, where it does not,
// the records to add and the place to add them. What the zone holds comes from Cloud
// DNS; what the world sees comes from the resolver. Which shape the domain has
// (registered by the network layer, delegated at its apex from a registrar elsewhere,
// or delegated as a label of a domain served elsewhere) is not configured: it is what
// the answers show. A vanity domain kept at the customer's DNS provider is the one
// shape the placement declares (dns customer), since there is no zone here to read: the
// check resolves each record the provider is to carry, the A records of the hostnames
// declared on the domain at the load balancer's address and the certificate's
// authorization CNAME, read from Certificate Manager, and prints the ones missing or
// differing. A registered domain's name servers are read from the answers too,
// never trusted from the registration: after its zone is made again, a registration
// still names the old set until it is pointed at the zone in Cloud Domains. In a root
// organization the same check reads each domain's zone in the root project, which holds the
// delegation alone, and then asks the world whether each label handed onward answers the
// name servers it is handed to.

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
	certificatemanager "google.golang.org/api/certificatemanager/v1"
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

// FormLine is the record as a DNS provider's form takes it, under the domain the
// provider serves: the host relative to that domain (@ for the domain itself), the type,
// and the first datum as a host name, without the trailing dot a zone file writes
// (Squarespace, for one, refuses the dot as a character).
func (r Record) FormLine(domain string) string {
	host := strings.TrimSuffix(r.Name, ".")
	switch {
	case host == domain:
		host = "@"
	case strings.HasSuffix(host, "."+domain):
		host = strings.TrimSuffix(host, "."+domain)
	}
	data := ""
	if len(r.Data) > 0 {
		data = strings.TrimSuffix(r.Data[0], ".")
	}

	return host + " " + r.Type + " " + data
}

// Line is the record as a zone-file line: name, type and the first datum.
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
	// ZoneByDNSName is the project's zone for the DNS name (example.app., with the
	// trailing dot), by its name, or "" and nil when the project holds none.
	ZoneByDNSName(ctx context.Context, project, dnsName string) (string, *Zone, error)
	// Close releases the connection.
	Close() error
}

// ZoneReaderFunc opens a ZoneReader.
type ZoneReaderFunc func(ctx context.Context) (ZoneReader, error)

// AuthorizationReader reads a DNS authorization of Certificate Manager: the record that
// proves a domain, which no zone here holds for a vanity domain kept at the customer's
// DNS provider. NewCertificateManager is the real one; tests pass a fake.
type AuthorizationReader interface {
	// DNSAuthorization is the record of the project's authorization by name
	// (<prefix>-net-gbl-dnsauth-<domain>), or nil when the project holds no such
	// authorization.
	DNSAuthorization(ctx context.Context, project, name string) (*Record, error)
	// Close releases the connection.
	Close() error
}

// AuthorizationReaderFunc opens an AuthorizationReader.
type AuthorizationReaderFunc func(ctx context.Context) (AuthorizationReader, error)

// Resolver answers the public questions the check asks. *net.Resolver is the real one;
// tests pass a fake.
type Resolver interface {
	LookupNS(ctx context.Context, name string) ([]*net.NS, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupCNAME(ctx context.Context, host string) (string, error)
}

// Lookups are what the check reads through: Cloud DNS for what the zone holds, opened by
// OpenZones, Certificate Manager for a kept vanity domain's authorization record, opened
// by OpenAuthorizations, and Resolver for what the world sees.
type Lookups struct {
	OpenZones          ZoneReaderFunc
	OpenAuthorizations AuthorizationReaderFunc
	Resolver           Resolver
}

// DefaultLookups are the real ones: the Cloud DNS and Certificate Manager APIs with the
// run's Google credentials, and the system's resolver.
func DefaultLookups() *Lookups {
	return &Lookups{OpenZones: NewCloudDNS, OpenAuthorizations: NewCertificateManager, Resolver: net.DefaultResolver}
}

// certificateManager is the AuthorizationReader over the Certificate Manager API.
type certificateManager struct {
	service *certificatemanager.Service
}

// NewCertificateManager opens the Certificate Manager API with Application Default
// Credentials.
func NewCertificateManager(ctx context.Context) (AuthorizationReader, error) {
	service, err := certificatemanager.NewService(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "certificatemanager.NewService()")
	}

	return &certificateManager{service: service}, nil
}

// DNSAuthorization reads the authorization (dnsAuthorizations.get, in the global
// location, where 2-net makes them); one the project lacks is nil.
func (c *certificateManager) DNSAuthorization(ctx context.Context, project, name string) (*Record, error) {
	a, err := c.service.Projects.Locations.DnsAuthorizations.Get("projects/" + project + "/locations/global/dnsAuthorizations/" + name).Context(ctx).Do()
	if err != nil {
		var apiErr *googleapi.Error
		if errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound {
			return nil, nil
		}

		return nil, errors.Wrapf(err, "certificatemanager.ProjectsLocationsDnsAuthorizationsService.Get(): %s in %s", name, project)
	}
	if a.DnsResourceRecord == nil {
		return nil, nil
	}

	return &Record{Name: a.DnsResourceRecord.Name, Type: a.DnsResourceRecord.Type, Data: []string{a.DnsResourceRecord.Data}}, nil
}

// Close has nothing to release: the REST client holds no connection of its own.
func (*certificateManager) Close() error {
	return nil
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

// ZoneByDNSName lists the project's zones for the DNS name (managedZones.list filtered by
// dnsName) and answers the first; a project without one answers none.
func (c *cloudDNS) ZoneByDNSName(ctx context.Context, project, dnsName string) (string, *Zone, error) {
	list, err := c.service.ManagedZones.List(project).DnsName(dnsName).Context(ctx).Do()
	if err != nil {
		return "", nil, errors.Wrapf(err, "dns.ManagedZonesService.List(): %s in %s", dnsName, project)
	}
	if len(list.ManagedZones) == 0 {
		return "", nil, nil
	}
	z := list.ManagedZones[0]

	return z.Name, &Zone{DNSName: z.DnsName, NameServers: z.NameServers}, nil
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

// CheckRequest is what domain check was asked to check: one domain of the
// organization's, the apps domain or a vanity domain.
type CheckRequest struct {
	// Domain is the domain, as the organization's placement names it (appsDomain, or
	// one of vanityDomains).
	Domain string
	// Project is the network project, and Zone the name the network layer gives the
	// domain's zone there.
	Project string
	Zone    string
	// Dir is the infrastructure repository's root, whose network layer placement lists
	// the domains the layer registers.
	Dir string
	// Vanity marks a vanity domain (vanityDomains in the organization's placement): a
	// client's own domain an application serves on, delegated whole to its zone as the
	// apps domain is. The check is the same; what it says to do instead of delegating
	// a domain that carries other DNS differs (a label of it declared as the vanity
	// domain, not a new apps domain).
	Vanity bool
	// CustomerDNS marks a vanity domain kept at the customer's DNS provider (dns
	// customer in vanityDomains): no zone here serves it, and the provider carries the
	// records the check resolves instead of a delegation: an A record at the load
	// balancer's address for each of Hostnames (the hostnames the organization records on
	// the domain, the domain itself or a label of it), and the authorization CNAME of
	// the domain's DNS authorization in Certificate Manager, named Authorization. Zone
	// is then the apps zone, whose apex record carries the load balancer's address.
	CustomerDNS   bool
	Hostnames     []string
	Authorization string
	// Delegating marks a domain of a root organization (domains in its placement),
	// whose zone in the root project holds the domain's delegation alone: no address record, no
	// certificate authorization, one NS record set per label handed onward (Labels). The
	// check reads the zone and resolves the domain as for any domain, skips the records
	// 2-net's apply creates, and, once the domain is delegated, asks the world for each
	// label's name servers.
	Delegating bool
	// Labels are the labels the zone hands onward, in the order they are reported.
	Labels []Label
}

// Label is one label a root organization's zone hands onward: the label (acme, for
// acme.mydomain.com), the hosting organization that serves the name, and the four name
// servers of its zone there, as the record set names them.
type Label struct {
	Name         string
	Organization string
	NameServers  []string
}

// LabelReport is what the answers show about one label handed onward: the name servers
// the world sees for its name, and whether the servers it is handed to do not answer for
// it (Lame: the application's zone in the hosting organization is not applied, or the
// servers recorded are not that zone's).
type LabelReport struct {
	Label
	Answered []string
	Lame     bool
}

// Handed reports whether the name answers the name servers it is handed to.
func (l LabelReport) Handed() bool {
	return sameNames(l.Answered, l.NameServers)
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
	// Domain is the domain checked; Project and Zone are where its zone is; Vanity marks
	// a vanity domain.
	Domain  string
	Project string
	Zone    string
	Vanity  bool
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
	// whether the world sees it; neither for a root organization's domain.
	Authorization         Record
	AuthorizationResolves bool
	// Delegating marks a root organization's domain, and Labels are what the answers
	// show about each label it hands onward, in the request's order, read once the
	// domain is delegated.
	Delegating bool
	Labels     []LabelReport
	// CustomerDNS marks a vanity domain kept at the customer's DNS provider; Address is
	// the load balancer's address its hostnames point at, and Hosts what the world
	// answers for each hostname declared on it.
	CustomerDNS bool
	Address     string
	Hosts       []HostReport
}

// HostReport is one hostname of a kept vanity domain: the name and the IPv4 addresses
// the world answers for it.
type HostReport struct {
	Name     string
	Answered []string
}

// Points reports whether the hostname answers the address and no other.
func (h HostReport) Points(address string) bool {
	return len(h.Answered) == 1 && h.Answered[0] == address
}

// appsZone is what the zone holds that the check compares against.
type appsZone struct {
	zone          *Zone
	address       string
	authorization Record
}

// Check reads the domain's zone through Cloud DNS, resolves the domain's delegation
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
	if req.CustomerDNS {
		return checkKept(ctx, l, req)
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
		Domain: req.Domain, Project: req.Project, Zone: req.Zone, Vanity: req.Vanity, Delegating: req.Delegating,
		ZoneName: z.zone.DNSName, NameServers: z.zone.NameServers, Authorization: z.authorization,
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
	if req.Delegating {
		if err := r.checkLabels(ctx, l.Resolver, req); err != nil {
			return nil, err
		}

		return r, nil
	}
	r.AuthorizationResolves, err = resolvesTo(ctx, l.Resolver, z.authorization)
	if err != nil && (r.Delegation == Delegated || !serverFailure(err)) {
		return nil, err
	}

	return r, nil
}

// checkKept checks a vanity domain kept at the customer's DNS provider: the load
// balancer's address from the apps zone's apex record, the authorization record from
// Certificate Manager, then what the world answers for each hostname declared on the
// domain and for the authorization's name. Every other record of the domain is the
// customer's and is not read. It is refused when the apps zone or the authorization is
// not there (2-net not applied with the domain declared).
func checkKept(ctx context.Context, l *Lookups, req CheckRequest) (*Report, error) {
	address, err := loadBalancerAddress(ctx, l.OpenZones, req)
	if err != nil {
		return nil, err
	}
	if l.OpenAuthorizations == nil {
		return nil, errors.New("no Certificate Manager client is wired: the check of a vanity domain kept at the customer's DNS provider reads the domain's authorization record there")
	}
	authorizations, err := l.OpenAuthorizations(ctx)
	if err != nil {
		return nil, err
	}
	defer authorizations.Close()
	authorization, err := authorizations.DNSAuthorization(ctx, req.Project, req.Authorization)
	if err != nil {
		return nil, err
	}
	if authorization == nil {
		return nil, errors.Newf("no DNS authorization %s in %s: the apply of %s makes it for the vanity domain %s (vanityDomains in placement.json, bedrock org render); apply %s, then check again", req.Authorization, req.Project, DefaultLayer, req.Domain, DefaultLayer)
	}
	r := &Report{Domain: req.Domain, Project: req.Project, Vanity: true, CustomerDNS: true, Address: address, Authorization: *authorization}
	for _, host := range req.Hostnames {
		answered, err := l.Resolver.LookupHost(ctx, host)
		if err := answered4(err, "the addresses of "+host); err != nil {
			return nil, err
		}
		r.Hosts = append(r.Hosts, HostReport{Name: host, Answered: ipv4(answered)})
	}
	r.AuthorizationResolves, err = resolvesTo(ctx, l.Resolver, *authorization)
	if err != nil {
		return nil, err
	}

	return r, nil
}

// loadBalancerAddress is the address the apps zone's apex record points at: the one
// address of the load balancer, which every hostname of a kept vanity domain points at
// too.
func loadBalancerAddress(ctx context.Context, open ZoneReaderFunc, req CheckRequest) (string, error) {
	reader, err := open(ctx)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	zone, err := reader.ManagedZone(ctx, req.Project, req.Zone)
	if err != nil {
		return "", err
	}
	if zone == nil {
		return "", errors.Newf("no zone %s in %s: the apply of %s creates it, with the load balancer's address the hostnames of %s point at; apply %s, then check again", req.Zone, req.Project, DefaultLayer, req.Domain, DefaultLayer)
	}
	records, err := reader.RecordSets(ctx, req.Project, req.Zone)
	if err != nil {
		return "", err
	}
	for _, rec := range records {
		if rec.Type == typeA && sameName(rec.Name, zone.DNSName) && len(rec.Data) > 0 {
			return rec.Data[0], nil
		}
	}

	return "", errors.Newf("the zone %s in %s lacks the A record of %s, which carries the load balancer's address: the apply of %s creates it; apply %s, then check again", req.Zone, req.Project, zone.DNSName, DefaultLayer, DefaultLayer)
}

// answered4 is answered for a hostname's addresses.
func answered4(err error, what string) error {
	return answered(err, what)
}

// ipv4 keeps the IPv4 addresses of an answer: the load balancer has one IPv4 address,
// and an AAAA record the customer carries beside is not this check's.
func ipv4(addresses []string) []string {
	var out []string
	for _, a := range addresses {
		if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
			out = append(out, a)
		}
	}

	return out
}

// checkLabels asks the world, for each label the zone hands onward, which name servers
// its name answers, once the domain is delegated to the zone; before that nothing under
// the domain reaches the zone, and the labels are reported unread. Servers that do not
// answer for the name (SERVFAIL) are Lame, not a failure of the check.
func (r *Report) checkLabels(ctx context.Context, resolver Resolver, req CheckRequest) error {
	for _, label := range req.Labels {
		lr := LabelReport{Label: label}
		if r.Delegation == Delegated {
			answered, err := nameServers(ctx, resolver, label.Name+"."+req.Domain)
			lr.Lame = serverFailure(err)
			if err != nil && !lr.Lame {
				return err
			}
			lr.Answered = answered
		}
		r.Labels = append(r.Labels, lr)
	}

	return nil
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
		if req.Delegating {
			return nil, errors.Newf("no zone %s in %s: the root organization's apply makes it from domains in placement.json; merge the pull request that lists the domain, then check again", req.Zone, req.Project)
		}

		return nil, errors.Newf("no zone %s in %s: the apply of %s creates it; apply %s, then check again", req.Zone, req.Project, DefaultLayer, DefaultLayer)
	}
	if !sameName(zone.DNSName, req.Domain) {
		if req.Delegating {
			return nil, errors.Newf("the zone %s in %s serves %s, not the domain %s: the root organization names a domain's zone after the domain (domains in placement.json); bedrock org check says what the apply would change, and the apply makes it so", req.Zone, req.Project, zone.DNSName, req.Domain)
		}
		if req.Vanity {
			return nil, errors.Newf("the zone %s in %s serves %s, not the vanity domain %s: %s names a vanity domain's zone after the domain (vanityDomains in placement.json, bedrock org render); apply %s as rendered, then check again", req.Zone, req.Project, zone.DNSName, req.Domain, DefaultLayer, DefaultLayer)
		}

		return nil, errors.Newf("the zone %s in %s serves %s, not the apps domain %s: set apps_domain in %s to appsDomain in placement.json and apply %s, then check again", req.Zone, req.Project, zone.DNSName, req.Domain, netValues, DefaultLayer)
	}
	if req.Delegating {
		// The zone holds the delegation alone: no address, no authorization record.
		return &appsZone{zone: zone}, nil
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
// and the authorization record resolves; for a root organization's domain, the domain
// answers the zone's name servers and every label handed onward answers its own.
func (r *Report) Passed() bool {
	if r.Delegating {
		return r.Delegation == Delegated && !slices.ContainsFunc(r.Labels, func(l LabelReport) bool { return !l.Handed() })
	}
	if r.CustomerDNS {
		return !slices.ContainsFunc(r.Hosts, func(h HostReport) bool { return !h.Points(r.Address) }) && r.AuthorizationResolves
	}

	return r.Delegation == Delegated && r.AuthorizationResolves
}

// Write prints what the check found and, for what is missing, the place to act and the
// records to add there; each record on its own line exactly as it is pasted where it
// goes: the name servers for a registrar as bare host names, and a record for a DNS
// provider's form as the form takes it (the host relative to the domain the provider
// serves, the value without the trailing dot), since a registrar and a provider's form
// take host names and the trailing dot is a zone file's convention; a record the zone
// itself holds is shown as the zone-file line it is.
func (r *Report) Write(w io.Writer) {
	if r.CustomerDNS {
		r.writeKept(w)

		return
	}
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
		if len(r.Answered) == 0 && !r.Delegating {
			fmt.Fprintf(w, "%s answers no name servers at all: if it is not registered yet, bedrock domain add registers it through Cloud Domains in %s instead, with no registrar step.\n", r.Domain, DefaultLayer)
		}
	case Repoint:
		fmt.Fprintf(w, "%s does not answer the zone's name servers (%s).\n", r.Domain, r.answeredNow())
		fmt.Fprintf(w, "%s registers it through Cloud Domains (registrations in %s), and its registration names name servers that are not the zone's, as after the zone was made again: the apply does not change the name servers of a registration that exists (%s/README.md, \"Making a zone again\").\n", DefaultLayer, netValues, DefaultLayer)
		fmt.Fprintf(w, "In Cloud Domains, in the network project %s: point the registration of %s at the zone %s with the command below, or in the console (the Cloud Domains page, %s, Edit DNS details, Cloud DNS, the zone %s, Save). It takes the Cloud Domains Admin role (roles/domains.admin) on the project; the change takes up to 48 hours to be seen everywhere.\n", r.Project, r.Domain, r.Zone, r.Domain, r.Zone)
		fmt.Fprintf(w, "gcloud domains registrations configure dns %s --cloud-dns-zone=%s --project=%s\n", r.Domain, r.Zone, r.Project)
	case LabelUndelegated:
		fmt.Fprintf(w, "%s does not answer the zone's name servers (%s).\n", r.Domain, r.answeredNow())
		fmt.Fprintf(w, "At the DNS provider that serves %s: add NS records for %s pointing at the zone's name servers, in place of any it has, one record per line below as the provider's form takes it (host, type, value: the host relative to %s, the value without the trailing dot a zone file writes). The other records of %s stay as they are.\n", r.Parent, r.Label, r.Parent, r.Parent)
		for _, ns := range r.NameServers {
			fmt.Fprintln(w, Record{Name: r.ZoneName, Type: typeNS, Data: []string{ns}}.FormLine(r.Parent))
		}
	case Refused:
		fmt.Fprintf(w, "%s does not answer the zone's name servers (%s), and it answers records that are not the zone's:\n", r.Domain, r.answeredNow())
		for _, f := range r.Foreign {
			fmt.Fprintln(w, f)
		}
		if r.Delegating {
			fmt.Fprintln(w, "Refused: a domain that carries other DNS (a website, mail) is never delegated whole, and the names handed onward would each need their own records at its DNS provider.")
			fmt.Fprintf(w, "Hold a label of it instead, such as apps.%s: set it in place of %s in domains in placement.json, merge the pull request, and run bedrock domain check again for the NS records to add at the DNS provider that serves %s.\n", r.Domain, r.Domain, r.Domain)

			return
		}
		if r.Vanity {
			fmt.Fprintln(w, "Refused: a domain that carries other DNS (a website, mail) is never delegated whole. Two ways out, the customer's choice:")
			fmt.Fprintf(w, "Declare a label of it as the vanity domain instead, such as app.%s: set it in place of %s in vanityDomains in placement.json and put the site's hostnames on it in the application's placement (hostnames.<site>.vanity), run bedrock org render, apply %s, and run bedrock domain check again for the NS records to add at the DNS provider that serves %s.\n", r.Domain, r.Domain, DefaultLayer, r.Domain)
			fmt.Fprintf(w, "Or keep %s where it is: set its entry in vanityDomains to {\"dns\": \"customer\"}, run bedrock org render, apply %s, and run bedrock domain check again for the records to add at the DNS provider that serves it, an A record per hostname declared on it and the certificate's authorization CNAME; its mail and every other record stay as they are.\n", r.Domain, DefaultLayer)

			return
		}
		fmt.Fprintln(w, "Refused: a domain that carries other DNS (a website, mail) is never delegated whole, and application hostnames directly under it would each need their own records at its DNS provider, with a wildcard on it for the pull-request environments.")
		fmt.Fprintf(w, "Serve the applications under a label of it instead, such as apps.%s: set appsDomain in placement.json and apps_domain in %s to that name, run bedrock org render, apply %s, and run bedrock domain check again for the NS records to add at the DNS provider that serves %s.\n", r.Domain, netValues, DefaultLayer, r.Domain)

		return
	}
	if r.Delegating {
		r.writeLabels(w)

		return
	}
	r.writeAuthorization(w)
}

// writeKept says, for a vanity domain kept at the customer's DNS provider, what each
// hostname declared on it answers and whether the authorization record resolves, and
// prints the records missing or differing as the provider's form takes them.
func (r *Report) writeKept(w io.Writer) {
	fmt.Fprintf(w, "%s is kept at its DNS provider (vanityDomains: dns customer), and no zone here serves it: its hostnames point at the load balancer (%s) by records added there.\n", r.Domain, r.Address)
	var missing []Record
	for _, h := range r.Hosts {
		switch {
		case h.Points(r.Address):
			fmt.Fprintf(w, "%s answers the load balancer's address.\n", h.Name)
		case len(h.Answered) == 0:
			fmt.Fprintf(w, "%s answers no address.\n", h.Name)
			missing = append(missing, Record{Name: h.Name + ".", Type: typeA, Data: []string{r.Address}})
		default:
			fmt.Fprintf(w, "%s answers %s, not the load balancer's address alone.\n", h.Name, strings.Join(h.Answered, ", "))
			missing = append(missing, Record{Name: h.Name + ".", Type: typeA, Data: []string{r.Address}})
		}
	}
	if len(r.Hosts) == 0 {
		fmt.Fprintf(w, "No application declares a hostname on %s yet (hostnames.<site>.vanity in an application's placement, recorded by bedrock org register --next or bedrock org hostnames): a hostname declared later adds one A record here, which this check prints then.\n", r.Domain)
	}
	if r.AuthorizationResolves {
		fmt.Fprintf(w, "The authorization record resolves: the certificate for %s and *.%s is not waiting on it.\n", r.Domain, r.Domain)
	} else {
		fmt.Fprintf(w, "The certificate for %s and *.%s is still waiting on the authorization record below: it does not resolve yet (a record just added can take up to 48 hours to be seen everywhere).\n", r.Domain, r.Domain)
		missing = append(missing, r.Authorization)
	}
	if len(missing) == 0 {
		return
	}
	fmt.Fprintf(w, "At the DNS provider that serves %s: add the %d record(s) below, as its form takes them (host, type, value: the host relative to %s, @ for the domain itself, the value without the trailing dot a zone file writes), in place of any record of the same host and type. Every other record of %s (its mail, its website elsewhere) stays as it is. A CAA record set at %s, if it has one, must admit pki.goog (0 issue \"pki.goog\"), or the certificate is never issued.\n", r.Domain, len(missing), r.Domain, r.Domain, r.Domain)
	for _, rec := range missing {
		fmt.Fprintln(w, rec.FormLine(r.Domain))
	}
}

// writeLabels says, for each label the zone hands onward, whether its name answers the
// name servers it is handed to, and otherwise what the answer shows: the servers not
// answering for it (the application's zone in the hosting organization not applied, or
// the servers recorded not that zone's), no answer yet, or another set.
func (r *Report) writeLabels(w io.Writer) {
	if len(r.Labels) == 0 {
		fmt.Fprintf(w, "No name under %s is handed onward yet (domains in placement.json).\n", r.Domain)

		return
	}
	if r.Delegation != Delegated {
		fmt.Fprintf(w, "The %d name(s) handed onward under %s resolve once the domain is delegated here.\n", len(r.Labels), r.Domain)

		return
	}
	for _, l := range r.Labels {
		name := l.Name + "." + r.Domain
		switch {
		case l.Handed():
			fmt.Fprintf(w, "%s is handed to %s: it answers the name servers of its zone there (%s).\n", name, l.Organization, strings.Join(l.NameServers, ", "))
		case l.Lame:
			fmt.Fprintf(w, "%s is handed to %s, and the name servers it is handed to do not answer for it: the application's zone in %s is not applied yet, or the servers recorded here (%s) are not that zone's; bedrock domain check in %s's infrastructure repository prints them.\n", name, l.Organization, l.Organization, strings.Join(l.NameServers, ", "), l.Organization)
		case len(l.Answered) == 0:
			fmt.Fprintf(w, "%s is handed to %s, and it answers no name servers yet: the record set is in the zone, and an answer cached before it was added lasts until the zone's negative time to live runs out.\n", name, l.Organization)
		default:
			fmt.Fprintf(w, "%s is handed to %s on %s, and it answers %s: a record set changed within the hour is still seen as it was (its time to live), and servers that are not the application's zone's are a wrong entry in placement.json (domains.%s.labels.%s), to correct and render.\n", name, l.Organization, strings.Join(l.NameServers, ", "), strings.Join(l.Answered, ", "), r.Domain, l.Name)
		}
	}
}

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
		fmt.Fprintf(w, "The certificate is still waiting on the authorization record below, which resolves once the delegation is in place. To have the certificate issued sooner, add it now at the DNS provider that serves %s, as its form takes it (host, type, value):\n", host)
		fmt.Fprintln(w, r.Authorization.FormLine(host))

		return
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
