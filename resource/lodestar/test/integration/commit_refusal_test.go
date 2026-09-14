// Demonstrates: commit.referential-refusal.
package integration

// This suite pins what a commit Spanner refuses for a referential reason answers: 409 with
// a message the resource library composes from the resources the transaction buffered,
// decided on the gRPC code alone. Lodestar's foreign keys are NO ACTION apart from three
// cascades, so any referenced parent reproduces the refusal. The combined deletes prove
// that detecting at commit keeps a child-and-parent delete legal in either order, and the
// duplicate key pins that a commit refused with any other code still passes through.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestCommitRefusal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	h := newTestApp(db, grants{
		accesstypes.Delete: {"Hangars", "Ships", "Sorties", "SortieExpenses"},
		accesstypes.Create: append(
			withFields(missionsResource, "clientId", "kindId", "title", "hazard", "fee", "deadline"),
			withFields("RefitTasks", "instructions", "done")...),
	})

	const missingClientID = "10000000-0000-4000-8000-000000000099"
	mission := func(title string) string {
		return fmt.Sprintf(`{"op":"add","path":%q,"value":{"clientId":%q,"kindId":"courier","title":%q,"hazard":1,"fee":100,"deadline":"2027-01-01T00:00:00Z"}}`, opPath(anvil, "missions"), missingClientID, title)
	}
	remove := func(rest string) string {
		return fmt.Sprintf(`{"op":"remove","path":%q}`, opPath(anvil, rest))
	}

	tests := []struct {
		name        string
		target      string
		ops         string
		wantStatus  int
		wantMessage string
	}{
		{
			// Hangar is excluded from consolidation, so the refusal rides its standalone
			// PATCH surface; the Quarantine Bay still holds two ships.
			name:        "a delete of a hangar that still holds ships answers 409 naming Hangars",
			target:      sectorPath(anvil, "hangars"),
			ops:         fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, hangarQuarantineID),
			wantStatus:  http.StatusConflict,
			wantMessage: "Hangars: this record cannot be deleted while other records still reference it.",
		},
		{
			// Ship is change-tracked: the delete buffers a DataChangeEvents row beside
			// it, which the message must not name.
			name:        "a delete of a tracked ship under refit names the ship alone",
			target:      "/api/resources",
			ops:         "[" + remove("ships/"+shipStubbornMuleID) + "]",
			wantStatus:  http.StatusConflict,
			wantMessage: "Ships: this record cannot be deleted while other records still reference it.",
		},
		{
			name:        "a delete of a sortie with expenses answers 409 naming Sorties",
			target:      "/api/resources",
			ops:         "[" + remove("sorties/"+sortiePodID) + "]",
			wantStatus:  http.StatusConflict,
			wantMessage: "Sorties: this record cannot be deleted while other records still reference it.",
		},
		{
			name:        "refused deletes on two resources list both in buffering order",
			target:      "/api/resources",
			ops:         "[" + remove("sorties/"+sortiePodID) + "," + remove("ships/"+shipLanternID) + "]",
			wantStatus:  http.StatusConflict,
			wantMessage: "Sorties, Ships: a record cannot be deleted while other records still reference it.",
		},
		{
			// Mission is change-tracked too; its client is not its tenancy hop, so the
			// missing row is found at commit, not by the tenancy check.
			name:        "a create naming a client that does not exist answers 409 with the write sentence",
			target:      "/api/resources",
			ops:         "[" + mission("Booked for nobody") + "]",
			wantStatus:  http.StatusConflict,
			wantMessage: "Missions: a referenced record does not exist.",
		},
		{
			name:        "a batch mixing a write and a delete answers the sentence covering both causes",
			target:      "/api/resources",
			ops:         "[" + mission("Booked beside a refused delete") + "," + remove("sorties/"+sortiePodID) + "]",
			wantStatus:  http.StatusConflict,
			wantMessage: "The request could not be applied: a deleted record is still referenced, or a referenced record does not exist.",
		},
		{
			name:       "children then their parent in one transaction succeed",
			target:     "/api/resources",
			ops:        "[" + remove("sortie-expenses/"+expenseConvoyFuelID) + "," + remove("sortie-expenses/"+expenseConvoyMedID) + "," + remove("sorties/"+sortieConvoyID) + "]",
			wantStatus: http.StatusOK,
		},
		{
			name:       "a parent then its child in one transaction succeed",
			target:     "/api/resources",
			ops:        "[" + remove("sorties/"+sortieCourierID) + "," + remove("sortie-expenses/"+expenseCourierFuelID) + "]",
			wantStatus: http.StatusOK,
		},
		{
			// The Good Samaritan's refit already has task 2; a duplicate key is refused
			// with AlreadyExists, which is not a referential refusal.
			name:       "a duplicate key at commit is not a referential refusal and still answers 500",
			target:     "/api/resources",
			ops:        fmt.Sprintf(`[{"op":"add","path":%q,"value":{"instructions":"Recertify hull seals again","done":false}}]`, opPath(anvil, "refit-tasks/"+refitSamaritanID+"/2")),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequest(t, h, http.MethodPatch, tt.target, tt.ops)
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantMessage == "" {
				return
			}

			var resp struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(body, &resp); err != nil {
				t.Fatalf("json.Unmarshal(%s): %v", body, err)
			}
			if resp.Message != tt.wantMessage {
				t.Errorf("message = %q, want %q", resp.Message, tt.wantMessage)
			}
		})
	}
}
