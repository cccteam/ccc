package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// serviceResult is what a run of org teardown <app> or org serve left: what it printed,
// its error, the placement after it, 2-net's hosts file and the journal.
type serviceResult struct {
	out     string
	err     error
	p       *org.Placement
	hosts   string
	journal string
}

// serviceRun runs org teardown <app> or org serve against the fixture organization.
func serviceRun(t *testing.T, dir string, store *memoryStore, interactive func() bool, in string, args ...string) serviceResult {
	t.Helper()
	d := orgDeps(dir)
	d.interactive = interactive
	d.deploy = &deploy.Clients{Storage: store.open, Exec: &teardownTofu{}}
	out, err := execute(d, in, append(args, "--dir", dir)...)
	p, readErr := org.ReadPlacement(filepath.Join(dir, placementFile))
	if readErr != nil {
		t.Fatal(readErr)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "2-net", "applications.auto.tfvars"))
	lines, _ := os.ReadFile(filepath.Join(dir, "JOURNAL.md"))

	return serviceResult{out: out, err: err, p: p, hosts: string(data), journal: string(lines)}
}

func TestOrgTeardownApplication(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		before      map[string][]string
		tornDown    bool
		interactive func() bool
		in          string
		args        []string
		wantErr     string
		wantOut     []string
		wantAfter   map[string][]string
		wantHosts   []string
		wantNoHosts []string
		wantJournal string
	}{
		{
			name:    "an application not registered is refused",
			args:    []string{"nobody", "--yes"},
			wantErr: `application "nobody" is not registered (bedrock org register nobody)`,
		},
		{
			name:    "an environment the model lacks is refused",
			args:    []string{"harbor", "qa", "--yes"},
			wantErr: `environment "qa" is not one of tst, stg, prd`,
		},
		{
			name:        "the organization torn down records the entry alone",
			tornDown:    true,
			interactive: never,
			args:        []string{"harbor", "--yes"},
			wantOut:     []string{"The imp organization is torn down (teardown in placement.json), so nothing leaves 2-net now: the entry keeps harbor out when bedrock org serve brings the organization back"},
			wantAfter:   map[string][]string{"harbor": {"tst", "stg", "prd"}},
			wantHosts:   []string{"hosts   = {}", "serving = false"},
			wantJournal: "bedrock org teardown harbor: harbor's hostnames in tst, stg, prd leave 2-net",
		},
		{
			name:        "the first environment out, with the application typed back",
			interactive: always,
			in:          "harbor\n",
			args:        []string{"harbor", "tst"},
			wantOut:     []string{"bedrock org teardown harbor takes harbor out of service in tst.", "Type harbor to go on; anything else stops:", "Recorded harbor out of service in tst", "2-net's applications.auto.tfvars no longer routes harbor's hostnames there.", "Then, from harbor's checkout: bedrock teardown tst (or bedrock destroy)."},
			wantAfter:   map[string][]string{"harbor": {"tst"}},
			wantHosts:   []string{`"harbor-stg.`, `"harbor.`, `"beacon-tst.`},
			wantNoHosts: []string{`"harbor-tst.`, `"harbor-tst-next.`},
			wantJournal: "bedrock org teardown harbor: harbor's hostnames in tst leave 2-net on this pull request (outOfService in placement.json); the listeners and the other applications stay.",
		},
		{
			name:        "every environment out with --yes, after one already out",
			before:      map[string][]string{"harbor": {"stg"}},
			interactive: never,
			args:        []string{"harbor", "--yes"},
			wantOut:     []string{"takes harbor out of service in tst, prd.", "Then, from harbor's checkout: bedrock teardown --all (or bedrock destroy)."},
			wantAfter:   map[string][]string{"harbor": {"tst", "stg", "prd"}},
			wantHosts:   []string{`"beacon-tst.`, `"beacon.`},
			wantNoHosts: []string{`"harbor-tst.`, `"harbor-stg.`, `"harbor.`, `"harbor-next.`},
			wantJournal: "bedrock org teardown harbor: harbor's hostnames in tst, prd leave 2-net",
		},
		{
			name:    "out of service already is refused",
			before:  map[string][]string{"harbor": {"tst", "stg", "prd"}},
			args:    []string{"harbor", "--yes"},
			wantErr: "harbor is out of service in tst, stg, prd already (outOfService in placement.json)",
		},
		{
			name:        "stopped at the prompt changes nothing",
			interactive: always,
			in:          "no\n",
			args:        []string{"harbor"},
			wantOut:     []string{"Stopped: nothing was destroyed."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			editPlacement(t, dir, func(p *org.Placement) { p.OutOfService = tt.before; p.Teardown = tt.tornDown })
			interactive := tt.interactive
			if interactive == nil {
				interactive = never
			}
			r := serviceRun(t, dir, orgTeardownStore(t, false), interactive, tt.in, append([]string{"org", "teardown"}, tt.args...)...)
			out, err, p, hosts, journal := r.out, r.err, r.p, r.hosts, r.journal
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("org teardown = %v, want no error; printed:\n%s", err, out)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("org teardown = %v, want an error containing %q; printed:\n%s", err, tt.wantErr, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q; printed:\n%s", want, out)
				}
			}
			if tt.wantErr == "" && fmt.Sprint(p.OutOfService) != fmt.Sprint(tt.wantAfter) {
				t.Errorf("outOfService = %v, want %v", p.OutOfService, tt.wantAfter)
			}
			if tt.wantErr != "" && fmt.Sprint(p.OutOfService) != fmt.Sprint(tt.before) {
				t.Errorf("a refused run changed outOfService to %v", p.OutOfService)
			}
			for _, want := range tt.wantHosts {
				if !strings.Contains(hosts, want) {
					t.Errorf("2-net lacks %q:\n%s", want, hosts)
				}
			}
			for _, want := range tt.wantNoHosts {
				if strings.Contains(hosts, want) {
					t.Errorf("2-net still routes %q:\n%s", want, hosts)
				}
			}
			if tt.wantJournal != "" && !strings.Contains(journal, tt.wantJournal) {
				t.Errorf("the journal lacks %q:\n%s", tt.wantJournal, journal)
			}
			if tt.wantJournal == "" && journal != "" {
				t.Errorf("the journal was written:\n%s", journal)
			}
		})
	}
}

