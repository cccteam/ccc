package filestore_test

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/filestore"
	perrors "github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

// scans is a named store the cleanup tests keep a second holder on.
type scans struct{ resource.Store }

// plainStore is a store that cannot list its objects: the frame's three methods alone.
type plainStore struct {
	mem *filestore.Mem
}

func (s plainStore) Put(ctx context.Context, key, contentType string, r io.Reader) error {
	if err := s.mem.Put(ctx, key, contentType, r); err != nil {
		return perrors.Wrap(err, "filestore.Mem.Put()")
	}

	return nil
}

func (s plainStore) Delete(ctx context.Context, keys []string) error {
	if err := s.mem.Delete(ctx, keys); err != nil {
		return perrors.Wrap(err, "filestore.Mem.Delete()")
	}

	return nil
}

func (s plainStore) Open(ctx context.Context, key string) (*resource.Content, error) {
	content, err := s.mem.Open(ctx, key)
	if err != nil {
		return nil, perrors.Wrap(err, "filestore.Mem.Open()")
	}

	return content, nil
}

// cleanupWorld is a memory store with objects of every kind the cleanup tells apart,
// and the holders and keys that make them live or orphaned.
type cleanupWorld struct {
	store *filestore.Mem
	now   time.Time
	// live and orphan are aged UUID keys, held and unheld; young is an unheld key
	// inside the window; named is an aged object with a row's own name.
	live, orphan, young, named string
}

func newCleanupWorld(t *testing.T) *cleanupWorld {
	t.Helper()

	w := &cleanupWorld{store: filestore.NewMem(), now: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)}
	w.live, w.orphan, w.young, w.named = newKey(t), newKey(t), newKey(t), "sponsors/logos/"+newKey(t)
	for _, key := range []string{w.live, w.orphan, w.young, w.named} {
		put(t, w.store, key, "text/plain", key)
	}
	for _, key := range []string{w.live, w.orphan, w.named} {
		if !w.store.Age(key, w.now.Add(-3*24*time.Hour)) {
			t.Fatalf("Age(%s) found no object", key)
		}
	}
	if !w.store.Age(w.young, w.now.Add(-time.Hour)) {
		t.Fatalf("Age(%s) found no object", w.young)
	}

	return w
}

// client wires the world's store as the default on a mock client.
func (w *cleanupWorld) client(opts ...resource.ClientOption) resource.Client {
	return resource.NewMockClient(nil, nil, nil, append([]resource.ClientOption{resource.WithFileStore(w.store)}, opts...)...)
}

// holders are the default store's computed holder and a named store's, whose keys the
// test supplies through ComputedKeys.
func (w *cleanupWorld) holders() []resource.FileHolder {
	return []resource.FileHolder{
		resource.ComputedFileHolder("Documents", resource.FileKey{Field: "StoreKey", Store: resource.DefaultStore}),
		resource.ComputedFileHolder("Scans", resource.FileKey{Field: "ScanKey", Store: resource.StoreNameFor[scans]()}),
	}
}

// keysOf supplies each holder's keys: the live key for Documents, and a key that would
// be an orphan on the default store for Scans, which holds it on the other store.
func (w *cleanupWorld) keysOf(_ context.Context, holder resource.FileHolder) ([]string, error) {
	switch holder.Resource {
	case "Documents":
		return []string{w.live, ""}, nil
	case "Scans":
		return []string{w.orphan}, nil
	default:
		return nil, fmt.Errorf("unexpected holder %s", holder.Resource)
	}
}

