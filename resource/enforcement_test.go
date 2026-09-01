package resource

// These tests pin the permission-enforcement contract of the resource package:
//
//   - Resource-level permissions are fail closed: every enforced operation requires the
//     operation's permission on the base resource.
//   - Field-level permissions are fail closed and structural: every client-addressable
//     field requires the operation's permission on "resource.tag", with a single
//     exemption — the perm:"-" primary-key marker, whose readability follows the
//     resource-level grant.
//   - Requesting a specific denied column is Forbidden, while the accessible-fields path
//     silently filters denied columns out.

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/cccteam/spxscan/spxapi"
	"github.com/go-playground/errors/v5"
	"go.uber.org/mock/gomock"
)

const enforcedResource = accesstypes.Resource("enforcementResources")

type enforcementResource struct {
	ID     ccc.UUID `spanner:"Id"`
	Public string   `spanner:"Public"`
	Tagged string   `spanner:"Tagged"`
	Locked string   `spanner:"Locked"`
	Frozen string   `spanner:"Frozen"`
}

func (enforcementResource) Resource() accesstypes.Resource { return enforcedResource }

func (enforcementResource) DefaultConfig() Config { return Config{} }

type enforcementReadRequest struct {
	ID     ccc.UUID `json:"id"     perm:"-"`
	Public string   `json:"public"`
	Tagged string   `json:"tagged"`
	Locked string   `json:"locked"`
	Frozen string   `json:"frozen"`
}

type enforcementPatchRequest struct {
	Public string `json:"public"`
	Tagged string `json:"tagged"`
	Frozen string `json:"frozen" immutable:"true"`
}

// enforcementExemptReadRequest has only the exempt primary key, so enforcement needs no
// field-level Check at all.
type enforcementExemptReadRequest struct {
	ID ccc.UUID `json:"id" perm:"-"`
}

// testScope is the tenant scope the enforcement fixtures evaluate in.
var testScope = accesstypes.DomainScope("testDomain")

// testCondition is the payload every fixture Conditional decision carries: a
// row-referencing condition over the fixture collection's one attribute.
var testCondition = mustCondition("owner = subject")

func mustCondition(source string) accesstypes.Condition {
	c, err := accesstypes.NewCondition(source)
	if err != nil {
		panic(err)
	}

	return c
}

// enforcementCollection is the generated-collection stand-in read rendering
// resolves against: the fixture resource with the owner attribute bound to its
// Owner column.
func enforcementCollection(t *testing.T) *GeneratedCollection {
	t.Helper()

	g, err := NewGeneratedCollection(CollectionData{Resources: []CollectionResource{{
		Name:        enforcedResource,
		Scope:       accesstypes.DomainPermissionScope,
		Permissions: []accesstypes.Permission{accesstypes.Read},
		Attributes:  []AttributeData{{Name: "owner", Column: "Owner"}},
	}}})
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	return g
}

// fakeUserPermissions is a UserPermissions implementation backed by a static grant
// table: resources under conditional answer Conditional, resources under granted
// answer Granted, everything else fails closed to Denied. It records every Check
// invocation so tests can assert call batching, scope routing, and the decode-stamped
// Environment.
type fakeUserPermissions struct {
	granted     map[accesstypes.Permission][]accesstypes.Resource
	conditional map[accesstypes.Permission][]accesstypes.Resource
	err         error

	checkCalls   int
	gotEnvs      []accesstypes.Environment
	gotScopes    []accesstypes.Scope
	gotResources [][]accesstypes.Resource
}

func (f *fakeUserPermissions) Check(_ context.Context, env accesstypes.Environment, scope accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	f.checkCalls++
	f.gotEnvs = append(f.gotEnvs, env)
	f.gotScopes = append(f.gotScopes, scope)
	f.gotResources = append(f.gotResources, slices.Clone(resources))

	if f.err != nil {
		return nil, f.err
	}

	decisions := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		switch {
		case slices.Contains(f.conditional[perm], res):
			decisions[res] = accesstypes.Conditional(accesstypes.ConditionGroup{Resources: []accesstypes.Resource{res}, Condition: testCondition})
		case slices.Contains(f.granted[perm], res):
			decisions[res] = accesstypes.Granted()
		default:
			decisions[res] = accesstypes.Denied()
		}
	}

	return decisions, nil
}

