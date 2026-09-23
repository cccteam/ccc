// Demonstrates: bindings.dot, @attribute.join-path, @domain.join-path, @subjectSet.dotted-value.
package integration

// bindings_graph_test reads the committed bindings graph and asserts every join path
// the resources declare is drawn exactly once, hop by hop, in its kind's style, so a
// path cannot vanish from the review surface or draw twice silently. The generator's
// own test pins the file format; this one pins Lodestar's facts.

import (
	"os"
	"strings"
	"testing"
)

func TestBindingsGraphDrawsEveryPathOnce(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../pkg/resources/zz_gen_bindings.dot")
	if err != nil {
		t.Fatalf("reading the bindings graph: %v", err)
	}
	dot := string(raw)

	tests := []struct {
		name string
		line string
	}{
		// @attribute(via:) paths, solid.
		{name: "MissionDocument client lands on Missions.ClientId", line: `"MissionDocument" -> "Mission" [label="client: MissionId ⇒ ClientId"];`},
		{name: "Ship hangarZone lands on Hangars.Zone", line: `"Ship" -> "Hangar" [label="hangarZone: HangarId ⇒ Zone"];`},
		{name: "Ship shipRole lands on ShipClasses.RoleId", line: `"Ship" -> "ShipClass" [label="shipRole: ClassId ⇒ RoleId"];`},
		// The synthesized state paths: one hop for a direct member, two for the chain,
		// the shared hop into the root drawn once.
		{name: "RefitTask state lands on Refits.StatusId", line: `"RefitTask" -> "Refit" [label="state: Id ⇒ StatusId"];`},
		{name: "Sortie state lands on Missions.StatusId", line: `"Sortie" -> "Mission" [label="state: MissionId ⇒ StatusId"];`},
		{name: "SortieExpense state continues through Sorties", line: `"SortieExpense" -> "Sortie" [label="state: SortieId"];`},
		// @domain(via:) paths, dotted: the three-hop RefitTask path shares Refit's and
		// Ship's hops, each drawn once.
		{name: "RefitTask domain continues through Refits", line: `"RefitTask" -> "Refit" [style=dotted, label="domain: Id"];`},
		{name: "Refit domain continues through Ships", line: `"Refit" -> "Ship" [style=dotted, label="domain: ShipId"];`},
		{name: "Ship domain lands on Hangars.SectorId", line: `"Ship" -> "Hangar" [style=dotted, label="domain: HangarId ⇒ SectorId"];`},
		{name: "MissionDocument domain lands on Missions.SectorId", line: `"MissionDocument" -> "Mission" [style=dotted, label="domain: MissionId ⇒ SectorId"];`},
		{name: "Sortie domain lands on Missions.SectorId", line: `"Sortie" -> "Mission" [style=dotted, label="domain: MissionId ⇒ SectorId"];`},
		{name: "SortieExpense domain continues through Sorties", line: `"SortieExpense" -> "Sortie" [style=dotted, label="domain: SortieId"];`},
		{name: "Squadron domain lands on Wings.SectorId", line: `"Squadron" -> "Wing" [style=dotted, label="domain: WingId ⇒ SectorId"];`},
		{name: "SquadronMembership domain continues through Squadrons", line: `"SquadronMembership" -> "Squadron" [style=dotted, label="domain: SquadronId"];`},
		// The dotted subject value, dashed: from the requester through the anchor.
		{name: "subject enters SquadronMembership on UserId for wings", line: `"subject" -> "SquadronMembership" [style=dashed, label="wings: UserId"];`},
		{name: "wings lands on Squadrons.WingId", line: `"SquadronMembership" -> "Squadron" [style=dashed, label="wings: SquadronId ⇒ WingId"];`},
		// Nodes: a table only landed on is dashed; the requester is an ellipse.
		{name: "Hangar is landed on", line: `"Hangar" [style=dashed];`},
		{name: "Wing is landed on", line: `"Wing" [style=dashed];`},
		{name: "the requester node", line: `"subject" [shape=ellipse];`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := strings.Count(dot, tt.line); got != 1 {
				t.Errorf("bindings graph carries %q %d times, want exactly once:\n%s", tt.line, got, dot)
			}
		})
	}

	// Bare bindings are not drawn, so a resource declaring only bare ones is absent:
	// the bare subject anchors and the bare-attribute resources.
	for _, absent := range []string{`"ClientContact"`, `"Pilot"`, `"PilotCertification"`, `"Client"`, `"Consignment"`, `"DistressCall"`} {
		if strings.Contains(dot, absent) {
			t.Errorf("bindings graph draws %s, which declares no join path:\n%s", absent, dot)
		}
	}
}
