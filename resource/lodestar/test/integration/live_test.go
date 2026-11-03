// Demonstrates: live.pages, live.derived.
package integration

// This suite pins live pages over the seeded world through an in-memory live service
// (live.Fake): the harbormaster's fleet board and one ship open register two
// subscriptions before their queries run and answer cacheable when they carry the
// version parameter; the engineer's refit commits exactly two change documents into
// her set, the row and the list; a pilot watching another sector's fleet receives
// nothing; and a refused request registers nothing. The walkthrough plays the same
// scenario against the Firestore emulator.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/live"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/go-playground/errors/v5"
)

// The live suite's cast: who watches what, and who commits.
const (
	liveHarbormaster accesstypes.User = "live-harbormaster"
	liveBastionPilot accesstypes.User = "live-bastion-pilot"
	liveEngineer     accesstypes.User = "live-engineer"
	liveTabAnvil                      = "tab-anvil-1"
	liveTabBastion                    = "tab-bastion-1"
	liveSeed                          = "seed-0001"
)

// principalID is a console user's live principal id: the console's sessions come from
// the crew auth, so the id is the auth's name and the user name joined by the separator
// (crew|live-harbormaster), the uid of the user's change set and browser identity.
func principalID(user accesstypes.User) string {
	return crew.Name + live.AuthSeparator + string(user)
}

// liveHeaders is what a live request carries: the tab in the subscribe header.
func liveHeaders(tab string) map[string]string {
	return map[string]string{live.SubscribeHeader: tab}
}

// versioned appends the version parameter to a target.
func versioned(target, version string) string {
	return target + "?" + live.VersionParam + "=" + version
}

// subscriptionNames spells the fake's record the way the walkthrough reads the
// emulator's: principal, tab, resource, key and domain.
func subscriptionNames(subs []live.Subscription) []string {
	names := make([]string, 0, len(subs))
	for _, sub := range subs {
		names = append(names, fmt.Sprintf("%s|%s|%s|%s|%s", sub.Principal, sub.Tab, sub.Resource, sub.Key, sub.Domain))
	}
	slices.Sort(names)

	return names
}

// changeNames spells a change set: kind, resource, key, domain and the deleted flag.
func changeNames(changes []live.FakeChange) []string {
	names := make([]string, 0, len(changes))
	for _, change := range changes {
		names = append(names, fmt.Sprintf("%s|%s|%s|%s|%t", change.Kind, change.Resource, change.Key, change.Domain, change.Deleted))
	}
	slices.Sort(names)

	return names
}