func (f *fakeUserPermissions) User() accesstypes.User { return "testUser" }

var _ ReadWriteTransaction = (*recordingTxn)(nil)

// recordingTxn records buffered mutations so tests can assert whether enforcement
// allowed a mutation through and which columns it contained.
type recordingTxn struct {
	bufferMapCalls []map[string]any
}

func (r *recordingTxn) DBType() DBType { return SpannerDBType }

func (r *recordingTxn) SpannerReadOnlyTransaction() spxapi.Querier { return nil }

func (r *recordingTxn) PostgresReadOnlyTransaction() any {
	panic("recordingTxn.PostgresReadOnlyTransaction() should never be called")
}

func (r *recordingTxn) BufferMap(_ PatchSetMetadata, patch map[string]any) error {
	r.bufferMapCalls = append(r.bufferMapCalls, patch)

	return nil
}

func (r *recordingTxn) BufferStruct(PatchSetMetadata) error { return nil }

func (r *recordingTxn) DataChangeEventIndex(accesstypes.Resource, string) int { return 0 }

func mustUUIDFromString(s string) ccc.UUID {
	u, err := ccc.UUIDFromString(s)
	if err != nil {
		panic(err)
	}

	return u
}

func TestQuerySet_Read_permissionEnforcement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		target             string
		withoutPermissions bool
		grants             map[accesstypes.Permission][]accesstypes.Resource
		permCheckErr       error
		wantForbidden      bool
		wantErrContains    string
		wantFields         []accesstypes.Field
	}{
		{
			name:            "resource permission is fail closed",
			target:          "/?columns=id,public",
			grants:          nil,
			wantForbidden:   true,
			wantErrContains: string(enforcedResource),
		},
		{
			name:            "requested columns without field grants are forbidden",
			target:          "/?columns=id,public,frozen",
			grants:          map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource}},
			wantForbidden:   true,
			wantErrContains: string(enforcedResource) + ".public",
		},
		{
			name:   "requested columns with field grants are allowed",
			target: "/?columns=id,public,frozen",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {enforcedResource, enforcedResource + ".public", enforcedResource + ".frozen"},
			},
			wantFields: []accesstypes.Field{"ID", "Public", "Frozen"},
		},
		{
			name:       "exempt column alone requires only the resource grant",
			target:     "/?columns=id",
			grants:     map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource}},
			wantFields: []accesstypes.Field{"ID"},
		},
		{
			name:            "requested column without its field grant is forbidden",
			target:          "/?columns=id,tagged",
			grants:          map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource}},
			wantForbidden:   true,
			wantErrContains: string(enforcedResource) + ".tagged",
		},
		{
			name:   "requested column with its field grant is allowed",
			target: "/?columns=id,tagged",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {enforcedResource, enforcedResource + ".tagged"},
			},
			wantFields: []accesstypes.Field{"ID", "Tagged"},
		},
		{
			name:       "accessible fields with resource-only grant returns only the exempt primary key",
			target:     "/",
			grants:     map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource}},
			wantFields: []accesstypes.Field{"ID"},
		},
		{
			name:   "accessible fields includes exactly the granted fields",
			target: "/",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {enforcedResource, enforcedResource + ".tagged"},
			},
			wantFields: []accesstypes.Field{"ID", "Tagged"},
		},
		{
			name:   "accessible fields includes all fields with full grants",
			target: "/",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {
					enforcedResource, enforcedResource + ".public", enforcedResource + ".tagged",
					enforcedResource + ".locked", enforcedResource + ".frozen",
				},
			},
			wantFields: []accesstypes.Field{"ID", "Public", "Frozen", "Tagged", "Locked"},
		},
		{
			name:            "permission check error propagates and is not forbidden",
			target:          "/?columns=id,public",
			grants:          nil,
			permCheckErr:    errors.New("enforcer unavailable"),
			wantErrContains: "enforcer unavailable",
		},
		{
			name:               "decode without permissions bypasses field enforcement",
			target:             "/?columns=id,tagged,locked",
			withoutPermissions: true,
			wantFields:         []accesstypes.Field{"ID", "Tagged", "Locked"},
		},
		{
			name:               "decode without permissions returns all fields when no columns requested",
			target:             "/",
			withoutPermissions: true,
			wantFields:         []accesstypes.Field{"ID", "Public", "Tagged", "Locked", "Frozen"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resSet, err := NewSet[enforcementResource, enforcementReadRequest](accesstypes.Read)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}
			decoder, err := NewQueryDecoder[enforcementResource, enforcementReadRequest](resSet)
			if err != nil {
				t.Fatalf("NewQueryDecoder() error = %v", err)
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, http.NoBody)

			var qSet *QuerySet[enforcementResource]
			if tt.withoutPermissions {
				qSet, err = decoder.DecodeWithoutPermissions(req)
			} else {
				qSet, err = decoder.Decode(req, &fakeUserPermissions{granted: tt.grants, err: tt.permCheckErr}, testScope)
			}
			if err != nil {
				t.Fatalf("QueryDecoder.Decode() error = %v", err)
			}

			wantErr := tt.wantForbidden || tt.wantErrContains != ""

			ctrl := gomock.NewController(t)
			reader := NewMockReader[enforcementResource](ctrl)
			reader.EXPECT().DBType().MinTimes(1).Return(SpannerDBType)
			if !wantErr {
				reader.EXPECT().Read(gomock.Any(), gomock.Any()).Return(&Row[enforcementResource]{}, nil)
			}
			client := NewMockClient(nil, []any{reader}, nil)

			_, err = qSet.Read(t.Context(), client)

			if wantErr {
				if err == nil {
					t.Fatal("QuerySet.Read() expected an error, got nil")
				}
				if httpio.HasForbidden(err) != tt.wantForbidden {
					t.Errorf("QuerySet.Read() error forbidden = %v, want %v: %v", httpio.HasForbidden(err), tt.wantForbidden, err)
				}
				if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("QuerySet.Read() error = %v, want error containing %q", err, tt.wantErrContains)
				}

				return
			}
			if err != nil {
				t.Fatalf("QuerySet.Read() error = %v", err)
			}

			gotFields := slices.Sorted(slices.Values(qSet.Fields()))
			wantFields := slices.Sorted(slices.Values(tt.wantFields))
			if !slices.Equal(gotFields, wantFields) {
				t.Errorf("QuerySet.Fields() = %v, want %v", gotFields, wantFields)
			}
		})
	}
}

