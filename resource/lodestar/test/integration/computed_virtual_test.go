package integration

// This suite pins the read-only resource shapes over the demo world: computed
// resources (hand-written query logic behind generated routes, single and compound
// keys) and virtual resources (embedded subqueries, including a WITH clause with a
// named parameter, and the sector-scoped variant).

import (
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestComputedResources(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	h := newTestApp(db, grants{
		accesstypes.List: append(withFields("ServiceLedgers", "name", "openMissions", "feesOutstanding", "settlements"), withFields("SectorHazardBoards", "shipName", "worstReading", "recordedAt")...),
		accesstypes.Read: withFields("SectorHazardBoards", "shipName", "worstReading"),
	})

	tests := []struct {
		name     string
		target   string
		wantRows int
		check    func(t *testing.T, respBody []byte)
	}{
		{
			// Anvil has five missions still moving (open ×2, claimed, underway, on_hold)
			// worth 8000+24000+15000+3000+6000, and one settlement of 38500.
			name:   "the service ledger aggregates the seeded world",
			target: "/api/service-ledgers",
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				row := rowsByID(t, decodeRows(t, respBody), "sectorId")[anvil]
				if row == nil {
					t.Fatalf("no anvil ledger: %s", respBody)
				}
				if got := row["openMissions"]; got != float64(5) {
					t.Errorf("anvil openMissions = %v, want 5", got)
				}
				if got := row["feesOutstanding"]; got != "56000" {
					t.Errorf("anvil feesOutstanding = %v, want 56000", got)
				}
				if got := row["settlements"]; got != "38500" {
					t.Errorf("anvil settlements = %v, want 38500", got)
				}
			},
		},
		{
			name:     "the hazard board folds readings to the worst per ship and subsystem in the sector",
			target:   sectorPath(anvil, "sector-hazard-boards"),
			wantRows: 3,
		},
		{
			name:   "the compound-key read route addresses one ship and subsystem pair",
			target: sectorPath(anvil, "sector-hazard-boards/"+shipKingfisherID+"/hull"),
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				if got := decodeRow(t, respBody)["worstReading"]; got != float64(0.61) {
					t.Errorf("worst hull reading = %v, want 0.61", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequest(t, h, http.MethodGet, tt.target, "")
			assertStatus(t, status, http.StatusOK, body)
			if tt.wantRows > 0 {
				if rows := decodeRows(t, body); len(rows) != tt.wantRows {
					t.Errorf("rows = %d, want %d: %s", len(rows), tt.wantRows, body)
				}
			}
			if tt.check != nil {
				tt.check(t, body)
			}
		})
	}
}

func TestVirtualResources(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	h := newTestApp(db, grants{accesstypes.List: append(
		withFields("FeeByKinds", "missionCount", "totalFee", "topFee"),
		withFields("OpenMissionsBySquadrons", "squadronName", "openMissions", "nextDeadline")...,
	)})

	tests := []struct {
		name     string
		target   string
		wantRows int
		check    func(t *testing.T, respBody []byte)
	}{
		{
			// Four kinds; salvage (the Corvid, the pod, the tow, the sweep) sums 126000
			// once the stood-down bullion escort is excluded by the WITH clause.
			name:     "the WITH-clause subquery folds fees by kind",
			target:   "/api/fee-by-kinds",
			wantRows: 4,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				salvage := rowsByID(t, decodeRows(t, respBody), "kindId")["salvage"]
				if salvage == nil {
					t.Fatalf("no salvage row: %s", respBody)
				}
				if got := salvage["missionCount"]; got != float64(4) {
					t.Errorf("salvage missionCount = %v, want 4", got)
				}
				if got := salvage["totalFee"]; got != "126000" {
					t.Errorf("salvage totalFee = %v, want 126000", got)
				}
			},
		},
		{
			// Hammer (Corvid, convoy) and Tongs (courier) at Anvil; Portcullis at Bastion
			// stays out of the Anvil partition.
			name:     "the sector-scoped virtual serves under the sector segment",
			target:   sectorPath(anvil, "open-missions-by-squadrons"),
			wantRows: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequest(t, h, http.MethodGet, tt.target, "")
			assertStatus(t, status, http.StatusOK, body)
			if tt.wantRows > 0 {
				if rows := decodeRows(t, body); len(rows) != tt.wantRows {
					t.Errorf("rows = %d, want %d: %s", len(rows), tt.wantRows, body)
				}
			}
			if tt.check != nil {
				tt.check(t, body)
			}
		})
	}
}
