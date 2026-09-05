package resource

import (
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
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

			row, err := scanEnvelopeRow[enforcementResource](spannerRow, &Statement{maskedNamesColumn: maskedNamesColumnName})
			if err != nil {
				t.Fatalf("scanEnvelopeRow() error = %v", err)
			}

			if row.Data != tt.wantData {
				t.Errorf("scanEnvelopeRow() data = %+v, want %+v", row.Data, tt.wantData)
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

// TestScanEnvelopeRow_capabilities pins the capability half of the envelope
// scan: the reserved checks column reads through the plan into per-row
// answers (NULL booleans read as false — a condition permits only on TRUE),
// and a plan with no SQL groups assembles from grants alone with no reserved
// column in the statement.
func TestScanEnvelopeRow_capabilities(t *testing.T) {
	t.Parallel()

	id := mustUUIDFromString("8a6570c8-1e51-4870-9def-3f68d0447d09")

	plan := &capabilityPlan{
		checksColumn: capabilityChecksColumnName,
		groups:       2,
		perms: []plannedCapability{
			{
				perm: accesstypes.Update,
				fields: []capabilityField{
					{jsonName: "public", group: -1},
					{jsonName: "tagged", group: 0},
				},
				group: -1,
			},
			{perm: accesstypes.Delete, isDelete: true, group: 1},
		},
	}

	tests := []struct {
		name   string
		checks any
		useCol bool
		plan   *capabilityPlan
		want   map[accesstypes.Permission]any
	}{
		{
			name:   "true group admits its field and the delete",
			checks: []bool{true, true},
			useCol: true,
			plan:   plan,
			want:   map[accesstypes.Permission]any{accesstypes.Update: []string{"public", "tagged"}, accesstypes.Delete: true},
		},
		{
			name:   "false and NULL groups both read as not permitted",
			checks: []spanner.NullBool{{Bool: false, Valid: true}, {}},
			useCol: true,
			plan:   plan,
			want:   map[accesstypes.Permission]any{accesstypes.Update: []string{"public"}, accesstypes.Delete: false},
		},
		{
			name:   "a data-free plan assembles from grants alone",
			useCol: false,
			plan: &capabilityPlan{perms: []plannedCapability{
				{perm: accesstypes.Update, fields: []capabilityField{{jsonName: "public", group: -1}}, group: -1},
				{perm: accesstypes.Delete, isDelete: true, allowed: true, group: -1},
			}},
			want: map[accesstypes.Permission]any{accesstypes.Update: []string{"public"}, accesstypes.Delete: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			columns := []string{"Id", "Public", "Tagged"}
			values := []any{id.String(), "visible", "shown"}
			if tt.useCol {
				columns = append(columns, capabilityChecksColumnName)
				values = append(values, tt.checks)
			}

			spannerRow, err := spanner.NewRow(columns, values)
			if err != nil {
				t.Fatalf("spanner.NewRow() error = %v", err)
			}

			row, err := scanEnvelopeRow[enforcementResource](spannerRow, &Statement{capabilityPlan: tt.plan})
			if err != nil {
				t.Fatalf("scanEnvelopeRow() error = %v", err)
			}

			if diff := cmp.Diff(tt.want, row.Capabilities()); diff != "" {
				t.Errorf("Row.Capabilities() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