func decodeForBatching[Request any](t *testing.T, target string, userPermissions UserPermissions, permissions ...accesstypes.Permission) (*QuerySet[enforcementResource], error) {
	t.Helper()

	resSet, err := NewSet[enforcementResource, Request](permissions...)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	decoder, err := NewQueryDecoder[enforcementResource, Request](resSet)
	if err != nil {
		t.Fatalf("NewQueryDecoder() error = %v", err)
	}
	decoder.collection = enforcementCollection(t)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)

	return decoder.Decode(req, userPermissions, testScope)
}

func TestQuerySet_Read_checkBatching(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		target           string
		exemptRequest    bool
		grants           map[accesstypes.Permission][]accesstypes.Resource
		wantCheckCalls   int
		wantBatched      []accesstypes.Resource // sorted resources of the field-level batch call
		wantForbidden    bool
		wantOrderedInErr []string // substrings that must appear in this order in the error
	}{
		{
			name:   "accessible fields issues one batched field check",
			target: "/",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {enforcedResource, enforcedResource + ".tagged"},
			},
			wantCheckCalls: 2,
			wantBatched: []accesstypes.Resource{
				enforcedResource + ".frozen", enforcedResource + ".locked",
				enforcedResource + ".public", enforcedResource + ".tagged",
			},
		},
		{
			name:           "exempt-only request issues zero field checks",
			target:         "/",
			exemptRequest:  true,
			grants:         map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource}},
			wantCheckCalls: 1,
		},
		{
			name:             "requested columns forbidden lists denied resources sorted",
			target:           "/?columns=id,tagged,locked",
			grants:           map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource}},
			wantCheckCalls:   2,
			wantForbidden:    true,
			wantOrderedInErr: []string{string(enforcedResource) + ".locked", string(enforcedResource) + ".tagged"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			userPermissions := &fakeUserPermissions{granted: tt.grants}

			var qSet *QuerySet[enforcementResource]
			var err error
			if tt.exemptRequest {
				qSet, err = decodeForBatching[enforcementExemptReadRequest](t, tt.target, userPermissions, accesstypes.Read)
			} else {
				qSet, err = decodeForBatching[enforcementReadRequest](t, tt.target, userPermissions, accesstypes.Read)
			}
			if err != nil {
				t.Fatalf("QueryDecoder.Decode() error = %v", err)
			}

			ctrl := gomock.NewController(t)
			reader := NewMockReader[enforcementResource](ctrl)
			reader.EXPECT().DBType().MinTimes(1).Return(SpannerDBType)
			if !tt.wantForbidden {
				reader.EXPECT().Read(gomock.Any(), gomock.Any()).Return(&Row[enforcementResource]{}, nil)
			}
			client := NewMockClient(nil, []any{reader}, nil)

			_, err = qSet.Read(t.Context(), client)

			if tt.wantForbidden {
				if err == nil || !httpio.HasForbidden(err) {
					t.Fatalf("QuerySet.Read() error = %v, want Forbidden", err)
				}
				last := -1
				for _, sub := range tt.wantOrderedInErr {
					idx := strings.Index(err.Error(), sub)
					if idx < 0 || idx < last {
						t.Errorf("QuerySet.Read() error = %v, want substrings in order %v", err, tt.wantOrderedInErr)

						break
					}
					last = idx
				}
			} else if err != nil {
				t.Fatalf("QuerySet.Read() error = %v", err)
			}

			if userPermissions.checkCalls != tt.wantCheckCalls {
				t.Errorf("Check calls = %d, want %d", userPermissions.checkCalls, tt.wantCheckCalls)
			}
			for i, scope := range userPermissions.gotScopes {
				if scope != testScope {
					t.Errorf("Check call %d scope = %q, want %q", i, scope, testScope)
				}
			}
			if !slices.Equal(userPermissions.gotResources[0], []accesstypes.Resource{enforcedResource}) {
				t.Errorf("first Check resources = %v, want %v", userPermissions.gotResources[0], []accesstypes.Resource{enforcedResource})
			}
			if tt.wantBatched != nil {
				gotBatched := slices.Sorted(slices.Values(userPermissions.gotResources[1]))
				if !slices.Equal(gotBatched, tt.wantBatched) {
					t.Errorf("batched Check resources = %v, want %v", gotBatched, tt.wantBatched)
				}
			}
		})
	}
}

