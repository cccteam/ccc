// Demonstrates: decode.nullable-slice.
package integration

// nullable_slice_test: Squadrons.Callsigns is a nullable ARRAY<STRING(16)> column typed
// by the plain []string, where NULL and [] say different things. The seed carries both
// answers and the absence of one: Hammer has filed its callsigns, Portcullis flies silent
// and says so with an empty array, and Tongs has not filed yet, so its row carries a
// null. The marshal files Tongs as silent with an empty array, clears the filing with a
// null, which the decoder accepts because the generated request struct carries
// nullable:"true" from the column, and the row reads back null; a null into
// Ships.CargoBays, whose column is NOT NULL and whose field carries no tag, is refused
// at decode as cannot be null, before anything is written.

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

// TestSquadronCallsigns_seededStates pins the three seeded answers on the wire: a filed
// list arrives as a JSON array of strings, a squadron flying silent as an empty array,
// and one that has not filed as a null under the key, never an empty array.
func TestSquadronCallsigns_seededStates(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	tests := []struct {
		name     string
		user     accesstypes.User
		sector   string
		squadron string
		// want is the JSON value under callsigns; nil pins a null.
		want any
	}{
		{name: "Hammer has filed two callsigns", user: "marshal", sector: anvil, squadron: squadronHammerID, want: []any{"Hammerfall", "Anvil Actual"}},
		{name: "Tongs has not filed yet and carries a null", user: "marshal", sector: anvil, squadron: squadronTongsID, want: nil},
		{name: "Portcullis flies silent and says so with an empty array", user: "governor", sector: bastion, squadron: squadronPortcullisID, want: []any{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, sectorPath(tt.sector, "squadrons/"+tt.squadron), "")
			assertStatus(t, status, http.StatusOK, body)
			got, ok := decodeRow(t, body)["callsigns"]
			if !ok {
				t.Fatalf("callsigns is absent from the row: %s", body)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("callsigns = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestSquadronCallsigns_nullClearsTheFiling pins the write path: the marshal files Tongs
// as flying silent with an empty array, the column holds an empty array and the row reads
// it back; a null then clears the filing, the column reads NULL and the row null; and a
// list filed after that is stored whole.
//
// Deliberately not a table: each step depends on the state the previous one left behind.
func TestSquadronCallsigns_nullClearsTheFiling(t *testing.T) {
	t.Parallel()

	ctx, db, h := demoWorld(t)

	patch := func(value string) (int, []byte) {
		return doRequestAs(t, h, "marshal", http.MethodPatch, "/api/resources",
			fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"callsigns":%s}}]`, opPath(anvil, "squadrons/"+squadronTongsID), value))
	}
	read := func() any {
		status, body := doRequestAs(t, h, "marshal", http.MethodGet, sectorPath(anvil, "squadrons/"+squadronTongsID), "")
		assertStatus(t, status, http.StatusOK, body)

		return decodeRow(t, body)["callsigns"]
	}

	status, body := patch(`[]`)
	assertStatus(t, status, http.StatusOK, body)
	if stored := readColumn[[]string](ctx, t, db, "Squadrons", spanner.Key{squadronTongsID}, "Callsigns"); stored == nil || len(stored) != 0 {
		t.Errorf("Callsigns column after [] = %#v, want an empty array", stored)
	}
	if got := read(); !reflect.DeepEqual(got, []any{}) {
		t.Errorf("callsigns after [] = %#v, want an empty array", got)
	}

	status, body = patch(`null`)
	assertStatus(t, status, http.StatusOK, body)
	if stored := readColumn[[]string](ctx, t, db, "Squadrons", spanner.Key{squadronTongsID}, "Callsigns"); stored != nil {
		t.Errorf("Callsigns column after null = %#v, want NULL", stored)
	}
	if got := read(); got != nil {
		t.Errorf("callsigns after null = %#v, want a null", got)
	}

	status, body = patch(`["Tongs Actual"]`)
	assertStatus(t, status, http.StatusOK, body)
	if stored := readColumn[[]string](ctx, t, db, "Squadrons", spanner.Key{squadronTongsID}, "Callsigns"); !reflect.DeepEqual(stored, []string{"Tongs Actual"}) {
		t.Errorf("Callsigns column after the filing = %#v, want [Tongs Actual]", stored)
	}
	if got := read(); !reflect.DeepEqual(got, []any{"Tongs Actual"}) {
		t.Errorf("callsigns after the filing = %#v, want [Tongs Actual]", got)
	}
}

// TestSquadronCallsigns_notNullSliceRefused pins the other column: a null into
// Ships.CargoBays, an ARRAY column declared NOT NULL whose request field carries no
// nullable tag, answers 400 naming the field at decode, before anything is written, as a
// null into any other field without a null form does.
func TestSquadronCallsigns_notNullSliceRefused(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	tests := []struct {
		name        string
		ops         string
		wantMessage string
	}{
		{
			name:        "a null into the NOT NULL array column",
			ops:         fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"cargoBays":null}}]`, opPath(anvil, "ships/"+shipKingfisherID)),
			wantMessage: "cargoBays cannot be null",
		},
		{
			name:        "a null into a NOT NULL string column, for comparison",
			ops:         fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"name":null}}]`, opPath(anvil, "squadrons/"+squadronHammerID)),
			wantMessage: "name cannot be null",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, "marshal", http.MethodPatch, "/api/resources", tt.ops)
			assertStatus(t, status, http.StatusBadRequest, body)
			if !strings.Contains(string(body), tt.wantMessage) {
				t.Errorf("body = %s, want it to carry %q", body, tt.wantMessage)
			}
		})
	}
}
