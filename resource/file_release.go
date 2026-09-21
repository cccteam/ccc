package resource

import (
	"context"
	"reflect"
	"slices"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"
)

// Deleting a row that carries a @file key, or pointing it at a new key, releases the
// old object: the patch machinery records the key the transaction lets go of, and the
// transaction's executor deletes it from the application's store once the commit
// lands. The store is handed to the database client at construction (WithFileStore),
// so every transaction the application runs gets the behavior: a generated frame, a
// patch applied on its own, application code calling ExecuteFunc. A body never writes
// the store inside a transaction, since a store delete cannot roll back; a transaction
// that does not commit releases nothing.
//
// One row owns one object: the keys the upload frame mints are UUIDs, so a key is never
// shared by construction, and no claim check runs before the delete. Rows the database
// deletes by cascade never pass through the patch machinery, so their objects are the
// application's sweep's, as is an object left by a crash between the commit and the
// delete.

// releasedKeys is the record a write transaction keeps of the store objects its patches
// let go of, in recording order; ExecuteFunc holds the same record when the commit comes
// back and deletes what it names. Like the buffered-patches record, it belongs to one
// attempt: a retried transaction runs its function again over a fresh record.
type releasedKeys struct {
	keys []string
}

// newReleasedKeys returns an empty record.
func newReleasedKeys() *releasedKeys {
	return &releasedKeys{}
}

// record notes released keys, skipping empty ones (a NULL or blank key column names no
// object) and ones already noted.
func (r *releasedKeys) record(keys ...string) {
	for _, key := range keys {
		if key == "" || slices.Contains(r.keys, key) {
			continue
		}
		r.keys = append(r.keys, key)
	}
}

// list returns the recorded keys, in recording order.
func (r *releasedKeys) list() []string {
	return slices.Clone(r.keys)
}

// releaseRecorder is how the patch machinery reaches a transaction's record: the
// Spanner and Mock wrappers satisfy it; a transaction that does not (Postgres) records
// nothing and no key columns are read for it.
type releaseRecorder interface {
	recordReleased(keys ...string)
}

// releaseFiles deletes the objects a committed transaction released. It runs after the
// commit, synchronously, so the request answers once the objects are gone. A store that
// fails to delete, or a client constructed with no store, is logged naming the keys and
// nothing more: the rows are gone and the answer is unchanged, and the objects are the
// application's sweep's.
func releaseFiles(ctx context.Context, store FileStore, released []string) {
	if len(released) == 0 {
		return
	}
	if store == nil {
		logger.FromCtx(ctx).Errorf("resource: a committed transaction released file objects %v, and the client holds no FileStore to delete them from (resource.WithFileStore); they are left to the application's sweep", released)

		return
	}
	if err := store.Delete(ctx, released); err != nil {
		logger.FromCtx(ctx).Errorf("resource: FileStore.Delete(%v) failed after the transaction committed; the objects are left to the application's sweep: %v", released, err)
	}
}

// ClientOption configures a database client at construction.
type ClientOption func(*clientOptions)

// clientOptions is what the options set.
type clientOptions struct {
	store FileStore
}

// WithFileStore hands the client the application's object store, so a transaction that
// deletes a row carrying a @file key, or points it at another object, has the old
// object deleted from the store once the commit lands. A client with no store logs the
// released keys instead and leaves the objects to the application's sweep.
func WithFileStore(store FileStore) ClientOption {
	return func(o *clientOptions) {
		o.store = store
	}
}

// applyClientOptions folds the options into their zero value.
func applyClientOptions(opts []ClientOption) clientOptions {
	var o clientOptions
	for _, opt := range opts {
		opt(&o)
	}

	return o
}

// recordReleasedOnDelete records the row's file keys as released before a delete is
// buffered: one point read on the primary key selecting the key columns alone, each
// non-NULL key noted. A row that does not exist records nothing; the commit refuses the
// delete as it does today. A resource with no file keys, and a transaction with no
// record, read nothing.
func (p *PatchSet[Resource]) recordReleasedOnDelete(ctx context.Context, txn ReadWriteTransaction) error {
	recorder, ok := txn.(releaseRecorder)
	fields := p.querySet.rMeta.fileKeys
	if !ok || len(fields) == 0 {
		return nil
	}
	old, found, err := p.readFileKeys(ctx, txn, fields)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	recorder.recordReleased(old...)

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
	var fields []accesstypes.Field
	for _, field := range p.querySet.rMeta.fileKeys {
		if p.IsSet(field) {
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	old, found, err := p.readFileKeys(ctx, txn, fields)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	for i, field := range fields {
		newKey, err := fileKeyString(p.Get(field))
		if err != nil {
			return errors.Wrapf(err, "field %s", field)
		}
		if old[i] != "" && old[i] != newKey {
			recorder.recordReleased(old[i])
		}
	}

	return nil
}

// readFileKeys reads the current values of the given file key fields for the patch's
// row, one point read on the primary key, on the transaction. A NULL key reads as the
// empty string. found is false when the row does not exist.
func (p *PatchSet[Resource]) readFileKeys(ctx context.Context, txn ReadWriteTransaction, fields []accesstypes.Field) (keys []string, found bool, err error) {
	qSet := NewQuerySet(p.querySet.rMeta)
	for field, value := range p.querySet.KeySet().KeyMap() {
		qSet.SetKey(field, value)
	}
	for _, field := range fields {
		qSet.AddField(field)
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
	keys = make([]string, 0, len(fields))
	for _, field := range fields {
		value := data.FieldByName(string(field))
		if !value.IsValid() {
			return nil, false, errors.Newf("file key field %s is not a field of %s", field, data.Type())
		}
		key, err := fileKeyString(value.Interface())
		if err != nil {
			return nil, false, errors.Wrapf(err, "field %s", field)
		}
		keys = append(keys, key)
	}

	return keys, true, nil
}

// fileKeyString reads a file key column's value: a string, or a nullable string whose
// NULL is the empty string. The generator admits no other type for a key column.
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

		return "", errors.Newf("a file key is a string or a nullable string, not %T", value)
	}
}
