// package resourceset is a set of resources that provides a way to map permissions to fields in a struct.
package resourceset

import (
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

func TestNew(t *testing.T) {
	type args struct {
		v                  any
		resource           accesstypes.Resource
		requiredPermission accesstypes.Permission
	}
	tests := []struct {
		name    string
		args    args
		want    *ResourceSet
		wantErr bool
	}{
		{
			name: "New",
			args: args{
				v: struct {
					Field1 string `json:"field1"`
					Field2 string `json:"field2" perm:"required"`
				}{},
				resource:           "resource",
				requiredPermission: accesstypes.Read,
			},
			want: &ResourceSet{
				requiredPermission: accesstypes.Read,
				requiredTagPerm: accesstypes.TagPermission{
					"field2": accesstypes.Read,
				},
				fieldToTag: map[accesstypes.Field]accesstypes.Tag{
					"Field2": "field2",
				},
				resource: "resource",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := New(tt.args.v, tt.args.resource, tt.args.requiredPermission)
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
