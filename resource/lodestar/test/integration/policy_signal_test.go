// Demonstrates: live.signals, auth.user-management.
package integration

// This suite pins the policy kind of the one signal channel. Two instances of the
// application run over one database, each over its own crew engine on the crew store,
// both engines wired with the policy signal over one in-memory live service (live.Fake,
// which fans a signal out to every subscription) and their heartbeat pinned to an hour,
// so only the signal can carry a change between them. The marshal seats the cadet as a
// harbormaster at Anvil through the first instance's role-membership route (the access
// library's user-management handlers behind Create on RoleMemberships in the sector); the
// engine that wrote announces the policy kind, and the second instance serves the cadet
// the fleet board at its next request, with no restart and no heartbeat; unseated, it
// refuses again. The border holds on the way: the cadet cannot seat herself, and Bastion
// is a sector that does not exist for the marshal. The steps run in order, each named in
// its failure, since every one reads what the ones before it wrote. The walkthrough plays
// the same scenario against the Firestore emulator with a second server process.

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/live"
	"github.com/cccteam/ccc/resource/lodestar/app"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/httpio"
	"github.com/go-chi/chi/v5"
)

// The suite's cast, the shipped personas: the marshal holds SectorMarshal at Anvil and
// with it the sector's role memberships; the cadet holds no List on Ships until seated.
const (
	policyMarshal accesstypes.User = "marshal"
	policyCadet   accesstypes.User = "cadet"
	policyHollis  accesstypes.User = "harbormaster"

	harbormasterRole accesstypes.Role = "Harbormaster"

	// policyHeartbeat is the engines' heartbeat in the suite: far beyond the settle
	// bound, so a reload on the second instance can only have come from the signal.
	policyHeartbeat = time.Hour
)

// seatBody is the membership routes' body: the users to seat or unseat.
const seatBody = `{"users":["cadet"]}`

// harbormasterSeats is the console's role-membership route for Harbormaster in the sector.
func harbormasterSeats(sector string) string {
	return sectorPath(sector, "roles/"+string(harbormasterRole)+"/users")
}

// newPolicyEngine opens a crew engine over the store with the policy signal and the
// suite's heartbeat, and waits for its first snapshot as crew.New does, which starts its
// watch on the policy kind.
func newPolicyEngine(ctx context.Context, t *testing.T, db *initiator.SpannerDB, signal access.ChangeSignal) *access.Client {
	t.Helper()

	engine, err := openEngineWith(db, crew.TablePrefix, crew.Roles(), access.WithChangeSignal(signal), access.WithHeartbeatInterval(policyHeartbeat))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := engine.Close(); err != nil {
			t.Errorf("access.Client.Close() error = %v", err)
		}
	})
	if err := engine.WaitReady(ctx); err != nil {
		t.Fatalf("access.Client.WaitReady() error = %v", err)
	}

	return engine
}

// startPolicyInstance builds an App over the database, its own crew engine, the shared
// members engine and the shared live service, starts it, and mounts the console's
// role-membership routes beside the hand-written routes and the test router, where
// production's AppHooks mounts them.
func startPolicyInstance(t *testing.T, name string, db *initiator.SpannerDB, crewEngine *access.Client, membersEngine access.Controller, svc live.Service) *flagInstance {
	t.Helper()

	a := app.New(&testConfigurer{db: db, access: crewEngine, membersAccess: membersEngine, live: svc, management: crewEngine.Handlers(httpio.Log)})
	if err := a.Start(t.Context()); err != nil {
		t.Fatalf("app.Start() error = %v", err)
	}

	r := chi.NewRouter()
	r.Use(httpio.WithParams)
	r.Get(router.RoleMembershipsRoute, a.DomainGuard()(a.RoleUsers()))
	r.Post(router.RoleMembershipsRoute, a.DomainGuard()(a.AddRoleUsers()))
	r.Delete(router.RoleMembershipsRoute, a.DomainGuard()(a.DeleteRoleUsers()))
	r.Mount("/", withHandWrittenRoutes(a))

	return &flagInstance{name: name, h: r}
}

