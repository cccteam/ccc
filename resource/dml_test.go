package resource

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestConfig_SetChangeTrackingTable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		initialCfg  Config
		newCTTable  string
		expectedCfg Config
	}{
		{
			name:        "Set new CT table name",
			initialCfg:  Config{ChangeTrackingTable: "old_ct", TrackChanges: true},
			newCTTable:  "new_ct_table",
			expectedCfg: Config{ChangeTrackingTable: "new_ct_table", TrackChanges: true},
		},
		{
			name:        "Set empty CT table name",
			initialCfg:  Config{ChangeTrackingTable: "some_ct", TrackChanges: false},
			newCTTable:  "",
			expectedCfg: Config{ChangeTrackingTable: "", TrackChanges: false},
		},
		{
			name:        "Set to same CT table name",
			initialCfg:  Config{ChangeTrackingTable: "ct", TrackChanges: true},
			newCTTable:  "ct",
			expectedCfg: Config{ChangeTrackingTable: "ct", TrackChanges: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			originalCfgPtr := &tt.initialCfg
			updatedCfg := tt.initialCfg.SetChangeTrackingTable(tt.newCTTable)
			updatedCfgPtr := &updatedCfg

			if !cmp.Equal(tt.expectedCfg, updatedCfg) {
				t.Errorf("SetChangeTrackingTable() mismatch (-want +got):\n%s", cmp.Diff(tt.expectedCfg, updatedCfg))
			}
			if originalCfgPtr == updatedCfgPtr {
				t.Errorf("SetChangeTrackingTable() did not return a new Config instance")
			}
			// Verify other fields remain unchanged
			if tt.initialCfg.TrackChanges != updatedCfg.TrackChanges {
				t.Errorf("SetChangeTrackingTable() changed TrackChanges: initial=%t, updated=%t", tt.initialCfg.TrackChanges, updatedCfg.TrackChanges)
			}
		})
	}
}

func TestConfig_SetTrackChanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		initialCfg      Config
		newTrackChanges bool
		expectedCfg     Config
	}{
		{
			name:            "Set TrackChanges to true",
			initialCfg:      Config{ChangeTrackingTable: "ct", TrackChanges: false},
			newTrackChanges: true,
			expectedCfg:     Config{ChangeTrackingTable: "ct", TrackChanges: true},
		},
		{
			name:            "Set TrackChanges to false",
			initialCfg:      Config{ChangeTrackingTable: "ct_table", TrackChanges: true},
			newTrackChanges: false,
			expectedCfg:     Config{ChangeTrackingTable: "ct_table", TrackChanges: false},
		},
		{
			name:            "Set to same TrackChanges value (true)",
			initialCfg:      Config{ChangeTrackingTable: "ct", TrackChanges: true},
			newTrackChanges: true,
			expectedCfg:     Config{ChangeTrackingTable: "ct", TrackChanges: true},
		},
		{
			name:            "Set to same TrackChanges value (false)",
			initialCfg:      Config{ChangeTrackingTable: "ct", TrackChanges: false},
			newTrackChanges: false,
			expectedCfg:     Config{ChangeTrackingTable: "ct", TrackChanges: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			originalCfgPtr := &tt.initialCfg
			updatedCfg := tt.initialCfg.SetTrackChanges(tt.newTrackChanges)
			updatedCfgPtr := &updatedCfg

			if !cmp.Equal(tt.expectedCfg, updatedCfg) {
				t.Errorf("SetTrackChanges() mismatch (-want +got):\n%s", cmp.Diff(tt.expectedCfg, updatedCfg))
			}
			if originalCfgPtr == updatedCfgPtr {
				t.Errorf("SetTrackChanges() did not return a new Config instance")
			}
			// Verify other fields remain unchanged
			if tt.initialCfg.ChangeTrackingTable != updatedCfg.ChangeTrackingTable {
				t.Errorf("SetTrackChanges() changed ChangeTrackingTable: initial=%s, updated=%s", tt.initialCfg.ChangeTrackingTable, updatedCfg.ChangeTrackingTable)
			}
		})
	}
}
