package resource

// These tests pin the create-path half of structural row tenancy (design plan
// §06): the bare-column tenant key is stamped from the request's domain
// partition at decode, and only there — global requests, join-path bindings,
// and hand-built patch sets are untouched.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

const tenancyTestResource = accesstypes.Resource("tenancyResources")

type tenancyResource struct {
	ID        ccc.UUID `spanner:"Id"`
	StationID string   `spanner:"StationId"`
	Name      string   `spanner:"Name"`
}

func (tenancyResource) Resource() accesstypes.Resource { return tenancyTestResource }

func (tenancyResource) DefaultConfig() Config { return Config{} }

type tenancyCreateRequest struct {
	ID        ccc.UUID `json:"id"   perm:"-"`
	StationID string   `json:"-"`
	Name      string   `json:"name"`
}

func tenancyCollection(t *testing.T, domain *DomainBindingData) *GeneratedCollection {
	t.Helper()

	g, err := NewGeneratedCollection(CollectionData{Resources: []CollectionResource{{
		Name:        tenancyTestResource,
		Scope:       accesstypes.DomainPermissionScope,
		Permissions: []accesstypes.Permission{accesstypes.Create},
		Domain:      domain,
	}}})
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	return g
}

func TestPatchSet_stampTenantKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		collection func(t *testing.T) *GeneratedCollection
		scope      accesstypes.Scope
		wantSet    bool
		wantValue  any
	}{
		{
			name: "bare-column binding stamps the request's domain",
			collection: func(t *testing.T) *GeneratedCollection {
				return tenancyCollection(t, &DomainBindingData{Column: "StationId"})
			},
			scope:     accesstypes.DomainScope("station-alpha"),
			wantSet:   true,
			wantValue: "station-alpha",
		},
		{
			name: "global request stamps nothing",
			collection: func(t *testing.T) *GeneratedCollection {
				return tenancyCollection(t, &DomainBindingData{Column: "StationId"})
			},
			scope: accesstypes.GlobalScope(),
		},
		{
			name: "join-path binding has no local column to stamp",
			collection: func(t *testing.T) *GeneratedCollection {
				return tenancyCollection(t, &DomainBindingData{Column: "ParentId", Path: []BindingHop{{Table: "Parents", JoinColumn: "Id", Column: "StationId"}}})
			},
			scope: accesstypes.DomainScope("station-alpha"),
		},
		{
			name:       "resource without a domain binding stamps nothing",
			collection: func(t *testing.T) *GeneratedCollection { return tenancyCollection(t, nil) },
			scope:      accesstypes.DomainScope("station-alpha"),
		},
		{
			name:       "patch set without a collection stamps nothing",
			collection: func(*testing.T) *GeneratedCollection { return nil },
			scope:      accesstypes.DomainScope("station-alpha"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rSet, err := NewSet[tenancyResource, tenancyCreateRequest](accesstypes.Create)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}

			p := NewPatchSet(NewMetadata[tenancyResource]()).
				Set("Name", "dock four").
				SetPatchType(CreatePatchType)
			p.querySet.collection = tt.collection(t)
			p.EnableUserPermissionEnforcement(rSet, renderStubPermissions{}, tt.scope, accesstypes.Create)

			if err := p.stampTenantKey(); err != nil {
				t.Fatalf("stampTenantKey() error = %v", err)
			}

			if got := p.IsSet("StationID"); got != tt.wantSet {
				t.Fatalf("IsSet(StationID) = %v, want %v", got, tt.wantSet)
			}
			if tt.wantSet {
				if got := p.Get("StationID"); got != tt.wantValue {
					t.Errorf("Get(StationID) = %v (%T), want %v", got, got, tt.wantValue)
				}
			}
		})
	}
}