// TestQuerySet_Read_conditionalDecisions pins the read-path gate semantics for
// Conditional decisions: a conditional grant is a grant, so its resources pass the
// gate — explicitly requested or defaulted into the projection — and the conditions
// are carried on the QuerySet for the E-phase lowering. It also pins the Environment
// plumbing: every Check receives the decode-stamped decision context.
func TestQuerySet_Read_conditionalDecisions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		target      string
		grants      map[accesstypes.Permission][]accesstypes.Resource
		conditional map[accesstypes.Permission][]accesstypes.Resource
		wantFields  []accesstypes.Field
		wantCarried []accesstypes.Resource
	}{
		{
			name:   "conditional field joins the accessible-fields projection and is carried",
			target: "/",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {enforcedResource, enforcedResource + ".public"},
			},
			conditional: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {enforcedResource + ".tagged"},
			},
			wantFields:  []accesstypes.Field{"ID", "Public", "Tagged"},
			wantCarried: []accesstypes.Resource{enforcedResource + ".tagged"},
		},
		{
			name:   "explicitly requested conditional field passes the gate and is carried",
			target: "/?columns=id,tagged",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {enforcedResource},
			},
			conditional: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {enforcedResource + ".tagged"},
			},
			wantFields:  []accesstypes.Field{"ID", "Tagged"},
			wantCarried: []accesstypes.Resource{enforcedResource + ".tagged"},
		},
		{
			name:   "conditional base-resource grant passes the gate and is carried",
			target: "/?columns=id,public",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {enforcedResource + ".public"},
			},
			conditional: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {enforcedResource},
			},
			wantFields:  []accesstypes.Field{"ID", "Public"},
			wantCarried: []accesstypes.Resource{enforcedResource},
		},
		{
			name:   "no conditional grants carry nothing",
			target: "/",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {enforcedResource, enforcedResource + ".public"},
			},
			wantFields: []accesstypes.Field{"ID", "Public"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			userPermissions := &fakeUserPermissions{granted: tt.grants, conditional: tt.conditional}

			qSet, err := decodeForBatching[enforcementReadRequest](t, tt.target, userPermissions, accesstypes.Read)
			if err != nil {
				t.Fatalf("QueryDecoder.Decode() error = %v", err)
			}

			ctrl := gomock.NewController(t)
			reader := NewMockReader[enforcementResource](ctrl)
			reader.EXPECT().DBType().MinTimes(1).Return(SpannerDBType)
			reader.EXPECT().Read(gomock.Any(), gomock.Any()).Return(&Row[enforcementResource]{}, nil)
			client := NewMockClient(nil, []any{reader}, nil)

			if _, err := qSet.Read(t.Context(), client); err != nil {
				t.Fatalf("QuerySet.Read() error = %v", err)
			}

			gotFields := slices.Sorted(slices.Values(qSet.Fields()))
			wantFields := slices.Sorted(slices.Values(tt.wantFields))
			if !slices.Equal(gotFields, wantFields) {
				t.Errorf("QuerySet.Fields() = %v, want %v", gotFields, wantFields)
			}

			gotCarried := slices.Sorted(maps.Keys(qSet.conditionalDecisions))
			if !slices.Equal(gotCarried, tt.wantCarried) {
				t.Errorf("conditionalDecisions resources = %v, want %v", gotCarried, tt.wantCarried)
			}
			for res, decision := range qSet.conditionalDecisions {
				if !decision.IsConditional() {
					t.Errorf("carried decision for %s = %s, want conditional", res, decision)
				}
			}

			if len(userPermissions.gotEnvs) == 0 {
				t.Fatal("Check() was never called")
			}
			for i, env := range userPermissions.gotEnvs {
				if _, ok := env.Now(); !ok {
					t.Errorf("Check call %d Environment carries no now; decoders must stamp the decision context", i)
				}
			}
		})
	}
}

