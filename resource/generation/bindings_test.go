package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// bindingFixtureTables is the synthetic schema behind the bindingfixture
// structs: the FK, primary-key, and unique-index metadata the resolver
// validates paths and anchors against.
func bindingFixtureTables() map[string]*tableMetadata {
	// A single-column primary key is the whole key of the PRIMARY_KEY index, so the
	// schema read marks it indexed and row-identifying; one column of a composite key
	// is indexed and nothing more (deriveIndexFlags).
	pk := columnMeta{IsPrimaryKey: true, IsIndex: true, IsUniqueIndex: true}
	compositeKey := columnMeta{IsPrimaryKey: true, IsIndex: true}
	// A column of a composite unique index is indexed and nothing more; a single-column
	// unique index, null-filtered or not, identifies a row.
	compositeUnique := columnMeta{IsIndex: true}
	uniqueAnchor := columnMeta{IsIndex: true, IsUniqueIndex: true}
	plain := columnMeta{}
	fk := func(table string) columnMeta {
		return columnMeta{IsForeignKey: true, ReferencedTable: table, ReferencedColumn: "Id"}
	}

	return map[string]*tableMetadata{
		"MaintenanceTasks": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "StationId": fk("Stations"), "CrewId": plain,
			"ShipId": fk("Ships"), "BerthId": fk("Berths"), "EstimatedCost": plain,
		}},
		"Ships":  {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "Class": plain}},
		"Berths": {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "StationId": fk("Stations")}},
		"Stations": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "Sector": plain,
		}},
		"CrewMembers": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "UserId": plain, "CrewId": plain, "StationId": plain,
		}},
		"UserProfiles": {PkCount: 1, Columns: map[string]columnMeta{
			"UserId": pk, "ApprovalLimit": plain,
		}},
		"HomeProfiles": {PkCount: 1, Columns: map[string]columnMeta{
			"UserId": pk, "StationId": fk("Stations"),
		}},
		"UnsupportedValueTypes": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "UserId": plain, "Payload": plain,
		}},
		"ReservedNames":     {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "CrewId": plain}},
		"BadCharsets":       {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "CrewId": plain}},
		"DuplicateNames":    {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "CrewId": plain, "UserId": plain}},
		"PathOffNonFKs":     {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "Label": plain}},
		"PathUnknownFields": {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "ShipId": fk("Ships")}},
		"PathThroughNonFKs": {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "ShipId": fk("Ships")}},
		"ScalarOnNonUniques": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "UserId": plain, "ApprovalLimit": plain,
		}},
		"ScalarOnCompositeKeys": {PkCount: 2, Columns: map[string]columnMeta{
			"GroupId": compositeKey, "UserId": compositeKey, "Seat": plain,
		}},
		"ScalarOnCompositeUniques": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "UserId": compositeUnique, "GroupId": compositeUnique, "Seat": plain,
		}},
		"NullFilteredAnchors": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "UserId": uniqueAnchor, "ApprovalLimit": plain,
		}},
		"UnknownValueFields": {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "UserId": plain}},
		"PartitionBlindAnchors": {PkCount: 1, Columns: map[string]columnMeta{
			"Id": pk, "UserId": plain, "CrewId": plain,
		}},
		"PathTenantTasks":    {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "BerthId": fk("Berths"), "Name": plain}},
		"StatefulTasks":      {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "State": fk("TaskStates"), "ShipId": fk("Ships"), "Notes": plain}},
		"StateOnNonFKs":      {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "Label": plain}},
		"StateBadDefaults":   {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "State": fk("TaskStates")}},
		"StateStatedTwices":  {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "State": fk("TaskStates")}},
		"TaskParts":          {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "TaskId": fk("StatefulTasks"), "ShipId": fk("Ships")}},
		"PartOrders":         {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "PartId": fk("TaskParts")}},
		"UnknownRootMembers": {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "TaskId": fk("StatefulTasks")}},
		"ChainBreakMembers":  {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "ShipId": fk("Ships")}},
		"ScopedMembers":      {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "TaskId": fk("StatefulTasks")}},
		"DoubleTenants":      {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "StationId": plain, "RegionId": plain}},
		"GlobalTenants":      {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "StationId": plain}},
		"TenantStatedTwices": {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "StationId": plain}},
		"UnboundTenants":     {PkCount: 1, Columns: map[string]columnMeta{"Id": pk, "Name": plain}},
	}
}

