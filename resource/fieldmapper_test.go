package resource

import (
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

func TestNewFieldMapper(t *testing.T) {
	t.Parallel()

	type args struct {
		v any
	}
	tests := []struct {
		name    string
		args    args
		want    *RequestFieldMapper
		wantErr bool
	}{
		{
			name: "NewFieldMapper",
			args: args{
				v: struct {
					Field1 string `json:"field1"`
					Field2 string `json:"field2"`
				}{},
			},
			want: &RequestFieldMapper{
				jsonTagToFields: map[string]accesstypes.Field{
					"field1": "Field1",
					"field2": "Field2",
				},
				fieldToJSONName: map[accesstypes.Field]string{
					"Field1": "field1",
					"Field2": "field2",
				},
				fields: []accesstypes.Field{
					"Field1",
					"Field2",
				},
			},
			wantErr: false,
		},
		{
			name: "NewFieldMapper non-struct error",
			args: args{
				v: int(64),
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewRequestFieldMapper(tt.args.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewFieldMapper() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(RequestFieldMapper{})); diff != "" {
				t.Errorf("NewFieldMapper() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFieldMapper_StructFieldName(t *testing.T) {
	t.Parallel()

	type fields struct {
		jsonTagToFields map[string]accesstypes.Field
	}
	type args struct {
		tag string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   accesstypes.Field
		want1  bool
	}{
		{
			name: "StructFieldName",
			fields: fields{
				jsonTagToFields: map[string]accesstypes.Field{
					"field1": "Field1",
					"field2": "Field2",
				},
			},
			args: args{
				tag: "field1",
			},
			want:  "Field1",
			want1: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &RequestFieldMapper{
				jsonTagToFields: tt.fields.jsonTagToFields,
			}
			got, got1 := f.StructFieldName(tt.args.tag)
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(RequestFieldMapper{})); diff != "" {
				t.Errorf("FieldMapper.StructFieldName() mismatch (-want +got):\n%s", diff)
			}
			if got1 != tt.want1 {
				t.Errorf("FieldMapper.StructFieldName() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestFieldMapper_Len(t *testing.T) {
	t.Parallel()

	type fields struct {
		jsonTagToFields map[string]accesstypes.Field
	}
	tests := []struct {
		name   string
		fields fields
		want   int
	}{
		{
			name: "Len",
			fields: fields{
				jsonTagToFields: map[string]accesstypes.Field{
					"field1": "Field1",
					"field2": "Field2",
				},
			},
			want: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &RequestFieldMapper{
				jsonTagToFields: tt.fields.jsonTagToFields,
			}
			if got := f.Len(); got != tt.want {
				t.Errorf("FieldMapper.Len() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFieldMapper_Fields(t *testing.T) {
	t.Parallel()

	type fields struct {
		jsonTagToFields map[string]accesstypes.Field
		fields          []accesstypes.Field
	}
	tests := []struct {
		name   string
		fields fields
		want   []accesstypes.Field
	}{
		{
			name: "Fields",
			fields: fields{
				jsonTagToFields: map[string]accesstypes.Field{
					"field1": "Field1",
					"field2": "Field2",
				},
				fields: []accesstypes.Field{
					"Field1",
					"Field2",
				},
			},
			want: []accesstypes.Field{
				"Field1",
				"Field2",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &RequestFieldMapper{
				jsonTagToFields: tt.fields.jsonTagToFields,
				fields:          tt.fields.fields,
			}
			got := f.Fields()
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(RequestFieldMapper{})); diff != "" {
				t.Errorf("FieldMapper.Fields() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_tagToFieldMap(t *testing.T) {
	t.Parallel()

	type args struct {
		v any
	}
	tests := []struct {
		name       string
		args       args
		want       map[string]accesstypes.Field
		wantFields []accesstypes.Field
		wantFormer map[string]string
		wantErr    bool
	}{
		{
			name: "tagToFieldMap",
			args: args{
				v: struct {
					Field1 string `json:"field1"`
					Field2 string `json:"field2"`
				}{},
			},
			want: map[string]accesstypes.Field{
				"field1": "Field1",
				"field2": "Field2",
			},
			wantFields: []accesstypes.Field{
				"Field1",
				"Field2",
			},
			wantErr: false,
		},
		{
			name: "tagToFieldMap struct ptr",
			args: args{
				v: &struct {
					Field1 string `json:"field1"`
					Field2 string `json:"field2"`
				}{},
			},
			want: map[string]accesstypes.Field{
				"field1": "Field1",
				"field2": "Field2",
			},
			wantFields: []accesstypes.Field{
				"Field1",
				"Field2",
			},
			wantErr: false,
		},
		{
			name: "tagToFieldMap with - tag",
			args: args{
				v: struct {
					Field1 string `json:"-"`
					Field2 string `json:"field2"`
				}{},
			},
			want: map[string]accesstypes.Field{
				"field2": "Field2",
			},
			wantFields: []accesstypes.Field{
				"Field2",
			},
			wantErr: false,
		},
		{
			name: "tagToFieldMap with empty tag",
			args: args{
				v: struct {
					FieldTag1 string
					FieldTag2 string `json:"fieldTag2"`
				}{},
			},
			want: map[string]accesstypes.Field{
				"FieldTag1": "FieldTag1",
				"fieldtag1": "FieldTag1",
				"fieldTag2": "FieldTag2",
			},
			wantFields: []accesstypes.Field{
				"FieldTag1",
				"FieldTag2",
			},
			wantErr: false,
		},
		{
			name: "tagToFieldMap with comma in tag",
			args: args{
				v: struct {
					Field1 string `json:"field1,omitzero"`
					Field2 string `json:"field2"`
				}{},
			},
			want: map[string]accesstypes.Field{
				"field1": "Field1",
				"field2": "Field2",
			},
			wantFields: []accesstypes.Field{
				"Field1",
				"Field2",
			},
			wantErr: false,
		},
		{
			name: "tagToFieldMap non-struct error",
			args: args{
				v: int(43),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "a former wire name maps to the field beside its current name",
			args: args{
				v: struct {
					ID       string `json:"id"`
					Headline string `json:"headline" formerly:"title"`
				}{},
			},
			want: map[string]accesstypes.Field{
				"id":       "ID",
				"headline": "Headline",
				"title":    "Headline",
			},
			wantFields: []accesstypes.Field{
				"ID",
				"Headline",
			},
			wantFormer: map[string]string{"title": "headline"},
		},
		{
			name: "a former wire name that is another field's name is refused",
			args: args{
				v: struct {
					Headline string `json:"headline" formerly:"body"`
					Body     string `json:"body"`
				}{},
			},
			wantErr: true,
		},
		{
			name: "a former wire name two fields share is refused",
			args: args{
				v: struct {
					Headline string `json:"headline" formerly:"title"`
					Body     string `json:"body"     formerly:"title"`
				}{},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mapper, err := tagToFieldMap(tt.args.v)
			if (err != nil) != tt.wantErr {
				t.Errorf("tagToFieldMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if diff := cmp.Diff(tt.want, mapper.jsonTagToFields); diff != "" {
				t.Errorf("tagToFieldMap() mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantFields, mapper.fields); diff != "" {
				t.Errorf("tagToFieldMap() fields mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantFormer, mapper.FormerNames()); diff != "" {
				t.Errorf("FormerNames() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
