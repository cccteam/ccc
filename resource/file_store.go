package resource

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// An application keeps its files in one or more stores. The default store has no type:
// a @file column typed string, an @upload with no store argument, and resource.Files
// with string keys all name it, and it is wired with WithFileStore. A named store is a
// Go type the application declares in one line, embedding Store, and everything that
// uses it says so by that type: a column typed Key[Documents], an @upload(store:
// Documents) whose Execute takes FilesIn[Documents], and the client wired with
// WithNamedFileStore[Documents]. A key of one store cannot be written into another
// store's column, because the generated setter takes exactly that column's type; the
// mismatch does not compile. The store's name is derived from the type name, so there is
// no second place to spell it: Documents is the store documents, and renaming the store
// is renaming the type.
//
// The resource client is the one wiring point. Each store is provided at most once, and
// two stores on one location are refused where the framework can see the location
// (LocatedFileStore), since the orphaned-file cleanup of one would delete the other's
// files. The default is optional: the generated router requires it at start only when
// the package has an untyped @file column or an unnamed @upload, and an application may
// wire one nothing declared uses and drive it itself.

// Store is embedded by a named file store's type:
//
//	// Documents holds mission documents in their own bucket.
//	type Documents struct{ resource.Store }
//
// Only a type embedding it satisfies NamedStore, so Key[int] does not compile.
type Store struct{}

// namedFileStore is the unexported method that seals NamedStore to types embedding
// Store.
func (Store) namedFileStore() {
	// The method's presence is the whole point; it is never called.
}

// NamedStore is the constraint a named store's type satisfies: a struct embedding
// Store. It is the type parameter of Key, FilesIn, FileIn, StoreNameFor and
// WithNamedFileStore.
type NamedStore interface {
	namedFileStore()
}

// Key is a stored file's key in the named store S: what a @file column typed Key[S]
// holds, what FileIn[S].Key carries, and what the generated setter of that column takes.
// The generator maps it to a STRING column. Copying an existing key, or recording one
// the application made, takes an explicit conversion, visible in review; code that opens
// an object itself converts with string(key) at the store boundary, since FileStore is
// untyped.
type Key[S NamedStore] string

// StoreName names a wired file store: DefaultStore for the default, and the name
// StoreNameFor derives from a named store's type.
type StoreName string

// DefaultStore is the default store's name: the store an untyped @file column, an
// unnamed @upload and resource.Files belong to.
const DefaultStore StoreName = ""

// StoreNameFor derives a named store's name from its type's name, lower-cased with
// underscores between its words: Documents gives documents, ClientFiles gives
// client_files. The configuration variable that holds the store's URL is
// APP_FILE_STORE_<NAME> upper-cased (APP_FILE_STORE_DOCUMENTS), and the infrastructure
// names its bucket with the suffix files-<name> hyphenated (files-documents).
func StoreNameFor[S NamedStore]() StoreName {
	return StoreName(snakeCase(reflect.TypeFor[S]().Name()))
}

// String spells the name as refusals and logs name the store: "the default store", or
// "store documents".
func (n StoreName) String() string {
	if n == DefaultStore {
		return "the default store"
	}

	return "store " + string(n)
}

// snakeCase lowers a Go type name to its words joined by underscores, an acronym kept
// as one word: Documents gives documents, ClientFiles client_files, PDFScans pdf_scans.
func snakeCase(name string) string {
	runes := []rune(name)
	var b strings.Builder
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prevLower := unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1])
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if prevLower || (unicode.IsUpper(runes[i-1]) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}

	return b.String()
}

// FileKey is one @file key column of a resource and the store its keys name: what the
// generated FileKeys method declares, one per field-scope @file, so the release after a
// commit and the orphaned-file cleanup know which store each key belongs to.
type FileKey struct {
	Field accesstypes.Field
	Store StoreName
}

// LocatedFileStore is a FileStore that knows where it keeps its objects, as the
// framework's stores (resource/filestore) do: the URL it was opened from. The client
// refuses two stores on one location at construction.
type LocatedFileStore interface {
	FileStore
	Location() string
}

// ClientOption configures a database client at construction.
type ClientOption func(*clientOptions)

// clientOptions is what the options set.
type clientOptions struct {
	stores []wiredStore
}

