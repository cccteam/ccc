package org

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/render"
)

// TestPlacedEnvironments: an application's entry names a prefix of the promotion order or
// nothing, which means every environment; the registration starts in the environments
// named; a list that is not a prefix is refused.
func TestPlacedEnvironments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		envs    []string
		wantErr string
	}{
		{name: "every environment by default"},
		{name: "the first alone", envs: []string{"tst"}},
		{name: "the first two", envs: []string{"tst", "stg"}},
		{name: "every environment named", envs: []string{"tst", "stg", "prd"}},
		{name: "a later environment alone is refused", envs: []string{"stg"}, wantErr: "application quill's environments (stg) are not a prefix of the promotion order (tst, stg, prd): an application is placed in tst, in tst and stg, or in every environment"},
		{name: "a gap is refused", envs: []string{"tst", "prd"}, wantErr: "are not a prefix of the promotion order"},
		{name: "an environment the model lacks is refused", envs: []string{"tst", "qa"}, wantErr: "are not a prefix of the promotion order"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			err := p.Register("quill", "quill", tt.envs)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Register() error = %v, wantErr %q", err, tt.wantErr)
				}
				if p.Registered("quill") {
					t.Error("a refused registration added the application")
				}

				return
			}
			if err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			want := tt.envs
			if len(want) == 0 {
				want = Environments
			}
			if got := p.PlacedEnvironments("quill"); !slices.Equal(got, want) {
				t.Errorf("PlacedEnvironments() = %v, want %v", got, want)
			}
			if got := p.UnderWay("quill"); !slices.Equal(got, want) {
				t.Errorf("UnderWay() = %v, want %v", got, want)
			}
			if got := p.Step("quill"); got != RegistrationIdentities {
				t.Errorf("Step() = %q, want identities", got)
			}
			entry := p.Applications[len(p.Applications)-1]
			if len(want) == len(Environments) && entry.Environments != nil {
				t.Errorf("an application placed in every environment records %v; the file leaves the list out", entry.Environments)
			}
			for _, env := range Environments {
				if got := p.Placed("quill", env); got != slices.Contains(want, env) {
					t.Errorf("Placed(%s) = %t", env, got)
				}
			}
			if err := p.Validate(); err != nil {
				t.Errorf("Validate() error = %v", err)
			}
			if p.Placed("nobody", "tst") {
				t.Error("an application the placement does not list is placed somewhere")
			}
		})
	}
}

