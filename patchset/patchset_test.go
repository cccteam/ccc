// package patchset provides types to store json patch set mapping to struct fields.
package patchset

import (
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

func TestNewPatchSet(t *testing.T) {
	t.Parallel()

	type args struct {
		data map[accesstypes.Field]any
	}
	tests := []struct {
		name string
		args args
		want *PatchSet
	}{
		{
			name: "NewPatchSet",
			args: args{
				data: map[accesstypes.Field]any{
					"field1": "value1",
					"field2": "value2",
				},
			},
			want: &PatchSet{
				data: map[accesstypes.Field]any{
					"field1": "value1",
					"field2": "value2",
				},
				pkey: make(map[accesstypes.Field]any),
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NewPatchSet(tt.args.data)
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
		args args
		want *PatchSet
	}{
		{
			name: "Set",
			args: args{
				field: "field1",
				value: "value1",
			},
			want: &PatchSet{
				data: map[accesstypes.Field]any{
					"field1": "value1",
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
			p.Set(tt.args.field, tt.args.value)
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
		args args
		want *PatchSet
	}{
		{
			name: "SetKey",
			args: args{
				field: "field1",
				value: "value1",
			},
			want: &PatchSet{
				data: make(map[accesstypes.Field]any),
				pkey: map[accesstypes.Field]any{
					"field1": "value1",
				},
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
			p.SetKey(tt.args.field, tt.args.value)
			got := p
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(PatchSet{})); diff != "" {
				t.Errorf("PatchSet.SetKey () mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPatchSet_StructFields(t *testing.T) {
	t.Parallel()

	type fields struct {
		data map[accesstypes.Field]any
		pkey map[accesstypes.Field]any
	}
	tests := []struct {
		name   string
		fields fields
		want   []accesstypes.Field
	}{
		{
			name: "StructFields",
			fields: fields{
				data: map[accesstypes.Field]any{
					"field1": "value1",
					"field2": "value2",
				},
			},
			want: []accesstypes.Field{
				"field1",
				"field2",
			},
		},
		{
			name: "StructFields with keys",
			fields: fields{
				data: map[accesstypes.Field]any{
					"field1": "value1",
					"field2": "value2",
				},
				pkey: map[accesstypes.Field]any{
					"field3": "value1",
				},
			},
			want: []accesstypes.Field{
				"field1",
				"field2",
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
			got := p.StructFields()
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("PatchSet.StructFields () mismatch (-want +got):\n%s", diff)
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

func TestPatchSet_KeyData(t *testing.T) {
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
			name: "KeyData",
			fields: fields{
				pkey: map[accesstypes.Field]any{
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
			name: "KeyData with data",
			fields: fields{
				data: map[accesstypes.Field]any{
					"field3": "value1",
				},
				pkey: map[accesstypes.Field]any{
					"field1": "value1",
					"field2": "value2",
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
			got := p.KeyData()
			if diff := cmp.Diff(tt.want, got); diff != "" {
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