// wiredStore is one store as an option wired it, with the option that did, for the
// refusal that names it.
type wiredStore struct {
	name   StoreName
	store  FileStore
	option string
}

// WithFileStore wires the default store: the one an untyped @file column, an unnamed
// @upload and resource.Files belong to. The upload frames stream into it, the file routes
// open from it, and a transaction that deletes a row carrying a default-store key, or
// points it at another object, has the old object deleted from it once the commit
// lands. Wired twice, the client panics at construction naming the store.
func WithFileStore(store FileStore) ClientOption {
	return func(o *clientOptions) {
		o.stores = append(o.stores, wiredStore{name: DefaultStore, store: store, option: withFileStoreOption})
	}
}

// WithNamedFileStore wires the named store S: the one a Key[S] column, an @upload(store:
// S) and FilesIn[S] belong to. Any number of named stores of different types can be
// wired; the same type wired twice panics at construction naming the store.
func WithNamedFileStore[S NamedStore](store FileStore) ClientOption {
	return func(o *clientOptions) {
		o.stores = append(o.stores, wiredStore{name: StoreNameFor[S](), store: store, option: "resource.WithNamedFileStore[" + reflect.TypeFor[S]().Name() + "]"})
	}
}

// applyClientOptions folds the options into the client's stores, refusing a store
// wired twice and two stores on one location. Each refusal is a panic: the constructors
// return no error and are used in struct literals, and a duplicate is a programming
// error found the first time the application starts, the treatment Go gives a duplicate
// HTTP route.
func applyClientOptions(opts []ClientOption) *fileStores {
	var o clientOptions
	for _, opt := range opts {
		opt(&o)
	}
	stores := newFileStores()
	for _, w := range o.stores {
		if w.store == nil {
			panic(fmt.Sprintf("resource: %s wires a nil store for %s", w.option, w.name))
		}
		if prior, dup := stores.byName[w.name]; dup {
			panic(fmt.Sprintf("resource: %s is wired twice (%s and %s); each store is provided at most once", w.name, prior.option, w.option))
		}
		if located, ok := w.store.(LocatedFileStore); ok {
			if other, shared := stores.byLocation[located.Location()]; shared {
				panic(fmt.Sprintf("resource: %s and %s share one location, %s; each store needs a location of its own, or the orphaned-file cleanup of one would delete the other's files", other.name, w.name, located.Location()))
			}
			stores.byLocation[located.Location()] = w
		}
		stores.byName[w.name] = w
	}

	return stores
}

// fileStores is what a client holds: its wired stores by name, and by location where a
// store reports one.
type fileStores struct {
	byName     map[StoreName]wiredStore
	byLocation map[string]wiredStore
}

func newFileStores() *fileStores {
	return &fileStores{byName: make(map[StoreName]wiredStore), byLocation: make(map[string]wiredStore)}
}

// get returns the store wired under name, nil when none is.
func (s *fileStores) get(name StoreName) FileStore {
	if s == nil {
		return nil
	}
	if w, ok := s.byName[name]; ok {
		return w.store
	}

	return nil
}

// RequireFileStores reports which of the named stores the client holds no store for:
// the generated router calls it at start with every store the package uses, and refuses
// to start naming the first store that is not wired. The message names the store and
// the option that wires it, never a configuration variable, since where the URL comes
// from is the application's configuration's convention.
func RequireFileStores(client Client, names ...StoreName) error {
	if client == nil {
		return errors.New("resource.RequireFileStores: no resource client")
	}
	for _, name := range names {
		if client.FileStore(name) == nil {
			return errors.Newf("no file store is wired for %s; wire it on the resource client with %s", name, wiringOption(name))
		}
	}

	return nil
}

// withFileStoreOption spells the default store's option in refusals.
const withFileStoreOption = "resource.WithFileStore"

// wiringOption names the option that wires a store, for refusals.
func wiringOption(name StoreName) string {
	if name == DefaultStore {
		return withFileStoreOption
	}

	return "resource.WithNamedFileStore[" + storeTypeHint(name) + "]"
}

