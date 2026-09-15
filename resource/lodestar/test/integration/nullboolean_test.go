// Demonstrates: nullboolean.
package integration

// nullboolean_test: Clients.Insured is the demo's one nullable BOOL column, its third
// state the story's "unknown until the underwriter answers". Headquarters walks the
// Meridian Survey Office's cover from the seed's NULL to true, to false, and back to NULL
// through the standalone Clients PATCH surface, reading the row back over the API and
// from the column after each step. The null in the body clearing the column is the write
// a two-state widget never sends and the console's three-way picker does; over the API
// the key is present and null, where a masked cell would be absent.

import (
	"fmt"
	"net/http"
	"testing"

	"cloud.google.com/go/spanner"
)

func TestClientInsured_nullBooleanWalk(t *testing.T) {
	t.Parallel()

	ctx, db, h := demoWorld(t)

	// The steps run in order, not as parallel subtests: each starts from the state the
	// previous one left, and the walk ends where the seed began.
	steps := []struct {
		name string
		// body is the PATCH value; empty reads the seed as it stands.
		body string
		want spanner.NullBool
	}{
		{name: "the seed leaves Meridian's cover unknown", want: spanner.NullBool{}},
		{name: "the underwriter confirms cover", body: `{"insured":true}`, want: spanner.NullBool{Bool: true, Valid: true}},
		{name: "the underwriter withdraws it", body: `{"insured":false}`, want: spanner.NullBool{Bool: false, Valid: true}},
		{name: "a null in the body clears the column", body: `{"insured":null}`, want: spanner.NullBool{}},
	}

	for _, step := range steps {
		if step.body != "" {
			status, body := doRequestAs(t, h, "governor", http.MethodPatch, "/api/clients",
				fmt.Sprintf(`[{"op":"patch","path":"/%s","value":%s}]`, clientMeridianID, step.body))
			if status != http.StatusOK {
				t.Fatalf("%s: PATCH status = %d, want %d: %s", step.name, status, http.StatusOK, body)
			}
		}

		status, body := doRequestAs(t, h, "governor", http.MethodGet, "/api/clients/"+clientMeridianID, "")
		if status != http.StatusOK {
			t.Fatalf("%s: GET status = %d, want %d: %s", step.name, status, http.StatusOK, body)
		}
		got, ok := decodeRow(t, body)["insured"]
		if !ok {
			t.Fatalf("%s: insured is absent from the row, as a masked cell would be: %s", step.name, body)
		}
		var want any
		if step.want.Valid {
			want = step.want.Bool
		}
		if got != want {
			t.Errorf("%s: insured over the API = %v, want %v", step.name, got, want)
		}

		if col := readColumn[spanner.NullBool](ctx, t, db, "Clients", spanner.Key{clientMeridianID}, "Insured"); col != step.want {
			t.Errorf("%s: Clients.Insured = %v, want %v", step.name, col, step.want)
		}
	}
}
