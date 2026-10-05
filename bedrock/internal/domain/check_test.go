package domain

import (
	"bytes"
	"context"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
)

// The zone the fakes hold: its project and name, its four name servers, the load
// balancer's address and the authorization record, as Cloud DNS spells them.
const (
	checkProject = "ex-net-gbl-core-1a2b"
	checkZone    = "ex-net-gbl-dns-apps"
	lbAddress    = "203.0.113.10"
	authData     = "0f1e2d3c-4b5a.6.authorize.certificatemanager.goog."
)

// zoneServers are the zone's name servers, and movedServers the set an earlier zone of
// the same name had before it was made again.
var (
	zoneServers  = []string{"ns-cloud-c1.googledomains.com.", "ns-cloud-c2.googledomains.com.", "ns-cloud-c3.googledomains.com.", "ns-cloud-c4.googledomains.com."}
	movedServers = []string{"ns-cloud-b1.googledomains.com.", "ns-cloud-b2.googledomains.com.", "ns-cloud-b3.googledomains.com.", "ns-cloud-b4.googledomains.com."}
)

// fakeZones is a Cloud DNS holding one zone, or none.
type fakeZones struct {
	zone    *Zone
	records []Record
}

func (f *fakeZones) ManagedZone(_ context.Context, project, zone string) (*Zone, error) {
	if f.zone == nil || project != checkProject || zone != checkZone {
		return nil, nil
	}

	return f.zone, nil
}

func (f *fakeZones) RecordSets(context.Context, string, string) ([]Record, error) {
	return f.records, nil
}

func (*fakeZones) Close() error {
	return nil
}

// fakeResolver answers from maps; a name a map lacks is not found, a name in fails fails
// the lookup outright, and a name in servfail is answered SERVFAIL, as the Go resolver
// reports it, the answer a delegation to servers that do not serve the zone gets.
type fakeResolver struct {
	ns       map[string][]string
	hosts    map[string][]string
	mx       map[string][]*net.MX
	cname    map[string]string
	fails    string
	servfail map[string]bool
}

// lookup answers one question from its map: the answer, not found, or the failure.
func lookup[T any](f *fakeResolver, answers map[string]T, name string) (T, error) {
	var zero T
	if name == f.fails {
		return zero, errors.New("connection refused")
	}
	if f.servfail[name] {
		return zero, &net.DNSError{Err: serverMisbehaving, Name: name, IsTemporary: true}
	}
	v, ok := answers[name]
	if !ok {
		return zero, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}

	return v, nil
}

func (f *fakeResolver) LookupNS(_ context.Context, name string) ([]*net.NS, error) {
	hosts, err := lookup(f, f.ns, name)
	ns := make([]*net.NS, 0, len(hosts))
	for _, h := range hosts {
		ns = append(ns, &net.NS{Host: h})
	}

	return ns, err
}

func (f *fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	return lookup(f, f.hosts, host)
}

func (f *fakeResolver) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	return lookup(f, f.mx, name)
}

func (f *fakeResolver) LookupCNAME(_ context.Context, host string) (string, error) {
	return lookup(f, f.cname, host)
}

// authName is the authorization record's name for the domain.
func authName(domain string) string {
	return "_acme-challenge." + domain + "."
}

// zoneFor is the zone 2-net makes for the domain: the apex and wildcard addresses and the
// authorization record.
func zoneFor(domain string) *fakeZones {
	return &fakeZones{
		zone: &Zone{DNSName: domain + ".", NameServers: zoneServers},
		records: []Record{
			{Name: domain + ".", Type: "NS", Data: zoneServers},
			{Name: domain + ".", Type: "A", Data: []string{lbAddress}},
			{Name: "*." + domain + ".", Type: "A", Data: []string{lbAddress}},
			{Name: authName(domain), Type: "CNAME", Data: []string{authData}},
		},
	}
}

// resolved is what the world sees of a domain delegated to the zone: its name servers
// and the authorization record.
func resolved(domain string) *fakeResolver {
	return &fakeResolver{
		ns:    map[string][]string{domain: zoneServers},
		hosts: map[string][]string{domain: {lbAddress}},
		cname: map[string]string{authName(domain): authData},
	}
}

