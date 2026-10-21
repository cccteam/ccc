package integration

// This suite pins the reach-time admission on the served surface: a sector is in a
// session's star chart only when the session holds a grant there and the sector's
// declaration admits the way the session was established, and a direct request for a
// sector the session may not enter answers as a sector that does not exist, the
// concealed-sector guard's own refusal. The harness scripts the declarations, since no
// shipped sector declares anything yet: Bastion admits its people through a directory
// only, Cinder declares a directory beside passwords, Anvil declares nothing. The
// harness's sessions record no sign-in, which no directory admits and a sector that is
// not directory only does.
//
// The deckhand is a crew member this suite provisions for itself, SectorCrew in Anvil and
// Bastion and nothing in the global partition: every shipped persona holds CrewCommon
// there, which the staff rule counts (a grant in the global partition reaches every
// sector, as the sign-in policy admits the application's own staff), so the shipped
// cast cannot show a refusal. The deckhand charts Anvil alone and Bastion is not there
// for him; the pilot, CrewCommon in hand, keeps Bastion; the governor, headquarters'
// staff, charts every sector and enters Bastion.

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/session/sessioninfo"
	"github.com/cccteam/session/signin"
)

// deckhand is the suite's own crew member: held in two sectors, with no global role.
const deckhand accesstypes.User = "deckhand"

func TestTenantAdmission_sectorDeclarationsDecideTheChart(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	crewEngine := newAccessClient(t, db, crew.Roles())
	membersEngine, err := openEngine(db, members.TablePrefix, members.Roles())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := membersEngine.Close(); err != nil {
			t.Errorf("access.Client.Close() error = %v", err)
		}
	})
	if err := provisionDemoAccess(ctx, crewEngine, membersEngine); err != nil {
		t.Fatal(err)
	}
	if err := assignDemoRoles(ctx, crewEngine, deckhand, demoRoles{Domains: map[accesstypes.Domain][]accesstypes.Role{anvil: {"SectorCrew"}, bastion: {"SectorCrew"}}}); err != nil {
		t.Fatal(err)
	}
	if err := waitForDecision(ctx, crewEngine, deckhand, bastion, accesstypes.List, "Wings"); err != nil {
		t.Fatal(err)
	}

	workspace := signin.Directory{Method: sessioninfo.MethodGoogle, Connection: "frontier-workspace"}
	declarations := map[accesstypes.Domain]*signin.TenantDeclaration{
		bastion: {Directories: []signin.Directory{workspace}, DirectoryOnly: true},
		cinder:  {Directories: []signin.Directory{workspace}},
	}
	h := withHandWrittenRoutes(newAppWithDeclarations(db, crewEngine, membersEngine, func(_ context.Context, sector accesstypes.Domain) (*signin.TenantDeclaration, error) {
		return declarations[sector], nil
	}))
	bastionDigest := consoleAPI + "/permission-digest?domain=" + bastion
	ghostWing := `[{"op":"add","path":"` + opPath(bastion, "wings") + `","value":{"name":"Ghost Wing"}}]`

	tests := []struct {
		name       string
		user       accesstypes.User
		method     string
		target     string
		body       string
		wantStatus int
		wantChart  []accesstypes.Domain
	}{
		{name: "the deckhand's chart lights Anvil alone: Bastion admits its people through its directory only", user: deckhand, method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, wantChart: []accesstypes.Domain{anvil}},
		{name: "Anvil's wings answer the deckhand", user: deckhand, method: http.MethodGet, target: sectorPath(anvil, "wings"), wantStatus: http.StatusOK},
		{name: "Bastion is not there for the deckhand: the guard refuses a sector the session may not enter as one that does not exist", user: deckhand, method: http.MethodGet, target: sectorPath(bastion, "wings"), wantStatus: http.StatusNotFound},
		{name: "nor through the consolidated patch, whose descent answers as it does an unknown sector", user: deckhand, method: http.MethodPatch, target: resourcesRoute, body: ghostWing, wantStatus: http.StatusBadRequest},
		{name: "Bastion's digest is empty for the deckhand", user: deckhand, method: http.MethodGet, target: bastionDigest, wantStatus: http.StatusOK},
		{name: "the hazard analyst's chart lights Anvil and Cinder: Cinder admits passwords beside its directory", user: "hazards", method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, wantChart: []accesstypes.Domain{anvil, cinder}},
		{name: "the pilot holds CrewCommon in the global partition, which the staff rule counts, so Bastion stays lit for him", user: "pilot", method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, wantChart: []accesstypes.Domain{anvil, bastion}},
		{name: "the governor, headquarters' staff, charts every sector", user: "governor", method: http.MethodGet, target: userDomainsRoute, wantStatus: http.StatusOK, wantChart: []accesstypes.Domain{anvil, bastion, cinder}},
		{name: "and enters Bastion", user: "governor", method: http.MethodGet, target: sectorPath(bastion, "wings"), wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, tt.method, tt.target, tt.body)
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantChart != nil {
				var got []accesstypes.Domain
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatalf("decoding the chart %s: %v", body, err)
				}
				if !slices.Equal(got, tt.wantChart) {
					t.Errorf("chart = %v, want %v", got, tt.wantChart)
				}
			}
			if tt.target == bastionDigest {
				var digest accesstypes.PermissionDigest
				if err := json.Unmarshal(body, &digest); err != nil {
					t.Fatalf("decoding the digest %s: %v", body, err)
				}
				if len(digest) != 0 {
					t.Errorf("Bastion's digest for the deckhand = %v, want empty", digest)
				}
			}
		})
	}
}
