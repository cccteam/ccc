package integration

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/pkg/auth/staff"
)

// TestGrants_everyUnconditionalGrantIsServed proves the deploy path delivers the committed
// roles files: for every auth the harness provisions, every role at its scope (a global role in the global scope, a domain role in the north tenant),
// and every unconditional grant with each of its fields, the engine answers granted for a
// login holding the role. The expectation is the file itself; what the test proves is that
// MigrateRoles delivers it through the application's own auth package and store prefix and
// that the engine serves it. Conditional grants are left out, since their answer depends
// on a row: each is proven by the case that names it through provesGrant.
func TestGrants_everyUnconditionalGrantIsServed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)

	tests := []struct {
		name      string
		rolesPath string
		engine    *access.Client
	}{
		{name: "staff", rolesPath: staff.RolesPath, engine: s.access},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw, err := os.ReadFile(filepath.Join("..", "..", tt.rolesPath))
			if err != nil {
				t.Fatalf("reading %s: %v", tt.rolesPath, err)
			}
			var roles access.RoleConfig
			if err := json.Unmarshal(raw, &roles); err != nil {
				t.Fatalf("parsing %s: %v", tt.rolesPath, err)
			}
			rows := unconditionalRows(accesstypes.GlobalScope(), roles.Roles.Global)
			rows = append(rows, unconditionalRows(accesstypes.DomainScope(north), roles.Roles.Domain)...)
			for i := range rows {
				row := &rows[i]
				t.Run(string(row.role), func(t *testing.T) {
					t.Parallel()

					checkGrantsServed(ctx, t, tt.engine, row)
				})
			}
		})
	}
}

// grantRow is one role of a roles file with its unconditional grants, each expanded to the
// resources the engine is asked about: the base resource and one per field.
type grantRow struct {
	scope  accesstypes.Scope
	role   accesstypes.Role
	grants []grantResources
}

// grantResources is one unconditional grant as the engine sees it.
type grantResources struct {
	permission accesstypes.Permission
	resources  []accesstypes.Resource
}

// unconditionalRows expands the roles into one row per role at its scope, carrying the
// unconditional grants alone: a conditional grant's answer depends on a row and belongs to
// the case that names it.
func unconditionalRows(scope accesstypes.Scope, roles []*access.Role) []grantRow {
	rows := make([]grantRow, 0, len(roles))
	for _, r := range roles {
		row := grantRow{scope: scope, role: r.Name}
		for _, p := range slices.Sorted(maps.Keys(r.Permissions)) {
			for _, g := range r.Permissions[p] {
				if g.Condition != "" {
					continue
				}
				resources := []accesstypes.Resource{g.Resource}
				for _, f := range g.Fields {
					resources = append(resources, accesstypes.Resource(string(g.Resource)+"."+string(f)))
				}
				row.grants = append(row.grants, grantResources{permission: p, resources: resources})
			}
		}
		rows = append(rows, row)
	}

	return rows
}

// checkGrantsServed assigns the row's role to a login of its own and asks the engine about
// every resource of every unconditional grant. The store write signals a snapshot reload
// and the swap is asynchronous, so each decision is polled until it is granted or the
// deadline passes, when the last answer stands.
func checkGrantsServed(ctx context.Context, t *testing.T, engine *access.Client, row *grantRow) {
	t.Helper()

	user := accesstypes.User("grants-" + strings.ToLower(string(row.role)))
	if err := engine.UserManager().AddUserRoles(ctx, row.scope, user, row.role); err != nil {
		t.Fatalf("AddUserRoles(%s, %s) error = %v", user, row.role, err)
	}
	checker := engine.ForUser(user)
	deadline := time.Now().Add(15 * time.Second)
	for _, g := range row.grants {
		for _, res := range g.resources {
			decision := decisionFor(ctx, t, checker, row.scope, g.permission, res, deadline)
			if !decision.IsGranted() {
				t.Errorf("%s %s for a login holding %s = %s, want granted: the roles file grants it and the engine does not serve it", g.permission, res, row.role, decision)
			}
		}
	}
}

// decisionFor polls one decision until it is granted or the deadline passes.
func decisionFor(ctx context.Context, t *testing.T, checker *access.UserChecker, scope accesstypes.Scope, permission accesstypes.Permission, res accesstypes.Resource, deadline time.Time) accesstypes.Decision {
	t.Helper()

	for {
		decisions, err := checker.Check(ctx, accesstypes.NewEnvironment().WithNow(time.Now()), scope, permission, res)
		if err != nil {
			t.Fatalf("Check(%s, %s) error = %v", permission, res, err)
		}
		if d := decisions[res]; d.IsGranted() || time.Now().After(deadline) {
			return d
		}
		time.Sleep(50 * time.Millisecond)
	}
}
