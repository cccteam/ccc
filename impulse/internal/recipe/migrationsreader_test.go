package recipe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// readerDeclaration is a generator program naming the schema migrations directory.
const readerDeclaration = `package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
)

func newGenerator(ctx context.Context) (generation.Generator, error) {
	return generation.NewResourceGenerator(ctx, "pkg/resources", []string{"file://schema/migrations"},
		generation.GenerateHandlers("app"),
		generation.GenerateRoutes("pkg/router", "api"),
	)
}
`

func TestMigrationsReader(t *testing.T) {
	t.Parallel()

	const (
		first  = "CREATE TABLE DataChangeEvents (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n"
		role   = "CREATE ROLE migrations_reader;\nGRANT SELECT ON TABLE SchemaMigrations TO ROLE migrations_reader;\n"
		second = "CREATE TABLE Things (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n"
	)
	tests := []struct {
		name       string
		files      map[string]string
		wantDetect []string
		wantDid    []string
		wantFiles  []string
	}{
		{
			name:  "a sequence with the role has nothing to do",
			files: map[string]string{"schema/migrations/000001_Init.up.sql": first + role, "schema/migrations/000002_Things.up.sql": second},
		},
		{
			name:       "a sequence without it gets the migration at its next index",
			files:      map[string]string{"schema/migrations/000001_Init.up.sql": first, "schema/migrations/000002_Things.up.sql": second, "schema/migrations/000002_Things.down.sql": "DROP TABLE Things;\n"},
			wantDetect: []string{"schema/migrations: no migration runs CREATE ROLE migrations_reader and GRANT SELECT ON TABLE SchemaMigrations TO ROLE migrations_reader"},
			wantDid:    []string{"wrote schema/migrations/000003_MigrationsReader.up.sql and schema/migrations/000003_MigrationsReader.down.sql: the migrations_reader database role, created with select on SchemaMigrations, which the pipeline's migration guard reads this database's migration version through"},
			wantFiles:  []string{"schema/migrations/000003_MigrationsReader.down.sql", "schema/migrations/000003_MigrationsReader.up.sql"},
		},
		{
			name:  "an application without a schema has nothing to do",
			files: map[string]string{},
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
			write("cmd/generate/main.go", readerDeclaration)
			for rel, content := range tt.files {
				write(rel, content)
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			r := MigrationsReader{}
			found, err := r.Detect(t.Context(), a)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDetect, found); diff != "" {
				t.Errorf("Detect() mismatch (-want +got):\n%s", diff)
			}
			ch, err := r.Apply(t.Context(), a, nil)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDid, ch.Did); diff != "" {
				t.Errorf("Apply() Did mismatch (-want +got):\n%s", diff)
			}
			for _, rel := range tt.wantFiles {
				data, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil {
					t.Fatalf("%s: %v", rel, err)
				}
				if filepath.Ext(rel) == ".sql" && filepath.Base(rel) == "000003_MigrationsReader.up.sql" && string(data[len(data)-len(role):]) != role {
					t.Errorf("%s ends with %q, want the two statements", rel, data)
				}
			}
			// A second run finds nothing and writes nothing.
			found, err = r.Detect(t.Context(), a)
			if err != nil {
				t.Fatalf("Detect() again error = %v", err)
			}
			if len(found) != 0 {
				t.Errorf("Detect() again = %v, want nothing", found)
			}
			ch, err = r.Apply(t.Context(), a, nil)
			if err != nil {
				t.Fatalf("Apply() again error = %v", err)
			}
			if len(ch.Did) != 0 {
				t.Errorf("Apply() again did %v, want nothing", ch.Did)
			}
		})
	}
}
