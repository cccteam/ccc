//go:build skipAuth

package integration

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// TestLastOwner_anAdministratorLeavesOnceAnotherRemains proves the last-owner rule on
// the served stack: a person leaves an administrator seat of a partition once another
// holder remains there, and the last one is refused. The governor, the one holder of
// Headquarters, the global partition's administrator role (crew.AdministratorRoles), is
// refused leaving it, in the rule's words; a seat that is no administrator role of its
// partition is left freely, his SectorMarshal held in every sector; and once the marshal
// holds Headquarters beside him, he leaves it. So an operator creates a customer's
// tenant, seats its first administrator and leaves, and no partition is left with nobody
// to seat the next.
//
// Demonstrates: tenancy.creator-seat.
func TestLastOwner_anAdministratorLeavesOnceAnotherRemains(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	seatsRoute := crewGlobalRoute(crewPartitions().Global.SeatsRoute)
	usersRoute := crewGlobalRoute(crewPartitions().Global.UsersRoute)

	governor := newBrowser(t, s, consoleAPI)
	governor.signIn(ctx, "governor")
	governorID := crewUserID(ctx, t, governor, "governor")
	marshalID := crewUserID(ctx, t, governor, "marshal")

	// The last holder of the partition's administrator role is refused, in the rule's words.
	status, body := governor.do(ctx, http.MethodPatch, seatsRoute, []byte(unseatBody(governorID, "Headquarters")))
	if status != http.StatusForbidden || !strings.Contains(string(body), "you are the last administrator: seat another before you unseat yourself") {
		t.Fatalf("the governor leaving Headquarters as its one holder: status %d, want 403 naming the rule: %s", status, body)
	}
	if row := userRow(ctx, t, governor, usersRoute, "governor"); row == nil || !slices.Contains(rolesOf(row), "Headquarters") {
		t.Fatalf("the governor's roles after the refusal = %v, want Headquarters kept", rolesOf(row))
	}

	// A seat that is no administrator role of its partition goes freely: SectorMarshal
	// held in every sector is a domain role, not the global partition's.
	if status, body := governor.do(ctx, http.MethodPatch, seatsRoute, []byte(unseatBody(governorID, "SectorMarshal"))); status != http.StatusOK {
		t.Fatalf("the governor leaving SectorMarshal held in every sector: status %d, want 200: %s", status, body)
	}

	// Another holder seated, the governor leaves Headquarters.
	if status, body := governor.do(ctx, http.MethodPatch, seatsRoute, []byte(seatBody(marshalID, "Headquarters"))); status != http.StatusOK {
		t.Fatalf("seating the marshal as Headquarters: status %d: %s", status, body)
	}
	if status, body := governor.do(ctx, http.MethodPatch, seatsRoute, []byte(unseatBody(governorID, "Headquarters"))); status != http.StatusOK {
		t.Fatalf("the governor leaving Headquarters once the marshal holds it: status %d, want 200: %s", status, body)
	}
	marshal := newBrowser(t, s, consoleAPI)
	marshal.signIn(ctx, "marshal")
	if row := userRow(ctx, t, marshal, usersRoute, "governor"); row == nil || slices.Contains(rolesOf(row), "Headquarters") {
		t.Errorf("the governor's roles once he left = %v, want Headquarters gone", rolesOf(row))
	}
}
