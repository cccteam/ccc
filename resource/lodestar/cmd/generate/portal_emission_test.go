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
// portal outlet, names the portal's members and none of the console-only resources, and
// its descriptor bootstraps under the domain route the console shares. The manual
// registrations without an @outlet stay on the default outlet and the portal filter drops
// them; ClientStatements names @outlet(portal) and appears in the portal's constants
// alone. Both targets carry the two application-typed columns the same way: the
// resources file imports Point from geojson for DistressCalls.Position and declares
// MissionDocuments.Provenance in the resource's namespace. Needs no emulator: it reads
// the committed output.
//
// Demonstrates: typescript.second-target, @manualAddResource.outlet, workflow.ts-constant, outlet.isolation, typescript.imported-type, typescript.derived-object.
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
	portalResources := read("web/portal/src/app/core/service/zz_gen_resources.ts")
	consoleResources := read("web/console/src/app/core/service/zz_gen_resources.ts")
	consoleAPI := read("web/console/src/app/core/service/zz_gen_api.ts")

	// The two column types, as both targets render them.
	columnTypes := []string{
		"import { Point } from 'geojson';",
		"  position?: Point;",
		"{ fieldName: 'position', displayType: 'object', required: false, isIndex: false }",
		"  provenance?: MissionDocuments.Provenance;",
		"export namespace MissionDocuments {\n  export interface Provenance {\n    system: string;\n    reference?: string;\n    receivedAt: Date;\n  }\n}",
		"{ fieldName: 'provenance', displayType: 'object', required: false, isIndex: false }",
	}

	tests := []struct {
		name   string
		source string
		want   []string
		absent []string
	}{
		{
			name:   "the portal's Resources constant carries its members only",
			source: portalConstants,
			want:   []string{"ClientContacts: 'ClientContacts'", "DistressCalls: 'DistressCalls'", "Missions: 'Missions'", "ClientStatements: 'ClientStatements'"},
			absent: []string{"Refits:", "Ships:", "Squadrons:", "Pilots:", "Sorties:", "Consignments:", "SectorHazardBoards:", "Wings:", "ShipsLogEntries:"},
		},
		{
			name:   "the portal's Methods constant carries Stand Down only; the default-outlet manual registrations drop",
			source: portalConstants,
			want:   []string{"StandDownMission: 'StandDownMission'"},
			absent: []string{"ClaimMission:", "HailShip:", "ScrapShip:", "IngestDroidReports:", "ViewAsUser:", "AssumeRole:"},
		},
		{
			name:   "the portal descriptor bootstraps under the shared domain route with its own permission channels",
			source: portalAPI,
			want:   []string{"domainRoute: { segment: 'sectors', param: 'sectorID' }", "permissionDigestRoute: 'permission-digest'", "userDomainsRoute: 'user-domains'", "consolidatedRoute: 'resources'"},
			absent: []string{"Refits", "Squadrons", "Pilots"},
		},
		{
			name:   "the console target still names everything, and not the portal-only statement",
			source: consoleConstants,
			want:   []string{"Refits: 'Refits'", "Ships: 'Ships'", "Squadrons: 'Squadrons'", "Pilots: 'Pilots'", "Missions: 'Missions'", "ShipsLogEntries: 'ShipsLogEntries'"},
			absent: []string{"ClientStatements:"},
		},
		{
			name:   "the console resources file imports the declared type and derives the struct",
			source: consoleResources,
			want:   columnTypes,
			absent: []string{"CustomTypes"},
		},
		{
			name:   "the portal resources file carries the same two column types",
			source: portalResources,
			want:   columnTypes,
			absent: []string{"CustomTypes"},
		},
		{
			name:   "the console client imports Point for the create and patch shapes, and types provenance through the row type",
			source: consoleAPI,
			want:   []string{"import { Point } from 'geojson';", "  position?: Point;"},
			absent: []string{"CustomTypes"},
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

	// The portal emits exactly its members' resource interfaces (the members with a
	// struct, and with them the resources Mission's declared pickers name, which follow
	// Mission onto every outlet it serves) and the console emits every one.
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
	want := "ClientContacts,ClientRosters,DistressCalls,Missions,MissionDocuments,BriefingTemplates"
	if strings.Join(names, ",") != want {
		t.Errorf("portal resource interfaces = %v, want %s", names, want)
	}
}
