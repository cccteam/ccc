package integration

// custom_role_test: a custom role is the store's, created in one sector through the user
// manager beside the release's default roles, and the manager validates no grant name. A
// grant on a resource the release does not declare is left out when the policy compiles
// and reported by the deploy's role check (access.Client.CheckPolicy) as a skipped grant;
// the role's other grants hold, so a check answers the declared grants granted and the
// undeclared one denied.

import (
	"testing"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
)

func TestCustomRole_undeclaredGrantIsSkipped(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	client := newAccessClient(t, db, crew.Roles())
	manager := client.UserManager()

	const (
		role       accesstypes.Role     = "SectorAuditor"
		login      accesstypes.User     = "auditor"
		declared   accesstypes.Resource = "Missions"
		undeclared accesstypes.Resource = "Ledgers"
	)
	held := accesstypes.DomainPolicyScope(anvil)
	if err := manager.AddRole(ctx, held, role); err != nil {
		t.Fatalf("access.UserManager.AddRole() error = %v", err)
	}
	// The manager validates no grant name: the grant on the undeclared resource is
	// written as given, beside two the release declares.
	if err := manager.AddRoleGrants(ctx, held, role,
		access.GrantRow{Permission: accesstypes.List, Resource: declared},
		access.GrantRow{Permission: accesstypes.Read, Resource: declared},
		access.GrantRow{Permission: accesstypes.List, Resource: undeclared},
	); err != nil {
		t.Fatalf("access.UserManager.AddRoleGrants() error = %v", err)
	}
	if err := manager.AddUserRoles(ctx, held, login, role); err != nil {
		t.Fatalf("access.UserManager.AddUserRoles() error = %v", err)
	}

	t.Run("the deploy check lists the undeclared grant as skipped", func(t *testing.T) {
		t.Parallel()

		warnings, err := client.CheckPolicy(ctx)
		if err != nil {
			t.Fatalf("access.Client.CheckPolicy() error = %v", err)
		}
		var skipped []*access.SkippedGrant
		for _, w := range warnings {
			if sg, ok := w.(*access.SkippedGrant); ok {
				skipped = append(skipped, sg)
			}
		}
		if len(skipped) != 1 {
			t.Fatalf("CheckPolicy() listed %d skipped grant(s), want exactly the undeclared one: %v", len(skipped), skipped)
		}
		sg := skipped[0]
		if sg.Scope != held || sg.Subject != "role "+string(role) || sg.Permission != accesstypes.List || sg.Resource != undeclared || sg.Condition != "" {
			t.Errorf("SkippedGrant = %+v, want %s's unconditional List on %s in %s", sg, role, undeclared, held)
		}
	})

	// The store writes signal a reload and the swap is asynchronous: once the declared
	// grant shows, every answer below reads a policy that holds the whole role.
	checker := client.ForUser(login)
	scope := accesstypes.DomainScope(anvil)
	if d := decisionFor(ctx, t, checker, scope, accesstypes.List, declared, time.Now().Add(15*time.Second)); !d.IsGranted() {
		t.Fatalf("List %s for a login holding %s = %s, want granted before the cases run", declared, role, d)
	}

	tests := []struct {
		name        string
		permission  accesstypes.Permission
		resource    accesstypes.Resource
		wantGranted bool
	}{
		{name: "the declared grant is granted", permission: accesstypes.List, resource: declared, wantGranted: true},
		{name: "the undeclared grant is denied", permission: accesstypes.List, resource: undeclared, wantGranted: false},
		{name: "the role's other grant holds", permission: accesstypes.Read, resource: declared, wantGranted: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decisions, err := checker.Check(ctx, accesstypes.NewEnvironment().WithNow(time.Now()), scope, tt.permission, tt.resource)
			if err != nil {
				t.Fatalf("Check(%s, %s) error = %v", tt.permission, tt.resource, err)
			}
			if got := decisions[tt.resource]; got.IsGranted() != tt.wantGranted {
				t.Errorf("%s %s for a login holding %s = %s, want granted = %t", tt.permission, tt.resource, role, got, tt.wantGranted)
			}
		})
	}
}
