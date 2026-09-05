package integration

// temporal_test (design plan §9): the shipped shift personas pin fixed views only by
// pairing — at any hour exactly one of Dara and Nadia sees the hangar deck — so the
// suite also provisions shift roles through the real engine with windows computed
// around its own instant: the on-shift role's Refits list answers and the off-shift
// role's refuses at any hour; a wrap-around window holds across midnight; dayOfWeek
// refuses on a computed weekend and admits on a computed weekday; the digest reads
// conditional for every windowed grant; a local condition in an app that wires no
// zone fails its first check loudly.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	initiator "github.com/cccteam/db-initiator"
)

// timeOfDayWindow renders a two-sided timeOfDay window covering
// [center-halfWidth, center+halfWidth) in the given wall clock, in the OR form when
// it crosses midnight — so the expression is correct at any hour the suite runs.
func timeOfDayWindow(zoneArg string, center time.Time, halfWidth time.Duration) string {
	start := center.Add(-halfWidth).Format("15:04")
	end := center.Add(halfWidth).Format("15:04")
	if start < end {
		return fmt.Sprintf("timeOfDay(now, %s) >= '%s' AND timeOfDay(now, %s) < '%s'", zoneArg, start, zoneArg, end)
	}

	return fmt.Sprintf("(timeOfDay(now, %s) >= '%s' OR timeOfDay(now, %s) < '%s')", zoneArg, start, zoneArg, end)
}

var dayNames = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// dayNeighborhood renders a dayOfWeek membership covering the instant's day and both
// neighbors, so a suite straddling midnight stays inside it.
func dayNeighborhood(zoneArg string, at time.Time) string {
	day := int(at.Weekday())

	return fmt.Sprintf("dayOfWeek(now, %s) IN ('%s', '%s', '%s')", zoneArg, dayNames[(day+6)%7], dayNames[day], dayNames[(day+1)%7])
}

// awayDays renders a dayOfWeek membership that excludes the instant's day and both
// neighbors — a computed weekend the instant can never fall on.
func awayDays(zoneArg string, at time.Time) string {
	day := int(at.Weekday())

	return fmt.Sprintf("dayOfWeek(now, %s) IN ('%s', '%s')", zoneArg, dayNames[(day+3)%7], dayNames[(day+4)%7])
}

