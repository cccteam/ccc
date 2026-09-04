package integration

// Dynamic temporal expressions (design plan §05, decided 2026-09-04): a grant
// may carry a wall-clock window — timeOfDay(now, zone), dayOfWeek(now, zone) —
// evaluated by the ENGINE at the decision instant; SQL never renders timezone
// arithmetic. The bare word local resolves to the zone the application wires
// (app.New sets the fleet's operations clock, America/Denver).
//
// The shipped demo roles deliberately carry no wall-clock window: the persona
// suites pin fixed views, and a windowed grant would make them depend on the
// hour the suite runs. This suite provisions its own roles through the REAL
// engine — spannerstore → access.Client → MigrateRoles — with windows computed
// around the test's own instant, so the assertions are deterministic at any
// hour: the day-shift user's window always contains now, the night-shift
// user's never does.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/waystation/pkg/router"
	initiator "github.com/cccteam/db-initiator"
)

// timeOfDayWindow renders a two-sided timeOfDay window covering
// [center-halfWidth, center+halfWidth) in the given wall clock, in the OR
// form when it crosses midnight — so the expression is correct at any hour
// the suite runs.
func timeOfDayWindow(zoneArg string, center time.Time, halfWidth time.Duration) string {
	start := center.Add(-halfWidth).Format("15:04")
	end := center.Add(halfWidth).Format("15:04")
	if start < end {
		return fmt.Sprintf("timeOfDay(now, %s) >= '%s' AND timeOfDay(now, %s) < '%s'", zoneArg, start, zoneArg, end)
	}

	return fmt.Sprintf("(timeOfDay(now, %s) >= '%s' OR timeOfDay(now, %s) < '%s')", zoneArg, start, zoneArg, end)
}

// dayNeighborhood renders a dayOfWeek membership covering the instant's day
// and both neighbors in the given wall clock, so a suite straddling midnight
// stays inside it.
func dayNeighborhood(zoneArg string, at time.Time) string {
	names := [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
	day := int(at.Weekday())

	return fmt.Sprintf("dayOfWeek(now, %s) IN ('%s', '%s', '%s')",
		zoneArg, names[(day+6)%7], names[day], names[(day+1)%7])
}

// provisionTemporalAccess migrates the scripted shift roles through the
// production deploy path and returns the live engine.
func provisionTemporalAccess(ctx context.Context, t *testing.T, db *initiator.SpannerDB, conf *access.RoleConfig, users map[accesstypes.User][]accesstypes.Role) *access.Client {
	t.Helper()

	store, err := spannerstore.New(db.Client)
	if err != nil {
		t.Fatalf("spannerstore.New() error = %v", err)
	}
	client, err := access.New(store)
	if err != nil {
		t.Fatalf("access.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("access.Client.Close() error = %v", err)
		}
	})

	if err := access.MigrateRoles(ctx, client.UserManager(), router.Collection(), conf, wsAlpha, wsBeta, wsCeres); err != nil {
		t.Fatalf("access.MigrateRoles() error = %v", err)
	}
	for user, roles := range users {
		if err := client.UserManager().AddUserRoles(ctx, accesstypes.DomainScope(wsAlpha), user, roles...); err != nil {
			t.Fatalf("AddUserRoles(%s) error = %v", user, err)
		}
	}

	// The snapshot swap is asynchronous: poll until the in-window grant folds
	// visible for the day-shift user. The poll's Environment carries the zone
	// the grant's local resolves to — the fold fails loudly without it, the
	// designed posture for a missing fact.
	opsClock, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}
	checker := client.ForUser("sato-day")
	deadline := time.Now().Add(15 * time.Second)
	for {
		decisions, err := checker.Check(ctx, accesstypes.NewEnvironment().WithNow(time.Now()).WithZone(opsClock),
			accesstypes.DomainScope(wsAlpha), accesstypes.List, "Teams")
		if err == nil && !decisions["Teams"].IsDenied() {
			return client
		}
		if time.Now().After(deadline) {
			t.Fatalf("policy snapshot never became visible; last decisions: %v (err %v)", decisions, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestTemporalWindows walks the wall-clock window end to end through the real
// engine: the day-shift role's window contains the suite's instant (via the
// app-wired local zone), the night-shift role's window — the same clock read
// twelve hours away, in an explicit zone — never does, and the digest reports
// both grants conditional regardless of the hour (structural, never folded at
// fetch).
func TestTemporalWindows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	// The app wires local = America/Denver (the operations clock); the
	// windows are computed on the same wall clock the fold will read.
	opsClock, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}
	nowLocal := time.Now().In(opsClock)

	teamFields := []accesstypes.Tag{"waystationId", "name", "specialty"}
	conf := &access.RoleConfig{Roles: access.ScopedRoles{Domain: []*access.Role{
		{
			Name: "DayShift",
			Permissions: map[accesstypes.Permission][]access.Grant{
				accesstypes.List: {{
					Resource:  "Teams",
					Fields:    teamFields,
					Condition: timeOfDayWindow("local", nowLocal, 2*time.Hour) + " AND " + dayNeighborhood("'UTC'", time.Now().UTC()),
				}},
			},
		},
		{
			Name: "NightShift",
			Permissions: map[accesstypes.Permission][]access.Grant{
				accesstypes.List: {{
					Resource:  "Teams",
					Fields:    teamFields,
					Condition: timeOfDayWindow("'America/Denver'", nowLocal.Add(12*time.Hour), 2*time.Hour),
				}},
			},
		},
	}}}

	client := provisionTemporalAccess(ctx, t, db, conf, map[accesstypes.User][]accesstypes.Role{
		"sato-day":  {"DayShift"},
		"nyx-night": {"NightShift"},
	})
	h := newTestAppWithAccess(db, client)

	t.Run("the day shift's window is open now", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "sato-day", http.MethodGet, "/api/waystations/ws-alpha/teams", "")
		assertStatus(t, status, http.StatusOK, body)
		if rows := decodeRows(t, body); len(rows) == 0 {
			t.Error("day-shift list returned no teams; the seeded station has two")
		}
	})

	t.Run("the night shift's window is closed now", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "nyx-night", http.MethodGet, "/api/waystations/ws-alpha/teams", "")
		assertStatus(t, status, http.StatusForbidden, body)
	})

	// The digest never folds a window (§13: structural tri-state): both
	// shifts read conditional at any hour — navigation renders, and the
	// check refuses per request.
	t.Run("the digest reports the window conditional at any hour", func(t *testing.T) {
		t.Parallel()

		for _, user := range []accesstypes.User{"sato-day", "nyx-night"} {
			status, body := doRequestAs(t, h, user, http.MethodGet, "/api/permission-digest?domain=ws-alpha", "")
			assertStatus(t, status, http.StatusOK, body)

			var digest accesstypes.PermissionDigest
			if err := json.Unmarshal(body, &digest); err != nil {
				t.Fatalf("decoding digest for %s: %v", user, err)
			}
			if got := digest["Teams"]["List"]; got != accesstypes.DigestConditional {
				t.Errorf("digest[Teams][List] for %s = %q, want %q", user, got, accesstypes.DigestConditional)
			}
		}
	})
}
