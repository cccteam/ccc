package cli

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/domain"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// The fake zones of domain check's command tests: the fixture organization's network
// project, its apps zone serving example.dev and a vanity domain's zone serving
// harborlights.example, both on the one name server.
const (
	checkDomain = "example.dev"
	checkVanity = "harborlights.example"
	checkServer = "ns-cloud-c1.googledomains.com."
	checkAuth   = "0f1e2d3c-4b5a.6.authorize.certificatemanager.goog."
)

// zonesByName is a Cloud DNS holding the fixture organization's zones by the names 2-net
// gives them, in any project.
type zonesByName map[string]string

func (z zonesByName) ManagedZone(_ context.Context, _, zone string) (*domain.Zone, error) {
	name, ok := z[zone]
	if !ok {
		return nil, nil
	}

	return &domain.Zone{DNSName: name + ".", NameServers: []string{checkServer}}, nil
}

func (z zonesByName) RecordSets(_ context.Context, _, zone string) ([]domain.Record, error) {
	name := z[zone]

	return []domain.Record{
		{Name: name + ".", Type: "A", Data: []string{"203.0.113.10"}},
		{Name: "*." + name + ".", Type: "A", Data: []string{"203.0.113.10"}},
		{Name: "_acme-challenge." + name + ".", Type: "CNAME", Data: []string{checkAuth}},
	}, nil
}

func (zonesByName) Close() error {
	return nil
}

// world answers name servers and CNAMEs from maps; a name a map lacks is not found.
type world struct {
	ns    map[string]string
	cname map[string]string
}