// TestLivePages_subscribeRefitPublish plays the scenario in order: the subscriptions
// land before any commit, the refit publishes, and each set holds what its viewer was
// subscribed to.
func TestLivePages_subscribeRefitPublish(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	fake := live.NewFake()
	h := newTestAppWithLive(db, grants{
		accesstypes.List:    {"Ships"},
		accesstypes.Read:    {"Ships"},
		accesstypes.Execute: {"StartFlightTest", "PassFlightTest"},
	}, fake)

	// The harbormaster's fleet board at Anvil and the Patient Heron open, the pilot's
	// fleet board at Bastion, and a cadet's request the grants refuse: each live request
	// is one row of the table, in the order a browser would send them.
	requests := []struct {
		name       string
		user       accesstypes.User
		tab        string
		target     string
		wantStatus int
		wantCache  string
	}{
		{
			name:       "the harbormaster's fleet board at Anvil, by the seed",
			user:       liveHarbormaster,
			tab:        liveTabAnvil,
			target:     versioned(sectorPath(anvil, "ships"), liveSeed),
			wantStatus: http.StatusOK,
			wantCache:  live.CacheControl,
		},
		{
			name:       "the Patient Heron open on her board, by the seed",
			user:       liveHarbormaster,
			tab:        liveTabAnvil,
			target:     versioned(sectorPath(anvil, "ships/"+shipPatientHeronID), liveSeed),
			wantStatus: http.StatusOK,
			wantCache:  live.CacheControl,
		},
		{
			name:       "the pilot's fleet board at Bastion, without a version",
			user:       liveBastionPilot,
			tab:        liveTabBastion,
			target:     sectorPath(bastion, "ships"),
			wantStatus: http.StatusOK,
			wantCache:  "",
		},
		{
			name:       "a request the grants refuse registers nothing",
			user:       liveHarbormaster,
			tab:        liveTabAnvil,
			target:     versioned(sectorPath(anvil, "refits"), liveSeed),
			wantStatus: http.StatusForbidden,
			wantCache:  "",
		},
	}
	for _, tt := range requests {
		rr := doRequestRecordedWithHeaders(t, h, tt.user, http.MethodGet, tt.target, "", liveHeaders(tt.tab))
		assertStatus(t, rr.Code, tt.wantStatus, rr.Body.Bytes())
		if got := rr.Header().Get("Cache-Control"); got != tt.wantCache {
			t.Errorf("%s: Cache-Control = %q, want %q", tt.name, got, tt.wantCache)
		}
	}

	wantSubscriptions := []string{
		fmt.Sprintf("%s|%s|Ships|%s|", principalID(liveHarbormaster), liveTabAnvil, shipPatientHeronID),
		fmt.Sprintf("%s|%s|Ships||%s", principalID(liveHarbormaster), liveTabAnvil, anvil),
		fmt.Sprintf("%s|%s|Ships||%s", principalID(liveBastionPilot), liveTabBastion, bastion),
	}
	slices.Sort(wantSubscriptions)
	if got := subscriptionNames(fake.Subscriptions()); !slices.Equal(got, wantSubscriptions) {
		t.Fatalf("subscriptions after the live requests = %v, want %v", got, wantSubscriptions)
	}

	// The engineer takes the Heron's refit from in_refit through the flight test; the
	// pass stamps the ship's LastRefitAt over the seeded one, so the commit touches the
	// Ships row the harbormaster watches and the Ships list in Anvil she watches, and
	// nothing else anyone subscribed to.
	before := readColumn[spanner.NullTime](ctx, t, db, "Ships", spanner.Key{shipPatientHeronID}, "LastRefitAt")
	for _, transition := range []string{"start-flight-test", "pass-flight-test"} {
		status, body := doRequestAs(t, h, liveEngineer, http.MethodPost, sectorPath(anvil, transition), fmt.Sprintf(`{"refitId":%q}`, refitHeronID))
		assertStatus(t, status, http.StatusOK, body)
	}

	changeSets := []struct {
		name string
		user accesstypes.User
		want []string
	}{
		{
			name: "the harbormaster's set holds the row and the list, one document each",
			user: liveHarbormaster,
			want: []string{
				"list|Ships||" + anvil + "|false",
				"row|Ships|" + shipPatientHeronID + "||false",
			},
		},
		{
			name: "the pilot watching Bastion's fleet receives nothing",
			user: liveBastionPilot,
			want: []string{},
		},
		{
			name: "the engineer subscribed to nothing and receives nothing",
			user: liveEngineer,
			want: []string{},
		},
	}
	for _, tt := range changeSets {
		if got := changeNames(fake.Changes(principalID(tt.user))); !slices.Equal(got, tt.want) {
			t.Errorf("%s: change set = %v, want %v", tt.name, got, tt.want)
		}
	}

	// The browser answers each document by asking again with the change's timestamp as
	// the version: the refetch is a plain live request, cacheable like the first.
	for _, target := range []string{sectorPath(anvil, "ships"), sectorPath(anvil, "ships/"+shipPatientHeronID)} {
		rr := doRequestRecordedWithHeaders(t, h, liveHarbormaster, http.MethodGet, versioned(target, "1700000000000000"), "", liveHeaders(liveTabAnvil))
		assertStatus(t, rr.Code, http.StatusOK, rr.Body.Bytes())
		if got := rr.Header().Get("Cache-Control"); got != live.CacheControl {
			t.Errorf("refetch of %s: Cache-Control = %q, want %q", target, got, live.CacheControl)
		}
	}
	after := readColumn[spanner.NullTime](ctx, t, db, "Ships", spanner.Key{shipPatientHeronID}, "LastRefitAt")
	if !after.Valid || !before.Valid || !after.Time.After(before.Time) {
		t.Errorf("the Heron's LastRefitAt = %v, want later than the seeded %v: the pass stamped nothing, so the row document reports no change", after, before)
	}
}

