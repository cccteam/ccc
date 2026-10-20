package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

// TestOrgHostnames: org hostnames records what an application's stacks serve, read from
// their states, renders 2-net from the record and leaves the pull request; it refuses an
// application still in its registration, a state without the hostnames output, and a
// hostname another application serves, and changes nothing when the record is current.
func TestOrgHostnames(t *testing.T) {
	t.Parallel()

	// stackState is a stack's state with the hostnames it serves in the environment.
	stackState := func(t *testing.T, hosts ...string) string {
		t.Helper()
		data, err := json.Marshal(map[string]any{"outputs": map[string]any{
			"hostnames": map[string]any{"value": hosts},
			"net_hosts": map[string]any{"value": map[string]string{hosts[0]: "projects/p/global/backendServices/b"}},
		}})
		if err != nil {
			t.Fatal(err)
		}

		return string(data)
	}
	// lighthouse is harbor's stacks serving the label lighthouse in every environment.
	lighthouse := func(t *testing.T, store *memoryStore) {
		t.Helper()
		for _, env := range []string{"tst", "stg", "prd"} {
			host := "lighthouse-" + env + ".apps.imp.example"
			if env == "prd" {
				host = "lighthouse.apps.imp.example"
			}
			store.objects["gs://imp-boot-gbl-state-a1b2/3-app/harbor/"+env+"/default.tfstate"] = stackState(t, host)
		}
	}
	tests := []struct {
		name        string
		edit        func(p *org.Placement)
		store       func(t *testing.T, store *memoryStore)
		args        []string
		wantErr     string
		wantOut     []string
		wantRecord  map[string][]string
		wantHosts   []string
		wantNoHosts []string
		wantJournal string
	}{
		{
			name:    "an application not registered is refused",
			args:    []string{"quill"},
			wantErr: `application "quill" is not registered (bedrock org register quill)`,
		},
		{
			name:    "an application in its registration is refused",
			edit:    func(p *org.Placement) { p.Registration = map[string]string{"harbor": org.RegistrationGrants} },
			args:    []string{"harbor"},
			wantErr: "harbor is at the grants step of its registration: its hostnames are recorded when the registration completes (bedrock org register harbor --next)",
		},
		{
			name:    "a state without the hostnames output is refused",
			args:    []string{"harbor"},
			wantErr: "harbor's stack state in tst (3-app/harbor/tst) has no hostnames output: apply the stack there (the operations workflow's apply action) before recording its hostnames",
		},
		{
			name:  "a changed label is recorded and 2-net follows",
			store: lighthouse,
			args:  []string{"harbor"},
			wantOut: []string{
				"Recorded harbor's hostnames in", "(tst: lighthouse-tst.apps.imp.example; stg: lighthouse-stg.apps.imp.example; prd: lighthouse.apps.imp.example)",
				"2-net's applications.auto.tfvars routes them.", "Next: commit placement.json,",
			},
			wantRecord:  map[string][]string{"tst": {"lighthouse-tst.apps.imp.example"}, "stg": {"lighthouse-stg.apps.imp.example"}, "prd": {"lighthouse.apps.imp.example"}},
			wantHosts:   []string{`"lighthouse-tst.apps.imp.example"`, `"lighthouse-tst-next.apps.imp.example"`, `"lighthouse.apps.imp.example"`, `"beacon-tst.apps.imp.example"`},
			wantNoHosts: []string{`"harbor-tst.apps.imp.example"`, `"harbor.apps.imp.example"`},
			wantJournal: "bedrock org hostnames harbor: harbor's hostnames recorded from its stacks' states (tst: lighthouse-tst.apps.imp.example; stg: lighthouse-stg.apps.imp.example; prd: lighthouse.apps.imp.example); 2-net's host rules follow on this pull request.",
		},
		{
			name: "a record that is current changes nothing",
			edit: func(p *org.Placement) {
				p.Hostnames = map[string]map[string][]string{"harbor": {"tst": {"lighthouse-tst.apps.imp.example"}, "stg": {"lighthouse-stg.apps.imp.example"}, "prd": {"lighthouse.apps.imp.example"}}}
			},
			store:      lighthouse,
			args:       []string{"harbor"},
			wantOut:    []string{"harbor's hostnames are recorded as its stacks serve them (tst: lighthouse-tst.apps.imp.example; stg: lighthouse-stg.apps.imp.example; prd: lighthouse.apps.imp.example); nothing to change."},
			wantRecord: map[string][]string{"tst": {"lighthouse-tst.apps.imp.example"}, "stg": {"lighthouse-stg.apps.imp.example"}, "prd": {"lighthouse.apps.imp.example"}},
		},
		{
			name: "a hostname another application serves is refused",
			store: func(t *testing.T, store *memoryStore) {
				t.Helper()
				for _, env := range []string{"tst", "stg", "prd"} {
					host := "beacon-" + env + ".apps.imp.example"
					if env == "prd" {
						host = "beacon.apps.imp.example"
					}
					store.objects["gs://imp-boot-gbl-state-a1b2/3-app/harbor/"+env+"/default.tfstate"] = stackState(t, host)
				}
			},
			args:    []string{"harbor"},
			wantErr: "hostname beacon-tst.apps.imp.example in tst is harbor's and beacon's: each hostname of an environment belongs to one application",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := orgRepo(t)
			if tt.edit != nil {
				editPlacement(t, dir, tt.edit)
			}
			store := &memoryStore{objects: map[string]string{}}
			if tt.store != nil {
				tt.store(t, store)
			}
			r := serviceRun(t, dir, store, never, "", append([]string{"org", "hostnames"}, tt.args...)...)
			switch {
			case tt.wantErr == "" && r.err != nil:
				t.Fatalf("org hostnames = %v, want no error; printed:\n%s", r.err, r.out)
			case tt.wantErr != "" && (r.err == nil || !strings.Contains(r.err.Error(), tt.wantErr)):
				t.Fatalf("org hostnames = %v, want an error containing %q; printed:\n%s", r.err, tt.wantErr, r.out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(r.out, want) {
					t.Errorf("output lacks %q; printed:\n%s", want, r.out)
				}
			}
			if tt.wantErr == "" && fmt.Sprint(r.p.Hostnames["harbor"]) != fmt.Sprint(tt.wantRecord) {
				t.Errorf("recorded hostnames = %v, want %v", r.p.Hostnames["harbor"], tt.wantRecord)
			}
			for _, want := range tt.wantHosts {
				if !strings.Contains(r.hosts, want) {
					t.Errorf("2-net's hosts lack %s:\n%s", want, r.hosts)
				}
			}
			for _, unwanted := range tt.wantNoHosts {
				if strings.Contains(r.hosts, unwanted) {
					t.Errorf("2-net's hosts still carry %s:\n%s", unwanted, r.hosts)
				}
			}
			if tt.wantJournal != "" && !strings.Contains(r.journal, tt.wantJournal) {
				t.Errorf("journal lacks %q:\n%s", tt.wantJournal, r.journal)
			}
		})
	}
}
