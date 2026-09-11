package deploy

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestSchemaOwnedTables pins which tables a reset keeps: those the schema's up scripts
// insert into, read from the migrations directory, and nothing on a source it cannot
// read.
func TestSchemaOwnedTables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		files   map[string]string
		source  func(dir string) string
		want    []string
		wantErr bool
	}{
		{
			name: "the tables the up scripts insert into",
			files: map[string]string{
				"000001_kinds.up.sql":    "CREATE TABLE Kinds (Id STRING(36)) PRIMARY KEY (Id);\nINSERT INTO Kinds (Id) VALUES ('a');\nINSERT INTO Kinds (Id) VALUES ('b');",
				"000002_statuses.up.sql": "INSERT INTO Statuses (Id) VALUES ('x');",
			},
			want: []string{"Kinds", "Statuses"},
		},
		{
			name: "down scripts are not read",
			files: map[string]string{
				"000001_kinds.up.sql":   "CREATE TABLE Kinds (Id STRING(36)) PRIMARY KEY (Id);",
				"000001_kinds.down.sql": "INSERT INTO Kinds (Id) VALUES ('a');",
			},
			want: []string{},
		},
		{
			name: "a backticked name and a lowercase keyword",
			files: map[string]string{
				"000001_kinds.up.sql": "insert into `Kinds` (Id) VALUES ('a');",
			},
			want: []string{"Kinds"},
		},
		{
			name:    "no up scripts is an error, not an empty set",
			files:   map[string]string{},
			wantErr: true,
		},
		{
			name:  "a source that is not a file URL is an error",
			files: map[string]string{"000001_kinds.up.sql": "INSERT INTO Kinds (Id) VALUES ('a');"},
			source: func(string) string {
				return "spanner://kinds"
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			for name, content := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatalf("os.WriteFile() error = %v", err)
				}
			}
			source := "file://" + dir
			if tt.source != nil {
				source = tt.source(dir)
			}

			owned, err := schemaOwnedTables(source)
			if (err != nil) != tt.wantErr {
				t.Fatalf("schemaOwnedTables() error = %v, wantErr %t", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			got := make([]string, 0, len(owned))
			for table := range owned {
				got = append(got, table)
			}
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf("schemaOwnedTables() = %v, want %v", got, tt.want)
			}
		})
	}
}
