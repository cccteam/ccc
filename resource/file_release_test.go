package resource

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/cccteam/spxscan/spxapi"
	"github.com/google/go-cmp/cmp"
	"go.uber.org/mock/gomock"
)

// fileRow is a resource with two @file keys as the generator would declare them: one
// NOT NULL, one nullable.
type fileRow struct {
	ID       string  `spanner:"Id"       postgres:"Id"`
	Title    string  `spanner:"Title"    postgres:"Title"`
	StoreKey string  `spanner:"StoreKey" postgres:"StoreKey"`
	ThumbKey *string `spanner:"ThumbKey" postgres:"ThumbKey"`
}

func (fileRow) Resource() accesstypes.Resource {
	return "FileRows"
}

func (fileRow) FileKeys() []FileKey {
	return []FileKey{{Field: "StoreKey", Store: DefaultStore}, {Field: "ThumbKey", Store: DefaultStore}}
}

// docStore is a named store as an application declares one.
type docStore struct{ Store }

// typedFileRow is a resource with a typed @file key, as the generator declares one for
// a column typed resource.Key[docStore]: the key names the store docs.
type typedFileRow struct {
	ID     string        `spanner:"Id"     postgres:"Id"`
	DocKey Key[docStore] `spanner:"DocKey" postgres:"DocKey"`
}

func (typedFileRow) Resource() accesstypes.Resource {
	return "TypedFileRows"
}

func (typedFileRow) FileKeys() []FileKey {
	return []FileKey{{Field: "DocKey", Store: StoreNameFor[docStore]()}}
}

// trackedFileRow overrides Config, as an application turning change tracking on does;
// the generated FileKeys stands beside it, untouched by the override.
type trackedFileRow struct {
	ID       string `spanner:"Id"`
	StoreKey string `spanner:"StoreKey"`
}

func (trackedFileRow) Resource() accesstypes.Resource {
	return "TrackedFileRows"
}

func (trackedFileRow) Config() Config {
	return Config{}.SetChangeTrackingTable("Changes")
}

func (trackedFileRow) DefaultConfig() Config {
	return Config{}
}

func (trackedFileRow) FileKeys() []FileKey {
	return []FileKey{{Field: "StoreKey", Store: DefaultStore}}
}

// plainRow carries no file keys.
type plainRow struct {
	ID    string `spanner:"Id"`
	Title string `spanner:"Title"`
}

func (plainRow) Resource() accesstypes.Resource {
	return "PlainRows"
}

// bufferingTxn is the ReadWriteTransaction the Mock wrapper delegates its buffering to:
// it records what was buffered and stands in for Spanner.
type bufferingTxn struct {
	buffered []PatchSetMetadata
}

func (*bufferingTxn) DBType() DBType {
	return SpannerDBType
}

func (*bufferingTxn) SpannerReadOnlyTransaction() spxapi.Querier {
	return nil
}

func (*bufferingTxn) PostgresReadOnlyTransaction() PostgresQuerier {
	return nil
}

func (b *bufferingTxn) BufferMap(r PatchSetMetadata, _ map[string]any) error {
	b.buffered = append(b.buffered, r)

	return nil
}

func (b *bufferingTxn) BufferStruct(p PatchSetMetadata) error {
	b.buffered = append(b.buffered, p)

	return nil
}

func (*bufferingTxn) DataChangeEventIndex(accesstypes.Resource, string) int {
	return 0
}

// fakeStore records the deletes a client asks of it.
type fakeStore struct {
	deleted   [][]string
	deleteErr error
}

var _ FileStore = (*fakeStore)(nil)

func (*fakeStore) Put(context.Context, string, string, io.Reader) error {
	return nil
}

func (s *fakeStore) Delete(_ context.Context, keys []string) error {
	s.deleted = append(s.deleted, keys)

	return s.deleteErr
}

func (*fakeStore) Open(context.Context, string) (*Content, error) {
	return nil, ErrFileNotFound
}

func ptr[T any](v T) *T {
	return &v
}

