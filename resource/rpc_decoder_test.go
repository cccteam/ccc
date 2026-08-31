package resource

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

const rpcEnforcedMethod = accesstypes.Resource("EnforcedMethod")

type rpcEnforcementRequest struct {
	Name string `json:"name"`
}

// TestRPCDecoder_Decode_permissionGate pins the decode-time gate semantics for RPC
// methods: Denied is Forbidden, Granted decodes, and Conditional is a 500-class
// invariant breach — an RPC method has no rows for a condition to evaluate against.
// It also pins the Environment plumbing: the check receives the decode-stamped
// decision context.
func TestRPCDecoder_Decode_permissionGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		grants          map[accesstypes.Permission][]accesstypes.Resource
		conditional     map[accesstypes.Permission][]accesstypes.Resource
		permCheckErr    error
		wantForbidden   bool
		wantErrContains string
	}{
		{
			name: "granted method decodes",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Execute: {rpcEnforcedMethod},
			},
		},
		{
			name:          "missing grant is Forbidden",
			wantForbidden: true,
		},
		{
			name: "conditional grant is an invariant breach, not Forbidden",
			conditional: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.Execute: {rpcEnforcedMethod},
			},
			wantErrContains: "invariant breach",
		},
		{
			name:            "permission check error propagates",
			permCheckErr:    errors.New("engine unavailable"),
			wantErrContains: "engine unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			userPermissions := &fakeUserPermissions{granted: tt.grants, conditional: tt.conditional, err: tt.permCheckErr}
			decoder, err := NewRPCDecoder[rpcEnforcementRequest](
				func(*http.Request) UserPermissions { return userPermissions },
				rpcEnforcedMethod, accesstypes.Execute,
			)
			if err != nil {
				t.Fatalf("NewRPCDecoder() error = %v", err)
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"name":"n"}`))

			_, err = decoder.Decode(req, testScope)

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

			if len(userPermissions.gotEnvs) != 1 {
				t.Fatalf("Check calls = %d, want 1", len(userPermissions.gotEnvs))
			}
			if _, ok := userPermissions.gotEnvs[0].Now(); !ok {
				t.Error("Check() Environment carries no now; the decoder must stamp the decision context")
			}
		})
	}
}
