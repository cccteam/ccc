package resource

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

// digestStubPermissions scripts PermissionDigest and records the scope each
// call asked for. Check and User exist only to satisfy UserPermissions.
type digestStubPermissions struct {
	digest    accesstypes.PermissionDigest
	err       error
	gotScopes []accesstypes.Scope
}

func (s *digestStubPermissions) Check(context.Context, accesstypes.Environment, accesstypes.Scope, accesstypes.Permission, ...accesstypes.Resource) (accesstypes.Decisions, error) {
	return accesstypes.Decisions{}, nil
}

func (s *digestStubPermissions) PermissionDigest(_ context.Context, scope accesstypes.Scope) (accesstypes.PermissionDigest, error) {
	s.gotScopes = append(s.gotScopes, scope)
	if s.err != nil {
		return nil, s.err
	}

	return s.digest, nil
}

func (s *digestStubPermissions) HasGrants(context.Context, accesstypes.Scope) (bool, error) {
	return false, nil
}

func (s *digestStubPermissions) Domains(context.Context) ([]accesstypes.Domain, error) {
	return []accesstypes.Domain{}, nil
}

func (s *digestStubPermissions) User() accesstypes.User { return "dana" }

// TestPermissionDigestHandler pins the digest endpoint's wire contract: the
// scope is the request's input (?domain= names a tenant partition, absence
// means global), the payload is the checker's digest verbatim, and a checker
// error surfaces as an error response, never a partial payload.
// formerNamesStub answers the former names of one renamed field and one renamed method,
// in one permission scope.
type formerNamesStub struct {
	scope accesstypes.PermissionScope
}

func (f formerNamesStub) FormerTagName(scope accesstypes.PermissionScope, res accesstypes.Resource, tag accesstypes.Tag) (accesstypes.Tag, bool) {
	if scope == f.scope && res == "articles" && tag == "headline" {
		return "title", true
	}

	return "", false
}

func (f formerNamesStub) FormerName(scope accesstypes.PermissionScope, res accesstypes.Resource) (accesstypes.Resource, bool) {
	if scope == f.scope && res == "publish" {
		return "release", true
	}

	return "", false
}

