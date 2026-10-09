package resource

import (
	"context"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/httpio"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// featuresDatabase is postgresDatabase with the feature flag tables the library's own
// DDL declares, as an application copies them into a migration.
func featuresDatabase(t *testing.T, name string) (*initiator.PostgresDatabase, *PostgresClient) {
	t.Helper()

	db, client := postgresDatabase(t, name)
	for _, ddl := range FeatureFlagsDDL(PostgresDBType) {
		if _, err := db.Exec(t.Context(), ddl); err != nil {
			t.Fatalf("FeatureFlagsDDL() error = %v", err)
		}
	}

	return db, client
}

// postgresFeatureRows reads the flags table as the tests compare it.
func postgresFeatureRows(ctx context.Context, t *testing.T, db *initiator.PostgresDatabase) []FeatureFlag {
	t.Helper()

	txn := newPostgresReadOnlyTransaction(db.Pool)
	defer txn.Close()

	rows, err := readFeatureFlags(ctx, txn, PostgresDBType)
	if err != nil {
		t.Fatalf("readFeatureFlags() error = %v", err)
	}

	return rows
}

// TestPostgres_features pins the feature flags over Postgres as the Spanner test pins
// them: the deploy step inserts, rewords and deletes flags, a flip writes the flag and
// its audit row in one transaction, and a flag the table lacks is not found.
func TestPostgres_features(t *testing.T) {
	t.Parallel()

	db, client := featuresDatabase(t, "pg-features")
	ctx := t.Context()
	process := ProcessEvent("MigrateFeatures")

	if err := MigrateFeatures(ctx, client, []FeatureDeclaration{
		{Name: "debriefs", Description: "Mission debriefs.", Constant: "Debriefs"},
		{Name: "beacons", Description: "Beacons.", Constant: "Beacons"},
	}); err != nil {
		t.Fatalf("MigrateFeatures() error = %v", err)
	}
	want := []FeatureFlag{
		{Name: "beacons", Description: "Beacons.", UpdatedBy: process},
		{Name: "debriefs", Description: "Mission debriefs.", UpdatedBy: process},
	}
	if diff := cmp.Diff(want, postgresFeatureRows(ctx, t, db), cmpopts.IgnoreFields(FeatureFlag{}, "UpdatedAt")); diff != "" {
		t.Errorf("FeatureFlags mismatch (-want +got):\n%s", diff)
	}

	if err := SetFeatureEnabled(ctx, client, "debriefs", true, "harbormaster (test)"); err != nil {
		t.Fatalf("SetFeatureEnabled() error = %v", err)
	}
	if err := SetFeatureEnabled(ctx, client, "ghost", true, "harbormaster (test)"); !httpio.HasNotFound(err) {
		t.Errorf("SetFeatureEnabled() of an unknown flag error = %v, want NotFound", err)
	}
	var changes int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM "FeatureFlagChanges" WHERE "Name" = 'debriefs' AND "Enabled" AND "ChangedBy" = 'harbormaster (test)'`).Scan(&changes); err != nil {
		t.Fatalf("count error = %v", err)
	}
	if changes != 1 {
		t.Errorf("audit rows = %d, want 1", changes)
	}

	// The next deploy keeps the flipped value, rewords it, and drops the undeclared flag.
	if err := MigrateFeatures(ctx, client, []FeatureDeclaration{
		{Name: "debriefs", Description: "Mission debriefs, reworded.", Constant: "Debriefs"},
	}); err != nil {
		t.Fatalf("second MigrateFeatures() error = %v", err)
	}
	want = []FeatureFlag{{Name: "debriefs", Description: "Mission debriefs, reworded.", Enabled: true, UpdatedBy: "harbormaster (test)"}}
	if diff := cmp.Diff(want, postgresFeatureRows(ctx, t, db), cmpopts.IgnoreFields(FeatureFlag{}, "UpdatedAt")); diff != "" {
		t.Errorf("FeatureFlags after the second migration mismatch (-want +got):\n%s", diff)
	}

	// A loaded set answers from the table.
	set, err := LoadFeatures(ctx, client)
	if err != nil {
		t.Fatalf("LoadFeatures() error = %v", err)
	}
	if !set.Enabled("debriefs") || set.Enabled("beacons") {
		t.Errorf("FeatureSet.Enabled(debriefs, beacons) = %v, %v, want true, false", set.Enabled("debriefs"), set.Enabled("beacons"))
	}

	if err := MigrateFeatures(ctx, client, []FeatureDeclaration{{Name: "Bad", Constant: "Bad"}}); err == nil || !strings.Contains(err.Error(), "is not a feature name") {
		t.Errorf("MigrateFeatures() of a malformed declaration error = %v, want it refused", err)
	}
}

// TestPostgres_tenantRoster pins the roster's read over Postgres: the keys of the tenant
// table are the set, an empty table an empty set, and a read that fails fails the start.
func TestPostgres_tenantRoster(t *testing.T) {
	t.Parallel()

	db, client := postgresDatabase(t, "pg-roster")
	ctx := t.Context()
	for _, id := range []string{"beta", "alpha"} {
		if _, err := db.Exec(ctx, `INSERT INTO "Stations" ("Id", "Name") VALUES ($1, $1)`, id); err != nil {
			t.Fatalf("insert error = %v", err)
		}
	}

	r := NewTenantRoster(client, stationsTable, stationsKey)
	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	domains, err := r.Domains(ctx)
	if err != nil {
		t.Fatalf("Domains() error = %v", err)
	}
	if diff := cmp.Diff([]accesstypes.Domain{"alpha", "beta"}, domains); diff != "" {
		t.Errorf("Domains() mismatch (-want +got):\n%s", diff)
	}
	if !r.Has("alpha") || r.Has("gamma") {
		t.Errorf("Has(alpha, gamma) = %v, %v, want true, false", r.Has("alpha"), r.Has("gamma"))
	}

	if err := NewTenantRoster(client, "NoSuchTable", stationsKey).Start(ctx); err == nil {
		t.Error("Start() over a table that does not exist = nil, want an error")
	}
}
