package resource

import (
	"context"
	"strings"
	"testing"

	"github.com/cccteam/httpio"
	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestPostgresClient_ExecuteFunc_releasesFiles pins the release over Postgres as
// TestSpannerClient_ExecuteFunc_releasesFiles pins it over the emulator: a committed
// delete or key replacement deletes the released objects from the store, a commit
// Postgres refuses releases nothing, a function that fails releases nothing, and a
// retried transaction releases what its committing attempt recorded, not its first.
func TestPostgresClient_ExecuteFunc_releasesFiles(t *testing.T) {
	t.Parallel()

	db, _ := postgresDatabase(t, "pg-file-release")
	ctx := t.Context()

	for _, row := range [][4]any{
		{"row-1", "one", "key-1", "thumb-1"},
		{"row-2", "two", "key-2", nil},
		{"row-3", "three", "key-3", nil},
		{"row-4", "four", "key-4", nil},
		{"row-5", "five", "key-5", nil},
	} {
		if _, err := db.Exec(ctx, `INSERT INTO "FileRows" ("Id", "Title", "StoreKey", "ThumbKey") VALUES ($1, $2, $3, $4)`, row[0], row[1], row[2], row[3]); err != nil {
			t.Fatalf("insert error = %v", err)
		}
	}
	// The third row is referenced.
	if _, err := db.Exec(ctx, `INSERT INTO "FileRefs" ("Id", "FileRowId") VALUES ('ref-1', 'row-3')`); err != nil {
		t.Fatalf("insert error = %v", err)
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
		// wantStatus is the 4xx a refused write answers.
		wantStatus  func(error) bool
		wantErr     bool
		wantDeleted [][]string
		wantGone    []string
		wantKept    []string
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
			name: "a delete the foreign key refuses releases nothing",
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
						// The first attempt records row-4's key, then loses to a
						// concurrent transaction: the client runs the function again
						// over a fresh record.
						if err := deleteRow("row-4")(ctx, txn); err != nil {
							return err
						}

						return &pgconn.PgError{Code: pgSerializationFailure}
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
			t.Parallel()

			store := &fakeStore{}
			client := NewPostgresClient(db.Pool, WithFileStore(store))

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
				if postgresFileRowExists(t, db.QueryRow, id) {
					t.Errorf("row %s still exists, want it deleted", id)
				}
			}
			for _, id := range tt.wantKept {
				if !postgresFileRowExists(t, db.QueryRow, id) {
					t.Errorf("row %s is gone, want it kept", id)
				}
			}
		})
	}
}

// postgresFileRowExists reports whether the row is in the table.
func postgresFileRowExists(t *testing.T, queryRow func(context.Context, string, ...any) pgx.Row, id string) bool {
	t.Helper()

	var exists bool
	if err := queryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM "FileRows" WHERE "Id" = $1)`, id).Scan(&exists); err != nil {
		t.Fatalf("exists(%s) error = %v", id, err)
	}

	return exists
}

// TestPostgresClient_typedKeys pins the typed key over Postgres: a resource.Key[S] column
// is written and read back through the client, its release names the store, and a client
// with no store for it refuses the write before the rows go.
func TestPostgresClient_typedKeys(t *testing.T) {
	t.Parallel()

	db, _ := postgresDatabase(t, "pg-typed-keys")
	ctx := t.Context()

	docs := &fakeStore{}
	wired := NewPostgresClient(db.Pool, WithFileStore(&fakeStore{}), WithNamedFileStore[docStore](docs))
	unwired := NewPostgresClient(db.Pool, WithFileStore(&fakeStore{}))

	createTyped := func(id string, key Key[docStore]) func(ctx context.Context, txn ReadWriteTransaction) error {
		return func(ctx context.Context, txn ReadWriteTransaction) error {
			p := NewPatchSet(NewMetadata[typedFileRow]()).SetPatchType(CreatePatchType)
			p.SetKey("ID", id)
			p.Set("DocKey", key)

			return p.Buffer(ctx, txn)
		}
	}
	for id, key := range map[string]Key[docStore]{"typed-1": "doc-1", "typed-2": "doc-2"} {
		if err := wired.ExecuteFunc(ctx, createTyped(id, key)); err != nil {
			t.Fatalf("ExecuteFunc(create %s) error = %v", id, err)
		}
	}

	qSet := NewQuerySet(NewMetadata[typedFileRow]())
	qSet.SetKey("ID", "typed-1")
	qSet.AddField("DocKey")
	txn := wired.ReadOnlyTransaction()
	row, err := qSet.Read(ctx, txn)
	txn.Close()
	if err != nil {
		t.Fatalf("QuerySet.Read() error = %v", err)
	}
	if row.Data.DocKey != "doc-1" {
		t.Errorf("DocKey read back = %q, want %q", row.Data.DocKey, "doc-1")
	}

	deleteTyped := func(ctx context.Context, txn ReadWriteTransaction) error {
		p := NewPatchSet(NewMetadata[typedFileRow]()).SetPatchType(DeletePatchType)
		p.SetKey("ID", "typed-2")

		return p.Buffer(ctx, txn)
	}
	err = unwired.ExecuteFunc(ctx, deleteTyped)
	if err == nil || !strings.Contains(err.Error(), "releases file object doc-2 of store doc_store, and no file store is wired for it") {
		t.Fatalf("ExecuteFunc(delete, unwired) error = %v, want the refusal naming the store", err)
	}
	var kept bool
	if err := db.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM "TypedFileRows" WHERE "Id" = 'typed-2')`).Scan(&kept); err != nil || !kept {
		t.Errorf("row typed-2 kept = %v, %v, want it kept by the refused delete", kept, err)
	}

	if err := wired.ExecuteFunc(ctx, deleteTyped); err != nil {
		t.Fatalf("ExecuteFunc(delete, wired) error = %v", err)
	}
	if diff := cmp.Diff([][]string{{"doc-2"}}, docs.deleted); diff != "" {
		t.Errorf("docs store deletes mismatch (-want +got):\n%s", diff)
	}
}
