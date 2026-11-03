package generation

import (
	"go/format"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// The generated record next to the resources: a package's entries as the types over the
// tables with their origin said, and the application's assembly of the packages' values.

func Test_liveDerivationsPackageTemplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		data         *liveDerivationsPackageData
		wantContains []string
	}{
		{
			name: "a root and its tables read off the query, a rootless global resource declared, the resources package qualified",
			data: &liveDerivationsPackageData{Source: "pkg/resources", Package: "virtualresources", ResourcesQualifier: "resources.", Resources: []derivedResourceData{
				{Type: "MissionBoard", Comment: "MissionBoards: the tables its query reads.", Root: "Mission", Tables: []sourceTableData{{Type: "Client", Global: true}, {Type: "Squadron"}}},
				{Type: "Roster", Comment: "Rosters: the tables declared with @changedBy.", Global: true, Tables: []sourceTableData{{Type: "Pilot"}}},
			}},
			wantContains: []string{
				"package virtualresources",
				"var LiveDerivations = &live.Derivations{",
				"// MissionBoards: the tables its query reads.\n",
				"{Resource: MissionBoard{}.Resource(), Root: resources.Mission{}.Resource(), Tables: []live.SourceTable{",
				"{Name: resources.Client{}.Resource(), Global: true},",
				"{Name: resources.Squadron{}.Resource()},",
				"// Rosters: the tables declared with @changedBy.\n",
				"{Resource: Roster{}.Resource(), Global: true, Tables: []live.SourceTable{",
				"{Name: resources.Pilot{}.Resource()},",
			},
		},
		{
			name: "in the resources package itself the types are bare",
			data: &liveDerivationsPackageData{Source: "pkg/resources", Package: "resources", Resources: []derivedResourceData{
				{Type: "MissionBoard", Comment: "MissionBoards: the tables its query reads.", Root: "Mission", Tables: []sourceTableData{{Type: "Squadron"}}},
			}},
			wantContains: []string{
				"{Resource: MissionBoard{}.Resource(), Root: Mission{}.Resource(), Tables: []live.SourceTable{",
				"{Name: Squadron{}.Resource()},",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out := renderFormatted(t, "liveDerivationsPackageTemplate", liveDerivationsPackageTemplate, tt.data)
			for _, want := range tt.wantContains {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

func Test_liveDerivationsTemplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		data            *liveDerivationsAssemblyData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:            "no package publishes: the empty value, still handed to every publish",
			data:            &liveDerivationsAssemblyData{Source: "pkg/resources", Package: "app"},
			wantContains:    []string{"package app", "var liveDerivations = live.Merge()"},
			wantNotContains: []string{"LiveDerivations,"},
		},
		{
			name: "two packages publish, one of them the handler package itself",
			data: &liveDerivationsAssemblyData{Source: "pkg/resources", Package: "app", Values: []string{"LiveDerivations", "computedresources.LiveDerivations", "virtualresources.LiveDerivations"}},
			wantContains: []string{
				"var liveDerivations = live.Merge(\n\tLiveDerivations,\n\tcomputedresources.LiveDerivations,\n\tvirtualresources.LiveDerivations,\n)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out := renderFormatted(t, "liveDerivationsTemplate", liveDerivationsTemplate, tt.data)
			for _, want := range tt.wantContains {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(out, notWant) {
					t.Errorf("output carries %q:\n%s", notWant, out)
				}
			}
		})
	}
}

// renderFormatted renders a Go file template and formats it as the writer does, so the
// expectations read the formatted text.
func renderFormatted(t *testing.T, templateName, fileTemplate string, data any) string {
	t.Helper()

	c := &client{}
	rendered, err := c.generateTemplateOutput(templateName, fileTemplate, data)
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}
	out, err := format.Source(rendered)
	if err != nil {
		t.Fatalf("format.Source() error = %v:\n%s", err, rendered)
	}

	return string(out)
}

// Test_liveDerivationsByPackage pins the grouping: a published virtual resource in the
// virtual package and a declared computed one in the computed package, each table as the
// type over it, a table no resource backs named in the comment alone.
func Test_liveDerivationsByPackage(t *testing.T) {
	t.Parallel()

	loaded, parsed := loadFixturePackage(t, "changedbyfixture")
	structs := fixtureStructs(parsed)
	c := changedByClient(loaded)
	c.resources = []*resourceInfo{fixtureTable(t, structs, "Berth", "DockID", "Slot"), fixtureTable(t, structs, "Dock", "ID")}
	roster := fixtureVirtualPublished(t, structs, "Roster")
	tally := fixtureComputedPublished(t, structs, "Tally")
	if err := c.resolvePublished([]*resourceInfo{roster}, []*computedResource{tally}); err != nil {
		t.Fatalf("resolvePublished() error = %v", err)
	}
	c.resources = append(c.resources, roster)
	c.computedResources = []*computedResource{tally}
	c.virtual, c.computed = "pkg/virtualresources", "pkg/computedresources"
	r := &resourceGenerator{client: c}

	got, err := r.liveDerivationsByPackage()
	if err != nil {
		t.Fatalf("liveDerivationsByPackage() error = %v", err)
	}
	want := []derivationsPackage{
		{dir: "pkg/computedresources", Resources: []derivedResourceData{
			{Type: "Tally", Comment: "Tallies: the tables declared with @changedBy. The set also names Visits, which no resource backs: no write to it is published, so it is left out.", Global: true, Tables: []sourceTableData{{Type: "Dock", Global: true}}},
		}},
		{dir: "pkg/virtualresources", Resources: []derivedResourceData{
			{Type: "Roster", Comment: "Rosters: the tables its query reads.", Global: true, Root: "Berth", Tables: []sourceTableData{{Type: "Dock", Global: true}}},
		}},
	}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(derivationsPackage{})); diff != "" {
		t.Errorf("liveDerivationsByPackage() mismatch (-want +got):\n%s", diff)
	}
}

func Test_derivationComment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resource string
		declared bool
		unbacked []string
		want     string
	}{
		{name: "read off the query", resource: "MissionBoards", want: "MissionBoards: the tables its query reads."},
		{name: "declared", resource: "SectorHazardBoards", declared: true, want: "SectorHazardBoards: the tables declared with @changedBy."},
		{name: "one table no resource backs", resource: "Rosters", unbacked: []string{"AuditTrail"}, want: "Rosters: the tables its query reads. The set also names AuditTrail, which no resource backs: no write to it is published, so it is left out."},
		{name: "two tables no resource backs", resource: "Rosters", declared: true, unbacked: []string{"AuditTrail", "Ledger"}, want: "Rosters: the tables declared with @changedBy. The set also names AuditTrail, Ledger, which no resource backs: no write to them is published, so they are left out."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, derivationComment(tt.resource, tt.declared, tt.unbacked)); diff != "" {
				t.Errorf("derivationComment() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_packageQualifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		from, to packageDir
		want     string
	}{
		{name: "another package is qualified by its name", from: "app", to: "pkg/virtualresources", want: "virtualresources."},
		{name: "the same package, spelled differently, is bare", from: "pkg/resources/", to: "pkg/resources", want: ""},
		{name: "a package named by a file in it", from: "app", to: "pkg/resources/resources.go", want: "resources."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := packageQualifier(tt.from, tt.to); got != tt.want {
				t.Errorf("packageQualifier() = %q, want %q", got, tt.want)
			}
		})
	}
}
