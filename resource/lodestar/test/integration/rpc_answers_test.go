package integration

// This suite covers a method that chooses its status per response: CompleteMission
// declares @answers(200, 409) and answers with a Settlement. On 200 the figures come
// back and the mission completes; on 409 — the booked expenses exceed the fee — the
// same figures come back as the refusal's body, and nothing the body armed commits:
// the mission stays underway, its sorties stay out, its settlement stays unwritten. A
// dry run reports the 409 the same way.

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/shopspring/decimal"
)

// settlementBody is CompleteMission's answer as the wire carries it.
type settlementBody struct {
	Fee      decimal.Decimal `json:"fee"`
	Expenses decimal.Decimal `json:"expenses"`
	Net      decimal.Decimal `json:"net"`
	Sorties  []struct {
		SortieID string          `json:"sortieId"`
		Expenses decimal.Decimal `json:"expenses"`
	} `json:"sorties"`
}

func TestCompleteMission_answers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// overrun is an extra expense booked against the convoy's open sortie before
		// the call; zero books nothing.
		overrun      int64
		dryRun       bool
		wantStatus   int
		wantNet      string
		wantState    string
		wantReturned bool
		wantSettled  bool
	}{
		{name: "the fee covers the expenses: 200 with the figures, and the mission completes", wantStatus: http.StatusOK, wantNet: "13500", wantState: "completed", wantReturned: true, wantSettled: true},
		{name: "the expenses exceed the fee: 409 with the same figures, and nothing commits", overrun: 20000, wantStatus: http.StatusConflict, wantNet: "-6500", wantState: "underway"},
		{name: "a dry run reports the 409 as the real call would", overrun: 20000, dryRun: true, wantStatus: http.StatusConflict, wantNet: "-6500", wantState: "underway"},
		{name: "a dry run of a completion that would commit answers 200 with no body", dryRun: true, wantStatus: http.StatusOK, wantState: "underway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, db, h := demoWorld(t)
			if tt.overrun != 0 {
				if _, err := db.Apply(ctx, []*spanner.Mutation{spanner.InsertMap("SortieExpenses", map[string]any{
					"Id":       "91000000-0000-4000-8000-0000000000ff",
					"SortieId": sortieConvoyID,
					"Category": "fuel",
					"Amount":   spanner.NullNumeric{Numeric: *big.NewRat(tt.overrun, 1), Valid: true},
					"Note":     "Emergency refuel at the relay",
				})}); err != nil {
					t.Fatalf("spanner.Client.Apply() error = %v", err)
				}
			}

			target, reqBody := sectorPath(anvil, "complete-mission"), fmt.Sprintf(`{"missionId":%q}`, missionConvoyID)
			var status int
			var body []byte
			if tt.dryRun {
				status, body = doDryRunAs(t, h, "marshal", target, reqBody)
			} else {
				status, body = doRequestAs(t, h, "marshal", http.MethodPost, target, reqBody)
			}
			assertStatus(t, status, tt.wantStatus, body)

			if tt.wantNet != "" {
				var got settlementBody
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatalf("json.Unmarshal(%s) error = %v", body, err)
				}
				if got.Net.String() != tt.wantNet || !got.Fee.Sub(got.Expenses).Equal(got.Net) {
					t.Errorf("settlement = %+v, want net %s = fee - expenses", got, tt.wantNet)
				}
				if len(got.Sorties) != 1 || got.Sorties[0].SortieID != sortieConvoyID {
					t.Errorf("sorties = %+v, want the convoy's one sortie", got.Sorties)
				}
			} else if len(body) != 0 {
				t.Errorf("body = %s, want none", body)
			}

			if got := readColumn[string](ctx, t, db, "Missions", spanner.Key{missionConvoyID}, "StatusId"); got != tt.wantState {
				t.Errorf("StatusId = %q, want %q", got, tt.wantState)
			}
			returned := readColumn[spanner.NullTime](ctx, t, db, "Sorties", spanner.Key{sortieConvoyID}, "ReturnedAt")
			if returned.Valid != tt.wantReturned {
				t.Errorf("sortie ReturnedAt valid = %v, want %v", returned.Valid, tt.wantReturned)
			}
			settled := readColumn[spanner.NullNumeric](ctx, t, db, "Missions", spanner.Key{missionConvoyID}, "Settlement")
			if settled.Valid != tt.wantSettled {
				t.Errorf("Settlement valid = %v, want %v", settled.Valid, tt.wantSettled)
			}
		})
	}
}