// provisionTemporalAccess migrates the scripted shift roles through the production
// deploy path at Anvil and returns the live engine.
func provisionTemporalAccess(ctx context.Context, t *testing.T, db *initiator.SpannerDB, conf *access.RoleConfig, users map[accesstypes.User][]accesstypes.Role, probe accesstypes.User) *access.Client {
	t.Helper()

	client := newAccessClient(t, db)
	if err := access.MigrateRoles(ctx, client.UserManager(), router.Collection(), conf, anvil, bastion, cinder); err != nil {
		t.Fatalf("access.MigrateRoles() error = %v", err)
	}
	for user, roles := range users {
		if err := client.UserManager().AddUserRoles(ctx, accesstypes.DomainScope(anvil), user, roles...); err != nil {
			t.Fatalf("AddUserRoles(%s) error = %v", user, err)
		}
	}

	opsClock, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}
	checker := client.ForUser(probe)
	deadline := time.Now().Add(15 * time.Second)
	for {
		decisions, err := checker.Check(ctx, accesstypes.NewEnvironment().WithNow(time.Now()).WithZone(opsClock),
			accesstypes.DomainScope(anvil), accesstypes.List, "Refits")
		if err == nil && !decisions["Refits"].IsDenied() {
			return client
		}
		if time.Now().After(deadline) {
			t.Fatalf("policy snapshot never became visible; last decisions: %v (err %v)", decisions, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestTemporalWindows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	opsClock, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}
	nowLocal := time.Now().In(opsClock)

	refitFields := []accesstypes.Tag{"shipId", "statusId", "estimate"}
	taskFields := []accesstypes.Tag{"notes"}
	conf := &access.RoleConfig{Roles: access.ScopedRoles{Domain: []*access.Role{
		{
			Name: "OnShift",
			Permissions: map[accesstypes.Permission][]access.Grant{
				accesstypes.List: {{
					Resource: "Refits", Fields: refitFields,
					Condition: timeOfDayWindow("local", nowLocal, 2*time.Hour) + " AND " + dayNeighborhood("'UTC'", time.Now().UTC()),
				}},
				accesstypes.Update: {{Resource: "RefitTasks", Fields: taskFields, Condition: dayNeighborhood("local", nowLocal) + " AND state = 'in_refit'"}},
			},
		},
		{
			Name: "OffShift",
			Permissions: map[accesstypes.Permission][]access.Grant{
				accesstypes.List: {{
					Resource: "Refits", Fields: refitFields,
					Condition: timeOfDayWindow("'America/Denver'", nowLocal.Add(12*time.Hour), 2*time.Hour),
				}},
				accesstypes.Update: {{Resource: "RefitTasks", Fields: taskFields, Condition: awayDays("local", nowLocal) + " AND state = 'in_refit'"}},
			},
		},
		{
			// A window straddling midnight around the instant: the wrap-around OR
			// form must hold across the boundary.
			Name: "Midnight",
			Permissions: map[accesstypes.Permission][]access.Grant{
				accesstypes.List: {{
					Resource: "Refits", Fields: refitFields,
					Condition: timeOfDayWindow("local", nowLocal, 11*time.Hour),
				}},
			},
		},
	}}}

	client := provisionTemporalAccess(ctx, t, db, conf, map[accesstypes.User][]accesstypes.Role{
		"shift-on":       {"OnShift"},
		"shift-off":      {"OffShift"},
		"shift-midnight": {"Midnight"},
	}, "shift-on")
	h := newTestAppWithAccess(db, client)

	tests := []struct {
		name       string
		user       accesstypes.User
		method     string
		target     string
		body       string
		wantStatus int
		wantRows   bool
		wantDigest accesstypes.DigestState
	}{
		{
			name:       "the on-shift window is open now",
			user:       "shift-on",
			target:     sectorPath(anvil, "refits"),
			wantStatus: http.StatusOK,
			wantRows:   true,
		},
		{
			name:       "the off-shift window is closed now",
			user:       "shift-off",
			target:     sectorPath(anvil, "refits"),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "a wrap-around window holds across midnight",
			user:       "shift-midnight",
			target:     sectorPath(anvil, "refits"),
			wantStatus: http.StatusOK,
			wantRows:   true,
		},
		{
			name:       "dayOfWeek admits on a computed weekday beside a state term",
			user:       "shift-on",
			method:     http.MethodPatch,
			target:     "/api/resources",
			body:       fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"notes":"checked on shift"}}]`, opPath(anvil, "refit-tasks/"+refitSamaritanID+"/2")),
			wantStatus: http.StatusOK,
		},
		{
			name:       "the state term beside a folded temporal term still refuses",
			user:       "shift-on",
			method:     http.MethodPatch,
			target:     "/api/resources",
			body:       fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"notes":"not in refit"}}]`, opPath(anvil, "refit-tasks/"+refitMuleID+"/1")),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "dayOfWeek refuses on a computed weekend",
			user:       "shift-off",
			method:     http.MethodPatch,
			target:     "/api/resources",
			body:       fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"notes":"weekend"}}]`, opPath(anvil, "refit-tasks/"+refitSamaritanID+"/2")),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "the on-shift digest reads conditional at any hour",
			user:       "shift-on",
			target:     "/api/permission-digest?domain=" + anvil,
			wantStatus: http.StatusOK,
			wantDigest: accesstypes.DigestConditional,
		},
		{
			name:       "the off-shift digest reads conditional at any hour",
			user:       "shift-off",
			target:     "/api/permission-digest?domain=" + anvil,
			wantStatus: http.StatusOK,
			wantDigest: accesstypes.DigestConditional,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			status, body := doRequestAs(t, h, tt.user, method, tt.target, tt.body)
			assertStatus(t, status, tt.wantStatus, body)

			if tt.wantRows {
				if rows := decodeRows(t, body); len(rows) == 0 {
					t.Error("list returned no refits; Anvil has four")
				}
			}
			if tt.wantDigest != "" {
				var digest accesstypes.PermissionDigest
				if err := json.Unmarshal(body, &digest); err != nil {
					t.Fatalf("decoding digest: %v", err)
				}
				if got := digest["Refits"]["List"]; got != tt.wantDigest {
					t.Errorf("digest[Refits][List] = %q, want %q", got, tt.wantDigest)
				}
			}
		})
	}
}

// TestShiftPairIsComplementary pins the shipped personas: at any hour exactly one of
// Dockmaster Dara (06:00–18:00 local) and Night Watch Nadia (18:00–06:00 Denver) sees
// the hangar deck.
func TestShiftPairIsComplementary(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	dara, daraBody := doRequestAs(t, h, "dock", http.MethodGet, sectorPath(anvil, "refits"), "")
	nadia, nadiaBody := doRequestAs(t, h, "watch", http.MethodGet, sectorPath(anvil, "refits"), "")
	if (dara == http.StatusOK) == (nadia == http.StatusOK) {
		t.Fatalf("dara = %d (%s), nadia = %d (%s): exactly one shift must see the deck", dara, daraBody, nadia, nadiaBody)
	}
}

// TestLocalZoneMustBeWired pins the fail-loud posture: a `local` condition checked
// through an Environment that carries no zone is an error, never a silent decision.
func TestLocalZoneMustBeWired(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	_, _, client := sharedWorld(t)
	checker := client.ForUser("dock")

	if _, err := checker.Check(ctx, accesstypes.EnvironmentAt(time.Now()), accesstypes.DomainScope(anvil), accesstypes.List, "Refits"); err == nil {
		t.Fatal("Check() with no zone succeeded; want a loud error for an unresolvable local")
	}
}
