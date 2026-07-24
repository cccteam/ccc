package resource

// These tests pin the permission-enforcement contract of the resource package:
//
//   - Resource-level permissions are fail closed: every enforced operation requires the
//     operation's permission on the base resource.
//   - Field-level permissions are fail open: a field with no perm tag never requires a
//     field-level grant. Tagged fields require a grant on "resource.tag".
//   - Requesting a specific denied column is Forbidden, while the accessible-fields path
//     silently filters denied columns out.
//
// The fail-open assertions document current behavior that existing applications depend
// on. They are expected to be deliberately rewritten when field permissions migrate to
// fail closed.

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
	ID     ccc.UUID `json:"id"`
	Public string   `json:"public"`
	Tagged string   `json:"tagged" perm:"Read"`
	Locked string   `json:"locked" perm:"Read"`
	Frozen string   `json:"frozen"`
}

type enforcementPatchRequest struct {
	Public string `json:"public"`
	Tagged string `json:"tagged" perm:"Create,Update"`
	Frozen string `json:"frozen" immutable:"true"`
}

// fakeUserPermissions is a UserPermissions implementation backed by a static grant table.
type fakeUserPermissions struct {
	granted map[accesstypes.Permission][]accesstypes.Resource
	err     error
}

func (f *fakeUserPermissions) Check(_ context.Context, perm accesstypes.Permission, resources ...accesstypes.Resource) (ok bool, missing []accesstypes.Resource, err error) {
	if f.err != nil {
		return false, nil, f.err
	}

	for _, res := range resources {
		if !slices.Contains(f.granted[perm], res) {
			missing = append(missing, res)
		}
	}

	return len(missing) == 0, missing, nil
}

func (f *fakeUserPermissions) Domain() accesstypes.Domain { return "testDomain" }

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
			name:       "untagged requested columns are fail open with resource-only grant",
			target:     "/?columns=id,public,frozen",
			grants:     map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource}},
			wantFields: []accesstypes.Field{"ID", "Public", "Frozen"},
		},
		{
			name:            "tagged requested column without field grant is forbidden",
			target:          "/?columns=id,tagged",
			grants:          map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource}},
			wantForbidden:   true,
			wantErrContains: string(enforcedResource) + ".tagged",
		},
		{
			name:   "tagged requested column with field grant is allowed",
			target: "/?columns=id,tagged",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {enforcedResource, enforcedResource + ".tagged"},
			},
			wantFields: []accesstypes.Field{"ID", "Tagged"},
		},
		{
			name:       "accessible fields includes untagged fields and filters denied tagged fields",
			target:     "/",
			grants:     map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Read: {enforcedResource}},
			wantFields: []accesstypes.Field{"ID", "Public", "Frozen"},
		},
		{
			name:   "accessible fields includes tagged fields with grants",
			target: "/",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {enforcedResource, enforcedResource + ".tagged"},
			},
			wantFields: []accesstypes.Field{"ID", "Public", "Frozen", "Tagged"},
		},
		{
			name:   "accessible fields includes all fields with full grants",
			target: "/",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Read: {enforcedResource, enforcedResource + ".tagged", enforcedResource + ".locked"},
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

			resSet, err := NewSet[enforcementResource, enforcementReadRequest]()
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
				qSet, err = decoder.Decode(req, &fakeUserPermissions{granted: tt.grants, err: tt.permCheckErr})
			}
			if err != nil {
				t.Fatalf("QueryDecoder.Decode() error = %v", err)
			}

			wantErr := tt.wantForbidden || tt.wantErrContains != ""

			ctrl := gomock.NewController(t)
			reader := NewMockReader[enforcementResource](ctrl)
			reader.EXPECT().DBType().MinTimes(1).Return(SpannerDBType)
			if !wantErr {
				reader.EXPECT().Read(gomock.Any(), gomock.Any()).Return(&enforcementResource{}, nil)
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
			name:             "create untagged and immutable fields are fail open with resource-only grant",
			operation:        OperationCreate,
			body:             `{"public":"x","frozen":"y"}`,
			grants:           map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Create: {enforcedResource}},
			wantBufferedCols: []string{"Frozen", "Id", "Public"},
		},
		{
			name:            "create tagged field without field grant is forbidden",
			operation:       OperationCreate,
			body:            `{"tagged":"x"}`,
			grants:          map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Create: {enforcedResource}},
			wantForbidden:   true,
			wantErrContains: string(enforcedResource) + ".tagged",
		},
		{
			name:      "create tagged field with field grant is allowed",
			operation: OperationCreate,
			body:      `{"tagged":"x"}`,
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Create: {enforcedResource, enforcedResource + ".tagged"},
			},
			wantBufferedCols: []string{"Id", "Tagged"},
		},
		{
			name:            "update tagged field without field grant is forbidden",
			operation:       OperationUpdate,
			body:            `{"tagged":"x"}`,
			grants:          map[accesstypes.Permission][]accesstypes.Resource{accesstypes.Update: {enforcedResource}},
			wantForbidden:   true,
			wantErrContains: string(enforcedResource) + ".tagged",
		},
		{
			name:      "update tagged field with field grant is allowed",
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

			resSet, err := NewSet[enforcementResource, enforcementPatchRequest]()
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
				patchSet, err = decoder.DecodeOperation(&Operation{Type: OperationDelete, Req: req}, userPermissions)
			case tt.withoutPermissions:
				patchSet, err = decoder.DecodeWithoutPermissions(req)
			default:
				patchSet, err = decoder.Decode(req, userPermissions, permissionFromType(tt.operation))
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

			got, err := decoder.Decode(req)

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