func (w world) LookupNS(_ context.Context, name string) ([]*net.NS, error) {
	if host, ok := w.ns[name]; ok {
		return []*net.NS{{Host: host}}, nil
	}

	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (world) LookupHost(_ context.Context, host string) ([]string, error) {
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func (world) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (w world) LookupCNAME(_ context.Context, host string) (string, error) {
	if target, ok := w.cname[host]; ok {
		return target, nil
	}

	return "", &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

// checkRepo is an organization repository whose placement names checkDomain as its apps
// domain and the vanity domains given, with the network project recorded or not, and
// zoneReplacement set or not.
func checkRepo(t *testing.T, recorded, zoneReplacement bool, vanity []string) string {
	t.Helper()

	dir := t.TempDir()
	p, err := org.ReadPlacement(filepath.Join(orgFixture, "placement.json"))
	if err != nil {
		t.Fatal(err)
	}
	p.AppsDomain = checkDomain
	p.VanityDomains = vanity
	p.ZoneReplacement = zoneReplacement
	if !recorded {
		p.Projects = nil
		p.ProjectNumbers = nil
	}
	if err := p.Write(filepath.Join(dir, "placement.json")); err != nil {
		t.Fatal(err)
	}

	return dir
}

// TestDomainCheck runs domain check over fake zones and a fake world: it passes on a
// delegated apps domain, saying zoneReplacement can be cleared when the placement sets
// it, exits 1 with the registrar step on one that is not, with nothing said about the
// value, checks each vanity domain after the apps domain the same way, says the value
// can be cleared only once every domain passes, stops on a vanity domain whose zone
// 2-net has not made, and is refused before the network project is recorded.
func TestDomainCheck(t *testing.T) {
	t.Parallel()

	delegated := world{ns: map[string]string{checkDomain: checkServer}, cname: map[string]string{"_acme-challenge." + checkDomain + ".": checkAuth}}
	bothDelegated := world{
		ns:    map[string]string{checkDomain: checkServer, checkVanity: checkServer},
		cname: map[string]string{"_acme-challenge." + checkDomain + ".": checkAuth, "_acme-challenge." + checkVanity + ".": checkAuth},
	}
	zones := zonesByName{"imp-net-gbl-dns-apps": checkDomain, "imp-net-gbl-dns-harborlights-example": checkVanity}
	tests := []struct {
		name            string
		recorded        bool
		zoneReplacement bool
		vanity          []string
		world           world
		wantCode        int
		wantOut         []string
		absent          []string
		wantErr         string
	}{
		{
			name:     "a delegated domain passes",
			recorded: true,
			world:    delegated,
			wantOut:  []string{"example.dev is delegated to the zone imp-net-gbl-dns-apps in imp-net-gbl-core-9c0d: it answers the zone's name servers (" + checkServer + ")."},
			absent:   []string{"zoneReplacement"},
		},
		{
			name:            "a delegated domain passes, and zoneReplacement set can be cleared",
			recorded:        true,
			zoneReplacement: true,
			world:           delegated,
			wantOut:         []string{"\nzoneReplacement is set in placement.json and every domain points at its zone: clear it and run bedrock org render.\n"},
		},
		{
			name:     "an undelegated domain exits 1 with the registrar step",
			recorded: true,
			world:    world{},
			wantCode: 1,
			wantOut:  []string{"At the registrar where example.dev is registered: set its name servers to the zone's 1 below", "\n" + strings.TrimSuffix(checkServer, ".") + "\n"},
		},
		{
			name:            "an undelegated domain with zoneReplacement set: the value stays",
			recorded:        true,
			zoneReplacement: true,
			world:           world{},
			wantCode:        1,
			wantOut:         []string{"At the registrar where example.dev is registered"},
			absent:          []string{"zoneReplacement"},
		},
		{
			name:            "the apps domain and a vanity domain delegated: both pass, the value can be cleared",
			recorded:        true,
			zoneReplacement: true,
			vanity:          []string{checkVanity},
			world:           bothDelegated,
			wantOut: []string{
				"example.dev is delegated to the zone imp-net-gbl-dns-apps in imp-net-gbl-core-9c0d",
				"\n\nharborlights.example is delegated to the zone imp-net-gbl-dns-harborlights-example in imp-net-gbl-core-9c0d: it answers the zone's name servers (" + checkServer + ").",
				"The authorization record resolves: the certificate for harborlights.example and *.harborlights.example is not waiting on it.\nzoneReplacement is set in placement.json and every domain points at its zone: clear it and run bedrock org render.\n",
			},
		},
		{
			name:            "the apps domain delegated and a vanity domain not: exit 1 with the vanity domain's registrar step, the value staying",
			recorded:        true,
			zoneReplacement: true,
			vanity:          []string{checkVanity},
			world:           delegated,
			wantCode:        1,
			wantOut: []string{
				"example.dev is delegated to the zone imp-net-gbl-dns-apps in imp-net-gbl-core-9c0d",
				"harborlights.example does not answer the zone's name servers (it answers none).",
				"At the registrar where harborlights.example is registered: set its name servers to the zone's 1 below",
			},
			absent: []string{"zoneReplacement"},
		},
		{
			name:     "a vanity domain whose zone 2-net has not made stops the check after the apps domain",
			recorded: true,
			vanity:   []string{"beacon.example"},
			world:    delegated,
			wantErr:  "no zone imp-net-gbl-dns-beacon-example in imp-net-gbl-core-9c0d: the apply of 2-net creates it",
		},
		{
			name:     "refused before 1-org's project_ids are recorded",
			recorded: false,
			world:    delegated,
			wantErr:  "placement.json records no network project (projects.net)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := checkRepo(t, tt.recorded, tt.zoneReplacement, tt.vanity)
			d := orgDeps(dir)
			d.lookups = &domain.Lookups{
				OpenZones: func(context.Context) (domain.ZoneReader, error) {
					return zones, nil
				},
				Resolver: tt.world,
			}
			out, err := execute(d, "", "domain", "check", "--dir", dir)
			code := 0
			if err != nil {
				var exit exitError
				if !asExit(err, &exit) {
					if tt.wantErr == "" || !strings.Contains(err.Error(), tt.wantErr) {
						t.Fatalf("Execute() error = %v, wantErr %q", err, tt.wantErr)
					}

					return
				}
				code = exit.code
			}
			if tt.wantErr != "" {
				t.Fatalf("Execute() error = %v, wantErr %q", err, tt.wantErr)
			}
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d:\n%s", code, tt.wantCode, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(out, a) {
					t.Errorf("output carries %q:\n%s", a, out)
				}
			}
		})
	}
}
