package resource

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

const computedEnforcedResource = accesstypes.Resource("computedEnforcementResources")

// computedEnforcementResource carries no database tags: a computed resource has no
// table, and the decoder must not depend on database metadata.
type computedEnforcementResource struct {
	ID     ccc.UUID
	Public string
	Tagged string
}

func (computedEnforcementResource) Resource() accesstypes.Resource { return computedEnforcedResource }

func (computedEnforcementResource) DefaultConfig() Config { return Config{} }

type computedEnforcementRequest struct {
	ID     ccc.UUID `json:"id"     perm:"-"`
	Public string   `json:"public"`
	Tagged string   `json:"tagged"`
}

func TestComputedQueryDecoder_Decode_permissionEnforcement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		target          string
		grants          map[accesstypes.Permission][]accesstypes.Resource
		conditional     map[accesstypes.Permission][]accesstypes.Resource
		permCheckErr    error
		wantForbidden   bool
		wantErrContains string
		wantFields      []accesstypes.Field
	}{
		{
			name:          "missing resource-level grant is Forbidden",
			target:        "/",
			grants:        map[accesstypes.Permission][]accesstypes.Resource{},
			wantForbidden: true,
		},
		{
			name:   "missing resource-level grant is Forbidden even with every field grant",
			target: "/",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.List: {computedEnforcedResource + ".public", computedEnforcedResource + ".tagged"},
			},
			wantForbidden: true,
		},
		{
			name:   "explicitly requested field without its grant is Forbidden",
			target: "/?columns=public,tagged",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.List: {computedEnforcedResource, computedEnforcedResource + ".public"},
			},
			wantForbidden: true,
		},
		{
			name:   "explicitly requested granted fields decode to exactly those fields",
			target: "/?columns=public",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.List: {computedEnforcedResource, computedEnforcedResource + ".public"},
			},
			wantFields: []accesstypes.Field{"Public"},
		},
		{
			name:   "no requested fields narrows to accessible fields silently",
			target: "/",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.List: {computedEnforcedResource, computedEnforcedResource + ".public"},
			},
			wantFields: []accesstypes.Field{"ID", "Public"},
		},
		{
			name:   "no requested fields with every grant materializes every field",
			target: "/",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.List: {computedEnforcedResource, computedEnforcedResource + ".public", computedEnforcedResource + ".tagged"},
			},
			wantFields: []accesstypes.Field{"ID", "Public", "Tagged"},
		},
		{
			name:            "permission check error propagates",
			target:          "/",
			permCheckErr:    errors.New("engine unavailable"),
			wantErrContains: "engine unavailable",
		},
		{
			name:   "conditional resource-level grant is an invariant breach, not Forbidden",
			target: "/",
			conditional: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.List: {computedEnforcedResource},
			},
			wantErrContains: "invariant breach",
		},
		{
			name:   "conditional grant on an explicitly requested field is an invariant breach",
			target: "/?columns=public",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.List: {computedEnforcedResource},
			},
			conditional: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.List: {computedEnforcedResource + ".public"},
			},
			wantErrContains: "invariant breach",
		},
		{
			name:   "conditional field grant on the narrowing path is an invariant breach",
			target: "/",
			grants: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.List: {computedEnforcedResource, computedEnforcedResource + ".public"},
			},
			conditional: map[accesstypes.Permission][]accesstypes.Resource{
				accesstypes.List: {computedEnforcedResource + ".tagged"},
			},
			wantErrContains: "invariant breach",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decoder := MustNewComputedQueryDecoder[computedEnforcementResource, computedEnforcementRequest](accesstypes.List)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, http.NoBody)
			userPermissions := &fakeUserPermissions{granted: tt.grants, conditional: tt.conditional, err: tt.permCheckErr}

			qSet, err := decoder.Decode(req, userPermissions, testScope)

			if tt.wantForbidden || tt.wantErrContains != "" {
				if err == nil {
					t.Fatal("ComputedQueryDecoder.Decode() expected an error, got nil")
				}
				if httpio.HasForbidden(err) != tt.wantForbidden {
					t.Errorf("ComputedQueryDecoder.Decode() error forbidden = %v, want %v: %v", httpio.HasForbidden(err), tt.wantForbidden, err)
				}
				if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("ComputedQueryDecoder.Decode() error = %v, want error containing %q", err, tt.wantErrContains)
				}

				return
			}
			if err != nil {
				t.Fatalf("ComputedQueryDecoder.Decode() error = %v", err)
			}

			gotFields := slices.Sorted(slices.Values(qSet.Fields()))
			wantFields := slices.Sorted(slices.Values(tt.wantFields))
			if !slices.Equal(gotFields, wantFields) {
				t.Errorf("QuerySet.Fields() = %v, want %v", gotFields, wantFields)
			}

			for _, scope := range userPermissions.gotScopes {
				if scope != testScope {
					t.Errorf("Check() scope = %v, want %v", scope, testScope)
				}
			}
		})
	}
}
