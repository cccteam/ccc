package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanMigrations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		files   []string
		missing bool
		want    []string
	}{
		{name: "a contiguous sequence from 000001 passes", files: []string{"000001_init.up.sql", "000001_init.down.sql", "000002_users.up.sql", "000003_roles.up.sql", "000003_roles.down.sql"}},
		{name: "a consolidated history starting above 000001 passes", files: []string{"000069_consolidated.up.sql", "000070_next.up.sql"}},
		{name: "no directory: nothing to check", missing: true},
		{name: "a gap is reported", files: []string{"000001_init.up.sql", "000002_users.up.sql", "000004_late.up.sql"}, want: []string{": gap: no migration 000003 between 000001 and 000004; the sequence is contiguous so nothing is skipped"}},
		{name: "two up files on one index are reported", files: []string{"000001_init.up.sql", "000002_a.up.sql", "000002_b.up.sql"}, want: []string{"000002_a.up.sql: index 000002 has 2 up files; one migration per index", "000002_b.up.sql: index 000002 has 2 up files; one migration per index"}},
		{name: "a down without an up is reported", files: []string{"000001_init.up.sql", "000002_orphan.down.sql"}, want: []string{"000002_orphan.down.sql: index 000002 has a down file and no up file"}},
		{name: "a file that is not a migration is reported", files: []string{"000001_init.up.sql", "notes.sql", "2_short.up.sql"}, want: []string{"2_short.up.sql: not a migration file name (NNNNNN_name.up.sql or NNNNNN_name.down.sql)", "notes.sql: not a migration file name (NNNNNN_name.up.sql or NNNNNN_name.down.sql)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "schema", "migrations")
			if !tt.missing {
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatal(err)
				}
				for _, name := range tt.files {
					if err := os.WriteFile(filepath.Join(dir, name), []byte("-- sql\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			findings, err := scanMigrations(dir, "schema/migrations")
			if err != nil {
				t.Fatalf("scanMigrations() error = %v", err)
			}
			got := make([]string, 0, len(findings))
			for _, f := range findings {
				// A finding of the whole directory names the directory itself.
				got = append(got, strings.TrimPrefix(strings.TrimPrefix(f.Path, "schema/migrations/"), "schema/migrations")+": "+f.Problem)
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("scanMigrations() = %q, want %q", got, tt.want)
			}
		})
	}
}
