// Demonstrates: commit.referential-refusal, commit.constraint-refusal.
package integration

// This suite pins what a commit Spanner refuses answers: a 4xx with a message the resource
// library composes from the resources the transaction buffered, decided on the gRPC code
// alone. Lodestar's foreign keys are NO ACTION apart from three cascades, so any referenced
// parent reproduces the referential refusal (409), and the combined deletes prove that
// detecting at commit keeps a child-and-parent delete legal in either order. A refit task
// created under a task number the refit already holds (a client-supplied compound key) and
// a ship created under a seeded registry (ShipsByRegistry) are the duplicate-key and
// duplicate-unique-value refusals on a create (409); a client renamed to another client's
// name is the duplicate unique value on an update (409); a client updated under an id that
// does not exist is the missing row on an update (404), which reaches the commit because
// Client is global, untracked, and updated under unconditional grants; and a mission's
// hazard updated to 9 is the CHECK refusal (400), beside the create validator's own 400 for
// the same value, the two enforcement points of one rule.

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
			append(
				withFields(missionsResource, "clientId", "kindId", "title", "hazard", "fee", "deadline"),
				withFields("RefitTasks", "instructions", "done")...),
			withFields("Ships", "hangarId", "classId", "registry", "name")...),
		accesstypes.Update: append(
			withFields(missionsResource, "hazard"),
			withFields("Clients", "name")...),
	})

	const (
		missingClientID = "10000000-0000-4000-8000-000000000099"
		missingID       = "10000000-0000-4000-8000-000000000098"
	)
	mission := func(clientID, title string, hazard int) string {
		return fmt.Sprintf(`{"op":"add","path":%q,"value":{"clientId":%q,"kindId":"courier","title":%q,"hazard":%d,"fee":100,"deadline":%q}}`, opPath(anvil, "missions"), clientID, title, hazard, deadline(365))
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
			ops:         "[" + mission(missingClientID, "Booked for nobody", 1) + "]",
			wantStatus:  http.StatusConflict,
			wantMessage: "Missions: a referenced record does not exist, or a value is too long for its field.",
		},
		{
			name:        "a batch mixing a write and a delete answers the sentence covering both causes",
			target:      "/api/resources",
			ops:         "[" + mission(missingClientID, "Booked beside a refused delete", 1) + "," + remove("sorties/"+sortiePodID) + "]",
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
			// The Good Samaritan's refit already has task 2: the client-supplied compound
			// key is taken, and the commit refuses it with AlreadyExists.
			name:        "a create under a task number the refit already holds answers 409 naming the key",
			target:      "/api/resources",
			ops:         fmt.Sprintf(`[{"op":"add","path":%q,"value":{"instructions":"Recertify hull seals again","done":false}}]`, opPath(anvil, "refit-tasks/"+refitSamaritanID+"/2")),
			wantStatus:  http.StatusConflict,
			wantMessage: "RefitTasks: a record with this key or a unique value already exists.",
		},
		{
			// LS-101 is the Kingfisher's registry; Ship is tracked, so the change event is
			// buffered beside the create and must not be named.
			name:        "a ship created under a seeded registry answers 409 with the create sentence",
			target:      "/api/resources",
			ops:         fmt.Sprintf(`[{"op":"add","path":%q,"value":{"hangarId":%q,"classId":%q,"registry":"LS-101","name":"Kingfisher's Shadow"}}]`, opPath(anvil, "ships"), hangarAnvilDockID, shipClassKestrelID),
			wantStatus:  http.StatusConflict,
			wantMessage: "Ships: a record with this key or a unique value already exists.",
		},
		{
			// Client is excluded from consolidation and global: its standalone PATCH
			// surface has no sector. Halvard Freight is a seeded client's name.
			name:        "a client renamed to another client's name answers 409 with the update sentence",
			target:      "/api/clients",
			ops:         fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"name":"Halvard Freight"}}]`, clientMeridianID),
			wantStatus:  http.StatusConflict,
			wantMessage: "Clients: a unique value already exists on another record.",
		},
		{
			name:        "a client updated under an id that does not exist answers 404",
			target:      "/api/clients",
			ops:         fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"name":"Nobody's Freight"}}]`, missingID),
			wantStatus:  http.StatusNotFound,
			wantMessage: "Clients: this record does not exist.",
		},
		{
			// The update path has no validator: the schema's CK_Missions_Hazard refuses
			// hazard 9 at commit, and the library answers it in the resource's name.
			name:        "a mission's hazard updated to 9 answers 400 from the CHECK constraint",
			target:      "/api/resources",
			ops:         fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"hazard":9}}]`, opPath(anvil, "missions/"+missionHaulerID)),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "Missions: a value is outside the range the record allows.",
		},
		{
			// The same rule on a create is the validator's, answered naming the field
			// before anything is buffered.
			name:        "a mission created with hazard 9 answers 400 from the create validator",
			target:      "/api/resources",
			ops:         "[" + mission(clientHalvardID, "Too hazardous to book", 9) + "]",
			wantStatus:  http.StatusBadRequest,
			wantMessage: "hazard must be between 1 and 5",
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
