package resource

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

// These tests pin the typed stores' runtime: the name a store's type derives, the one
// wiring point with its two refusals, the start-up requirement the generated router
// calls, and the holders the orphaned-file cleanup reads keys through.

// Named store types in the shapes an application declares them.
type (
	clientFiles struct{ Store }
	pdfScans    struct{ Store }
	htmlPages   struct{ Store }
)

func Test_snakeCase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: "Documents", want: "documents"},
		{name: "ClientFiles", want: "client_files"},
		{name: "PDFScans", want: "pdf_scans"},
		{name: "HTMLPages", want: "html_pages"},
		{name: "Scans2", want: "scans2"},
		{name: "ID", want: "id"},
		{name: "documents", want: "documents"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := snakeCase(tt.name); got != tt.want {
				t.Errorf("snakeCase(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestStoreNameFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		got        StoreName
		want       StoreName
		wantString string
		wantOption string
	}{
		{name: "a one-word type", got: StoreNameFor[docStore](), want: "doc_store", wantString: "store doc_store", wantOption: "resource.WithNamedFileStore[DocStore]"},
		{name: "a two-word type", got: StoreNameFor[clientFiles](), want: "client_files", wantString: "store client_files", wantOption: "resource.WithNamedFileStore[ClientFiles]"},
		{name: "an acronym", got: StoreNameFor[pdfScans](), want: "pdf_scans", wantString: "store pdf_scans", wantOption: "resource.WithNamedFileStore[PdfScans]"},
		{name: "the default", got: DefaultStore, want: "", wantString: "the default store", wantOption: "resource.WithFileStore"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Errorf("StoreNameFor() = %q, want %q", tt.got, tt.want)
			}
			if got := tt.got.String(); got != tt.wantString {
				t.Errorf("String() = %q, want %q", got, tt.wantString)
			}
			if got := wiringOption(tt.got); got != tt.wantOption {
				t.Errorf("wiringOption() = %q, want %q", got, tt.wantOption)
			}
		})
	}
}

// locatedStore is a store that reports its location, as filestore's do.
type locatedStore struct {
	fakeStore
	location string
}

func (s *locatedStore) Location() string {
	return s.location
}

func Test_applyClientOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []ClientOption
		// wantNames are the stores wired afterwards; wantPanic the refusal's text.
		wantNames []StoreName
		wantPanic string
	}{
		{name: "no option wires nothing", wantNames: nil},
		{name: "the default alone", opts: []ClientOption{WithFileStore(&fakeStore{})}, wantNames: []StoreName{DefaultStore}},
		{name: "the default and two named stores", opts: []ClientOption{WithFileStore(&fakeStore{}), WithNamedFileStore[docStore](&fakeStore{}), WithNamedFileStore[clientFiles](&fakeStore{})}, wantNames: []StoreName{DefaultStore, "doc_store", "client_files"}},
		{name: "a named store without a default", opts: []ClientOption{WithNamedFileStore[docStore](&fakeStore{})}, wantNames: []StoreName{"doc_store"}},
		{name: "the default wired twice is refused", opts: []ClientOption{WithFileStore(&fakeStore{}), WithFileStore(&fakeStore{})}, wantPanic: "the default store is wired twice (resource.WithFileStore and resource.WithFileStore); each store is provided at most once"},
		{name: "a named store wired twice is refused", opts: []ClientOption{WithNamedFileStore[docStore](&fakeStore{}), WithNamedFileStore[docStore](&fakeStore{})}, wantPanic: "store doc_store is wired twice (resource.WithNamedFileStore[docStore] and resource.WithNamedFileStore[docStore])"},
		{name: "two stores on one location are refused", opts: []ClientOption{WithFileStore(&locatedStore{location: "file://uploads"}), WithNamedFileStore[docStore](&locatedStore{location: "file://uploads"})}, wantPanic: "the default store and store doc_store share one location, file://uploads; each store needs a location of its own"},
		{name: "two stores on two locations are fine", opts: []ClientOption{WithFileStore(&locatedStore{location: "file://uploads"}), WithNamedFileStore[docStore](&locatedStore{location: "file://uploads-documents"})}, wantNames: []StoreName{DefaultStore, "doc_store"}},
		{name: "a nil store is refused", opts: []ClientOption{WithFileStore(nil)}, wantPanic: "resource.WithFileStore wires a nil store for the default store"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				r := recover()
				if tt.wantPanic == "" {
					if r != nil {
						t.Fatalf("applyClientOptions() panicked: %v", r)
					}

					return
				}
				msg, _ := r.(string)
				if !strings.Contains(msg, tt.wantPanic) {
					t.Fatalf("applyClientOptions() panic = %v, want containing %q", r, tt.wantPanic)
				}
			}()
			stores := applyClientOptions(tt.opts)
			for _, name := range tt.wantNames {
				if stores.get(name) == nil {
					t.Errorf("store %q not wired", name)
				}
			}
			if got := len(stores.byName); got != len(tt.wantNames) {
				t.Errorf("%d stores wired, want %d", got, len(tt.wantNames))
			}
		})
	}
}