// TestCleanup pins the orphaned-file cleanup: what it deletes and keeps, its report,
// the dry run, and every refusal.
func TestCleanup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// cleanup builds the run over the world; the world's store is the default.
		cleanup func(w *cleanupWorld) filestore.Cleanup
		// wantKept are the keys left in the store; wantOrphans the report's orphans.
		wantKept    func(w *cleanupWorld) []string
		wantOrphans func(w *cleanupWorld) []string
		wantReport  string
		wantErr     string
	}{
		{
			name: "deletes the aged orphan and keeps the live, the young and the named object",
			cleanup: func(w *cleanupWorld) filestore.Cleanup {
				return filestore.Cleanup{Client: w.client(), Store: resource.DefaultStore, Holders: w.holders(), ComputedKeys: w.keysOf, Now: func() time.Time { return w.now }}
			},
			wantKept: func(w *cleanupWorld) []string {
				return []string{w.live, w.named, w.young}
			},
			wantOrphans: func(w *cleanupWorld) []string {
				return []string{w.orphan}
			},
			wantReport: "the default store: 4 objects listed, 2 older than 48h0m0s with a UUID name, 1 keys held by rows, deleted 1 orphaned objects",
		},
		{
			name: "a dry run lists the orphan and deletes nothing",
			cleanup: func(w *cleanupWorld) filestore.Cleanup {
				return filestore.Cleanup{Client: w.client(), Store: resource.DefaultStore, Holders: w.holders(), ComputedKeys: w.keysOf, Now: func() time.Time { return w.now }, DryRun: true}
			},
			wantKept: func(w *cleanupWorld) []string {
				return []string{w.live, w.named, w.orphan, w.young}
			},
			wantOrphans: func(w *cleanupWorld) []string {
				return []string{w.orphan}
			},
			wantReport: "the default store: 4 objects listed, 2 older than 48h0m0s with a UUID name, 1 keys held by rows, would delete 1 orphaned objects",
		},
		{
			name: "a longer window keeps the orphan",
			cleanup: func(w *cleanupWorld) filestore.Cleanup {
				return filestore.Cleanup{Client: w.client(), Store: resource.DefaultStore, Holders: w.holders(), ComputedKeys: w.keysOf, Now: func() time.Time { return w.now }, Window: 7 * 24 * time.Hour}
			},
			wantKept: func(w *cleanupWorld) []string {
				return []string{w.live, w.named, w.orphan, w.young}
			},
			wantOrphans: func(*cleanupWorld) []string {
				return nil
			},
			wantReport: "the default store: 4 objects listed, 0 older than 168h0m0s with a UUID name, 1 keys held by rows, deleted 0 orphaned objects",
		},
		{
			name: "a window under a day is refused",
			cleanup: func(w *cleanupWorld) filestore.Cleanup {
				return filestore.Cleanup{Client: w.client(), Store: resource.DefaultStore, Holders: w.holders(), ComputedKeys: w.keysOf, Window: 23 * time.Hour}
			},
			wantErr: "the window 23h0m0s is under the minimum 24h0m0s; an object younger than that may belong to a row still being written",
		},
		{
			name: "no client is refused",
			cleanup: func(w *cleanupWorld) filestore.Cleanup {
				return filestore.Cleanup{Store: resource.DefaultStore, Holders: w.holders(), ComputedKeys: w.keysOf}
			},
			wantErr: "the cleanup needs the resource client the store is wired on",
		},
		{
			name: "a store that is not wired is refused",
			cleanup: func(w *cleanupWorld) filestore.Cleanup {
				return filestore.Cleanup{Client: w.client(), Store: resource.StoreNameFor[scans](), Holders: w.holders(), ComputedKeys: w.keysOf}
			},
			wantErr: "no file store is wired for store scans on the resource client",
		},
		{
			name: "a store that cannot list is refused",
			cleanup: func(w *cleanupWorld) filestore.Cleanup {
				return filestore.Cleanup{Client: w.client(resource.WithNamedFileStore[scans](plainStore{mem: filestore.NewMem()})), Store: resource.StoreNameFor[scans](), Holders: w.holders(), ComputedKeys: w.keysOf}
			},
			wantErr: "the store wired for store scans (filestore_test.plainStore) cannot list its objects; the cleanup runs over the stores this package opens",
		},
		{
			name: "a computed holder with no keys supplied is refused",
			cleanup: func(w *cleanupWorld) filestore.Cleanup {
				return filestore.Cleanup{Client: w.client(), Store: resource.DefaultStore, Holders: w.holders()}
			},
			wantErr: "Documents is a computed resource holding keys in the default store, and the cleanup was given no ComputedKeys to read them with; supply them, or the cleanup refuses",
		},
		{
			name: "no live key is refused",
			cleanup: func(w *cleanupWorld) filestore.Cleanup {
				return filestore.Cleanup{Client: w.client(), Store: resource.DefaultStore, Holders: w.holders(), ComputedKeys: func(context.Context, resource.FileHolder) ([]string, error) {
					return nil, nil
				}}
			},
			wantErr: "no row holds any key in the default store; the cleanup refuses to empty a store whose holders it was not given",
		},
		{
			name: "a holder's read failure stops the run",
			cleanup: func(w *cleanupWorld) filestore.Cleanup {
				return filestore.Cleanup{Client: w.client(), Store: resource.DefaultStore, Holders: w.holders(), ComputedKeys: func(context.Context, resource.FileHolder) ([]string, error) {
					return nil, fmt.Errorf("the database is away")
				}}
			},
			wantErr: "reading the keys Documents holds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newCleanupWorld(t)
			cleanup := tt.cleanup(w)
			report, err := cleanup.Run(t.Context())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Run() error = %v, want containing %q", err, tt.wantErr)
				}
				if got := w.store.Keys(); len(got) != 4 {
					t.Errorf("a refused run deleted objects: %d left of 4", len(got))
				}

				return
			}
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			wantKept := tt.wantKept(w)
			slices.Sort(wantKept)
			if diff := cmp.Diff(wantKept, w.store.Keys()); diff != "" {
				t.Errorf("kept keys mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantOrphans(w), report.Orphans); diff != "" {
				t.Errorf("Orphans mismatch (-want +got):\n%s", diff)
			}
			if got := report.String(); got != tt.wantReport {
				t.Errorf("Report = %q, want %q", got, tt.wantReport)
			}
		})
	}
}

