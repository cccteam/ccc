package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

func TestMigrationsReader(t *testing.T) {
	t.Parallel()

	const (
		first  = "CREATE TABLE DataChangeEvents (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n"
		role   = "-- the reader role\nCREATE ROLE migrations_reader;\nGRANT SELECT ON TABLE SchemaMigrations TO ROLE migrations_reader;\n"
		second = "CREATE TABLE Things (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n"
	)
	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "the first migration carries the role",
			files: map[string]string{
				"schema/migrations/000001_Init.up.sql":   first + role,
				"schema/migrations/000002_Things.up.sql": second,
			},
			wantStatus:  Pass,
			wantSummary: "the migrations_reader role is created and granted select on SchemaMigrations in schema/migrations",
		},
		{
			name: "a later migration carries it, in any case and spacing",
			files: map[string]string{
				"schema/migrations/000001_Init.up.sql":             first,
				"schema/migrations/000002_Things.up.sql":           second,
				"schema/migrations/000003_MigrationsReader.up.sql": "create role Migrations_Reader;\ngrant select\n  on table SchemaMigrations\n  to role Migrations_Reader;\n",
			},
			wantStatus:  Pass,
			wantSummary: "the migrations_reader role is created and granted select on SchemaMigrations in schema/migrations",
		},
		{
			name: "a sequence without the role fails naming the migration to add",
			files: map[string]string{
				"schema/migrations/000001_Init.up.sql":     first,
				"schema/migrations/000002_Things.up.sql":   second,
				"schema/migrations/000002_Things.down.sql": "DROP TABLE Things;\n",
			},
			wantStatus:  Fail,
			wantSummary: "1 schema sequence(s) without the migrations_reader role",
			wantDetails: []string{
				"schema/migrations: no migration runs `CREATE ROLE migrations_reader` and `GRANT SELECT ON TABLE SchemaMigrations TO ROLE migrations_reader`, so the pipeline's migration guard cannot read this database's migration version through the migrations_reader role; add schema/migrations/000003_MigrationsReader.up.sql with `CREATE ROLE migrations_reader` and `GRANT SELECT ON TABLE SchemaMigrations TO ROLE migrations_reader` (and a down file reversing it), which impulse upgrade writes",
			},
		},
		{
			name: "the role without its grant fails naming the grant",
			files: map[string]string{
				"schema/migrations/000001_Init.up.sql": first + "CREATE ROLE migrations_reader;\n",
			},
			wantStatus:  Fail,
			wantSummary: "1 schema sequence(s) without the migrations_reader role",
			wantDetails: []string{
				"schema/migrations: no migration runs `GRANT SELECT ON TABLE SchemaMigrations TO ROLE migrations_reader`, so the pipeline's migration guard cannot read this database's migration version through the migrations_reader role; add schema/migrations/000002_MigrationsReader.up.sql with `CREATE ROLE migrations_reader` and `GRANT SELECT ON TABLE SchemaMigrations TO ROLE migrations_reader` (and a down file reversing it), which impulse upgrade writes",
			},
		},
		{
			name:        "no migrations directory is skipped",
			files:       map[string]string{},
			wantStatus:  Skip,
			wantSummary: "no schema migrations directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			write := func(rel, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write("go.mod", "module example.com/harbor\n\ngo 1.26.6\n")
			write("cmd/generate/main.go", readingDeclaration)
			for rel, content := range tt.files {
				write(rel, content)
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			r := migrationsReader{}.Run(context.Background(), &Env{App: a})
			if r.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v (%s %v)", r.Status, tt.wantStatus, r.Summary, r.Details)
			}
			if r.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", r.Summary, tt.wantSummary)
			}
			if diff := cmp.Diff(tt.wantDetails, r.Details); diff != "" {
				t.Errorf("Details mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
