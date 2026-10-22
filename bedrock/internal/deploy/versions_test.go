package deploy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestVersions(t *testing.T) {
	t.Parallel()

	const (
		fresh   = "projects/p/instances/i/databases/quill-pr7-db"
		applied = "projects/p/instances/i/databases/imp-tst-gbl-quill-db"
		broken  = "projects/p/instances/i/databases/broken-db"
		gone    = "projects/p/instances/i/databases/gone-db"
	)
	var mu sync.Mutex
	deleted := map[string]int{}
	answer := func(w http.ResponseWriter, status int, body map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	refusal := func(code int, message string) map[string]any {
		return map[string]any{"error": map[string]any{"code": code, "message": message}}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := strings.TrimPrefix(r.URL.Path, "/v1/")
		switch {
		case r.Method == http.MethodPost && path == gone+"/sessions":
			answer(w, http.StatusNotFound, refusal(404, "Database not found: "+gone))
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/sessions"):
			answer(w, http.StatusOK, map[string]any{"name": strings.TrimSuffix(path, "/sessions") + "/sessions/s1"})
		case r.Method == http.MethodDelete && strings.HasSuffix(path, "/sessions/s1"):
			deleted[strings.TrimSuffix(path, "/sessions/s1")]++
			answer(w, http.StatusOK, map[string]any{})
		case r.Method == http.MethodPost && path == fresh+"/sessions/s1:executeSql":
			// A database nothing has run on: the migrate command has not created the table.
			answer(w, http.StatusBadRequest, refusal(400, "Table not found: SchemaMigrations [at 1:28]\nSELECT Version, Dirty FROM SchemaMigrations LIMIT 1\n                           ^"))
		case r.Method == http.MethodPost && path == applied+"/sessions/s1:executeSql":
			answer(w, http.StatusOK, map[string]any{"rows": []any{[]any{"7", false}}})
		case r.Method == http.MethodPost && path == broken+"/sessions/s1:executeSql":
			answer(w, http.StatusBadRequest, refusal(400, "Syntax error: Unexpected keyword LIMIT [at 1:36]"))
		default:
			answer(w, http.StatusForbidden, refusal(403, "Permission denied on resource"))
		}
	}))
	t.Cleanup(srv.Close)
	reader := &versions{spanner: &cloudRun{http: srv.Client(), base: srv.URL, service: serviceSpanner}}
	ctx := t.Context()

	tests := []struct {
		name     string
		database string
		want     MigrationVersion
		wantErr  string
		notFound bool
	}{
		{name: "a database without the table yet is no version", database: fresh, want: MigrationVersion{}},
		{name: "a database with a row is its version", database: applied, want: MigrationVersion{Number: 7, Known: true}},
		{name: "another refusal of the query is an error", database: broken, wantErr: "Spanner answered HTTP 400"},
		{name: "a database that does not exist is a not-found error", database: gone, wantErr: "Spanner answered HTTP 404", notFound: true},
	}
	// The sessions are deleted once each, checked when every case is done.
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, database := range []string{fresh, applied, broken} {
			if deleted[database] != 1 {
				t.Errorf("the session of %s was deleted %d time(s), want once", database, deleted[database])
			}
		}
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := reader.Version(ctx, tt.database, "")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Version() error = %v, want %q", err, tt.wantErr)
				}
				if isNotFound(err) != tt.notFound {
					t.Errorf("isNotFound() = %v, want %v", isNotFound(err), tt.notFound)
				}

				return
			}
			if err != nil {
				t.Fatalf("Version() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Version() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