// resolveFixtureBindings runs the resolver on one fixture struct exactly the
// way structsToResources does: schema-backed fields first, then the binding
// annotations against them.
func resolveFixtureBindings(t *testing.T, c *client, structs map[string]*parser.Struct, name string) (*resourceInfo, error) {
	t.Helper()

	s := structs[name]
	if s == nil {
		t.Fatalf("struct %q not found in fixture package", name)
	}

	annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(s)
	if err != nil {
		t.Fatalf("ScanStruct(%s) error = %v", name, err)
	}
	table, err := c.tableMetadataFor(name)
	if err != nil {
		t.Fatalf("tableMetadataFor(%s) error = %v", name, err)
	}

	res := &resourceInfo{TypeInfo: s.TypeInfo, PkCount: table.PkCount}
	if err := resolvePermissionScope(annotations, &res.PermissionScope); err != nil {
		t.Fatalf("resolvePermissionScope(%s) error = %v", name, err)
	}
	res.Fields, err = newResourceFields(res, s, table)
	if err != nil {
		t.Fatalf("newResourceFields(%s) error = %v", name, err)
	}

	structsByTable := make(map[string]*parser.Struct, len(structs))
	for _, other := range structs {
		structsByTable[c.pluralize(other.Name())] = other
	}

	return res, c.resolveBindingAnnotations(res, s, annotations, structsByTable)
}

