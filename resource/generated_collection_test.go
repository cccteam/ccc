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

	// Build the equivalent collection through both population paths and require every
	// read method to agree: GeneratedCollection must be indistinguishable from a
	// runtime-populated Collection to its consumers (MigrateRoles, TypeScript
	// generation).
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

	runtime := newPopulatableCollection()
	b := NewCollectionBuilder()
	for _, apply := range []func(scope accesstypes.PermissionScope, res accesstypes.Resource, set SetData) error{
		func(scope accesstypes.PermissionScope, res accesstypes.Resource, set SetData) error {
			return runtime.addResourceSet(scope, res, set.Permissions, set.TagPermissions, set.ImmutableFields)
		},
		b.AddResourceSet,
	} {
		if err := apply(accesstypes.GlobalPermissionScope, "Widgets", listSet); err != nil {
			t.Fatalf("registering list set: %v", err)
		}
		if err := apply(accesstypes.GlobalPermissionScope, "Widgets", patchSet); err != nil {
			t.Fatalf("registering patch set: %v", err)
		}
	}
	if err := runtime.addResource(false, accesstypes.GlobalPermissionScope, accesstypes.Execute, "DoThing"); err != nil {
		t.Fatalf("addResource() error = %v", err)
	}
	if err := b.AddMethodResource(accesstypes.GlobalPermissionScope, accesstypes.Execute, "DoThing"); err != nil {
		t.Fatalf("AddMethodResource() error = %v", err)
	}

	g, err := NewGeneratedCollection(b.Data())
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	if diff := cmp.Diff(runtime.Resources(), g.Resources()); diff != "" {
		t.Errorf("Resources() mismatch (-runtime +generated):\n%s", diff)
	}
	// List()'s value ordering follows map iteration and is not deterministic; its
	// consumers are order-insensitive.
	if diff := cmp.Diff(runtime.List(), g.List(), cmpopts.SortSlices(func(a, b accesstypes.Resource) bool { return a < b })); diff != "" {
		t.Errorf("List() mismatch (-runtime +generated):\n%s", diff)
	}
	if diff := cmp.Diff(runtime.TypescriptData(), g.TypescriptData()); diff != "" {
		t.Errorf("TypescriptData() mismatch (-runtime +generated):\n%s", diff)
	}
	if got, want := g.Scope("Widgets"), runtime.Scope("Widgets"); got != want {
		t.Errorf("Scope() = %v, want %v", got, want)
	}
	if !g.ResourceExists("Widgets") {
		t.Error("ResourceExists(Widgets) = false, want true")
	}
	if g.ResourceExists("DoThing") {
		t.Error("ResourceExists(DoThing) = true, want false (Execute-only resources are methods)")
	}
	if !g.IsResourceImmutable(accesstypes.GlobalPermissionScope, accesstypes.Resource("Widgets").ResourceWithTag("code")) {
		t.Error("IsResourceImmutable(Widgets.code) = false, want true")
	}
	if g.IsResourceImmutable(accesstypes.GlobalPermissionScope, accesstypes.Resource("Widgets").ResourceWithTag("name")) {
		t.Error("IsResourceImmutable(Widgets.name) = true, want false")
	}

	if diffs := DiffCollections(runtime, g); len(diffs) != 0 {
		t.Errorf("DiffCollections() = %v, want empty", diffs)
	}
}

