package jobs

import (
	"strings"
	"testing"
)

// TestNone refuses every start, saying no job process is configured.
func TestNone(t *testing.T) {
	t.Parallel()

	if _, err := (None{}).Start(t.Context(), "cleanup-files"); err == nil || !strings.Contains(err.Error(), "no job process is configured") {
		t.Errorf("None.Start() error = %v, want the refusal", err)
	}
}

// TestFake records the starts in order and answers its error when set.
func TestFake(t *testing.T) {
	t.Parallel()

	f := NewFake()
	first, err := f.Start(t.Context(), "cleanup-files", "-dry-run")
	if err != nil || first != "projects/p/locations/l/jobs/j/executions/j-1" {
		t.Fatalf("Start() = %q, %v", first, err)
	}
	if _, err := f.Start(t.Context(), "cleanup-files"); err != nil {
		t.Fatal(err)
	}
	if got := f.Started(); len(got) != 2 || strings.Join(got[0], " ") != "cleanup-files -dry-run" || strings.Join(got[1], " ") != "cleanup-files" {
		t.Errorf("Started() = %v", got)
	}
}
