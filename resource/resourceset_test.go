package resource

import (
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

type ARequest struct {
	Field1 string `json:"field1"`
	Field2 string `json:"field2" perm:"Read"`
}

type BRequest struct {
	Field1 string `json:"field1"`
	Field2 string `json:"field2" perm:"Create"`
}

type CRequest struct {
	Field1 string `json:"field1"`
	Field2 string `json:"field2" perm:"Create,Update"`
}

type DRequest struct {
	Field1 string `json:"field1"`
	Field2 string `json:"field2" perm:"Read,Update"`
}

type ERequest struct {
	Field1 string `json:"field1"`
	Field2 string `json:"field2"`
}

type FRequest struct {
	Field1 string `json:"field1"`
	Field2 string `json:"field2" perm:"Delete"`
}

type GRequest struct {
	Field1 string `json:"field1"`
	Field2 string `json:"-" perm:"Read"`
}

type HRequest struct {
	Field1 string `json:"field1"`
	Field3 string `json:"field3"`
}

type AResource struct {
	Field1 string `json:"Field1"`
	Field2 string `json:"Field2"`
}

func (r AResource) Resource() accesstypes.Resource {
	return "AResources"
}

func TestNewResourceSet(t *testing.T) {
	t.Parallel()

	type args struct {
		permissions []accesstypes.Permission
	}
	tests := []struct {
		name    string
		args    args
		testFn  func(permissions ...accesstypes.Permission) (*ResourceSet, error)
		want    *ResourceSet
		wantErr bool
	}{
		{
			name:   "New only tag permissions",
			testFn: NewResourceSet[AResource, ARequest],
			want: &ResourceSet{
				permissions: []accesstypes.Permission{accesstypes.Read},
				requiredTagPerm: accesstypes.TagPermissions{
					"field2": {accesstypes.Read},
				},
				fieldToTag: map[accesstypes.Field]accesstypes.Tag{
					"Field2": "field2",
				},
				resource: "AResources",
			},
			wantErr: false,
		},
		{
			name: "New with permissions same as tag",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Read},
			},
			testFn: NewResourceSet[AResource, ARequest],
			want: &ResourceSet{
				permissions: []accesstypes.Permission{accesstypes.Read},
				requiredTagPerm: accesstypes.TagPermissions{
					"field2": {accesstypes.Read},
				},
				fieldToTag: map[accesstypes.Field]accesstypes.Tag{
					"Field2": "field2",
				},
				resource: "AResources",
			},
			wantErr: false,
		},
		{
			name: "New with additional permissions",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update},
			},
			testFn: NewResourceSet[AResource, BRequest],
			want: &ResourceSet{
				permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update},
				requiredTagPerm: accesstypes.TagPermissions{
					"field2": {accesstypes.Create},
				},
				fieldToTag: map[accesstypes.Field]accesstypes.Tag{
					"Field2": "field2",
				},
				resource: "AResources",
			},
			wantErr: false,
		},
		{
			name:   "New with multiple permissions",
			testFn: NewResourceSet[AResource, CRequest],
			want: &ResourceSet{
				permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update},
				requiredTagPerm: accesstypes.TagPermissions{
					"field2": {accesstypes.Create, accesstypes.Update},
				},
				fieldToTag: map[accesstypes.Field]accesstypes.Tag{
					"Field2": "field2",
				},
				resource: "AResources",
			},
			wantErr: false,
		},
		{
			name:    "New with invalid permission mix on tags",
			testFn:  NewResourceSet[AResource, DRequest],
			wantErr: true,
		},
		{
			name:   "New with invalid permission mix on input",
			testFn: NewResourceSet[AResource, ERequest],
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Read, accesstypes.Update},
			},
			wantErr: true,
		},
		{
			name:    "New with invalid Delete permission",
			testFn:  NewResourceSet[AResource, FRequest],
			wantErr: true,
		},
		{
			name:    "New with permission on ignored field",
			testFn:  NewResourceSet[AResource, GRequest],
			wantErr: true,
		},
		{
			name:    "New with Resource that can not convert to Request",
			testFn:  NewResourceSet[AResource, HRequest],
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.testFn(tt.args.permissions...)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewResourceSet() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(ResourceSet{})); diff != "" {
				t.Errorf("NewResourceSet() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
