// Demonstrates: @feature, @feature.field.
package integration

// This suite pins the commendations flag over the seeded world through two instances
// of the application over one database, the real engines provisioned from the shipped
// role files, and one in-memory live service (live.Fake), the shape a deployment has:
// off, the desk's routes answer the router's own 404 on both instances, its arm of the
// consolidated patch answers as an unknown resource, the permission digest leaves the
// desk and the card's field out, and the card's field named in a request is an unknown
// field; the adjutant, who holds FeatureAdministrator, turns the flag on through
// SetFeature on the first instance, which writes the row and its change record,
// broadcasts on the features topic and answers the flag as written, and the second
// instance serves the desk at its next request with no restart; off again, both refuse
// again, and the change table holds both flips. The steps run in order, each named in
// its failure, since every one reads what the ones before it wrote. The walkthrough
// plays the same scenario against the Firestore emulator with a second server process.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/live"
	"github.com/cccteam/ccc/resource/lodestar/app"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	initiator "github.com/cccteam/db-initiator"
	"github.com/go-playground/errors/v5"
)

// The feature suite's cast, the shipped personas: the adjutant holds FeatureAdministrator
// and the desk's role, the pilot the crew's card, the cadet neither flag grant.
const (
	featureAdjutant accesstypes.User = "adjutant"
	featurePilot    accesstypes.User = "pilot"
	featureCadet    accesstypes.User = "cadet"

	pilotPaxID             = "30000000-0000-4000-8000-000000000004" // Pilot Pax: two seeded citations
	commendationKingfisher = "e0000000-0000-4000-8000-000000000001" // Pax, the oldest of his two

	commendationsRoute = consoleAPI + "/commendations"
	featuresRoute      = consoleAPI + "/" + resource.FeaturesRoute
	featureFlagsRoute  = consoleAPI + "/" + resource.FeatureFlagsRoute
	setFeatureRoute    = consoleAPI + "/" + resource.SetFeatureRoute
	pilotCardsRoute    = consoleAPI + "/pilot-cards"
	cardFieldRoute     = pilotCardsRoute + "?columns=userId,commendations"

	// settleWithin bounds how long an instance may take to reread its flags after the
	// signal: the Fake delivers it synchronously, the reread runs on the set's goroutine.
	settleWithin = 5 * time.Second
)

// flagInstance is one running instance of the application in the suite.
type flagInstance struct {
	name string
	h    http.Handler
}

// startInstance builds an App over the database, the real engines and the shared live
// service and starts it, so its FeatureSet follows the features topic as a served
// instance's does.
func startInstance(t *testing.T, name string, db *initiator.SpannerDB, crewEngine, membersEngine access.Controller, svc live.Service) *flagInstance {
	t.Helper()

	a := app.New(&testConfigurer{db: db, access: crewEngine, membersAccess: membersEngine, live: svc})
	if err := a.Start(t.Context()); err != nil {
		t.Fatalf("app.Start() error = %v", err)
	}

	return &flagInstance{name: name, h: withHandWrittenRoutes(a)}
}

// commendationBody is the consolidated patch that files a citation on Pax's record.
func commendationBody(citation string) string {
	return fmt.Sprintf(`[{"op":"add","path":"/commendations","value":{"pilotId":%q,"citation":%q}}]`, pilotPaxID, citation)
}

// flagStep is one request of the scenario and what it must answer.
type flagStep struct {
	name       string
	on         *flagInstance
	user       accesstypes.User
	method     string
	target     string
	body       string
	wantStatus int
	// settle polls until the status matches, for an instance that learns of a flip on
	// its own goroutine.
	settle bool
	check  func(t *testing.T, step string, body []byte)
}