func TestPatchSet_recordsReleasedFileKeys(t *testing.T) {
	t.Parallel()

	notFound := httpio.NewNotFoundMessage("FileRows (Id = row-1) not found")

	tests := []struct {
		name  string
		patch func() *PatchSet[fileRow]
		// row is what the point read answers, readErr its error; noRead expects none.
		row     *fileRow
		readErr error
		noRead  bool
		// wantSelected are the columns the point read selects, in order.
		wantSelected []string
		wantReleased []string
	}{
		{
			name: "a delete releases the row's key",
			patch: func() *PatchSet[fileRow] {
				p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(DeletePatchType)
				p.SetKey("ID", "row-1")

				return p
			},
			row:          &fileRow{StoreKey: "key-1"},
			wantSelected: []string{"StoreKey", "ThumbKey"},
			wantReleased: []string{"key-1"},
		},
		{
			name: "a delete releases both keys of a row with two files",
			patch: func() *PatchSet[fileRow] {
				p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(DeletePatchType)
				p.SetKey("ID", "row-1")

				return p
			},
			row:          &fileRow{StoreKey: "key-1", ThumbKey: ptr("thumb-1")},
			wantSelected: []string{"StoreKey", "ThumbKey"},
			wantReleased: []string{"key-1", "thumb-1"},
		},
		{
			name: "a NULL key and a blank key name no object",
			patch: func() *PatchSet[fileRow] {
				p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(DeletePatchType)
				p.SetKey("ID", "row-1")

				return p
			},
			row:          &fileRow{},
			wantSelected: []string{"StoreKey", "ThumbKey"},
		},
		{
			name: "a delete of a row that does not exist releases nothing",
			patch: func() *PatchSet[fileRow] {
				p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(DeletePatchType)
				p.SetKey("ID", "row-1")

				return p
			},
			readErr:      notFound,
			wantSelected: []string{"StoreKey", "ThumbKey"},
		},
		{
			name: "an update that points the row at another object releases the old one",
			patch: func() *PatchSet[fileRow] {
				p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(UpdatePatchType)
				p.SetKey("ID", "row-1")
				p.Set("StoreKey", "key-2")

				return p
			},
			row:          &fileRow{StoreKey: "key-1"},
			wantSelected: []string{"StoreKey"},
			wantReleased: []string{"key-1"},
		},
		{
			name: "an update that sets a nullable key to NULL releases the object it held",
			patch: func() *PatchSet[fileRow] {
				p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(UpdatePatchType)
				p.SetKey("ID", "row-1")
				p.Set("ThumbKey", (*string)(nil))

				return p
			},
			row:          &fileRow{StoreKey: "key-1", ThumbKey: ptr("thumb-1")},
			wantSelected: []string{"ThumbKey"},
			wantReleased: []string{"thumb-1"},
		},
		{
			name: "an update that writes the same key back releases nothing",
			patch: func() *PatchSet[fileRow] {
				p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(UpdatePatchType)
				p.SetKey("ID", "row-1")
				p.Set("StoreKey", "key-1")

				return p
			},
			row:          &fileRow{StoreKey: "key-1"},
			wantSelected: []string{"StoreKey"},
		},
		{
			name: "an update that leaves the keys alone reads nothing",
			patch: func() *PatchSet[fileRow] {
				p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(UpdatePatchType)
				p.SetKey("ID", "row-1")
				p.Set("Title", "renamed")

				return p
			},
			noRead: true,
		},
		{
			name: "an update of a row that does not exist releases nothing",
			patch: func() *PatchSet[fileRow] {
				p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(UpdatePatchType)
				p.SetKey("ID", "row-1")
				p.Set("StoreKey", "key-2")

				return p
			},
			readErr:      notFound,
			wantSelected: []string{"StoreKey"},
		},
		{
			name: "an insert-or-update over an existing row releases the key it replaces",
			patch: func() *PatchSet[fileRow] {
				p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(CreateOrUpdatePatchType)
				p.SetKey("ID", "row-1")
				p.Set("StoreKey", "key-2")

				return p
			},
			row:          &fileRow{StoreKey: "key-1"},
			wantSelected: []string{"StoreKey"},
			wantReleased: []string{"key-1"},
		},
		{
			name: "an insert-or-update that inserts releases nothing",
			patch: func() *PatchSet[fileRow] {
				p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(CreateOrUpdatePatchType)
				p.SetKey("ID", "row-1")
				p.Set("StoreKey", "key-2")

				return p
			},
			readErr:      notFound,
			wantSelected: []string{"StoreKey"},
		},
		{
			name: "an insert reads nothing and releases nothing",
			patch: func() *PatchSet[fileRow] {
				p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(CreatePatchType)
				p.SetKey("ID", "row-1")
				p.Set("StoreKey", "key-1")
				p.Set("Title", "new")

				return p
			},
			noRead: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			reader := NewMockReader[fileRow](ctrl)
			var selected string
			if !tt.noRead {
				reader.EXPECT().Read(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, stmt *Statement) (*Row[fileRow], error) {
					selected = stmt.SQL
					if tt.readErr != nil {
						return nil, tt.readErr
					}

					return &Row[fileRow]{Data: *tt.row}, nil
				})
			}
			store := &fakeStore{}
			buffering := &bufferingTxn{}
			client := NewMockClient(buffering, nil, []any{reader}, WithFileStore(store))

			var released []string
			if err := client.ExecuteFunc(t.Context(), func(ctx context.Context, txn ReadWriteTransaction) error {
				if err := tt.patch().Buffer(ctx, txn); err != nil {
					return err
				}
				mockTxn, ok := txn.(*MockReadWriteTransaction)
				if !ok {
					t.Fatalf("txn is %T, want *MockReadWriteTransaction", txn)
				}
				for _, r := range mockTxn.Released() {
					if r.Store != DefaultStore {
						t.Errorf("Released() names %s for %s, want the default store", r.Store, r.Key)
					}
					released = append(released, r.Key)
				}

				return nil
			}); err != nil {
				t.Fatalf("ExecuteFunc() error = %v", err)
			}

			if diff := cmp.Diff(tt.wantReleased, released, cmp.Transformer("nilToEmpty", func(s []string) []string {
				if s == nil {
					return []string{}
				}

				return s
			})); diff != "" {
				t.Errorf("Released() mismatch (-want +got):\n%s", diff)
			}
			if len(tt.wantReleased) == 0 {
				if len(store.deleted) != 0 {
					t.Errorf("store.Delete() called with %v, want no call", store.deleted)
				}
			} else if diff := cmp.Diff([][]string{tt.wantReleased}, store.deleted); diff != "" {
				t.Errorf("store deletes mismatch (-want +got):\n%s", diff)
			}
			if len(buffering.buffered) != 1 {
				t.Errorf("buffered %d patches, want the one", len(buffering.buffered))
			}
			for _, column := range tt.wantSelected {
				if !strings.Contains(selected, column) {
					t.Errorf("point read %q does not select %s", selected, column)
				}
			}
			if tt.wantSelected != nil && strings.Contains(selected, "Title") {
				t.Errorf("point read %q selects Title, want the key columns alone", selected)
			}
		})
	}
}