// TestDiffCollections pins the drift report between a runtime-populated Collection and
// a GeneratedCollection: resources absent from one side, per-resource-per-direction
// grouping, and the manual-registration provenance carried on runtime-only entries.
func TestDiffCollections(t *testing.T) {
	t.Parallel()

	scope := accesstypes.GlobalPermissionScope

	tests := []struct {
		name string
		// runtime populates the runtime-path collection, failing the test on errors.
		runtime func(t *testing.T) *Collection
		data    CollectionData
		want    []CollectionDiff
	}{
		{
			name: "resources absent from either side are reported with their direction",
			runtime: func(t *testing.T) *Collection {
				t.Helper()
				c := newPopulatableCollection()
				if err := c.addResource(false, scope, accesstypes.Read, "Widgets"); err != nil {
					t.Fatalf("addResource() error = %v", err)
				}
				if err := c.addResource(false, scope, accesstypes.Execute, "RuntimeOnly"); err != nil {
					t.Fatalf("addResource() error = %v", err)
				}

				return c
			},
			data: CollectionData{Resources: []CollectionResource{
				{Name: "Widgets", Scope: scope, Permissions: []accesstypes.Permission{accesstypes.Read}},
				{Name: "GeneratedOnly", Scope: scope, Permissions: []accesstypes.Permission{accesstypes.List}},
			}},
			want: []CollectionDiff{
				{Resource: "GeneratedOnly", Scope: scope, RuntimeOnly: false, Permissions: []accesstypes.Permission{accesstypes.List}},
				{Resource: "RuntimeOnly", Scope: scope, RuntimeOnly: true, Permissions: []accesstypes.Permission{accesstypes.Execute}},
			},
		},
		{
			// A resource absent from one side yields a single group carrying its
			// permissions and tags together, while a resource present on both sides
			// with a matching permission reports only its field-level drift.
			name: "differences group per resource and per direction",
			runtime: func(t *testing.T) *Collection {
				t.Helper()
				c := newPopulatableCollection()
				if err := c.addResourceSet(scope, "Contacts", []accesstypes.Permission{accesstypes.List, accesstypes.Read}, accesstypes.TagPermissions{
					"id":    {accesstypes.NullPermission},
					"email": {accesstypes.Read},
				}, nil); err != nil {
					t.Fatalf("addResourceSet() error = %v", err)
				}
				if err := c.addResourceSet(scope, "Widgets", []accesstypes.Permission{accesstypes.List}, accesstypes.TagPermissions{
					"id":     {accesstypes.NullPermission},
					"secret": {accesstypes.List},
				}, nil); err != nil {
					t.Fatalf("addResourceSet() error = %v", err)
				}

				return c
			},
			data: CollectionData{Resources: []CollectionResource{
				{
					Name:        "Widgets",
					Scope:       scope,
					Permissions: []accesstypes.Permission{accesstypes.List},
					Tags: []TagData{
						{Name: "id"},
						{Name: "secret"},
					},
				},
			}},
			want: []CollectionDiff{
				{
					// Absent from the generated collection entirely: one group carries
					// the permissions and every tag.
					Resource:    "Contacts",
					Scope:       scope,
					RuntimeOnly: true,
					Permissions: []accesstypes.Permission{accesstypes.List, accesstypes.Read},
					Tags:        []accesstypes.Tag{"email", "id"},
				},
				{
					// Permission and tags match; only the per-tag permission drifts.
					Resource:       "Widgets",
					Scope:          scope,
					RuntimeOnly:    true,
					TagPermissions: map[accesstypes.Tag][]accesstypes.Permission{"secret": {accesstypes.List}},
				},
			},
		},
		{
			// Runtime-only entries recorded through the deprecated AddResource carry
			// the provenance the migration hints render as exact annotations.
			name: "manual registrations carry provenance",
			runtime: func(t *testing.T) *Collection {
				t.Helper()
				c := newPopulatableCollection()
				for _, reg := range []ManualRegistration{
					{Scope: accesstypes.GlobalPermissionScope, Permission: accesstypes.Execute, Resource: "UploadThings"},
					{Scope: accesstypes.DomainPermissionScope, Permission: accesstypes.Read, Resource: "ScopedThings"},
				} {
					if err := c.addResource(true, reg.Scope, reg.Permission, reg.Resource); err != nil {
						t.Fatalf("addResource() error = %v", err)
					}
					c.recordManualRegistration(reg.Scope, reg.Permission, reg.Resource)
				}

				return c
			},
			want: []CollectionDiff{
				{
					Resource:    "ScopedThings",
					Scope:       accesstypes.DomainPermissionScope,
					RuntimeOnly: true,
					Permissions: []accesstypes.Permission{accesstypes.Read},
					ManualRegistrations: []ManualRegistration{
						{Scope: accesstypes.DomainPermissionScope, Permission: accesstypes.Read, Resource: "ScopedThings"},
					},
				},
				{
					Resource:    "UploadThings",
					Scope:       accesstypes.GlobalPermissionScope,
					RuntimeOnly: true,
					Permissions: []accesstypes.Permission{accesstypes.Execute},
					ManualRegistrations: []ManualRegistration{
						{Scope: accesstypes.GlobalPermissionScope, Permission: accesstypes.Execute, Resource: "UploadThings"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, err := NewGeneratedCollection(tt.data)
			if err != nil {
				t.Fatalf("NewGeneratedCollection() error = %v", err)
			}

			if diff := cmp.Diff(tt.want, DiffCollections(tt.runtime(t), g)); diff != "" {
				t.Errorf("DiffCollections() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestManualRegistration_Annotation pins the exact @manualAddResource annotation text
// the migration hints tell users to add.
func TestManualRegistration_Annotation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		reg  ManualRegistration
		want string
	}{
		{
			name: "explicit scope is spelled out",
			reg:  ManualRegistration{Scope: accesstypes.DomainPermissionScope, Permission: accesstypes.Read, Resource: "ScopedThings"},
			want: "@manualAddResource(Read, domain)",
		},
		{
			name: "global scope is omitted as the default",
			reg:  ManualRegistration{Scope: accesstypes.GlobalPermissionScope, Permission: accesstypes.Execute, Resource: "UploadThings"},
			want: "@manualAddResource(Execute)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.reg.Annotation(); got != tt.want {
				t.Errorf("Annotation() = %q, want %q", got, tt.want)
			}
		})
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

func TestCollection_ManualRegistrations(t *testing.T) {
	t.Parallel()

	c := newPopulatableCollection()
	regs := []ManualRegistration{
		{Scope: accesstypes.GlobalPermissionScope, Permission: accesstypes.Execute, Resource: "UploadThings"},
		{Scope: accesstypes.DomainPermissionScope, Permission: accesstypes.Read, Resource: "ScopedThings"},
	}
	for _, reg := range regs {
		if err := c.addResource(true, reg.Scope, reg.Permission, reg.Resource); err != nil {
			t.Fatalf("addResource() error = %v", err)
		}
		c.recordManualRegistration(reg.Scope, reg.Permission, reg.Resource)
	}
	// Duplicate manual registrations collapse to one record.
	c.recordManualRegistration(accesstypes.GlobalPermissionScope, accesstypes.Execute, "UploadThings")

	want := []ManualRegistration{
		{Scope: accesstypes.DomainPermissionScope, Permission: accesstypes.Read, Resource: "ScopedThings"},
		{Scope: accesstypes.GlobalPermissionScope, Permission: accesstypes.Execute, Resource: "UploadThings"},
	}
	if diff := cmp.Diff(want, c.ManualRegistrations()); diff != "" {
		t.Errorf("ManualRegistrations() mismatch (-want +got):\n%s", diff)
	}
}
