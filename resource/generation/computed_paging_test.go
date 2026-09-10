package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/google/go-cmp/cmp"
)

// TestComputedQueryRules pins the generation-time rules for a computed resource's
// query surface: allow_filter on a comparable leaf is admitted, an index tag is
// refused (no database index exists), allow_filter on a list is refused, and a
// declared order may name only a leaf field.
func TestComputedQueryRules(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "pagingfixture"))

	tests := []struct {
		name       string
		structName string
		wantOrder  []resource.SortField
		wantErr    string
	}{
		{
			name:       "comparable filterable leaves and a leaf order are admitted",
			structName: "Board",
			wantOrder:  []resource.SortField{{Field: "Worst", Direction: resource.SortDescending}},
		},
		{
			name:       "an index tag is refused naming the field",
			structName: "IndexedBoard",
			wantErr:    "IndexedBoard.ID: the index tag names a database index",
		},
		{
			name:       "allow_filter on a list field is refused",
			structName: "ListFilterBoard",
			wantErr:    "ListFilterBoard.Tags: allow_filter on a list field",
		},
		{
			name:       "an order on a nested field is refused",
			structName: "NestedOrderBoard",
			wantErr:    "Readings is a nested field of NestedOrderBoard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			resources, err := c.structsToCompResources([]*parser.Struct{structs[tt.structName]})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("structsToCompResources() error = %v, want error containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("structsToCompResources() error = %v", err)
			}
			if len(resources) != 1 {
				t.Fatalf("structsToCompResources() returned %d resources, want 1", len(resources))
			}
			if diff := cmp.Diff(tt.wantOrder, resources[0].DeclaredOrder); diff != "" {
				t.Errorf("DeclaredOrder mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