// TestFeatureFlags_commendationsDesk plays the scenario in order over two instances.
func TestFeatureFlags_commendationsDesk(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	// The deploy step's work: every declared flag written into the table, off.
	if err := resource.MigrateFeatures(ctx, resource.NewSpannerClient(db.Client), resources.Features()); err != nil {
		t.Fatalf("resource.MigrateFeatures() error = %v", err)
	}
	declared := resources.Features()
	if len(declared) != 1 || declared[0].Name != resources.Commendations {
		t.Fatalf("resources.Features() = %v, want the one commendations flag", declared)
	}

	// The engines are the shipped role files over this database, provisioned with the
	// personas' memberships, so the adjutant's authority is the FeatureAdministrator role
	// as it ships and nothing scripted.
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

	fake := live.NewFake()
	first := startInstance(t, "the first instance", db, crewEngine, membersEngine, fake)
	second := startInstance(t, "the second instance", db, crewEngine, membersEngine, fake)
	if got := fake.Watching(resource.FeaturesTopic); got != 2 {
		t.Fatalf("instances watching the %s topic = %d, want 2: Start follows the topic", resource.FeaturesTopic, got)
	}

	steps := []flagStep{
		{
			name: "off: the features route lists nothing", on: first, user: featureAdjutant, method: http.MethodGet, target: featuresRoute, wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertEnabled(t, step, body, []string{})
			},
		},
		{
			name: "off: the flags list carries the desk's flag, off, with its description and the migration's stamp", on: first, user: featureAdjutant, method: http.MethodGet, target: featureFlagsRoute, wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				rows := decodeRows(t, body)
				if len(rows) != 1 {
					t.Fatalf("%s: flags = %d, want 1: %s", step, len(rows), body)
				}
				assertFlag(t, step, rows[0], string(resources.Commendations), false, declared[0].Description)
				if by := cell[string](t, rows[0], "updatedBy"); !strings.Contains(by, "MigrateFeatures") {
					t.Errorf("%s: updatedBy = %q, want the migration's process event", step, by)
				}
			},
		},
		{name: "off: the desk's list answers the router's own not found on the first instance", on: first, user: featureAdjutant, method: http.MethodGet, target: commendationsRoute, wantStatus: http.StatusNotFound, check: assertRouterNotFound},
		{name: "off: and on the second", on: second, user: featureAdjutant, method: http.MethodGet, target: commendationsRoute, wantStatus: http.StatusNotFound, check: assertRouterNotFound},
		{name: "off: a seeded citation's read is not found either", on: first, user: featureAdjutant, method: http.MethodGet, target: commendationsRoute + "/" + commendationKingfisher, wantStatus: http.StatusNotFound, check: assertRouterNotFound},
		{name: "off: the desk's arm of the consolidated patch answers as an unknown resource", on: first, user: featureAdjutant, method: http.MethodPatch, target: consoleAPI + "/resources", body: commendationBody("Filed while the desk is dark"), wantStatus: http.StatusBadRequest},
		{
			name: "off: the digest leaves the desk and the card's field out and carries the flags", on: first, user: featureAdjutant, method: http.MethodGet, target: consoleAPI + "/permission-digest", wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertDigest(t, step, body, []string{"FeatureFlags", "SetFeature", "PilotCards"}, []string{"Commendations", "Commendations.citation", "PilotCards.commendations"})
			},
		},
		{name: "off: the card's field named in a request is an unknown field", on: first, user: featurePilot, method: http.MethodGet, target: cardFieldRoute, wantStatus: http.StatusBadRequest},
		{
			name: "off: the card leaves the field out", on: first, user: featurePilot, method: http.MethodGet, target: pilotCardsRoute, wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				rows := decodeRows(t, body)
				if len(rows) != 1 {
					t.Fatalf("%s: cards = %d, want 1: %s", step, len(rows), body)
				}
				if _, ok := rows[0]["commendations"]; ok {
					t.Errorf("%s: the card carries commendations while the flag is off: %v", step, rows[0])
				}
			},
		},
		{name: "the cadet holds no Execute on SetFeature: refused at the method's own gate", on: first, user: featureCadet, method: http.MethodPost, target: setFeatureRoute, body: `{"name":"commendations","enabled":true}`, wantStatus: http.StatusForbidden},
		{name: "the cadet holds no List on the flags either", on: first, user: featureCadet, method: http.MethodGet, target: featureFlagsRoute, wantStatus: http.StatusForbidden},
		{name: "a flag the package does not declare is not found", on: first, user: featureAdjutant, method: http.MethodPost, target: setFeatureRoute, body: `{"name":"nonesuch","enabled":true}`, wantStatus: http.StatusNotFound},
		{
			name: "the adjutant turns the desk on: the flag as written, and one broadcast on the features topic", on: first, user: featureAdjutant, method: http.MethodPost, target: setFeatureRoute, body: `{"name":"commendations","enabled":true}`, wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertSetFeature(t, step, body, true)
				if got := fake.Broadcasts(); !slices.Equal(got, []string{resource.FeaturesTopic}) {
					t.Errorf("%s: broadcasts = %v, want [%s]", step, got, resource.FeaturesTopic)
				}
			},
		},
		{
			name: "on: the features route lists it", on: first, user: featureAdjutant, method: http.MethodGet, target: featuresRoute, wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertEnabled(t, step, body, []string{string(resources.Commendations)})
			},
		},
		{
			name: "on: the flag reads back on, stamped by the adjutant", on: first, user: featureAdjutant, method: http.MethodGet, target: featureFlagsRoute + "/" + string(resources.Commendations), wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				row := decodeRow(t, body)
				assertFlag(t, step, row, string(resources.Commendations), true, declared[0].Description)
				if by := cell[string](t, row, "updatedBy"); !strings.Contains(by, string(featureAdjutant)) {
					t.Errorf("%s: updatedBy = %q, want the adjutant's session", step, by)
				}
			},
		},
		{
			name: "on: the desk lists the seeded citations, newest first", on: first, user: featureAdjutant, method: http.MethodGet, target: commendationsRoute, wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertCitations(t, step, body, []string{
					"Talked a drifting hauler crew through a cold restart",
					"Brought the Kingfisher home on one engine",
					"Held formation through the debris belt with a cracked canopy",
				})
			},
		},
		{
			name: "on: the desk lists one pilot's citations off the index", on: first, user: featureAdjutant, method: http.MethodGet, target: commendationsRoute + "?filter=pilotId:eq:" + pilotPaxID, wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertCitations(t, step, body, []string{"Talked a drifting hauler crew through a cold restart", "Brought the Kingfisher home on one engine"})
			},
		},
		{
			name: "on: the digest carries the desk and the card's field", on: first, user: featureAdjutant, method: http.MethodGet, target: consoleAPI + "/permission-digest", wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertDigest(t, step, body, []string{"Commendations", "Commendations.citation", "PilotCards.commendations"}, nil)
			},
		},
		{
			name: "on: Pax's card counts his two citations", on: first, user: featurePilot, method: http.MethodGet, target: cardFieldRoute, wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertCardCount(t, step, body, 2)
			},
		},
		{name: "on: the adjutant files a citation through the consolidated patch", on: first, user: featureAdjutant, method: http.MethodPatch, target: consoleAPI + "/resources", body: commendationBody("Flew the Hesper's wounded home through the belt"), wantStatus: http.StatusOK},
		{
			name: "on: the card counts three", on: first, user: featurePilot, method: http.MethodGet, target: cardFieldRoute, wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertCardCount(t, step, body, 3)
			},
		},
		{
			name: "on: the second instance serves the desk at its next request, with no restart", on: second, user: featureAdjutant, method: http.MethodGet, target: commendationsRoute, wantStatus: http.StatusOK, settle: true,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				if got := len(decodeRows(t, body)); got != 4 {
					t.Errorf("%s: citations = %d, want 4", step, got)
				}
			},
		},
		{
			name: "the adjutant turns the desk off again", on: first, user: featureAdjutant, method: http.MethodPost, target: setFeatureRoute, body: `{"name":"commendations","enabled":false}`, wantStatus: http.StatusOK,
			check: func(t *testing.T, step string, body []byte) {
				t.Helper()
				assertSetFeature(t, step, body, false)
			},
		},
		{name: "off again: the first instance refuses", on: first, user: featureAdjutant, method: http.MethodGet, target: commendationsRoute, wantStatus: http.StatusNotFound, check: assertRouterNotFound},
		{name: "off again: the second instance refuses at its next request", on: second, user: featureAdjutant, method: http.MethodGet, target: commendationsRoute, wantStatus: http.StatusNotFound, settle: true, check: assertRouterNotFound},
		{name: "off again: the card's field is unknown again", on: first, user: featurePilot, method: http.MethodGet, target: cardFieldRoute, wantStatus: http.StatusBadRequest},
	}
	for i := range steps {
		runFlagStep(t, &steps[i])
	}

	// The change table is the audit: one row per flip, in order, each naming the
	// adjutant's session.
	flips := readFlips(ctx, t, db, string(resources.Commendations))
	if len(flips) != 2 || !strings.HasPrefix(flips[0], "true by ") || !strings.HasPrefix(flips[1], "false by ") ||
		!strings.Contains(flips[0], string(featureAdjutant)) || !strings.Contains(flips[1], string(featureAdjutant)) {
		t.Errorf("the flag's change history = %v, want the flip on and the flip off, each by the adjutant", flips)
	}
}

