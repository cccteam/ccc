package resource

import (
	"bytes"
	"context"
	"encoding"
	"encoding/json"
	"iter"
	"reflect"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/cccteam/spxscan"
	"github.com/go-playground/errors/v5"
)

type PatchType string

const (
	CreatePatchType PatchType = "CreatePatchType"
	UpdatePatchType PatchType = "UpdatePatchType"
	DeletePatchType PatchType = "DeletePatchType"
)

type PatchSet[Resource Resourcer] struct {
	querySet  *QuerySet[Resource]
	data      *fieldSet
	patchType PatchType
}

func NewPatchSet[Resource Resourcer](rMeta *ResourceMetadata[Resource]) *PatchSet[Resource] {
	return &PatchSet[Resource]{
		querySet: NewQuerySet(rMeta),
		data:     newFieldSet(),
	}
}

func (p *PatchSet[Resource]) SetPatchType(t PatchType) *PatchSet[Resource] {
	p.patchType = t

	return p
}

func (p *PatchSet[Resource]) PatchType() PatchType {
	return p.patchType
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
	for field := range p.querySet.rMeta.fieldMap {
		p.querySet.AddField(field)
	}

	return p.querySet
}

func (p *PatchSet[Resource]) Row() any {
	return p.querySet.Row()
}

func (p *PatchSet[Resource]) Resource() accesstypes.Resource {
	return p.querySet.Resource()
}

func (p *PatchSet[Resource]) SpannerPatch(ctx context.Context, spanner *spanner.Client, eventSource ...string) error {
	switch p.patchType {
	case CreatePatchType:
		return p.SpannerInsert(ctx, spanner, eventSource...)
	case UpdatePatchType:
		return p.SpannerUpdate(ctx, spanner, eventSource...)
	case DeletePatchType:
		return p.SpannerDelete(ctx, spanner, eventSource...)
	default:
		return errors.Newf("PatchType %s not supported", p.patchType)
	}
}

func (p *PatchSet[Resource]) SpannerBuffer(ctx context.Context, txn *spanner.ReadWriteTransaction, eventSource ...string) error {
	switch p.patchType {
	case CreatePatchType:
		return p.SpannerBufferInsert(txn, eventSource...)
	case UpdatePatchType:
		return p.SpannerBufferUpdate(ctx, txn, eventSource...)
	case DeletePatchType:
		return p.SpannerBufferDelete(ctx, txn, eventSource...)
	default:
		return errors.Newf("PatchType %s not supported", p.patchType)
	}
}

