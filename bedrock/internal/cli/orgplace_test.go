package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

// TestOrgPlace: an application leaves environments it is out of service in and reaches new
// ones through the registration's steps, the render and the journal following; the other
// changes are refused and change nothing.
func TestOrgPlace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		before      func(p *org.Placement)
		args        []string
		wantErr     string
		wantOut     []string
		wantPlaced  []string
		wantUnder   []string
		wantHosts   []string
		wantNoHosts []string
		wantFiles   map[string][]string
		wantAbsent  map[string][]string
		wantJournal string
	}{
		{
			name:    "an application not registered is refused",
			args:    []string{"nobody", "tst"},
			wantErr: `application "nobody" is not registered (bedrock org register nobody)`,
		},
		{
			name:    "a list that is not a prefix is refused",
			args:    []string{"beacon", "stg"},
			wantErr: "application beacon's environments (stg) are not a prefix of the promotion order (tst, stg, prd)",
		},
		{
			name:    "the same list is refused",
			args:    []string{"beacon", "tst", "stg", "prd"},
			wantErr: "beacon is placed in tst, stg, prd already",
		},
		{
			name:    "an environment in service is kept",
			args:    []string{"beacon", "tst"},
			wantErr: "beacon is in service in stg, prd: an environment leaves the list once its stack is down there",
		},
		{
			name:        "environments out of service are dropped",
			before:      func(p *org.Placement) { p.OutOfService = map[string][]string{"beacon": {"stg", "prd"}} },
			args:        []string{"beacon", "tst"},
			wantOut:     []string{"Placed beacon in tst (environments in", "beacon leaves stg, prd: the pull request's apply deletes its identities", "set environments in\ninfrastructure/placement.json to [\"tst\"]"},
			wantPlaced:  []string{"tst"},
			wantHosts:   []string{`"beacon-tst.`, `"harbor.`},
			wantNoHosts: []string{`"beacon-stg.`, `"beacon.`},
			wantFiles:   map[string][]string{"2-env/applications.auto.tfvars": {`beacon = ["tst"]`}, "1-org/applications.auto.tfvars": {`beacon = ["tst"]`}},
			wantAbsent:  map[string][]string{"2-shr/applications.auto.tfvars": {"imp-stg-gbl-beacon-deploy"}, "2-spn/applications.auto.tfvars": {"beacon"}},
			wantJournal: "bedrock org place beacon tst: beacon is placed in tst (environments in placement.json), dropping stg, prd.",
		},
		{
			name:        "an environment added starts its registration there",
			before:      func(p *org.Placement) { p.Applications[1].Environments = []string{"tst"} },
			args:        []string{"beacon", "tst", "stg"},
			wantOut:     []string{"Placed beacon in tst, stg (environments in", "1. This render, beacon at its identities in stg: its GitHub Environment (1-org)", "3. bedrock org register beacon --next", `environments in infrastructure/placement.json lists ["tst", "stg"]`},
			wantPlaced:  []string{"tst", "stg"},
			wantUnder:   []string{"stg"},
			wantHosts:   []string{`"beacon-tst.`},
			wantNoHosts: []string{`"beacon-stg.`},
			wantFiles:   map[string][]string{"2-env/applications.auto.tfvars": {`beacon = ["tst", "stg"]`}},
			wantAbsent:  map[string][]string{"2-shr/applications.auto.tfvars": {"imp-stg-gbl-beacon-deploy"}, "2-spn/applications.auto.tfvars": {"imp-stg-gbl-beacon-tofu"}},
			wantJournal: "bedrock org place beacon tst stg: beacon is placed in tst, stg (environments in placement.json), adding stg.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			if tt.before != nil {
				editPlacement(t, dir, tt.before)
			}
			r := serviceRun(t, dir, orgTeardownStore(t, false), never, "", append([]string{"org", "place"}, tt.args...)...)
			switch {
			case tt.wantErr == "" && r.err != nil:
				t.Fatalf("org place = %v, want no error; printed:\n%s", r.err, r.out)
			case tt.wantErr != "" && (r.err == nil || !strings.Contains(r.err.Error(), tt.wantErr)):
				t.Fatalf("org place = %v, want an error containing %q; printed:\n%s", r.err, tt.wantErr, r.out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(r.out, want) {
					t.Errorf("output lacks %q; printed:\n%s", want, r.out)
				}
			}
			if tt.wantErr != "" {
				if r.journal != "" {
					t.Errorf("a refused run wrote the journal:\n%s", r.journal)
				}

				return
			}
			if got := r.p.PlacedEnvironments("beacon"); !slices.Equal(got, tt.wantPlaced) {
				t.Errorf("placed in %v, want %v", got, tt.wantPlaced)
			}
			if got := r.p.UnderWay("beacon"); !slices.Equal(got, tt.wantUnder) {
				t.Errorf("under way in %v, want %v", got, tt.wantUnder)
			}
			for _, want := range tt.wantHosts {
				if !strings.Contains(r.hosts, want) {
					t.Errorf("2-net lacks %q:\n%s", want, r.hosts)
				}
			}
			for _, want := range tt.wantNoHosts {
				if strings.Contains(r.hosts, want) {
					t.Errorf("2-net routes %q:\n%s", want, r.hosts)
				}
			}
			for path, lines := range tt.wantFiles {
				data, err := os.ReadFile(filepath.Join(dir, path))
				if err != nil {
					t.Fatal(err)
				}
				for _, line := range lines {
					if !strings.Contains(string(data), line) {
						t.Errorf("%s lacks %q:\n%s", path, line, data)
					}
				}
			}
			for path, lines := range tt.wantAbsent {
				data, err := os.ReadFile(filepath.Join(dir, path))
				if err != nil {
					t.Fatal(err)
				}
				for _, line := range lines {
					if strings.Contains(string(data), line) {
						t.Errorf("%s carries %q:\n%s", path, line, data)
					}
				}
			}
			if !strings.Contains(r.journal, tt.wantJournal) {
				t.Errorf("the journal lacks %q:\n%s", tt.wantJournal, r.journal)
			}
			if code, out := runOrg(t, "org", "check", "--dir", dir); code != 0 {
				t.Errorf("org check after place: %d %s", code, out)
			}
		})
	}
}
