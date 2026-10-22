package org

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestInstanceCapacities: each instance's databases against what it holds, from the
// seeded values: by default the shared instance with stg and prd and tst's own, each at
// 100 processing units; the map in 2-env's values decides otherwise (an environment it
// leaves out is on the shared instance, an own instance takes its own processing units);
// 2-spn's values size the shared instance; and the refusals of values that are not what
// the layers take.
func TestInstanceCapacities(t *testing.T) {
	t.Parallel()

	shared := func(apps int, units int, envs ...string) InstanceCapacity {
		return InstanceCapacity{Environments: envs, ProcessingUnits: units, Capacity: units / 10, Applications: apps, Declared: apps * len(envs)}
	}
	own := func(apps int, units int, env string) InstanceCapacity {
		return InstanceCapacity{Environment: env, Environments: []string{env}, ProcessingUnits: units, Capacity: units / 10, Applications: apps, Declared: apps}
	}
	tests := []struct {
		name         string
		applications int
		spn, env     string
		want         []InstanceCapacity
		wantErr      string
	}{
		{
			name:         "the defaults: five applications fill the shared instance and take half of tst's own",
			applications: 5,
			want:         []InstanceCapacity{shared(5, 100, "stg", "prd"), own(5, 100, "tst")},
		},
		{
			name:         "a sixth application is past the shared instance",
			applications: 6,
			spn:          "processing_units = 100 # never above 200\n",
			want:         []InstanceCapacity{shared(6, 100, "stg", "prd"), own(6, 100, "tst")},
		},
		{
			name:         "two hundred processing units in 2-spn hold ten applications on the shared instance",
			applications: 6,
			spn:          "processing_units = 200\n",
			want:         []InstanceCapacity{shared(6, 200, "stg", "prd"), own(6, 100, "tst")},
		},
		{
			name:         "every environment on the shared instance: one instance, three databases each",
			applications: 3,
			env:          "spanner_instances = {\n  tst = { placement = \"shared\" }\n  stg = { placement = \"shared\" }\n  prd = { placement = \"shared\" }\n}\n",
			want:         []InstanceCapacity{shared(3, 100, "tst", "stg", "prd")},
		},
		{
			name:         "own instances take their own processing units; an environment the map leaves out is on the shared instance",
			applications: 4,
			env:          "spanner_instances = {\n  tst = { placement = \"own\", config = \"regional-us-central1\", processing_units = 200 }\n  stg = { placement = \"own\" }\n}\n",
			want:         []InstanceCapacity{shared(4, 100, "prd"), own(4, 200, "tst"), own(4, 100, "stg")},
		},
		{
			name:    "shared processing units that are not a number are refused",
			spn:     "processing_units = \"many\"\n",
			wantErr: "processing_units in " + spnTfvarsFile + " is not a number",
		},
		{
			name:    "processing units that are not whole are refused",
			spn:     "processing_units = 150.5\n",
			wantErr: "is not a whole number above zero",
		},
		{
			name:    "an own instance's processing units that are not a number are refused",
			env:     "spanner_instances = {\n  tst = { placement = \"own\", processing_units = \"two\" }\n}\n",
			wantErr: "spanner_instances.tst.processing_units in " + envTfvarsFile + " is not a number",
		},
		{
			name:    "an instance map that is not written out is refused",
			env:     "spanner_instances = var.elsewhere\n",
			wantErr: "spanner_instances in " + envTfvarsFile + " is not a map written out ({ ... })",
		},
		{
			name:    "an instance entry without its placement is refused",
			env:     "spanner_instances = {\n  stg = { config = \"nam10\" }\n}\n",
			wantErr: "spanner_instances.stg in " + envTfvarsFile + " sets no placement",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			for file, content := range map[string]string{spnTfvarsFile: tt.spn, envTfvarsFile: tt.env} {
				if content == "" {
					continue
				}
				path := filepath.Join(dir, filepath.FromSlash(file))
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			p := &Placement{}
			for i := 0; i < tt.applications; i++ {
				p.Applications = append(p.Applications, Application{Code: "app" + strconv.Itoa(i)})
			}
			got, err := p.InstanceCapacities(dir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("InstanceCapacities() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("InstanceCapacities() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("InstanceCapacities() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestInstanceCapacitySentences: the refusal names the instance, what is declared, what
// it holds at its size, the setting that grows it (2-spn's for the shared instance, the
// environment's entry in 2-env's for its own) and the other way out; the pull-request
// room sentence says what the applications take on the integration environment's
// instance and what is left, or that nothing is.
func TestInstanceCapacitySentences(t *testing.T) {
	t.Parallel()

	sharedFull := InstanceCapacity{Environments: []string{"stg", "prd"}, ProcessingUnits: 100, Capacity: 10, Applications: 6, Declared: 12}
	if !sharedFull.Exceeded() {
		t.Fatal("Exceeded() = false, want true")
	}
	want := "the placement declares 12 databases on the shared Spanner instance (6 applications, one database each in stg and prd), and the instance holds 10 at 100 processing units (ten for every hundred below a node): raise processing_units in 2-spn/terraform.tfvars (100 or 200; beyond 200 is a change to the variable's validation, the decision of 2026-09-25 in JOURNAL.md), or bedrock destroy an application the organization no longer needs, which frees its databases"
	if got := sharedFull.Problem(); got != want {
		t.Errorf("Problem() =\n%s\nwant\n%s", got, want)
	}
	ownFull := InstanceCapacity{Environment: "tst", Environments: []string{"tst"}, ProcessingUnits: 100, Capacity: 10, Applications: 11, Declared: 11}
	want = "the placement declares 11 databases on tst's own Spanner instance (11 applications, one database each in tst), and the instance holds 10 at 100 processing units (ten for every hundred below a node): raise spanner_instances.tst.processing_units in 2-env/terraform.tfvars (100 or 200; beyond 200 is a change to the variable's validation, the decision of 2026-09-25 in JOURNAL.md), or bedrock destroy an application the organization no longer needs, which frees its databases"
	if got := ownFull.Problem(); got != want {
		t.Errorf("Problem() =\n%s\nwant\n%s", got, want)
	}
	if (InstanceCapacity{Capacity: 10, Declared: 10}).Exceeded() {
		t.Error("Exceeded() = true for a placement that fills the instance exactly, want false")
	}
	room := InstanceCapacity{Environment: "tst", Environments: []string{"tst"}, ProcessingUnits: 100, Capacity: 10, Applications: 6, Declared: 6}
	want = "tst's own Spanner instance: 6 of the 10 databases it holds go to the applications, 4 are left for pull-request stacks across every application (each holds a database of its own unless it shares tst's)"
	if got := room.PullRequestRoom("tst"); got != want {
		t.Errorf("PullRequestRoom() =\n%s\nwant\n%s", got, want)
	}
	none := InstanceCapacity{Environments: []string{"tst", "stg", "prd"}, ProcessingUnits: 100, Capacity: 10, Applications: 4, Declared: 12}
	want = "the shared Spanner instance: the applications' databases take all 10 it holds, so no pull-request stack can hold a database of its own there (one shares tst's with /gcbrun shared-db) until processing_units in 2-spn/terraform.tfvars is raised or an application is destroyed"
	if got := none.PullRequestRoom("tst"); got != want {
		t.Errorf("PullRequestRoom() =\n%s\nwant\n%s", got, want)
	}
	capacities := []InstanceCapacity{sharedFull, room}
	if got, ok := IntegrationInstance(capacities); !ok || got.Environment != "tst" {
		t.Errorf("IntegrationInstance() = %+v, %v; want tst's own", got, ok)
	}
	if _, ok := IntegrationInstance([]InstanceCapacity{sharedFull}); ok {
		t.Error("IntegrationInstance() found an instance among those without the integration environment")
	}
}
