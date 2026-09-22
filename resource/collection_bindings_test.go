package resource

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

// TestGeneratedCollection_bindings pins the binding carriage: CollectionData
// round-trips through NewGeneratedCollection and Data() in canonical form
// (vocabulary sorted by name, hop order preserved), a binding-only resource
// survives with no permissions, and Bindings answers per resource.
func TestGeneratedCollection_bindings(t *testing.T) {
	t.Parallel()

	data := CollectionData{Resources: []CollectionResource{
		{
			Name:        "MaintenanceTasks",
			Scope:       accesstypes.DomainPermissionScope,
			Permissions: []accesstypes.Permission{accesstypes.Read},
			Attributes: []AttributeData{
				{Name: "crew", Column: "CrewId", Type: AttributeTypeString},
				{Name: "sector", Column: "BerthId", Type: AttributeTypeString, Path: []BindingHop{
					{Table: "Berths", JoinColumn: "Id", Column: "StationId"},
					{Table: "Stations", JoinColumn: "Id", Column: "Sector"},
				}},
			},
			Domain: &DomainBindingData{Column: "StationId"},
		},
		{
			// Binding-only: the vocabulary describes the data model, so a
			// resource with no permission registrations still carries it.
			Name:        "UserProfiles",
			Scope:       accesstypes.GlobalPermissionScope,
			SubjectSets: []SubjectBindingData{{Name: "crews", UserColumn: "UserId", Column: "CrewId", Type: AttributeTypeString}},
			SubjectValues: []SubjectBindingData{
				{Name: "approvalLimit", UserColumn: "UserId", Column: "Limit", Type: AttributeTypeNumber},
			},
		},
	}}

	g, err := NewGeneratedCollection(data)
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	bindings, ok := g.Bindings(accesstypes.DomainPermissionScope, "MaintenanceTasks")
	if !ok {
		t.Fatal("Bindings(MaintenanceTasks) ok = false, want true")
	}
	if len(bindings.Attributes) != 2 || bindings.Domain == nil {
		t.Errorf("Bindings(MaintenanceTasks) = %+v, want two attributes and the domain binding", bindings)
	}
	if _, ok := g.Bindings(accesstypes.DomainPermissionScope, "Berths"); ok {
		t.Error("Bindings(Berths) ok = true for a resource declaring none")
	}

	roundTripped, err := NewGeneratedCollection(g.Data())
	if err != nil {
		t.Fatalf("NewGeneratedCollection(round trip) error = %v", err)
	}
	if diff := cmp.Diff(g.Data(), roundTripped.Data()); diff != "" {
		t.Errorf("Data() round trip mismatch (-first +second):\n%s", diff)
	}

	profile, ok := roundTripped.Bindings(accesstypes.GlobalPermissionScope, "UserProfiles")
	if !ok {
		t.Fatal("Bindings(UserProfiles) ok = false after round trip, want true")
	}
	if len(profile.SubjectSets) != 1 || len(profile.SubjectValues) != 1 {
		t.Errorf("Bindings(UserProfiles) = %+v, want the subject vocabulary intact", profile)
	}
}

// TestGeneratedCollection_subjectComparisonTypes pins the typed subject
// accessors deploy validation reads: the comparison type of the column a set
// or value yields, and ok false for a name the collection does not declare in
// that form.
func TestGeneratedCollection_subjectComparisonTypes(t *testing.T) {
	t.Parallel()

	g, err := NewGeneratedCollection(CollectionData{Resources: []CollectionResource{{
		Name:        "UserProfiles",
		Scope:       accesstypes.GlobalPermissionScope,
		SubjectSets: []SubjectBindingData{{Name: "crews", UserColumn: "UserId", Column: "CrewId", Type: AttributeTypeString}},
		SubjectValues: []SubjectBindingData{
			{Name: "approvalLimit", UserColumn: "UserId", Column: "Limit", Type: AttributeTypeNumber},
			{Name: "homeSector", UserColumn: "UserId", Column: "StationId", Type: AttributeTypeString, Path: []BindingHop{{Table: "Stations", JoinColumn: "Id", Column: "Sector"}}},
		},
	}}})
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	tests := []struct {
		name     string
		lookup   func(string) (accesstypes.AttributeType, bool)
		arg      string
		wantType accesstypes.AttributeType
		wantOK   bool
	}{
		{name: "set carries its column's type", lookup: g.SubjectSetComparisonType, arg: "crews", wantType: AttributeTypeString, wantOK: true},
		{name: "value carries its column's type", lookup: g.SubjectValueComparisonType, arg: "approvalLimit", wantType: AttributeTypeNumber, wantOK: true},
		{name: "dotted value carries its terminal's type", lookup: g.SubjectValueComparisonType, arg: "homeSector", wantType: AttributeTypeString, wantOK: true},
		{name: "undeclared set", lookup: g.SubjectSetComparisonType, arg: "ghosts"},
		{name: "a value is not a set", lookup: g.SubjectSetComparisonType, arg: "approvalLimit"},
		{name: "a set is not a value", lookup: g.SubjectValueComparisonType, arg: "crews"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			typ, ok := tt.lookup(tt.arg)
			if typ != tt.wantType || ok != tt.wantOK {
				t.Errorf("lookup(%q) = (%q, %v), want (%q, %v)", tt.arg, typ, ok, tt.wantType, tt.wantOK)
			}
		})
	}
}

