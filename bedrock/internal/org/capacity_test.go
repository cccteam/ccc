package org

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestSharedCapacity: the databases the placement declares on the shared instance against
// what it holds, from the seeded values: the environments 2-env places there (stg and prd
// by default; the map in the file decides, an environment it leaves out included), the
// processing units 2-spn declares (100 by default), and the refusals of values that are
// not what the layers take.
func TestSharedCapacity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		applications int
		spn, env     string
		want         SharedCapacity
		wantErr      string
	}{
		{
			name:         "the defaults: five applications fill the smallest instance",
			applications: 5,
			want:         SharedCapacity{Environments: []string{"stg", "prd"}, ProcessingUnits: 100, Capacity: 10, Applications: 5, Declared: 10},
		},
		{
			name:         "a sixth application is past it",
			applications: 6,
			spn:          "processing_units = 100 # never above 200\n",
			want:         SharedCapacity{Environments: []string{"stg", "prd"}, ProcessingUnits: 100, Capacity: 10, Applications: 6, Declared: 12},
		},
		{
			name:         "two hundred processing units hold ten applications",
			applications: 6,
			spn:          "processing_units = 200\n",
			want:         SharedCapacity{Environments: []string{"stg", "prd"}, ProcessingUnits: 200, Capacity: 20, Applications: 6, Declared: 12},
		},
		{
			name:         "the integration environment on the shared instance counts",
			applications: 3,
			env:          "spanner_instances = {\n  tst = { placement = \"shared\" }\n  stg = { placement = \"shared\" }\n  prd = { placement = \"shared\" }\n}\n",
			want:         SharedCapacity{Environments: []string{"tst", "stg", "prd"}, ProcessingUnits: 100, Capacity: 10, Applications: 3, Declared: 9},
		},
		{
			name:         "an environment with an instance of its own does not; one the map leaves out does",
			applications: 4,
			env:          "spanner_instances = {\n  tst = { placement = \"own\", config = \"regional-us-central1\" }\n  stg = { placement = \"own\" }\n}\n",
			want:         SharedCapacity{Environments: []string{"prd"}, ProcessingUnits: 100, Capacity: 10, Applications: 4, Declared: 4},
		},
		{
			name:    "processing units that are not a number are refused",
			spn:     "processing_units = \"many\"\n",
			wantErr: "processing_units in " + spnTfvarsFile + " is not a number",
		},
		{
			name:    "processing units that are not whole are refused",
			spn:     "processing_units = 150.5\n",
			wantErr: "is not a whole number above zero",
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
			got, err := p.SharedCapacity(dir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("SharedCapacity() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("SharedCapacity() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("SharedCapacity() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSharedCapacityProblem: the sentence names what is declared, what the instance holds
// at its size, the setting that grows it and the other way out.
func TestSharedCapacityProblem(t *testing.T) {
	t.Parallel()

	c := SharedCapacity{Environments: []string{"stg", "prd"}, ProcessingUnits: 100, Capacity: 10, Applications: 6, Declared: 12}
	if !c.Exceeded() {
		t.Fatal("Exceeded() = false, want true")
	}
	want := "the placement declares 12 databases on the shared Spanner instance (6 applications, one database each in stg and prd), and the instance holds 10 at 100 processing units (ten for every hundred below a node): raise processing_units in 2-spn/terraform.tfvars (100 or 200; beyond 200 is a change to the variable's validation, the decision of 2026-09-25 in JOURNAL.md), or bedrock destroy an application the organization no longer needs, which frees its databases"
	if got := c.Problem(); got != want {
		t.Errorf("Problem() =\n%s\nwant\n%s", got, want)
	}
	if (SharedCapacity{Capacity: 10, Declared: 10}).Exceeded() {
		t.Error("Exceeded() = true for a placement that fills the instance exactly, want false")
	}
}
