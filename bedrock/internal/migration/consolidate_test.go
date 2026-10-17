package migration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeContent puts a file in the working tree with the content given.
func (r *repo) writeContent(name, content string) {
	r.t.Helper()
	p := filepath.Join(r.root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func TestConsolidate(t *testing.T) {
	t.Parallel()

	// three commits the sequence 000001 to 000003 on master (000002 with no down file)
	// and cuts the branch there.
	three := func(r *repo) {
		r.writeContent(migrations+"/000001_Init.up.sql", "CREATE TABLE Ships (\n  Id STRING(36) NOT NULL,\n) PRIMARY KEY (Id);\n")
		r.writeContent(migrations+"/000001_Init.down.sql", "DROP TABLE Ships;\n")
		r.writeContent(migrations+"/000002_Crews.up.sql", "-- the crews\nCREATE TABLE Crews (Id STRING(36) NOT NULL) PRIMARY KEY (Id)\n")
		r.writeContent(migrations+"/000003_Rows.up.sql", "INSERT INTO Ships (Id) VALUES ('a');\n-- seeded\n")
		r.writeContent(migrations+"/000003_Rows.down.sql", "DELETE FROM Ships WHERE Id = 'a'")
		r.git("add", "-A")
		r.git("commit", "-q", "-m", "three migrations")
		r.git("checkout", "-q", "-b", "consolidate")
	}
	wantUp := `-- 000003_Consolidated: the migrations 000001 to 000003 folded into one, identical by construction
-- (bedrock migration consolidate); each file's statements follow a comment naming it.

-- 000001_Init.up.sql
CREATE TABLE Ships (
  Id STRING(36) NOT NULL,
) PRIMARY KEY (Id);

-- 000002_Crews.up.sql
-- the crews
CREATE TABLE Crews (Id STRING(36) NOT NULL) PRIMARY KEY (Id)
;

-- 000003_Rows.up.sql
INSERT INTO Ships (Id) VALUES ('a');
-- seeded
`
	wantDown := `-- 000003_Consolidated: the down files of the migrations 000001 to 000003 in reverse order
-- (bedrock migration consolidate); each file's statements follow a comment naming it.

-- 000003_Rows.down.sql
DELETE FROM Ships WHERE Id = 'a'
;

-- 000001_Init.down.sql
DROP TABLE Ships;
`
	tests := []struct {
		name  string
		setup func(r *repo)
		// want is the consolidation; wantFiles the directory after; wantUp and wantDown
		// the files' content; wantStaged the lines git status --porcelain shows.
		want       *Consolidation
		wantFiles  []string
		wantUp     string
		wantDown   string
		wantStaged []string
		wantErr    string
	}{
		{
			name:       "folds the sequence into the latest index: the up files in order with a semicolon and a comment each, the down files in reverse, the folded files removed and the change staged",
			setup:      three,
			want:       &Consolidation{Dir: migrations, Index: 3, Up: "000003_Consolidated.up.sql", Down: "000003_Consolidated.down.sql", Folded: []string{"000001_Init", "000002_Crews", "000003_Rows"}, WithoutDown: []string{"000002_Crews"}},
			wantFiles:  []string{"000003_Consolidated.down.sql", "000003_Consolidated.up.sql"},
			wantUp:     wantUp,
			wantDown:   wantDown,
			wantStaged: []string{"D  schema/migrations/000001_Init.down.sql", "D  schema/migrations/000001_Init.up.sql", "D  schema/migrations/000002_Crews.up.sql", "A  schema/migrations/000003_Consolidated.down.sql", "A  schema/migrations/000003_Consolidated.up.sql", "D  schema/migrations/000003_Rows.down.sql", "D  schema/migrations/000003_Rows.up.sql"},
		},
		{
			name: "a sequence with no down file writes no down file",
			setup: func(r *repo) {
				r.commit("two", migrations+"/000001_Init.up.sql", migrations+"/000002_Crews.up.sql")
				r.git("checkout", "-q", "-b", "consolidate")
			},
			want:      &Consolidation{Dir: migrations, Index: 2, Up: "000002_Consolidated.up.sql", Folded: []string{"000001_Init", "000002_Crews"}, WithoutDown: []string{"000001_Init", "000002_Crews"}},
			wantFiles: []string{"000002_Consolidated.up.sql"},
		},
		{
			name: "a second consolidation folds the first with what followed it",
			setup: func(r *repo) {
				r.commit("folded once", migrations+"/000002_Consolidated.up.sql", migrations+"/000002_Consolidated.down.sql", migrations+"/000003_Crews.up.sql")
				r.git("checkout", "-q", "-b", "consolidate")
			},
			want:      &Consolidation{Dir: migrations, Index: 3, Up: "000003_Consolidated.up.sql", Down: "000003_Consolidated.down.sql", Folded: []string{"000002_Consolidated", "000003_Crews"}, WithoutDown: []string{"000003_Crews"}},
			wantFiles: []string{"000003_Consolidated.down.sql", "000003_Consolidated.up.sql"},
		},
		{
			name: "one migration is nothing to fold",
			setup: func(r *repo) {
				r.commit("one", migrations+"/000001_Init.up.sql")
			},
			want:      &Consolidation{Dir: migrations},
			wantFiles: []string{"000001_Init.up.sql"},
		},
		{
			name: "refuses a change under the directory",
			setup: func(r *repo) {
				three(r)
				r.write(migrations + "/000004_Sites.up.sql")
			},
			wantErr: "schema/migrations is not as master holds it (git status lists a change under it): a consolidation folds the committed sequence alone, in a pull request of its own; commit or stash the change, or cut the branch from master",
		},
		{
			name: "refuses a migration of the branch's own",
			setup: func(r *repo) {
				three(r)
				r.commit("sites on the branch", migrations+"/000004_Sites.up.sql")
			},
			wantErr: "schema/migrations holds 000004_Sites.up.sql, which master does not: a consolidation folds the branch's committed sequence alone, in a pull request of its own; merge the migration first, then consolidate on a branch cut from master",
		},
		{
			name: "refuses a broken sequence",
			setup: func(r *repo) {
				r.commit("a gap", migrations+"/000001_Init.up.sql", migrations+"/000003_Crews.up.sql")
			},
			wantErr: "schema/migrations is not one sequence (1 problem(s); bedrock migration check lists them): a consolidation folds a sound sequence",
		},
		{
			name:    "a missing directory is an error",
			setup:   func(r *repo) { r.commit("no migrations", "README.md") },
			wantErr: "os.ReadDir(): schema/migrations",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRepo(t)
			tt.setup(r)
			got, err := Consolidate(context.Background(), ConsolidateOptions{Root: r.root, Dir: migrations, Branch: "master"})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Consolidate() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Consolidate() error = %v", err)
			}
			if got.Ref != "refs/heads/master" || got.Commit == "" {
				t.Errorf("Consolidate() read %s at %q, want refs/heads/master at a commit", got.Ref, got.Commit)
			}
			got.Ref, got.Commit = "", ""
			if strings.Join(got.Folded, ",") != strings.Join(tt.want.Folded, ",") || strings.Join(got.WithoutDown, ",") != strings.Join(tt.want.WithoutDown, ",") || got.Dir != tt.want.Dir || got.Index != tt.want.Index || got.Up != tt.want.Up || got.Down != tt.want.Down {
				t.Errorf("Consolidate() = %+v, want %+v", got, tt.want)
			}
			if files := list(t, r.root, migrations); strings.Join(files, " ") != strings.Join(tt.wantFiles, " ") {
				t.Errorf("files = %v, want %v", files, tt.wantFiles)
			}
			if tt.wantUp != "" {
				data, err := os.ReadFile(filepath.Join(r.root, migrations, got.Up))
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != tt.wantUp {
					t.Errorf("up file:\n%s\nwant:\n%s", data, tt.wantUp)
				}
			}
			if tt.wantDown != "" {
				data, err := os.ReadFile(filepath.Join(r.root, migrations, got.Down))
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != tt.wantDown {
					t.Errorf("down file:\n%s\nwant:\n%s", data, tt.wantDown)
				}
			}
			if tt.wantStaged != nil {
				status := strings.TrimSpace(r.git("status", "--porcelain", "--", migrations))
				if status != strings.Join(tt.wantStaged, "\n") {
					t.Errorf("git status:\n%s\nwant:\n%s", status, strings.Join(tt.wantStaged, "\n"))
				}
			}
		})
	}
}
