package resource

import (
	"context"
	"testing"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

// stubPermissions is a UserPermissions that grants everything it is asked and
// records what it was asked, so masking and routing can be asserted.
type stubPermissions struct {
	user       accesstypes.User
	digest     accesstypes.PermissionDigest
	domains    []accesstypes.Domain
	err        error
	checkPerms []accesstypes.Permission
}

func (s *stubPermissions) Check(_ context.Context, _ accesstypes.Environment, _ accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	s.checkPerms = append(s.checkPerms, perm)
	if s.err != nil {
		return nil, s.err
	}

	decisions := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		decisions[res] = accesstypes.Granted()
	}

	return decisions, nil
}

func (s *stubPermissions) PermissionDigest(context.Context, accesstypes.Scope) (accesstypes.PermissionDigest, error) {
	if s.err != nil {
		return nil, s.err
	}

	return s.digest, nil
}

func (s *stubPermissions) Domains(context.Context) ([]accesstypes.Domain, error) {
	return s.domains, nil
}

func (s *stubPermissions) User() accesstypes.User { return s.user }

func sessionCtx(username string, imp *sessioninfo.Impersonation) context.Context {
	return context.WithValue(context.Background(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
		SessionInfo:   &sessioninfo.SessionInfo{ID: ccc.Must(ccc.UUIDFromString("de6e1a12-2d4d-4c4d-aaf1-d82cb9a9eff5")), Username: username},
		Impersonation: imp,
	})
}