// TestQuerySet_stmt_tenancyPredicate pins the read-path resource rule: a
// partitioned request's statement carries the tenant predicate whether or not
// conditional grants exist — bare column as a direct comparison, join path as
// nested correlated EXISTS — and a global request carries none.
func TestQuerySet_stmt_tenancyPredicate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		domain     *DomainBindingData
		scope      accesstypes.Scope
		wantSQL    string
		wantParams map[string]any
		wantErr    string
	}{
		{
			name:       "bare-column binding filters on the tenant column",
			domain:     &DomainBindingData{Column: "StationId"},
			scope:      accesstypes.DomainScope("station-alpha"),
			wantSQL:    "SELECT Id, StationId, Name FROM tenancyResources WHERE (`tenancyResources`.`StationId` = @domain)",
			wantParams: map[string]any{"domain": "station-alpha"},
		},
		{
			name:   "join-path binding filters through correlated EXISTS",
			domain: &DomainBindingData{Column: "ParentId", Path: []BindingHop{{Table: "Parents", JoinColumn: "Id", Column: "StationId"}}},
			scope:  accesstypes.DomainScope("station-alpha"),
			wantSQL: "SELECT Id, StationId, Name FROM tenancyResources " +
				"WHERE (EXISTS (SELECT 1 FROM `Parents` `ca1` WHERE `ca1`.`Id` = `tenancyResources`.`ParentId` AND `ca1`.`StationId` = @domain))",
			wantParams: map[string]any{"domain": "station-alpha"},
		},
		{
			name:       "global request renders no tenant predicate",
			domain:     &DomainBindingData{Column: "StationId"},
			scope:      accesstypes.GlobalScope(),
			wantSQL:    "SELECT Id, StationId, Name FROM tenancyResources",
			wantParams: map[string]any{},
		},
		{
			name:    "partitioned request over a binding-less resource fails loud",
			domain:  nil,
			scope:   accesstypes.DomainScope("station-alpha"),
			wantErr: "no domain binding",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rSet, err := NewSet[tenancyResource, tenancyCreateRequest](accesstypes.Create)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}

			q := NewQuerySet(NewMetadata[tenancyResource]())
			q.collection = tenancyCollection(t, tt.domain)
			q.EnableUserPermissionEnforcement(rSet, renderStubPermissions{}, tt.scope, accesstypes.Read)
			for _, field := range []accesstypes.Field{"ID", "StationID", "Name"} {
				q.AddField(field)
			}

			stmt, err := q.stmt(SpannerDBType)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("QuerySet.stmt() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("QuerySet.stmt() error = %v", err)
			}
			if got := normalizeSQL(stmt.SQL); got != tt.wantSQL {
				t.Errorf("QuerySet.stmt() SQL =\n%s\nwant\n%s", got, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantParams, stmt.Params); diff != "" {
				t.Errorf("QuerySet.stmt() params mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPatchSet_mutationTenancy_bare pins the write-path obligations of a
// bare-column binding: update and delete locate their row within the tenant
// predicate even with zero conditional grants (the check-SELECT becomes
// unconditional — pure RBAC no longer pays zero queries on partitioned
// mutations), creates are verified against the stamp, re-tenanting is
// unrepresentable, and upsert fails closed.
func TestPatchSet_mutationTenancy_bare(t *testing.T) {
	t.Parallel()

	id := mustUUIDFromString("8a6570c8-1e51-4870-9def-3f68d0447d09")

	tests := []struct {
		name        string
		patchType   PatchType
		scope       accesstypes.Scope
		set         map[accesstypes.Field]any
		wantNil     bool
		wantNoQuery bool
		wantSQL     string
		wantParams  map[string]any
		wantErr     string
	}{
		{
			name:      "pure-RBAC update locates the row within the tenant predicate",
			patchType: UpdatePatchType,
			scope:     accesstypes.DomainScope("station-alpha"),
			set:       map[accesstypes.Field]any{"Name": "dock four"},
			wantSQL:   "SELECT TRUE AS g0 FROM tenancyResources WHERE `Id` = @_id AND (`tenancyResources`.`StationId` = @domain)",
			wantParams: map[string]any{
				"_id":    id,
				"domain": "station-alpha",
			},
		},
		{
			name:      "pure-RBAC delete locates the row within the tenant predicate",
			patchType: DeletePatchType,
			scope:     accesstypes.DomainScope("station-alpha"),
			wantSQL:   "SELECT TRUE AS g0 FROM tenancyResources WHERE `Id` = @_id AND (`tenancyResources`.`StationId` = @domain)",
			wantParams: map[string]any{
				"_id":    id,
				"domain": "station-alpha",
			},
		},
		{
			name:        "create with the stamped tenant key needs no query",
			patchType:   CreatePatchType,
			scope:       accesstypes.DomainScope("station-alpha"),
			set:         map[accesstypes.Field]any{"StationID": "station-alpha", "Name": "dock four"},
			wantNoQuery: true,
		},
		{
			name:      "create without its tenant key fails loud",
			patchType: CreatePatchType,
			scope:     accesstypes.DomainScope("station-alpha"),
			set:       map[accesstypes.Field]any{"Name": "dock four"},
			wantErr:   "must carry its tenant key",
		},
		{
			name:      "create with a foreign tenant key fails loud",
			patchType: CreatePatchType,
			scope:     accesstypes.DomainScope("station-alpha"),
			set:       map[accesstypes.Field]any{"StationID": "station-beta", "Name": "dock four"},
			wantErr:   "does not equal the request partition",
		},
		{
			name:      "update proposing a foreign tenant key fails loud",
			patchType: UpdatePatchType,
			scope:     accesstypes.DomainScope("station-alpha"),
			set:       map[accesstypes.Field]any{"StationID": "station-beta"},
			wantErr:   "does not equal the request partition",
		},
		{
			name:      "upsert on a partitioned resource fails closed",
			patchType: CreateOrUpdatePatchType,
			scope:     accesstypes.DomainScope("station-alpha"),
			set:       map[accesstypes.Field]any{"Name": "dock four"},
			wantErr:   "cannot enforce an insert-or-update",
		},
		{
			name:      "global request has no tenancy obligation",
			patchType: UpdatePatchType,
			scope:     accesstypes.GlobalScope(),
			set:       map[accesstypes.Field]any{"Name": "dock four"},
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rSet, err := NewSet[tenancyResource, tenancyCreateRequest](accesstypes.Create, accesstypes.Update, accesstypes.Delete)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}

			p := NewPatchSet(NewMetadata[tenancyResource]()).
				SetKey("ID", id).
				SetPatchType(tt.patchType)
			p.querySet.collection = tenancyCollection(t, &DomainBindingData{Column: "StationId"})
			p.EnableUserPermissionEnforcement(rSet, renderStubPermissions{}, tt.scope, accesstypes.Update)
			for field, value := range tt.set {
				p.Set(field, value)
			}

			tenancy, err := p.mutationTenancy()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("mutationTenancy() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("mutationTenancy() error = %v", err)
			}
			if tt.wantNil {
				if tenancy != nil {
					t.Fatalf("mutationTenancy() = %+v, want nil", tenancy)
				}

				return
			}
			if tt.wantNoQuery {
				if tenancy.needsQuery() {
					t.Fatal("needsQuery() = true, want false: a verified bare-column create needs no query")
				}

				return
			}

			stmt, err := p.writeCheckStatement(SpannerDBType, nil, tenancy)
			if err != nil {
				t.Fatalf("writeCheckStatement() error = %v", err)
			}
			if got := normalizeSQL(stmt.SQL); got != tt.wantSQL {
				t.Errorf("writeCheckStatement() SQL =\n%s\nwant\n%s", got, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantParams, stmt.Params); diff != "" {
				t.Errorf("writeCheckStatement() params mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

const tenancyChildTestResource = accesstypes.Resource("tenancyChildResources")

type tenancyChildResource struct {
	ID       ccc.UUID `spanner:"Id"`
	ParentID ccc.UUID `spanner:"ParentId"`
	Note     string   `spanner:"Note"`
}

func (tenancyChildResource) Resource() accesstypes.Resource { return tenancyChildTestResource }

func (tenancyChildResource) DefaultConfig() Config { return Config{} }

type tenancyChildCreateRequest struct {
	ID       ccc.UUID `json:"id"       perm:"-"`
	ParentID ccc.UUID `json:"parentId"`
	Note     string   `json:"note"`
}

// TestPatchSet_mutationTenancy_joinPath pins a join-path binding's insert
// proof: the proposed foreign key must land in the request's partition,
// rendered as correlated EXISTS over the proposed image.
func TestPatchSet_mutationTenancy_joinPath(t *testing.T) {
	t.Parallel()

	id := mustUUIDFromString("8a6570c8-1e51-4870-9def-3f68d0447d09")
	parentID := mustUUIDFromString("1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d")

	childCollection := func(t *testing.T) *GeneratedCollection {
		t.Helper()

		g, err := NewGeneratedCollection(CollectionData{Resources: []CollectionResource{{
			Name:        tenancyChildTestResource,
			Scope:       accesstypes.DomainPermissionScope,
			Permissions: []accesstypes.Permission{accesstypes.Create},
			Domain:      &DomainBindingData{Column: "ParentId", Path: []BindingHop{{Table: "Parents", JoinColumn: "Id", Column: "StationId"}}},
		}}})
		if err != nil {
			t.Fatalf("NewGeneratedCollection() error = %v", err)
		}

		return g
	}

	tests := []struct {
		name       string
		set        map[accesstypes.Field]any
		wantSQL    string
		wantParams map[string]any
		wantErr    string
	}{
		{
			name:    "insert proves the proposed parent lands in the partition",
			set:     map[accesstypes.Field]any{"ParentID": parentID, "Note": "spare part"},
			wantSQL: "SELECT (EXISTS (SELECT 1 FROM `Parents` `ca1` WHERE `ca1`.`Id` = @_c1 AND `ca1`.`StationId` = @domain)) AS zzTenancy",
			wantParams: map[string]any{
				"_c1":    parentID,
				"domain": "station-alpha",
			},
		},
		{
			name:    "insert without the anchor foreign key fails loud",
			set:     map[accesstypes.Field]any{"Note": "spare part"},
			wantErr: "must set ParentId",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rSet, err := NewSet[tenancyChildResource, tenancyChildCreateRequest](accesstypes.Create)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}

			p := NewPatchSet(NewMetadata[tenancyChildResource]()).
				SetKey("ID", id).
				SetPatchType(CreatePatchType)
			p.querySet.collection = childCollection(t)
			p.EnableUserPermissionEnforcement(rSet, renderStubPermissions{}, accesstypes.DomainScope("station-alpha"), accesstypes.Create)
			for field, value := range tt.set {
				p.Set(field, value)
			}

			tenancy, err := p.mutationTenancy()
			if err != nil {
				t.Fatalf("mutationTenancy() error = %v", err)
			}
			if !tenancy.insertPathTerm() {
				t.Fatal("insertPathTerm() = false, want true")
			}

			stmt, err := p.writeCheckStatement(SpannerDBType, nil, tenancy)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("writeCheckStatement() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("writeCheckStatement() error = %v", err)
			}
			if got := normalizeSQL(stmt.SQL); got != tt.wantSQL {
				t.Errorf("writeCheckStatement() SQL =\n%s\nwant\n%s", got, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantParams, stmt.Params); diff != "" {
				t.Errorf("writeCheckStatement() params mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTenantKeyValue(t *testing.T) {
	t.Parallel()

	uuidText := "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	wantUUID, err := ccc.UUIDFromString(uuidText)
	if err != nil {
		t.Fatalf("ccc.UUIDFromString() error = %v", err)
	}

	type namedString string

	tests := []struct {
		name    string
		typed   any
		domain  accesstypes.Domain
		want    any
		wantErr bool
	}{
		{name: "plain string", typed: "", domain: "station-alpha", want: "station-alpha"},
		{name: "named string type", typed: namedString(""), domain: "station-alpha", want: namedString("station-alpha")},
		{name: "text unmarshaler", typed: ccc.UUID{}, domain: accesstypes.Domain(uuidText), want: wantUUID},
		{name: "unsupported type", typed: 0, domain: "station-alpha", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tenantKeyValue(reflect.TypeOf(tt.typed), tt.domain)
			if tt.wantErr {
				if err == nil {
					t.Fatal("tenantKeyValue() expected an error, got nil")
				}

				return
			}
			if err != nil {
				t.Fatalf("tenantKeyValue() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("tenantKeyValue() = %v (%T), want %v", got, got, tt.want)
			}
		})
	}
}
