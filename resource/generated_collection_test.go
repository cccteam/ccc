package resource

import (
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestNewSetData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		fields      []FieldTags
		permissions []accesstypes.Permission
		want        SetData
		wantErr     bool
	}{
		{
			name: "list request struct",
			fields: []FieldTags{
				{Field: "ID", JSON: "id", Perm: "List"},
				{Field: "Name", JSON: "name"},
				{Field: "Secret", JSON: "-", Perm: ""},
			},
			permissions: []accesstypes.Permission{accesstypes.List},
			want: SetData{
				Permissions: []accesstypes.Permission{accesstypes.List},
				TagPermissions: accesstypes.TagPermissions{
					"id":   {accesstypes.List},
					"name": {accesstypes.NullPermission},
				},
				ImmutableFields: map[accesstypes.Tag]struct{}{},
			},
		},
		{
			name: "patch request struct with immutable field",
			fields: []FieldTags{
				{Field: "Name", JSON: "name", Perm: "Create,Update"},
				{Field: "Code", JSON: "code", Immutable: true},
			},
			permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update, accesstypes.Delete},
			want: SetData{
				Permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Delete, accesstypes.Update},
				TagPermissions: accesstypes.TagPermissions{
					"name": {accesstypes.Create, accesstypes.Update},
					"code": {accesstypes.Update},
				},
				ImmutableFields: map[accesstypes.Tag]struct{}{"code": {}},
			},
		},
		{
			name: "delete permission in tag is rejected",
			fields: []FieldTags{
				{Field: "Name", JSON: "name", Perm: "Delete"},
			},
			permissions: []accesstypes.Permission{accesstypes.Create},
			wantErr:     true,
		},
		{
			name: "mixed mutating and non-mutating permissions are rejected",
			fields: []FieldTags{
				{Field: "Name", JSON: "name", Perm: "Read"},
			},
			permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update, accesstypes.Delete},
			wantErr:     true,
		},
		{
			name: "permission on field without json tag is rejected",
			fields: []FieldTags{
				{Field: "Name", JSON: "-", Perm: "Read"},
			},
			permissions: []accesstypes.Permission{accesstypes.Read},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewSetData(tt.fields, tt.permissions...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewSetData() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("NewSetData() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCollectionBuilder_Data(t *testing.T) {
	t.Parallel()

	b := NewCollectionBuilder()

	listSet, err := NewSetData([]FieldTags{
		{Field: "ID", JSON: "id", Perm: "List"},
		{Field: "Name", JSON: "name"},
	}, accesstypes.List)
	if err != nil {
		t.Fatalf("NewSetData() error = %v", err)
	}
	readSet, err := NewSetData([]FieldTags{
		{Field: "ID", JSON: "id", Perm: "Read"},
		{Field: "Name", JSON: "name"},
	}, accesstypes.Read)
	if err != nil {
		t.Fatalf("NewSetData() error = %v", err)
	}
	patchSet, err := NewSetData([]FieldTags{
		{Field: "Name", JSON: "name", Perm: "Create,Update"},
		{Field: "Code", JSON: "code", Immutable: true},
	}, accesstypes.Create, accesstypes.Update, accesstypes.Delete)
	if err != nil {
		t.Fatalf("NewSetData() error = %v", err)
	}

	scope := accesstypes.GlobalPermissionScope
	for _, set := range []SetData{listSet, readSet, patchSet} {
		if err := b.AddResourceSet(scope, "Widgets", set); err != nil {
			t.Fatalf("AddResourceSet() error = %v", err)
		}
	}

	if err := b.AddMethodResource(scope, accesstypes.Execute, "DoThing"); err != nil {
		t.Fatalf("AddMethodResource() error = %v", err)
	}

	// Manual registration allows duplicates; Data must deduplicate them.
	for range 2 {
		if err := b.AddResource(scope, accesstypes.Read, "ManualThing"); err != nil {
			t.Fatalf("AddResource() error = %v", err)
		}
	}

	want := CollectionData{Resources: []CollectionResource{
		{
			Name:        "DoThing",
			Scope:       scope,
			Permissions: []accesstypes.Permission{accesstypes.Execute},
		},
		{
			Name:        "ManualThing",
			Scope:       scope,
			Permissions: []accesstypes.Permission{accesstypes.Read},
		},
		{
			Name:        "Widgets",
			Scope:       scope,
			Permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Delete, accesstypes.List, accesstypes.Read, accesstypes.Update},
			Tags: []TagData{
				{Name: "code", Permissions: []accesstypes.Permission{accesstypes.Update}},
				{Name: "id", Permissions: []accesstypes.Permission{accesstypes.List, accesstypes.Read}},
				{Name: "name", Permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update}},
			},
			ImmutableTags: []accesstypes.Tag{"code"},
		},
	}}

	if diff := cmp.Diff(want, b.Data()); diff != "" {
		t.Errorf("CollectionBuilder.Data() mismatch (-want +got):\n%s", diff)
	}
}

