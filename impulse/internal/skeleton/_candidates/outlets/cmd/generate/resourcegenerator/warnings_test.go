package main

import (
	"reflect"
	"testing"

	"github.com/cccteam/ccc/resource/generation"
)

// TestSchemaWarnings pins the schema warnings the generator raises over this
// application: none. A warning is a performance finding about the schema (an index a
// listed tenant-scoped resource wants, a tenant resolved through a join path, an
// enumeration table too large to bake), printed by every generation and never a
// refusal; this test makes the accepted set explicit, so a new one fails CI until it
// is either fixed in the schema (add the index the line names, carry the tenant key on
// the row) or accepted by adding its typed value (generation.IndexWarning,
// generation.JoinPathWarning, generation.EnumerationSizeWarning) to want, which
// records the acceptance in code. Runs the generator in-process, so it regenerates the
// tree like go generate does. Requires the Spanner emulator (podman/docker), like the
// rest of this module's tests.
func TestSchemaWarnings(t *testing.T) {
	if testing.Short() {
		t.Skip("generation requires the Spanner emulator")
	}

	generator, err := newGenerator(t.Context())
	if err != nil {
		t.Fatalf("newGenerator() error = %v", err)
	}
	defer generator.Close()

	if err := generator.Generate(); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	var want []generation.Warning
	got := generator.Warnings()
	if len(want) == 0 && len(got) == 0 {
		return
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Warnings() = %d, want %d:", len(got), len(want))
		for _, w := range got {
			// The line, and the value to pin it with: the typed value carries fields the
			// line leaves out.
			t.Logf("got:  %s\n      %#v", w, w)
		}
		t.Logf("want: %v", want)
	}
}
