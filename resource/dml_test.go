package resource

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestConfig_SetDBType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		initialCfg  Config
		newDBType   DBType
		expectedCfg Config
	}{
		{
			name:        "Set Spanner DB type from Postgres",
			initialCfg:  Config{DBType: PostgresDBType, ChangeTrackingTable: "ct_table", TrackChanges: true},
			newDBType:   SpannerDBType,
			expectedCfg: Config{DBType: SpannerDBType, ChangeTrackingTable: "ct_table", TrackChanges: true},
		},
		{
			name:        "Set Postgres DB type from Spanner",
			initialCfg:  Config{DBType: SpannerDBType, ChangeTrackingTable: "another_ct", TrackChanges: false},
			newDBType:   PostgresDBType,
			expectedCfg: Config{DBType: PostgresDBType, ChangeTrackingTable: "another_ct", TrackChanges: false},
		},
		{
			name:        "Set to same DB type (Spanner)",
			initialCfg:  Config{DBType: SpannerDBType, ChangeTrackingTable: "ct", TrackChanges: true},
			newDBType:   SpannerDBType,
			expectedCfg: Config{DBType: SpannerDBType, ChangeTrackingTable: "ct", TrackChanges: true},
		},
		{
			name:        "Set from undefined DB type",
			initialCfg:  Config{ChangeTrackingTable: "ct", TrackChanges: true}, // DBType is zero value (e.g. UnknownDBType or first enum)
			newDBType:   PostgresDBType,
			expectedCfg: Config{DBType: PostgresDBType, ChangeTrackingTable: "ct", TrackChanges: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			originalCfgPtr := &tt.initialCfg
			updatedCfg := tt.initialCfg.SetDBType(tt.newDBType)
			updatedCfgPtr := &updatedCfg

			if !cmp.Equal(tt.expectedCfg, updatedCfg) {
				t.Errorf("SetDBType() mismatch (-want +got):\n%s", cmp.Diff(tt.expectedCfg, updatedCfg))
			}
			if originalCfgPtr == updatedCfgPtr {
				t.Errorf("SetDBType() did not return a new Config instance, original and updated point to the same memory location")
			}
			// Verify other fields remain unchanged
			if tt.initialCfg.ChangeTrackingTable != updatedCfg.ChangeTrackingTable {
				t.Errorf("SetDBType() changed ChangeTrackingTable: initial=%s, updated=%s", tt.initialCfg.ChangeTrackingTable, updatedCfg.ChangeTrackingTable)
			}
			if tt.initialCfg.TrackChanges != updatedCfg.TrackChanges {
				t.Errorf("SetDBType() changed TrackChanges: initial=%t, updated=%t", tt.initialCfg.TrackChanges, updatedCfg.TrackChanges)
			}
		})
	}
}

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
			initialCfg:  Config{DBType: PostgresDBType, ChangeTrackingTable: "old_ct", TrackChanges: true},
			newCTTable:  "new_ct_table",
			expectedCfg: Config{DBType: PostgresDBType, ChangeTrackingTable: "new_ct_table", TrackChanges: true},
		},
		{
			name:        "Set empty CT table name",
			initialCfg:  Config{DBType: SpannerDBType, ChangeTrackingTable: "some_ct", TrackChanges: false},
			newCTTable:  "",
			expectedCfg: Config{DBType: SpannerDBType, ChangeTrackingTable: "", TrackChanges: false},
		},
		{
			name:        "Set to same CT table name",
			initialCfg:  Config{DBType: PostgresDBType, ChangeTrackingTable: "ct", TrackChanges: true},
			newCTTable:  "ct",
			expectedCfg: Config{DBType: PostgresDBType, ChangeTrackingTable: "ct", TrackChanges: true},
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
			if tt.initialCfg.DBType != updatedCfg.DBType {
				t.Errorf("SetChangeTrackingTable() changed DBType: initial=%v, updated=%v", tt.initialCfg.DBType, updatedCfg.DBType)
			}
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
			initialCfg:      Config{DBType: PostgresDBType, ChangeTrackingTable: "ct", TrackChanges: false},
			newTrackChanges: true,
			expectedCfg:     Config{DBType: PostgresDBType, ChangeTrackingTable: "ct", TrackChanges: true},
		},
		{
			name:            "Set TrackChanges to false",
			initialCfg:      Config{DBType: SpannerDBType, ChangeTrackingTable: "ct_table", TrackChanges: true},
			newTrackChanges: false,
			expectedCfg:     Config{DBType: SpannerDBType, ChangeTrackingTable: "ct_table", TrackChanges: false},
		},
		{
			name:            "Set to same TrackChanges value (true)",
			initialCfg:      Config{DBType: PostgresDBType, ChangeTrackingTable: "ct", TrackChanges: true},
			newTrackChanges: true,
			expectedCfg:     Config{DBType: PostgresDBType, ChangeTrackingTable: "ct", TrackChanges: true},
		},
		{
			name:            "Set to same TrackChanges value (false)",
			initialCfg:      Config{DBType: PostgresDBType, ChangeTrackingTable: "ct", TrackChanges: false},
			newTrackChanges: false,
			expectedCfg:     Config{DBType: PostgresDBType, ChangeTrackingTable: "ct", TrackChanges: false},
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
			if tt.initialCfg.DBType != updatedCfg.DBType {
				t.Errorf("SetTrackChanges() changed DBType: initial=%v, updated=%v", tt.initialCfg.DBType, updatedCfg.DBType)
			}
			if tt.initialCfg.ChangeTrackingTable != updatedCfg.ChangeTrackingTable {
				t.Errorf("SetTrackChanges() changed ChangeTrackingTable: initial=%s, updated=%s", tt.initialCfg.ChangeTrackingTable, updatedCfg.ChangeTrackingTable)
			}
		})
	}
}