// TestCleanup_unclaimedShare pins the share rule: at the floor, an unclaimed share above
// the limit refuses; under the floor, or with enough live objects, the run proceeds.
func TestCleanup_unclaimedShare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		orphans  int
		live     int
		wantErr  string
		wantKept int
	}{
		{name: "under the floor every orphan goes", orphans: filestore.UnclaimedFloor - 1, live: 0, wantKept: 0},
		{name: "at the floor with no live objects the run is refused", orphans: filestore.UnclaimedFloor, live: 0, wantErr: "100 of the 100 aged objects in the default store are held by no row, above the 50% the cleanup accepts; the live keys were most likely not all read, so nothing is deleted"},
		{name: "at the floor with as many live objects the share is at the limit and the run proceeds", orphans: filestore.UnclaimedFloor, live: filestore.UnclaimedFloor, wantKept: filestore.UnclaimedFloor},
		{name: "at the floor with one live object fewer the run is refused", orphans: filestore.UnclaimedFloor, live: filestore.UnclaimedFloor - 1, wantErr: "above the 50% the cleanup accepts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := filestore.NewMem()
			now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
			aged := now.Add(-3 * 24 * time.Hour)
			var live []string
			for range tt.orphans {
				key := newKey(t)
				put(t, store, key, "text/plain", "o")
				store.Age(key, aged)
			}
			for range tt.live {
				key := newKey(t)
				put(t, store, key, "text/plain", "l")
				store.Age(key, aged)
				live = append(live, key)
			}
			// One live key always exists, so the no-live-key rule does not fire first;
			// it is young, so it is not among the aged objects.
			anchor := newKey(t)
			put(t, store, anchor, "text/plain", "a")
			live = append(live, anchor)
			cleanup := filestore.Cleanup{
				Client:  resource.NewMockClient(nil, nil, nil, resource.WithFileStore(store)),
				Store:   resource.DefaultStore,
				Holders: []resource.FileHolder{resource.ComputedFileHolder("Documents", resource.FileKey{Field: "StoreKey", Store: resource.DefaultStore})},
				ComputedKeys: func(context.Context, resource.FileHolder) ([]string, error) {
					return live, nil
				},
				Now: func() time.Time { return now },
			}
			report, err := cleanup.Run(t.Context())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Run() error = %v, want containing %q", err, tt.wantErr)
				}
				if got := len(store.Keys()); got != tt.orphans+tt.live+1 {
					t.Errorf("a refused run deleted objects: %d left", got)
				}

				return
			}
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := len(report.Orphans); got != tt.orphans {
				t.Errorf("Orphans = %d, want %d", got, tt.orphans)
			}
			kept := store.Keys()
			if len(kept) != tt.wantKept+1 {
				t.Errorf("kept %d objects, want %d live and the anchor", len(kept), tt.wantKept)
			}
			for _, key := range live {
				if !slices.Contains(kept, key) {
					t.Errorf("live key %s was deleted", key)
				}
			}
		})
	}
}