// runFlagStep issues one step's request, settling first where the step says so, and
// fails the suite in the step's name: every later step reads what this one wrote.
func runFlagStep(t *testing.T, step *flagStep) {
	t.Helper()

	status, body := doRequestAs(t, step.on.h, step.user, step.method, step.target, step.body)
	if step.settle {
		status, body = settleOn(t, step.on, step.user, step.method, step.target, step.wantStatus)
	}
	if status != step.wantStatus {
		t.Fatalf("%s (%s): status = %d, want %d: %s", step.name, step.on.name, status, step.wantStatus, body)
	}
	if step.check != nil {
		step.check(t, step.name, body)
	}
}

// settleOn asks the instance again until it answers the wanted status or the bound
// passes, returning the last answer: the instance rereads its flags on its own goroutine
// once the topic's signal lands.
func settleOn(t *testing.T, on *flagInstance, user accesstypes.User, method, target string, wantStatus int) (status int, body []byte) {
	t.Helper()

	deadline := time.Now().Add(settleWithin)
	for {
		status, body = doRequestAs(t, on.h, user, method, target, "")
		if status == wantStatus || time.Now().After(deadline) {
			return status, body
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// assertRouterNotFound pins the body a gated-off route answers: the router's own.
func assertRouterNotFound(t *testing.T, step string, body []byte) {
	t.Helper()

	if string(body) != "Not Found\n" {
		t.Errorf("%s: body = %q, want the router's not-found body", step, body)
	}
}

// assertEnabled pins the features route's answer.
func assertEnabled(t *testing.T, step string, body []byte, want []string) {
	t.Helper()

	var resp resource.FeaturesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("%s: decoding the features route: %v: %s", step, err, body)
	}
	if !slices.Equal(resp.Enabled, want) {
		t.Errorf("%s: enabled = %v, want %v", step, resp.Enabled, want)
	}
}

// assertFlag pins one row of the FeatureFlags resource, the dialog's inputs: the name,
// the state, the description, and a stamp within the last day.
func assertFlag(t *testing.T, step string, row map[string]any, name string, enabled bool, description string) {
	t.Helper()

	if got := cell[string](t, row, "name"); got != name {
		t.Errorf("%s: name = %q, want %q", step, got, name)
	}
	if got := cell[bool](t, row, "enabled"); got != enabled {
		t.Errorf("%s: enabled = %v, want %v", step, got, enabled)
	}
	if got := cell[string](t, row, "description"); got != description {
		t.Errorf("%s: description = %q, want %q", step, got, description)
	}
	assertRecent(t, step, cell[string](t, row, "updatedAt"))
}

// assertSetFeature pins SetFeature's answer: the flag as written, stamped now.
func assertSetFeature(t *testing.T, step string, body []byte, enabled bool) {
	t.Helper()

	var result resource.SetFeatureResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("%s: decoding SetFeature's answer: %v: %s", step, err, body)
	}
	if result.Name != string(resources.Commendations) || result.Enabled != enabled {
		t.Errorf("%s: answer = %+v, want %s %v", step, result, resources.Commendations, enabled)
	}
	assertRecent(t, step, result.UpdatedAt.Format(time.RFC3339Nano))
}

// assertRecent pins a timestamp to the last day.
func assertRecent(t *testing.T, step, stamp string) {
	t.Helper()

	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		t.Fatalf("%s: time.Parse(%q): %v", step, stamp, err)
	}
	if age := time.Since(at); age < 0 || age > 24*time.Hour {
		t.Errorf("%s: updatedAt = %s, want within the last day", step, stamp)
	}
}

