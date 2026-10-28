package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// TestLastOwner_anAdministratorLeavesOnceAnotherRemains proves the last-owner rule on
// the served stack: a person leaves an administrator seat of a partition once another
// holder remains there, and the last one is refused. The administrator, the one holder
// of Administrator, the global partition's administrator role
// (staff.AdministratorRoles), is refused leaving it in the rule's words; a seat that is
// no administrator role of its partition goes freely, TenantAdministrator held in every
// tenant, a domain role; in a tenant they created, where the create path seated them as
// its administrator, they are refused leaving as its last one and leave once a holder in
// every tenant stands beside them; and once the member holds Administrator
// beside them, they leave it. So an operator creates a customer's tenant, seats its
// first administrator and leaves, and no partition is left with nobody to seat the next.
func TestLastOwner_anAdministratorLeavesOnceAnotherRemains(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	admin := signedIn(ctx, t, s, adminUser)
	adminID := staffUserID(ctx, t, admin, adminUser)
	memberID := staffUserID(ctx, t, admin, memberUser)

	// The last holder of the partition's administrator role is refused, in the rule's words.
	status, body := admin.do(ctx, http.MethodPatch, "/api/staff-seats", []byte(unseatOp(adminID, "Administrator")))
	if status != http.StatusForbidden || !strings.Contains(string(body), "you are the last administrator: seat another before you unseat yourself") {
		t.Fatalf("the administrator leaving Administrator as its one holder: status %d, want 403 naming the rule: %s", status, body)
	}
	if roles := globalRolesOf(ctx, t, admin, adminUser); !slices.Contains(roles, "Administrator") {
		t.Fatalf("the administrator's roles after the refusal = %v, want Administrator kept", roles)
	}

	// A seat that is no administrator role of its partition goes freely: TenantAdministrator
	// held in every tenant is a domain role, not the global partition's.
	if status, body := admin.do(ctx, http.MethodPatch, "/api/staff-seats", []byte(unseatOp(adminID, "TenantAdministrator"))); status != http.StatusOK {
		t.Fatalf("the administrator leaving TenantAdministrator held in every tenant: status %d, want 200: %s", status, body)
	}

	// In a tenant: the create path seated the creator as west's administrator, and with
	// no holder in every tenant left, they are its last one, refused in the tenant's
	// words; seated in every tenant again, they leave west's seat.
	const west = "west"
	if status, body := admin.do(ctx, http.MethodPatch, "/api/resources", []byte(`[{"op":"add","path":"/tenants/`+west+`","value":{"name":"West"}}]`)); status != http.StatusOK {
		t.Fatalf("creating %s: status %d: %s", west, status, body)
	}
	if roles := tenantRolesOf(ctx, t, admin, west, adminUser); !slices.Contains(roles, "TenantAdministrator") {
		t.Fatalf("the administrator's roles in %s = %v, want TenantAdministrator: the create path seats the creator", west, roles)
	}
	westSeats := "/api/tenants/" + west + "/staff-tenant-seats"
	status, body = admin.do(ctx, http.MethodPatch, westSeats, []byte(unseatOp(adminID, "TenantAdministrator")))
	if status != http.StatusForbidden || !strings.Contains(string(body), "you are the last administrator of this tenant: seat another before you unseat yourself") {
		t.Fatalf("the administrator leaving %s's seat as its one holder: status %d, want 403 naming the rule: %s", west, status, body)
	}
	if status, body := admin.do(ctx, http.MethodPatch, "/api/staff-seats", []byte(seatOp(adminID, "TenantAdministrator"))); status != http.StatusOK {
		t.Fatalf("the administrator taking TenantAdministrator in every tenant again: status %d: %s", status, body)
	}
	if status, body := admin.do(ctx, http.MethodPatch, westSeats, []byte(unseatOp(adminID, "TenantAdministrator"))); status != http.StatusOK {
		t.Fatalf("the administrator leaving %s's seat with the every-tenant seat standing: status %d, want 200: %s", west, status, body)
	}

	// Another holder seated, the administrator leaves.
	if status, body := admin.do(ctx, http.MethodPatch, "/api/staff-seats", []byte(seatOp(memberID, "Administrator"))); status != http.StatusOK {
		t.Fatalf("seating the member as Administrator: status %d: %s", status, body)
	}
	if status, body := admin.do(ctx, http.MethodPatch, "/api/staff-seats", []byte(unseatOp(adminID, "Administrator"))); status != http.StatusOK {
		t.Fatalf("the administrator leaving Administrator once the member holds it: status %d, want 200: %s", status, body)
	}
	member := signedIn(ctx, t, s, memberUser)
	if roles := globalRolesOf(ctx, t, member, adminUser); slices.Contains(roles, "Administrator") {
		t.Errorf("the administrator's roles once they left = %v, want Administrator gone", roles)
	}
}

// seatOp is the operations body that seats the user in the role: the seat's key in the
// operation's path, the way the generated resources take theirs.
func seatOp(user, role string) string {
	return `[{"op":"add","path":"/` + user + `/` + role + `","value":{}}]`
}

// unseatOp is the operations body that removes the seat.
func unseatOp(user, role string) string {
	return `[{"op":"remove","path":"/` + user + `/` + role + `"}]`
}

// staffUserID finds a staff member's id by username through the global users route, as
// the browser b (one who may list the staff) reads it.
func staffUserID(ctx context.Context, t *testing.T, b *browser, username string) string {
	t.Helper()

	for _, row := range staffUsers(ctx, t, b) {
		if row.Username == username {
			return row.ID
		}
	}
	t.Fatalf("no staff member %q", username)

	return ""
}

// globalRolesOf reads the roles the user holds in the global partition, as the global
// users route answers the browser b.
func globalRolesOf(ctx context.Context, t *testing.T, b *browser, username string) []string {
	t.Helper()

	for _, row := range staffUsers(ctx, t, b) {
		if row.Username == username {
			return row.Roles
		}
	}

	return nil
}

// staffUser is a row of the global users route.
type staffUser struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
}

// staffUsers lists the staff auth's global partition.
func staffUsers(ctx context.Context, t *testing.T, b *browser) []staffUser {
	t.Helper()

	status, body := b.do(ctx, http.MethodGet, "/api/staff-users", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/staff-users: status %d: %s", status, body)
	}
	var rows []staffUser
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("the staff are not a list: %v: %s", err, body)
	}

	return rows
}
