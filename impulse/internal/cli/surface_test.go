package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/cccteam/ccc/impulse/internal/check"
)

var updateSurface = flag.Bool("update", false, "rewrite testdata/surface.golden from the command tree")

// TestSurface freezes the vocabulary an adopter meets: every command path with its
// arguments, each command's own flags with their type and default, and the check names,
// written to testdata/surface.golden. A change to any of them is a deliberate golden update
// in the same commit (go test ./internal/cli -update), reviewed as a rename, never an
// accident.
func TestSurface(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	writeSurface(&b, newRoot())
	b.WriteString("\nchecks:\n")
	for _, c := range check.All() {
		fmt.Fprintf(&b, "  %s\n", c.Name())
	}
	got := b.String()

	golden := filepath.Join("testdata", "surface.golden")
	if *updateSurface {
		if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%s: %v (go test ./internal/cli -update writes it)", golden, err)
	}
	if diff := cmp.Diff(string(want), got); diff != "" {
		t.Errorf("the command surface changed (-golden +got); when the change is meant, update the golden in the same commit with go test ./internal/cli -update:\n%s", diff)
	}
}

// writeSurface writes a command and, after it, its subcommands: the command path with
// its arguments as Use spells them, then each flag the command itself declares with its
// type, its default when it has one, and whether it is required.
func writeSurface(b *strings.Builder, cmd *cobra.Command) {
	fmt.Fprintf(b, "%s%s\n", cmd.CommandPath(), useArgs(cmd))
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		line := fmt.Sprintf("  --%s %s", f.Name, f.Value.Type())
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "[]" {
			line += fmt.Sprintf(" (default %s)", f.DefValue)
		}
		if _, required := f.Annotations[cobra.BashCompOneRequiredFlag]; required {
			line += " (required)"
		}
		fmt.Fprintf(b, "%s\n", line)
	})
	for _, sub := range cmd.Commands() {
		writeSurface(b, sub)
	}
}

// useArgs is the argument part of a command's Use line, with its leading space, or empty.
func useArgs(cmd *cobra.Command) string {
	_, args, ok := strings.Cut(cmd.Use, " ")
	if !ok {
		return ""
	}

	return " " + args
}
