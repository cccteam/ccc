package deploy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// TestFirestoreDeleteAllDocuments holds the bulk delete to the default namespace, the only
// one a Firestore database has: Firestore refuses an empty filter ("Empty entity filter. To
// delete all entities, Use database deletion instead."), which stopped harbor's first
// restore with the maintenance revision after the database was replaced.
func TestFirestoreDeleteAllDocuments(t *testing.T) {
	t.Parallel()

	const database = "projects/p/databases/app-fs"
	tests := []struct {
		name string
		// failed makes the operation end in an error.
		failed  bool
		wantErr string
	}{
		{name: "the delete names the default namespace and waits for the operation"},
		{name: "a failed operation is an error", failed: true, wantErr: "Firestore operation projects/p/databases/app-fs/operations/op-1 failed: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				mu    sync.Mutex
				body  map[string]any
				polls int
			)
			answer := func(w http.ResponseWriter, status int, doc map[string]any) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(doc)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/v1/"+database+":bulkDeleteDocuments":
					data, _ := io.ReadAll(r.Body)
					_ = json.Unmarshal(data, &body)
					answer(w, http.StatusOK, map[string]any{"name": database + "/operations/op-1", "done": false})
				case r.Method == http.MethodGet && r.URL.Path == "/v1/"+database+"/operations/op-1":
					polls++
					op := map[string]any{"name": database + "/operations/op-1", "done": polls >= 2}
					if tt.failed && polls >= 2 {
						op["error"] = map[string]any{"code": 3, "message": "boom"}
					}
					answer(w, http.StatusOK, op)
				default:
					answer(w, http.StatusNotFound, map[string]any{"error": map[string]any{"code": 404, "message": r.Method + " " + r.URL.Path}})
				}
			}))
			t.Cleanup(srv.Close)
			f := &firestore{cloudRun: &cloudRun{http: srv.Client(), base: srv.URL, poll: time.Millisecond}}
			err := f.DeleteAllDocuments(t.Context(), database)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("DeleteAllDocuments() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("DeleteAllDocuments() error = %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if diff := cmp.Diff(map[string]any{"namespaceIds": []any{""}}, body); diff != "" {
				t.Errorf("request body (-want +got):\n%s", diff)
			}
			if polls != 2 {
				t.Errorf("polls = %d, want 2", polls)
			}
		})
	}
}
