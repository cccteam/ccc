// package resourceset is a set of resources that provides a way to map permissions to fields in a struct.
package resourceset

import (
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

func TestNew(t *testing.T) {
	type args struct {
		v           any
		resource    accesstypes.Resource
		permissions []accesstypes.Permission
	}
	tests := []struct {
		name    string
		args    args
		want    *ResourceSet
		wantErr bool
	}{
		{
			name: "New only tag permissions",
			args: args{
				v: struct {
					Field1 string `json:"field1"`
					Field2 string `json:"field2" perm:"Read"`
				}{},
				resource: "resource",
			},
			want: &ResourceSet{
				permissions: []accesstypes.Permission{accesstypes.Read},
				requiredTagPerm: accesstypes.TagPermissions{
					"field2": {accesstypes.Read},
				},
				fieldToTag: map[accesstypes.Field]accesstypes.Tag{
					"Field2": "field2",
				},
				resource: "resource",
			},
			wantErr: false,
		},
		{
			name: "New with permissions same as tag",
			args: args{
				v: struct {
					Field1 string `json:"field1"`
					Field2 string `json:"field2" perm:"Read"`
				}{},
				resource:    "resource",
				permissions: []accesstypes.Permission{accesstypes.Read},
			},
			want: &ResourceSet{
				permissions: []accesstypes.Permission{accesstypes.Read},
				requiredTagPerm: accesstypes.TagPermissions{
					"field2": {accesstypes.Read},
				},
				fieldToTag: map[accesstypes.Field]accesstypes.Tag{
					"Field2": "field2",
				},
				resource: "resource",
			},
			wantErr: false,
		},
		{
			name: "New with additional permissions",
			args: args{
				v: struct {
					Field1 string `json:"field1"`
					Field2 string `json:"field2" perm:"Create"`
				}{},
				resource:    "resource",
				permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update},
			},
			want: &ResourceSet{
				permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update},
				requiredTagPerm: accesstypes.TagPermissions{
					"field2": {accesstypes.Create},
				},
				fieldToTag: map[accesstypes.Field]accesstypes.Tag{
					"Field2": "field2",
				},
				resource: "resource",
			},
			wantErr: false,
		},
		{
			name: "New with multiple permissions",
			args: args{
				v: struct {
					Field1 string `json:"field1"`
					Field2 string `json:"field2" perm:"Create,Update"`
				}{},
				resource: "resource",
			},
			want: &ResourceSet{
				permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update},
				requiredTagPerm: accesstypes.TagPermissions{
					"field2": {accesstypes.Create, accesstypes.Update},
				},
				fieldToTag: map[accesstypes.Field]accesstypes.Tag{
					"Field2": "field2",
				},
				resource: "resource",
			},
			wantErr: false,
		},
		{
			name: "New with invalid permission mix on tags",
			args: args{
				v: struct {
					Field1 string `json:"field1"`
					Field2 string `json:"field2" perm:"Read,Update"`
				}{},
				resource: "resource",
			},
			wantErr: true,
		},
		{
			name: "New with invalid permission mix on input",
			args: args{
				v: struct {
					Field1 string `json:"field1"`
					Field2 string `json:"field2"`
				}{},
				resource:    "resource",
				permissions: []accesstypes.Permission{accesstypes.Read, accesstypes.Update},
			},
			wantErr: true,
		},
		{
			name: "New with invalid permission",
			args: args{
				v: struct {
					Field1 string `json:"field1"`
					Field2 string `json:"field2" perm:"Delete"`
				}{},
				resource: "resource",
			},
			wantErr: true,
		},
		{
			name: "New with permission on ignored field",
			args: args{
				v: struct {
					Field1 string `json:"field1"`
					Field2 string `json:"-" perm:"Read"`
				}{},
				resource: "resource",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := New(tt.args.v, tt.args.resource, tt.args.permissions...)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(ResourceSet{})); diff != "" {
				t.Errorf("New() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
