package resource

import (
	"context"
	"slices"
	"testing"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/session/sessioninfo"
	"github.com/cccteam/session/signin"
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
	// global makes the checker hold a grant in the global partition: the
	// application's staff.
	global bool
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

func (s *stubPermissions) HasGrants(_ context.Context, scope accesstypes.Scope) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	domain, ok := scope.Domain()
	if !ok {
		return s.global, nil
	}

	return slices.Contains(s.domains, domain), nil
}

func (s *stubPermissions) Domains(context.Context) ([]accesstypes.Domain, error) {
	return s.domains, nil
}

func (s *stubPermissions) Reaches(context.Context, accesstypes.Domain) (bool, error) {
	return true, nil
}

func (s *stubPermissions) User() accesstypes.User { return s.user }

// stubRolePermissions is a RolePermissions — deliberately without User(), as
// a role checker is not anyone — that grants everything it is asked, so the
// role route can be asserted to accept such a checker and to supply User()
// itself.
type stubRolePermissions struct {
	role accesstypes.Role
}

func (s *stubRolePermissions) Check(_ context.Context, _ accesstypes.Environment, _ accesstypes.Scope, _ accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	decisions := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		decisions[res] = accesstypes.Granted()
	}

	return decisions, nil
}

func (s *stubRolePermissions) PermissionDigest(context.Context, accesstypes.Scope) (accesstypes.PermissionDigest, error) {
	return accesstypes.PermissionDigest{"documents": {accesstypes.Update: accesstypes.DigestGranted}}, nil
}

func (s *stubRolePermissions) HasGrants(_ context.Context, scope accesstypes.Scope) (bool, error) {
	domain, ok := scope.Domain()

	return ok && domain == "tenant", nil
}