// bare is each name server as a registrar takes it: the host name, without the zone
// file's trailing dot.
func bare() []string {
	hosts := make([]string, 0, len(zoneServers))
	for _, ns := range zoneServers {
		hosts = append(hosts, strings.TrimSuffix(ns, "."))
	}

	return hosts
}

// authLine is the authorization record's line for the domain.
func authLine(domain string) string {
	return authName(domain) + " CNAME " + authData
}

// formed is each name server after the prefix as a DNS provider's form takes it, the
// trailing dot dropped, one line each.
func formed(prefix string) []string {
	lines := make([]string, 0, len(zoneServers))
	for _, ns := range zoneServers {
		lines = append(lines, prefix+strings.TrimSuffix(ns, "."))
	}

	return lines
}

// authForm is the authorization record as a DNS provider's form takes it under the
// domain the provider serves: the host relative to that domain (label empty for the
// domain itself), the value without the trailing dot.
func authForm(label string) string {
	host := "_acme-challenge"
	if label != "" {
		host += "." + label
	}

	return host + " CNAME " + strings.TrimSuffix(authData, ".")
}

// TestCheck classifies the apps domain over a fake Cloud DNS and a fake resolver, and
// holds every record line the check prints, byte for byte, to the fakes' values.
func TestCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		domain   string
		zones    *fakeZones
		resolver *fakeResolver
		// zoneReplacement is zoneReplacement set in the organization's placement.
		zoneReplacement bool
		want            Delegation
		wantPassed      bool
		wantLines       []string
		// wantAbsent are texts no line may hold.
		wantAbsent []string
		wantErr    string
	}{
		{
			name:            "a domain that passes while zoneReplacement is set: the value can be cleared",
			domain:          "example.dev",
			zones:           zoneFor("example.dev"),
			resolver:        resolved("example.dev"),
			zoneReplacement: true,
			want:            Delegated,
			wantPassed:      true,
			wantLines:       []string{ZoneReplacementCleared},
		},
		{
			name:            "a domain that does not pass yet while zoneReplacement is set: nothing said of it",
			domain:          "example.app",
			zones:           zoneFor("example.app"),
			resolver:        &fakeResolver{ns: map[string][]string{"example.app": movedServers}},
			zoneReplacement: true,
			want:            Repoint,
			wantAbsent:      []string{"zoneReplacement"},
		},
		{
			name:       "an apex registered here or delegated: the zone's name servers answer",
			domain:     "example.dev",
			zones:      zoneFor("example.dev"),
			resolver:   resolved("example.dev"),
			want:       Delegated,
			wantPassed: true,
			wantLines: []string{
				"example.dev is delegated to the zone ex-net-gbl-dns-apps in ex-net-gbl-core-1a2b: it answers the zone's name servers (" + strings.Join(zoneServers, ", ") + ").",
				"The authorization record resolves: the certificate for example.dev and *.example.dev is not waiting on it.",
			},
			wantAbsent: []string{"zoneReplacement"},
		},
		{
			name:       "a label delegated at its parent's DNS provider",
			domain:     "apps.example.com",
			zones:      zoneFor("apps.example.com"),
			resolver:   resolved("apps.example.com"),
			want:       Delegated,
			wantPassed: true,
			wantLines:  []string{"apps.example.com is delegated to the zone ex-net-gbl-dns-apps in ex-net-gbl-core-1a2b: it answers the zone's name servers (" + strings.Join(zoneServers, ", ") + ")."},
		},
		{
			name:   "a label with nothing: the NS records to add at the parent's DNS provider",
			domain: "apps.example.com",
			zones:  zoneFor("apps.example.com"),
			resolver: &fakeResolver{
				hosts: map[string][]string{"example.com": {"192.0.2.80"}},
				mx:    map[string][]*net.MX{"example.com": {{Host: "mail.example.net.", Pref: 10}}},
			},
			want: LabelUndelegated,
			wantLines: append(append([]string{
				"apps.example.com does not answer the zone's name servers (it answers none).",
				"At the DNS provider that serves example.com: add NS records for apps pointing at the zone's name servers, in place of any it has, one record per line below as the provider's form takes it (host, type, value: the host relative to example.com, the value without the trailing dot a zone file writes). The other records of example.com stay as they are.",
			}, formed("apps NS ")...),
				"The certificate is still waiting on the authorization record below, which resolves once the delegation is in place. To have the certificate issued sooner, add it now at the DNS provider that serves example.com, as its form takes it (host, type, value):",
				authForm("apps"),
			),
		},
		{
			name:   "an apex with other records and no delegation is refused, with the label named",
			domain: "example.com",
			zones:  zoneFor("example.com"),
			resolver: &fakeResolver{
				ns:    map[string][]string{"example.com": {"ns1.example.net.", "ns2.example.net."}},
				hosts: map[string][]string{"example.com": {"192.0.2.80", "2001:db8::80"}},
				mx:    map[string][]*net.MX{"example.com": {{Host: "mail.example.net.", Pref: 10}}},
			},
			want: Refused,
			wantLines: []string{
				"example.com does not answer the zone's name servers (it answers ns1.example.net., ns2.example.net.), and it answers records that are not the zone's:",
				"A 192.0.2.80",
				"AAAA 2001:db8::80",
				"MX 10 mail.example.net.",
				"Serve the applications under a label of it instead, such as apps.example.com: set appsDomain in placement.json and apps_domain in 2-net/terraform.tfvars to that name, run bedrock org render, apply 2-net, and run bedrock domain check again for the NS records to add at the DNS provider that serves example.com.",
			},
		},
		{
			name:     "the authorization record missing alone: the record, as the zone holds it",
			domain:   "example.dev",
			zones:    zoneFor("example.dev"),
			resolver: &fakeResolver{ns: map[string][]string{"example.dev": zoneServers}},
			want:     Delegated,
			wantLines: []string{
				"The certificate is still waiting on the authorization record below: the zone holds it and it does not resolve yet (a delegation just made can take up to 48 hours to be seen everywhere).",
				authLine("example.dev"),
			},
		},
		{
			name:   "an apex with nothing else on it, registered elsewhere: the registrar step",
			domain: "example.org",
			zones:  zoneFor("example.org"),
			resolver: &fakeResolver{
				ns:    map[string][]string{"example.org": {"ns1.registrar.example.", "ns2.registrar.example."}},
				hosts: map[string][]string{"example.org": {lbAddress}},
			},
			want: ApexUndelegated,
			wantLines: append(append([]string{
				"example.org does not answer the zone's name servers (it answers ns1.registrar.example., ns2.registrar.example.).",
				"At the registrar where example.org is registered: set its name servers to the zone's 4 below, in place of the ones it has. " + squarespacePath,
			}, bare()...),
				"The certificate is still waiting on the authorization record below, which resolves once the delegation is in place. To have the certificate issued sooner, add it now at the DNS provider that serves example.org, as its form takes it (host, type, value):",
				authForm(""),
			),
		},
		{
			name:     "an apex with no name servers at all: the registrar step, and registering it here",
			domain:   "example.org",
			zones:    zoneFor("example.org"),
			resolver: &fakeResolver{},
			want:     ApexUndelegated,
			wantLines: append(bare(),
				"example.org answers no name servers at all: if it is not registered yet, bedrock domain add registers it through Cloud Domains in 2-net instead, with no registrar step.",
			),
		},
		{
			name:     "registered here and not active yet: the registrant's mailbox",
			domain:   "example.app",
			zones:    zoneFor("example.app"),
			resolver: &fakeResolver{},
			want:     Registering,
			wantLines: []string{
				"example.app does not answer the zone's name servers yet (it answers none).",
				"In the registrant's mailbox (registrant_contact in 2-net/terraform.tfvars): open the registrar's verification mail and follow its link within fifteen days of the registration, or the domain is suspended.",
				authLine("example.app"),
			},
			wantAbsent: []string{"gcloud domains", "Cloud Domains, in the network project"},
		},
		{
			name:       "registered here, its registration pointing at the zone: passes",
			domain:     "example.app",
			zones:      zoneFor("example.app"),
			resolver:   resolved("example.app"),
			want:       Delegated,
			wantPassed: true,
			wantLines: []string{
				"example.app is delegated to the zone ex-net-gbl-dns-apps in ex-net-gbl-core-1a2b: it answers the zone's name servers (" + strings.Join(zoneServers, ", ") + ").",
				"The authorization record resolves: the certificate for example.app and *.example.app is not waiting on it.",
			},
		},
		{
			name:   "registered here, its registration naming the set of a zone since made again: the step in Cloud Domains",
			domain: "example.app",
			zones:  zoneFor("example.app"),
			resolver: &fakeResolver{
				ns: map[string][]string{"example.app": movedServers},
			},
			want: Repoint,
			wantLines: []string{
				"example.app does not answer the zone's name servers (it answers " + strings.Join(movedServers, ", ") + ").",
				"2-net registers it through Cloud Domains (registrations in 2-net/terraform.tfvars), and its registration names name servers that are not the zone's, as after the zone was made again: the apply does not change the name servers of a registration that exists (2-net/README.md, \"Making a zone again\").",
				"In Cloud Domains, in the network project ex-net-gbl-core-1a2b: point the registration of example.app at the zone ex-net-gbl-dns-apps with the command below, or in the console (the Cloud Domains page, example.app, Edit DNS details, Cloud DNS, the zone ex-net-gbl-dns-apps, Save). It takes the Cloud Domains Admin role (roles/domains.admin) on the project; the change takes up to 48 hours to be seen everywhere.",
				"gcloud domains registrations configure dns example.app --cloud-dns-zone=ex-net-gbl-dns-apps --project=ex-net-gbl-core-1a2b",
				"The certificate is still waiting on the authorization record below, which the zone holds and which resolves once the registration points at the zone.",
				authLine("example.app"),
			},
			wantAbsent: []string{"the apply sets", "the apply that registers it gives", "registrant's mailbox"},
		},
		{
			name:   "registered here, the servers its registration names not answering for it: the step in Cloud Domains",
			domain: "example.app",
			zones:  zoneFor("example.app"),
			resolver: &fakeResolver{
				servfail: map[string]bool{"example.app": true, authName("example.app"): true},
			},
			want: Repoint,
			wantLines: []string{
				"example.app does not answer the zone's name servers (the name servers it is delegated to do not answer for it).",
				"gcloud domains registrations configure dns example.app --cloud-dns-zone=ex-net-gbl-dns-apps --project=ex-net-gbl-core-1a2b",
				authLine("example.app"),
			},
			wantAbsent: []string{"the apply sets", "the apply that registers it gives"},
		},
		{
			name:     "servers that do not answer for a domain not registered here stop the check",
			domain:   "example.dev",
			zones:    zoneFor("example.dev"),
			resolver: &fakeResolver{servfail: map[string]bool{"example.dev": true}},
			wantErr:  "resolving the name servers of example.dev",
		},
		{
			name:   "name servers that are the zone's and another's are not a delegation",
			domain: "example.dev",
			zones:  zoneFor("example.dev"),
			resolver: &fakeResolver{
				ns:    map[string][]string{"example.dev": append(slices.Clone(zoneServers), "ns1.example.net.")},
				cname: map[string]string{authName("example.dev"): authData},
			},
			want:      ApexUndelegated,
			wantLines: bare(),
		},
		{
			name:     "no zone: 2-net is not applied",
			domain:   "example.dev",
			zones:    &fakeZones{},
			resolver: resolved("example.dev"),
			wantErr:  "no zone ex-net-gbl-dns-apps in ex-net-gbl-core-1a2b: the apply of 2-net creates it",
		},
		{
			name:     "a zone serving another domain",
			domain:   "example.dev",
			zones:    zoneFor("example.org"),
			resolver: resolved("example.dev"),
			wantErr:  "the zone ex-net-gbl-dns-apps in ex-net-gbl-core-1a2b serves example.org., not the apps domain example.dev",
		},
		{
			name:   "a zone without the records 2-net's apply makes",
			domain: "example.dev",
			zones: &fakeZones{
				zone:    &Zone{DNSName: "example.dev.", NameServers: zoneServers},
				records: []Record{{Name: "example.dev.", Type: "A", Data: []string{lbAddress}}},
			},
			resolver: resolved("example.dev"),
			wantErr:  "lacks the A record of *.example.dev, the certificate's authorization record (_acme-challenge)",
		},
		{
			name:     "a resolver that fails, rather than finding nothing",
			domain:   "example.dev",
			zones:    zoneFor("example.dev"),
			resolver: &fakeResolver{fails: "example.dev"},
			wantErr:  "resolving the name servers of example.dev",
		},
		{
			name:     "a domain that is not a bare lowercase name",
			domain:   "Example.dev",
			zones:    zoneFor("example.dev"),
			resolver: resolved("example.dev"),
			wantErr:  `domain "Example.dev": a bare lowercase domain`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			zones := tt.zones
			lookups := &Lookups{
				OpenZones: func(context.Context) (ZoneReader, error) {
					return zones, nil
				},
				Resolver: tt.resolver,
			}
			r, err := Check(context.Background(), lookups, CheckRequest{Domain: tt.domain, Project: checkProject, Zone: checkZone, Dir: repo, ZoneReplacement: tt.zoneReplacement})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Check() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if r.Delegation != tt.want {
				t.Errorf("Delegation = %d, want %d", r.Delegation, tt.want)
			}
			if r.Passed() != tt.wantPassed {
				t.Errorf("Passed() = %v, want %v", r.Passed(), tt.wantPassed)
			}
			var out bytes.Buffer
			r.Write(&out)
			lines := strings.Split(out.String(), "\n")
			for _, want := range tt.wantLines {
				if !slices.Contains(lines, want) {
					t.Errorf("output lacks the line %q:\n%s", want, out.String())
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(out.String(), absent) {
					t.Errorf("output holds %q:\n%s", absent, out.String())
				}
			}
		})
	}
}

