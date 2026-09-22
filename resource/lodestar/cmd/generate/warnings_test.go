package generate

import (
	"slices"
	"strings"
	"testing"
)

// TestSchemaWarnings pins the schema warnings the generator raises over this
// application: exactly one index warning, DroidReports, the registered demonstration
// that keeps no tenant-leading index while every other bare-tenant resource listing in
// a declared order carries one (migration 000032), and one join-path warning per
// resource that resolves its sector through foreign keys. The set is exact: a new
// bare-tenant resource without its index, or a new join path, shows up here. Runs the
// generator in-process, so it regenerates the tree like go generate does. Requires the
// Spanner emulator (podman/docker), like the rest of this module's tests.
//
// Demonstrates: warning.tenant-index, warning.join-path-list.
func TestSchemaWarnings(t *testing.T) {
	if testing.Short() {
		t.Skip("generation requires the Spanner emulator")
	}

	generator, err := NewGenerator(t.Context())
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}
	defer generator.Close()

	if err := generator.Generate(); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	var got []string
	for _, warning := range generator.Warnings() {
		got = append(got, warning.String())
	}

	want := []string{
		"DroidReport lists in SectorId, RecordedAt DESC order with no index leading with those columns, so every page sorts the tenant's partition; wanted: CREATE INDEX DroidReportsBySectorIdRecordedAt ON DroidReports(SectorId, RecordedAt DESC)",
		"MissionDocument resolves its tenant through MissionId, Missions.SectorId, so its lists scan all of MissionDocuments" + joinPathTail("MissionDocuments"),
		"Refit resolves its tenant through ShipId, Ships.HangarId, Hangars.SectorId, so its lists scan all of Refits" + joinPathTail("Refits"),
		"RefitTask resolves its tenant through Id, Refits.ShipId, Ships.HangarId, Hangars.SectorId, so its lists scan all of RefitTasks" + joinPathTail("RefitTasks"),
		"Ship resolves its tenant through HangarId, Hangars.SectorId, so its lists scan all of Ships" + joinPathTail("Ships"),
		"Sortie resolves its tenant through MissionId, Missions.SectorId, so its lists scan all of Sorties" + joinPathTail("Sorties"),
		"SortieExpense resolves its tenant through SortieId, Sorties.MissionId, Missions.SectorId, so its lists scan all of SortieExpenses" + joinPathTail("SortieExpenses"),
		"Squadron resolves its tenant through WingId, Wings.SectorId, so its lists scan all of Squadrons" + joinPathTail("Squadrons"),
		"SquadronMembership resolves its tenant through SquadronId, Squadrons.WingId, Wings.SectorId, so its lists scan all of SquadronMemberships" + joinPathTail("SquadronMemberships"),
	}
	if !slices.Equal(want, got) {
		t.Errorf("Warnings() mismatch\nwant:\n%s\ngot:\n%s", strings.Join(want, "\n"), strings.Join(got, "\n"))
	}
}

// joinPathTail is the fixed second half of a join-path warning for the named table.
func joinPathTail(table string) string {
	return ", every tenant, and no index on " + table + " changes that; a table listed at volume carries the tenant key on the row"
}
