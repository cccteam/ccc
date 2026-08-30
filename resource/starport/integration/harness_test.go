package integration

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/starport/app"
	"github.com/cccteam/ccc/resource/starport/pkg/router"
	"github.com/cccteam/ccc/resource/starport/pkg/rpc"
	"github.com/cccteam/ccc/resource/starport/pkg/stations"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-playground/validator/v10"
)

// Seeded row identifiers, matching the fixture data in testdata/seed. The values are
// stable so tests can address rows directly.
const (
	bayAlphaID       = "5f2d1c3b-9a8e-4d7f-8b6a-1c2d3e4f5a6b"
	shipVantaID      = "0b9e8d7c-6f5a-4b3c-9d2e-1f0a9b8c7d6e"
	shipComostID     = "7a6b5c4d-3e2f-4a1b-8c9d-0e1f2a3b4c5d"
	crewIlyanID      = "3c2b1a09-8d7e-4f6a-9b5c-4d3e2f1a0b9c"
	crateCoolant40ID = "9d8c7b6a-5f4e-4d3c-8b2a-1f0e9d8c7b6a"
	crateRationsID   = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5e"
	crateCoolant12ID = "6f5e4d3c-2b1a-4098-8765-4321fedcba98"
)

// grants is a static permission table: the set of resources granted for each permission.
type grants map[accesstypes.Permission][]accesstypes.Resource

// staticAccess scripts access.Controller's permission checks over a grants table.
// Every Controller method the pipeline does not consume panics through the embedded
// nil interface, keeping the fake honest about what the pipeline actually draws on.
type staticAccess struct {
	access.Controller
	g grants
}

func (s *staticAccess) CheckUserResources(_ context.Context, _ accesstypes.Environment, _ accesstypes.User, _ accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	decisions := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		if slices.Contains(s.g[perm], res) {
			decisions[res] = accesstypes.Granted()
		} else {
			decisions[res] = accesstypes.Denied()
		}
	}

	return decisions, nil
}

// testConfigurer implements app.Configurer over the test dependencies, so the App is
// assembled through the same seam main assembles it through. The permission engine
// and tenancy source are injectable: each suite scripts them through the same two
// Configurer methods production wires to the real engine and station list. Session is
// nil: the App owns no router, these suites compose the API surface through
// router.NewTestRouter, and nothing on that path touches the session.
type testConfigurer struct {
	db           *initiator.SpannerDB
	access       access.Controller
	domainExists func(ctx context.Context, domain accesstypes.Domain) (bool, error)
}

func (c *testConfigurer) ResourceClient() resource.Client {
	return resource.NewSpannerClient(c.db.Client)
}

func (c *testConfigurer) RPCClient() *rpc.Client {
	return rpc.NewClient()
}

func (c *testConfigurer) Access() access.Controller {
	return c.access
}

func (c *testConfigurer) Session() *session.PasswordAuth[session.NoCustomData, session.NoCustomData] {
	return nil
}

func (c *testConfigurer) Validator() *validator.Validate {
	return validator.New()
}

func (c *testConfigurer) GuiDist() string { return "" }

// AutomationAPIKey is unused by these suites: they drive the bare test router, which
// carries no outlet middleware.
func (c *testConfigurer) AutomationAPIKey() string { return "integration-automation-key" }

func (c *testConfigurer) DomainExists(ctx context.Context, domain accesstypes.Domain) (bool, error) {
	return c.domainExists(ctx, domain)
}

// stationsExist is the production tenancy rule: only the demo stations are domains.
func stationsExist(_ context.Context, domain accesstypes.Domain) (bool, error) {
	return stations.Exists(domain), nil
}

// newTestApp builds the application with the given permission table backing every
// request, served through the generated test router.
func newTestApp(db *initiator.SpannerDB, g grants) http.Handler {
	return router.NewTestRouter(app.New(&testConfigurer{
		db:           db,
		access:       &staticAccess{g: g},
		domainExists: stationsExist,
	}))
}

// doRequest performs a request against the app and returns the status code and body.
func doRequest(t *testing.T, h http.Handler, method, target, body string) (statusCode int, respBody []byte) {
	t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}

	// Generated mutation handlers derive their change-event source from the session, which
	// production apps establish via session middleware. The harness injects a synthetic one.
	sessionID, err := ccc.NewUUID()
	if err != nil {
		t.Fatalf("ccc.NewUUID: %v", err)
	}
	ctx := context.WithValue(t.Context(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
		SessionInfo: &sessioninfo.SessionInfo{
			ID:       sessionID,
			Username: "integration-test-user",
		},
	})

	req := httptest.NewRequestWithContext(ctx, method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	return rr.Code, rr.Body.Bytes()
}

// decodeRows decodes a list response body into rows.
func decodeRows(t *testing.T, body []byte) []map[string]any {
	t.Helper()

	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decodeRows: %v: %s", err, body)
	}

	return rows
}

// decodeRow decodes a single-resource response body into a row.
func decodeRow(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var row map[string]any
	if err := json.Unmarshal(body, &row); err != nil {
		t.Fatalf("decodeRow: %v: %s", err, body)
	}

	return row
}

// assertKeys asserts that a row contains exactly the wanted JSON keys.
func assertKeys(t *testing.T, row map[string]any, wantKeys []string) {
	t.Helper()

	got := slices.Sorted(maps.Keys(row))
	want := slices.Sorted(slices.Values(wantKeys))
	if !slices.Equal(got, want) {
		t.Errorf("response keys = %v, want %v", got, want)
	}
}

// assertStatus asserts the response status code, printing the body on mismatch.
func assertStatus(t *testing.T, gotStatus, wantStatus int, body []byte) {
	t.Helper()

	if gotStatus != wantStatus {
		t.Fatalf("status = %d, want %d: %s", gotStatus, wantStatus, body)
	}
}

// readColumn reads one column of one row directly from the database.
func readColumn[T any](ctx context.Context, t *testing.T, db *initiator.SpannerDB, table string, key spanner.Key, column string) T {
	t.Helper()

	row, err := db.Single().ReadRow(ctx, table, key, []string{column})
	if err != nil {
		t.Fatalf("ReadRow(%s): %v", table, err)
	}

	var value T
	if err := row.Columns(&value); err != nil {
		t.Fatalf("row.Columns(%s.%s): %v", table, column, err)
	}

	return value
}
