package deploy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestLatestBackup pins the backup listing a restore from a production backup starts
// with: the filter names the database and the READY state, the page size joins the
// filter's query (a second '?' made Spanner refuse the filter in the first restore from a
// backup ever run), every page is read, and the newest READY backup of the database
// alone is the answer.
func TestLatestBackup(t *testing.T) {
	t.Parallel()

	const (
		instance = "projects/p/instances/i"
		database = "projects/p/instances/i/databases/d"
	)
	backup := func(name, db, state, created string) map[string]any {
		return map[string]any{"name": instance + "/backups/" + name, "database": db, "state": state, "createTime": created}
	}
	tests := []struct {
		name      string
		pages     []map[string]any
		want      string
		wantState string
	}{
		{
			name: "the newest backup of the database, still CREATING, with its state",
			pages: []map[string]any{{"backups": []map[string]any{
				backup("old", database, "READY", "2026-09-30T02:00:00Z"),
				backup("new", database, "READY", "2026-10-01T02:00:00Z"),
				backup("creating", database, "CREATING", "2026-10-02T02:00:00Z"),
				backup("other", database+"2", "READY", "2026-10-03T02:00:00Z"),
			}}},
			want:      instance + "/backups/creating",
			wantState: "CREATING",
		},
		{
			name: "the newest READY backup when none is being taken",
			pages: []map[string]any{{"backups": []map[string]any{
				backup("old", database, "READY", "2026-09-30T02:00:00Z"),
				backup("new", database, "READY", "2026-10-01T02:00:00Z"),
				backup("unspecified", database, "STATE_UNSPECIFIED", "2026-10-02T02:00:00Z"),
			}}},
			want:      instance + "/backups/new",
			wantState: "READY",
		},
		{
			name:  "no backup",
			pages: []map[string]any{{"backups": []map[string]any{}}},
			want:  "",
		},
		{
			name: "a second page",
			pages: []map[string]any{
				{"backups": []map[string]any{backup("first", database, "READY", "2026-09-30T02:00:00Z")}, "nextPageToken": "more"},
				{"backups": []map[string]any{backup("second", database, "READY", "2026-10-01T02:00:00Z")}},
			},
			want:      instance + "/backups/second",
			wantState: "READY",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := int(calls.Add(1)) - 1
				q := r.URL.Query()
				if r.URL.Path != "/v1/"+instance+"/backups" {
					t.Errorf("path = %s, want /v1/%s/backups", r.URL.Path, instance)
				}
				if got, want := q.Get("filter"), `database:"`+database+`"`; got != want {
					t.Errorf("filter = %q, want %q", got, want)
				}
				if got := q.Get("pageSize"); got != "100" {
					t.Errorf("pageSize = %q, want 100", got)
				}
				if n > 0 && q.Get("pageToken") != "more" {
					t.Errorf("page %d: pageToken = %q, want more", n+1, q.Get("pageToken"))
				}
				if n >= len(tt.pages) {
					t.Errorf("page %d asked for, %d exist", n+1, len(tt.pages))
					n = len(tt.pages) - 1
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(tt.pages[n]); err != nil {
					t.Errorf("Encode() error = %v", err)
				}
			}))
			defer srv.Close()

			s := &spanner{cloudRun: &cloudRun{http: srv.Client(), base: srv.URL, service: "Spanner"}}
			got, err := s.LatestBackup(t.Context(), instance, database)
			if err != nil {
				t.Fatalf("LatestBackup() error = %v", err)
			}
			switch {
			case tt.want == "" && got != nil:
				t.Errorf("LatestBackup() = %s, want none", got.Name)
			case tt.want != "" && got == nil:
				t.Errorf("LatestBackup() = none, want %s", tt.want)
			case got != nil && got.Name != tt.want:
				t.Errorf("LatestBackup() = %s, want %s", got.Name, tt.want)
			case got != nil && got.State != tt.wantState:
				t.Errorf("LatestBackup().State = %s, want %s", got.State, tt.wantState)
			}
			if int(calls.Load()) != len(tt.pages) {
				t.Errorf("pages read = %d, want %d", calls.Load(), len(tt.pages))
			}
		})
	}
}