// TestNewGeneratedCollection_bindingValidation pins the runtime vocabulary
// rules: names unique within one resource, subject names unique across the
// whole collection, and every attribute and subject entry typed from the
// vocabulary.
func TestNewGeneratedCollection_bindingValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		data        CollectionData
		wantContain string
	}{
		{
			name: "duplicate name within one resource",
			data: CollectionData{Resources: []CollectionResource{{
				Name:  "Widgets",
				Scope: accesstypes.GlobalPermissionScope,
				Attributes: []AttributeData{
					{Name: "crew", Column: "CrewId", Type: AttributeTypeString},
					{Name: "crew", Column: "TeamId", Type: AttributeTypeString},
				},
			}}},
			wantContain: `binding name "crew" twice`,
		},
		{
			name: "attribute and subject name colliding on one resource",
			data: CollectionData{Resources: []CollectionResource{{
				Name:        "Widgets",
				Scope:       accesstypes.GlobalPermissionScope,
				Attributes:  []AttributeData{{Name: "crew", Column: "CrewId", Type: AttributeTypeString}},
				SubjectSets: []SubjectBindingData{{Name: "crew", UserColumn: "UserId", Column: "CrewId", Type: AttributeTypeString}},
			}}},
			wantContain: `binding name "crew" twice`,
		},
		{
			name: "subject name is one application-wide namespace",
			data: CollectionData{Resources: []CollectionResource{
				{
					Name:        "CrewMembers",
					Scope:       accesstypes.GlobalPermissionScope,
					SubjectSets: []SubjectBindingData{{Name: "crews", UserColumn: "UserId", Column: "CrewId", Type: AttributeTypeString}},
				},
				{
					Name:        "TeamMembers",
					Scope:       accesstypes.DomainPermissionScope,
					SubjectSets: []SubjectBindingData{{Name: "crews", UserColumn: "UserId", Column: "TeamId", Type: AttributeTypeString}},
				},
			}},
			wantContain: "application-wide namespace",
		},
		{
			name: "attribute with a type outside the vocabulary",
			data: CollectionData{Resources: []CollectionResource{{
				Name:       "Widgets",
				Scope:      accesstypes.GlobalPermissionScope,
				Attributes: []AttributeData{{Name: "crew", Column: "CrewId", Type: "uuid"}},
			}}},
			wantContain: `attribute "crew" carries comparison type "uuid"`,
		},
		{
			name: "subject set without a type",
			data: CollectionData{Resources: []CollectionResource{{
				Name:        "CrewMembers",
				Scope:       accesstypes.GlobalPermissionScope,
				SubjectSets: []SubjectBindingData{{Name: "crews", UserColumn: "UserId", Column: "CrewId"}},
			}}},
			wantContain: `subject binding "crews" carries comparison type ""`,
		},
		{
			name: "subject value with a type outside the vocabulary",
			data: CollectionData{Resources: []CollectionResource{{
				Name:          "UserProfiles",
				Scope:         accesstypes.GlobalPermissionScope,
				SubjectValues: []SubjectBindingData{{Name: "approvalLimit", UserColumn: "UserId", Column: "Limit", Type: "bytes"}},
			}}},
			wantContain: `subject binding "approvalLimit" carries comparison type "bytes"`,
		},
		{
			name: "empty binding name",
			data: CollectionData{Resources: []CollectionResource{{
				Name:       "Widgets",
				Scope:      accesstypes.GlobalPermissionScope,
				Attributes: []AttributeData{{Column: "CrewId"}},
			}}},
			wantContain: "empty name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewGeneratedCollection(tt.data)
			if err == nil {
				t.Fatalf("NewGeneratedCollection() expected an error containing %q, got nil", tt.wantContain)
			}
			if !strings.Contains(err.Error(), tt.wantContain) {
				t.Errorf("NewGeneratedCollection() error = %q, want containing %q", err, tt.wantContain)
			}
		})
	}
}