// TestLivePages_leaveAndLogout pins the two ways a tab's subscriptions end: the page
// leaving deletes the tab's, the logout deletes every one of the principal's and
// revokes the browser's identity, and both answer No Content whether or not anything
// was there.
func TestLivePages_leaveAndLogout(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	fake := live.NewFake()
	h := newTestAppWithLive(db, grants{accesstypes.List: {"Ships"}, accesstypes.Read: {"Ships"}}, fake)

	for _, tab := range []string{"tab-one", "tab-two"} {
		rr := doRequestRecordedWithHeaders(t, h, liveHarbormaster, http.MethodGet, sectorPath(anvil, "ships"), "", liveHeaders(tab))
		assertStatus(t, rr.Code, http.StatusOK, rr.Body.Bytes())
	}
	if got := len(fake.Subscriptions()); got != 2 {
		t.Fatalf("two tabs registered %d subscriptions, want 2", got)
	}

	steps := []struct {
		name         string
		body         string
		wantSubs     int
		wantRevoked  []string
		wantIdentity bool
	}{
		{name: "the first tab leaves", body: `{"tab":"tab-one","all":false}`, wantSubs: 1, wantRevoked: nil},
		{name: "the logout ends every subscription and the identity", body: `{"tab":"tab-two","all":true}`, wantSubs: 0, wantRevoked: []string{principalID(liveHarbormaster)}},
		{name: "a logout with nothing left still answers", body: `{"tab":"tab-two","all":true}`, wantSubs: 0, wantRevoked: []string{principalID(liveHarbormaster), principalID(liveHarbormaster)}},
	}
	for _, tt := range steps {
		rr := doRequestRecordedWithHeaders(t, h, liveHarbormaster, http.MethodPost, consoleAPI+"/"+live.UnsubscribeRoute, tt.body, liveHeaders("tab-two"))
		assertStatus(t, rr.Code, http.StatusNoContent, rr.Body.Bytes())
		if got := len(fake.Subscriptions()); got != tt.wantSubs {
			t.Errorf("%s: %d subscriptions remain, want %d", tt.name, got, tt.wantSubs)
		}
		if got := fake.Revoked(); !slices.Equal(got, tt.wantRevoked) {
			t.Errorf("%s: revoked = %v, want %v", tt.name, got, tt.wantRevoked)
		}
	}
}