// TestCheckRecordOrder holds the records the check prints to the zone's order, so the
// lines paste in the order the zone lists them.
func TestCheckRecordOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		domain string
		want   []string
	}{
		{name: "the registrar's name servers, bare host names", domain: "example.org", want: bare()},
		{name: "the label's NS records, as the parent's provider's form takes them", domain: "apps.example.org", want: formed("apps NS ")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lookups := &Lookups{
				OpenZones: func(context.Context) (ZoneReader, error) {
					return zoneFor(tt.domain), nil
				},
				Resolver: &fakeResolver{},
			}
			r, err := Check(context.Background(), lookups, CheckRequest{Domain: tt.domain, Project: checkProject, Zone: checkZone, Dir: repo})
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			var out bytes.Buffer
			r.Write(&out)
			want := "\n" + strings.Join(tt.want, "\n") + "\n"
			if !strings.Contains(out.String(), want) {
				t.Errorf("output lacks the records in the zone's order:\n%s\nwant:\n%s", out.String(), want)
			}
		})
	}
}

// TestFormLine holds a record's form line to the host relative to the domain the
// provider serves, the domain itself as @, and the value without the trailing dot.
func TestFormLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		record Record
		domain string
		want   string
	}{
		{name: "a label's NS record under its parent", record: Record{Name: "apps.example.com.", Type: "NS", Data: []string{"ns-cloud-c1.googledomains.com."}}, domain: "example.com", want: "apps NS ns-cloud-c1.googledomains.com"},
		{name: "the domain itself is @", record: Record{Name: "example.com.", Type: "NS", Data: []string{"ns-cloud-c1.googledomains.com."}}, domain: "example.com", want: "@ NS ns-cloud-c1.googledomains.com"},
		{name: "the authorization record under a label", record: Record{Name: "_acme-challenge.apps.example.com.", Type: "CNAME", Data: []string{authData}}, domain: "example.com", want: "_acme-challenge.apps CNAME " + strings.TrimSuffix(authData, ".")},
		{name: "a name under another domain stays whole", record: Record{Name: "apps.example.net.", Type: "NS", Data: []string{"ns1.example."}}, domain: "example.com", want: "apps.example.net NS ns1.example"},
		{name: "no datum", record: Record{Name: "apps.example.com.", Type: "NS"}, domain: "example.com", want: "apps NS "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.record.FormLine(tt.domain); got != tt.want {
				t.Errorf("FormLine() = %q, want %q", got, tt.want)
			}
		})
	}
}