// TestPermissionDigestHandler_formerNames pins the mirror: a renamed field's and a
// renamed method's entries appear under their former names too, with the same states,
// in the permission scope the request names; an entry the engine already answered
// under the former name is left alone; a digest without renamed targets is unchanged.
func TestPermissionDigestHandler_formerNames(t *testing.T) {
	t.Parallel()

	renamed := accesstypes.PermissionDigest{
		"articles":          {"Read": accesstypes.DigestGranted},
		"articles.headline": {"Read": accesstypes.DigestGranted, "Update": accesstypes.DigestConditional},
		"publish":           {"Execute": accesstypes.DigestGranted},
	}
	tests := []struct {
		name   string
		url    string
		scope  accesstypes.PermissionScope
		digest accesstypes.PermissionDigest
		want   accesstypes.PermissionDigest
	}{
		{
			name:   "the global scope mirrors the field and the method",
			url:    "/api/permission-digest",
			scope:  accesstypes.GlobalPermissionScope,
			digest: renamed,
			want: accesstypes.PermissionDigest{
				"articles":          {"Read": accesstypes.DigestGranted},
				"articles.headline": {"Read": accesstypes.DigestGranted, "Update": accesstypes.DigestConditional},
				"articles.title":    {"Read": accesstypes.DigestGranted, "Update": accesstypes.DigestConditional},
				"publish":           {"Execute": accesstypes.DigestGranted},
				"release":           {"Execute": accesstypes.DigestGranted},
			},
		},
		{
			name:   "a domain request asks the domain permission scope",
			url:    "/api/permission-digest?domain=tenant1",
			scope:  accesstypes.DomainPermissionScope,
			digest: renamed,
			want: accesstypes.PermissionDigest{
				"articles":          {"Read": accesstypes.DigestGranted},
				"articles.headline": {"Read": accesstypes.DigestGranted, "Update": accesstypes.DigestConditional},
				"articles.title":    {"Read": accesstypes.DigestGranted, "Update": accesstypes.DigestConditional},
				"publish":           {"Execute": accesstypes.DigestGranted},
				"release":           {"Execute": accesstypes.DigestGranted},
			},
		},
		{
			name:   "names known in the other scope do not mirror",
			url:    "/api/permission-digest",
			scope:  accesstypes.DomainPermissionScope,
			digest: renamed,
			want:   renamed,
		},
		{
			name:  "an entry the engine answered under the former name stands",
			url:   "/api/permission-digest",
			scope: accesstypes.GlobalPermissionScope,
			digest: accesstypes.PermissionDigest{
				"articles.headline": {"Read": accesstypes.DigestGranted},
				"articles.title":    {"Read": accesstypes.DigestConditional},
			},
			want: accesstypes.PermissionDigest{
				"articles.headline": {"Read": accesstypes.DigestGranted},
				"articles.title":    {"Read": accesstypes.DigestConditional},
			},
		},
		{
			name:   "a digest without renamed targets is unchanged",
			url:    "/api/permission-digest",
			scope:  accesstypes.GlobalPermissionScope,
			digest: accesstypes.PermissionDigest{"employees": {"Read": accesstypes.DigestGranted}},
			want:   accesstypes.PermissionDigest{"employees": {"Read": accesstypes.DigestGranted}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := &digestStubPermissions{digest: maps.Clone(tt.digest)}
			handler := PermissionDigestHandler(func(*http.Request) UserPermissions { return stub }, WithFormerNames(formerNamesStub{scope: tt.scope}))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.url, http.NoBody)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("response code = %d, want 200 (body %q)", rr.Code, rr.Body.String())
			}
			var got accesstypes.PermissionDigest
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshaling response %q: %v", rr.Body.String(), err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("digest payload mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPermissionDigestHandler(t *testing.T) {
	t.Parallel()

	digest := accesstypes.PermissionDigest{
		"employees":      {"Read": accesstypes.DigestGranted, "Update": accesstypes.DigestConditional},
		"employees.name": {"Read": accesstypes.DigestGranted},
	}

	tests := []struct {
		name       string
		url        string
		stub       *digestStubPermissions
		wantScope  accesstypes.Scope
		wantStatus int
		wantBody   accesstypes.PermissionDigest
	}{
		{
			name:       "no domain asks for the global scope",
			url:        "/api/permission-digest",
			stub:       &digestStubPermissions{digest: digest},
			wantScope:  accesstypes.GlobalScope(),
			wantStatus: http.StatusOK,
			wantBody:   digest,
		},
		{
			name:       "domain names one tenant partition",
			url:        "/api/permission-digest?domain=tenant1",
			stub:       &digestStubPermissions{digest: accesstypes.PermissionDigest{}},
			wantScope:  accesstypes.DomainScope("tenant1"),
			wantStatus: http.StatusOK,
			wantBody:   accesstypes.PermissionDigest{},
		},
		{
			name:       "checker error is an error response",
			url:        "/api/permission-digest",
			stub:       &digestStubPermissions{err: errors.New("engine unavailable")},
			wantScope:  accesstypes.GlobalScope(),
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := PermissionDigestHandler(func(*http.Request) UserPermissions { return tt.stub })

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.url, http.NoBody)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("response code = %d, want %d (body %q)", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if len(tt.stub.gotScopes) != 1 || tt.stub.gotScopes[0] != tt.wantScope {
				t.Errorf("PermissionDigest scopes = %v, want [%v]", tt.stub.gotScopes, tt.wantScope)
			}
			if tt.wantBody == nil {
				return
			}

			var got accesstypes.PermissionDigest
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshaling response %q: %v", rr.Body.String(), err)
			}
			if diff := cmp.Diff(tt.wantBody, got); diff != "" {
				t.Errorf("digest payload mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
