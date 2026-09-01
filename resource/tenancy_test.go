package resource

// These tests pin the create-path half of structural row tenancy (design plan
// §06): the bare-column tenant key is stamped from the request's domain
// partition at decode, and only there — global requests, join-path bindings,
// and hand-built patch sets are untouched.

import (
	"reflect"
	"testing"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
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
