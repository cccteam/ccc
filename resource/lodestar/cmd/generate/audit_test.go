package generate

import (
	"slices"
	"testing"

	"github.com/cccteam/ccc/resource/generation"
)

// TestAuditFindings pins the audit pass's findings over this application as typed
// values: exactly one, the cascade finding on RefitTask, whose table is interleaved in
// Refits ON DELETE CASCADE and stores the task photo (migration 000040), so a refit
// deleted by patch takes its tasks and their photos past the release. The set is
// exact: a new @file table on a cascade shows up here. Runs the generator in-process
// like TestSchemaWarnings, so it regenerates the tree like go generate does; the
// findings are the demonstration's pin, not a pattern for an application, whose
// generate program prints them under -audit alone. Requires the Spanner emulator
// (podman/docker), like the rest of this module's tests.
//
// Demonstrates: audit.cascade-release.
func TestAuditFindings(t *testing.T) {
	if testing.Short() {
		t.Skip("generation requires the Spanner emulator")
	}

	generator, err := NewGenerator(t.Context())
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}
	defer generator.Close()

	if got := generator.Audit(); got != nil {
		t.Errorf("Audit() before Generate = %v, want nil", got)
	}

	if err := generator.Generate(); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	want := []generation.Finding{
		generation.CascadeReleaseFinding{Resource: "RefitTask", Table: "RefitTasks", Parent: "Refits"},
	}
	if got := generator.Audit(); !slices.Equal(want, got) {
		t.Errorf("Audit() = %#v, want %#v", got, want)
	}
}
