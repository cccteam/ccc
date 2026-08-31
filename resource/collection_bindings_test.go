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
				{Name: "crew", Column: "CrewId"},
				{Name: "sector", Column: "BerthId", Path: []BindingHop{
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
			SubjectSets: []SubjectBindingData{{Name: "crews", UserColumn: "UserId", Column: "CrewId"}},
			SubjectValues: []SubjectBindingData{
				{Name: "approvalLimit", UserColumn: "UserId", Column: "Limit"},
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

// TestNewGeneratedCollection_bindingValidation pins the runtime vocabulary
// rules: names unique within one resource, subject names unique across the
// whole collection.
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
					{Name: "crew", Column: "CrewId"},
					{Name: "crew", Column: "TeamId"},
				},
			}}},
			wantContain: `binding name "crew" twice`,
		},
		{
			name: "attribute and subject name colliding on one resource",
			data: CollectionData{Resources: []CollectionResource{{
				Name:        "Widgets",
				Scope:       accesstypes.GlobalPermissionScope,
				Attributes:  []AttributeData{{Name: "crew", Column: "CrewId"}},
				SubjectSets: []SubjectBindingData{{Name: "crew", UserColumn: "UserId", Column: "CrewId"}},
			}}},
			wantContain: `binding name "crew" twice`,
		},
		{
			name: "subject name is one application-wide namespace",
			data: CollectionData{Resources: []CollectionResource{
				{
					Name:        "CrewMembers",
					Scope:       accesstypes.GlobalPermissionScope,
					SubjectSets: []SubjectBindingData{{Name: "crews", UserColumn: "UserId", Column: "CrewId"}},
				},
				{
					Name:        "TeamMembers",
					Scope:       accesstypes.DomainPermissionScope,
					SubjectSets: []SubjectBindingData{{Name: "crews", UserColumn: "UserId", Column: "TeamId"}},
				},
			}},
			wantContain: "application-wide namespace",
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