func TestRequireFileStores(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		client  Client
		names   []StoreName
		wantErr string
	}{
		{name: "nothing required passes", client: NewMockClient(&bufferingTxn{}, nil, nil), names: nil},
		{name: "a wired default passes", client: NewMockClient(&bufferingTxn{}, nil, nil, WithFileStore(&fakeStore{})), names: []StoreName{DefaultStore}},
		{name: "a wired named store passes", client: NewMockClient(&bufferingTxn{}, nil, nil, WithNamedFileStore[docStore](&fakeStore{})), names: []StoreName{StoreNameFor[docStore]()}},
		{name: "the default missing is refused naming the option", client: NewMockClient(&bufferingTxn{}, nil, nil), names: []StoreName{DefaultStore}, wantErr: "no file store is wired for the default store; wire it on the resource client with resource.WithFileStore"},
		{name: "a named store missing is refused naming the option", client: NewMockClient(&bufferingTxn{}, nil, nil, WithFileStore(&fakeStore{})), names: []StoreName{DefaultStore, StoreNameFor[clientFiles]()}, wantErr: "no file store is wired for store client_files; wire it on the resource client with resource.WithNamedFileStore[ClientFiles]"},
		{name: "a Postgres client with no store wired holds none", client: NewPostgresClient(nil), names: []StoreName{DefaultStore}, wantErr: "no file store is wired for the default store"},
		{name: "a Postgres client's wired default passes", client: NewPostgresClient(nil, WithFileStore(&fakeStore{})), names: []StoreName{DefaultStore}},
		{name: "no client is refused", client: nil, names: []StoreName{DefaultStore}, wantErr: "no resource client"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := RequireFileStores(tt.client, tt.names...)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("RequireFileStores() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("RequireFileStores() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestKey_asStoreBoundary pins what application code does with a typed key: converts
// it at the store boundary, and nowhere implicitly.
func TestKey_asStoreBoundary(t *testing.T) {
	t.Parallel()

	key := Key[docStore]("0193e2a7-522c-708f-bfd0-4adf33486bb1")
	store := newMemoryStore()
	if err := store.Put(t.Context(), string(key), "text/plain", io.NopCloser(strings.NewReader("x"))); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	content, err := store.Open(context.Background(), string(key))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer content.Body.Close()
	if _, ok := store.objects[string(key)]; !ok {
		t.Error("the object is not stored under the key's string")
	}
}

func TestFileHolder(t *testing.T) {
	t.Parallel()

	docs := StoreNameFor[docStore]()

	tests := []struct {
		name         string
		holder       FileHolder
		wantResource accesstypes.Resource
		wantKeys     []FileKey
		wantStores   []StoreName
		wantComputed bool
	}{
		{name: "a table resource with two default-store keys", holder: FileHolderOf[fileRow](), wantResource: "FileRows", wantKeys: []FileKey{{Field: "StoreKey"}, {Field: "ThumbKey"}}, wantStores: []StoreName{DefaultStore}},
		{name: "a table resource with a typed key", holder: FileHolderOf[typedFileRow](), wantResource: "TypedFileRows", wantKeys: []FileKey{{Field: "DocKey", Store: docs}}, wantStores: []StoreName{docs}},
		{name: "a resource declaring no keys holds nothing", holder: FileHolderOf[plainRow](), wantResource: "PlainRows"},
		{name: "a computed holder carries the flag and the keys it was given", holder: ComputedFileHolder("Manifests", FileKey{Field: "SheetKey", Store: docs}, FileKey{Field: "CoverKey"}), wantResource: "Manifests", wantKeys: []FileKey{{Field: "SheetKey", Store: docs}, {Field: "CoverKey"}}, wantStores: []StoreName{docs, DefaultStore}, wantComputed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.holder.Resource != tt.wantResource {
				t.Errorf("Resource = %q, want %q", tt.holder.Resource, tt.wantResource)
			}
			if diff := cmp.Diff(tt.wantKeys, tt.holder.Keys); diff != "" {
				t.Errorf("Keys mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantStores, tt.holder.Stores()); diff != "" {
				t.Errorf("Stores() mismatch (-want +got):\n%s", diff)
			}
			if tt.holder.Computed != tt.wantComputed {
				t.Errorf("Computed = %v, want %v", tt.holder.Computed, tt.wantComputed)
			}
			if tt.wantComputed {
				_, err := tt.holder.HeldKeys(t.Context(), NewMockClient(&bufferingTxn{}, nil, nil), docs)
				if err == nil || !strings.Contains(err.Error(), "is a computed resource: the framework cannot read its store doc_store keys, so the application supplies them") {
					t.Errorf("HeldKeys() of a computed holder error = %v, want the refusal", err)
				}
			}
		})
	}
}
