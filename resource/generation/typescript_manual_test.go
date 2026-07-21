//go:build collect_resource_permissions

package generation

import (
	"slices"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

// Test_verifyManualRegistrations pins the staged-migration hints around deprecated
// Collection.AddResource calls: undeclared registrations are a hard error naming the
// annotation, fully declared ones produce the removal hint, and the check is skipped
// without cached collection data.
func Test_verifyManualRegistrations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		declared     resource.CollectionData
		haveData     bool
		wantDeclared int
		wantMissing  []resource.ManualRegistration
	}{
		{
			name: "declared registrations are counted for the removal hint",
			declared: resource.CollectionData{Resources: []resource.CollectionResource{
				{Name: "UploadThings", Scope: accesstypes.GlobalPermissionScope, Permissions: []accesstypes.Permission{accesstypes.Execute}},
			}},
			haveData:     true,
			wantDeclared: 1,
		},
		{
			name:     "undeclared registrations are reported for the error box",
			haveData: true,
			wantMissing: []resource.ManualRegistration{
				{Scope: accesstypes.GlobalPermissionScope, Permission: accesstypes.Execute, Resource: "UploadThings"},
			},
		},
		{
			name:     "skipped without cached collection data",
			haveData: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rc := resource.NewCollection()
			if err := rc.AddResource(accesstypes.GlobalPermissionScope, accesstypes.Execute, "UploadThings"); err != nil {
				t.Fatalf("AddResource() error = %v", err)
			}

			declared, missing, err := verifyManualRegistrations(rc, tt.declared, tt.haveData)
			if err != nil {
				t.Fatalf("verifyManualRegistrations() error = %v", err)
			}
			if declared != tt.wantDeclared || !slices.Equal(missing, tt.wantMissing) {
				t.Errorf("verifyManualRegistrations() = (%d, %v), want (%d, %v)", declared, missing, tt.wantDeclared, tt.wantMissing)
			}
		})
	}
}