// TestResolveBindingAnnotations pins the compiled shapes of the §04 binding
// vocabulary: column and join-path attributes, the domain binding, and the
// subject-side set and scalar forms — anchors are the annotated fields, and
// every remote hop resolves through real FK metadata.
func TestResolveBindingAnnotations(t *testing.T) {
	t.Parallel()

	c := &client{tableMap: bindingFixtureTables()}
	structs := fixtureStructs(loadFixture(t, "bindingfixture"))

	anchorName := func(f *resourceField) string { return f.Name() }
	opts := cmp.Options{
		cmp.Transformer("anchor", anchorName),
		cmpopts.EquateEmpty(),
	}

	t.Run("attribute and domain bindings", func(t *testing.T) {
		t.Parallel()

		res, err := resolveFixtureBindings(t, c, structs, "MaintenanceTask")
		if err != nil {
			t.Fatalf("resolveBindingAnnotations() error = %v", err)
		}

		type wantAttribute struct {
			Name   string
			Anchor string
			Type   string
			Path   []bindingHop
		}
		wantAttributes := []wantAttribute{
			{Name: "crew", Anchor: "CrewID", Type: "string"},
			{Name: "shipClass", Anchor: "ShipID", Type: "string", Path: []bindingHop{
				{Table: "Ships", JoinColumn: "Id", Column: "Class"},
			}},
			{Name: "sector", Anchor: "BerthID", Type: "string", Path: []bindingHop{
				{Table: "Berths", JoinColumn: "Id", Column: "StationId"},
				{Table: "Stations", JoinColumn: "Id", Column: "Sector"},
			}},
			{Name: "estimatedCost", Anchor: "EstimatedCost", Type: "number"},
		}

		gotAttributes := make([]wantAttribute, 0, len(res.Attributes))
		for _, a := range res.Attributes {
			gotAttributes = append(gotAttributes, wantAttribute{Name: a.Name, Anchor: a.Anchor.Name(), Type: a.Type, Path: a.Path})
		}
		if diff := cmp.Diff(wantAttributes, gotAttributes, opts); diff != "" {
			t.Errorf("Attributes mismatch (-want +got):\n%s", diff)
		}

		if res.DomainBinding == nil || res.DomainBinding.Anchor.Name() != "StationID" || len(res.DomainBinding.Path) != 0 {
			t.Errorf("DomainBinding = %+v, want bare binding anchored on StationID", res.DomainBinding)
		}
	})

	t.Run("subject set", func(t *testing.T) {
		t.Parallel()

		res, err := resolveFixtureBindings(t, c, structs, "CrewMember")
		if err != nil {
			t.Fatalf("resolveBindingAnnotations() error = %v", err)
		}
		if len(res.SubjectSets) != 1 {
			t.Fatalf("SubjectSets = %d entries, want 1", len(res.SubjectSets))
		}
		set := res.SubjectSets[0]
		if set.Name != "crews" || set.Anchor.Name() != "UserID" || set.ValueField.Name() != "CrewID" || set.Scalar || len(set.Path) != 0 || set.Type != "string" {
			t.Errorf("SubjectSets[0] = {Name:%s Anchor:%s Value:%s Scalar:%v Path:%v Type:%s}, want crews anchored on UserID yielding CrewID as a string", set.Name, set.Anchor.Name(), set.ValueField.Name(), set.Scalar, set.Path, set.Type)
		}
		if res.DomainBinding == nil || res.DomainBinding.Anchor.Name() != "StationID" {
			t.Errorf("DomainBinding = %+v, want bare binding anchored on StationID", res.DomainBinding)
		}
	})

	t.Run("subject value on a null-filtered unique anchor", func(t *testing.T) {
		t.Parallel()

		res, err := resolveFixtureBindings(t, c, structs, "NullFilteredAnchor")
		if err != nil {
			t.Fatalf("resolveBindingAnnotations() error = %v", err)
		}
		if len(res.SubjectValues) != 1 || res.SubjectValues[0].Name != "approvalLimit" || !res.SubjectValues[0].Scalar {
			t.Fatalf("SubjectValues = %v, want one scalar approvalLimit: a null-filtered single-column unique index still enforces one row per non-null user", res.SubjectValues)
		}
	})

	t.Run("subject value on a unique anchor", func(t *testing.T) {
		t.Parallel()

		res, err := resolveFixtureBindings(t, c, structs, "UserProfile")
		if err != nil {
			t.Fatalf("resolveBindingAnnotations() error = %v", err)
		}
		if len(res.SubjectValues) != 1 {
			t.Fatalf("SubjectValues = %d entries, want 1", len(res.SubjectValues))
		}
		value := res.SubjectValues[0]
		if value.Name != "approvalLimit" || value.Anchor.Name() != "UserID" || value.ValueField.Name() != "ApprovalLimit" || !value.Scalar || value.Type != "number" {
			t.Errorf("SubjectValues[0] = {Name:%s Anchor:%s Value:%s Scalar:%v Type:%s}, want approvalLimit anchored on UserID yielding ApprovalLimit as a number", value.Name, value.Anchor.Name(), value.ValueField.Name(), value.Scalar, value.Type)
		}
	})

	t.Run("dotted subject value takes its terminal column's type", func(t *testing.T) {
		t.Parallel()

		res, err := resolveFixtureBindings(t, c, structs, "HomeProfile")
		if err != nil {
			t.Fatalf("resolveBindingAnnotations() error = %v", err)
		}
		if len(res.SubjectValues) != 1 {
			t.Fatalf("SubjectValues = %d entries, want 1", len(res.SubjectValues))
		}
		value := res.SubjectValues[0]
		wantPath := []bindingHop{{Table: "Stations", JoinColumn: "Id", Column: "Sector"}}
		if value.Name != "homeSector" || value.ValueField.Name() != "StationID" || value.Type != "string" || !cmp.Equal(value.Path, wantPath) {
			t.Errorf("SubjectValues[0] = {Name:%s Value:%s Type:%s Path:%v}, want homeSector through StationID to Stations.Sector as a string", value.Name, value.ValueField.Name(), value.Type, value.Path)
		}
	})
}