// TestPlacedValidate: the placement refuses an empty environments list, a registration
// step in an environment the application is not placed in, steps that differ between an
// application's environments, and an out-of-service or hostname record in an environment
// the application is not placed in; the older form of the registration field, one step for
// the application, reads as every placed environment at that step.
func TestPlacedValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(p *Placement)
		wantErr string
	}{
		{name: "an empty list is refused", mutate: func(p *Placement) { p.Applications[1].Environments = []string{} }, wantErr: "application beacon is placed in no environment"},
		{name: "a step where the application is not placed is refused", mutate: func(p *Placement) {
			p.Applications[1].Environments = []string{"tst"}
			p.Registration = map[string]RegistrationSteps{"beacon": {"stg": RegistrationIdentities}}
		}, wantErr: `registration of "beacon" names stg, which it is not placed in (tst)`},
		{name: "steps that differ are refused", mutate: func(p *Placement) {
			p.Registration = map[string]RegistrationSteps{"beacon": {"tst": RegistrationGrants, "stg": RegistrationIdentities}}
		}, wantErr: `registration of "beacon" is at identities in one environment and grants in another: every environment under way moves together`},
		{name: "no environment under way is refused", mutate: func(p *Placement) { p.Registration = map[string]RegistrationSteps{"beacon": {}} }, wantErr: `registration of "beacon" names no environment`},
		{name: "out of service where not placed is refused", mutate: func(p *Placement) {
			p.Applications[1].Environments = []string{"tst"}
			p.OutOfService = map[string][]string{"beacon": {"stg"}}
		}, wantErr: `outOfService names "stg" for "beacon", which it is not placed in (tst)`},
		{name: "a hostname record where not placed is refused", mutate: func(p *Placement) {
			p.Applications[1].Environments = []string{"tst"}
			p.Hostnames = map[string]map[string]map[string]string{"beacon": {"stg": {"beacon-stg.apps.imp.example": "projects/p/global/backendServices/b"}}}
		}, wantErr: "hostnames.beacon names stg, which it is not placed in (tst)"},
		{name: "a prefix with the registration under way in it", mutate: func(p *Placement) {
			p.Applications[1].Environments = []string{"tst", "stg"}
			p.Registration = map[string]RegistrationSteps{"beacon": {"stg": RegistrationGrants}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			tt.mutate(p)
			err := p.Validate()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("Validate() error = %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("Validate() error = %v, wantErr %q", err, tt.wantErr)
			}
		})
	}

	t.Run("the older form of the field reads as every placed environment", func(t *testing.T) {
		t.Parallel()

		data, err := os.ReadFile(filepath.Join(fixture, "placement.json"))
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		raw["registration"] = map[string]any{"beacon": RegistrationGrants}
		data, err = json.Marshal(raw)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "placement.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		p, err := ReadPlacement(path)
		if err != nil {
			t.Fatalf("ReadPlacement() error = %v", err)
		}
		if got := p.Registration["beacon"]; fmt.Sprint(got) != fmt.Sprint(EveryEnvironment(RegistrationGrants)) {
			t.Errorf("Registration[beacon] = %v, want every environment at grants", got)
		}
		if got := p.GrantedEnvironments("beacon"); !slices.Equal(got, Environments) {
			t.Errorf("GrantedEnvironments() = %v, want every environment", got)
		}
		if got := p.ServedEnvironments("beacon"); len(got) != 0 {
			t.Errorf("ServedEnvironments() = %v, want none at the grants step", got)
		}
	})
}

// TestPlacedRender: an application placed in the first environment alone has its identities,
// grants and hostnames rendered there alone, the other applications' everywhere, and the
// capacity count counts the one database it has.
func TestPlacedRender(t *testing.T) {
	t.Parallel()

	p := testPlacement(t)
	p.Applications[1].Environments = []string{"tst"}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	files, err := Render(p)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	text := func(path string) string {
		t.Helper()
		for _, f := range files {
			if f.Path == path {
				return string(f.Content)
			}
		}
		t.Fatalf("%s is not rendered", path)

		return ""
	}
	want := map[string][]string{
		"1-org/applications.auto.tfvars":    {`  harbor = ["tst", "stg", "prd"]`, `  beacon = ["tst"]`, `applied_applications = ["harbor", "beacon"]`},
		"2-env/applications.auto.tfvars":    {`  harbor = ["tst", "stg", "prd"]`, `  beacon = ["tst"]`},
		"1-org/public-invokers.auto.tfvars": {`tst = "serviceAccount:imp-tst-gbl-beacon-tofu@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com"`, `stg = "serviceAccount:imp-stg-gbl-harbor-tofu@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com"`},
		"2-shr/applications.auto.tfvars":    {`"serviceAccount:imp-tst-gbl-beacon-deploy@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com",`, `"serviceAccount:imp-prd-gbl-harbor-deploy@imp-prd-gbl-core-5e6f.iam.gserviceaccount.com",`},
		"2-spn/applications.auto.tfvars":    {`application = "harbor", restore_from = "prd"`},
		"2-net/applications.auto.tfvars":    {`"beacon-tst.apps.imp.example"`, `"harbor.apps.imp.example"`},
		"2-env/locals.tf":                   {`applications = [for app in var.applications : app if contains(var.application_environments[app], var.environment)]`},
		"1-org/github.tf":                   {`if contains(var.application_environments[pair[0]], pair[1])`},
	}
	absent := map[string][]string{
		"1-org/public-invokers.auto.tfvars": {`imp-stg-gbl-beacon-tofu`, `imp-prd-gbl-beacon-tofu`},
		"2-shr/applications.auto.tfvars":    {`imp-stg-gbl-beacon-deploy`, `imp-stg-gbl-beacon-tofu`},
		"2-spn/applications.auto.tfvars":    {`beacon`},
		"2-net/applications.auto.tfvars":    {`"beacon-stg.apps.imp.example"`, `"beacon.apps.imp.example"`, `"beacon-next.apps.imp.example"`},
	}
	for path, lines := range want {
		got := text(path)
		for _, line := range lines {
			if !strings.Contains(got, line) {
				t.Errorf("%s lacks %q:\n%s", path, line, got)
			}
		}
	}
	for path, lines := range absent {
		got := text(path)
		for _, line := range lines {
			if strings.Contains(got, line) {
				t.Errorf("%s carries %q, which the placement leaves out:\n%s", path, line, got)
			}
		}
	}
	shared := p.instanceCapacity("", []string{"stg", "prd"}, 100)
	if shared.Applications != 1 || shared.Declared != 2 {
		t.Errorf("the shared instance counts %d applications and %d databases, want 1 and 2", shared.Applications, shared.Declared)
	}
	own := p.instanceCapacity("tst", []string{"tst"}, 100)
	if own.Applications != 2 || own.Declared != 2 {
		t.Errorf("tst's own instance counts %d applications and %d databases, want 2 and 2", own.Applications, own.Declared)
	}
	if got := shared.Problem(); !strings.Contains(got, "(1 applications, a database for each of stg and prd an application is placed in)") {
		t.Errorf("Problem() = %s", got)
	}
}