func TestMasked(t *testing.T) {
	t.Parallel()

	fullDigest := accesstypes.PermissionDigest{
		"documents": {accesstypes.Read: accesstypes.DigestGranted, accesstypes.Update: accesstypes.DigestConditional},
		"images":    {accesstypes.Delete: accesstypes.DigestGranted},
	}

	tests := []struct {
		name        string
		mask        accesstypes.PermissionMask
		perm        accesstypes.Permission
		wantGranted bool
		wantChecked []accesstypes.Permission
		wantDigest  accesstypes.PermissionDigest
	}{
		{
			name:        "unrestricted mask returns the checker itself",
			perm:        accesstypes.Update,
			wantGranted: true,
			wantChecked: []accesstypes.Permission{accesstypes.Update},
			wantDigest:  fullDigest,
		},
		{
			name:        "allowed permission delegates to policy",
			mask:        accesstypes.MaskPermissions(accesstypes.List, accesstypes.Read),
			perm:        accesstypes.Read,
			wantGranted: true,
			wantChecked: []accesstypes.Permission{accesstypes.Read},
			wantDigest:  accesstypes.PermissionDigest{"documents": {accesstypes.Read: accesstypes.DigestGranted}},
		},
		{
			name:        "masked permission is denied without consulting policy",
			mask:        accesstypes.MaskPermissions(accesstypes.List, accesstypes.Read),
			perm:        accesstypes.Update,
			wantChecked: nil,
			wantDigest:  accesstypes.PermissionDigest{"documents": {accesstypes.Read: accesstypes.DigestGranted}},
		},
		{
			name:        "mask that allows nothing denies everything and empties the digest",
			mask:        accesstypes.MaskPermissions(),
			perm:        accesstypes.Read,
			wantChecked: nil,
			wantDigest:  accesstypes.PermissionDigest{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := &stubPermissions{user: "bob", digest: fullDigest, domains: []accesstypes.Domain{"tenant"}}
			perms := Masked(stub, tt.mask)
			if tt.mask.IsZero() && perms != UserPermissions(stub) {
				t.Fatal("Masked() with the unrestricted mask did not return the checker itself")
			}

			decisions, err := perms.Check(context.Background(), accesstypes.NewEnvironment(), accesstypes.GlobalScope(), tt.perm, "documents", "images")
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			for _, res := range []accesstypes.Resource{"documents", "images"} {
				if got := decisions[res].IsGranted(); got != tt.wantGranted {
					t.Errorf("Check() %s granted = %v, want %v", res, got, tt.wantGranted)
				}
			}
			if diff := cmp.Diff(tt.wantChecked, stub.checkPerms); diff != "" {
				t.Errorf("delegated permissions mismatch (-want +got):\n%s", diff)
			}

			digest, err := perms.PermissionDigest(context.Background(), accesstypes.GlobalScope())
			if err != nil {
				t.Fatalf("PermissionDigest() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDigest, digest); diff != "" {
				t.Errorf("PermissionDigest() mismatch (-want +got):\n%s", diff)
			}

			domains, err := perms.Domains(context.Background())
			if err != nil || len(domains) != 1 || domains[0] != "tenant" {
				t.Errorf("Domains() = (%v, %v), want ([tenant], nil)", domains, err)
			}
			if perms.User() != "bob" {
				t.Errorf("User() = %q, want bob", perms.User())
			}
		})
	}
}

func TestMasked_WrapsDelegateErrors(t *testing.T) {
	t.Parallel()

	stub := &stubPermissions{user: "bob", err: errors.New("snapshot unavailable")}
	perms := Masked(stub, accesstypes.MaskPermissions(accesstypes.Read))

	if _, err := perms.Check(context.Background(), accesstypes.NewEnvironment(), accesstypes.GlobalScope(), accesstypes.Read, "documents"); err == nil {
		t.Error("Check() error = nil, want the delegate's error")
	}
	if _, err := perms.PermissionDigest(context.Background(), accesstypes.GlobalScope()); err == nil {
		t.Error("PermissionDigest() error = nil, want the delegate's error")
	}
}

// checkerRecorder builds the forUser/forRole arguments for SessionPermissions and
// records which principals they were asked for.
type checkerRecorder struct {
	users []accesstypes.User
	roles []accesstypes.Role
}

func (r *checkerRecorder) forUser(user accesstypes.User) *stubPermissions {
	r.users = append(r.users, user)

	return &stubPermissions{user: user, digest: accesstypes.PermissionDigest{"documents": {accesstypes.Update: accesstypes.DigestGranted}}}
}

func (r *checkerRecorder) forRole(role accesstypes.Role) *stubPermissions {
	r.roles = append(r.roles, role)

	return &stubPermissions{user: "role:" + accesstypes.User(role)}
}

func TestSessionPermissions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		ctx           context.Context
		unsupported   bool
		wantUsers     []accesstypes.User
		wantRoles     []accesstypes.Role
		wantUser      accesstypes.User
		wantUpdate    bool
		wantDigestLen int
	}{
		{
			name:          "ordinary session routes to the user checker, unmasked",
			ctx:           sessionCtx("alice", nil),
			wantUsers:     []accesstypes.User{"alice"},
			wantUser:      "alice",
			wantUpdate:    true,
			wantDigestLen: 1,
		},
		{
			name: "impersonated user routes to the user checker for the impersonated user and is masked",
			ctx: sessionCtx("bob", &sessioninfo.Impersonation{
				Actor:     "alice",
				Principal: accesstypes.UserPrincipal("bob"),
				Mask:      accesstypes.MaskPermissions(accesstypes.List, accesstypes.Read),
			}),
			wantUsers:     []accesstypes.User{"bob"},
			wantUser:      "bob",
			wantDigestLen: 0,
		},
		{
			name:       "role principal routes to the role checker",
			ctx:        sessionCtx("alice", &sessioninfo.Impersonation{Actor: "alice", Principal: accesstypes.RolePrincipal("PartnerViewer")}),
			wantRoles:  []accesstypes.Role{"PartnerViewer"},
			wantUser:   "role:PartnerViewer",
			wantUpdate: true,
		},
		{
			name:        "role principal without a role checker fails closed with the actor as User()",
			ctx:         sessionCtx("alice", &sessioninfo.Impersonation{Actor: "alice", Principal: accesstypes.RolePrincipal("PartnerViewer")}),
			unsupported: true,
			wantUser:    "alice",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := &checkerRecorder{}
			var perms UserPermissions
			if tt.unsupported {
				perms = SessionPermissions(tt.ctx, rec.forUser, RolePrincipalsUnsupported)
			} else {
				perms = SessionPermissions(tt.ctx, rec.forUser, rec.forRole)
			}

			if diff := cmp.Diff(tt.wantUsers, rec.users); diff != "" {
				t.Errorf("forUser calls mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantRoles, rec.roles); diff != "" {
				t.Errorf("forRole calls mismatch (-want +got):\n%s", diff)
			}
			if perms.User() != tt.wantUser {
				t.Errorf("User() = %q, want %q", perms.User(), tt.wantUser)
			}

			decisions, err := perms.Check(context.Background(), accesstypes.NewEnvironment(), accesstypes.GlobalScope(), accesstypes.Update, "documents")
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if got := decisions["documents"].IsGranted(); got != tt.wantUpdate {
				t.Errorf("Check(Update) granted = %v, want %v", got, tt.wantUpdate)
			}

			digest, err := perms.PermissionDigest(context.Background(), accesstypes.GlobalScope())
			if err != nil {
				t.Fatalf("PermissionDigest() error = %v", err)
			}
			if len(digest) != tt.wantDigestLen {
				t.Errorf("PermissionDigest() = %v, want %d entries", digest, tt.wantDigestLen)
			}
		})
	}
}

func TestUserEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{
			name: "ordinary session",
			ctx:  sessionCtx("alice@example.com", nil),
			want: "alice@example.com (de6e1a12-2d4d-4c4d-aaf1-d82cb9a9eff5)",
		},
		{
			name: "impersonated user names the actor first",
			ctx:  sessionCtx("bob@partner.org", &sessioninfo.Impersonation{Actor: "alice@example.com", Principal: accesstypes.UserPrincipal("bob@partner.org")}),
			want: "alice@example.com impersonating bob@partner.org (de6e1a12-2d4d-4c4d-aaf1-d82cb9a9eff5)",
		},
		{
			name: "impersonated role names the actor and the role",
			ctx:  sessionCtx("alice@example.com", &sessioninfo.Impersonation{Actor: "alice@example.com", Principal: accesstypes.RolePrincipal("PartnerViewer")}),
			want: "alice@example.com as role PartnerViewer (de6e1a12-2d4d-4c4d-aaf1-d82cb9a9eff5)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := UserEvent(tt.ctx); got != tt.want {
				t.Errorf("UserEvent() = %q, want %q", got, tt.want)
			}
			if got, want := UserProcessEvent(tt.ctx, "nightly"), tt.want+": Process nightly"; got != want {
				t.Errorf("UserProcessEvent() = %q, want %q", got, want)
			}
		})
	}
}