func TestMockClient_ExecuteFunc_releases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// fnErr is what the function returns after recording a key.
		fnErr error
		// withStore hands the client the default store; withDocs the docs store;
		// deleteErr is the stores' answer.
		withStore bool
		withDocs  bool
		deleteErr error
		// release is what the function records: a key per store.
		release     []ReleasedKey
		wantErr     bool
		wantErrText string
		wantDeleted [][]string
		wantDocs    [][]string
	}{
		{name: "a committed function's released keys are deleted from the store", withStore: true, release: []ReleasedKey{{DefaultStore, "key-1"}}, wantDeleted: [][]string{{"key-1"}}},
		{name: "a function that errors releases nothing", fnErr: errors.New("body failed"), withStore: true, release: []ReleasedKey{{DefaultStore, "key-1"}}, wantErr: true},
		{name: "a dry run releases nothing", fnErr: ErrDryRun, withStore: true, release: []ReleasedKey{{DefaultStore, "key-1"}}, wantErr: true},
		{name: "a store that fails to delete is logged and the call still succeeds", withStore: true, deleteErr: errors.New("store down"), release: []ReleasedKey{{DefaultStore, "key-1"}}, wantDeleted: [][]string{{"key-1"}}},
		{name: "a client with no store for the released key refuses the commit naming the store", release: []ReleasedKey{{DefaultStore, "key-1"}}, wantErr: true, wantErrText: "releases file object key-1 of the default store, and no file store is wired for it"},
		{name: "a named store's key is refused when only the default is wired", withStore: true, release: []ReleasedKey{{StoreNameFor[docStore](), "doc-1"}}, wantErr: true, wantErrText: "of store doc_store, and no file store is wired for it; the commit is refused so the object is not left behind, wire the store on the resource client with resource.WithNamedFileStore[DocStore]"},
		{name: "keys of two stores are deleted from each", withStore: true, withDocs: true, release: []ReleasedKey{{DefaultStore, "key-1"}, {StoreNameFor[docStore](), "doc-1"}, {DefaultStore, "key-2"}}, wantDeleted: [][]string{{"key-1", "key-2"}}, wantDocs: [][]string{{"doc-1"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeStore{deleteErr: tt.deleteErr}
			docs := &fakeStore{deleteErr: tt.deleteErr}
			var opts []ClientOption
			if tt.withStore {
				opts = append(opts, WithFileStore(store))
			}
			if tt.withDocs {
				opts = append(opts, WithNamedFileStore[docStore](docs))
			}
			client := NewMockClient(&bufferingTxn{}, nil, nil, opts...)

			err := client.ExecuteFunc(t.Context(), func(_ context.Context, txn ReadWriteTransaction) error {
				recorder, ok := txn.(releaseRecorder)
				if !ok {
					t.Fatalf("txn is %T, want a release recorder", txn)
				}
				for _, r := range tt.release {
					recorder.recordReleased(r.Store, r.Key)
				}

				return tt.fnErr
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("ExecuteFunc() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.fnErr != nil && !errors.Is(err, tt.fnErr) {
				t.Errorf("ExecuteFunc() error = %v, want it to wrap %v", err, tt.fnErr)
			}
			if tt.wantErrText != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrText)) {
				t.Errorf("ExecuteFunc() error = %v, want it to contain %q", err, tt.wantErrText)
			}
			if diff := cmp.Diff(tt.wantDeleted, store.deleted); diff != "" {
				t.Errorf("store deletes mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantDocs, docs.deleted); diff != "" {
				t.Errorf("docs store deletes mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReadWriteTransaction_Released_outsideExecutor(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	reader := NewMockReader[fileRow](ctrl)
	reader.EXPECT().Read(gomock.Any(), gomock.Any()).Return(&Row[fileRow]{Data: fileRow{StoreKey: "key-1", ThumbKey: ptr("thumb-1")}}, nil)
	txn := NewMockReadWriteTransaction(&bufferingTxn{}, reader)

	p := NewPatchSet(NewMetadata[fileRow]()).SetPatchType(DeletePatchType)
	p.SetKey("ID", "row-1")
	if err := p.Buffer(t.Context(), txn); err != nil {
		t.Fatalf("Buffer() error = %v", err)
	}
	if diff := cmp.Diff([]ReleasedKey{{DefaultStore, "key-1"}, {DefaultStore, "thumb-1"}}, txn.Released()); diff != "" {
		t.Errorf("Released() mismatch (-want +got):\n%s", diff)
	}
	// The record is a copy: the caller cannot alter what the wrapper holds.
	txn.Released()[0].Key = "changed"
	if got := txn.Released()[0].Key; got != "key-1" {
		t.Errorf("Released()[0] = %q after altering a returned slice, want key-1", got)
	}
}

func TestReleasedKeys_record(t *testing.T) {
	t.Parallel()

	docs := StoreNameFor[docStore]()

	tests := []struct {
		name string
		keys []ReleasedKey
		want []ReleasedKey
	}{
		{name: "an empty record lists nothing", want: []ReleasedKey{}},
		{name: "keys in recording order", keys: []ReleasedKey{{DefaultStore, "b"}, {DefaultStore, "a"}, {DefaultStore, "c"}}, want: []ReleasedKey{{DefaultStore, "b"}, {DefaultStore, "a"}, {DefaultStore, "c"}}},
		{name: "blank keys are skipped", keys: []ReleasedKey{{DefaultStore, ""}, {DefaultStore, "a"}, {docs, ""}}, want: []ReleasedKey{{DefaultStore, "a"}}},
		{name: "a key recorded twice is listed once", keys: []ReleasedKey{{DefaultStore, "a"}, {DefaultStore, "a"}, {DefaultStore, "b"}, {DefaultStore, "b"}}, want: []ReleasedKey{{DefaultStore, "a"}, {DefaultStore, "b"}}},
		{name: "the same key in two stores is two objects", keys: []ReleasedKey{{DefaultStore, "a"}, {docs, "a"}}, want: []ReleasedKey{{DefaultStore, "a"}, {docs, "a"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newReleasedKeys()
			for _, key := range tt.keys {
				r.record(key.Store, key.Key)
			}
			got := r.list()
			if got == nil {
				got = []ReleasedKey{}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("list() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMetadata_fileKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  []FileKey
		want []FileKey
	}{
		{name: "the generated FileKeys names the keys", got: NewMetadata[fileRow]().fileKeys, want: []FileKey{{Field: "StoreKey", Store: DefaultStore}, {Field: "ThumbKey", Store: DefaultStore}}},
		{name: "a typed key carries its store", got: NewMetadata[typedFileRow]().fileKeys, want: []FileKey{{Field: "DocKey", Store: "doc_store"}}},
		{name: "a Config override leaves the generated keys untouched", got: NewMetadata[trackedFileRow]().fileKeys, want: []FileKey{{Field: "StoreKey", Store: DefaultStore}}},
		{name: "a resource declaring no keys has none", got: NewMetadata[plainRow]().fileKeys},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, tt.got); diff != "" {
				t.Errorf("fileKeys mismatch (-want +got):\n%s", diff)
			}
		})
	}

	// The override's own configuration stands beside the keys.
	if got := NewMetadata[trackedFileRow]().changeTrackingTable; got != "Changes" {
		t.Errorf("changeTrackingTable = %q, want Changes", got)
	}
}

func Test_fileKeyString(t *testing.T) {
	t.Parallel()

	type named string

	tests := []struct {
		name    string
		value   any
		want    string
		wantErr bool
	}{
		{name: "nil", value: nil},
		{name: "a string", value: "key", want: "key"},
		{name: "a nullable string", value: ptr("key"), want: "key"},
		{name: "a NULL nullable string", value: (*string)(nil)},
		{name: "a named string", value: named("key"), want: "key"},
		{name: "a pointer to a named string", value: ptr(named("key")), want: "key"},
		{name: "not a string", value: 7, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := fileKeyString(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("fileKeyString(%v) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("fileKeyString(%v) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
