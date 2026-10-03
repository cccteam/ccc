package resource

import (
	"context"
	"reflect"
	"slices"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"
)

// Deleting a row that carries a @file key, or pointing it at a new key, releases the
// old object: the patch machinery records the key the transaction lets go of, with the
// store it belongs to, and the transaction's executor deletes it from that store once
// the commit lands. The stores are handed to the database client at construction
// (WithFileStore, WithNamedFileStore), so every transaction the application runs gets
// the behavior: a generated frame, a patch applied on its own, application code calling
// ExecuteFunc. A body never writes the store inside a transaction, since a store delete
// cannot roll back; a transaction that does not commit releases nothing.
//
// Before the commit, the executor refuses a transaction that would release a key of a
// store the client holds no store for: the rows would go and the object would stay with
// nothing to delete it, so the function's error ends the transaction naming the store.
// The guard covers every process that runs transactions, the job process too, which
// builds no router.
//
// The deletes after the commit run detached from the request's cancellation
// (context.WithoutCancel) under a timeout of their own, and still synchronously before
// the response: a request a client abandoned must not leave objects behind, and the
// platform gives CPU only while a request is in flight.
//
// One row owns one object: the keys the upload frame mints are UUIDs, so a key is never
// shared by construction, and no claim check runs before the delete. Rows the database
// deletes by cascade never pass through the patch machinery, so their objects are the
// orphaned-file cleanup's (resource/filestore), as is an object left by a crash between
// the commit and the delete.

// fileDeleteTimeout bounds the deletes that run after a request's outcome is known: the
// release after a commit and the discard after a failure, each detached from the
// request's own deadline.
const fileDeleteTimeout = 30 * time.Second

// ReleasedKey is one object a transaction's patches let go of: the key, and the store
// it belongs to.
type ReleasedKey struct {
	Store StoreName
	Key   string
}

// releasedKeys is the record a write transaction keeps of the store objects its patches
// let go of, in recording order; ExecuteFunc holds the same record when the commit comes
// back and deletes what it names. Like the buffered-patches record, it belongs to one
// attempt: a retried transaction runs its function again over a fresh record.
type releasedKeys struct {
	keys []ReleasedKey
}

// newReleasedKeys returns an empty record.
func newReleasedKeys() *releasedKeys {
	return &releasedKeys{}
}

// record notes released keys of one store, skipping empty ones (a NULL or blank key
// column names no object) and ones already noted.
func (r *releasedKeys) record(store StoreName, keys ...string) {
	for _, key := range keys {
		if key == "" {
			continue
		}
		entry := ReleasedKey{Store: store, Key: key}
		if slices.Contains(r.keys, entry) {
			continue
		}
		r.keys = append(r.keys, entry)
	}
}

// list returns the recorded keys, in recording order.
func (r *releasedKeys) list() []ReleasedKey {
	return slices.Clone(r.keys)
}

// releaseRecorder is how the patch machinery reaches a transaction's record: the
// Spanner and Mock wrappers satisfy it; a transaction that does not (Postgres) records
// nothing and no key columns are read for it.
type releaseRecorder interface {
	recordReleased(store StoreName, keys ...string)
}

// refuseUnwiredRelease is the executor's check before the commit: every released key
// belongs to a store the client holds, or the transaction is refused naming the first
// store that is not wired, so no row goes while its object stays with nothing to delete
// it.
func refuseUnwiredRelease(stores *fileStores, released []ReleasedKey) error {
	for _, r := range released {
		if stores.get(r.Store) == nil {
			return errors.Newf("the transaction releases file object %s of %s, and no file store is wired for it; the commit is refused so the object is not left behind, wire the store on the resource client with %s", r.Key, r.Store, wiringOption(r.Store))
		}
	}

	return nil
}

// releaseFiles deletes the objects a committed transaction released, store by store. It
// runs after the commit, synchronously, so the request answers once the objects are
// gone, and detached from the request's cancellation under its own timeout, since the
// rows are gone whatever the client did. A store that fails to delete is logged naming
// the keys and nothing more: the answer is unchanged, and the objects are the
// orphaned-file cleanup's. A store the client does not hold is unreachable here, since
// the executor refused the commit; it is logged all the same.
func releaseFiles(ctx context.Context, stores *fileStores, released []ReleasedKey) {
	if len(released) == 0 {
		return
	}
	ctx, cancel := detachedContext(ctx)
	defer cancel()
	for store, keys := range keysByStore(released) {
		fs := stores.get(store)
		if fs == nil {
			logger.FromCtx(ctx).Errorf("resource: a committed transaction released file objects %v of %s, and the client holds no store to delete them from; they are left to the orphaned-file cleanup", keys, store)

			continue
		}
		if err := fs.Delete(ctx, keys); err != nil {
			logger.FromCtx(ctx).Errorf("resource: FileStore.Delete(%v) on %s failed after the transaction committed; the objects are left to the orphaned-file cleanup: %v", keys, store, err)
		}
	}
}

