// Demonstrates: filestore.cleanup.
package integration

// This suite covers the orphaned-file cleanup the job process runs (cmd/jobs
// cleanup-files, pkg/jobs): over a world holding one attached brief, an object no row
// holds and older than the window is deleted from the Documents store, the brief and a
// young orphan stay, the default store refuses to run while no row holds a key in it
// and runs once a photo is attached, and a dry run deletes nothing.

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/jobs"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
)

// documentsStore is the Documents store's name.
var documentsStore = resource.StoreNameFor[resources.Documents]()

// cleanupWorld is a fresh demo world with its stores and the resource client the job
// process would open over them.
func cleanupWorld(t *testing.T) (h http.Handler, stores *testStores, client resource.Client) {
	t.Helper()

	db, err := prepareDatabase(t.Context(), t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	stores = newTestStores()
	a := newAppWithStores(db, demoAccessClient(t), stores)

	return router.NewTestRouter(a), stores, resource.NewSpannerClient(db.Client, stores.options()...)
}

// plant stores an object no row holds, aged to the given time.
func plant(t *testing.T, stores *testStores, store resource.StoreName, created time.Time) string {
	t.Helper()

	id, err := ccc.NewUUID()
	if err != nil {
		t.Fatalf("ccc.NewUUID() error = %v", err)
	}
	mem := stores.documents
	if store == resource.DefaultStore {
		mem = stores.files
	}
	if err := mem.Put(t.Context(), id.String(), "text/plain", strings.NewReader("left behind")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if !mem.Age(id.String(), created) {
		t.Fatalf("Age(%s) found no object", id)
	}

	return id.String()
}

func TestCleanupFiles(t *testing.T) {
	t.Parallel()

	aged := time.Now().Add(-3 * 24 * time.Hour)
	young := time.Now().Add(-time.Hour)

	tests := []struct {
		name string
		// arrange sets the world up beyond the attached brief and returns the keys the
		// run must delete.
		arrange func(t *testing.T, h http.Handler, stores *testStores) (orphans []string)
		opts    jobs.CleanupOptions
		// wantReports is the reports' one-line form with the store's listed count
		// elided; wantErr the refusal.
		wantReports []string
		wantErr     string
	}{
		{
			name: "an aged orphan goes from the Documents store, the brief and a young orphan stay",
			arrange: func(t *testing.T, _ http.Handler, stores *testStores) []string {
				t.Helper()
				plant(t, stores, documentsStore, young)

				return []string{plant(t, stores, documentsStore, aged)}
			},
			opts:        jobs.CleanupOptions{Stores: []resource.StoreName{documentsStore}},
			wantReports: []string{"store documents: 3 objects listed, 1 older than 48h0m0s with a UUID name, 1 keys held by rows, deleted 1 orphaned objects"},
		},
		{
			name: "a dry run reports the orphan and deletes nothing",
			arrange: func(t *testing.T, _ http.Handler, stores *testStores) []string {
				t.Helper()
				plant(t, stores, documentsStore, aged)

				return nil
			},
			opts:        jobs.CleanupOptions{Stores: []resource.StoreName{documentsStore}, DryRun: true},
			wantReports: []string{"store documents: 2 objects listed, 1 older than 48h0m0s with a UUID name, 1 keys held by rows, would delete 1 orphaned objects"},
		},
		{
			name: "a longer window keeps the orphan",
			arrange: func(t *testing.T, _ http.Handler, stores *testStores) []string {
				t.Helper()
				plant(t, stores, documentsStore, aged)

				return nil
			},
			opts:        jobs.CleanupOptions{Stores: []resource.StoreName{documentsStore}, Window: 7 * 24 * time.Hour},
			wantReports: []string{"store documents: 2 objects listed, 0 older than 168h0m0s with a UUID name, 1 keys held by rows, deleted 0 orphaned objects"},
		},
		{
			name: "the default store refuses while no row holds a key in it",
			arrange: func(t *testing.T, _ http.Handler, stores *testStores) []string {
				t.Helper()
				plant(t, stores, resource.DefaultStore, aged)

				return nil
			},
			opts:    jobs.CleanupOptions{Stores: []resource.StoreName{resource.DefaultStore}},
			wantErr: "no row holds any key in the default store; the cleanup refuses to empty a store whose holders it was not given",
		},
		{
			name: "every store at once, each read through its own holders",
			arrange: func(t *testing.T, h http.Handler, stores *testStores) []string {
				t.Helper()
				body, contentType := multipartBody(t, photoTarget, uploadFile{name: "hull.png", contentType: "image/png", content: []byte("PNG")})
				status, respBody := doUploadAs(t, h, "photographer", sectorPath(anvil, "attach-refit-photo"), body, contentType, false)
				assertStatus(t, status, http.StatusOK, respBody)

				return []string{plant(t, stores, documentsStore, aged), plant(t, stores, resource.DefaultStore, aged)}
			},
			wantReports: []string{
				"store documents: 2 objects listed, 1 older than 48h0m0s with a UUID name, 1 keys held by rows, deleted 1 orphaned objects",
				"the default store: 2 objects listed, 1 older than 48h0m0s with a UUID name, 1 keys held by rows, deleted 1 orphaned objects",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, stores, client := cleanupWorld(t)
			attachHaulerBrief(t, h)
			live := stores.documents.Keys()
			if len(live) != 1 {
				t.Fatalf("Documents store = %v, want the brief's one object", live)
			}
			orphans := tt.arrange(t, h, stores)
			before := slices.Concat(stores.documents.Keys(), stores.files.Keys())

			reports, err := jobs.CleanupFiles(t.Context(), client, tt.opts)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("CleanupFiles() error = %v, want containing %q", err, tt.wantErr)
				}
				if after := slices.Concat(stores.documents.Keys(), stores.files.Keys()); !slices.Equal(after, before) {
					t.Errorf("a refused run changed the stores: %v, want %v", after, before)
				}

				return
			}
			if err != nil {
				t.Fatalf("CleanupFiles() error = %v", err)
			}
			var got []string
			for _, report := range reports {
				got = append(got, report.String())
			}
			if !slices.Equal(got, tt.wantReports) {
				t.Errorf("reports = %q, want %q", got, tt.wantReports)
			}
			after := slices.Concat(stores.documents.Keys(), stores.files.Keys())
			for _, orphan := range orphans {
				if slices.Contains(after, orphan) {
					t.Errorf("orphan %s is still stored", orphan)
				}
			}
			if !slices.Contains(after, live[0]) {
				t.Errorf("the brief's object %s was deleted", live[0])
			}
			if want := len(before) - len(orphans); len(after) != want {
				t.Errorf("stores hold %v after the run, want %d objects", after, want)
			}
		})
	}
}
