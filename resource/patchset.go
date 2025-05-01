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

type PatchType string

const (
	CreatePatchType PatchType = "CreatePatchType"
	UpdatePatchType PatchType = "UpdatePatchType"
	DeletePatchType PatchType = "DeletePatchType"
)

type PatchSet[Resource Resourcer] struct {
	querySet   *QuerySet[Resource]
	data       *fieldSet
	patchType  PatchType
	defaultFns map[accesstypes.Field]FieldDefaultFn
}

func NewPatchSet[Resource Resourcer](rMeta *ResourceMetadata[Resource]) *PatchSet[Resource] {
	var res Resource
	return &PatchSet[Resource]{
		querySet:   NewQuerySet(rMeta),
		data:       newFieldSet(),
		defaultFns: res.DefaultFns(),
	}
}

func (p *PatchSet[Resource]) SetPatchType(t PatchType) *PatchSet[Resource] {
	p.patchType = t

	return p
}

func (p *PatchSet[Resource]) PatchType() PatchType {
	return p.patchType
}

func (p *PatchSet[Resource]) EnableUserPermissionEnforcement(rSet *ResourceSet[Resource], userPermissions UserPermissions, requiredPermission accesstypes.Permission) *PatchSet[Resource] {
	p.querySet.EnableUserPermissionEnforcement(rSet, userPermissions, requiredPermission)

	return p
}

func (p *PatchSet[Resource]) checkPermissions(ctx context.Context) error {
	return p.querySet.checkPermissions(ctx)
}

func (p *PatchSet[Resource]) Set(field accesstypes.Field, value any) *PatchSet[Resource] {
	p.data.Set(field, value)
	p.querySet.AddField(field)

	return p
}

func (p *PatchSet[Resource]) Get(field accesstypes.Field) any {
	return p.data.Get(field)
}

func (p *PatchSet[Resource]) IsSet(field accesstypes.Field) bool {
	return p.data.IsSet(field)
}

func (p *PatchSet[Resource]) SetKey(field accesstypes.Field, value any) *PatchSet[Resource] {
	p.querySet.SetKey(field, value)

	return p
}

func (p *PatchSet[Resource]) Key(field accesstypes.Field) any {
	return p.querySet.Key(field)
}

func (p *PatchSet[Resource]) Fields() []accesstypes.Field {
	return p.querySet.Fields()
}

func (p *PatchSet[Resource]) Len() int {
	return p.querySet.Len()
}

func (p *PatchSet[Resource]) Data() map[accesstypes.Field]any {
	return p.data.data
}

func (p *PatchSet[Resource]) PrimaryKey() KeySet {
	return p.querySet.KeySet()
}

func (p *PatchSet[Resource]) HasKey() bool {
	return len(p.querySet.Fields()) > 0
}

func (p *PatchSet[Resource]) deleteQuerySet() *QuerySet[Resource] {
	for _, field := range p.querySet.rMeta.Fields() {
		p.querySet.AddField(field)
	}

	return p.querySet
}

func (p *PatchSet[Resource]) Resource() accesstypes.Resource {
	return p.querySet.Resource()
}

func (p *PatchSet[Resource]) SpannerApply(ctx context.Context, committer SpannerCommitter, eventSource ...string) error {
	switch p.patchType {
	case CreatePatchType:
		return p.spannerInsert(ctx, committer, eventSource...)
	case UpdatePatchType:
		return p.spannerUpdate(ctx, committer, eventSource...)
	case DeletePatchType:
		return p.spannerDelete(ctx, committer, eventSource...)
	default:
		return errors.Newf("PatchType %s not supported", p.patchType)
	}
}

func (p *PatchSet[Resource]) SpannerBuffer(ctx context.Context, txn TxnBuffer, eventSource ...string) error {
	switch p.patchType {
	case CreatePatchType:
		return p.spannerBufferInsert(ctx, txn, eventSource...)
	case UpdatePatchType:
		return p.spannerBufferUpdate(ctx, txn, eventSource...)
	case DeletePatchType:
		return p.spannerBufferDelete(ctx, txn, eventSource...)
	default:
		return errors.Newf("PatchType %s not supported", p.patchType)
	}
}

