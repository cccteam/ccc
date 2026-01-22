package resource

import (
	"bytes"
	"testing"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/securehash"
	"github.com/google/go-cmp/cmp"
)

type resourcer struct{}

func (r resourcer) Resource() accesstypes.Resource {
	return accesstypes.Resource("resourcer")
}

func TestNewPatchSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want *PatchSet[nilResource]
	}{
		{
			name: "New",
			want: &PatchSet[nilResource]{
				querySet: NewQuerySet(NewMetadata[nilResource]()),
				data:     newFieldSet(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NewPatchSet(NewMetadata[nilResource]())
			if diff := PatchSetDiff()(tt.want, got); diff != "" {
				t.Errorf("NewPatchSet() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPatchSet_Set(t *testing.T) {
	t.Parallel()

	type args struct {
		field accesstypes.Field
		value any
	}
	tests := []struct {
		name string
		args []args
		want *PatchSet[nilResource]
	}{
		{
			name: "Set",
			args: []args{
				{
					field: "field1",
					value: "value1",
				},
				{
					field: "field2",
					value: "value2",
				},
			},
			want: &PatchSet[nilResource]{
				querySet: NewQuerySet(NewMetadata[nilResource]()).AddField("field1").AddField("field2"),
				data: &fieldSet{
					data: map[accesstypes.Field]any{
						"field1": "value1",
						"field2": "value2",
					},
					fields: []accesstypes.Field{
						"field1",
						"field2",
					},
				},
			},
		},
		{
			name: "Set with ordering",
			args: []args{
				{
					field: "field2",
					value: "value2",
				},
				{
					field: "field1",
					value: "value1",
				},
			},
			want: &PatchSet[nilResource]{
				querySet: NewQuerySet(NewMetadata[nilResource]()).AddField("field2").AddField("field1"),
				data: &fieldSet{
					data: map[accesstypes.Field]any{
						"field1": "value1",
						"field2": "value2",
					},
					fields: []accesstypes.Field{
						"field2",
						"field1",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &PatchSet[nilResource]{
				querySet: NewQuerySet(NewMetadata[nilResource]()),
				data:     newFieldSet(),
			}
			for _, i := range tt.args {
				p.Set(i.field, i.value)
			}
			got := p
			if diff := PatchSetDiff()(tt.want, got); diff != "" {
				t.Errorf("PatchSet.Set() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPatchSet_Get(t *testing.T) {
	t.Parallel()

	type fields struct {
		data *fieldSet
	}
	type args struct {
		field accesstypes.Field
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   any
	}{
		{
			name: "Get",
			fields: fields{
				data: &fieldSet{
					data: map[accesstypes.Field]any{
						"field1": "value1",
					},
				},
			},
			args: args{
				field: "field1",
			},
			want: "value1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &PatchSet[nilResource]{
				data: tt.fields.data,
			}
			got := p.Get(tt.args.field)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("PatchSet.Get() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPatchSet_SetKey(t *testing.T) {
	t.Parallel()

	type args struct {
		field accesstypes.Field
		value any
	}
	tests := []struct {
		name string
		args []args
		want *PatchSet[nilResource]
	}{
		{
			name: "SetKey",
			args: []args{
				{
					field: "field1",
					value: "value1",
				},
				{
					field: "field2",
					value: "value2",
				},
			},
			want: &PatchSet[nilResource]{
				querySet: &QuerySet[nilResource]{
					keys: &fieldSet{
						data: map[accesstypes.Field]any{
							"field1": "value1",
							"field2": "value2",
						},
						fields: []accesstypes.Field{"field1", "field2"},
					},
					rMeta: NewMetadata[nilResource](),
				},
				data: newFieldSet(),
			},
		},
		{
			name: "SetKey with ordering",
			args: []args{
				{
					field: "field2",
					value: "value2",
				},
				{
					field: "field1",
					value: "value1",
				},
			},
			want: &PatchSet[nilResource]{
				querySet: &QuerySet[nilResource]{
					keys: &fieldSet{
						data: map[accesstypes.Field]any{
							"field1": "value1",
							"field2": "value2",
						},
						fields: []accesstypes.Field{"field2", "field1"},
					},
					rMeta: NewMetadata[nilResource](),
				},
				data: newFieldSet(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &PatchSet[nilResource]{
				querySet: NewQuerySet(NewMetadata[nilResource]()),
				data:     newFieldSet(),
			}
			for _, i := range tt.args {
				p.SetKey(i.field, i.value)
			}
			got := p
			if diff := PatchSetDiff()(tt.want, got); diff != "" {
				t.Errorf("PatchSet.SetKey () mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPatchSet_Fields(t *testing.T) {
	t.Parallel()

	type fields struct {
		data    map[accesstypes.Field]any
		pkey    map[accesstypes.Field]any
		dFields []accesstypes.Field
	}
	tests := []struct {
		name   string
		fields fields
		want   []accesstypes.Field
	}{
		{
			name: "Fields",
			fields: fields{
				dFields: []accesstypes.Field{
					"field1",
					"field2",
				},
			},
			want: []accesstypes.Field{
				"field1",
				"field2",
			},
		},
		{
			name: "Fields with ordering",
			fields: fields{
				dFields: []accesstypes.Field{
					"field2",
					"field1",
				},
			},
			want: []accesstypes.Field{
				"field2",
				"field1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &PatchSet[nilResource]{
				querySet: &QuerySet[nilResource]{
					keys: &fieldSet{
						data: tt.fields.pkey,
					},
					fields: tt.fields.dFields,
				},
				data: &fieldSet{
					data:   tt.fields.data,
					fields: tt.fields.dFields,
				},
			}
			got := p.Fields()
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("PatchSet.Fields () mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPatchSet_Len(t *testing.T) {
	t.Parallel()

	type fields struct {
		data   map[accesstypes.Field]any
		fields []accesstypes.Field
		pkey   map[accesstypes.Field]any
	}
	tests := []struct {
		name   string
		fields fields
		want   int
	}{
		{
			name: "Len",
			fields: fields{
				data: map[accesstypes.Field]any{
					"field1": "value1",
					"field2": "value2",
				},
				fields: []accesstypes.Field{
					"field1",
					"field2",
				},
			},
			want: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &PatchSet[nilResource]{
				querySet: &QuerySet[nilResource]{
					keys: &fieldSet{
						data:   tt.fields.pkey,
						fields: tt.fields.fields,
					},
					fields: tt.fields.fields,
				},
				data: &fieldSet{
					data: tt.fields.data,
				},
			}
			if got := p.Len(); got != tt.want {
				t.Errorf("PatchSet.Len() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPatchSet_Data(t *testing.T) {
	t.Parallel()

	type fields struct {
		data map[accesstypes.Field]any
		pkey map[accesstypes.Field]any
	}
	tests := []struct {
		name   string
		fields fields
		want   map[accesstypes.Field]any
	}{
		{
			name: "Data",
			fields: fields{
				data: map[accesstypes.Field]any{
					"field1": "value1",
					"field2": "value2",
				},
			},
			want: map[accesstypes.Field]any{
				"field1": "value1",
				"field2": "value2",
			},
		},
		{
			name: "Data with keys",
			fields: fields{
				data: map[accesstypes.Field]any{
					"field1": "value1",
					"field2": "value2",
				},
				pkey: map[accesstypes.Field]any{
					"field3": "value1",
				},
			},
			want: map[accesstypes.Field]any{
				"field1": "value1",
				"field2": "value2",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &PatchSet[nilResource]{
				querySet: &QuerySet[nilResource]{
					keys: &fieldSet{
						data: tt.fields.pkey,
					},
				},
				data: &fieldSet{
					data: tt.fields.data,
				},
			}
			got := p.Data()
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("PatchSet.Data () mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPatchSet_PrimaryKey(t *testing.T) {
	t.Parallel()

	type fields struct {
		data   map[accesstypes.Field]any
		pkey   map[accesstypes.Field]any
		fields []accesstypes.Field
	}
	tests := []struct {
		name   string
		fields fields
		want   KeySet
	}{
		{
			name: "PrimaryKey",
			fields: fields{
				pkey: map[accesstypes.Field]any{
					"field1": "value1",
					"field2": "value2",
				},
				fields: []accesstypes.Field{
					"field1",
					"field2",
				},
			},
			want: KeySet{
				keyParts: []KeyPart{
					{Key: "field1", Value: "value1"},
					{Key: "field2", Value: "value2"},
				},
			},
		},
		{
			name: "PrimaryKey with ordering",
			fields: fields{
				data: map[accesstypes.Field]any{
					"field3": "value1",
				},
				pkey: map[accesstypes.Field]any{
					"field1": "value1",
					"field2": "value2",
				},
				fields: []accesstypes.Field{
					"field2",
					"field1",
				},
			},
			want: KeySet{
				keyParts: []KeyPart{
					{Key: "field2", Value: "value2"},
					{Key: "field1", Value: "value1"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &PatchSet[nilResource]{
				querySet: &QuerySet[nilResource]{
					keys: &fieldSet{
						data:   tt.fields.pkey,
						fields: tt.fields.fields,
					},
				},
				data: &fieldSet{
					data: tt.fields.data,
				},
			}
			got := p.PrimaryKey()
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(KeySet{}, KeyPart{})); diff != "" {
				t.Errorf("PatchSet.KeySet () mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPatchSet_HasKey(t *testing.T) {
	t.Parallel()

	type fields struct {
		data    map[accesstypes.Field]any
		pkey    map[accesstypes.Field]any
		pFields []accesstypes.Field
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "HasKey",
			fields: fields{
				pkey: map[accesstypes.Field]any{
					"field1": "value1",
				},
				pFields: []accesstypes.Field{"field1"},
			},
			want: true,
		},
		{
			name: "HasKey with empty",
			fields: fields{
				pkey: make(map[accesstypes.Field]any),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &PatchSet[nilResource]{
				querySet: &QuerySet[nilResource]{
					keys: &fieldSet{
						data:   tt.fields.pkey,
						fields: tt.fields.pFields,
					},
					fields: tt.fields.pFields,
				},
				data: &fieldSet{
					data: tt.fields.data,
				},
			}
			if got := p.HasKey(); got != tt.want {
				t.Errorf("PatchSet.HasKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

type Int int

type Marshaler struct {
	field string
}

func (m Marshaler) MarshalText() ([]byte, error) {
	return []byte(m.field), nil
}

type Marshaler2 Marshaler

func Test_match(t *testing.T) {
	t.Parallel()

	Time := time.Date(2032, 4, 23, 12, 2, 3, 4, time.UTC)
	Time2 := Time.Add(time.Hour)

	type args struct {
		v  any
		v2 any
	}
	tests := []struct {
		name        string
		args        args
		wantMatched bool
		wantErr     bool
	}{
		{name: "primitive matched int", args: args{v: int(1), v2: int(1)}, wantMatched: true},
		{name: "primitive matched int8", args: args{v: int8(1), v2: int8(1)}, wantMatched: true},
		{name: "primitive matched int16", args: args{v: int16(1), v2: int16(1)}, wantMatched: true},
		{name: "primitive matched int32", args: args{v: int32(1), v2: int32(1)}, wantMatched: true},
		{name: "primitive matched int64", args: args{v: int64(1), v2: int64(1)}, wantMatched: true},
		{name: "primitive matched uint", args: args{v: uint(1), v2: uint(1)}, wantMatched: true},
		{name: "primitive matched uint8", args: args{v: uint8(1), v2: uint8(1)}, wantMatched: true},
		{name: "primitive matched uint16", args: args{v: uint16(1), v2: uint16(1)}, wantMatched: true},
		{name: "primitive matched uint32", args: args{v: uint32(1), v2: uint32(1)}, wantMatched: true},
		{name: "primitive matched uint64", args: args{v: uint64(1), v2: uint64(1)}, wantMatched: true},
		{name: "primitive matched float32", args: args{v: float32(1), v2: float32(1)}, wantMatched: true},
		{name: "primitive matched float64", args: args{v: float64(1), v2: float64(1)}, wantMatched: true},
		{name: "primitive matched string", args: args{v: "1", v2: "1"}, wantMatched: true},
		{name: "primitive matched bool", args: args{v: true, v2: true}, wantMatched: true},
		{name: "primitive matched *int", args: args{v: ccc.Ptr(int(1)), v2: ccc.Ptr(int(1))}, wantMatched: true},
		{name: "primitive matched *int8", args: args{v: ccc.Ptr(int8(1)), v2: ccc.Ptr(int8(1))}, wantMatched: true},
		{name: "primitive matched *int16", args: args{v: ccc.Ptr(int16(1)), v2: ccc.Ptr(int16(1))}, wantMatched: true},
		{name: "primitive matched *int32", args: args{v: ccc.Ptr(int32(1)), v2: ccc.Ptr(int32(1))}, wantMatched: true},
		{name: "primitive matched *int64", args: args{v: ccc.Ptr(int64(1)), v2: ccc.Ptr(int64(1))}, wantMatched: true},
		{name: "primitive matched *uint", args: args{v: ccc.Ptr(uint(1)), v2: ccc.Ptr(uint(1))}, wantMatched: true},
		{name: "primitive matched *uint8", args: args{v: ccc.Ptr(uint8(1)), v2: ccc.Ptr(uint8(1))}, wantMatched: true},
		{name: "primitive matched *uint16", args: args{v: ccc.Ptr(uint16(1)), v2: ccc.Ptr(uint16(1))}, wantMatched: true},
		{name: "primitive matched *uint32", args: args{v: ccc.Ptr(uint32(1)), v2: ccc.Ptr(uint32(1))}, wantMatched: true},
		{name: "primitive matched *uint64", args: args{v: ccc.Ptr(uint64(1)), v2: ccc.Ptr(uint64(1))}, wantMatched: true},
		{name: "primitive matched *float32", args: args{v: ccc.Ptr(float32(1)), v2: ccc.Ptr(float32(1))}, wantMatched: true},
		{name: "primitive matched *float64", args: args{v: ccc.Ptr(float64(1)), v2: ccc.Ptr(float64(1))}, wantMatched: true},
		{name: "primitive matched *string", args: args{v: ccc.Ptr("1"), v2: ccc.Ptr("1")}, wantMatched: true},
		{name: "primitive matched *bool", args: args{v: ccc.Ptr(true), v2: ccc.Ptr(true)}, wantMatched: true},
		{name: "primitive not matched", args: args{v: 1, v2: 4}, wantMatched: false},

		{name: "named matched", args: args{v: Int(1), v2: Int(1)}, wantMatched: true},
		{name: "named not matched", args: args{v: Int(1), v2: Int(4)}, wantMatched: false},

		{name: "marshaler matched", args: args{v: Marshaler{field: "1"}, v2: Marshaler{field: "1"}}, wantMatched: true},
		{name: "marshaler not matched", args: args{v: Marshaler{field: "1"}, v2: Marshaler{"4"}}, wantMatched: false},
		{name: "marshaler error", args: args{v: Marshaler{field: "1"}, v2: Marshaler2{"1"}}, wantErr: true},

		{name: "time.Time matched", args: args{v: Time, v2: Time}, wantMatched: true},
		{name: "time.Time not matched", args: args{v: Time, v2: Time2}, wantMatched: false},

		{name: "[]time.Time matched", args: args{v: []time.Time{Time, Time2}, v2: []time.Time{Time, Time2}}, wantMatched: true},
		{name: "[]time.Time not matched", args: args{v: []time.Time{Time, Time2}, v2: []time.Time{Time, Time}}, wantMatched: false},

		{name: "different types error", args: args{v: Int(1), v2: 1}, wantErr: true},

		{name: "[]any matched", args: args{v: []any{1, 5}, v2: []any{1, 5}}, wantMatched: true},
		{name: "[]any slices not matched", args: args{v: []any{1, 5}, v2: []any{4, 5}}, wantMatched: false},

		{name: "[]int matched", args: args{v: []int{1, 5}, v2: []int{1, 5}}, wantMatched: true},
		{name: "[]int not matched", args: args{v: []int{1, 5}, v2: []int{4, 5}}, wantMatched: false},

		{name: "[]*int matched", args: args{v: []*int{ccc.Ptr(1), ccc.Ptr(5)}, v2: []*int{ccc.Ptr(1), ccc.Ptr(5)}}, wantMatched: true},
		{name: "[]*int not matched", args: args{v: []*int{ccc.Ptr(1), ccc.Ptr(5)}, v2: []*int{ccc.Ptr(4), ccc.Ptr(5)}}, wantMatched: false},

		{name: "[]int8 matched", args: args{v: []int8{1, 5}, v2: []int8{1, 5}}, wantMatched: true},
		{name: "[]int8 not matched", args: args{v: []int8{1, 5}, v2: []int8{4, 5}}, wantMatched: false},

		{name: "[]Int matched", args: args{v: []Int{1, 5}, v2: []Int{1, 5}}, wantMatched: true},
		{name: "[]Int not matched", args: args{v: []Int{1, 5}, v2: []Int{4, 5}}, wantMatched: false},

		{name: "*[]Int matched", args: args{v: &[]Int{1, 5}, v2: &[]Int{1, 5}}, wantMatched: true},
		{name: "*[]Int not matched", args: args{v: &[]Int{1, 5}, v2: &[]Int{4, 5}}, wantMatched: false},

		{name: "ccc.UUID matched", args: args{v: ccc.Must(ccc.UUIDFromString("a517b48d-63a9-4c1f-b45b-8474b164e423")), v2: ccc.Must(ccc.UUIDFromString("a517b48d-63a9-4c1f-b45b-8474b164e423"))}, wantMatched: true},
		{name: "ccc.UUID not matched", args: args{v: ccc.Must(ccc.UUIDFromString("a517b48d-63a9-4c1f-b45b-8474b164e423")), v2: ccc.Must(ccc.UUIDFromString("B517b48d-63a9-4c1f-b45b-8474b164e423"))}, wantMatched: false},

		{name: "*ccc.UUID matched", args: args{v: ccc.Ptr(ccc.Must(ccc.UUIDFromString("a517b48d-63a9-4c1f-b45b-8474b164e423"))), v2: ccc.Ptr(ccc.Must(ccc.UUIDFromString("a517b48d-63a9-4c1f-b45b-8474b164e423")))}, wantMatched: true},
		{name: "*ccc.UUID matched", args: args{v: ccc.Ptr(ccc.Must(ccc.UUIDFromString("a517b48d-63a9-4c1f-b45b-8474b164e423"))), v2: ccc.Ptr(ccc.Must(ccc.UUIDFromString("B517b48d-63a9-4c1f-b45b-8474b164e423")))}, wantMatched: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotMatched, err := match(tt.args.v, tt.args.v2)
			if (err != nil) != tt.wantErr {
				t.Errorf("match() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotMatched != tt.wantMatched {
				t.Errorf("match() = %v, want %v", gotMatched, tt.wantMatched)
			}
		})
	}
}

type myCustomType int

type toStructTestResource struct {
	StringField   string
	IntField      int
	PtrField      *string
	TimeField     time.Time
	unexported    string
	SliceField    []string
	IfaceField    any
	MyCustomType  myCustomType
	MyCustomType2 *myCustomType
}

func (toStructTestResource) Resource() accesstypes.Resource {
	return "toStructTestResources"
}

func TestPatchSet_ToStruct(t *testing.T) {
	t.Parallel()

	strPtr := "pointed to string"
	timeVal := time.Now().UTC()

	tests := []struct {
		name     string
		patchSet *PatchSet[toStructTestResource]
		want     *toStructTestResource
		panic    bool
	}{
		{
			name: "simple",
			patchSet: func() *PatchSet[toStructTestResource] {
				p := NewPatchSet(NewMetadata[toStructTestResource]())
				p.Set("StringField", "hello")
				p.Set("IntField", 123)
				p.Set("PtrField", &strPtr)
				p.Set("TimeField", timeVal)
				p.Set("MyCustomType", myCustomType(123))
				p.Set("MyCustomType2", ccc.Ptr(myCustomType(123)))

				return p
			}(),
			want: &toStructTestResource{
				StringField:   "hello",
				IntField:      123,
				PtrField:      &strPtr,
				TimeField:     timeVal,
				MyCustomType:  123,
				MyCustomType2: ccc.Ptr(myCustomType(123)),
			},
		},
		{
			name: "value to pointer field",
			patchSet: func() *PatchSet[toStructTestResource] {
				p := NewPatchSet(NewMetadata[toStructTestResource]())
				p.Set("PtrField", "a string for a pointer field")

				return p
			}(),
			panic: true,
		},
		{
			name: "nil values",
			patchSet: func() *PatchSet[toStructTestResource] {
				p := NewPatchSet(NewMetadata[toStructTestResource]())
				p.Set("PtrField", nil)
				p.Set("SliceField", nil)
				p.Set("IfaceField", nil)
				p.Set("MyCustomType2", nil)

				return p
			}(),
			want: &toStructTestResource{
				PtrField:      nil,
				SliceField:    nil,
				IfaceField:    nil,
				MyCustomType2: nil,
			},
		},
		{
			name: "unexported field panic",
			patchSet: func() *PatchSet[toStructTestResource] {
				p := NewPatchSet(NewMetadata[toStructTestResource]())
				p.Set("unexported", "should not be set")

				return p
			}(),
			panic: true,
		},
		{
			name: "field not in struct panic",
			patchSet: func() *PatchSet[toStructTestResource] {
				p := NewPatchSet(NewMetadata[toStructTestResource]())
				p.Set("NonExistentField", "should panic")

				return p
			}(),
			panic: true,
		},
		{
			name: "type mismatch panic",
			patchSet: func() *PatchSet[toStructTestResource] {
				p := NewPatchSet(NewMetadata[toStructTestResource]())
				p.Set("IntField", "not an int")

				return p
			}(),
			panic: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.panic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("The code did not panic")
					}
				}()
			}

			got := tt.patchSet.ToStruct()

			if tt.panic {
				return
			}

			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(toStructTestResource{})); diff != "" {
				t.Errorf("PatchSet.ToStruct() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

type notAStruct int

func (r notAStruct) Resource() accesstypes.Resource {
	return "notAStruct"
}

func TestPatchSet_ToStruct_NotStructPanic(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("The code did not panic")
		}
	}()

	p := NewPatchSet(NewMetadata[notAStruct]())
	p.ToStruct()
}

type fromStructTestResource struct {
	StringField string
	IntField    int
	BoolField   bool
	PtrField    *string
}

func (fromStructTestResource) Resource() accesstypes.Resource {
	return "fromStructTestResources"
}

func TestPatchSet_FromStruct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   any
		skip    []string
		want    *PatchSet[fromStructTestResource]
		wantErr bool
	}{
		{
			name: "simple struct",
			input: struct {
				StringField string
				IntField    int
			}{
				StringField: "hello",
				IntField:    42,
			},
			want: func() *PatchSet[fromStructTestResource] {
				p := NewPatchSet(NewMetadata[fromStructTestResource]())
				p.Set("StringField", "hello")
				p.Set("IntField", 42)

				return p
			}(),
		},
		{
			name: "pointer to struct",
			input: &struct {
				StringField string
				IntField    int
			}{
				StringField: "hello",
				IntField:    42,
			},
			want: func() *PatchSet[fromStructTestResource] {
				p := NewPatchSet(NewMetadata[fromStructTestResource]())
				p.Set("StringField", "hello")
				p.Set("IntField", 42)

				return p
			}(),
		},
		{
			name: "with fields to skip",
			input: struct {
				StringField string
				ExtraField  string
			}{
				StringField: "world",
				ExtraField:  "should be skipped",
			},
			skip: []string{"ExtraField"},
			want: func() *PatchSet[fromStructTestResource] {
				p := NewPatchSet(NewMetadata[fromStructTestResource]())
				p.Set("StringField", "world")

				return p
			}(),
		},
		{
			name: "unexported fields are skipped",
			input: struct {
				StringField string
				unexported  string
			}{
				StringField: "world",
				unexported:  "should be skipped",
			},
			want: func() *PatchSet[fromStructTestResource] {
				p := NewPatchSet(NewMetadata[fromStructTestResource]())
				p.Set("StringField", "world")

				return p
			}(),
		},
		{
			name: "field not in resource error",
			input: struct {
				StringField string
				ExtraField  string
			}{
				StringField: "world",
				ExtraField:  "should cause error",
			},
			wantErr: true,
		},
		{
			name:    "not a struct error",
			input:   "this is not a struct",
			wantErr: true,
		},
		{
			name: "struct with all fields",
			input: struct {
				StringField string
				IntField    int
				ExtraField  string
				unexported  string
			}{
				StringField: "all",
				IntField:    100,
				ExtraField:  "skip me",
				unexported:  "i am not exported",
			},
			skip: []string{"ExtraField"},
			want: func() *PatchSet[fromStructTestResource] {
				p := NewPatchSet(NewMetadata[fromStructTestResource]())
				p.Set("StringField", "all")
				p.Set("IntField", 100)

				return p
			}(),
		},
		{
			name: "struct with nil pointer field",
			input: struct {
				StringField string
				PtrField    *string
			}{
				StringField: "hello",
				PtrField:    nil,
			},
			want: func() *PatchSet[fromStructTestResource] {
				p := NewPatchSet(NewMetadata[fromStructTestResource]())
				p.Set("StringField", "hello")
				return p
			}(),
		},
		{
			name: "struct with non-nil pointer field",
			input: struct {
				StringField string
				PtrField    *string
			}{
				StringField: "hello",
				PtrField:    ccc.Ptr("world"),
			},
			want: func() *PatchSet[fromStructTestResource] {
				p := NewPatchSet(NewMetadata[fromStructTestResource]())
				p.Set("StringField", "hello")
				p.Set("PtrField", ccc.Ptr("world"))
				return p
			}(),
		},
		{
			name: "pointer to value field is dereferenced",
			input: struct {
				StringField *string
				IntField    *int
			}{
				StringField: ccc.Ptr("hello"),
				IntField:    ccc.Ptr(42),
			},
			want: func() *PatchSet[fromStructTestResource] {
				p := NewPatchSet(NewMetadata[fromStructTestResource]())
				p.Set("StringField", "hello")
				p.Set("IntField", 42)
				return p
			}(),
		},
		{
			name: "type to pointer value returns error",
			input: struct {
				PtrField string
			}{
				PtrField: "not an int",
			},
			wantErr: true,
		},
		{
			name: "type mismatch returns error",
			input: struct {
				IntField string
			}{
				IntField: "not an int",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := NewPatchSet(NewMetadata[fromStructTestResource]())
			err := p.FromStruct(tt.input, tt.skip...)

			if (err != nil) != tt.wantErr {
				t.Errorf("PatchSet.FromStruct() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if diff := PatchSetDiff()(tt.want, p); diff != "" {
				t.Errorf("PatchSet.FromStruct() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPatchSet_SetPatchType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want PatchType
	}{
		{
			name: "SetPatchType",
			want: CreatePatchType,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewPatchSet(NewMetadata[nilResource]())
			p.SetPatchType(tt.want)
			if got := p.PatchType(); got != tt.want {
				t.Errorf("PatchSet.PatchType() = %v, want %v", got, tt.want)
			}
		})
	}
}

type resolveTestResourcer struct {
	ID     int    `spanner:"id"     postgres:"id"`
	Field1 string `spanner:"field1" postgres:"field1"`
	Field2 string `spanner:"field2" postgres:"field2"`
}

func (r resolveTestResourcer) Resource() accesstypes.Resource {
	return "resolveTestResourcer"
}

func TestPatchSet_Resolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		patchSet *PatchSet[resolveTestResourcer]
		dbType   DBType
		want     map[string]any
		wantErr  bool
	}{
		{
			name: "spanner",
			patchSet: func() *PatchSet[resolveTestResourcer] {
				p := NewPatchSet(NewMetadata[resolveTestResourcer]())
				p.Set("Field1", "value1")
				p.Set("Field2", "value2")
				p.SetKey("ID", 1)

				return p
			}(),
			dbType: SpannerDBType,
			want: map[string]any{
				"id":     1,
				"field1": "value1",
				"field2": "value2",
			},
		},
		{
			name: "postgres",
			patchSet: func() *PatchSet[resolveTestResourcer] {
				p := NewPatchSet(NewMetadata[resolveTestResourcer]())
				p.Set("Field1", "value1")
				p.Set("Field2", "value2")
				p.SetKey("ID", 1)

				return p
			}(),
			dbType: PostgresDBType,
			want: map[string]any{
				"id":     1,
				"field1": "value1",
				"field2": "value2",
			},
		},
		{
			name: "no pkey error",
			patchSet: func() *PatchSet[resolveTestResourcer] {
				p := NewPatchSet(NewMetadata[resolveTestResourcer]())
				p.Set("Field1", "value1")

				return p
			}(),
			dbType:  SpannerDBType,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.patchSet.Resolve(tt.dbType)
			if (err != nil) != tt.wantErr {
				t.Errorf("PatchSet.Resolve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("PatchSet.Resolve() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

type diffTestResourcer struct {
	Field1 string
	Field2 int
}

func (r diffTestResourcer) Resource() accesstypes.Resource {
	return "diffTestResourcer"
}

func TestPatchSet_Diff(t *testing.T) {
	t.Parallel()

	meta := NewMetadata[diffTestResourcer]()

	tests := []struct {
		name     string
		patchSet *PatchSet[diffTestResourcer]
		old      *diffTestResourcer
		want     map[accesstypes.Field]DiffElem
		wantErr  bool
	}{
		{
			name: "no changes",
			patchSet: func() *PatchSet[diffTestResourcer] {
				p := NewPatchSet(meta)
				p.Set("Field1", "value1")
				p.Set("Field2", 2)
				return p
			}(),
			old: &diffTestResourcer{
				Field1: "value1",
				Field2: 2,
			},
			want: map[accesstypes.Field]DiffElem{},
		},
		{
			name: "with changes",
			patchSet: func() *PatchSet[diffTestResourcer] {
				p := NewPatchSet(meta)
				p.Set("Field1", "value2")
				p.Set("Field2", 3)
				return p
			}(),
			old: &diffTestResourcer{
				Field1: "value1",
				Field2: 2,
			},
			want: map[accesstypes.Field]DiffElem{
				"Field1": {Old: "value1", New: "value2"},
				"Field2": {Old: 2, New: 3},
			},
		},
		{
			name: "field in patch not in old error",
			patchSet: func() *PatchSet[diffTestResourcer] {
				p := NewPatchSet(meta)
				p.Set("Field3", "value3")
				return p
			}(),
			old:     &diffTestResourcer{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.patchSet.Diff(tt.old)
			if (err != nil) != tt.wantErr {
				t.Errorf("PatchSet.Diff() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("PatchSet.Diff() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

type deleteChangeSetTestResourcer struct {
	Field1 string
	Field2 int
}

func (r deleteChangeSetTestResourcer) Resource() accesstypes.Resource {
	return "deleteChangeSetTestResourcer"
}

func TestPatchSet_deleteChangeSet(t *testing.T) {
	t.Parallel()

	meta := NewMetadata[deleteChangeSetTestResourcer]()
	p := NewPatchSet(meta)

	old := &deleteChangeSetTestResourcer{
		Field1: "value1",
		Field2: 2,
	}

	want := map[accesstypes.Field]DiffElem{
		"Field1": {Old: "value1"},
		"Field2": {Old: 2},
	}

	got, err := p.deleteChangeSet(old)
	if err != nil {
		t.Fatalf("PatchSet.deleteChangeSet() error = %v", err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("PatchSet.deleteChangeSet() mismatch (-want +got):\n%s", diff)
	}
}

func Test_all(t *testing.T) {
	t.Parallel()

	map1 := map[string]int{"a": 1, "b": 2}
	map2 := map[string]int{"c": 3, "d": 4}
	map3 := map[string]int{"a": 5} // Duplicate key

	want := map[string]int{"a": 5, "b": 2, "c": 3, "d": 4}
	got := make(map[string]int)

	for k, v := range all(map1, map2, map3) {
		got[k] = v
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("all() mismatch (-want +got):\n%s", diff)
	}
}

func TestPatchSet_validateEventSource(t *testing.T) {
	t.Parallel()

	metaWithTracking := NewMetadata[nilResource]()
	metaWithTracking.trackChanges = true

	metaWithoutTracking := NewMetadata[nilResource]()

	tests := []struct {
		name        string
		patchSet    *PatchSet[nilResource]
		eventSource []string
		want        string
		wantErr     bool
	}{
		{
			name:        "tracking enabled, source provided",
			patchSet:    NewPatchSet(metaWithTracking),
			eventSource: []string{"test-source"},
			want:        "test-source",
		},
		{
			name:        "tracking enabled, no source",
			patchSet:    NewPatchSet(metaWithTracking),
			eventSource: []string{},
			wantErr:     true,
		},
		{
			name:        "tracking enabled, multiple sources",
			patchSet:    NewPatchSet(metaWithTracking),
			eventSource: []string{"source1", "source2"},
			wantErr:     true,
		},
		{
			name:        "tracking disabled, source provided",
			patchSet:    NewPatchSet(metaWithoutTracking),
			eventSource: []string{"test-source"},
			want:        "test-source",
		},
		{
			name:        "tracking disabled, no source",
			patchSet:    NewPatchSet(metaWithoutTracking),
			eventSource: []string{},
			want:        "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.patchSet.validateEventSource(tt.eventSource)
			if (err != nil) != tt.wantErr {
				t.Errorf("PatchSet.validateEventSource() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("PatchSet.validateEventSource() = %v, want %v", got, tt.want)
			}
		})
	}
}

type insertChangeSetTestResourcer struct {
	Field1 string
	Field2 int
}

func (r insertChangeSetTestResourcer) Resource() accesstypes.Resource {
	return "insertChangeSetTestResourcer"
}

func TestPatchSet_insertChangeSet(t *testing.T) {
	t.Parallel()

	meta := NewMetadata[insertChangeSetTestResourcer]()
	p := NewPatchSet(meta)
	p.Set("Field1", "value1")
	p.Set("Field2", 2)

	want := map[accesstypes.Field]DiffElem{
		"Field1": {New: "value1"},
		"Field2": {New: 2},
	}

	got, err := p.insertChangeSet()
	if err != nil {
		t.Fatalf("PatchSet.insertChangeSet() error = %v", err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("PatchSet.insertChangeSet() mismatch (-want +got):\n%s", diff)
	}
}

func TestPatchSetCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a        PatchSetComparer
		b        PatchSetComparer
		wantDiff bool
	}{
		{
			name: "equal patch sets",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(CreatePatchType)
				ps.Set("field1", "value1")
				ps.SetKey("id", 1)

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(CreatePatchType)
				ps.Set("field1", "value1")
				ps.SetKey("id", 1)

				return ps
			}(),
			wantDiff: false,
		},
		{
			name: "equal patch sets with different field order",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(CreatePatchType)
				ps.Set("field1", "value1")
				ps.Set("field2", "value2")
				ps.SetKey("id", 1)

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(CreatePatchType)
				ps.Set("field2", "value2")
				ps.Set("field1", "value1")
				ps.SetKey("id", 1)

				return ps
			}(),
			wantDiff: false,
		},
		{
			name: "equal patch sets with different primary key order on CreatePatchType",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(CreatePatchType)
				ps.SetKey("id", 1)
				ps.SetKey("name", "test")

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(CreatePatchType)
				ps.SetKey("name", "test")
				ps.SetKey("id", 1)

				return ps
			}(),
			wantDiff: false,
		},
		{
			name: "equal patch sets with different primary key order on UpdatePatchType",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(UpdatePatchType)
				ps.SetKey("id", 1)
				ps.SetKey("name", "test")

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(UpdatePatchType)
				ps.SetKey("name", "test")
				ps.SetKey("id", 1)

				return ps
			}(),
			wantDiff: false,
		},
		{
			name: "different patch type",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(CreatePatchType)
				ps.Set("field1", "value1")
				ps.SetKey("id", 1)

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(UpdatePatchType)
				ps.Set("field1", "value1")
				ps.SetKey("id", 1)

				return ps
			}(),
			wantDiff: true,
		},
		{
			name: "different data for create patch",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(CreatePatchType)
				ps.Set("field1", "value1")
				ps.SetKey("id", 1)

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(CreatePatchType)
				ps.Set("field1", "value2")
				ps.SetKey("id", 1)

				return ps
			}(),
			wantDiff: true,
		},
		{
			name: "different data for other patch",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(UpdatePatchType)
				ps.Set("field1", "value1")
				ps.SetKey("id", 1)

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(UpdatePatchType)
				ps.Set("field1", "value2")
				ps.SetKey("id", 1)

				return ps
			}(),
			wantDiff: true,
		},
		{
			name: "different fields",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(CreatePatchType)
				ps.Set("field1", "value1")
				ps.SetKey("id", 1)

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(CreatePatchType)
				ps.Set("field2", "value1")
				ps.SetKey("id", 1)

				return ps
			}(),
			wantDiff: true,
		},
		{
			name: "different primary key for create patch",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(CreatePatchType)
				ps.Set("field1", "value1")
				ps.SetKey("id", 1)

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(CreatePatchType)
				ps.Set("field1", "value1")
				ps.SetKey("id", 2)

				return ps
			}(),
			wantDiff: false,
		},
		{
			name: "different primary key for other patchs",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(UpdatePatchType)
				ps.Set("field1", "value1")
				ps.SetKey("id", 1)

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(UpdatePatchType)
				ps.Set("field1", "value1")
				ps.SetKey("id", 2)

				return ps
			}(),
			wantDiff: true,
		},
		{
			name: "different resource",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[resourcer]())
				ps.SetPatchType(CreatePatchType)
				ps.Set("field1", "value1")
				ps.SetKey("id", 1)

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[nilResource]())
				ps.SetPatchType(CreatePatchType)
				ps.Set("field1", "value1")
				ps.SetKey("id", 1)

				return ps
			}(),
			wantDiff: true,
		},
		{
			name: "resource with securehash.Hash field",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[nilResource]())
				ps.SetPatchType(UpdatePatchType)
				ps.Set("field1", func() securehash.Hash {
					h := securehash.Hash{}
					_ = h.UnmarshalText([]byte("1$12288$3$1$oGwawstCMOWozw2vbJgyyQ==.TwIukshFIMhe8brmzjO21FBjB/OeMiHHEEVVVRliDIc="))

					return h
				}())

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[nilResource]())
				ps.SetPatchType(UpdatePatchType)
				ps.Set("field1", func() securehash.Hash {
					h := securehash.Hash{}
					_ = h.UnmarshalText([]byte("1$12288$3$1$oGwawstCMOWozw2vbJgyyQ==.TwIukshFIMhe8brmzjO21FBjB/OeMiHHEEVVVRliDIc="))

					return h
				}())

				return ps
			}(),
			wantDiff: false,
		},
		{
			name: "resource with *securehash.Hash field",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[nilResource]())
				ps.SetPatchType(UpdatePatchType)
				ps.Set("field1", func() *securehash.Hash {
					h := new(securehash.Hash)
					_ = h.UnmarshalText([]byte("1$12288$3$1$oGwawstCMOWozw2vbJgyyQ==.TwIukshFIMhe8brmzjO21FBjB/OeMiHHEEVVVRliDIc="))

					return h
				}())

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[nilResource]())
				ps.SetPatchType(UpdatePatchType)
				ps.Set("field1", func() *securehash.Hash {
					h := new(securehash.Hash)
					_ = h.UnmarshalText([]byte("1$12288$3$1$oGwawstCMOWozw2vbJgyyQ==.TwIukshFIMhe8brmzjO21FBjB/OeMiHHEEVVVRliDIc="))

					return h
				}())

				return ps
			}(),
			wantDiff: false,
		},
		{
			name: "resource with different securehash.Hash field",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[nilResource]())
				ps.SetPatchType(UpdatePatchType)
				ps.Set("field1", func() *securehash.Hash {
					h := new(securehash.Hash)
					_ = h.UnmarshalText([]byte("1$12288$3$1$oGwawstCMOWozw2vbJgyyQ==.TwIukshFIMhe8brmzjO21FBjB/OeMiHHEEVVVRliDIc="))

					return h
				}())

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[nilResource]())
				ps.SetPatchType(UpdatePatchType)
				ps.Set("field1", func() *securehash.Hash {
					h := new(securehash.Hash)
					_ = h.UnmarshalText([]byte("1$12288$3$1$nlP2592Ld9cIt2wwhyW7xw==.Cfghih+wTsPz00Fp4PraYRorRJim1RjxFXykoxtJjBM="))

					return h
				}())

				return ps
			}(),
			wantDiff: true,
		},
		{
			name: "resource with securehash.Hash key",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[nilResource]())
				ps.SetPatchType(UpdatePatchType)
				ps.SetKey("field1", func() securehash.Hash {
					h := securehash.Hash{}
					_ = h.UnmarshalText([]byte("1$12288$3$1$oGwawstCMOWozw2vbJgyyQ==.TwIukshFIMhe8brmzjO21FBjB/OeMiHHEEVVVRliDIc="))

					return h
				}())

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[nilResource]())
				ps.SetPatchType(UpdatePatchType)
				ps.SetKey("field1", func() securehash.Hash {
					h := securehash.Hash{}
					_ = h.UnmarshalText([]byte("1$12288$3$1$oGwawstCMOWozw2vbJgyyQ==.TwIukshFIMhe8brmzjO21FBjB/OeMiHHEEVVVRliDIc="))

					return h
				}())

				return ps
			}(),
			wantDiff: false,
		},
		{
			name: "resource with different securehash.Hash key",
			a: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[nilResource]())
				ps.SetPatchType(UpdatePatchType)
				ps.SetKey("field1", func() *securehash.Hash {
					h := new(securehash.Hash)
					_ = h.UnmarshalText([]byte("1$12288$3$1$oGwawstCMOWozw2vbJgyyQ==.TwIukshFIMhe8brmzjO21FBjB/OeMiHHEEVVVRliDIc="))

					return h
				}())

				return ps
			}(),
			b: func() PatchSetComparer {
				ps := NewPatchSet(NewMetadata[nilResource]())
				ps.SetPatchType(UpdatePatchType)
				ps.SetKey("field1", func() *securehash.Hash {
					h := new(securehash.Hash)
					_ = h.UnmarshalText([]byte("1$12288$3$1$nlP2592Ld9cIt2wwhyW7xw==.Cfghih+wTsPz00Fp4PraYRorRJim1RjxFXykoxtJjBM="))

					return h
				}())

				return ps
			}(),
			wantDiff: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := PatchSetDiff(cmp.Comparer(hashCompare))(tt.a, tt.b); (diff != "") != tt.wantDiff {
				t.Errorf("PatchSetCompare() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func hashCompare(h1, h2 securehash.Hash) bool {
	v1, _ := h1.MarshalText()
	v2, _ := h2.MarshalText()

	return bytes.Equal(v1, v2)
}

func Test_matchPrimitive(t *testing.T) {
	t.Parallel()

	type args struct {
		v  string
		v2 any
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "equal strings",
			args: args{v: "hello", v2: "hello"},
			want: true,
		},
		{
			name: "unequal strings",
			args: args{v: "hello", v2: "world"},
			want: false,
		},
		{
			name:    "incompatible type",
			args:    args{v: "hello", v2: 123},
			wantErr: true,
		},
		{
			name:    "v2 is nil",
			args:    args{v: "hello", v2: nil},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := matchPrimitive(tt.args.v, tt.args.v2)
			if (err != nil) != tt.wantErr {
				t.Errorf("matchPrimitive() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("matchPrimitive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_matchPrimitivePtr_string(t *testing.T) {
	t.Parallel()

	type args struct {
		v  *string
		v2 any
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "both nil",
			args: args{
				v:  nil,
				v2: nil,
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "v nil",
			args: args{
				v:  nil,
				v2: ccc.Ptr("hello"),
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "v2 nil",
			args: args{
				v:  ccc.Ptr("hello"),
				v2: nil,
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "both pointing to same value",
			args: args{
				v:  ccc.Ptr("hello"),
				v2: ccc.Ptr("hello"),
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "both pointing to different values",
			args: args{
				v:  ccc.Ptr("hello"),
				v2: ccc.Ptr("world"),
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "v2 is not a pointer",
			args: args{
				v:  ccc.Ptr("hello"),
				v2: "hello",
			},
			want:    false,
			wantErr: true,
		},
		{
			name: "v2 is a pointer to a different type",
			args: args{
				v:  ccc.Ptr("hello"),
				v2: ccc.Ptr(123),
			},
			want:    false,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := matchPrimitivePtr(tt.args.v, tt.args.v2)
			if (err != nil) != tt.wantErr {
				t.Errorf("matchPrimitivePtr() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("matchPrimitivePtr() = %v, want %v", got, tt.want)
			}
		})
	}
}
