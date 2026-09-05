package integration

// chain_test (design plan §9): SortieExpense creates and updates flip legal → refused
// as the mission moves underway → on_hold, proving the two-hop state binding —
// `state = 'underway'` evaluated on SortieExpenses through Sortie to Mission. Runs
// over the real engine with the Quartermaster's shipped grants.

import (
	"fmt"
	"net/http"
	"testing"
)

func TestTwoHopStateChain(t *testing.T) {
	t.Parallel()

	_, _, h := demoWorld(t)

	// Deliberately not a table: the sections move the convoy mission's state and
	// each step depends on the state the previous one left behind.

	// Underway: the quartermaster books an expense two hops below the root...
	status, body := doRequestAs(t, h, "quartermaster", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":%q,"value":{"sortieId":%q,"category":"fuel","amount":250,"note":"top-up"}}]`, opPath(anvil, "sortie-expenses"), sortieConvoyID))
	assertStatus(t, status, http.StatusOK, body)
	ids, _ := decodeRow(t, body)["sortieExpenses"].([]any)
	if len(ids) != 1 {
		t.Fatalf("created ids = %v, want one expense id: %s", ids, body)
	}
	expenseID, _ := ids[0].(string)

	// ...and corrects it.
	status, body = doRequestAs(t, h, "quartermaster", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"amount":275}}]`, opPath(anvil, "sortie-expenses/"+expenseID)))
	assertStatus(t, status, http.StatusOK, body)

	// The courier mission is on hold: its sortie's expenses are closed to the
	// quartermaster — same grant, other side of the condition.
	status, body = doRequestAs(t, h, "quartermaster", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"amount":999}}]`, opPath(anvil, "sortie-expenses/"+expenseCourierFuelID)))
	assertStatus(t, status, http.StatusForbidden, body)
	status, body = doRequestAs(t, h, "quartermaster", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":%q,"value":{"sortieId":%q,"category":"fuel","amount":1,"note":"while held"}}]`, opPath(anvil, "sortie-expenses"), sortieCourierID))
	assertStatus(t, status, http.StatusForbidden, body)

	// Hold the convoy: the flight lead may, and the expense goes read-only two hops
	// down the moment the root's state changes.
	status, body = doRequestAs(t, h, "lead", http.MethodPost, sectorPath(anvil, "hold-mission"),
		fmt.Sprintf(`{"missionId":%q,"reason":"debris on the lane"}`, missionConvoyID))
	assertStatus(t, status, http.StatusOK, body)
	status, body = doRequestAs(t, h, "quartermaster", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"amount":300}}]`, opPath(anvil, "sortie-expenses/"+expenseID)))
	assertStatus(t, status, http.StatusForbidden, body)

	// Resume: the binding flips back.
	status, body = doRequestAs(t, h, "lead", http.MethodPost, sectorPath(anvil, "resume-mission"),
		fmt.Sprintf(`{"missionId":%q}`, missionConvoyID))
	assertStatus(t, status, http.StatusOK, body)
	status, body = doRequestAs(t, h, "quartermaster", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"amount":300}}]`, opPath(anvil, "sortie-expenses/"+expenseID)))
	assertStatus(t, status, http.StatusOK, body)

	// The delete rides the same two-hop binding.
	status, body = doRequestAs(t, h, "quartermaster", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"remove","path":%q}]`, opPath(anvil, "sortie-expenses/"+expensePodTowGearID)))
	assertStatus(t, status, http.StatusForbidden, body) // the pod mission is completed
	status, body = doRequestAs(t, h, "quartermaster", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"remove","path":%q}]`, opPath(anvil, "sortie-expenses/"+expenseID)))
	assertStatus(t, status, http.StatusOK, body)
}
