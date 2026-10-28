package resource

import (
	"slices"
	"testing"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/session/signin"
)

// TestSeatDeclared pins the directory admission's seat hook: each seat the sign-in
// handed over is written in its tenant's domain scope under the sign-in name, as the
// row names it, and no seat writes nothing.
func TestSeatDeclared(t *testing.T) {
	t.Parallel()

	seats := &fakeSeats{roles: map[accesstypes.PolicyScope][]access.RoleInfo{accesstypes.DomainPolicyScope(anvil): namedRoles("Clerk"), accesstypes.DomainPolicyScope(bastion): namedRoles("Pilot")}}
	hook := SeatDeclared(seats)
	if err := hook(t.Context(), ccc.NilUUID, "amy@lakeside.edu", []signin.DirectorySeat{{Tenant: anvil, Role: "Clerk"}, {Tenant: bastion, Role: "Pilot"}}); err != nil {
		t.Fatalf("hook error = %v", err)
	}
	if !slices.Contains(seats.held[accesstypes.DomainPolicyScope(anvil)]["Clerk"], "amy@lakeside.edu") || !slices.Contains(seats.held[accesstypes.DomainPolicyScope(bastion)]["Pilot"], "amy@lakeside.edu") {
		t.Errorf("seats = %v, want Clerk at anvil and Pilot at bastion", seats.held)
	}
	if err := hook(t.Context(), ccc.NilUUID, "amy@lakeside.edu", nil); err != nil {
		t.Errorf("hook with no seat error = %v, want nil", err)
	}
}
