package deploy

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// fakeVersions answers migration versions by database name: a name not in versions is a
// missing database (404); a name in denied is a read the identity may not make (403).
type fakeVersions struct {
	mu       sync.Mutex
	versions map[string]MigrationVersion
	denied   map[string]bool
	reads    []string
}

func (f *fakeVersions) Version(_ context.Context, database, role string) (MigrationVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, database+" as "+role)
	if f.denied[database] {
		return MigrationVersion{}, &apiError{service: serviceSpanner, status: http.StatusForbidden, method: http.MethodPost, path: "/v1/" + database + "/sessions", message: "Permission 'spanner.databaseRoles.use' denied"}
	}
	v, ok := f.versions[database]
	if !ok {
		return MigrationVersion{}, &apiError{service: serviceSpanner, status: http.StatusNotFound, method: http.MethodPost, path: "/v1/" + database + "/sessions", message: "Database not found"}
	}

	return v, nil
}

func (f *fakeVersions) open(context.Context) (Versions, error) { return f, nil }

func TestConsolidationShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		names []string
		want  int
	}{
		{name: "the folded up file alone", names: []string{"000003_Consolidated.up.sql"}, want: 3},
		{name: "the folded up and down files", names: []string{"000003_Consolidated.down.sql", "000003_Consolidated.up.sql", "README.md"}, want: 3},
		{name: "a sequence", names: []string{"000001_Init.up.sql", "000002_Next.up.sql"}, want: 0},
		{name: "one ordinary file", names: []string{"000001_Init.up.sql"}, want: 0},
		{name: "a folded file beside another", names: []string{"000003_Consolidated.up.sql", "000004_Next.up.sql"}, want: 0},
		{name: "nothing", names: nil, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := consolidationShape(tt.names); got != tt.want {
				t.Errorf("consolidationShape(%v) = %d, want %d", tt.names, got, tt.want)
			}
		})
	}
}

func TestConsolidationOf(t *testing.T) {
	t.Parallel()

	committed := []string{"000001_Init.up.sql", "000002_Next.up.sql", "000002_Next.down.sql"}
	tests := []struct {
		name      string
		committed []string
		tree      []string
		want      *consolidation
	}{
		{name: "every committed file folded into the last index", committed: committed, tree: []string{"000002_Consolidated.up.sql", "000002_Consolidated.down.sql"}, want: &consolidation{Dir: "schema/migrations", Index: 2}},
		{name: "a committed file kept is not a fold", committed: committed, tree: []string{"000001_Init.up.sql", "000002_Consolidated.up.sql"}, want: nil},
		{name: "a fold at the wrong index", committed: committed, tree: []string{"000003_Consolidated.up.sql"}, want: nil},
		{name: "no committed files", committed: nil, tree: []string{"000001_Consolidated.up.sql"}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, consolidationOf("schema/migrations", tt.committed, tt.tree)); diff != "" {
				t.Errorf("consolidationOf() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEnvironmentVersion(t *testing.T) {
	t.Parallel()

	const base = "projects/p/instances/i/databases/acme-stg-gbl-quill-db"
	tests := []struct {
		name     string
		versions map[string]MigrationVersion
		denied   map[string]bool
		wantLive string
		wantVer  MigrationVersion
		wantErr  string
		wantRead []string
	}{
		{
			name:     "the first generation is live",
			versions: map[string]MigrationVersion{base: {Number: 4, Known: true}},
			wantLive: base, wantVer: MigrationVersion{Number: 4, Known: true},
			wantRead: []string{base + " as migrations_reader", base + "-2 as migrations_reader"},
		},
		{
			name:     "a restore's third generation is live",
			versions: map[string]MigrationVersion{base: {Number: 4, Known: true}, base + "-2": {Number: 3, Known: true}, base + "-3": {Number: 5, Known: true}},
			wantLive: base + "-3", wantVer: MigrationVersion{Number: 5, Known: true},
		},
		{
			name:    "no database at all",
			wantErr: "no database " + base,
		},
		{
			name:     "a read the identity may not make",
			versions: map[string]MigrationVersion{base: {Number: 4, Known: true}},
			denied:   map[string]bool{base: true},
			wantErr:  "denied",
		},
		{
			name:     "a denial past a readable generation ends the generations: the readable one is live",
			versions: map[string]MigrationVersion{base: {Number: 4, Known: true}},
			denied:   map[string]bool{base + "-2": true},
			wantLive: base, wantVer: MigrationVersion{Number: 4, Known: true},
			wantRead: []string{base + " as migrations_reader", base + "-2 as migrations_reader"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &fakeVersions{versions: tt.versions, denied: tt.denied}
			version, live, err := environmentVersion(t.Context(), f, base, migrationsReaderRole)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("environmentVersion() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("environmentVersion() error = %v", err)
			}
			if live != tt.wantLive || version != tt.wantVer {
				t.Errorf("environmentVersion() = %v at %s, want %v at %s", version, live, tt.wantVer, tt.wantLive)
			}
			if tt.wantRead != nil {
				if diff := cmp.Diff(tt.wantRead, f.reads); diff != "" {
					t.Errorf("reads mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestVersionRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		answer  map[string]any
		want    MigrationVersion
		wantErr string
	}{
		{name: "no rows is no version", answer: map[string]any{"metadata": map[string]any{}}},
		{name: "a row", answer: map[string]any{"rows": []any{[]any{"8", false}}}, want: MigrationVersion{Number: 8, Known: true}},
		{name: "a dirty row", answer: map[string]any{"rows": []any{[]any{"9", true}}}, want: MigrationVersion{Number: 9, Known: true, Dirty: true}},
		{name: "a version that is not an integer", answer: map[string]any{"rows": []any{[]any{"x", false}}}, wantErr: "not an integer"},
		{name: "a short row", answer: map[string]any{"rows": []any{[]any{"8"}}}, wantErr: "1 column(s)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := versionRow(tt.answer)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("versionRow() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("versionRow() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("versionRow() = %v, want %v", got, tt.want)
			}
			if s := got.String(); tt.name == "no rows is no version" && s != "no version" || tt.name == "a dirty row" && s != "dirty at 9" || tt.name == "a row" && s != "8" {
				t.Errorf("String() = %q", s)
			}
		})
	}
}
