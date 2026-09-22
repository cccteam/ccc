package main

import (
	"regexp"
	"testing"
)

// uuidShape is the identifier form the application's UUID type accepts: version 4,
// variant 8, lowercase hex.
var uuidShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-8[0-9a-f]{3}-[0-9a-f]{12}$`)

// TestWorldSizing pins how many rows a scale derives per table, so a preset's cost is
// known before it runs, and that every identifier scheme yields a well-formed UUID the
// seed cannot collide with.
//
// Demonstrates: bootstrap.volume.
func TestWorldSizing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		scale         scale
		wantSectors   int // the synthetic ones plus Anvil
		wantSquadrons int // per sector
		wantShips     int // per sector
	}{
		{name: "medium", scale: scales["medium"], wantSectors: 21, wantSquadrons: 6, wantShips: 200},
		{name: "large", scale: scales["large"], wantSectors: 51, wantSquadrons: 6, wantShips: 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := &world{s: tt.scale}
			if got := len(w.sectorIDs()); got != tt.wantSectors {
				t.Errorf("sectorIDs() = %d sectors, want %d", got, tt.wantSectors)
			}
			if got := len(w.squadronIDs(1)); got != tt.wantSquadrons {
				t.Errorf("squadronIDs(1) = %d, want %d", got, tt.wantSquadrons)
			}
			if got := len(w.shipIDs(1)); got != tt.wantShips {
				t.Errorf("shipIDs(1) = %d, want %d", got, tt.wantShips)
			}
			if wings := w.wingIDs(0); wings[0] != forgeWingID || len(wings) != tt.scale.wingsPerSector {
				t.Errorf("wingIDs(0) = %v, want Forge Wing first among %d", wings, tt.scale.wingsPerSector)
			}
		})
	}
}

func TestIdentifierSchemes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
	}{
		{name: "wing", id: wingID(3, 1)},
		{name: "squadron", id: squadronID(3, 1, 2)},
		{name: "client", id: clientID(7)},
		{name: "hangar", id: hangarID(3, 2)},
		{name: "ship", id: shipID(3, 2, 99)},
		{name: "mission", id: missionID(3, 9999)},
		{name: "sortie", id: sortieID(3, 9995)},
		{name: "report", id: reportID(3, 199)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !uuidShape.MatchString(tt.id) {
				t.Errorf("%s id %q is not a version-4 UUID", tt.name, tt.id)
			}
			if tt.id[0] != 'e' {
				t.Errorf("%s id %q does not carry the synthetic prefix the seed never uses", tt.name, tt.id)
			}
		})
	}
}
