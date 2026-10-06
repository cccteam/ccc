package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/resource/lodestar/pkg/rpc"
	"github.com/cccteam/ccc/resource/scheduled"
	"google.golang.org/grpc/codes"
)

// TestScheduled_pruneDroidReports calls the scheduled route on the served stack the way
// Cloud Scheduler does: POST /_scheduled/prune-droid-reports carrying a bearer token
// Google signed (here, the fake standing in for Google's keys) for the route's URL, with
// the invoker identity as its verified email. No session is signed in for any call: the
// route sits behind the scheduler's check alone. A call without a token, with a token of
// another identity, or with a token minted for another route is refused 401 and deletes
// nothing; the scheduler's own call deletes the reading recorded before the retention
// window, leaves the one inside it, and answers how many readings went and the cutoff.
//
// Demonstrates: @schedule.
func TestScheduled_pruneDroidReports(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)

	const (
		staleID = "c0000000-0000-4000-8000-0000000000a1"
		freshID = "c0000000-0000-4000-8000-0000000000a2"
		route   = scheduled.Prefix + "/prune-droid-reports"
	)
	// Two readings on the Kingfisher: one recorded a day past the window, one an hour ago.
	now := time.Now().UTC()
	columns := []string{"Id", "SectorId", "ShipId", "Subsystem", "Reading", "RecordedAt"}
	if _, err := s.db.Apply(ctx, []*spanner.Mutation{
		spanner.Insert("DroidReports", columns, []any{staleID, anvil, shipKingfisherID, "hull", 0.31, now.Add(-rpc.DroidReportRetention - 24*time.Hour)}),
		spanner.Insert("DroidReports", columns, []any{freshID, anvil, shipKingfisherID, "hull", 0.42, now.Add(-time.Hour)}),
	}); err != nil {
		t.Fatalf("spanner.Client.Apply() error = %v", err)
	}

	// The URL Cloud Scheduler calls, and so the audience its token carries: the route on
	// the host the call names.
	audience := "https://" + s.server.Listener.Addr().String() + route

	// The steps run in order, not as parallel subtests: the refusals leave the stale
	// reading in place for the scheduler's call, the last step, to prune.
	steps := []struct {
		name       string
		token      string
		wantStatus int
		// wantStale says whether the stale reading is still there after the call.
		wantStale bool
	}{
		{name: "a call without a token is refused", wantStatus: http.StatusUnauthorized, wantStale: true},
		{name: "a token of another identity is refused", token: s.scheduler.Mint(audience, "someone@lodestar.iam.gserviceaccount.com"), wantStatus: http.StatusUnauthorized, wantStale: true},
		{name: "a token minted for another route is refused", token: s.scheduler.Mint("https://"+s.server.Listener.Addr().String()+scheduled.Prefix+"/another-route", schedulerInvoker), wantStatus: http.StatusUnauthorized, wantStale: true},
		{name: "the scheduler's call prunes the stale reading", token: s.scheduler.Mint(audience, schedulerInvoker), wantStatus: http.StatusOK},
	}
	for _, step := range steps {
		status, body := postScheduled(t, s, route, step.token)
		if status != step.wantStatus {
			t.Fatalf("%s: POST %s: status = %d, want %d: %s", step.name, route, status, step.wantStatus, body)
		}
		if step.wantStatus == http.StatusOK {
			var pruned struct {
				Deleted int64     `json:"deleted"`
				Before  time.Time `json:"before"`
			}
			if err := json.Unmarshal(body, &pruned); err != nil {
				t.Fatalf("%s: the answer is not the prune's outcome: %v: %s", step.name, err, body)
			}
			// The seeded readings fall out of the window as the calendar moves, so the
			// count is at least the stale reading's.
			if pruned.Deleted < 1 || !pruned.Before.Before(now.Add(-rpc.DroidReportRetention+time.Hour)) {
				t.Errorf("%s: answer = %+v, want at least one reading deleted before %s", step.name, pruned, now.Add(-rpc.DroidReportRetention))
			}
		}
		if got := readingExists(t, s, staleID); got != step.wantStale {
			t.Errorf("%s: the stale reading exists = %v, want %v", step.name, got, step.wantStale)
		}
		if !readingExists(t, s, freshID) {
			t.Errorf("%s: the reading inside the window was deleted", step.name)
		}
	}
}

// postScheduled calls the scheduled route on the served stack as Cloud Scheduler does,
// carrying the token as a bearer token when there is one, and answers the status and body.
func postScheduled(t *testing.T, s *served, route, token string) (status int, body []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.server.URL+route, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.server.Client().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", route, err)
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	return resp.StatusCode, body
}

// readingExists reports whether the droid reading is in the served stack's database.
func readingExists(t *testing.T, s *served, id string) bool {
	t.Helper()

	_, err := s.db.Single().ReadRow(t.Context(), "DroidReports", spanner.Key{id}, []string{"Id"})
	switch {
	case err == nil:
		return true
	case spanner.ErrCode(err) == codes.NotFound:
		return false
	default:
		t.Fatalf("ReadRow(DroidReports, %s): %v", id, err)

		return false
	}
}