func TestPatchSet_Buffer_permissionEnforcement(t *testing.T) {
	t.Parallel()

	id := mustUUIDFromString("8a6570c8-1e51-4870-9def-3f68d0447d09")

	tests := []struct {
		name               string
		operation          OperationType
		body               string
		withoutPermissions bool
		grants             map[accesstypes.Permission][]accesstypes.Resource
		permCheckErr       error
		wantDecodeErr      string
		wantForbidden      bool
		wantErrContains    string
		wantBufferedCols   []string
	}{
		{
			name:            "create resource permission is fail closed",
			operation:       OperationCreate,
			body:            `{"public":"x"}`,
			grants:          nil,
			wantForbidden:   true,
			wantErrContains: string(enforcedResource),
		},
		{
			name:            "create without field grants is forbidden",
			operation:       OperationCreate,
			body:            `{"public":"x","frozen":"y"}`,
			grants:          map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Create: {enforcedResource}},
			wantForbidden:   true,
			wantErrContains: string(enforcedResource) + ".public",
		},
		{
			name:      "create with field grants sets the fields",
			operation: OperationCreate,
			body:      `{"public":"x","frozen":"y"}`,
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Create: {enforcedResource, enforcedResource + ".public", enforcedResource + ".frozen"},
			},
			wantBufferedCols: []string{"Frozen", "Id", "Public"},
		},
		{
			name:            "create field without its field grant is forbidden",
			operation:       OperationCreate,
			body:            `{"tagged":"x"}`,
			grants:          map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Create: {enforcedResource}},
			wantForbidden:   true,
			wantErrContains: string(enforcedResource) + ".tagged",
		},
		{
			name:      "create field with its field grant is allowed",
			operation: OperationCreate,
			body:      `{"tagged":"x"}`,
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Create: {enforcedResource, enforcedResource + ".tagged"},
			},
			wantBufferedCols: []string{"Id", "Tagged"},
		},
		{
			name:            "update field without its field grant is forbidden",
			operation:       OperationUpdate,
			body:            `{"tagged":"x"}`,
			grants:          map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Update: {enforcedResource}},
			wantForbidden:   true,
			wantErrContains: string(enforcedResource) + ".tagged",
		},
		{
			name:      "update field with its field grant is allowed",
			operation: OperationUpdate,
			body:      `{"tagged":"x"}`,
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Update: {enforcedResource, enforcedResource + ".tagged"},
			},
			wantBufferedCols: []string{"Id", "Tagged"},
		},
		{
			name:          "update immutable field is rejected at decode",
			operation:     OperationUpdate,
			body:          `{"frozen":"x"}`,
			grants:        map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Update: {enforcedResource}},
			wantDecodeErr: "json field frozen is immutable",
		},
		{
			name:      "empty update is a no-op without permission checks",
			operation: OperationUpdate,
			body:      `{}`,
			grants:    nil,
		},
		{
			name:            "delete resource permission is fail closed",
			operation:       OperationDelete,
			grants:          nil,
			wantForbidden:   true,
			wantErrContains: string(enforcedResource),
		},
		{
			name:             "delete with resource grant is allowed",
			operation:        OperationDelete,
			grants:           map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Delete: {enforcedResource}},
			wantBufferedCols: []string{},
		},
		{
			name:               "decode without permissions bypasses all enforcement",
			operation:          OperationCreate,
			body:               `{"tagged":"x"}`,
			withoutPermissions: true,
			wantBufferedCols:   []string{"Id", "Tagged"},
		},
		{
			name:            "permission check error propagates and is not forbidden",
			operation:       OperationCreate,
			body:            `{"public":"x"}`,
			permCheckErr:    errors.New("enforcer unavailable"),
			wantErrContains: "enforcer unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resSet, err := NewSet[enforcementResource, enforcementPatchRequest](accesstypes.Create, accesstypes.Update, accesstypes.Delete)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}
			decoder, err := NewDecoder[enforcementResource, enforcementPatchRequest](resSet)
			if err != nil {
				t.Fatalf("NewDecoder() error = %v", err)
			}

			userPermissions := &fakeUserPermissions{granted: tt.grants, err: tt.permCheckErr}

			method, err := httpMethod(string(tt.operation))
			if err != nil {
				t.Fatalf("httpMethod() error = %v", err)
			}
			req := httptest.NewRequestWithContext(t.Context(), method, "/", strings.NewReader(tt.body))

			var patchSet *PatchSet[enforcementResource]
			switch {
			case tt.operation == OperationDelete:
				patchSet, err = decoder.DecodeOperation(&Operation{Type: OperationDelete, Req: req}, userPermissions, testScope)
			case tt.withoutPermissions:
				patchSet, err = decoder.DecodeWithoutPermissions(req)
			default:
				patchSet, err = decoder.Decode(req, userPermissions, testScope, permissionFromType(tt.operation))
			}
			if tt.wantDecodeErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantDecodeErr) {
					t.Fatalf("Decoder.Decode() error = %v, want error containing %q", err, tt.wantDecodeErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Decoder.Decode() error = %v", err)
			}

			switch tt.operation {
			case OperationCreate:
				patchSet.SetPatchType(CreatePatchType)
			case OperationUpdate:
				patchSet.SetPatchType(UpdatePatchType)
			case OperationDelete:
				patchSet.SetPatchType(DeletePatchType)
			}
			patchSet.SetKey("ID", id)

			txn := &recordingTxn{}
			err = patchSet.Buffer(t.Context(), txn)

			if tt.wantForbidden || tt.wantErrContains != "" {
				if err == nil {
					t.Fatal("PatchSet.Buffer() expected an error, got nil")
				}
				if httpio.HasForbidden(err) != tt.wantForbidden {
					t.Errorf("PatchSet.Buffer() error forbidden = %v, want %v: %v", httpio.HasForbidden(err), tt.wantForbidden, err)
				}
				if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("PatchSet.Buffer() error = %v, want error containing %q", err, tt.wantErrContains)
				}
				if len(txn.bufferMapCalls) != 0 {
					t.Errorf("PatchSet.Buffer() buffered %d mutations after a permission failure, want 0", len(txn.bufferMapCalls))
				}

				return
			}
			if err != nil {
				t.Fatalf("PatchSet.Buffer() error = %v", err)
			}

			if tt.wantBufferedCols == nil {
				if len(txn.bufferMapCalls) != 0 {
					t.Errorf("PatchSet.Buffer() buffered %d mutations, want 0", len(txn.bufferMapCalls))
				}

				return
			}

			if len(txn.bufferMapCalls) != 1 {
				t.Fatalf("PatchSet.Buffer() buffered %d mutations, want 1", len(txn.bufferMapCalls))
			}

			gotCols := slices.Sorted(maps.Keys(txn.bufferMapCalls[0]))
			if !slices.Equal(gotCols, tt.wantBufferedCols) {
				t.Errorf("buffered columns = %v, want %v", gotCols, tt.wantBufferedCols)
			}
		})
	}
}

