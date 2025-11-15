package resource

import (
	"testing"
	"time"

	"github.com/cccteam/ccc"
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
	Field2 string `json:"-"      perm:"Read"`
}

type HRequest struct {
	Field1 string `json:"field1"`
	Field3 string `json:"field3"`
}

type AResource struct {
	ID   int    `spanner:"ID"`
	Name string `spanner:"Name"`
}

func (r AResource) Resource() accesstypes.Resource {
	return "AResources"
}

func (r AResource) DefaultConfig() Config {
	return defaultConfig
}

func TestNewSet(t *testing.T) {
	t.Parallel()

	type args struct {
		permissions []accesstypes.Permission
	}
	tests := []struct {
		name   string
		args   args
		testFn func(t *testing.T, name string, permissions []accesstypes.Permission, w wantResourceSetRun)
		wants  wantResourceSetRun
	}{
		{
			name:   "New only tag permissions",
			testFn: testNewSetRun[AResource, ARequest],
			wants: wantResourceSetRun{
				wantPermissions: []accesstypes.Permission{accesstypes.Read},
				requiredTagPerm: accesstypes.TagPermissions{"field2": {accesstypes.Read}},
				fieldToTag:      map[accesstypes.Field]accesstypes.Tag{"Field2": "field2"},
				immutableFields: map[accesstypes.Tag]struct{}{},
			},
		},
		{
			name: "New with permissions same as tag",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Read},
			},
			testFn: testNewSetRun[AResource, ARequest],
			wants: wantResourceSetRun{
				wantPermissions: []accesstypes.Permission{accesstypes.Read},
				requiredTagPerm: accesstypes.TagPermissions{"field2": {accesstypes.Read}},
				fieldToTag:      map[accesstypes.Field]accesstypes.Tag{"Field2": "field2"},
				immutableFields: map[accesstypes.Tag]struct{}{},
			},
		},
		{
			name: "New with additional permissions",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update},
			},
			testFn: testNewSetRun[AResource, BRequest],
			wants: wantResourceSetRun{
				wantPermissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update},
				requiredTagPerm: accesstypes.TagPermissions{"field2": {accesstypes.Create}},
				fieldToTag:      map[accesstypes.Field]accesstypes.Tag{"Field2": "field2"},
				immutableFields: map[accesstypes.Tag]struct{}{},
			},
		},
		{
			name:   "New with multiple permissions",
			testFn: testNewSetRun[AResource, CRequest],
			wants: wantResourceSetRun{
				wantPermissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update},
				requiredTagPerm: accesstypes.TagPermissions{"field2": {accesstypes.Create, accesstypes.Update}},
				fieldToTag:      map[accesstypes.Field]accesstypes.Tag{"Field2": "field2"},
				immutableFields: map[accesstypes.Tag]struct{}{},
			},
		},
		{
			name:   "New with invalid permission mix on tags",
			testFn: testNewSetRun[AResource, DRequest],
			wants: wantResourceSetRun{
				wantErr: true,
			},
		},
		{
			name:   "New with invalid permission mix on input",
			testFn: testNewSetRun[AResource, ERequest],
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Read, accesstypes.Update},
			},
			wants: wantResourceSetRun{
				wantErr: true,
			},
		},
		{
			name:   "New with invalid Delete permission",
			testFn: testNewSetRun[AResource, FRequest],
			wants: wantResourceSetRun{
				wantErr: true,
			},
		},
		{
			name:   "New with permission on ignored field",
			testFn: testNewSetRun[AResource, GRequest],
			wants: wantResourceSetRun{
				wantErr: true,
			},
		},
	}
	for _, tt := range tests {
		tt.testFn(t, tt.name, tt.args.permissions, tt.wants)
	}
}

type wantResourceSetRun struct {
	wantPermissions []accesstypes.Permission
	requiredTagPerm accesstypes.TagPermissions
	fieldToTag      map[accesstypes.Field]accesstypes.Tag
	immutableFields map[accesstypes.Tag]struct{}
	wantErr         bool
}

func testNewSetRun[Resource Resourcer, Request any](t *testing.T, name string, permissions []accesstypes.Permission, w wantResourceSetRun) {
	var want *Set[Resource]
	if !w.wantErr {
		want = &Set[Resource]{
			permissions:     w.wantPermissions,
			requiredJSONTagPerm: w.requiredTagPerm,
			fieldToJSONTag:      w.fieldToTag,
			immutableFields: w.immutableFields,
			rMeta:           NewMetadata[Resource](),
		}
	}

	t.Run(name, func(t *testing.T) {
		t.Parallel()
		got, err := NewSet[Resource, Request](permissions...)
		if (err != nil) != w.wantErr {
			t.Errorf("NewSet() error = %v, wantErr %v", err, w.wantErr)
			return
		}
		if !collectResourcePermissions {
			if diff := cmp.Diff(want, got, cmp.AllowUnexported(Set[Resource]{}, Metadata[Resource]{}, dbFieldMetadata{})); diff != "" {
				t.Errorf("NewSet() mismatch (-want +got):\n%s", diff)
			}
		}
	})
}

type DoeInstitution struct {
	ID                    ccc.UUID   `spanner:"Id"`
	InstitutionExternalID string     `spanner:"InstitutionExternalId" query:"uniqueIndex,partitioned"`
	DoeInstitutionTypeID  string     `spanner:"DoeInstitutionTypeId"  query:"linked:DoeInstitutionTypes"`
	Name                  string     `spanner:"Name"`
	Alias                 *string    `spanner:"Alias"`
	Note                  *string    `spanner:"Note"`
	AddressLine1          *string    `spanner:"AddressLine1"`
	AddressLine2          *string    `spanner:"AddressLine2"`
	City                  *string    `spanner:"City"`
	StateAbbreviation     *string    `spanner:"StateAbbreviation"`
	ZipCode               *string    `spanner:"ZipCode"`
	ZipCodeSuffix         *string    `spanner:"ZipCodeSuffix"`
	Active                bool       `spanner:"Active"`
	CreatedAt             time.Time  `spanner:"CreatedAt"`
	UpdatedAt             *time.Time `spanner:"UpdatedAt"`
}

const DoeInstitutions accesstypes.Resource = "DoeInstitutions"

func (DoeInstitution) Resource() accesstypes.Resource {
	return DoeInstitutions
}

func (DoeInstitution) DefaultConfig() Config {
	return defaultConfig
}

var defaultConfig = Config{
	ChangeTrackingTable: "DataChangeEvents",
	TrackChanges:        true,
}

func BenchmarkNewMetadata(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = NewMetadata[DoeInstitution]()
	}
}