func TestCollectionBuilder_duplicateErrors(t *testing.T) {
	t.Parallel()

	scope := accesstypes.GlobalPermissionScope

	tests := []struct {
		name string
		// run performs any valid setup on b (failing the test on setup errors) and
		// returns the error from the offending call.
		run func(t *testing.T, b *CollectionBuilder) error
	}{
		{
			name: "duplicate resource permission across sets",
			run: func(t *testing.T, b *CollectionBuilder) error {
				t.Helper()
				set, err := NewSetData([]FieldTags{{Field: "ID", JSON: "id"}}, accesstypes.List)
				if err != nil {
					t.Fatalf("NewSetData() error = %v", err)
				}
				if err := b.AddResourceSet(scope, "Widgets", set); err != nil {
					t.Fatalf("AddResourceSet() error = %v", err)
				}

				return b.AddResourceSet(scope, "Widgets", set)
			},
		},
		{
			name: "duplicate method resource",
			run: func(t *testing.T, b *CollectionBuilder) error {
				t.Helper()
				if err := b.AddMethodResource(scope, accesstypes.Execute, "DoThing"); err != nil {
					t.Fatalf("AddMethodResource() error = %v", err)
				}

				return b.AddMethodResource(scope, accesstypes.Execute, "DoThing")
			},
		},
		{
			name: "null permission",
			run: func(t *testing.T, b *CollectionBuilder) error {
				t.Helper()

				return b.AddResource(scope, accesstypes.NullPermission, "Widgets")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := tt.run(t, NewCollectionBuilder()); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestNewGeneratedCollection_validation(t *testing.T) {
	t.Parallel()

	scope := accesstypes.GlobalPermissionScope

	tests := []struct {
		name    string
		data    CollectionData
		wantErr bool
	}{
		{
			name: "valid",
			data: CollectionData{Resources: []CollectionResource{{
				Name:        "Widgets",
				Scope:       scope,
				Permissions: []accesstypes.Permission{accesstypes.List},
				Tags:        []TagData{{Name: "id", Permissions: []accesstypes.Permission{accesstypes.List}}, {Name: "name", Permissions: []accesstypes.Permission{}}},
			}}},
		},
		{
			name:    "empty resource name",
			data:    CollectionData{Resources: []CollectionResource{{Scope: scope}}},
			wantErr: true,
		},
		{
			name:    "empty scope",
			data:    CollectionData{Resources: []CollectionResource{{Name: "Widgets"}}},
			wantErr: true,
		},
		{
			name: "duplicate resource",
			data: CollectionData{Resources: []CollectionResource{
				{Name: "Widgets", Scope: scope, Permissions: []accesstypes.Permission{accesstypes.List}},
				{Name: "Widgets", Scope: scope, Permissions: []accesstypes.Permission{accesstypes.Read}},
			}},
			wantErr: true,
		},
		{
			name: "null resource permission",
			data: CollectionData{Resources: []CollectionResource{{
				Name: "Widgets", Scope: scope, Permissions: []accesstypes.Permission{accesstypes.NullPermission},
			}}},
			wantErr: true,
		},
		{
			name: "duplicate tag",
			data: CollectionData{Resources: []CollectionResource{{
				Name: "Widgets", Scope: scope,
				Tags: []TagData{{Name: "id"}, {Name: "id"}},
			}}},
			wantErr: true,
		},
		{
			name: "duplicate tag permission",
			data: CollectionData{Resources: []CollectionResource{{
				Name: "Widgets", Scope: scope,
				Tags: []TagData{{Name: "id", Permissions: []accesstypes.Permission{accesstypes.List, accesstypes.List}}},
			}}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewGeneratedCollection(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewGeneratedCollection() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGeneratedCollection_roundTrip(t *testing.T) {
	t.Parallel()

	b := NewCollectionBuilder()
	set, err := NewSetData([]FieldTags{
		{Field: "ID", JSON: "id", Perm: "List"},
		{Field: "Name", JSON: "name"},
	}, accesstypes.List)
	if err != nil {
		t.Fatalf("NewSetData() error = %v", err)
	}
	if err := b.AddResourceSet(accesstypes.GlobalPermissionScope, "Widgets", set); err != nil {
		t.Fatalf("AddResourceSet() error = %v", err)
	}
	if err := b.AddMethodResource(accesstypes.GlobalPermissionScope, accesstypes.Execute, "DoThing"); err != nil {
		t.Fatalf("AddMethodResource() error = %v", err)
	}

	data := b.Data()
	g, err := NewGeneratedCollection(data)
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	if diff := cmp.Diff(data, g.Data()); diff != "" {
		t.Errorf("GeneratedCollection.Data() round trip mismatch (-want +got):\n%s", diff)
	}
}

func TestGeneratedCollection_readMethods(t *testing.T) {
	t.Parallel()

	// Build the same collection through both CollectionBuilder exit paths -
	// GeneratedCollection() directly, and Data() round-tripped through
	// NewGeneratedCollection - and require every read method to agree, so the two paths
	// production code takes (in-run vs. deserialized-from-generated-code) can never
	// diverge.
	listSet, err := NewSetData([]FieldTags{
		{Field: "ID", JSON: "id", Perm: "List"},
		{Field: "Name", JSON: "name"},
	}, accesstypes.List)
	if err != nil {
		t.Fatalf("NewSetData() error = %v", err)
	}
	patchSet, err := NewSetData([]FieldTags{
		{Field: "Name", JSON: "name", Perm: "Create,Update"},
		{Field: "Code", JSON: "code", Immutable: true},
	}, accesstypes.Create, accesstypes.Update, accesstypes.Delete)
	if err != nil {
		t.Fatalf("NewSetData() error = %v", err)
	}

	b := NewCollectionBuilder()
	if err := b.AddResourceSet(accesstypes.GlobalPermissionScope, "Widgets", listSet); err != nil {
		t.Fatalf("registering list set: %v", err)
	}
	if err := b.AddResourceSet(accesstypes.GlobalPermissionScope, "Widgets", patchSet); err != nil {
		t.Fatalf("registering patch set: %v", err)
	}
	if err := b.AddMethodResource(accesstypes.GlobalPermissionScope, accesstypes.Execute, "DoThing"); err != nil {
		t.Fatalf("AddMethodResource() error = %v", err)
	}

	direct := b.GeneratedCollection()
	fromData, err := NewGeneratedCollection(b.Data())
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	if diff := cmp.Diff(direct.Resources(), fromData.Resources()); diff != "" {
		t.Errorf("Resources() mismatch (-direct +fromData):\n%s", diff)
	}
	// List()'s value ordering follows map iteration and is not deterministic; its
	// consumers are order-insensitive.
	if diff := cmp.Diff(direct.List(), fromData.List(), cmpopts.SortSlices(func(a, b accesstypes.Resource) bool { return a < b })); diff != "" {
		t.Errorf("List() mismatch (-direct +fromData):\n%s", diff)
	}
	if diff := cmp.Diff(direct.TypescriptData(), fromData.TypescriptData()); diff != "" {
		t.Errorf("TypescriptData() mismatch (-direct +fromData):\n%s", diff)
	}
	if got, want := fromData.Scope("Widgets"), direct.Scope("Widgets"); got != want {
		t.Errorf("Scope() = %v, want %v", got, want)
	}
	if !direct.ResourceExists("Widgets") {
		t.Error("ResourceExists(Widgets) = false, want true")
	}
	if direct.ResourceExists("DoThing") {
		t.Error("ResourceExists(DoThing) = true, want false (Execute-only resources are methods)")
	}
	if !direct.IsResourceImmutable(accesstypes.GlobalPermissionScope, accesstypes.Resource("Widgets").ResourceWithTag("code")) {
		t.Error("IsResourceImmutable(Widgets.code) = false, want true")
	}
	if direct.IsResourceImmutable(accesstypes.GlobalPermissionScope, accesstypes.Resource("Widgets").ResourceWithTag("name")) {
		t.Error("IsResourceImmutable(Widgets.name) = true, want false")
	}
}

func TestGeneratedCollection_HasPermission(t *testing.T) {
	t.Parallel()

	g, err := NewGeneratedCollection(CollectionData{Resources: []CollectionResource{
		{Name: "Widgets", Scope: accesstypes.GlobalPermissionScope, Permissions: []accesstypes.Permission{accesstypes.Read}},
	}})
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	tests := []struct {
		name       string
		scope      accesstypes.PermissionScope
		permission accesstypes.Permission
		resource   accesstypes.Resource
		want       bool
	}{
		{name: "registered permission", scope: accesstypes.GlobalPermissionScope, permission: accesstypes.Read, resource: "Widgets", want: true},
		{name: "unregistered permission", scope: accesstypes.GlobalPermissionScope, permission: accesstypes.Update, resource: "Widgets", want: false},
		{name: "wrong scope", scope: accesstypes.DomainPermissionScope, permission: accesstypes.Read, resource: "Widgets", want: false},
		{name: "unknown resource", scope: accesstypes.GlobalPermissionScope, permission: accesstypes.Read, resource: "Missing", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := g.HasPermission(tt.scope, tt.permission, tt.resource); got != tt.want {
				t.Errorf("HasPermission(%s, %s, %s) = %v, want %v", tt.scope, tt.permission, tt.resource, got, tt.want)
			}
		})
	}
}