func (p *PatchSet[Resource]) spannerInsert(ctx context.Context, s SpannerCommitter, eventSource ...string) error {
	if _, err := s.ReadWriteTransaction(ctx, func(_ context.Context, txn *spanner.ReadWriteTransaction) error {
		if err := p.spannerBufferInsert(ctx, txn, eventSource...); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return errors.Wrap(err, "spanner.Client.ReadWriteTransaction()")
	}

	return nil
}

func (p *PatchSet[Resource]) spannerUpdate(ctx context.Context, s SpannerCommitter, eventSource ...string) error {
	if _, err := s.ReadWriteTransaction(ctx, func(_ context.Context, txn *spanner.ReadWriteTransaction) error {
		if err := p.spannerBufferUpdate(ctx, txn, eventSource...); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return errors.Wrap(err, "spanner.Client.ReadWriteTransaction()")
	}

	return nil
}

func (p *PatchSet[Resource]) SpannerInsertOrUpdate(ctx context.Context, s *spanner.Client, eventSource ...string) error {
	if _, err := s.ReadWriteTransaction(ctx, func(_ context.Context, txn *spanner.ReadWriteTransaction) error {
		if err := p.SpannerBufferInsertOrUpdate(ctx, txn, eventSource...); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return errors.Wrap(err, "spanner.Client.ReadWriteTransaction()")
	}

	return nil
}

func (p *PatchSet[Resource]) spannerDelete(ctx context.Context, s SpannerCommitter, eventSource ...string) error {
	if _, err := s.ReadWriteTransaction(ctx, func(_ context.Context, txn *spanner.ReadWriteTransaction) error {
		if err := p.spannerBufferDelete(ctx, txn, eventSource...); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return errors.Wrap(err, "spanner.Client.ReadWriteTransaction()")
	}

	return nil
}

func (p *PatchSet[Resource]) spannerBufferInsert(ctx context.Context, txn TxnBuffer, eventSource ...string) error {
	if err := p.checkPermissions(ctx); err != nil {
		return err
	}

	event, err := p.validateEventSource(eventSource)
	if err != nil {
		return err
	}

	for field, defaultFn := range p.defaultFns {
		if !p.IsSet(field) {
			d, err := defaultFn(ctx, txn)
			if err != nil {
				return errors.Wrap(err, "defaultFn()")
			}
			p.Set(field, d)
		}
	}

	patch, err := p.Resolve()
	if err != nil {
		return errors.Wrap(err, "Resolve()")
	}
	m := spanner.InsertMap(string(p.Resource()), patch)

	if err := txn.BufferWrite([]*spanner.Mutation{m}); err != nil {
		return errors.Wrap(err, "spanner.ReadWriteTransaction.BufferWrite()")
	}

	if p.querySet.rMeta.trackChanges {
		if err := p.bufferInsertWithDataChangeEvent(txn, event); err != nil {
			return err
		}
	}

	return nil
}

func (p *PatchSet[Resource]) spannerBufferUpdate(ctx context.Context, txn TxnBuffer, eventSource ...string) error {
	if err := p.checkPermissions(ctx); err != nil {
		return err
	}

	event, err := p.validateEventSource(eventSource)
	if err != nil {
		return err
	}

	patch, err := p.Resolve()
	if err != nil {
		return errors.Wrap(err, "Resolve()")
	}
	m := spanner.UpdateMap(string(p.Resource()), patch)

	if err := txn.BufferWrite([]*spanner.Mutation{m}); err != nil {
		return errors.Wrap(err, "spanner.ReadWriteTransaction.BufferWrite()")
	}

	if p.querySet.rMeta.trackChanges {
		if err := p.bufferUpdateWithDataChangeEvent(ctx, txn, event); err != nil {
			return err
		}
	}

	return nil
}

func (p *PatchSet[Resource]) SpannerBufferInsertOrUpdate(ctx context.Context, txn TxnBuffer, eventSource ...string) error {
	if err := p.checkPermissions(ctx); err != nil {
		return err
	}

	event, err := p.validateEventSource(eventSource)
	if err != nil {
		return err
	}

	patch, err := p.Resolve()
	if err != nil {
		return errors.Wrap(err, "Resolve()")
	}
	m := spanner.InsertOrUpdateMap(string(p.Resource()), patch)

	if err := txn.BufferWrite([]*spanner.Mutation{m}); err != nil {
		return errors.Wrap(err, "spanner.ReadWriteTransaction.BufferWrite()")
	}

	if p.querySet.rMeta.trackChanges {
		if err := p.bufferInsertOrUpdateWithDataChangeEvent(ctx, txn, event); err != nil {
			return err
		}
	}

	return nil
}

func (p *PatchSet[Resource]) spannerBufferDelete(ctx context.Context, txn TxnBuffer, eventSource ...string) error {
	if err := p.checkPermissions(ctx); err != nil {
		return err
	}

	event, err := p.validateEventSource(eventSource)
	if err != nil {
		return err
	}

	m := spanner.Delete(string(p.Resource()), p.PrimaryKey().KeySet())

	if err := txn.BufferWrite([]*spanner.Mutation{m}); err != nil {
		return errors.Wrap(err, "spanner.ReadWriteTransaction.BufferWrite()")
	}

	if p.querySet.rMeta.trackChanges {
		if err := p.bufferDeleteWithDataChangeEvent(ctx, txn, event); err != nil {
			return err
		}
	}

	return nil
}

func (p *PatchSet[Resource]) bufferInsertWithDataChangeEvent(txn TxnBuffer, eventSource string) error {
	changeSet, err := p.insertChangeSet()
	if err != nil {
		return err
	}

	m, err := spanner.InsertStruct(p.querySet.rMeta.changeTrackingTable,
		&DataChangeEvent{
			TableName:   p.Resource(),
			RowID:       p.PrimaryKey().RowID(),
			EventTime:   spanner.CommitTimestamp,
			EventSource: eventSource,
			ChangeSet:   spanner.NullJSON{Valid: true, Value: changeSet},
		},
	)
	if err != nil {
		return errors.Wrap(err, "spanner.InsertStruct()")
	}

	if err := txn.BufferWrite([]*spanner.Mutation{m}); err != nil {
		return errors.Wrap(err, "spanner.ReadWriteTransaction.BufferWrite()")
	}

	return nil
}

func (p *PatchSet[Resource]) bufferInsertOrUpdateWithDataChangeEvent(ctx context.Context, txn TxnBuffer, eventSource string) error {
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

	m, err := spanner.InsertOrUpdateStruct(p.querySet.rMeta.changeTrackingTable,
		&DataChangeEvent{
			TableName:   p.Resource(),
			RowID:       p.PrimaryKey().RowID(),
			EventTime:   spanner.CommitTimestamp,
			EventSource: eventSource,
			ChangeSet:   spanner.NullJSON{Valid: true, Value: changeSet},
		},
	)
	if err != nil {
		return errors.Wrap(err, "spanner.InsertStruct()")
	}

	if err := txn.BufferWrite([]*spanner.Mutation{m}); err != nil {
		return errors.Wrap(err, "spanner.ReadWriteTransaction.BufferWrite()")
	}

	return nil
}

func (p *PatchSet[Resource]) bufferUpdateWithDataChangeEvent(ctx context.Context, txn TxnBuffer, eventSource string) error {
	changeSet, err := p.updateChangeSet(ctx, txn)
	if err != nil {
		return err
	}

	m, err := spanner.InsertStruct(p.querySet.rMeta.changeTrackingTable,
		&DataChangeEvent{
			TableName:   p.Resource(),
			RowID:       p.PrimaryKey().RowID(),
			EventTime:   spanner.CommitTimestamp,
			EventSource: eventSource,
			ChangeSet:   spanner.NullJSON{Valid: true, Value: changeSet},
		},
	)
	if err != nil {
		return errors.Wrap(err, "spanner.InsertStruct()")
	}

	if err := txn.BufferWrite([]*spanner.Mutation{m}); err != nil {
		return errors.Wrap(err, "spanner.ReadWriteTransaction.BufferWrite()")
	}

	return nil
}

func (p *PatchSet[Resource]) bufferDeleteWithDataChangeEvent(ctx context.Context, txn TxnBuffer, eventSource string) error {
	keySet := p.PrimaryKey()
	changeSet, err := p.jsonDeleteSet(ctx, txn)
	if err != nil {
		return err
	}

	m, err := spanner.InsertStruct(p.querySet.rMeta.changeTrackingTable,
		&DataChangeEvent{
			TableName:   p.Resource(),
			RowID:       keySet.RowID(),
			EventTime:   spanner.CommitTimestamp,
			EventSource: eventSource,
			ChangeSet:   spanner.NullJSON{Valid: true, Value: changeSet},
		},
	)
	if err != nil {
		return errors.Wrap(err, "spanner.InsertStruct()")
	}

	if err := txn.BufferWrite([]*spanner.Mutation{m}); err != nil {
		return errors.Wrap(err, "spanner.ReadWriteTransaction.BufferWrite()")
	}

	return nil
}

func (p *PatchSet[Resource]) insertChangeSet() (map[accesstypes.Field]DiffElem, error) {
	// FIXME(jwatson): We need nil values, not the zero value of the type.
	changeSet, err := p.Diff(new(Resource))
	if err != nil {
		return nil, errors.Wrap(err, "Diff()")
	}

	return changeSet, nil
}

func (p *PatchSet[Resource]) updateChangeSet(ctx context.Context, txn TxnBuffer) (map[accesstypes.Field]DiffElem, error) {
	stmt, err := p.querySet.SpannerStmt()
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.SpannerStmt()")
	}

	oldValues := new(Resource)
	if err := spxscan.Get(ctx, txn, oldValues, stmt); err != nil {
		if errors.Is(err, spxscan.ErrNotFound) {
			return nil, httpio.NewNotFoundMessagef("%s (%s) not found", p.Resource(), p.PrimaryKey().String())
		}

		return nil, errors.Wrap(err, "spxscan.Get()")
	}

	changeSet, err := p.Diff(oldValues)
	if err != nil {
		return nil, errors.Wrap(err, "Diff()")
	}

	if len(changeSet) == 0 {
		return nil, httpio.NewBadRequestMessagef("No changes to apply for %s (%s)", p.Resource(), p.PrimaryKey().String())
	}

	return changeSet, nil
}

func (p *PatchSet[Resource]) jsonDeleteSet(ctx context.Context, txn TxnBuffer) (map[accesstypes.Field]DiffElem, error) {
	stmt, err := p.deleteQuerySet().SpannerStmt()
	if err != nil {
		return nil, errors.Wrap(err, "PatchSet.deleteQuerySet().SpannerStmt()")
	}

	oldValues := new(Resource)
	if err := spxscan.Get(ctx, txn, oldValues, stmt); err != nil {
		if errors.Is(err, spxscan.ErrNotFound) {
			return nil, httpio.NewNotFoundMessagef("%s (%s) not found", p.Resource(), p.PrimaryKey().String())
		}

		return nil, errors.Wrap(err, "spxscan.Get()")
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
func (p *PatchSet[Resource]) Resolve() (map[string]any, error) {
	keySet := p.PrimaryKey()
	if keySet.Len() == 0 {
		return nil, errors.New("PatchSet must include at least one primary key in call to Resolve")
	}

	newMap := make(map[string]any, p.Len()+keySet.Len())
	for structField, value := range all(p.Data(), keySet.KeyMap()) {
		c, ok := p.querySet.rMeta.fieldMap[structField]
		if !ok {
			return nil, errors.Newf("field %s not found in struct", structField)
		}
		newMap[c.tag] = value
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

type PatchSetComparer interface {
	Data() map[accesstypes.Field]any
	Fields() []accesstypes.Field
	PatchType() PatchType
	PrimaryKey() KeySet
}

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