// TestLivePages_renewRechecksGrants pins the renewal: the subscriptions the grants still
// cover are kept with a fresh expiry, a row of a domain-scoped resource re-checked in the
// domain it was read in, and one the grants do not cover is dropped, never refused.
func TestLivePages_renewRechecksGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	fake := live.NewFake()
	h := newTestAppWithLive(db, grants{accesstypes.List: {"Ships"}, accesstypes.Read: {"Ships"}}, fake)

	body := fmt.Sprintf(`{"tab":%q,"subscriptions":[{"resource":"Ships","domain":%q},{"resource":"Ships","key":%q,"domain":%q},{"resource":"Refits","domain":%q}]}`,
		liveTabAnvil, anvil, shipPatientHeronID, anvil, anvil)
	rr := doRequestRecordedWithHeaders(t, h, liveHarbormaster, http.MethodPost, consoleAPI+"/"+live.RenewRoute, body, liveHeaders(liveTabAnvil))
	assertStatus(t, rr.Code, http.StatusOK, rr.Body.Bytes())

	var renewed live.RenewResponse
	if err := decodeJSON(rr.Body.Bytes(), &renewed); err != nil {
		t.Fatal(err)
	}
	wantKept := []live.SubscriptionRequest{{Resource: "Ships", Domain: anvil}, {Resource: "Ships", Key: shipPatientHeronID, Domain: anvil}}
	wantDropped := []live.SubscriptionRequest{{Resource: "Refits", Domain: anvil}}
	if !slices.Equal(renewed.Kept, wantKept) {
		t.Errorf("kept = %v, want %v", renewed.Kept, wantKept)
	}
	if !slices.Equal(renewed.Dropped, wantDropped) {
		t.Errorf("dropped = %v, want %v", renewed.Dropped, wantDropped)
	}
	if renewed.ExpiresAt.IsZero() {
		t.Error("expiresAt is unset")
	}

	// The record keeps the two, the row without its domain.
	want := []string{
		fmt.Sprintf("%s|%s|Ships|%s|", principalID(liveHarbormaster), liveTabAnvil, shipPatientHeronID),
		fmt.Sprintf("%s|%s|Ships||%s", principalID(liveHarbormaster), liveTabAnvil, anvil),
	}
	slices.Sort(want)
	if got := subscriptionNames(fake.Subscriptions()); !slices.Equal(got, want) {
		t.Errorf("subscriptions after the renewal = %v, want %v", got, want)
	}
}

// decodeJSON decodes a response body into v.
func decodeJSON(body []byte, v any) error {
	if err := json.Unmarshal(body, v); err != nil {
		return errors.Wrapf(err, "json.Unmarshal(%s)", body)
	}

	return nil
}

// The derived resources' cast: who watches a board, and who writes the tables behind it.
const (
	liveDispatcher    accesstypes.User = "live-dispatcher"
	liveQuartermaster accesstypes.User = "live-quartermaster"
	liveAnalyst       accesstypes.User = "live-analyst"
	liveBooking       accesstypes.User = "live-booking"
	liveTabBoards                      = "tab-boards-1"
)

