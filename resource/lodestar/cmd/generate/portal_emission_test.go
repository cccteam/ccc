package generate

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestPortalTargetEmission pins the second TypeScript target (design plan §5, §8): the
// portal client, emitted by the same generator run as the console's but filtered to the
// portal outlet, names the portal's four members and none of the console-only
// resources, and its descriptor bootstraps under the domain route the console shares.
// Manual registrations (@manualAddResource) are not outlet-aware and reach every
// target — recorded as a finding — so the pin lists them as present rather than
// pretending the filter drops them. Needs no emulator: it reads the committed output.
func TestPortalTargetEmission(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	moduleRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	read := func(rel string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(moduleRoot, rel))
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}

		return string(data)
	}

	portalConstants := read("web/portal/src/app/core/service/zz_gen_constants.ts")
	consoleConstants := read("web/console/src/app/core/service/zz_gen_constants.ts")
	portalAPI := read("web/portal/src/app/core/service/zz_gen_api.ts")

	tests := []struct {
		name   string
		source string
		want   []string
		absent []string
	}{
		{
			name:   "the portal's Resources constant carries its members and the manual registration",
			source: portalConstants,
			want:   []string{"ClientContacts: 'ClientContacts'", "DistressCalls: 'DistressCalls'", "Missions: 'Missions'", "ShipsLogEntries: 'ShipsLogEntries'"},
			absent: []string{"Refits:", "Ships:", "Squadrons:", "Pilots:", "Sorties:", "Consignments:", "SectorHazardBoards:", "Wings:"},
		},
		{
			name:   "the portal's Methods constant carries Stand Down and the manual Execute registrations only",
			source: portalConstants,
			want:   []string{"StandDownMission: 'StandDownMission'", "ViewAsUser: 'ViewAsUser'", "AssumeRole: 'AssumeRole'"},
			absent: []string{"ClaimMission:", "HailShip:", "ScrapShip:", "IngestDroidReports:"},
		},
		{
			name:   "the portal descriptor bootstraps under the shared domain route with its own permission channels",
			source: portalAPI,
			want:   []string{"domainRoute: { segment: 'sectors', param: 'sectorID' }", "permissionDigestRoute: 'permission-digest'", "userDomainsRoute: 'user-domains'", "consolidatedRoute: 'resources'"},
			absent: []string{"Refits", "Squadrons", "Pilots"},
		},
		{
			name:   "the console target still names everything",
			source: consoleConstants,
			want:   []string{"Refits: 'Refits'", "Ships: 'Ships'", "Squadrons: 'Squadrons'", "Pilots: 'Pilots'", "Missions: 'Missions'"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, want := range tt.want {
				if !strings.Contains(tt.source, want) {
					t.Errorf("missing %q", want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(tt.source, absent) {
					t.Errorf("carries %q, want absent", absent)
				}
			}
		})
	}

	// The portal emits exactly four resource interfaces (the members) — the manual
	// registration has no struct, so no interface — and the console emits every one.
	iface := regexp.MustCompile(`(?m)^export interface (\w+) \{`)
	portalIfaces := iface.FindAllStringSubmatch(read("web/portal/src/app/core/service/zz_gen_resources.ts"), -1)
	var names []string
	for _, m := range portalIfaces {
		// The per-resource Operation shapes and the Workflow types are emitted beside
		// the row interfaces; only the row interfaces name members.
		if strings.HasSuffix(m[1], "Operation") || strings.HasPrefix(m[1], "Workflow") {
			continue
		}
		names = append(names, m[1])
	}
	if strings.Join(names, ",") != "ClientContacts,DistressCalls,Missions" {
		t.Errorf("portal resource interfaces = %v, want ClientContacts, DistressCalls, Missions", names)
	}
}