// TestPlace: an application leaves an environment once it is out of service there, and
// reaches one through the registration's steps; the other changes are refused.
func TestPlace(t *testing.T) {
	t.Parallel()

	p := testPlacement(t)
	refused := func(envs []string, wantErr string) {
		t.Helper()
		if _, _, err := p.Place("beacon", envs); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("Place(%v) error = %v, wantErr %q", envs, err, wantErr)
		}
	}
	if _, _, err := p.Place("nobody", []string{"tst"}); err == nil || !strings.Contains(err.Error(), `application "nobody" is not registered`) {
		t.Fatalf("Place() of an unregistered application = %v", err)
	}
	refused([]string{"stg"}, "are not a prefix of the promotion order")
	refused([]string{"tst", "stg", "prd"}, "beacon is placed in tst, stg, prd already")
	refused([]string{"tst"}, "beacon is in service in stg, prd: an environment leaves the list once its stack is down there, so that nothing the layers own is left behind. First bedrock org teardown beacon stg prd")

	backend := func(env string) string {
		return "projects/imp-" + env + "-gbl-core-0000/global/backendServices/imp-" + env + "-gbl-beacon-backend"
	}
	p.Hostnames = map[string]map[string]map[string]string{"beacon": {
		"tst": {"beacon-tst.apps.imp.example": backend("tst")},
		"stg": {"beacon-stg.apps.imp.example": backend("stg")},
		"prd": {"beacon.apps.imp.example": backend("prd")},
	}}
	p.OutOfService = map[string][]string{"beacon": {"stg", "prd"}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	added, dropped, err := p.Place("beacon", []string{"tst"})
	if err != nil {
		t.Fatalf("Place() error = %v", err)
	}
	if len(added) != 0 || !slices.Equal(dropped, []string{"stg", "prd"}) {
		t.Errorf("Place() added %v and dropped %v, want none and stg, prd", added, dropped)
	}
	if got := p.PlacedEnvironments("beacon"); !slices.Equal(got, []string{"tst"}) {
		t.Errorf("PlacedEnvironments() = %v, want tst", got)
	}
	if p.OutOfService != nil {
		t.Errorf("outOfService = %v, want none: the dropped environments' entries go with them", p.OutOfService)
	}
	if got := p.Hostnames["beacon"]; len(got) != 1 || got["tst"] == nil {
		t.Errorf("hostnames = %v, want tst's alone", got)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate() after the drop: %v", err)
	}
	if !p.Routed("beacon", "tst") || p.Routed("beacon", "stg") {
		t.Error("Routed() after the drop: want tst routed and stg not")
	}

	added, dropped, err = p.Place("beacon", []string{"tst", "stg"})
	if err != nil {
		t.Fatalf("Place() error = %v", err)
	}
	if !slices.Equal(added, []string{"stg"}) || len(dropped) != 0 {
		t.Errorf("Place() added %v and dropped %v, want stg and none", added, dropped)
	}
	if got := p.UnderWay("beacon"); !slices.Equal(got, []string{"stg"}) {
		t.Errorf("UnderWay() = %v, want stg", got)
	}
	if got := p.GrantedEnvironments("beacon"); !slices.Equal(got, []string{"tst"}) {
		t.Errorf("GrantedEnvironments() = %v, want tst alone while stg is at its identities", got)
	}
	if got := p.ServedEnvironments("beacon"); !slices.Equal(got, []string{"tst"}) {
		t.Errorf("ServedEnvironments() = %v, want tst", got)
	}
	if p.Routed("beacon", "stg") {
		t.Error("Routed(stg) = true while the registration there is under way")
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate() after the extension: %v", err)
	}
	refused([]string{"tst", "stg", "prd"}, "beacon's registration is at the identities step in stg: finish it (bedrock org register beacon --next) before placing it elsewhere")
	if step, err := p.Advance("beacon"); err != nil || step != RegistrationGrants {
		t.Fatalf("Advance() = %q, %v, want grants", step, err)
	}
	if got := p.GrantedEnvironments("beacon"); !slices.Equal(got, []string{"tst", "stg"}) {
		t.Errorf("GrantedEnvironments() = %v, want tst, stg", got)
	}
	if step, err := p.Advance("beacon"); err != nil || step != "" {
		t.Fatalf("Advance() = %q, %v, want complete", step, err)
	}
	if got := p.ServedEnvironments("beacon"); !slices.Equal(got, []string{"tst", "stg"}) || p.Registration != nil {
		t.Errorf("ServedEnvironments() = %v, Registration = %v, want tst, stg and no registration", got, p.Registration)
	}
	files, err := Render(p)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	var hosts render.File
	for _, f := range files {
		if f.Path == "2-net/applications.auto.tfvars" {
			hosts = f
		}
	}
	if got := string(hosts.Content); !strings.Contains(got, `"beacon-tst.apps.imp.example"`) || strings.Contains(got, `"beacon.apps.imp.example"`) {
		t.Errorf("2-net after the extension:\n%s", got)
	}
}

