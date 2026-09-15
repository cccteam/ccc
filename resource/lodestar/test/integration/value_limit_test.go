// Demonstrates: decode.value-limit.
package integration

// This suite pins what a value the column cannot hold answers: 400 naming the field when
// the request is decoded, before anything is buffered and before any permission check,
// from the sqltype tag the generator wrote onto the request struct. Ships.Registry is
// STRING(16), so a seventeen-character registry on a create names registry (the field is
// immutable, so the create is the only path); Missions.Fee is NUMERIC, so a fee with ten
// decimals names fee on a create and on a PATCH, while nine decimals are accepted on both.
// The commit translation of the constraint suite stays the backstop for what the decoder
// does not size.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestValueLimit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	h := newTestApp(db, grants{
		accesstypes.Create: append(
			withFields(missionsResource, "clientId", "kindId", "title", "hazard", "fee", "deadline"),
			withFields("Ships", "hangarId", "classId", "registry", "name")...),
		accesstypes.Update: withFields(missionsResource, "fee"),
	})

	ship := func(registry string) string {
		return fmt.Sprintf(`[{"op":"add","path":%q,"value":{"hangarId":%q,"classId":%q,"registry":%q,"name":"Long Registry"}}]`, opPath(anvil, "ships"), hangarAnvilDockID, shipClassKestrelID, registry)
	}
	mission := func(fee string) string {
		return fmt.Sprintf(`[{"op":"add","path":%q,"value":{"clientId":%q,"kindId":"courier","title":"Priced to the decimal","hazard":1,"fee":%s,"deadline":"2027-01-01T00:00:00Z"}}]`, opPath(anvil, "missions"), clientHalvardID, fee)
	}
	reprice := func(fee string) string {
		return fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"fee":%s}}]`, opPath(anvil, "missions/"+missionHaulerID), fee)
	}

	const feeLimit = "fee is limited to 29 digits before the decimal point and 9 after"

	tests := []struct {
		name        string
		ops         string
		wantStatus  int
		wantMessage string
	}{
		{
			name:       "a sixteen-character registry is created",
			ops:        ship("LS-REGISTRY-0016"),
			wantStatus: http.StatusOK,
		},
		{
			name:        "a seventeen-character registry answers 400 naming registry",
			ops:         ship("LS-REGISTRY-00017"),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "registry is limited to 16 characters",
		},
		{
			// Spanner counts code points, and so does the decoder: sixteen CJK characters
			// are forty-eight bytes and fit.
			name:       "sixteen multibyte characters fit a sixteen-character registry",
			ops:        ship("登録登録登録登録登録登録登録登録"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "a fee with nine decimals is created",
			ops:        mission("1200.123456789"),
			wantStatus: http.StatusOK,
		},
		{
			name:        "a fee with ten decimals answers 400 naming fee on a create",
			ops:         mission("1200.1234567891"),
			wantStatus:  http.StatusBadRequest,
			wantMessage: feeLimit,
		},
		{
			name:       "a fee with nine decimals is accepted on a PATCH",
			ops:        reprice("8000.000000001"),
			wantStatus: http.StatusOK,
		},
		{
			name:        "a fee with ten decimals answers 400 naming fee on a PATCH",
			ops:         reprice("8000.0000000001"),
			wantStatus:  http.StatusBadRequest,
			wantMessage: feeLimit,
		},
		{
			name:        "a fee with thirty integer digits answers 400 naming fee",
			ops:         reprice("100000000000000000000000000000"),
			wantStatus:  http.StatusBadRequest,
			wantMessage: feeLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequest(t, h, http.MethodPatch, "/api/resources", tt.ops)
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