// TestResolveBindingAnnotations_rejections pins the resolver's validations:
// reserved and malformed names, duplicate names across the resource's
// vocabulary, paths that do not follow real foreign keys, non-unique scalar
// anchors, unknown value fields, value columns outside the comparison-type
// vocabulary, and a second domain binding.
func TestResolveBindingAnnotations_rejections(t *testing.T) {
	t.Parallel()

	c := &client{tableMap: bindingFixtureTables()}
	structs := fixtureStructs(loadFixture(t, "bindingfixture"))

	tests := []struct {
		name        string
		structName  string
		wantContain string
	}{
		{
			name:        "reserved binding name",
			structName:  "ReservedName",
			wantContain: "reserved by the condition expression language",
		},
		{
			name:        "binding name outside the identifier charset",
			structName:  "BadCharset",
			wantContain: "must match",
		},
		{
			name:        "duplicate name across the vocabulary",
			structName:  "DuplicateName",
			wantContain: "already declared",
		},
		{
			name:        "join path off a non-FK anchor",
			structName:  "PathOffNonFK",
			wantContain: "must leave through a foreign key",
		},
		{
			name:        "join path through an unknown field",
			structName:  "PathUnknownField",
			wantContain: `field "Nope" not found`,
		},
		{
			name:        "join path continuing through a non-FK column",
			structName:  "PathThroughNonFK",
			wantContain: "must be a foreign key to continue",
		},
		{
			name:        "subject value on a non-unique anchor",
			structName:  "ScalarOnNonUnique",
			wantContain: "alone to identify a row",
		},
		{
			name:        "subject value on one column of a composite key",
			structName:  "ScalarOnCompositeKey",
			wantContain: "a column of a composite key or composite unique index does not",
		},
		{
			name:        "subject value on one column of a composite unique index",
			structName:  "ScalarOnCompositeUnique",
			wantContain: "a column of a composite key or composite unique index does not",
		},
		{
			name:        "subject set with an unknown value field",
			structName:  "UnknownValueField",
			wantContain: "not found on the resource",
		},
		{
			name:        "subject set yielding a column outside the comparison-type vocabulary",
			structName:  "UnsupportedValueType",
			wantContain: "not a supported attribute comparison type",
		},
		{
			name:        "second domain binding",
			structName:  "DoubleTenant",
			wantContain: "exactly one tenant",
		},
		{
			name:        "domain-scoped subject anchor without a domain binding",
			structName:  "PartitionBlindAnchor",
			wantContain: "requires a @domain binding",
		},
		{
			name:        "domain-scoped resource without a domain binding",
			structName:  "UnboundTenant",
			wantContain: "requires a @domain binding",
		},
		{
			name:        "domain binding on a resource that is not domain-scoped",
			structName:  "GlobalTenant",
			wantContain: "only valid on a domain-scoped resource",
		},
		{
			name:        "tenant key restating derived behavior through a conditions tag",
			structName:  "TenantStatedTwice",
			wantContain: "behavior is never stated twice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := resolveFixtureBindings(t, c, structs, tt.structName)
			if err == nil {
				t.Fatalf("resolveBindingAnnotations(%s) expected an error containing %q, got nil", tt.structName, tt.wantContain)
			}
			if !strings.Contains(err.Error(), tt.wantContain) {
				t.Errorf("resolveBindingAnnotations(%s) error = %q, want containing %q", tt.structName, err, tt.wantContain)
			}
		})
	}
}

// TestRejectBindingAnnotations pins the placement rule: binding annotations
// are table-backed resources' vocabulary, rejected on every schemaless
// struct kind.
func TestRejectBindingAnnotations(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "bindingfixture"))
	s := structs["VirtualWithBinding"]
	if s == nil {
		t.Fatal("struct VirtualWithBinding not found in fixture package")
	}
	annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(s)
	if err != nil {
		t.Fatalf("ScanStruct() error = %v", err)
	}

	err = rejectBindingAnnotations(s, annotations, "virtual resource")
	if err == nil {
		t.Fatal("rejectBindingAnnotations() expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "only valid on @resource structs") {
		t.Errorf("rejectBindingAnnotations() error = %q, want the placement-rule message", err)
	}
}
