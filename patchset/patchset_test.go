// package patchset provides types to store json patch set mapping to struct fields.
package patchset

import (
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want *PatchSet
	}{
		{
			name: "New",
			want: &PatchSet{
				data: make(map[accesstypes.Field]any),
				pkey: make(map[accesstypes.Field]any),
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := New()
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(PatchSet{})); diff != "" {
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
		want *PatchSet
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
			want: &PatchSet{
				data: map[accesstypes.Field]any{
					"field1": "value1",
					"field2": "value2",
				},
				dFields: []accesstypes.Field{
					"field1",
					"field2",
				},
				pkey: make(map[accesstypes.Field]any),
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
			want: &PatchSet{
				data: map[accesstypes.Field]any{
					"field1": "value1",
					"field2": "value2",
				},
				dFields: []accesstypes.Field{
					"field2",
					"field1",
				},
				pkey: make(map[accesstypes.Field]any),
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &PatchSet{
				data: make(map[accesstypes.Field]any),
				pkey: make(map[accesstypes.Field]any),
			}
			for _, i := range tt.args {
				p.Set(i.field, i.value)
			}
			got := p
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(PatchSet{})); diff != "" {
				t.Errorf("PatchSet.Set() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPatchSet_Get(t *testing.T) {
	t.Parallel()

	type fields struct {
		data map[accesstypes.Field]any
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
				data: map[accesstypes.Field]any{
					"field1": "value1",
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
			p := &PatchSet{
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
		want *PatchSet
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
			want: &PatchSet{
				data: make(map[accesstypes.Field]any),
				pkey: map[accesstypes.Field]any{
					"field1": "value1",
					"field2": "value2",
				},
				kFields: []accesstypes.Field{"field1", "field2"},
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
			want: &PatchSet{
				data: make(map[accesstypes.Field]any),
				pkey: map[accesstypes.Field]any{
					"field1": "value1",
					"field2": "value2",
				},
				kFields: []accesstypes.Field{"field2", "field1"},
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &PatchSet{
				data: make(map[accesstypes.Field]any),
				pkey: make(map[accesstypes.Field]any),
			}
			for _, i := range tt.args {
				p.SetKey(i.field, i.value)
			}
			got := p
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(PatchSet{})); diff != "" {
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
			p := &PatchSet{
				data:    tt.fields.data,
				dFields: tt.fields.dFields,
				pkey:    tt.fields.pkey,
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
		data map[accesstypes.Field]any
		pkey map[accesstypes.Field]any
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
			},
			want: 2,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &PatchSet{
				data: tt.fields.data,
				pkey: tt.fields.pkey,
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
			p := &PatchSet{
				data: tt.fields.data,
				pkey: tt.fields.pkey,
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
		want   PrimaryKey
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
			want: PrimaryKey{
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
			want: PrimaryKey{
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
			p := &PatchSet{
				data:    tt.fields.data,
				pkey:    tt.fields.pkey,
				kFields: tt.fields.fields,
			}
			got := p.PrimaryKey()
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(PrimaryKey{}, KeyPart{})); diff != "" {
				t.Errorf("PatchSet.KeyData () mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPatchSet_HasKey(t *testing.T) {
	type fields struct {
		data map[accesstypes.Field]any
		pkey map[accesstypes.Field]any
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
			p := &PatchSet{
				data: tt.fields.data,
				pkey: tt.fields.pkey,
			}
			if got := p.HasKey(); got != tt.want {
				t.Errorf("PatchSet.HasKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
