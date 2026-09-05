package integration

// This suite pins structural permission enforcement over the demo world: field
// permissions are fail-closed (a resource-only grant exposes nothing beyond the
// primary key), explicitly granted fields shape the response exactly, the domain
// guard 404s unknown sectors before permissions are consulted, outlets stay
// exclusive, and suppressed routes do not exist. Wing is the deliberately minimal
// resource — no vocabulary beyond the mandatory @domain binding.

import (
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

const (
	wingsResource    = accesstypes.Resource("Wings")
	missionsResource = accesstypes.Resource("Missions")
)

func TestStructuralFailClosed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		grants     grants
		target     string
		wantStatus int
		wantRows   int
		wantKeys   []string
	}{
		{
			name:       "list with no grants at all conceals the sector",
			grants:     nil,
			target:     sectorPath(anvil, "wings"),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "list with resource-only grant returns exactly the primary key",
			grants:     grants{accesstypes.List: {wingsResource}},
			target:     sectorPath(anvil, "wings"),
			wantStatus: http.StatusOK,
			wantRows:   1, // Forge Wing only: row tenancy partitions the list
			wantKeys:   []string{"id"},
		},
		{
			name:       "list with field grants returns exactly the granted fields",
			grants:     grants{accesstypes.List: withFields(wingsResource, "name")},
			target:     sectorPath(anvil, "wings"),
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantKeys:   []string{"id", "name"},
		},
		{
			name:       "explicitly requesting an ungranted column is forbidden",
			grants:     grants{accesstypes.List: withFields(wingsResource, "name")},
			target:     sectorPath(anvil, "wings?columns=name,sectorId"),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "read follows the same field shaping",
			grants:     grants{accesstypes.Read: withFields(wingsResource, "name")},
			target:     sectorPath(anvil, "wings/"+wingForgeID),
			wantStatus: http.StatusOK,
			wantKeys:   []string{"id", "name"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newTestApp(db, tt.grants)
			status, body := doRequest(t, h, http.MethodGet, tt.target, "")
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantStatus != http.StatusOK {
				return
			}

			if tt.wantRows > 0 {
				rows := decodeRows(t, body)
				if len(rows) != tt.wantRows {
					t.Fatalf("rows = %d, want %d: %s", len(rows), tt.wantRows, body)
				}
				assertKeys(t, rows[0], tt.wantKeys)

				return
			}
			assertKeys(t, decodeRow(t, body), tt.wantKeys)
		})
	}
}

func TestDomainGuard(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	fullWings := grants{accesstypes.List: withFields(wingsResource, "sectorId", "name")}

	tests := []struct {
		name       string
		grants     grants
		target     string
		wantStatus int
	}{
		{
			name:       "unknown sector is 404",
			grants:     nil,
			target:     sectorPath("nowhere", "wings"),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "known sector without a foothold is indistinguishable from unknown",
			grants:     nil,
			target:     sectorPath(cinder, "wings"),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "known sector with a grant serves",
			grants:     fullWings,
			target:     sectorPath(anvil, "wings"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "global routes carry no domain guard",
			grants:     grants{accesstypes.List: {accesstypes.Resource("Sectors")}},
			target:     "/api/sectors",
			wantStatus: http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newTestApp(db, tt.grants)
			status, body := doRequest(t, h, http.MethodGet, tt.target, "")
			assertStatus(t, status, tt.wantStatus, body)
		})
	}
}

func TestOutletExclusivity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	// DroidReports and IngestDroidReports live on the droids outlet only; the
	// generated test router serves every outlet under its own prefix, so their
	// default-outlet and portal paths must not exist even with every permission
	// granted — and the portal serves only its four members.
	all := grants{
		accesstypes.List:    {accesstypes.Resource("DroidReports"), accesstypes.Resource("Refits"), accesstypes.Resource("Ships"), accesstypes.Resource("Squadrons")},
		accesstypes.Execute: {accesstypes.Resource("IngestDroidReports")},
	}
	h := newTestApp(db, all)

	for _, target := range []string{
		sectorPath(anvil, "droid-reports"),
		sectorPath(anvil, "ingest-droid-reports"),
		"/portal/sectors/" + anvil + "/droid-reports",
		"/portal/sectors/" + anvil + "/refits",
		"/portal/sectors/" + anvil + "/ships",
		"/portal/sectors/" + anvil + "/squadrons",
		"/droids/permission-digest",
		"/droids/user-domains",
	} {
		status, body := doRequest(t, h, http.MethodGet, target, "")
		if status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 404/405: %s", target, status, body)
		}
	}
}

func TestSuppressedReadHandler(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	// ServiceLedger suppresses its read handler: the list route exists (global,
	// computed) while the read route does not.
	ledger := grants{
		accesstypes.List: {accesstypes.Resource("ServiceLedgers")},
		accesstypes.Read: {accesstypes.Resource("ServiceLedgers")},
	}
	h := newTestApp(db, ledger)

	status, body := doRequest(t, h, http.MethodGet, "/api/service-ledgers", "")
	assertStatus(t, status, http.StatusOK, body)

	status, body = doRequest(t, h, http.MethodGet, "/api/service-ledgers/"+anvil, "")
	if status != http.StatusNotFound {
		t.Errorf("suppressed read route: status = %d, want 404: %s", status, body)
	}
}