// storeTypeHint spells a store's type name back from its name as well as it can be
// spelled (documents gives Documents, client_files gives ClientFiles), for a refusal
// that names the option.
func storeTypeHint(name StoreName) string {
	var b strings.Builder
	for word := range strings.SplitSeq(string(name), "_") {
		if word == "" {
			continue
		}
		b.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}

	return b.String()
}

// FileHolder is a resource that records stored files' keys: the @file key columns, each
// with its store, and how the orphaned-file cleanup reads the keys they hold. The
// generator writes one per resource with a stored @file into the resources package's
// FileHolders(), and an application's job process hands every package's list to the
// cleanup.
type FileHolder struct {
	// Resource is the resource holding the keys.
	Resource accesstypes.Resource
	// Keys are the key columns, each with the store its keys name.
	Keys []FileKey
	// Computed marks a computed resource: its rows come from application code and there
	// is no table to read, so the cleanup needs the application to supply its keys, or
	// it refuses.
	Computed bool
	// read reads one key column of every row, nil for a computed holder.
	read func(ctx context.Context, client Client, field accesstypes.Field) ([]string, error)
}

// FileHolderOf declares a table resource as a holder of stored files, its key columns
// read from the generated FileKeys. The generated FileHolders calls it.
func FileHolderOf[R Resourcer]() FileHolder {
	var res R
	holder := FileHolder{Resource: res.Resource()}
	if keyer, ok := any(res).(fileKeyer); ok {
		holder.Keys = keyer.FileKeys()
	}
	holder.read = func(ctx context.Context, client Client, field accesstypes.Field) ([]string, error) {
		return readHeldKeys[R](ctx, client, field)
	}

	return holder
}

// ComputedFileHolder declares a computed resource with a stored @file as a holder whose
// keys the application supplies. The generated FileHolders calls it.
func ComputedFileHolder(res accesstypes.Resource, keys ...FileKey) FileHolder {
	return FileHolder{Resource: res, Keys: keys, Computed: true}
}

// Stores lists the distinct stores the holder's key columns name, in column order.
func (h FileHolder) Stores() []StoreName {
	var stores []StoreName
	for _, key := range h.Keys {
		if !slices.Contains(stores, key.Store) {
			stores = append(stores, key.Store)
		}
	}

	return stores
}

// HeldKeys reads every key the holder's columns of the given store hold, each column
// read once, through a strong read on the client, NULL and empty keys left out. It
// returns an error for a computed holder, whose keys the application supplies.
func (h FileHolder) HeldKeys(ctx context.Context, client Client, store StoreName) ([]string, error) {
	if h.Computed {
		return nil, errors.Newf("%s is a computed resource: the framework cannot read its %s keys, so the application supplies them", h.Resource, store)
	}
	if h.read == nil {
		return nil, errors.Newf("%s: the holder has no reader; declare it with resource.FileHolderOf", h.Resource)
	}
	var held []string
	for _, key := range h.Keys {
		if key.Store != store {
			continue
		}
		keys, err := h.read(ctx, client, key.Field)
		if err != nil {
			return nil, errors.Wrapf(err, "%s.%s", h.Resource, key.Field)
		}
		held = append(held, keys...)
	}

	return held, nil
}

// readHeldKeys reads one key column of every row of R: a strong read through the
// client's read-only transaction, selecting the column alone.
func readHeldKeys[R Resourcer](ctx context.Context, client Client, field accesstypes.Field) ([]string, error) {
	qSet := NewQuerySet(NewMetadata[R]())
	qSet.AddField(field)
	stmt, err := qSet.stmt(client.DBType())
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.stmt()")
	}
	txn := client.ReadOnlyTransaction()
	defer txn.Close()

	var keys []string
	for row, err := range newReader[R](txn).List(ctx, stmt) {
		if err != nil {
			return nil, errors.Wrap(err, "Reader.List()")
		}
		data := reflect.Indirect(reflect.ValueOf(row.Data))
		value := fieldValue(data, string(field))
		if !value.IsValid() {
			return nil, errors.Newf("file key field %s is not a field of %s", field, data.Type())
		}
		key, err := fileKeyString(value.Interface())
		if err != nil {
			return nil, errors.Wrapf(err, "field %s", field)
		}
		if key != "" {
			keys = append(keys, key)
		}
	}

	return keys, nil
}