func (p *PatchSet[Resource]) SpannerInsert(ctx context.Context, s *spanner.Client, eventSource ...string) error {
	if _, err := s.ReadWriteTransaction(ctx, func(_ context.Context, txn *spanner.ReadWriteTransaction) error {
		if err := p.SpannerBufferInsert(txn, eventSource...); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return errors.Wrap(err, "spanner.Client.ReadWriteTransaction()")
	}

	return nil
}

func (p *PatchSet[Resource]) SpannerUpdate(ctx context.Context, s *spanner.Client, eventSource ...string) error {
	if _, err := s.ReadWriteTransaction(ctx, func(_ context.Context, txn *spanner.ReadWriteTransaction) error {
		if err := p.SpannerBufferUpdate(ctx, txn, eventSource...); err != nil {
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

func (p *PatchSet[Resource]) SpannerDelete(ctx context.Context, s *spanner.Client, eventSource ...string) error {
	if _, err := s.ReadWriteTransaction(ctx, func(_ context.Context, txn *spanner.ReadWriteTransaction) error {
		if err := p.SpannerBufferDelete(ctx, txn, eventSource...); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return errors.Wrap(err, "spanner.Client.ReadWriteTransaction()")
	}

	return nil
}

func (p *PatchSet[Resource]) SpannerBufferInsert(txn *spanner.ReadWriteTransaction, eventSource ...string) error {
	event, err := p.validateEventSource(eventSource)
	if err != nil {
		return err
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

func (p *PatchSet[Resource]) SpannerBufferUpdate(ctx context.Context, txn *spanner.ReadWriteTransaction, eventSource ...string) error {
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

func (p *PatchSet[Resource]) SpannerBufferInsertOrUpdate(ctx context.Context, txn *spanner.ReadWriteTransaction, eventSource ...string) error {
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

func (p *PatchSet[Resource]) SpannerBufferDelete(ctx context.Context, txn *spanner.ReadWriteTransaction, eventSource ...string) error {
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

func (p *PatchSet[Resource]) bufferInsertWithDataChangeEvent(txn *spanner.ReadWriteTransaction, eventSource string) error {
	jsonChangeSet, err := p.jsonInsertSet()
	if err != nil {
		return err
	}

	m, err := spanner.InsertStruct(p.querySet.rMeta.changeTrackingTable,
		&DataChangeEvent{
			TableName:   p.Resource(),
			RowID:       p.PrimaryKey().RowID(),
			EventTime:   spanner.CommitTimestamp,
			EventSource: eventSource,
			ChangeSet:   string(jsonChangeSet),
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

func (p *PatchSet[Resource]) bufferInsertOrUpdateWithDataChangeEvent(ctx context.Context, txn *spanner.ReadWriteTransaction, eventSource string) error {
	jsonChangeSet, err := p.jsonUpdateSet(ctx, txn)
	if err != nil {
		if !errors.Is(err, spxscan.ErrNotFound) {
			return err
		}
		jsonChangeSet, err = p.jsonInsertSet()
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
			ChangeSet:   string(jsonChangeSet),
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

func (p *PatchSet[Resource]) bufferUpdateWithDataChangeEvent(ctx context.Context, txn *spanner.ReadWriteTransaction, eventSource string) error {
	jsonChangeSet, err := p.jsonUpdateSet(ctx, txn)
	if err != nil {
		return err
	}

	m, err := spanner.InsertStruct(p.querySet.rMeta.changeTrackingTable,
		&DataChangeEvent{
			TableName:   p.Resource(),
			RowID:       p.PrimaryKey().RowID(),
			EventTime:   spanner.CommitTimestamp,
			EventSource: eventSource,
			ChangeSet:   string(jsonChangeSet),
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

func (p *PatchSet[Resource]) bufferDeleteWithDataChangeEvent(ctx context.Context, txn *spanner.ReadWriteTransaction, eventSource string) error {
	keySet := p.PrimaryKey()
	jsonChangeSet, err := p.jsonDeleteSet(ctx, txn)
	if err != nil {
		return err
	}

	m, err := spanner.InsertStruct(p.querySet.rMeta.changeTrackingTable,
		&DataChangeEvent{
			TableName:   p.Resource(),
			RowID:       keySet.RowID(),
			EventTime:   spanner.CommitTimestamp,
			EventSource: eventSource,
			ChangeSet:   string(jsonChangeSet),
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

func (p *PatchSet[Resource]) jsonInsertSet() ([]byte, error) {
	changeSet, err := p.Diff(p.Row())
	if err != nil {
		return nil, errors.Wrap(err, "Diff()")
	}

	jsonBytes, err := json.Marshal(changeSet)
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return jsonBytes, nil
}

func (p *PatchSet[Resource]) jsonUpdateSet(ctx context.Context, txn *spanner.ReadWriteTransaction) ([]byte, error) {
	stmt, err := p.querySet.SpannerStmt()
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.SpannerStmt()")
	}

	oldValues := p.Row()
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

	jsonBytes, err := json.Marshal(changeSet)
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return jsonBytes, nil
}

func (p *PatchSet[Resource]) jsonDeleteSet(ctx context.Context, txn *spanner.ReadWriteTransaction) ([]byte, error) {
	stmt, err := p.deleteQuerySet().SpannerStmt()
	if err != nil {
		return nil, errors.Wrap(err, "PatchSet.deleteQuerySet().SpannerStmt()")
	}

	oldValues := p.Row()
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

	jsonBytes, err := json.Marshal(changeSet)
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return jsonBytes, nil
}

func (p *PatchSet[Resource]) deleteChangeSet(old any) (map[string]DiffElem, error) {
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

	oldMap := map[string]DiffElem{}
	for _, field := range reflect.VisibleFields(oldType) {
		oldValue := oldValue.FieldByName(field.Name)
		if oldValue.IsValid() && !oldValue.IsZero() {
			oldMap[field.Name] = DiffElem{
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