func TestRPCDecoder_Decode_permissionEnforcement(t *testing.T) {
	t.Parallel()

	const method = accesstypes.Resource("LaunchProbe")

	type launchProbeRequest struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name            string
		grants          map[accesstypes.Permission][]accesstypes.Resource
		permCheckErr    error
		wantForbidden   bool
		wantErrContains string
	}{
		{
			name:            "execute permission is fail closed",
			grants:          nil,
			wantForbidden:   true,
			wantErrContains: string(method),
		},
		{
			name:   "execute permission grant is allowed",
			grants: map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Execute: {method}},
		},
		{
			name:            "permission check error propagates and is not forbidden",
			permCheckErr:    errors.New("enforcer unavailable"),
			wantErrContains: "enforcer unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			userPermissions := &fakeUserPermissions{granted: tt.grants, err: tt.permCheckErr}
			decoder, err := NewRPCDecoder[launchProbeRequest](
				func(*http.Request) UserPermissions { return userPermissions },
				method,
				accesstypes.Execute,
			)
			if err != nil {
				t.Fatalf("NewRPCDecoder() error = %v", err)
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"name":"probe-1"}`))

			got, err := decoder.Decode(req, testScope)

			if tt.wantForbidden || tt.wantErrContains != "" {
				if err == nil {
					t.Fatal("RPCDecoder.Decode() expected an error, got nil")
				}
				if httpio.HasForbidden(err) != tt.wantForbidden {
					t.Errorf("RPCDecoder.Decode() error forbidden = %v, want %v: %v", httpio.HasForbidden(err), tt.wantForbidden, err)
				}
				if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("RPCDecoder.Decode() error = %v, want error containing %q", err, tt.wantErrContains)
				}

				return
			}
			if err != nil {
				t.Fatalf("RPCDecoder.Decode() error = %v", err)
			}
			if got.Name != "probe-1" {
				t.Errorf("RPCDecoder.Decode() Name = %q, want %q", got.Name, "probe-1")
			}
		})
	}
}