// TestLivePages_derivedResources plays the publish through the virtual and computed
// resources: a mission's write reaches the mission board, whose rows are Missions' rows
// (@rowsOf), under the mission's own key and as the sector's list, exactly as Missions'
// own subscribers are reached; a client's write, a table the board reads otherwise and a
// global one, reaches every board subscriber in every sector as one resource document;
// a ship's write reaches the hazard board, a computed resource that declares Ships with
// @changedBy, in its sector alone; and the expense manifests, a computed resource that
// declares nothing although its body reads Missions, hear nothing at all.
func TestLivePages_derivedResources(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	fake := live.NewFake()
	h := newTestAppWithLive(db, grants{
		accesstypes.List:   {"MissionBoards", "ExpenseManifests", "SectorHazardBoards"},
		accesstypes.Read:   {"MissionBoards"},
		accesstypes.Update: slices.Concat(withFields("Missions", "brief"), withFields("Clients", "insured"), withFields("Ships", "cargoBays")),
	}, fake)

	// The dispatcher's mission board at Anvil with the Hauler open, the pilot's board and
	// hazard board at Bastion, the quartermaster's expense manifests at Anvil, and the
	// analyst's hazard board at Anvil.
	subscribe := []struct {
		user   accesstypes.User
		tab    string
		target string
	}{
		{liveDispatcher, liveTabBoards, versioned(sectorPath(anvil, "mission-boards"), liveSeed)},
		{liveDispatcher, liveTabBoards, versioned(sectorPath(anvil, "mission-boards/"+missionHaulerID), liveSeed)},
		{liveBastionPilot, liveTabBastion, sectorPath(bastion, "mission-boards")},
		{liveBastionPilot, liveTabBastion, sectorPath(bastion, "sector-hazard-boards")},
		{liveQuartermaster, liveTabBoards, sectorPath(anvil, "expense-manifests")},
		{liveAnalyst, liveTabBoards, sectorPath(anvil, "sector-hazard-boards")},
	}
	for _, s := range subscribe {
		rr := doRequestRecordedWithHeaders(t, h, s.user, http.MethodGet, s.target, "", liveHeaders(s.tab))
		assertStatus(t, rr.Code, http.StatusOK, rr.Body.Bytes())
	}
	wantSubscriptions := []string{
		fmt.Sprintf("%s|%s|MissionBoards||%s", principalID(liveDispatcher), liveTabBoards, anvil),
		fmt.Sprintf("%s|%s|MissionBoards|%s|", principalID(liveDispatcher), liveTabBoards, missionHaulerID),
		fmt.Sprintf("%s|%s|MissionBoards||%s", principalID(liveBastionPilot), liveTabBastion, bastion),
		fmt.Sprintf("%s|%s|SectorHazardBoards||%s", principalID(liveBastionPilot), liveTabBastion, bastion),
		fmt.Sprintf("%s|%s|ExpenseManifests||%s", principalID(liveQuartermaster), liveTabBoards, anvil),
		fmt.Sprintf("%s|%s|SectorHazardBoards||%s", principalID(liveAnalyst), liveTabBoards, anvil),
	}
	slices.Sort(wantSubscriptions)
	if got := subscriptionNames(fake.Subscriptions()); !slices.Equal(got, wantSubscriptions) {
		t.Fatalf("subscriptions after the live requests = %v, want %v", got, wantSubscriptions)
	}

	steps := []struct {
		name   string
		target string
		body   string
		want   map[accesstypes.User][]string
	}{
		{
			name:   "the Hauler's brief is rewritten: the board's row and the sector's list, as Missions' own; the manifests, undeclared, hear nothing",
			target: "/console/api/resources",
			body:   fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"brief":"Reroute through the Anvil lanes."}}]`, opPath(anvil, "missions/"+missionHaulerID)),
			want: map[accesstypes.User][]string{
				liveDispatcher: {
					"list|MissionBoards||" + anvil + "|false",
					"row|MissionBoards|" + missionHaulerID + "||false",
				},
				liveBastionPilot:  {},
				liveQuartermaster: {},
				liveAnalyst:       {},
			},
		},
		{
			name:   "a client is insured: Clients is global, so every board in every sector is told once, wholesale",
			target: "/console/api/clients",
			body:   fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"insured":true}}]`, clientMeridianID),
			want: map[accesstypes.User][]string{
				liveDispatcher: {
					"list|MissionBoards||" + anvil + "|false",
					"resource|MissionBoards|||false",
					"row|MissionBoards|" + missionHaulerID + "||false",
				},
				liveBastionPilot:  {"resource|MissionBoards|||false"},
				liveQuartermaster: {},
				liveAnalyst:       {},
			},
		},
		{
			name:   "a ship's bays change: the hazard board, which declares Ships, is told in Anvil alone",
			target: "/console/api/resources",
			body:   fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"cargoBays":[12,8]}}]`, opPath(anvil, "ships/"+shipKingfisherID)),
			want: map[accesstypes.User][]string{
				liveDispatcher: {
					"list|MissionBoards||" + anvil + "|false",
					"resource|MissionBoards|||false",
					"row|MissionBoards|" + missionHaulerID + "||false",
				},
				liveBastionPilot:  {"resource|MissionBoards|||false"},
				liveQuartermaster: {},
				liveAnalyst:       {"resource|SectorHazardBoards|||false"},
			},
		},
	}
	for _, step := range steps {
		status, body := doRequestAs(t, h, liveBooking, http.MethodPatch, step.target, step.body)
		assertStatus(t, status, http.StatusOK, body)
		for user, want := range step.want {
			if got := changeNames(fake.Changes(principalID(user))); !slices.Equal(got, want) {
				t.Errorf("%s: %s's change set = %v, want %v", step.name, user, got, want)
			}
		}
	}
	if got := changeNames(fake.Changes(principalID(liveBooking))); len(got) != 0 {
		t.Errorf("the writer subscribed to nothing and receives nothing; change set = %v", got)
	}
}
