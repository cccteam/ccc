package derive

import (
	"strings"
	"testing"
)

// TestProduction: the organization's production environment is the placement's, else the
// last of the application's; an application placed in a prefix of the promotion order is
// not in production, keeps no release backups by default, and seeds every environment past
// the first, since nothing restores them from production's backups.
func TestProduction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		envs       []string
		production string
		seed       []string
		wantProd   string
		wantIn     bool
		wantErr    string
	}{
		{name: "every environment, production unsaid", envs: []string{"tst", "stg", "prd"}, wantProd: "prd", wantIn: true},
		{name: "every environment, production said", envs: []string{"tst", "stg", "prd"}, production: "prd", wantProd: "prd", wantIn: true},
		{name: "the first alone", envs: []string{"tst"}, production: "prd", wantProd: "prd"},
		{name: "the first two, the second seeded", envs: []string{"tst", "stg"}, production: "prd", seed: []string{"tst", "stg"}, wantProd: "prd"},
		{name: "the first two, the second unseeded, is refused", envs: []string{"tst", "stg"}, production: "prd", seed: []string{"tst"}, wantErr: "environment stg is neither seeded (seed) nor restorable from prd's backups, since the application is not placed in prd (environments): list stg under seed, or place the application in prd"},
		{name: "production not last among the environments is refused", envs: []string{"tst", "stg", "prd"}, production: "stg", wantErr: `productionEnvironment "stg" is not the last of the environments (tst, stg, prd)`},
		{name: "a production name that breaks the rule is refused", envs: []string{"tst"}, production: "Prod!", wantErr: `productionEnvironment "Prod!": a lowercase word of two to eight characters`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := fixturePlacement(t, "placement-beacon.json")
			p.Environments = tt.envs
			p.ProductionEnvironment = tt.production
			p.Seed = tt.seed
			p.Projects = nil
			err := p.Validate()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Validate() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if got := p.Production(); got != tt.wantProd {
				t.Errorf("Production() = %q, want %q", got, tt.wantProd)
			}
			if got := p.InProduction(); got != tt.wantIn {
				t.Errorf("InProduction() = %t, want %t", got, tt.wantIn)
			}
			backups := p.ReleaseBackupEnvironments()
			if tt.wantIn && (len(backups) != 1 || backups[0] != tt.wantProd) {
				t.Errorf("ReleaseBackupEnvironments() = %v, want production alone", backups)
			}
			if !tt.wantIn && len(backups) != 0 {
				t.Errorf("ReleaseBackupEnvironments() = %v, want none for an application not placed in production", backups)
			}
		})
	}
}

// TestHostnamesOutsideProduction: an application placed in the first environment alone
// serves its label with the environment's suffix there, never the bare label, which is
// production's.
func TestHostnamesOutsideProduction(t *testing.T) {
	t.Parallel()

	p := fixturePlacement(t, "placement-beacon-tst.json")
	for _, env := range p.environments("beacon", "beacon") {
		if env.Name == "tst" && env.Hostnames[0] != "beacon-tst.impulseframework.dev" {
			t.Errorf("tst's hostname = %q, want beacon-tst.impulseframework.dev: the bare label is production's", env.Hostnames[0])
		}
	}
}
