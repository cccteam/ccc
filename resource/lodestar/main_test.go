package main

import (
	"os"
	"testing"
)

// TestModuleRoot is the module root's own test: the process files the tool laid in are
// where the Procfile and the README say, and the demonstration index
// (demonstrations_test.go) runs beside it.
//
// Demonstrates: demonstration-index, ci-stub, impulse.bootstrapped, regen-idempotent.
func TestModuleRoot(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"Procfile", ".envrc.template", "walkthrough.sh", "README.md"} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}
