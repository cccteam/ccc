//go:build collect_resource_permissions

package resource

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/accesstypes"
)

type parityWidget struct{}

func (parityWidget) Resource() accesstypes.Resource { return "ParityWidgets" }

type parityListRequest struct {
	ID     string `json:"id"   perm:"List"`
	Name   string `json:"name"`
	Hidden string `json:"-"`
}

type parityPatchRequest struct {
	Name string `json:"name" perm:"Create,Update"`
	Code string `json:"code" immutable:"true"`
}

// TestRuntimeVsGeneratedParity registers the same request structs through the deprecated
// runtime path (reflection over struct tags) and through the generator path (FieldTags
// mirroring the tags the generator writes), and requires the resulting collections to be
// equivalent. This is the invariant the deprecated generator's parity hard-error rests on.
func TestRuntimeVsGeneratedParity(t *testing.T) {
	t.Parallel()

	runtime := NewCollection()

	listSet, err := NewSet[parityWidget, parityListRequest](accesstypes.List)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	if err := AddResources(runtime, accesstypes.GlobalPermissionScope, listSet); err != nil {
		t.Fatalf("AddResources() error = %v", err)
	}
	patchSet, err := NewSet[parityWidget, parityPatchRequest](accesstypes.Create, accesstypes.Update, accesstypes.Delete)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	if err := AddResources(runtime, accesstypes.GlobalPermissionScope, patchSet); err != nil {
		t.Fatalf("AddResources() error = %v", err)
	}
	if err := runtime.AddMethodResource(accesstypes.GlobalPermissionScope, accesstypes.Execute, "ParityMethod"); err != nil {
		t.Fatalf("AddMethodResource() error = %v", err)
	}
	if err := runtime.AddResource(accesstypes.GlobalPermissionScope, accesstypes.Execute, "ParityManual"); err != nil {
		t.Fatalf("AddResource() error = %v", err)
	}

	b := NewCollectionBuilder()
	listData, err := NewSetData([]FieldTags{
		{Field: "ID", JSON: "id", Perm: "List"},
		{Field: "Name", JSON: "name"},
		{Field: "Hidden", JSON: "-"},
	}, accesstypes.List)
	if err != nil {
		t.Fatalf("NewSetData() error = %v", err)
	}
	if err := b.AddResourceSet(accesstypes.GlobalPermissionScope, "ParityWidgets", listData); err != nil {
		t.Fatalf("AddResourceSet() error = %v", err)
	}
	patchData, err := NewSetData([]FieldTags{
		{Field: "Name", JSON: "name", Perm: "Create,Update"},
		{Field: "Code", JSON: "code", Immutable: true},
	}, accesstypes.Create, accesstypes.Update, accesstypes.Delete)
	if err != nil {
		t.Fatalf("NewSetData() error = %v", err)
	}
	if err := b.AddResourceSet(accesstypes.GlobalPermissionScope, "ParityWidgets", patchData); err != nil {
		t.Fatalf("AddResourceSet() error = %v", err)
	}
	if err := b.AddMethodResource(accesstypes.GlobalPermissionScope, accesstypes.Execute, "ParityMethod"); err != nil {
		t.Fatalf("AddMethodResource() error = %v", err)
	}
	if err := b.AddResource(accesstypes.GlobalPermissionScope, accesstypes.Execute, "ParityManual"); err != nil {
		t.Fatalf("AddResource() error = %v", err)
	}

	generated, err := NewGeneratedCollection(b.Data())
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	if diffs := DiffCollections(runtime, generated); len(diffs) != 0 {
		t.Errorf("DiffCollections() reported differences between runtime and generated population:\n%s", diffs)
	}

	// The deprecated AddResource records manual provenance, and the declared pair is
	// visible to HasPermission — the exact checks the migration hints rest on.
	wantManual := []ManualRegistration{{Scope: accesstypes.GlobalPermissionScope, Permission: accesstypes.Execute, Resource: "ParityManual"}}
	if diff := cmp.Diff(wantManual, runtime.ManualRegistrations()); diff != "" {
		t.Errorf("ManualRegistrations() mismatch (-want +got):\n%s", diff)
	}
	if !generated.HasPermission(accesstypes.GlobalPermissionScope, accesstypes.Execute, "ParityManual") {
		t.Error("HasPermission(ParityManual) = false, want true")
	}
}
