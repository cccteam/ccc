// Demonstrates: bootstrap.reset.
package integration

// This suite pins the bootstrap's data-only reset: a seeded database, with both auths'
// roles provisioned and a role assigned, emptied of every row the schema migrations do
// not own, table by table, with the schema, its own rows (the enumeration tables), and
// its migration bookkeeping intact. The role assignment matters: it is an interleaved
// child under ON DELETE NO ACTION, which Spanner refuses to delete out from under.

import (
	"context"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/deploy"
	initiator "github.com/cccteam/db-initiator"
)

func TestResetDevelopmentData(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	crewAuth, err := crew.New(ctx, db.Client, crew.Settings{CookieKey: testCookieKey, SessionTimeout: time.Minute})
	if err != nil {
		t.Fatalf("crew.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := crewAuth.Close(); err != nil {
			t.Errorf("crew.Auth.Close() error = %v", err)
		}
	})
	manager := crewAuth.Access().UserManager()
	if err := deploy.MigrateRoles(ctx, manager, crewRolesPath, anvil, bastion, cinder); err != nil {
		t.Fatalf("deploy.MigrateRoles() error = %v", err)
	}
	if err := manager.AddUserRoles(ctx, accesstypes.DomainScope(anvil), "someone", "Archivist"); err != nil {
		t.Fatalf("access.UserManager.AddUserRoles() error = %v", err)
	}

	for table, column := range map[string]string{"Missions": "Id", "CrewRoles": "Role", "CrewUserRoles": "User"} {
		if n := countRows(ctx, t, db, table, column); n == 0 {
			t.Fatalf("%s is empty before the reset; the reset would prove nothing", table)
		}
	}

	if err := deploy.ResetDevelopmentData(ctx, db.Client, migrationsSource); err != nil {
		t.Fatalf("deploy.ResetDevelopmentData() error = %v", err)
	}

	tests := []struct {
		name      string
		table     string
		column    string
		wantEmpty bool
	}{
		{name: "the demo world is emptied", table: "Missions", column: "Id", wantEmpty: true},
		{name: "the tenants are emptied", table: "Sectors", column: "Id", wantEmpty: true},
		{name: "an interleaved child on cascade is emptied with its parent", table: "RefitTasks", column: "Id", wantEmpty: true},
		{name: "a foreign-key parent is emptied after its children", table: "Clients", column: "Id", wantEmpty: true},
		{name: "a role assignment, the interleaved child on no action, is emptied first", table: "CrewUserRoles", column: "User", wantEmpty: true},
		{name: "then the roles it hung under", table: "CrewRoles", column: "Role", wantEmpty: true},
		{name: "an enumeration the schema populates stays", table: "MissionStatuses", column: "Id", wantEmpty: false},
		{name: "the schema migration bookkeeping stays", table: "SchemaMigrations", column: "Version", wantEmpty: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			n := countRows(ctx, t, db, tt.table, tt.column)
			if (n == 0) != tt.wantEmpty {
				t.Errorf("%s holds %d rows, want empty = %t", tt.table, n, tt.wantEmpty)
			}
		})
	}
}

// countRows counts a table's rows by reading one column over every key.
func countRows(ctx context.Context, t *testing.T, db *initiator.SpannerDB, table, column string) int {
	t.Helper()

	n := 0
	if err := db.Single().Read(ctx, table, spanner.AllKeys(), []string{column}).Do(func(*spanner.Row) error {
		n++

		return nil
	}); err != nil {
		t.Fatalf("Read(%s): %v", table, err)
	}

	return n
}