// waitForSubscribers blocks until the fake holds the wanted subscriptions to the kind:
// an engine's watch subscribes on its own goroutine once the engine starts.
func waitForSubscribers(t *testing.T, fake *live.Fake, kind live.Kind, want int) {
	t.Helper()

	deadline := time.Now().Add(settleWithin)
	for fake.Subscribers(kind) != want {
		if time.Now().After(deadline) {
			t.Fatalf("subscriptions to the %s kind = %d, want %d", kind, fake.Subscribers(kind), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// policySignals counts the policy kind's signals the fake received.
func policySignals(fake *live.Fake) int {
	n := 0
	for _, kind := range fake.Signals() {
		if kind == live.KindPolicy {
			n++
		}
	}

	return n
}

// assertRoleUsers pins the roster the route answers, in any order.
func assertRoleUsers(t *testing.T, step string, body []byte, want []accesstypes.User) {
	t.Helper()

	var got []accesstypes.User
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("%s: decoding the roster: %v: %s", step, err, body)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s: roster = %v, want %v", step, got, want)
	}
}

// TestPolicySignal_seatReachesSecondInstance plays the scenario in order over two
// instances.
func TestPolicySignal_seatReachesSecondInstance(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	// One live service, one policy signal over it, two crew engines over one store: the
	// shape two instances of the application have.
	fake := live.NewFake()
	policySignal := auth.PolicySignal(fake)
	writer := newPolicyEngine(ctx, t, db, policySignal)
	follower := newPolicyEngine(ctx, t, db, policySignal)
	waitForSubscribers(t, fake, live.KindPolicy, 2)

	// The personas' memberships are written through the writer, which announces each;
	// the follower, whose first snapshot may predate them, learns them the same way the
	// scenario's seat travels, and the marshal's authority over Anvil's seats on it is
	// the precondition.
	membersEngine, err := openEngine(db, members.TablePrefix, members.Roles())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := membersEngine.Close(); err != nil {
			t.Errorf("access.Client.Close() error = %v", err)
		}
	})
	if err := provisionDemoAccess(ctx, writer, membersEngine); err != nil {
		t.Fatal(err)
	}
	if err := waitForDecision(ctx, follower, policyMarshal, anvil, accesstypes.Create, resources.RoleMemberships); err != nil {
		t.Fatalf("the follower never learned the marshal's seat authority from the writer's signals: %v", err)
	}

	first := startPolicyInstance(t, "the first instance", db, writer, membersEngine, fake)
	second := startPolicyInstance(t, "the second instance", db, follower, membersEngine, fake)
	signalsBefore := policySignals(fake)

	steps := []flagStep{
		{name: "before: the cadet holds no List on Ships on the second instance", on: second, user: policyCadet, method: http.MethodGet, target: sectorPath(anvil, "ships"), wantStatus: http.StatusForbidden},
		{
			name: "before: the marshal lists who holds Harbormaster at Anvil", on: first, user: policyMarshal, method: http.MethodGet, target: harbormasterSeats(anvil), wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertRoleUsers(t, step, body, []accesstypes.User{policyHollis})
			},
		},
		{name: "the cadet cannot seat herself: no Create on RoleMemberships", on: first, user: policyCadet, method: http.MethodPost, target: harbormasterSeats(anvil), body: seatBody, wantStatus: http.StatusForbidden},
		{name: "Bastion does not exist for the marshal: the concealed-sector guard", on: first, user: policyMarshal, method: http.MethodPost, target: harbormasterSeats(bastion), body: seatBody, wantStatus: http.StatusNotFound},
		{
			name: "the marshal seats the cadet as a harbormaster at Anvil: one signal of the policy kind", on: first, user: policyMarshal, method: http.MethodPost, target: harbormasterSeats(anvil), body: seatBody, wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, _ []byte) {
				t.Helper()
				// The engine announces after the write on its own goroutine.
				deadline := time.Now().Add(settleWithin)
				for policySignals(fake) <= signalsBefore {
					if time.Now().After(deadline) {
						t.Fatalf("%s: no signal of the %s kind followed the write", step, live.KindPolicy)
					}
					time.Sleep(20 * time.Millisecond)
				}
			},
		},
		{
			name: "seated: the role's roster carries her", on: first, user: policyMarshal, method: http.MethodGet, target: harbormasterSeats(anvil), wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertRoleUsers(t, step, body, []accesstypes.User{policyCadet, policyHollis})
			},
		},
		{name: "seated: the instance that wrote serves her the fleet board at once", on: first, user: policyCadet, method: http.MethodGet, target: sectorPath(anvil, "ships"), wantStatus: http.StatusOK},
		{name: "seated: the second instance serves her the fleet board at its next request, with no restart and no heartbeat", on: second, user: policyCadet, method: http.MethodGet, target: sectorPath(anvil, "ships"), wantStatus: http.StatusOK, settle: true},
		{name: "the marshal unseats her", on: first, user: policyMarshal, method: http.MethodDelete, target: harbormasterSeats(anvil), body: seatBody, wantStatus: http.StatusOK},
		{name: "unseated: the instance that wrote refuses again", on: first, user: policyCadet, method: http.MethodGet, target: sectorPath(anvil, "ships"), wantStatus: http.StatusForbidden},
		{name: "unseated: the second instance refuses at its next request", on: second, user: policyCadet, method: http.MethodGet, target: sectorPath(anvil, "ships"), wantStatus: http.StatusForbidden, settle: true},
	}
	for i := range steps {
		runFlagStep(t, &steps[i])
	}
}
