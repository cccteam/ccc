package resource

import (
	"testing"

	"cloud.google.com/go/spanner"
)

// TestScanMaskedRow pins the masking statement's scan contract: the resource
// columns scan into the envelope's data with the reserved column skipped
// (lenient scan), and the reserved column becomes the envelope's mask list —
// Masked answers by JSON name, and a masked cell holds its zero-value filler.
func TestScanMaskedRow(t *testing.T) {
	t.Parallel()

	id := mustUUIDFromString("8a6570c8-1e51-4870-9def-3f68d0447d09")

	tests := []struct {
		name       string
		columns    []string
		values     []any
		wantData   enforcementResource
		wantMasked []string
	}{
		{
			name:       "masked cell carries the filler and its name rides the mask list",
			columns:    []string{"Id", "Public", "Tagged", maskedNamesColumnName},
			values:     []any{id.String(), "visible", "", []string{"tagged"}},
			wantData:   enforcementResource{ID: id, Public: "visible", Tagged: ""},
			wantMasked: []string{"tagged"},
		},
		{
			name:       "empty mask list masks nothing",
			columns:    []string{"Id", "Public", "Tagged", maskedNamesColumnName},
			values:     []any{id.String(), "visible", "shown", []string{}},
			wantData:   enforcementResource{ID: id, Public: "visible", Tagged: "shown"},
			wantMasked: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spannerRow, err := spanner.NewRow(tt.columns, tt.values)
			if err != nil {
				t.Fatalf("spanner.NewRow() error = %v", err)
			}

			row, err := scanMaskedRow[enforcementResource](spannerRow, maskedNamesColumnName)
			if err != nil {
				t.Fatalf("scanMaskedRow() error = %v", err)
			}

			if row.Data != tt.wantData {
				t.Errorf("scanMaskedRow() data = %+v, want %+v", row.Data, tt.wantData)
			}
			for _, name := range tt.wantMasked {
				if !row.Masked(name) {
					t.Errorf("Row.Masked(%q) = false, want true", name)
				}
			}
			if len(tt.wantMasked) == 0 && row.Masked("tagged") {
				t.Error(`Row.Masked("tagged") = true with an empty mask list`)
			}
		})
	}
}