func TestOrgServe(t *testing.T) {
	t.Parallel()

	// The store: harbor stands in tst, has no stack in stg (stg gives it one), and is torn
	// down in prd with no release since (back puts a release after that teardown).
	tests := []struct {
		name        string
		before      map[string][]string
		tornDown    bool
		stg         bool
		back        bool
		args        []string
		wantErr     string
		wantOut     []string
		wantAfter   map[string][]string
		wantFlag    bool
		wantHosts   []string
		wantNoHosts []string
		wantJournal string
	}{
		{
			name:    "an application in service is refused",
			args:    []string{"harbor"},
			wantErr: "harbor is in service in tst, stg, prd already: nothing to put back (outOfService in placement.json)",
		},
		{
			name:    "an environment still torn down is refused",
			before:  map[string][]string{"harbor": {"tst", "prd"}},
			args:    []string{"harbor"},
			wantErr: "harbor is torn down in prd, with no release run since: run the operations workflow's apply action there, then bedrock rerun prd <release>, then bedrock org serve again",
		},
		{
			name:    "an environment with no stack is refused",
			before:  map[string][]string{"harbor": {"stg"}},
			args:    []string{"harbor"},
			wantErr: "harbor has no stack in stg: a host names a backend service that must exist. Run the operations workflow's apply action there, then bedrock rerun stg <release>, then bedrock org serve again",
		},
		{
			name:        "one environment back, the other left out",
			before:      map[string][]string{"harbor": {"tst", "prd"}},
			args:        []string{"harbor", "tst"},
			wantOut:     []string{"Recorded harbor in service in tst", "2-net's applications.auto.tfvars routes harbor's hostnames there again.", "the plan run shows harbor's host rules added to 2-net"},
			wantAfter:   map[string][]string{"harbor": {"prd"}},
			wantHosts:   []string{`"harbor-tst.`, `"harbor-tst-next.`},
			wantNoHosts: []string{`"harbor.`, `"harbor-next.`},
			wantJournal: "bedrock org serve harbor: harbor's hostnames in tst return to 2-net on this pull request (outOfService in placement.json).",
		},
		{
			name:        "every environment back once a release ran since the teardown",
			before:      map[string][]string{"harbor": {"tst", "prd"}},
			back:        true,
			args:        []string{"harbor"},
			wantOut:     []string{"Recorded harbor in service in tst, prd"},
			wantAfter:   nil,
			wantHosts:   []string{`"harbor-tst.`, `"harbor.`},
			wantJournal: "harbor's hostnames in tst, prd return to 2-net",
		},
		{
			name:    "the organization in service is refused",
			wantErr: "the imp organization is in service (no teardown in placement.json); an application out of service is put back with bedrock org serve <app>",
		},
		{
			name:     "the organization's way back refuses a stack still torn down",
			before:   map[string][]string{"beacon": {"tst", "stg", "prd"}},
			tornDown: true,
			stg:      true,
			wantErr:  "harbor is torn down in prd, with no release run since",
		},
		{
			name:     "the organization's way back refuses an environment with no stack",
			before:   map[string][]string{"beacon": {"tst", "stg", "prd"}},
			tornDown: true,
			back:     true,
			wantErr:  "harbor has no stack in stg: a host names a backend service that must exist. Run the operations workflow's apply action there, then bedrock rerun stg <release>, then bedrock org serve again; or bedrock org teardown harbor stg leaves it out",
		},
		{
			name:        "the organization's way back, every stack run again and a never-applied application left out",
			before:      map[string][]string{"beacon": {"tst", "stg", "prd"}},
			tornDown:    true,
			stg:         true,
			back:        true,
			wantOut:     []string{"Removed teardown from", "2-net's applications.auto.tfvars routes every host again and sets serving = true."},
			wantAfter:   map[string][]string{"beacon": {"tst", "stg", "prd"}},
			wantFlag:    true,
			wantHosts:   []string{`"harbor-tst.`, `"harbor.`},
			wantNoHosts: []string{"serving = false", `"beacon`},
			wantJournal: "bedrock org serve: the hosts and the load balancer's listeners return to 2-net on this pull request (teardown removed from placement.json); the applications (harbor, beacon) are applied and run again.",
		},
		{
			name:     "the organization torn down refuses an application's way back",
			tornDown: true,
			before:   map[string][]string{"harbor": {"tst"}},
			args:     []string{"harbor"},
			wantErr:  "the imp organization is torn down (teardown in placement.json): bedrock org serve, with no application, is its way back",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			editPlacement(t, dir, func(p *org.Placement) { p.OutOfService = tt.before; p.Teardown = tt.tornDown })
			store := orgTeardownStore(t, false)
			if tt.stg {
				store.objects["gs://imp-boot-gbl-state-a1b2/3-app/harbor/stg/default.tfstate"] = `{"outputs": {"net_hosts": {"value": {"harbor-stg.impulseframework.dev": "projects/p/global/backendServices/imp-stg-gbl-harbor-backend"}}, "substitutions": {"value": {"_RECORDS_BUCKET": "imp-stg-gbl-harbor-records", "_PROJECT": "imp-stg-gbl-core", "_SERVICES": "uc1=x"}}}}`
				store.objects["gs://imp-stg-gbl-harbor-records/harbor/stacks/stg/stack.json"] = `{"id": "2b3c"}`
			}
			if tt.back {
				store.objects["gs://imp-prd-gbl-harbor-records/harbor/prd/3c4d/releases/v0.1.0/b1.json"] = `{"app": "harbor", "version": "v0.1.0", "status": "live", "timestamp": "2026-10-15T12:00:00Z"}`
			}
			r := serviceRun(t, dir, store, never, "", append([]string{"org", "serve"}, tt.args...)...)
			out, err, p, hosts, journal := r.out, r.err, r.p, r.hosts, r.journal
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("org serve = %v, want no error; printed:\n%s", err, out)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("org serve = %v, want an error containing %q; printed:\n%s", err, tt.wantErr, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q; printed:\n%s", want, out)
				}
			}
			if tt.wantErr == "" && fmt.Sprint(p.OutOfService) != fmt.Sprint(tt.wantAfter) {
				t.Errorf("outOfService = %v, want %v", p.OutOfService, tt.wantAfter)
			}
			if tt.wantErr != "" && (fmt.Sprint(p.OutOfService) != fmt.Sprint(tt.before) || p.Teardown != tt.tornDown) {
				t.Errorf("a refused run changed the placement: outOfService %v, teardown %v", p.OutOfService, p.Teardown)
			}
			if tt.wantFlag && p.Teardown {
				t.Error("the placement still says teardown")
			}
			for _, want := range tt.wantHosts {
				if !strings.Contains(hosts, want) {
					t.Errorf("2-net lacks %q:\n%s", want, hosts)
				}
			}
			for _, want := range tt.wantNoHosts {
				if strings.Contains(hosts, want) {
					t.Errorf("2-net carries %q:\n%s", want, hosts)
				}
			}
			if tt.wantJournal != "" && !strings.Contains(journal, tt.wantJournal) {
				t.Errorf("the journal lacks %q:\n%s", tt.wantJournal, journal)
			}
			if tt.wantJournal == "" && journal != "" {
				t.Errorf("the journal was written:\n%s", journal)
			}
		})
	}
}
