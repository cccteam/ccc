package integration

// This suite pins the manual-resource seam end to end: ShipsLogEntries has no
// generated handler — @manualAddResource(List, domain) registers its List permission
// in the domain scope, and app.ShipsLogEntries is the hand-written surface that checks
// it, mounted here exactly as the production router mounts it (behind the generated
// DomainGuard).

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/app"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/httpio"
	"github.com/go-chi/chi/v5"
)

// mountLog mounts the generated routes and the hand-written log route on one handler.
func mountLog(a *app.App) http.Handler {
	r := chi.NewRouter()
	r.Use(httpio.WithParams)
	r.Get("/api/sectors/{sectorID}/ships-log-entries", a.DomainGuard()(a.ShipsLogEntries()))
	r.Mount("/", router.NewTestRouter(a))

	return r
}

func TestManualResourceShipsLog(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	logGrants := grants{
		accesstypes.List:    {resources.ShipsLogEntries},
		accesstypes.Execute: {"HailShip"},
	}

	// A tracked mutation through the generated surface produces the event the
	// hand-written surface serves.
	newApp := func(g grants) http.Handler {
		controller := &staticAccess{g: g}
		return mountLog(app.New(&testConfigurer{db: db, access: controller, domainVisible: domainVisibleVia(controller)}))
	}
	status, body := doRequestAs(t, newApp(logGrants), "log-actor", http.MethodPost, sectorPath(anvil, "hail-ship"), fmt.Sprintf(`{"shipId":%q}`, shipKingfisherID))
	assertStatus(t, status, http.StatusOK, body)

	tests := []struct {
		name       string
		g          grants
		sector     string
		wantStatus int
		check      func(t *testing.T, respBody []byte)
	}{
		{
			name:       "granted List serves the sector's change events newest first",
			g:          logGrants,
			sector:     anvil,
			wantStatus: http.StatusOK,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				rows := decodeRows(t, respBody)
				if len(rows) == 0 {
					t.Fatalf("log = 0 rows, want at least the hail: %s", respBody)
				}
				if got := rows[0]["tableName"]; got != "Ships" {
					t.Errorf("newest event tableName = %v, want Ships", got)
				}
				if src, _ := rows[0]["eventSource"].(string); src == "" {
					t.Errorf("newest event eventSource is empty, want the session-derived source")
				}
			},
		},
		{
			name:       "the hail does not appear in another sector's log",
			g:          logGrants,
			sector:     bastion,
			wantStatus: http.StatusOK,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				if rows := decodeRows(t, respBody); len(rows) != 0 {
					t.Errorf("Bastion log = %d rows, want none", len(rows))
				}
			},
		},
		{
			name:       "without the grant the surface is forbidden",
			g:          grants{accesstypes.Execute: {"HailShip"}},
			sector:     anvil,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "with no grants the sector is concealed",
			g:          grants{},
			sector:     anvil,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequest(t, newApp(tt.g), http.MethodGet, sectorPath(tt.sector, "ships-log-entries"), "")
			assertStatus(t, status, tt.wantStatus, body)
			if tt.check != nil {
				tt.check(t, body)
			}
		})
	}
}

// TestManualResourceBootstrapParity runs the log surface against the REAL engine
// provisioned with the shipped demo config: the archivist and the marshal reach the
// log at Anvil, the cadet holds no log grant and is refused, and the marshal's Anvil
// role does not open Bastion's log.
func TestManualResourceBootstrapParity(t *testing.T) {
	t.Parallel()

	db, _, controller := sharedWorld(t)
	h := mountLog(newApp(db, controller))

	tests := []struct {
		name       string
		user       accesstypes.User
		sector     string
		wantStatus int
	}{
		{name: "the archivist holds the domain-scoped log grant everywhere", user: "archivist", sector: cinder, wantStatus: http.StatusOK},
		{name: "the marshal reads Anvil's log", user: "marshal", sector: anvil, wantStatus: http.StatusOK},
		{name: "the cadet holds no log grant", user: "cadet", sector: anvil, wantStatus: http.StatusForbidden},
		{name: "the marshal's role stops at Anvil's border", user: "marshal", sector: bastion, wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, sectorPath(tt.sector, "ships-log-entries"), "")
			assertStatus(t, status, tt.wantStatus, body)
		})
	}
}

var _ = initiator.SpannerDB{}
