package resource

import (
	"testing"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
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
				querySet: NewQuerySet(NewResourceMetadata[nilResource]()),
				data:     newFieldSet(),
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NewPatchSet(NewResourceMetadata[nilResource]())
			if diff := cmp.Diff(tt.want, got, cmp.Comparer(PatchsetCompare)); diff != "" {
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
				querySet: NewQuerySet(NewResourceMetadata[nilResource]()).AddField("field1").AddField("field2"),
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
				querySet: NewQuerySet(NewResourceMetadata[nilResource]()).AddField("field2").AddField("field1"),
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &PatchSet[nilResource]{
				querySet: NewQuerySet(NewResourceMetadata[nilResource]()),
				data:     newFieldSet(),
			}
			for _, i := range tt.args {
				p.Set(i.field, i.value)
			}
			got := p
			if diff := cmp.Diff(tt.want, got, cmp.Comparer(PatchsetCompare)); diff != "" {
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
		tt := tt
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
					rMeta: NewResourceMetadata[nilResource](),
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
					rMeta: NewResourceMetadata[nilResource](),
				},
				data: newFieldSet(),
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &PatchSet[nilResource]{
				querySet: NewQuerySet(NewResourceMetadata[nilResource]()),
				data:     newFieldSet(),
			}
			for _, i := range tt.args {
				p.SetKey(i.field, i.value)
			}
			got := p
			if diff := cmp.Diff(tt.want, got, cmp.Comparer(PatchsetCompare)); diff != "" {
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
		tt := tt
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
		tt := tt
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
		tt := tt
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
		tt := tt
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
		tt := tt
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
		tt := tt
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
