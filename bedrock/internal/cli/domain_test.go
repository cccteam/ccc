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

// The fake zone of domain check's command tests: the fixture organization's network
// project and zone, serving example.dev.
const (
	checkDomain = "example.dev"
	checkServer = "ns-cloud-c1.googledomains.com."
	checkAuth   = "0f1e2d3c-4b5a.6.authorize.certificatemanager.goog."
)

// oneZone is a Cloud DNS holding the apps zone for checkDomain in any project.
type oneZone struct{}

func (oneZone) ManagedZone(context.Context, string, string) (*domain.Zone, error) {
	return &domain.Zone{DNSName: checkDomain + ".", NameServers: []string{checkServer}}, nil
}

func (oneZone) RecordSets(context.Context, string, string) ([]domain.Record, error) {
	return []domain.Record{
		{Name: checkDomain + ".", Type: "A", Data: []string{"203.0.113.10"}},
		{Name: "*." + checkDomain + ".", Type: "A", Data: []string{"203.0.113.10"}},
		{Name: "_acme-challenge." + checkDomain + ".", Type: "CNAME", Data: []string{checkAuth}},
	}, nil
}

func (oneZone) Close() error {
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
// domain, with the network project recorded or not.
func checkRepo(t *testing.T, recorded bool) string {
	t.Helper()

	dir := t.TempDir()
	p, err := org.ReadPlacement(filepath.Join(orgFixture, "placement.json"))
	if err != nil {
		t.Fatal(err)
	}
	p.AppsDomain = checkDomain
	if !recorded {
		p.Projects = nil
		p.ProjectNumbers = nil
	}
	if err := p.Write(filepath.Join(dir, "placement.json")); err != nil {
		t.Fatal(err)
	}

	return dir
}

// TestDomainCheck runs domain check over a fake zone and a fake world: it passes on a
// delegated domain, exits 1 with the registrar step on one that is not, and is refused
// before the network project is recorded.
func TestDomainCheck(t *testing.T) {
	t.Parallel()

	delegated := world{ns: map[string]string{checkDomain: checkServer}, cname: map[string]string{"_acme-challenge." + checkDomain + ".": checkAuth}}
	tests := []struct {
		name     string
		recorded bool
		world    world
		wantCode int
		wantOut  []string
		wantErr  string
	}{
		{
			name:     "a delegated domain passes",
			recorded: true,
			world:    delegated,
			wantOut:  []string{"example.dev is delegated to the zone imp-net-gbl-dns-apps in imp-net-gbl-core-9c0d: it answers the zone's name servers (" + checkServer + ")."},
		},
		{
			name:     "an undelegated domain exits 1 with the registrar step",
			recorded: true,
			world:    world{},
			wantCode: 1,
			wantOut:  []string{"At the registrar where example.dev is registered: set its name servers to the zone's 1 below", "\n" + strings.TrimSuffix(checkServer, ".") + "\n"},
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

			dir := checkRepo(t, tt.recorded)
			d := orgDeps(dir)
			d.lookups = &domain.Lookups{
				OpenZones: func(context.Context) (domain.ZoneReader, error) {
					return oneZone{}, nil
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
		})
	}
}