// TestApplicationPlacementPlaced: the first placement of an application placed in a prefix
// lists those environments and their projects, names production, and seeds every
// environment it is placed in, since nothing restores them from production's backups.
func TestApplicationPlacementPlaced(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		envs     []string
		wantSeed []string
	}{
		{name: "the first alone", envs: []string{"tst"}, wantSeed: []string{"tst"}},
		{name: "the first two", envs: []string{"tst", "stg"}, wantSeed: []string{"tst", "stg"}},
		{name: "every environment", wantSeed: []string{"tst"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			p.Applications[1].Environments = tt.envs
			a, err := p.ApplicationPlacement("beacon", "beacon", "v0.0.0-20260928182105-6f6f7795969d", "")
			if err != nil {
				t.Fatalf("ApplicationPlacement() error = %v", err)
			}
			want := tt.envs
			if len(want) == 0 {
				want = Environments
			}
			if !slices.Equal(a.Environments, want) {
				t.Errorf("Environments = %v, want %v", a.Environments, want)
			}
			if a.ProductionEnvironment != "prd" || a.Production() != "prd" {
				t.Errorf("production = %q, want prd", a.ProductionEnvironment)
			}
			if !slices.Equal(a.Seed, tt.wantSeed) {
				t.Errorf("Seed = %v, want %v", a.Seed, tt.wantSeed)
			}
			for env := range a.Projects {
				if !slices.Contains(want, env) {
					t.Errorf("Projects names %s, which the application is not placed in", env)
				}
			}
			if len(a.Projects) != len(want) {
				t.Errorf("Projects = %v, want one per environment placed in", a.Projects)
			}
		})
	}
}
