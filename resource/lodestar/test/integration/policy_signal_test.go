// Demonstrates: live.signals, auth.user-management.
package integration

// This suite pins the policy kind of the one signal channel. Two instances of the
// application run over one database, each over its own crew engine on the crew store,
// both engines wired with the policy signal over one in-memory live service (live.Fake,
// which fans a signal out to every subscription) and their heartbeat pinned to an hour,
// so only the signal can carry a change between them. The marshal seats the cadet as a
// harbormaster at Anvil through the first instance's seats route (the generated
// user-management surface's CrewSectorSeats: Create in the sector, held by SectorMarshal
// for every role but its own); the engine that wrote announces the policy kind, and the
// second instance serves the cadet the fleet board at its next request, with no restart
// and no heartbeat; unseated, it refuses again. The border holds on the way: the cadet
// cannot seat herself, the marshal cannot mint a marshal, and Bastion is a sector that
// does not exist for the marshal. The steps run in order, each named in its failure,
// since every one reads what the ones before it wrote. The walkthrough plays the same
// scenario against the Firestore emulator with a second server process.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/live"
	"github.com/cccteam/ccc/resource/lodestar/app"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	initiator "github.com/cccteam/db-initiator"
)

// The suite's cast, the shipped personas: the marshal holds SectorMarshal at Anvil and
// with it the sector's seats; the cadet holds no List on Ships until seated.
const (
	policyMarshal accesstypes.User = "marshal"
	policyCadet   accesstypes.User = "cadet"
	policyHollis  accesstypes.User = "harbormaster"

	harbormasterRole accesstypes.Role = "Harbormaster"
	marshalRole      accesstypes.Role = "SectorMarshal"

	// policyHeartbeat is the engines' heartbeat in the suite: far beyond the settle
	// bound, so a reload on the second instance can only have come from the signal.
	policyHeartbeat = time.Hour
)

// crewPartitions are the crew surface's names as the generator derived them: the
// sector partition's seats and users resources and their route segments.
func crewPartitions() resource.UserResourceNames {
	decl := router.CrewAuth{}.UserSurfaceDeclaration()

	return decl.Names()
}

// crewSectorSeats is the console's seats route of the crew's sector partition.
func crewSectorSeats(sector string) string {
	return sectorPath(sector, crewPartitions().Tenant.SeatsRoute)
}

// crewSectorUsers is the console's users route of the crew's sector partition.
func crewSectorUsers(sector string) string {
	return sectorPath(sector, crewPartitions().Tenant.UsersRoute)
}

// seatBody is the operations body that seats the user in the role: the seat's key in the
// operation's path, the way the generated resources take theirs.
func seatBody(user ccc.UUID, role accesstypes.Role) string {
	return fmt.Sprintf(`[{"op":"add","path":"/%s/%s","value":{}}]`, user, role)
}

