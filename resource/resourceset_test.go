package resource

import (
	"reflect"
	"testing"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

// ARequest is the plain structural fixture: an exempt primary key plus two enforced fields.
type ARequest struct {
	ID     string `json:"id"     perm:"-"`
	Field1 string `json:"field1"`
	Field2 string `json:"field2"`
}

// BRequest carries a stale pre-flip permission tag; NewSet must reject it at construction.
type BRequest struct {
	Field1 string `json:"field1"`
	Field2 string `json:"field2" perm:"Read"`
}

// CRequest carries a stale pre-flip multi-permission tag.
type CRequest struct {
	Field1 string `json:"field1"`
	Field2 string `json:"field2" perm:"Create,Update"`
}

// DRequest has an enforced field without a json tag, leaving it client-addressable by
// Go field name — a fail-open remnant NewSet must reject.
type DRequest struct {
	Field1 string `json:"field1"`
	Field2 string
}

// ERequest has no enforced fields: an exempt primary key and a json-hidden field.
type ERequest struct {
	ID     string `json:"id" perm:"-"`
	Hidden string `json:"-"`
}

// FRequest carries a stale pre-flip Delete permission tag.
type FRequest struct {
	Field1 string `json:"field1"`
	Field2 string `json:"field2" perm:"Delete"`
}

// GRequest carries a permission tag on a json-hidden field.
type GRequest struct {
	Field1 string `json:"field1"`
	Field2 string `json:"-"      perm:"Read"`
}

// IRequest has an immutable field; at runtime it is enforced like any other field, while
// NewSetData strips its Update grantability.
type IRequest struct {
	ID   string `json:"-"`
	Code string `json:"code" immutable:"true"`
	Name string `json:"name"`
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
			name: "structural enforcement with a non-mutating permission",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.List},
			},
			testFn: testNewSetRun[AResource, ARequest],
			wants: wantResourceSetRun{
				wantPermissions: []accesstypes.Permission{accesstypes.List},
				requiredTagPerm: accesstypes.TagPermissions{"field1": {accesstypes.List}, "field2": {accesstypes.List}},
				fieldToTag:      map[accesstypes.Field]accesstypes.Tag{"Field1": "field1", "Field2": "field2"},
				immutableFields: map[accesstypes.Tag]struct{}{},
			},
		},
		{
			name: "structural enforcement with mutating permissions keeps Delete resource-level",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update, accesstypes.Delete},
			},
			testFn: testNewSetRun[AResource, ARequest],
			wants: wantResourceSetRun{
				wantPermissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Delete, accesstypes.Update},
				requiredTagPerm: accesstypes.TagPermissions{"field1": {accesstypes.Create, accesstypes.Update}, "field2": {accesstypes.Create, accesstypes.Update}},
				fieldToTag:      map[accesstypes.Field]accesstypes.Tag{"Field1": "field1", "Field2": "field2"},
				immutableFields: map[accesstypes.Tag]struct{}{},
			},
		},
		{
			name: "exempt-only struct registers nothing",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Read},
			},
			testFn: testNewSetRun[AResource, ERequest],
			wants: wantResourceSetRun{
				wantPermissions: []accesstypes.Permission{accesstypes.Read},
				requiredTagPerm: accesstypes.TagPermissions{},
				fieldToTag:      map[accesstypes.Field]accesstypes.Tag{},
				immutableFields: map[accesstypes.Tag]struct{}{},
			},
		},
		{
			name: "immutable field is enforced like any other at runtime",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update, accesstypes.Delete},
			},
			testFn: testNewSetRun[AResource, IRequest],
			wants: wantResourceSetRun{
				wantPermissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Delete, accesstypes.Update},
				requiredTagPerm: accesstypes.TagPermissions{"code": {accesstypes.Create, accesstypes.Update}, "name": {accesstypes.Create, accesstypes.Update}},
				fieldToTag:      map[accesstypes.Field]accesstypes.Tag{"Code": "code", "Name": "name"},
				immutableFields: map[accesstypes.Tag]struct{}{"code": {}},
			},
		},
		{
			name:   "zero permissions with enforced fields",
			testFn: testNewSetRun[AResource, ARequest],
			wants: wantResourceSetRun{
				wantErr: true,
			},
		},
		{
			name:   "zero permissions with exempt-only struct",
			testFn: testNewSetRun[AResource, ERequest],
			wants: wantResourceSetRun{
				wantErr: true,
			},
		},
		{
			name: "Delete-only permissions with enforced fields",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Delete},
			},
			testFn: testNewSetRun[AResource, ARequest],
			wants: wantResourceSetRun{
				wantErr: true,
			},
		},
		{
			name: "stale perm tag fails at construction",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Read},
			},
			testFn: testNewSetRun[AResource, BRequest],
			wants: wantResourceSetRun{
				wantErr: true,
			},
		},
		{
			name: "stale multi-permission tag fails at construction",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update},
			},
			testFn: testNewSetRun[AResource, CRequest],
			wants: wantResourceSetRun{
				wantErr: true,
			},
		},
		{
			name: "stale Delete tag fails at construction",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update},
			},
			testFn: testNewSetRun[AResource, FRequest],
			wants: wantResourceSetRun{
				wantErr: true,
			},
		},
		{
			name: "perm tag on json-hidden field fails at construction",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Read},
			},
			testFn: testNewSetRun[AResource, GRequest],
			wants: wantResourceSetRun{
				wantErr: true,
			},
		},
		{
			name: "enforced field without json tag fails at construction",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Read},
			},
			testFn: testNewSetRun[AResource, DRequest],
			wants: wantResourceSetRun{
				wantErr: true,
			},
		},
		{
			name: "invalid permission mix on input",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Read, accesstypes.Update},
			},
			testFn: testNewSetRun[AResource, ARequest],
			wants: wantResourceSetRun{
				wantErr: true,
			},
		},
		{
			name: "multiple non-mutating permissions on input",
			args: args{
				permissions: []accesstypes.Permission{accesstypes.Read, accesstypes.List},
			},
			testFn: testNewSetRun[AResource, ARequest],
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
			requiredTagPerm: w.requiredTagPerm,
			fieldToTag:      w.fieldToTag,
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
		reflectTypesByIdentity := cmp.Comparer(func(a, b reflect.Type) bool { return a == b })
		if diff := cmp.Diff(want, got, cmp.AllowUnexported(Set[Resource]{}, Metadata[Resource]{}, dbFieldMetadata{}), reflectTypesByIdentity); diff != "" {
			t.Errorf("NewSet() mismatch (-want +got):\n%s", diff)
		}
	})
}

type Organization struct {
	ID           ccc.UUID   `spanner:"Id"`
	Name         string     `spanner:"Name"`
	AddressLine1 *string    `spanner:"AddressLine1"`
	AddressLine2 *string    `spanner:"AddressLine2"`
	City         *string    `spanner:"City"`
	State        *string    `spanner:"State"`
	ZipCode      *string    `spanner:"ZipCode"`
	Active       bool       `spanner:"Active"`
	CreatedAt    time.Time  `spanner:"CreatedAt"`
	UpdatedAt    *time.Time `spanner:"UpdatedAt"`
}

func (Organization) Resource() accesstypes.Resource {
	return "Organizations"
}

func (Organization) DefaultConfig() Config {
	return defaultConfig
}

var defaultConfig = Config{
	ChangeTrackingTable: "DataChangeEvents",
	TrackChanges:        true,
}

func BenchmarkNewMetadata(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = NewMetadata[Organization]()
	}
}
