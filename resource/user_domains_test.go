package resource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// domainsStubPermissions scripts Domains; Check, PermissionDigest, and User
// exist only to satisfy UserPermissions.
type domainsStubPermissions struct {
	domains []accesstypes.Domain
	err     error
}

func (s *domainsStubPermissions) Check(context.Context, accesstypes.Environment, accesstypes.Scope, accesstypes.Permission, ...accesstypes.Resource) (accesstypes.Decisions, error) {
	return accesstypes.Decisions{}, nil
}

func (s *domainsStubPermissions) PermissionDigest(context.Context, accesstypes.Scope) (accesstypes.PermissionDigest, error) {
	return accesstypes.PermissionDigest{}, nil
}

func (s *domainsStubPermissions) Domains(context.Context) ([]accesstypes.Domain, error) {
	return s.domains, s.err
}

func (s *domainsStubPermissions) User() accesstypes.User { return "dana" }

// TestUserDomainsHandler pins the user-domains wire contract: the checker's
// sorted list verbatim, an empty membership as [] (never null, so a picker
// iterates without a null guard), and a checker error as an error response.
func TestUserDomainsHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		stub       *domainsStubPermissions
		wantStatus int
		wantBody   string
	}{
		{
			name:       "membership lists verbatim",
			stub:       &domainsStubPermissions{domains: []accesstypes.Domain{"tenant1", "tenant2"}},
			wantStatus: http.StatusOK,
			wantBody:   `["tenant1","tenant2"]`,
		},
		{
			name:       "no membership is an empty array, never null",
			stub:       &domainsStubPermissions{},
			wantStatus: http.StatusOK,
			wantBody:   `[]`,
		},
		{
			name:       "checker error is an error response",
			stub:       &domainsStubPermissions{err: errors.New("engine unavailable")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := UserDomainsHandler(func(*http.Request) UserPermissions { return tt.stub })

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/user-domains", http.NoBody)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("response code = %d, want %d (body %q)", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if tt.wantBody != "" && strings.TrimSpace(rr.Body.String()) != tt.wantBody {
				t.Errorf("body = %q, want %q", strings.TrimSpace(rr.Body.String()), tt.wantBody)
			}
		})
	}
}
