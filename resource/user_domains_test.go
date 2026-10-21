package resource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/session/sessioninfo"
	"github.com/cccteam/session/signin"
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

func (s *domainsStubPermissions) HasGrants(context.Context, accesstypes.Scope) (bool, error) {
	return len(s.domains) > 0, s.err
}

func (s *domainsStubPermissions) Domains(context.Context) ([]accesstypes.Domain, error) {
	return s.domains, s.err
}

func (s *domainsStubPermissions) Reaches(context.Context, accesstypes.Domain) (bool, error) {
	return true, nil
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

// TestUserDomainsHandler_admission pins the endpoint over the composed session
// permissions: the picker lists the tenants the session holds a grant in and
// reaches, so a tenant whose declaration does not admit the way the session was
// established is never offered, while the application's staff see every tenant.
func TestUserDomainsHandler_admission(t *testing.T) {
	t.Parallel()

	const (
		anvil   accesstypes.Domain = "anvil"
		bastion accesstypes.Domain = "bastion"
	)
	workspace := signin.Directory{Method: sessioninfo.MethodGoogle, Connection: "acme"}
	declarations := map[accesstypes.Domain]*signin.TenantDeclaration{
		anvil:   {Directories: []signin.Directory{workspace}, DirectoryOnly: true},
		bastion: {Directories: []signin.Directory{workspace}},
	}
	roster := func(context.Context) ([]accesstypes.Domain, error) {
		return []accesstypes.Domain{anvil, bastion}, nil
	}

	tests := []struct {
		name     string
		ctx      context.Context
		global   bool
		wantBody string
	}{
		{
			name:     "a password session lists the tenant that admits passwords alone",
			ctx:      sessionCtx("dana", nil, sessioninfo.AuthEvent{Method: sessioninfo.MethodPassword}),
			wantBody: `["bastion"]`,
		},
		{
			name:     "a session through the declared directory lists both",
			ctx:      sessionCtx("dana", nil, sessioninfo.AuthEvent{Method: sessioninfo.MethodGoogle, Connection: "acme"}),
			wantBody: `["anvil","bastion"]`,
		},
		{
			name:     "a session through another directory lists neither, as an array",
			ctx:      sessionCtx("dana", nil, sessioninfo.AuthEvent{Method: sessioninfo.MethodAzure, Connection: "tenant-id"}),
			wantBody: `[]`,
		},
		{
			name:     "the application's staff list every tenant they hold a grant in",
			ctx:      sessionCtx("dana", nil, sessioninfo.AuthEvent{Method: sessioninfo.MethodPassword}),
			global:   true,
			wantBody: `["anvil","bastion"]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			forUser := func(user accesstypes.User) *stubPermissions {
				return &stubPermissions{user: user, domains: []accesstypes.Domain{anvil, bastion}, global: tt.global}
			}
			reader := &declarationReader{declarations: declarations}
			handler := UserDomainsHandler(func(r *http.Request) UserPermissions {
				return SessionPermissions(r.Context(), forUser, RolePrincipalsUnsupported, roster, reader.read)
			})

			req := httptest.NewRequestWithContext(tt.ctx, http.MethodGet, "/api/user-domains", http.NoBody)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("response code = %d, want 200 (body %q)", rr.Code, rr.Body.String())
			}
			if got := strings.TrimSpace(rr.Body.String()); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}