// unseatBody is the operations body that removes the seat.
func unseatBody(user ccc.UUID, role accesstypes.Role) string {
	return fmt.Sprintf(`[{"op":"remove","path":"/%s/%s"}]`, user, role)
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
// members engine, the shared live service and the shared account store, starts it, and
// serves it through the test router with the hand-written routes, where the generated
// user-management routes are mounted with every other. The crew surface seats through
// the instance's own engine, so a seat written on one instance is announced by that
// instance's engine, as production's is.
func startPolicyInstance(t *testing.T, name string, db *initiator.SpannerDB, crewEngine *access.Client, membersEngine access.Controller, svc live.Service, accounts *resource.MemUserStore) *flagInstance {
	t.Helper()

	memberStore := resource.NewMemUserStore()
	a := app.New(&testConfigurer{
		db:            db,
		access:        crewEngine,
		membersAccess: membersEngine,
		live:          svc,
		crewUsers:     router.NewCrewUserSurface(accounts, crewEngine.UserManager()),
		membersUsers:  router.NewMembersUserSurface(memberStore, memberStore),
	})
	if err := a.Start(t.Context()); err != nil {
		t.Fatalf("app.Start() error = %v", err)
	}

	return &flagInstance{name: name, h: withHandWrittenRoutes(a)}
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

// assertUserRoles pins the roles the sector's users route shows on one user's row.
func assertUserRoles(t *testing.T, step string, body []byte, username accesstypes.User, want []string) {
	t.Helper()

	var rows []struct {
		Username string   `json:"username"`
		Roles    []string `json:"roles"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("%s: decoding the users: %v: %s", step, err, body)
	}
	for _, row := range rows {
		if row.Username != string(username) {
			continue
		}
		got := slices.Clone(row.Roles)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: %s holds %v, want %v", step, username, got, want)
		}

		return
	}
	t.Errorf("%s: %s is not among the sector's users: %s", step, username, body)
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
	// the precondition. The accounts are an in-memory store the two instances share, so
	// the cadet's id is one on both.
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
	seats := crewPartitions().Tenant.Seats
	if err := waitForDecision(ctx, follower, policyMarshal, anvil, accesstypes.Create, seats); err != nil {
		t.Fatalf("the follower never learned the marshal's seat authority from the writer's signals: %v", err)
	}
	accounts := resource.NewMemUserStore()
	ids := make(map[accesstypes.User]ccc.UUID)
	for _, user := range demoUsers(t) {
		ids[user.User] = accounts.AddUser(string(user.User), true)
	}
	cadet := ids[policyCadet]

	first := startPolicyInstance(t, "the first instance", db, writer, membersEngine, fake, accounts)
	second := startPolicyInstance(t, "the second instance", db, follower, membersEngine, fake, accounts)
	signalsBefore := policySignals(fake)

	steps := []flagStep{
		{name: "before: the cadet holds no List on Ships on the second instance", on: second, user: policyCadet, method: http.MethodGet, target: sectorPath(anvil, "ships"), wantStatus: http.StatusForbidden},
		{
			name: "before: the marshal lists the sector's crew, the cadet's seats among them", on: first, user: policyMarshal, method: http.MethodGet, target: crewSectorUsers(anvil), wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertUserRoles(t, step, body, policyCadet, []string{"Cadet", "SectorCrew"})
				assertUserRoles(t, step, body, policyHollis, []string{string(harbormasterRole), "SectorCrew"})
			},
		},
		{name: "the cadet cannot seat herself: no Create on the sector's seats", on: first, user: policyCadet, method: http.MethodPatch, target: crewSectorSeats(anvil), body: seatBody(cadet, harbormasterRole), wantStatus: http.StatusForbidden},
		{name: "the marshal cannot mint a marshal: the grant's condition on the role", on: first, user: policyMarshal, method: http.MethodPatch, target: crewSectorSeats(anvil), body: seatBody(cadet, marshalRole), wantStatus: http.StatusForbidden},
		{name: "Bastion does not exist for the marshal: the concealed-sector guard", on: first, user: policyMarshal, method: http.MethodPatch, target: crewSectorSeats(bastion), body: seatBody(cadet, harbormasterRole), wantStatus: http.StatusNotFound},
		{
			name: "the marshal seats the cadet as a harbormaster at Anvil: one signal of the policy kind", on: first, user: policyMarshal, method: http.MethodPatch, target: crewSectorSeats(anvil), body: seatBody(cadet, harbormasterRole), wantStatus: http.StatusOK,
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
			name: "seated: her row carries the seat", on: first, user: policyMarshal, method: http.MethodGet, target: crewSectorUsers(anvil), wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertUserRoles(t, step, body, policyCadet, []string{"Cadet", string(harbormasterRole), "SectorCrew"})
			},
		},
		{name: "seated: the instance that wrote serves her the fleet board at once", on: first, user: policyCadet, method: http.MethodGet, target: sectorPath(anvil, "ships"), wantStatus: http.StatusOK},
		{name: "seated: the second instance serves her the fleet board at its next request, with no restart and no heartbeat", on: second, user: policyCadet, method: http.MethodGet, target: sectorPath(anvil, "ships"), wantStatus: http.StatusOK, settle: true},
		{name: "the marshal unseats her", on: first, user: policyMarshal, method: http.MethodPatch, target: crewSectorSeats(anvil), body: unseatBody(cadet, harbormasterRole), wantStatus: http.StatusOK},
		{name: "unseated: the instance that wrote refuses again", on: first, user: policyCadet, method: http.MethodGet, target: sectorPath(anvil, "ships"), wantStatus: http.StatusForbidden},
		{name: "unseated: the second instance refuses at its next request", on: second, user: policyCadet, method: http.MethodGet, target: sectorPath(anvil, "ships"), wantStatus: http.StatusForbidden, settle: true},
	}
	for i := range steps {
		runFlagStep(t, &steps[i])
	}
}
