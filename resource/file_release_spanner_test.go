package resource

import (
	"context"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/httpio"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestSpannerClient_ExecuteFunc_releasesFiles pins the release over the emulator: a
// committed delete or key replacement deletes the released objects from the store, a
// commit Spanner refuses releases nothing, a function that fails releases nothing, and a
// retried transaction releases what its committing attempt recorded, not its first.
func TestSpannerClient_ExecuteFunc_releasesFiles(t *testing.T) {
	t.Parallel()

	container := spannerEmulator(t)
	ctx := context.Background()
	db, err := container.CreateDatabase(ctx, "file-release")
	if err != nil {
		t.Fatalf("initiator.SpannerContainer.CreateDatabase() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.DropDatabase(context.Background()); err != nil {
			t.Error(err)
		}
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.MigrateUp("file://testdata/filerelease/schema"); err != nil {
		t.Fatalf("initiator.SpannerDB.MigrateUp() error = %v", err)
	}

	// The world: three rows, the third referenced by a FileRefs row.
	if _, err := db.Apply(ctx, []*spanner.Mutation{
		spanner.InsertMap("FileRows", map[string]any{"Id": "row-1", "Title": "one", "StoreKey": "key-1", "ThumbKey": "thumb-1"}),
		spanner.InsertMap("FileRows", map[string]any{"Id": "row-2", "Title": "two", "StoreKey": "key-2"}),
		spanner.InsertMap("FileRows", map[string]any{"Id": "row-3", "Title": "three", "StoreKey": "key-3"}),
		spanner.InsertMap("FileRows", map[string]any{"Id": "row-4", "Title": "four", "StoreKey": "key-4"}),
		spanner.InsertMap("FileRows", map[string]any{"Id": "row-5", "Title": "five", "StoreKey": "key-5"}),
		spanner.InsertMap("FileRefs", map[string]any{"Id": "ref-1", "FileRowId": "row-3"}),
	}); err != nil {
		t.Fatalf("spanner.Client.Apply() error = %v", err)
	}

	deleteRow := func(id string) func(ctx context.Context, txn ReadWriteTransaction) error {
		return func(ctx context.Context, txn ReadWriteTransaction) error {
			p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(DeletePatchType)
			p.SetKey("ID", id)

			return p.Buffer(ctx, txn)
		}
	}

	tests := []struct {
		name string
		fn   func(attempt int) func(ctx context.Context, txn ReadWriteTransaction) error
		// wantStatus is the 4xx a refused commit answers; 0 for success.
		wantStatus  func(error) bool
		wantErr     bool
		wantDeleted [][]string
		// wantRows are the FileRows ids left afterwards, checked among the ids the case touches.
		wantGone []string
		wantKept []string
	}{
		{
			name: "a committed delete releases both keys of the row",
			fn: func(int) func(ctx context.Context, txn ReadWriteTransaction) error {
				return deleteRow("row-1")
			},
			wantDeleted: [][]string{{"key-1", "thumb-1"}},
			wantGone:    []string{"row-1"},
		},
		{
			name: "a committed key replacement releases the old key",
			fn: func(int) func(ctx context.Context, txn ReadWriteTransaction) error {
				return func(ctx context.Context, txn ReadWriteTransaction) error {
					p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(UpdatePatchType)
					p.SetKey("ID", "row-2")
					p.Set("StoreKey", "key-2b")

					return p.Buffer(ctx, txn)
				}
			},
			wantDeleted: [][]string{{"key-2"}},
			wantKept:    []string{"row-2"},
		},
		{
			name: "a delete the foreign key refuses at commit releases nothing",
			fn: func(int) func(ctx context.Context, txn ReadWriteTransaction) error {
				return deleteRow("row-3")
			},
			wantErr:    true,
			wantStatus: httpio.HasConflict,
			wantKept:   []string{"row-3"},
		},
		{
			name: "a function that fails after recording releases nothing",
			fn: func(int) func(ctx context.Context, txn ReadWriteTransaction) error {
				return func(ctx context.Context, txn ReadWriteTransaction) error {
					if err := deleteRow("row-4")(ctx, txn); err != nil {
						return err
					}

					return ErrDryRun
				}
			},
			wantErr:  true,
			wantKept: []string{"row-4"},
		},
		{
			name: "a retried transaction releases what the committing attempt recorded",
			fn: func(attempt int) func(ctx context.Context, txn ReadWriteTransaction) error {
				return func(ctx context.Context, txn ReadWriteTransaction) error {
					if attempt == 1 {
						// The first attempt records row-4's key, then aborts: the client
						// runs the function again over a fresh record.
						if err := deleteRow("row-4")(ctx, txn); err != nil {
							return err
						}

						return status.Error(codes.Aborted, "forced abort")
					}

					return deleteRow("row-5")(ctx, txn)
				}
			},
			wantDeleted: [][]string{{"key-5"}},
			wantGone:    []string{"row-5"},
			wantKept:    []string{"row-4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each case owns its rows; the emulator aborts concurrent read-write
			// transactions and the client retries them, which the release survives.
			t.Parallel()

			store := &fakeStore{}
			client := NewSpannerClient(db.Client, WithFileStore(store))

			attempt := 0
			err := client.ExecuteFunc(ctx, func(ctx context.Context, txn ReadWriteTransaction) error {
				attempt++

				return tt.fn(attempt)(ctx, txn)
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("ExecuteFunc() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantStatus != nil && !tt.wantStatus(err) {
				t.Errorf("ExecuteFunc() error = %v, want the refusal's status", err)
			}
			if diff := cmp.Diff(tt.wantDeleted, store.deleted); diff != "" {
				t.Errorf("store deletes mismatch (-want +got):\n%s", diff)
			}
			for _, id := range tt.wantGone {
				if rowExists(t, db.Client, id) {
					t.Errorf("row %s still exists, want it deleted", id)
				}
			}
			for _, id := range tt.wantKept {
				if !rowExists(t, db.Client, id) {
					t.Errorf("row %s is gone, want it kept", id)
				}
			}
		})
	}
}

// rowExists reports whether FileRows holds the id.
func rowExists(t *testing.T, client *spanner.Client, id string) bool {
	t.Helper()

	_, err := client.Single().ReadRow(context.Background(), "FileRows", spanner.Key{id}, []string{"Id"})
	if err == nil {
		return true
	}
	if spanner.ErrCode(err) == codes.NotFound {
		return false
	}
	t.Fatalf("spanner.ReadOnlyTransaction.ReadRow(%s) error = %v", id, err)

	return false
}
