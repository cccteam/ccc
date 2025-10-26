package resource

import (
	"bytes"
	"context"
	"encoding"
	"iter"
	"reflect"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/cccteam/spxscan"
	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

// PatchType defines the type of operation for a PatchSet.
type PatchType string

const (
	// CreatePatchType indicates an insert operation.
	CreatePatchType PatchType = "CreatePatchType"
	// UpdatePatchType indicates an update operation.
	UpdatePatchType PatchType = "UpdatePatchType"
	// CreateOrUpdatePatchType indicates an insert or update operation.
	CreateOrUpdatePatchType PatchType = "CreateOrUpdatePatchType"
	// DeletePatchType indicates a delete operation.
	DeletePatchType PatchType = "DeletePatchType"
)

var _ PatchSetMetadata = (*PatchSet[nilResource])(nil)

// PatchSet represents a set of changes to be applied to a resource.
type PatchSet[Resource Resourcer] struct {
	querySet           *QuerySet[Resource]
	data               *fieldSet
	patchType          PatchType
	defaultCreateFuncs map[accesstypes.Field]FieldDefaultFunc
	defaultUpdateFuncs map[accesstypes.Field]FieldDefaultFunc
	defaultsCreateFunc DefaultsFunc
	defaultsUpdateFunc DefaultsFunc
	validateCreateFunc ValidateFunc
	validateUpdateFunc ValidateFunc
}

// NewPatchSet creates a new, empty PatchSet for a given resource metadata.
func NewPatchSet[Resource Resourcer](rMeta *Metadata[Resource]) *PatchSet[Resource] {
	return &PatchSet[Resource]{
		querySet:           NewQuerySet(rMeta),
		data:               newFieldSet(),
		defaultCreateFuncs: make(map[accesstypes.Field]FieldDefaultFunc),
		defaultUpdateFuncs: make(map[accesstypes.Field]FieldDefaultFunc),
	}
}

// SetPatchType sets the type of the patch (Create, Update, or Delete).
func (p *PatchSet[Resource]) SetPatchType(t PatchType) *PatchSet[Resource] {
	p.patchType = t

	return p
}

// PatchType returns the type of the patch.
func (p *PatchSet[Resource]) PatchType() PatchType {
	return p.patchType
}

// EnableUserPermissionEnforcement enables the checking of user permissions for the PatchSet.
func (p *PatchSet[Resource]) EnableUserPermissionEnforcement(rSet *Set[Resource], userPermissions UserPermissions, requiredPermission accesstypes.Permission) *PatchSet[Resource] {
	p.querySet.EnableUserPermissionEnforcement(rSet, userPermissions, requiredPermission)

	return p
}

// Set adds or updates a field's value in the PatchSet.
func (p *PatchSet[Resource]) checkPermissions(ctx context.Context, dbType DBType) error {
	return p.querySet.checkPermissions(ctx, dbType)
}

// Set adds or updates a field's value in the PatchSet.
func (p *PatchSet[Resource]) Set(field accesstypes.Field, value any) *PatchSet[Resource] {
	p.data.Set(field, value)
	p.querySet.AddField(field)

	return p
}

// Get retrieves the value of a field from the PatchSet.
func (p *PatchSet[Resource]) Get(field accesstypes.Field) any {
	return p.data.Get(field)
}

// IsSet checks if a field has been set in the PatchSet.
func (p *PatchSet[Resource]) IsSet(field accesstypes.Field) bool {
	return p.data.IsSet(field)
}

// SetKey sets a primary key field and value for the PatchSet.
func (p *PatchSet[Resource]) SetKey(field accesstypes.Field, value any) *PatchSet[Resource] {
	p.querySet.SetKey(field, value)

	return p
}

// Key retrieves the value of a primary key field.
func (p *PatchSet[Resource]) Key(field accesstypes.Field) any {
	return p.querySet.Key(field)
}

// Fields returns a slice of all fields that have been set in the PatchSet.
func (p *PatchSet[Resource]) Fields() []accesstypes.Field {
	return p.querySet.Fields()
}

// Len returns the number of fields in the PatchSet.
func (p *PatchSet[Resource]) Len() int {
	return p.querySet.Len()
}

// Data returns the underlying map of field-value pairs.
func (p *PatchSet[Resource]) Data() map[accesstypes.Field]any {
	return p.data.data
}

// PrimaryKey returns the KeySet containing the primary key(s) for the resource.
func (p *PatchSet[Resource]) PrimaryKey() KeySet {
	return p.querySet.KeySet()
}

// HasKey checks if any primary key has been set.
func (p *PatchSet[Resource]) HasKey() bool {
	return len(p.querySet.Fields()) > 0
}

// deleteQuerySet configures the internal QuerySet to select all fields for a delete operation.
func (p *PatchSet[Resource]) deleteQuerySet(dbType DBType) *QuerySet[Resource] {
	for _, field := range p.querySet.rMeta.DBFields(dbType) {
		p.querySet.AddField(field)
	}

	return p.querySet
}

// Resource returns the name of the resource this PatchSet applies to.
func (p *PatchSet[Resource]) Resource() accesstypes.Resource {
	return p.querySet.Resource()
}

// Apply applies the patch within a new read-write transaction.
func (p *PatchSet[Resource]) Apply(ctx context.Context, client Client, eventSource ...string) error {
	switch p.patchType {
	case CreatePatchType:
		return p.applyInsert(ctx, client, eventSource...)
	case UpdatePatchType:
		return p.applyUpdate(ctx, client, eventSource...)
	case CreateOrUpdatePatchType:
		return p.applyInsertOrUpdate(ctx, client, eventSource...)
	case DeletePatchType:
		return p.applyDelete(ctx, client, eventSource...)
	default:
		return errors.Newf("PatchType %s not supported", p.patchType)
	}
}

// Buffer buffers the patch's mutations into an existing transaction buffer.
func (p *PatchSet[Resource]) Buffer(ctx context.Context, txn ReadWriteTransaction, eventSource ...string) error {
	switch p.patchType {
	case CreatePatchType:
		return p.bufferInsert(ctx, txn, eventSource...)
	case UpdatePatchType:
		return p.bufferUpdate(ctx, txn, eventSource...)
	case CreateOrUpdatePatchType:
		return p.bufferInsertOrUpdate(ctx, txn, eventSource...)
	case DeletePatchType:
		return p.bufferDelete(ctx, txn, eventSource...)
	default:
		return errors.Newf("PatchType %s not supported", p.patchType)
	}
}

func (p *PatchSet[Resource]) applyInsert(ctx context.Context, c Client, eventSource ...string) error {
	if err := c.ExecuteFunc(ctx, func(c context.Context, txn ReadWriteTransaction) error {
		if err := p.bufferInsert(c, txn, eventSource...); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return errors.Wrap(err, "spanner.Client.ReadWriteTransaction()")
	}

	return nil
}

func (p *PatchSet[Resource]) applyUpdate(ctx context.Context, c Client, eventSource ...string) error {
	if err := c.ExecuteFunc(ctx, func(c context.Context, txn ReadWriteTransaction) error {
		if err := p.bufferUpdate(c, txn, eventSource...); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return errors.Wrap(err, "spanner.Client.ReadWriteTransaction()")
	}

	return nil
}

// applyInsertOrUpdate applies an insert-or-update operation within a new read-write transaction.
func (p *PatchSet[Resource]) applyInsertOrUpdate(ctx context.Context, c Client, eventSource ...string) error {
	if err := c.ExecuteFunc(ctx, func(c context.Context, txn ReadWriteTransaction) error {
		if err := p.bufferInsertOrUpdate(c, txn, eventSource...); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return errors.Wrap(err, "spanner.Client.ReadWriteTransaction()")
	}

	return nil
}

func (p *PatchSet[Resource]) applyDelete(ctx context.Context, c Client, eventSource ...string) error {
	if err := c.ExecuteFunc(ctx, func(c context.Context, txn ReadWriteTransaction) error {
		if err := p.bufferDelete(c, txn, eventSource...); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return errors.Wrap(err, "spanner.Client.ReadWriteTransaction()")
	}

	return nil
}

func (p *PatchSet[Resource]) bufferInsert(ctx context.Context, txn ReadWriteTransaction, eventSource ...string) error {
	if err := p.checkPermissions(ctx, txn.DBType()); err != nil {
		return err
	}

	event, err := p.validateEventSource(eventSource)
	if err != nil {
		return err
	}

	for field, defaultFunc := range p.defaultCreateFuncs {
		if !p.IsSet(field) {
			d, err := defaultFunc(ctx, txn)
			if err != nil {
				return errors.Wrap(err, "defaultFunc()")
			}
			p.Set(field, d)
		}
	}

	if p.defaultsCreateFunc != nil {
		if err := p.defaultsCreateFunc(ctx, txn); err != nil {
			return errors.Wrap(err, "defaultsCreateFunc()")
		}
	}

	if p.validateCreateFunc != nil {
		if err := p.validateCreateFunc(ctx, txn); err != nil {
			return errors.Wrap(err, "validateCreateFunc()")
		}
	}

	patch, err := p.Resolve(txn.DBType())
	if err != nil {
		return errors.Wrap(err, "Resolve()")
	}

	if err := txn.BufferMap(p.PatchType(), p, patch); err != nil {
		return errors.Wrap(err, "ReadWriteTransaction.Buffer()")
	}

	if p.querySet.rMeta.trackChanges {
		if err := p.bufferInsertWithDataChangeEvent(txn, event); err != nil {
			return err
		}
	}

	return nil
}

func (p *PatchSet[Resource]) bufferUpdate(ctx context.Context, txn ReadWriteTransaction, eventSource ...string) error {
	if err := p.checkPermissions(ctx, txn.DBType()); err != nil {
		return err
	}

	event, err := p.validateEventSource(eventSource)
	if err != nil {
		return err
	}

	for field, defaultFunc := range p.defaultUpdateFuncs {
		if !p.IsSet(field) {
			d, err := defaultFunc(ctx, txn)
			if err != nil {
				return errors.Wrap(err, "defaultFunc()")
			}
			p.Set(field, d)
		}
	}

	if p.defaultsUpdateFunc != nil {
		if err := p.defaultsUpdateFunc(ctx, txn); err != nil {
			return errors.Wrap(err, "defaultsUpdateFunc()")
		}
	}

	if p.validateUpdateFunc != nil {
		if err := p.validateUpdateFunc(ctx, txn); err != nil {
			return errors.Wrap(err, "validateUpdateFunc()")
		}
	}

	patch, err := p.Resolve(txn.DBType())
	if err != nil {
		return errors.Wrap(err, "Resolve()")
	}

	if err := txn.BufferMap(p.PatchType(), p, patch); err != nil {
		return errors.Wrap(err, "ReadWriteTransaction.Buffer()")
	}

	if p.querySet.rMeta.trackChanges {
		if err := p.bufferUpdateWithDataChangeEvent(ctx, txn, event); err != nil {
			return err
		}
	}

	return nil
}

// bufferInsertOrUpdate buffers an insert-or-update mutation into an existing transaction buffer.
func (p *PatchSet[Resource]) bufferInsertOrUpdate(ctx context.Context, txn ReadWriteTransaction, eventSource ...string) error {
	if err := p.checkPermissions(ctx, txn.DBType()); err != nil {
		return err
	}

	event, err := p.validateEventSource(eventSource)
	if err != nil {
		return err
	}

	patch, err := p.Resolve(txn.DBType())
	if err != nil {
		return errors.Wrap(err, "Resolve()")
	}

	if err := txn.BufferMap(p.PatchType(), p, patch); err != nil {
		return errors.Wrap(err, "ReadWriteTransaction.Buffer()")
	}

	if p.querySet.rMeta.trackChanges {
		if err := p.bufferInsertOrUpdateWithDataChangeEvent(ctx, txn, event); err != nil {
			return err
		}
	}

	return nil
}

func (p *PatchSet[Resource]) bufferDelete(ctx context.Context, txn ReadWriteTransaction, eventSource ...string) error {
	if err := p.checkPermissions(ctx, txn.DBType()); err != nil {
		return err
	}

	event, err := p.validateEventSource(eventSource)
	if err != nil {
		return err
	}

	if err := txn.BufferMap(p.PatchType(), p, nil); err != nil {
		return errors.Wrap(err, "ReadWriteTransaction.Buffer()")
	}

	if p.querySet.rMeta.trackChanges {
		if err := p.bufferDeleteWithDataChangeEvent(ctx, txn, event); err != nil {
			return err
		}
	}

	return nil
}

func (p *PatchSet[Resource]) bufferInsertWithDataChangeEvent(txn ReadWriteTransaction, eventSource string) error {
	changeSet, err := p.insertChangeSet()
	if err != nil {
		return err
	}

	rowID := p.PrimaryKey().RowID()
	event := &DataChangeEvent{
		TableName:   p.Resource(),
		RowID:       rowID,
		Sequence:    txn.DataChangeEventIndex(p.Resource(), rowID),
		EventTime:   spanner.CommitTimestamp,
		EventSource: eventSource,
		ChangeSet:   spanner.NullJSON{Valid: true, Value: changeSet},
	}

	if err := txn.BufferStruct(CreatePatchType, event, event); err != nil {
		return errors.Wrap(err, "ReadWriteTransaction.Buffer()")
	}

	return nil
}

func (p *PatchSet[Resource]) bufferInsertOrUpdateWithDataChangeEvent(ctx context.Context, txn ReadWriteTransaction, eventSource string) error {
	changeSet, err := p.updateChangeSet(ctx, txn)
	if err != nil {
		if !errors.Is(err, spxscan.ErrNotFound) {
			return err
		}
		changeSet, err = p.insertChangeSet()
		if err != nil {
			return err
		}
	}

	rowID := p.PrimaryKey().RowID()
	event := &DataChangeEvent{
		TableName:   p.Resource(),
		RowID:       rowID,
		Sequence:    txn.DataChangeEventIndex(p.Resource(), rowID),
		EventTime:   spanner.CommitTimestamp,
		EventSource: eventSource,
		ChangeSet:   spanner.NullJSON{Valid: true, Value: changeSet},
	}
	if err != nil {
		return errors.Wrap(err, "spanner.InsertStruct()")
	}

	if err := txn.BufferStruct(CreatePatchType, event, event); err != nil {
		return errors.Wrap(err, "ReadWriteTransaction.BufferStruct()")
	}

	return nil
}

func (p *PatchSet[Resource]) bufferUpdateWithDataChangeEvent(ctx context.Context, txn ReadWriteTransaction, eventSource string) error {
	changeSet, err := p.updateChangeSet(ctx, txn)
	if err != nil {
		return err
	}

	rowID := p.PrimaryKey().RowID()
	event := &DataChangeEvent{
		TableName:   p.Resource(),
		RowID:       rowID,
		Sequence:    txn.DataChangeEventIndex(p.Resource(), rowID),
		EventTime:   spanner.CommitTimestamp,
		EventSource: eventSource,
		ChangeSet:   spanner.NullJSON{Valid: true, Value: changeSet},
	}

	if err := txn.BufferStruct(CreatePatchType, event, event); err != nil {
		return errors.Wrap(err, "ReadWriteTransaction.BufferStruct()")
	}

	return nil
}

func (p *PatchSet[Resource]) bufferDeleteWithDataChangeEvent(ctx context.Context, txn ReadWriteTransaction, eventSource string) error {
	keySet := p.PrimaryKey()
	changeSet, err := p.jsonDeleteSet(ctx, txn)
	if err != nil {
		return err
	}

	rowID := keySet.RowID()
	event := &DataChangeEvent{
		TableName:   p.Resource(),
		RowID:       rowID,
		Sequence:    txn.DataChangeEventIndex(p.Resource(), rowID),
		EventTime:   spanner.CommitTimestamp,
		EventSource: eventSource,
		ChangeSet:   spanner.NullJSON{Valid: true, Value: changeSet},
	}

	if err := txn.BufferStruct(CreatePatchType, event, event); err != nil {
		return errors.Wrap(err, "ReadWriteTransaction.BufferStruct()")
	}

	return nil
}

func (p *PatchSet[Resource]) insertChangeSet() (map[accesstypes.Field]DiffElem, error) {
	changeSet, err := p.Diff(new(Resource))
	if err != nil {
		return nil, errors.Wrap(err, "Diff()")
	}

	// Old values for inserts are always nil
	for k, v := range changeSet {
		v.Old = nil
		changeSet[k] = v
	}

	return changeSet, nil
}

func (p *PatchSet[Resource]) updateChangeSet(ctx context.Context, txn ReadWriteTransaction) (map[accesstypes.Field]DiffElem, error) {
	stmt, err := p.querySet.stmt(txn.DBType())
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.SpannerStmt()")
	}

	oldValues, err := NewReader[Resource](txn).Read(ctx, stmt)
	if err != nil {
		return nil, errors.Wrap(err, "Reader[Resource].Read()")
	}

	changeSet, err := p.Diff(oldValues)
	if err != nil {
		return nil, errors.Wrap(err, "Diff()")
	}

	if len(changeSet) == 0 {
		return nil, httpio.NewBadRequestMessagef("No changes to apply for %s (%s)", p.Resource(), stmt.resolvedWhereClause)
	}

	return changeSet, nil
}

func (p *PatchSet[Resource]) jsonDeleteSet(ctx context.Context, txn ReadWriteTransaction) (map[accesstypes.Field]DiffElem, error) {
	stmt, err := p.deleteQuerySet(txn.DBType()).stmt(txn.DBType())
	if err != nil {
		return nil, errors.Wrap(err, "PatchSet.deleteQuerySet().SpannerStmt()")
	}

	oldValues, err := NewReader[Resource](txn).Read(ctx, stmt)
	if err != nil {
		return nil, errors.Wrap(err, "Reader.Read()")
	}

	changeSet, err := p.deleteChangeSet(oldValues)
	if err != nil {
		return nil, errors.Wrap(err, "Diff()")
	}

	return changeSet, nil
}

func (p *PatchSet[Resource]) deleteChangeSet(old any) (map[accesstypes.Field]DiffElem, error) {
	oldValue := reflect.ValueOf(old)
	if oldValue.Kind() == reflect.Pointer {
		oldValue = oldValue.Elem()
	}

	oldType := reflect.TypeOf(old)
	if oldType.Kind() == reflect.Pointer {
		oldType = oldType.Elem()
	}

	if kind := oldType.Kind(); kind != reflect.Struct {
		return nil, errors.Newf("Patcher.Diff(): old must be of kind struct, found kind %s", kind.String())
	}

	oldMap := map[accesstypes.Field]DiffElem{}
	for _, field := range reflect.VisibleFields(oldType) {
		oldValue := oldValue.FieldByName(field.Name)
		if oldValue.IsValid() && !oldValue.IsZero() {
			oldMap[accesstypes.Field(field.Name)] = DiffElem{
				Old: oldValue.Interface(),
			}
		}
	}

	return oldMap, nil
}

// Resolve returns a map with the keys set to the database struct tags found on databaseType, and the values set to the values in patchSet.
func (p *PatchSet[Resource]) Resolve(dbType DBType) (map[string]any, error) {
	keySet := p.PrimaryKey()
	if keySet.Len() == 0 {
		return nil, errors.New("PatchSet must include at least one primary key in call to Resolve")
	}

	newMap := make(map[string]any, p.Len()+keySet.Len())
	for structField, value := range all(p.Data(), keySet.KeyMap()) {
		f, ok := p.querySet.rMeta.dbFieldMap(dbType)[structField]
		if !ok {
			return nil, errors.Newf("field %s not found in struct", structField)
		}
		newMap[f.ColumnName] = value
	}

	return newMap, nil
}

// Diff returns a map of fields that have changed between old and patchSet.
func (p *PatchSet[Resource]) Diff(old any) (map[accesstypes.Field]DiffElem, error) {
	oldValue := reflect.ValueOf(old)
	oldType := reflect.TypeOf(old)

	if oldValue.Kind() == reflect.Pointer {
		oldValue = oldValue.Elem()
	}

	if oldType.Kind() == reflect.Pointer {
		oldType = oldType.Elem()
	}

	if kind := oldType.Kind(); kind != reflect.Struct {
		return nil, errors.Newf("Patcher.Diff(): old must be of kind struct, found kind %s", kind.String())
	}

	oldMap := map[accesstypes.Field]any{}
	for _, field := range reflect.VisibleFields(oldType) {
		oldMap[accesstypes.Field(field.Name)] = oldValue.FieldByName(field.Name).Interface()
	}

	diff := map[accesstypes.Field]DiffElem{}
	for field, newV := range p.Data() {
		oldV, foundInOld := oldMap[field]
		if !foundInOld {
			return nil, errors.Newf("Patcher.Diff(): field %s in patchSet does not exist in old", field)
		}

		if match, err := match(oldV, newV); err != nil {
			return nil, err
		} else if !match {
			diff[field] = DiffElem{
				Old: oldV,
				New: newV,
			}
		}
	}

	return diff, nil
}

func (p *PatchSet[Resource]) validateEventSource(eventSource []string) (string, error) {
	if p.querySet.rMeta.trackChanges && len(eventSource) == 0 {
		return "", errors.New("eventSource must be supplied when trackChanges is enabled")
	}

	if len(eventSource) > 1 {
		return "", errors.New("eventSource can only be supplied once")
	}

	var event string
	if len(eventSource) > 0 {
		event = eventSource[0]
	}

	return event, nil
}

// RegisterDefaultCreateFunc registers a function to set a default value for a field during a create operation.
func (p *PatchSet[Resource]) RegisterDefaultCreateFunc(field accesstypes.Field, fn FieldDefaultFunc) {
	p.defaultCreateFuncs[field] = fn
}

// RegisterDefaultUpdateFunc registers a function to set a default value for a field during an update operation.
func (p *PatchSet[Resource]) RegisterDefaultUpdateFunc(field accesstypes.Field, fn FieldDefaultFunc) {
	p.defaultUpdateFuncs[field] = fn
}

// RegisterDefaultsCreateFunc registers a function that will be called on all
// Create Patches just before the patch is buffered to set necessary default values
func (p *PatchSet[Resource]) RegisterDefaultsCreateFunc(fn DefaultsFunc) {
	p.defaultsCreateFunc = fn
}

// RegisterDefaultsUpdateFunc registers a function that will be called on all
// Update Patches just before the patch is buffered to set necessary default values
func (p *PatchSet[Resource]) RegisterDefaultsUpdateFunc(fn DefaultsFunc) {
	p.defaultsUpdateFunc = fn
}

// RegisterValidateCreateFunc registers a function that will be called on all
// Create Patches just before the patch is buffered to validate the patch
func (p *PatchSet[Resource]) RegisterValidateCreateFunc(fn ValidateFunc) {
	p.validateCreateFunc = fn
}

// RegisterValidateUpdateFunc registers a function that will be called on all
// Update Patches just before the patch is buffered to validate the patch
func (p *PatchSet[Resource]) RegisterValidateUpdateFunc(fn ValidateFunc) {
	p.validateUpdateFunc = fn
}

// all returns an iterator over key-value pairs from m.
//   - all is a similar to maps.All but it takes a variadic
//   - duplicate keys will not be deduped and will be yielded once for each duplication
func all[Map ~map[K]V, K comparable, V any](mapSlice ...Map) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for _, m := range mapSlice {
			for k, v := range m {
				if !yield(k, v) {
					return
				}
			}
		}
	}
}

func match(v, v2 any) (matched bool, err error) {
	switch t := v.(type) {
	case int:
		return matchPrimitive(t, v2)
	case *int:
		return matchPrimitivePtr(t, v2)
	case []int:
		return matchSlice(t, v2)
	case []*int:
		return matchSlice(t, v2)
	case int8:
		return matchPrimitive(t, v2)
	case *int8:
		return matchPrimitivePtr(t, v2)
	case []int8:
		return matchSlice(t, v2)
	case []*int8:
		return matchSlice(t, v2)
	case int16:
		return matchPrimitive(t, v2)
	case *int16:
		return matchPrimitivePtr(t, v2)
	case []int16:
		return matchSlice(t, v2)
	case []*int16:
		return matchSlice(t, v2)
	case int32:
		return matchPrimitive(t, v2)
	case *int32:
		return matchPrimitivePtr(t, v2)
	case []int32:
		return matchSlice(t, v2)
	case []*int32:
		return matchSlice(t, v2)
	case int64:
		return matchPrimitive(t, v2)
	case *int64:
		return matchPrimitivePtr(t, v2)
	case []int64:
		return matchSlice(t, v2)
	case []*int64:
		return matchSlice(t, v2)
	case uint:
		return matchPrimitive(t, v2)
	case *uint:
		return matchPrimitivePtr(t, v2)
	case []uint:
		return matchSlice(t, v2)
	case []*uint:
		return matchSlice(t, v2)
	case uint8:
		return matchPrimitive(t, v2)
	case *uint8:
		return matchPrimitivePtr(t, v2)
	case []uint8:
		return matchSlice(t, v2)
	case []*uint8:
		return matchSlice(t, v2)
	case uint16:
		return matchPrimitive(t, v2)
	case *uint16:
		return matchPrimitivePtr(t, v2)
	case []uint16:
		return matchSlice(t, v2)
	case []*uint16:
		return matchSlice(t, v2)
	case uint32:
		return matchPrimitive(t, v2)
	case *uint32:
		return matchPrimitivePtr(t, v2)
	case []uint32:
		return matchSlice(t, v2)
	case []*uint32:
		return matchSlice(t, v2)
	case uint64:
		return matchPrimitive(t, v2)
	case *uint64:
		return matchPrimitivePtr(t, v2)
	case []uint64:
		return matchSlice(t, v2)
	case []*uint64:
		return matchSlice(t, v2)
	case float32:
		return matchPrimitive(t, v2)
	case *float32:
		return matchPrimitivePtr(t, v2)
	case []float32:
		return matchSlice(t, v2)
	case []*float32:
		return matchSlice(t, v2)
	case float64:
		return matchPrimitive(t, v2)
	case *float64:
		return matchPrimitivePtr(t, v2)
	case []float64:
		return matchSlice(t, v2)
	case []*float64:
		return matchSlice(t, v2)
	case string:
		return matchPrimitive(t, v2)
	case *string:
		return matchPrimitivePtr(t, v2)
	case []string:
		return matchSlice(t, v2)
	case []*string:
		return matchSlice(t, v2)
	case bool:
		return matchPrimitive(t, v2)
	case *bool:
		return matchPrimitivePtr(t, v2)
	case []bool:
		return matchSlice(t, v2)
	case []*bool:
		return matchSlice(t, v2)
	case time.Time:
		switch t2 := v2.(type) {
		case time.Time:
			return matchTextMarshaler(t, t2)
		default:
			return false, errors.Newf("match(): attempted to diff incomparable types, old: %T, new: %T", v, v2)
		}
	case *time.Time:
		switch t2 := v2.(type) {
		case *time.Time:
			return matchTextMarshalerPtr(t, t2)
		default:
			return false, errors.Newf("match(): attempted to diff incomparable types, old: %T, new: %T", v, v2)
		}
	case ccc.UUID:
		switch t2 := v2.(type) {
		case ccc.UUID:
			return matchTextMarshaler(t, t2)
		default:
			return false, errors.Newf("match(): attempted to diff incomparable types, old: %T, new: %T", v, v2)
		}
	case *ccc.UUID:
		switch t2 := v2.(type) {
		case *ccc.UUID:
			return matchTextMarshalerPtr(t, t2)
		default:
			return false, errors.Newf("match(): attempted to diff incomparable types, old: %T, new: %T", v, v2)
		}
	case ccc.NullUUID:
		switch t2 := v2.(type) {
		case ccc.NullUUID:
			return matchTextMarshaler(t, t2)
		default:
			return false, errors.Newf("match(): attempted to diff incomparable types, old: %T, new: %T", v, v2)
		}
	}

	if reflect.TypeOf(v) != reflect.TypeOf(v2) {
		return false, errors.Newf("attempted to compare values having a different type, v.(type) = %T, v2.(type) = %T", v, v2)
	}

	return reflect.DeepEqual(v, v2), nil
}

func matchSlice[T comparable](v []T, v2 any) (bool, error) {
	t2, ok := v2.([]T)
	if !ok {
		return false, errors.Newf("matchSlice(): attempted to diff incomparable types, old: %T, new: %T", v, v2)
	}
	if len(v) != len(t2) {
		return false, nil
	}

	for i := range v {
		if match, err := match(v[i], t2[i]); err != nil {
			return false, err
		} else if !match {
			return false, nil
		}
	}

	return true, nil
}

func matchPrimitive[T comparable](v T, v2 any) (bool, error) {
	t2, ok := v2.(T)
	if !ok {
		return false, errors.Newf("matchPrimitive(): attempted to diff incomparable types, old: %T, new: %T", v, v2)
	}
	if v == t2 {
		return true, nil
	}

	return false, nil
}

func matchPrimitivePtr[T comparable](v *T, v2 any) (bool, error) {
	t2, ok := v2.(*T)
	if !ok {
		return false, errors.Newf("matchPrimitivePtr(): attempted to diff incomparable types, old: %T, new: %T", v, v2)
	}
	if v == nil || t2 == nil {
		if v == nil && t2 == nil {
			return true, nil
		}

		return false, nil
	}
	if *v == *t2 {
		return true, nil
	}

	return false, nil
}

func matchTextMarshalerPtr[T encoding.TextMarshaler](v, v2 *T) (bool, error) {
	if v == nil || v2 == nil {
		if v == nil && v2 == nil {
			return true, nil
		}

		return false, nil
	}

	return matchTextMarshaler(*v, *v2)
}

func matchTextMarshaler[T encoding.TextMarshaler](v, v2 T) (bool, error) {
	vText, err := v.MarshalText()
	if err != nil {
		return false, errors.Wrap(err, "encoding.TextMarshaler.MarshalText()")
	}

	v2Text, err := v2.MarshalText()
	if err != nil {
		return false, errors.Wrap(err, "encoding.TextMarshaler.MarshalText()")
	}

	if bytes.Equal(vText, v2Text) {
		return true, nil
	}

	return false, nil
}

// PatchSetComparer is an interface for comparing two PatchSet-like objects.
type PatchSetComparer interface {
	Data() map[accesstypes.Field]any
	Fields() []accesstypes.Field
	PatchType() PatchType
	PrimaryKey() KeySet
}

// PatchsetCompare compares two PatchSetComparer objects for equality. It checks patch type, data, fields, and primary keys.
func PatchsetCompare(a, b PatchSetComparer) bool {
	if a.PatchType() != b.PatchType() {
		return false
	}

	if cmp.Diff(a.Data(), b.Data()) != "" {
		return false
	}

	if cmp.Diff(a.Fields(), b.Fields()) != "" {
		return false
	}

	if a.PatchType() == CreatePatchType {
		if cmp.Diff(a.PrimaryKey().keys(), b.PrimaryKey().keys()) != "" {
			return false
		}
	} else {
		if cmp.Diff(a.PrimaryKey(), b.PrimaryKey(), cmp.AllowUnexported(KeySet{})) != "" {
			return false
		}
	}

	return true
}