// assertDigest pins the digest's keys present and absent.
func assertDigest(t *testing.T, step string, body []byte, present, absent []string) {
	t.Helper()

	digest := decodeRow(t, body)
	for _, key := range present {
		if _, ok := digest[key]; !ok {
			t.Errorf("%s: digest lacks %q", step, key)
		}
	}
	for _, key := range absent {
		if _, ok := digest[key]; ok {
			t.Errorf("%s: digest carries %q", step, key)
		}
	}
}

// assertCitations pins the desk's list by citation, in order.
func assertCitations(t *testing.T, step string, body []byte, want []string) {
	t.Helper()

	rows := decodeRows(t, body)
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, cell[string](t, row, "citation"))
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s: citations = %v, want %v", step, got, want)
	}
}

// assertCardCount pins the one card's commendations.
func assertCardCount(t *testing.T, step string, body []byte, want float64) {
	t.Helper()

	rows := decodeRows(t, body)
	if len(rows) != 1 {
		t.Fatalf("%s: cards = %d, want 1: %s", step, len(rows), body)
	}
	if got := cell[float64](t, rows[0], "commendations"); got != want {
		t.Errorf("%s: commendations = %v, want %v", step, got, want)
	}
}

// readFlips reads the flag's change records in order, each as "<enabled> by <who>".
func readFlips(ctx context.Context, t *testing.T, db *initiator.SpannerDB, name string) []string {
	t.Helper()

	var flips []string
	err := db.Single().Query(ctx, spanner.Statement{
		SQL:    "SELECT Enabled, ChangedBy FROM FeatureFlagChanges WHERE Name = @name ORDER BY ChangedAt",
		Params: map[string]any{"name": name},
	}).Do(func(row *spanner.Row) error {
		var enabled bool
		var by string
		if err := row.Columns(&enabled, &by); err != nil {
			return errors.Wrap(err, "spanner.Row.Columns()")
		}
		flips = append(flips, fmt.Sprintf("%t by %s", enabled, by))

		return nil
	})
	if err != nil {
		t.Fatalf("reading FeatureFlagChanges: %v", err)
	}

	return flips
}
