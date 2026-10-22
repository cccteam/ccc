package deploy

import (
	"strings"
	"testing"
)

// TestGuardSpannerCapacity: before an environment's stack or a pull request's is
// planned, the databases on the instance 2-env's state names are counted against what
// the instance holds at its processing units; a stack whose database is not there is
// refused when the instance is full, with the sentence naming how many of the databases
// are pull-request stacks' (closing one is the first way out then), the setting that
// grows the instance (2-spn's for the shared instance, 2-env's for an environment's own)
// and the other way out; a database already there, in any generation, and room on the
// instance pass; without 2-env's state, its output, a Spanner client or the permission
// nothing is counted.
func TestGuardSpannerCapacity(t *testing.T) {
	t.Parallel()

	const (
		bucket   = "state-b"
		identity = "harbor-plan@p-stg.iam.gserviceaccount.com"
		shared   = `{"outputs": {"spanner_instance": {"value": {"project": "p-spn", "name": "shared-spanner", "config": "nam10"}}}}`
		own      = `{"outputs": {"spanner_instance": {"value": {"project": "p-stg", "name": "imp-stg-gbl-spanner", "config": "regional-us-central1"}}}}`
	)
	full := make([]string, 0, 10)
	for _, code := range []string{"lamp", "pier", "signin", "outl", "sites"} {
		full = append(full, "projects/p-spn/instances/shared-spanner/databases/imp-stg-gbl-"+code+"-db", "projects/p-spn/instances/shared-spanner/databases/imp-prd-gbl-"+code+"-db")
	}
	// tstFull is tst's own instance at its ten: six applications' tst databases and four
	// pull-request stacks'.
	tstFull := []string{}
	for _, code := range []string{"lamp", "signin", "outl", "sites", "tenant", "quill"} {
		tstFull = append(tstFull, "projects/p-tst/instances/imp-tst-gbl-spanner/databases/imp-tst-gbl-"+code+"-db")
	}
	for _, pr := range []string{"quill-pr3-db", "quill-pr5-db-2", "sites-pr8-db", "lamp-pr9-db"} {
		tstFull = append(tstFull, "projects/p-tst/instances/imp-tst-gbl-spanner/databases/"+pr)
	}
	tests := []struct {
		name string
		// target is what is counted for; stg's environment database when nil.
		target *capacityTarget
		// state is 2-env's state in stg, none when empty.
		state string
		// spanner is nil for a run without a Spanner client.
		spanner     *fakeSpanner
		wantOut     string
		wantRefusal string
	}{
		{
			name:        "a pull request's database on the integration environment's full instance is refused, with the pull requests up named first; the job runs as the identity and reads directly",
			target:      &capacityTarget{identity: "harbor-apply@p-tst.iam.gserviceaccount.com", bucket: bucket, env: "tst", database: "harbor-pr7-db"},
			state:       `{"outputs": {"spanner_instance": {"value": {"project": "p-tst", "name": "imp-tst-gbl-spanner", "config": "regional-us-central1"}}}}`,
			spanner:     &fakeSpanner{databases: tstFull, units: 100},
			wantRefusal: "Build REJECTED: harbor-pr7-db does not exist, and projects/p-tst/instances/imp-tst-gbl-spanner holds its maximum: 10 databases, 4 of them pull-request stacks', what its 100 processing units allow (ten for every hundred below a node); close or merge a pull request whose stack is up (the sweep takes a closed one's down, /gcbrun down takes it down now), raise spanner_instances.tst.processing_units in 2-env/terraform.tfvars (100 or 200; beyond 200 is a change to the variable's validation), or bedrock destroy an application the organization no longer needs, which frees its databases",
		},
		{
			name:    "a pull request's database already on the instance needs no room",
			target:  &capacityTarget{identity: "harbor-apply@p-tst.iam.gserviceaccount.com", bucket: bucket, env: "tst", database: "quill-pr5-db"},
			state:   `{"outputs": {"spanner_instance": {"value": {"project": "p-tst", "name": "imp-tst-gbl-spanner"}}}}`,
			spanner: &fakeSpanner{databases: tstFull, units: 100},
			wantOut: "Spanner capacity guard passed: projects/p-tst/instances/imp-tst-gbl-spanner holds quill-pr5-db-2.",
		},
		{
			name:    "without a Spanner client nothing is counted",
			state:   shared,
			wantOut: "",
		},
		{
			name:    "without 2-env's state nothing is counted, said in the log",
			spanner: &fakeSpanner{databases: full, units: 100},
			wantOut: "Spanner capacity guard: 2-env has no state in stg (gs://state-b/2-env/stg); nothing is counted.",
		},
		{
			name:    "a state without the output nothing is counted either",
			state:   `{"outputs": {"records_bucket": {"value": "r"}}}`,
			spanner: &fakeSpanner{databases: full, units: 100},
			wantOut: "Spanner capacity guard: 2-env's state in stg names no spanner_instance output (a 2-env applied by an older bedrock); nothing is counted.",
		},
		{
			name:    "the stack's database on the instance needs no room",
			state:   shared,
			spanner: &fakeSpanner{databases: append(full[:9:9], "projects/p-spn/instances/shared-spanner/databases/imp-stg-gbl-harbor-db"), units: 100},
			wantOut: "Spanner capacity guard passed: projects/p-spn/instances/shared-spanner holds imp-stg-gbl-harbor-db.",
		},
		{
			name:    "a later generation of the stack's database counts as it",
			state:   shared,
			spanner: &fakeSpanner{databases: append(full[:9:9], "projects/p-spn/instances/shared-spanner/databases/imp-stg-gbl-harbor-db-2"), units: 100},
			wantOut: "Spanner capacity guard passed: projects/p-spn/instances/shared-spanner holds imp-stg-gbl-harbor-db-2.",
		},
		{
			name:    "room on the instance passes, with the count",
			state:   shared,
			spanner: &fakeSpanner{databases: full[:9], units: 100},
			wantOut: "Spanner capacity guard passed: projects/p-spn/instances/shared-spanner holds 9 of the 10 databases its 100 processing units allow, with room for imp-stg-gbl-harbor-db.",
		},
		{
			name:    "a larger instance holds more",
			state:   shared,
			spanner: &fakeSpanner{databases: full, units: 200},
			wantOut: "holds 10 of the 20 databases its 200 processing units allow, with room for imp-stg-gbl-harbor-db.",
		},
		{
			name:    "an identity the instance refuses counts nothing, said in the log",
			state:   shared,
			spanner: &fakeSpanner{databases: full, units: 100, denied: true},
			wantOut: "Spanner capacity guard: harbor-plan@p-stg.iam.gserviceaccount.com may not list the databases of projects/p-spn/instances/shared-spanner yet (Spanner answered HTTP 403 to GET /v1/projects/p-spn/instances/shared-spanner/databases: Caller is missing IAM permission spanner.databases.list on resource projects/p-spn/instances/shared-spanner); nothing is counted.",
		},
		{
			name:        "a full shared instance refuses the stack, naming 2-spn's setting",
			state:       shared,
			spanner:     &fakeSpanner{databases: full, units: 100},
			wantRefusal: "Build REJECTED: imp-stg-gbl-harbor-db does not exist, and projects/p-spn/instances/shared-spanner holds its maximum: 10 databases, what its 100 processing units allow (ten for every hundred below a node); raise processing_units in 2-spn/terraform.tfvars (100 or 200; beyond 200 is a change to the variable's validation), or bedrock destroy an application the organization no longer needs, which frees its databases",
		},
		{
			name:        "a full instance of the environment's own names 2-env's setting",
			state:       own,
			spanner:     &fakeSpanner{databases: full, units: 100},
			wantRefusal: "projects/p-stg/instances/imp-stg-gbl-spanner holds its maximum: 10 databases, what its 100 processing units allow (ten for every hundred below a node); raise spanner_instances.stg.processing_units in 2-env/terraform.tfvars",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The environment stacks' guard runs as the deploy identity impersonating the
			// apply identity (or the apply identity a plan identity); a pull request's runs
			// as the apply identity itself and reads directly, so the openers it is given are
			// the direct ones alone and an impersonation would find none.
			target := capacityTarget{identity: identity, impersonate: true, bucket: bucket, env: "stg", database: "imp-stg-gbl-harbor-db"}
			if tt.target != nil {
				target = *tt.target
			}
			store := &memoryStore{objects: map[string]string{}}
			if tt.state != "" {
				store.objects["gs://"+bucket+"/2-env/"+target.env+"/default.tfstate"] = tt.state
			}
			clients := &Clients{StorageAs: store.openAs}
			if !target.impersonate {
				clients = &Clients{Storage: store.open}
			}
			if tt.spanner != nil && target.impersonate {
				clients.SpannerAs = tt.spanner.open
			}
			if tt.spanner != nil && !target.impersonate {
				clients.Spanner = tt.spanner.openDirect
			}
			var out strings.Builder
			refusal, err := guardSpannerCapacity(t.Context(), clients, target, &out)
			if err != nil {
				t.Fatalf("guardSpannerCapacity() error = %v", err)
			}
			if tt.wantRefusal != "" {
				if !strings.Contains(refusal, tt.wantRefusal) {
					t.Errorf("refusal = %q, want it to contain %q", refusal, tt.wantRefusal)
				}
				if !strings.Contains(out.String(), tt.wantRefusal) {
					t.Errorf("output lacks the refusal:\n%s", out.String())
				}

				return
			}
			if refusal != "" {
				t.Errorf("refusal = %q, want none", refusal)
			}
			if tt.wantOut == "" && out.Len() != 0 {
				t.Errorf("output = %q, want none", out.String())
			}
			if tt.wantOut != "" && !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("output = %q, want it to contain %q", out.String(), tt.wantOut)
			}
		})
	}
}
