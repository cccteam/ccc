package resource

import (
	"context"
	"slices"
	"strings"
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

// TestSpannerClient_typedKeys pins the typed key over the emulator: a resource.Key[S]
// column is written and read back through the client as a STRING, its release names
// the store, a client with no store for it refuses the commit before the rows go, and
// the holders the cleanup reads through list the keys each store's columns hold.
func TestSpannerClient_typedKeys(t *testing.T) {
	t.Parallel()

	container := spannerEmulator(t)
	ctx := context.Background()
	db, err := container.CreateDatabase(ctx, "typed-keys")
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
	if _, err := db.Apply(ctx, []*spanner.Mutation{
		spanner.InsertMap("FileRows", map[string]any{"Id": "row-1", "Title": "one", "StoreKey": "key-1", "ThumbKey": "thumb-1"}),
		spanner.InsertMap("FileRows", map[string]any{"Id": "row-2", "Title": "two", "StoreKey": "key-2"}),
	}); err != nil {
		t.Fatalf("spanner.Client.Apply() error = %v", err)
	}

	docs := &fakeStore{}
	wired := NewSpannerClient(db.Client, WithFileStore(&fakeStore{}), WithNamedFileStore[docStore](docs))
	unwired := NewSpannerClient(db.Client, WithFileStore(&fakeStore{}))
	docKey := Key[docStore]("doc-1")

	// The typed key is written through the patch machinery as the generated setter
	// writes it, and read back into the typed field.
	createTyped := func(id string, key Key[docStore]) func(ctx context.Context, txn ReadWriteTransaction) error {
		return func(ctx context.Context, txn ReadWriteTransaction) error {
			p := NewPatchSet(NewMetadata[typedFileRow]()).SetPatchType(CreatePatchType)
			p.SetKey("ID", id)
			p.Set("DocKey", key)

			return p.Buffer(ctx, txn)
		}
	}
	if err := wired.ExecuteFunc(ctx, createTyped("typed-1", docKey)); err != nil {
		t.Fatalf("ExecuteFunc(create typed-1) error = %v", err)
	}
	if err := wired.ExecuteFunc(ctx, createTyped("typed-2", "doc-2")); err != nil {
		t.Fatalf("ExecuteFunc(create typed-2) error = %v", err)
	}
	qSet := NewQuerySet(NewMetadata[typedFileRow]())
	qSet.SetKey("ID", "typed-1")
	qSet.AddField("DocKey")
	stmt, err := qSet.stmt(SpannerDBType)
	if err != nil {
		t.Fatalf("QuerySet.stmt() error = %v", err)
	}
	txn := wired.ReadOnlyTransaction()
	row, err := newReader[typedFileRow](txn).Read(ctx, stmt)
	txn.Close()
	if err != nil {
		t.Fatalf("Reader.Read() error = %v", err)
	}
	if row.Data.DocKey != docKey {
		t.Errorf("DocKey read back = %q, want %q", row.Data.DocKey, docKey)
	}

	// A client with no docs store refuses the delete before the commit, and the row stays.
	deleteTyped := func(ctx context.Context, txn ReadWriteTransaction) error {
		p := NewPatchSet(NewMetadata[typedFileRow]()).SetPatchType(DeletePatchType)
		p.SetKey("ID", "typed-2")

		return p.Buffer(ctx, txn)
	}
	err = unwired.ExecuteFunc(ctx, deleteTyped)
	if err == nil || !strings.Contains(err.Error(), "releases file object doc-2 of store doc_store, and no file store is wired for it") {
		t.Fatalf("ExecuteFunc(delete, unwired) error = %v, want the refusal naming the store", err)
	}
	if _, err := db.Client.Single().ReadRow(ctx, "TypedFileRows", spanner.Key{"typed-2"}, []string{"Id"}); err != nil {
		t.Errorf("the refused delete removed the row: %v", err)
	}

	// The wired client deletes the row and the docs store's object, and nothing else's.
	if err := wired.ExecuteFunc(ctx, deleteTyped); err != nil {
		t.Fatalf("ExecuteFunc(delete, wired) error = %v", err)
	}
	if diff := cmp.Diff([][]string{{"doc-2"}}, docs.deleted); diff != "" {
		t.Errorf("docs store deletes mismatch (-want +got):\n%s", diff)
	}
	if _, err := db.Client.Single().ReadRow(ctx, "TypedFileRows", spanner.Key{"typed-2"}, []string{"Id"}); spanner.ErrCode(err) != codes.NotFound {
		t.Errorf("the committed delete left the row: %v", err)
	}

	// The holders list what each store's columns hold once the rows have settled.
	tests := []struct {
		name   string
		holder FileHolder
		store  StoreName
		want   []string
	}{
		{name: "the default store's keys of FileRows, NULLs left out", holder: FileHolderOf[fileRow](), store: DefaultStore, want: []string{"key-1", "key-2", "thumb-1"}},
		{name: "FileRows holds no key of the docs store", holder: FileHolderOf[fileRow](), store: StoreNameFor[docStore](), want: nil},
		{name: "the docs store's keys of TypedFileRows, the deleted row's gone", holder: FileHolderOf[typedFileRow](), store: StoreNameFor[docStore](), want: []string{"doc-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.holder.HeldKeys(ctx, wired, tt.store)
			if err != nil {
				t.Fatalf("HeldKeys() error = %v", err)
			}
			slices.Sort(got)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("HeldKeys() mismatch (-want +got):\n%s", diff)
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