func sessionCtx(username string, imp *sessioninfo.Impersonation, events ...sessioninfo.AuthEvent) context.Context {
	return context.WithValue(context.Background(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
		SessionInfo:   &sessioninfo.SessionInfo{ID: ccc.Must(ccc.UUIDFromString("de6e1a12-2d4d-4c4d-aaf1-d82cb9a9eff5")), Username: username},
		Impersonation: imp,
		AuthEvents:    events,
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
		wantDomains []accesstypes.Domain
	}{
		{
			name:        "unrestricted mask returns the checker itself",
			perm:        accesstypes.Update,
			wantGranted: true,
			wantChecked: []accesstypes.Permission{accesstypes.Update},
			wantDigest:  fullDigest,
			wantDomains: []accesstypes.Domain{"tenant"},
		},
		{
			name:        "allowed permission delegates to policy",
			mask:        accesstypes.MaskPermissions(accesstypes.DenyAll(), accesstypes.List, accesstypes.Read),
			perm:        accesstypes.Read,
			wantGranted: true,
			wantChecked: []accesstypes.Permission{accesstypes.Read},
			wantDigest:  accesstypes.PermissionDigest{"documents": {accesstypes.Read: accesstypes.DigestGranted}},
			wantDomains: []accesstypes.Domain{"tenant"},
		},
		{
			name:        "masked permission is denied without consulting policy",
			mask:        accesstypes.MaskPermissions(accesstypes.DenyAll(), accesstypes.List, accesstypes.Read),
			perm:        accesstypes.Update,
			wantChecked: nil,
			wantDigest:  accesstypes.PermissionDigest{"documents": {accesstypes.Read: accesstypes.DigestGranted}},
			wantDomains: []accesstypes.Domain{"tenant"},
		},
		{
			name:        "mask that allows nothing denies everything, empties the digest and lists no domain",
			mask:        accesstypes.DenyAll(),
			perm:        accesstypes.Read,
			wantChecked: nil,
			wantDigest:  accesstypes.PermissionDigest{},
			wantDomains: []accesstypes.Domain{},
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
			if err != nil {
				t.Fatalf("Domains() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDomains, domains); diff != "" {
				t.Errorf("Domains() mismatch (-want +got):\n%s", diff)
			}
			has, err := perms.HasGrants(context.Background(), accesstypes.DomainScope("tenant"))
			if err != nil {
				t.Fatalf("HasGrants() error = %v", err)
			}
			if want := len(tt.wantDomains) > 0; has != want {
				t.Errorf("HasGrants(tenant) = %v, want %v", has, want)
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
	perms := Masked(stub, accesstypes.MaskPermissions(accesstypes.DenyAll(), accesstypes.Read))

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

	return &stubPermissions{user: user, digest: accesstypes.PermissionDigest{"documents": {accesstypes.Update: accesstypes.DigestGranted}}, domains: []accesstypes.Domain{"tenant"}}
}

func (r *checkerRecorder) forRole(role accesstypes.Role) *stubRolePermissions {
	r.roles = append(r.roles, role)

	return &stubRolePermissions{role: role}
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
		// wantDomains is the roster (tenant, other) filtered by the session's
		// footholds: the checker holds a grant in tenant alone, and a mask
		// that allows nothing it holds leaves no foothold.
		wantDomains []accesstypes.Domain
	}{
		{
			name:          "ordinary session routes to the user checker, unmasked",
			ctx:           sessionCtx("alice", nil),
			wantUsers:     []accesstypes.User{"alice"},
			wantUser:      "alice",
			wantUpdate:    true,
			wantDigestLen: 1,
			wantDomains:   []accesstypes.Domain{"tenant"},
		},
		{
			name: "impersonated user routes to the user checker for the impersonated user and is masked",
			ctx: sessionCtx("bob", &sessioninfo.Impersonation{
				Actor:     "alice",
				Principal: accesstypes.UserPrincipal("bob"),
				Mask:      accesstypes.MaskPermissions(accesstypes.DenyAll(), accesstypes.List, accesstypes.Read),
			}),
			wantUsers:     []accesstypes.User{"bob"},
			wantUser:      "bob",
			wantDigestLen: 0,
			wantDomains:   []accesstypes.Domain{},
		},
		{
			name:          "role principal routes to the role checker, which has no User(); the session's effective identity is supplied",
			ctx:           sessionCtx("alice", &sessioninfo.Impersonation{Actor: "alice", Principal: accesstypes.RolePrincipal("PartnerViewer")}),
			wantRoles:     []accesstypes.Role{"PartnerViewer"},
			wantUser:      "alice",
			wantUpdate:    true,
			wantDigestLen: 1,
			wantDomains:   []accesstypes.Domain{"tenant"},
		},
		{
			name: "masked role principal is attenuated like any other session",
			ctx: sessionCtx("alice", &sessioninfo.Impersonation{
				Actor:     "alice",
				Principal: accesstypes.RolePrincipal("PartnerViewer"),
				Mask:      accesstypes.MaskPermissions(accesstypes.DenyAll(), accesstypes.List, accesstypes.Read),
			}),
			wantRoles:     []accesstypes.Role{"PartnerViewer"},
			wantUser:      "alice",
			wantDigestLen: 0,
			wantDomains:   []accesstypes.Domain{},
		},
		{
			name:        "role principal without a role checker fails closed with the session's effective identity as User()",
			ctx:         sessionCtx("alice", &sessioninfo.Impersonation{Actor: "alice", Principal: accesstypes.RolePrincipal("PartnerViewer")}),
			unsupported: true,
			wantUser:    "alice",
			wantDomains: []accesstypes.Domain{},
		},
	}
	roster := func(context.Context) ([]accesstypes.Domain, error) {
		return []accesstypes.Domain{"tenant", "other"}, nil
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := &checkerRecorder{}
			var perms UserPermissions
			if tt.unsupported {
				perms = SessionPermissions(tt.ctx, rec.forUser, RolePrincipalsUnsupported, roster, nil)
			} else {
				perms = SessionPermissions(tt.ctx, rec.forUser, rec.forRole, roster, nil)
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

			domains, err := perms.Domains(context.Background())
			if err != nil {
				t.Fatalf("Domains() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDomains, domains); diff != "" {
				t.Errorf("Domains() mismatch (-want +got):\n%s", diff)
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

// TestSessionPermissions_roster pins how the session's domains come from the
// application's roster: each tenant the roster lists where the checker has a
// foothold, sorted and without repeats; an application without tenants (nil
// roster) lists none, never nil; a roster error is the answer's error.
func TestSessionPermissions_roster(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		roster  DomainRoster
		holds   []accesstypes.Domain
		want    []accesstypes.Domain
		wantErr bool
	}{
		{
			name: "the roster filtered by footholds, sorted",
			roster: func(context.Context) ([]accesstypes.Domain, error) {
				return []accesstypes.Domain{"zeta", "alpha", "mid", "alpha"}, nil
			},
			holds: []accesstypes.Domain{"zeta", "alpha"},
			want:  []accesstypes.Domain{"alpha", "zeta"},
		},
		{
			name:   "no footholds lists nothing",
			roster: func(context.Context) ([]accesstypes.Domain, error) { return []accesstypes.Domain{"alpha"}, nil },
			want:   []accesstypes.Domain{},
		},
		{
			name:  "an application without tenants lists nothing",
			holds: []accesstypes.Domain{"alpha"},
			want:  []accesstypes.Domain{},
		},
		{
			name:    "a roster error is returned",
			roster:  func(context.Context) ([]accesstypes.Domain, error) { return nil, errors.New("roster unavailable") },
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			forUser := func(user accesstypes.User) *stubPermissions {
				return &stubPermissions{user: user, domains: tt.holds}
			}
			perms := SessionPermissions(sessionCtx("alice", nil), forUser, RolePrincipalsUnsupported, tt.roster, nil)
			got, err := perms.Domains(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Domains() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Domains() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// declarationReader scripts TenantDeclarations: a declaration per tenant, nil
// for a tenant that declares nothing, an error for every read when set, and a
// count of the reads per tenant.
type declarationReader struct {
	declarations map[accesstypes.Domain]*signin.TenantDeclaration
	err          error
	reads        map[accesstypes.Domain]int
}

func (d *declarationReader) read(_ context.Context, domain accesstypes.Domain) (*signin.TenantDeclaration, error) {
	if d.reads == nil {
		d.reads = map[accesstypes.Domain]int{}
	}
	d.reads[domain]++
	if d.err != nil {
		return nil, d.err
	}

	return d.declarations[domain], nil
}

// TestSessionPermissions_admission pins the reach-time admission: a tenant is
// in the session's list only when the session holds a grant there and the
// tenant's declaration admits the way the session was established, or the
// session's actor is the application's staff; in a tenant the session does
// not reach it holds nothing, on every question the composed permissions
// answer.
func TestSessionPermissions_admission(t *testing.T) {
	t.Parallel()

	const (
		anvil   accesstypes.Domain = "anvil"
		bastion accesstypes.Domain = "bastion"
	)
	password := sessioninfo.AuthEvent{Method: sessioninfo.MethodPassword}
	workspace := sessioninfo.AuthEvent{Method: sessioninfo.MethodGoogle, Connection: "acme"}
	impersonation := sessioninfo.AuthEvent{Method: sessioninfo.MethodImpersonation, Connection: "alice"}
	declaresWorkspace := &signin.TenantDeclaration{Directories: []signin.Directory{{Method: sessioninfo.MethodGoogle, Connection: "acme"}}}
	workspaceOnly := &signin.TenantDeclaration{Directories: []signin.Directory{{Method: sessioninfo.MethodGoogle, Connection: "acme"}}, DirectoryOnly: true}
	asBob := &sessioninfo.Impersonation{Actor: "alice", Principal: accesstypes.UserPrincipal("bob")}
	asRole := &sessioninfo.Impersonation{Actor: "alice", Principal: accesstypes.RolePrincipal("PartnerViewer")}

	tests := []struct {
		name string
		ctx  context.Context
		// declarations is nil for an application that keeps none; reader scripts
		// one that does.
		reader *declarationReader
		// holds are the tenants the principal's checker holds a grant in; global
		// makes it, and the actor's checker, staff.
		holds  []accesstypes.Domain
		global bool
		// wantDomains is the session's tenant list; wantReaches the answer per
		// tenant of the roster (anvil, bastion) when the list is not the whole story.
		wantDomains []accesstypes.Domain
		wantReaches map[accesstypes.Domain]bool
		// wantReads is how many times each tenant's declaration was read.
		wantReads map[accesstypes.Domain]int
		wantErr   bool
	}{
		{
			name:        "an application that keeps no declarations: every tenant admits every session",
			ctx:         sessionCtx("alice", nil, workspace),
			holds:       []accesstypes.Domain{anvil, bastion},
			wantDomains: []accesstypes.Domain{anvil, bastion},
			wantReaches: map[accesstypes.Domain]bool{anvil: true, bastion: true},
		},
		{
			name:        "a tenant that declares nothing admits a password session",
			ctx:         sessionCtx("alice", nil, password),
			reader:      &declarationReader{},
			holds:       []accesstypes.Domain{anvil, bastion},
			wantDomains: []accesstypes.Domain{anvil, bastion},
			wantReads:   map[accesstypes.Domain]int{anvil: 1, bastion: 1},
		},
		{
			name:        "a tenant that declares nothing refuses a session established through a directory",
			ctx:         sessionCtx("alice", nil, workspace),
			reader:      &declarationReader{},
			holds:       []accesstypes.Domain{anvil, bastion},
			wantDomains: []accesstypes.Domain{},
			wantReaches: map[accesstypes.Domain]bool{anvil: false, bastion: false},
		},
		{
			name:        "a tenant that declares the directory admits the session that came through it",
			ctx:         sessionCtx("alice", nil, workspace),
			reader:      &declarationReader{declarations: map[accesstypes.Domain]*signin.TenantDeclaration{anvil: declaresWorkspace}},
			holds:       []accesstypes.Domain{anvil, bastion},
			wantDomains: []accesstypes.Domain{anvil},
			wantReaches: map[accesstypes.Domain]bool{anvil: true, bastion: false},
		},
		{
			name: "a tenant that declares another connection of the directory refuses it",
			ctx:  sessionCtx("alice", nil, sessioninfo.AuthEvent{Method: sessioninfo.MethodGoogle, Connection: "other"}),
			reader: &declarationReader{declarations: map[accesstypes.Domain]*signin.TenantDeclaration{
				anvil: declaresWorkspace,
			}},
			holds:       []accesstypes.Domain{anvil},
			wantDomains: []accesstypes.Domain{},
			wantReaches: map[accesstypes.Domain]bool{anvil: false},
		},
		{
			name:        "a directory-only tenant refuses a password session; the other tenant admits it",
			ctx:         sessionCtx("alice", nil, password),
			reader:      &declarationReader{declarations: map[accesstypes.Domain]*signin.TenantDeclaration{anvil: workspaceOnly}},
			holds:       []accesstypes.Domain{anvil, bastion},
			wantDomains: []accesstypes.Domain{bastion},
			wantReaches: map[accesstypes.Domain]bool{anvil: false, bastion: true},
		},
		{
			name:        "a directory-only tenant admits the session that came through its directory",
			ctx:         sessionCtx("alice", nil, workspace),
			reader:      &declarationReader{declarations: map[accesstypes.Domain]*signin.TenantDeclaration{anvil: workspaceOnly}},
			holds:       []accesstypes.Domain{anvil, bastion},
			wantDomains: []accesstypes.Domain{anvil},
		},
		{
			name:        "a session that records no establishment is not through a directory",
			ctx:         sessionCtx("alice", nil),
			reader:      &declarationReader{declarations: map[accesstypes.Domain]*signin.TenantDeclaration{anvil: workspaceOnly, bastion: declaresWorkspace}},
			holds:       []accesstypes.Domain{anvil, bastion},
			wantDomains: []accesstypes.Domain{bastion},
		},
		{
			name:        "the application's staff reach every tenant",
			ctx:         sessionCtx("alice", nil, password),
			reader:      &declarationReader{declarations: map[accesstypes.Domain]*signin.TenantDeclaration{anvil: workspaceOnly}},
			holds:       []accesstypes.Domain{anvil, bastion},
			global:      true,
			wantDomains: []accesstypes.Domain{anvil, bastion},
			wantReaches: map[accesstypes.Domain]bool{anvil: true, bastion: true},
		},
		{
			name:        "an impersonated session is established by its impersonation, which a directory-only tenant refuses",
			ctx:         sessionCtx("bob", asBob, impersonation),
			reader:      &declarationReader{declarations: map[accesstypes.Domain]*signin.TenantDeclaration{anvil: workspaceOnly}},
			holds:       []accesstypes.Domain{anvil, bastion},
			wantDomains: []accesstypes.Domain{bastion},
			wantReaches: map[accesstypes.Domain]bool{anvil: false, bastion: true},
		},
		{
			name:        "an impersonated session reaches by its actor's rights: a staff actor reaches the directory-only tenant",
			ctx:         sessionCtx("bob", asBob, impersonation),
			reader:      &declarationReader{declarations: map[accesstypes.Domain]*signin.TenantDeclaration{anvil: workspaceOnly}},
			holds:       []accesstypes.Domain{anvil, bastion},
			global:      true,
			wantDomains: []accesstypes.Domain{anvil, bastion},
		},
		{
			// The role checker (stubRolePermissions) holds a grant in "tenant" alone, so
			// the list stays empty; the reach is the actor's, who is not staff.
			name:        "a role principal is admitted like any session, by its actor's rights",
			ctx:         sessionCtx("alice", asRole, impersonation),
			reader:      &declarationReader{declarations: map[accesstypes.Domain]*signin.TenantDeclaration{anvil: workspaceOnly}},
			wantDomains: []accesstypes.Domain{},
			wantReaches: map[accesstypes.Domain]bool{anvil: false, bastion: true},
		},
		{
			name:        "a tenant the session holds no grant in is not asked",
			ctx:         sessionCtx("alice", nil, password),
			reader:      &declarationReader{},
			holds:       []accesstypes.Domain{anvil},
			wantDomains: []accesstypes.Domain{anvil},
			wantReads:   map[accesstypes.Domain]int{anvil: 1},
		},
		{
			name:    "a reader error is the answer's error",
			ctx:     sessionCtx("alice", nil, password),
			reader:  &declarationReader{err: errors.New("declarations unavailable")},
			holds:   []accesstypes.Domain{anvil},
			wantErr: true,
		},
	}
	roster := func(context.Context) ([]accesstypes.Domain, error) {
		return []accesstypes.Domain{bastion, anvil}, nil
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			forUser := func(user accesstypes.User) *stubPermissions {
				return &stubPermissions{user: user, digest: accesstypes.PermissionDigest{"documents": {accesstypes.Read: accesstypes.DigestGranted}}, domains: tt.holds, global: tt.global}
			}
			forRole := func(accesstypes.Role) *stubRolePermissions {
				return &stubRolePermissions{}
			}
			var declarations TenantDeclarations
			if tt.reader != nil {
				declarations = tt.reader.read
			}
			perms := SessionPermissions(tt.ctx, forUser, forRole, roster, declarations)

			domains, err := perms.Domains(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Domains() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if _, err := perms.Reaches(context.Background(), anvil); err == nil {
					t.Error("Reaches() error = nil, want the reader's error")
				}
				if _, err := perms.Check(context.Background(), accesstypes.NewEnvironment(), accesstypes.DomainScope(anvil), accesstypes.Read, "documents"); err == nil {
					t.Error("Check() error = nil, want the reader's error")
				}
				if _, err := perms.HasGrants(context.Background(), accesstypes.DomainScope(anvil)); err == nil {
					t.Error("HasGrants() error = nil, want the reader's error")
				}

				return
			}
			if diff := cmp.Diff(tt.wantDomains, domains); diff != "" {
				t.Errorf("Domains() mismatch (-want +got):\n%s", diff)
			}
			for domain, want := range tt.wantReaches {
				reaches, err := perms.Reaches(context.Background(), domain)
				if err != nil {
					t.Fatalf("Reaches(%s) error = %v", domain, err)
				}
				if reaches != want {
					t.Errorf("Reaches(%s) = %v, want %v", domain, reaches, want)
				}
				// In a tenant the session does not reach it holds nothing; in one it
				// reaches, the checker's grants answer.
				wantHolds := want && slices.Contains(tt.holds, domain)
				has, err := perms.HasGrants(context.Background(), accesstypes.DomainScope(domain))
				if err != nil {
					t.Fatalf("HasGrants(%s) error = %v", domain, err)
				}
				if has != wantHolds {
					t.Errorf("HasGrants(%s) = %v, want %v", domain, has, wantHolds)
				}
				decisions, err := perms.Check(context.Background(), accesstypes.NewEnvironment(), accesstypes.DomainScope(domain), accesstypes.Read, "documents")
				if err != nil {
					t.Fatalf("Check(%s) error = %v", domain, err)
				}
				if got := decisions["documents"].IsGranted(); got != want {
					t.Errorf("Check(%s) granted = %v, want %v", domain, got, want)
				}
				digest, err := perms.PermissionDigest(context.Background(), accesstypes.DomainScope(domain))
				if err != nil {
					t.Fatalf("PermissionDigest(%s) error = %v", domain, err)
				}
				if got := len(digest) > 0; got != want {
					t.Errorf("PermissionDigest(%s) = %v, want entries %v", domain, digest, want)
				}
			}
			// The global partition is never attenuated.
			decisions, err := perms.Check(context.Background(), accesstypes.NewEnvironment(), accesstypes.GlobalScope(), accesstypes.Read, "documents")
			if err != nil {
				t.Fatalf("Check(global) error = %v", err)
			}
			if !decisions["documents"].IsGranted() {
				t.Error("Check(global) denied, want the checker's grant")
			}
			if tt.wantReads != nil {
				if diff := cmp.Diff(tt.wantReads, tt.reader.reads); diff != "" {
					t.Errorf("declaration reads mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestSessionPermissions_admissionReadsOnce pins that one composed value reads a
// tenant's declaration once and asks the staff question once, whatever is asked
// about the tenant after: the guard, the handler's checks and the digest share
// one answer.
func TestSessionPermissions_admissionReadsOnce(t *testing.T) {
	t.Parallel()

	const anvil accesstypes.Domain = "anvil"
	reader := &declarationReader{declarations: map[accesstypes.Domain]*signin.TenantDeclaration{anvil: {DirectoryOnly: true}}}
	users := 0
	forUser := func(user accesstypes.User) *stubPermissions {
		users++

		return &stubPermissions{user: user, domains: []accesstypes.Domain{anvil}}
	}
	roster := func(context.Context) ([]accesstypes.Domain, error) {
		return []accesstypes.Domain{anvil}, nil
	}
	perms := SessionPermissions(sessionCtx("alice", nil, sessioninfo.AuthEvent{Method: sessioninfo.MethodPassword}), forUser, RolePrincipalsUnsupported, roster, reader.read)

	for range 3 {
		if reaches, err := perms.Reaches(context.Background(), anvil); err != nil || reaches {
			t.Fatalf("Reaches() = %v, %v; want false, nil", reaches, err)
		}
		if _, err := perms.Domains(context.Background()); err != nil {
			t.Fatalf("Domains() error = %v", err)
		}
		if _, err := perms.PermissionDigest(context.Background(), accesstypes.DomainScope(anvil)); err != nil {
			t.Fatalf("PermissionDigest() error = %v", err)
		}
	}
	if got := reader.reads[anvil]; got != 1 {
		t.Errorf("the declaration was read %d times, want once", got)
	}
	if users != 1 {
		t.Errorf("forUser was called %d times, want once: an ordinary session's actor is its user", users)
	}
}