// keysByStore groups released keys by store, the stores in first-seen order.
func keysByStore(released []ReleasedKey) func(yield func(StoreName, []string) bool) {
	return func(yield func(StoreName, []string) bool) {
		var order []StoreName
		grouped := make(map[StoreName][]string)
		for _, r := range released {
			if _, seen := grouped[r.Store]; !seen {
				order = append(order, r.Store)
			}
			grouped[r.Store] = append(grouped[r.Store], r.Key)
		}
		for _, store := range order {
			if !yield(store, grouped[store]) {
				return
			}
		}
	}
}

// detachedContext is the context the deletes after a request's outcome run under: the
// request's values kept (the logger among them), its cancellation dropped, and a timeout
// of its own.
func detachedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), fileDeleteTimeout)
}

// recordReleasedOnDelete records the row's file keys as released before a delete is
// buffered: one point read on the primary key selecting the key columns alone, each
// non-NULL key noted with its store. A row that does not exist records nothing; the
// commit refuses the delete as it does today. A resource with no file keys, and a
// transaction with no record, read nothing.
func (p *PatchSet[Resource]) recordReleasedOnDelete(ctx context.Context, txn ReadWriteTransaction) error {
	recorder, ok := txn.(releaseRecorder)
	keys := p.querySet.rMeta.fileKeys
	if !ok || len(keys) == 0 {
		return nil
	}
	old, found, err := p.readFileKeys(ctx, txn, keys)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	for i, key := range keys {
		recorder.recordReleased(key.Store, old[i])
	}

	return nil
}

// recordReleasedOnWrite records, before an update or an insert-or-update is buffered,
// the old value of every file key the patch sets: when the row exists, the old value is
// non-NULL, and it differs from the new one (a new value of NULL included, the detach
// case), the old key is released. A patch that leaves the file keys alone reads nothing.
func (p *PatchSet[Resource]) recordReleasedOnWrite(ctx context.Context, txn ReadWriteTransaction) error {
	recorder, ok := txn.(releaseRecorder)
	if !ok {
		return nil
	}
	var keys []FileKey
	for _, key := range p.querySet.rMeta.fileKeys {
		if p.IsSet(key.Field) {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	old, found, err := p.readFileKeys(ctx, txn, keys)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	for i, key := range keys {
		newKey, err := fileKeyString(p.Get(key.Field))
		if err != nil {
			return errors.Wrapf(err, "field %s", key.Field)
		}
		if old[i] != "" && old[i] != newKey {
			recorder.recordReleased(key.Store, old[i])
		}
	}

	return nil
}

// readFileKeys reads the current values of the given file key fields for the patch's
// row, one point read on the primary key, on the transaction. A NULL key reads as the
// empty string. found is false when the row does not exist.
func (p *PatchSet[Resource]) readFileKeys(ctx context.Context, txn ReadWriteTransaction, keys []FileKey) (values []string, found bool, err error) {
	qSet := NewQuerySet(p.querySet.rMeta)
	for field, value := range p.querySet.KeySet().KeyMap() {
		qSet.SetKey(field, value)
	}
	fields := make([]accesstypes.Field, 0, len(keys))
	for _, key := range keys {
		fields = append(fields, key.Field)
		qSet.AddField(key.Field)
	}
	stmt, err := qSet.stmt(txn.DBType())
	if err != nil {
		return nil, false, errors.Wrap(err, "QuerySet.stmt()")
	}
	row, err := newReader[Resource](txn).Read(ctx, stmt)
	if err != nil {
		if httpio.HasNotFound(err) {
			return nil, false, nil
		}

		return nil, false, errors.Wrap(err, "Reader.Read()")
	}
	data := reflect.Indirect(reflect.ValueOf(row.Data))
	values = make([]string, 0, len(fields))
	for _, field := range fields {
		value := fieldValue(data, string(field))
		if !value.IsValid() {
			return nil, false, errors.Newf("file key field %s is not a field of %s", field, data.Type())
		}
		key, err := fileKeyString(value.Interface())
		if err != nil {
			return nil, false, errors.Wrapf(err, "field %s", field)
		}
		values = append(values, key)
	}

	return values, true, nil
}

// fileKeyString reads a file key column's value: a string, a Key[S], or a nullable
// one, whose NULL is the empty string. The generator admits no other type for a key
// column.
func fileKeyString(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case *string:
		if v == nil {
			return "", nil
		}

		return *v, nil
	default:
		rv := reflect.ValueOf(value)
		if rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return "", nil
			}
			rv = rv.Elem()
		}
		if rv.Kind() == reflect.String {
			return rv.String(), nil
		}

		return "", errors.Newf("a file key is a string, a resource.Key, or a nullable one, not %T", value)
	}
}
