// Demonstrates: computed.keyless, list.keyless.
package integration

// This suite pins the key-less list. StandingOrders is a @computed struct with no
// @primarykey, so it is a whole read-only list: a bare GET serves every line the
// filter admits in one response, in the book's own order (section by section, which
// is neither alphabetical nor keyed), a requested sort orders it, limit=all is the
// explicit spelling of the same shape, and count=true answers in Total-Count. It never
// pages: a numeric limit or a cursor is refused with a 400 naming the resource, the
// missing key, and @primarykey as the way to page, and no page ever carries a Link. It
// has no read route, so a line cannot be read by key, and its descriptor lists the one
// operation with an empty key tuple, which is what the browser client reads to send
// no limit and no cursor.

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource"
)

// bookOrder is the sequence the standing orders keep: the book's own, section by section.
var bookOrder = []string{"General", "General", "Flight", "Flight", "Hangar", "Salvage"}

func TestKeylessList_servedWhole(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	tests := []struct {
		name         string
		target       string
		wantStatus   int
		wantMessage  string
		wantSections []string
		wantTotal    string
	}{
		{
			name:         "a bare GET serves the whole book in its own order, with no order asked and none declared",
			target:       "/api/standing-orders",
			wantStatus:   http.StatusOK,
			wantSections: bookOrder,
		},
		{
			name:         "limit=all is the explicit spelling of the same shape",
			target:       "/api/standing-orders?limit=all",
			wantStatus:   http.StatusOK,
			wantSections: bookOrder,
		},
		{
			name:         "a requested sort orders the whole book",
			target:       "/api/standing-orders?sort=section",
			wantStatus:   http.StatusOK,
			wantSections: []string{"Flight", "Flight", "General", "General", "Hangar", "Salvage"},
		},
		{
			name:         "a filter narrows the whole book and the rest still arrives in one response",
			target:       "/api/standing-orders?filter=section:eq:Flight",
			wantStatus:   http.StatusOK,
			wantSections: []string{"Flight", "Flight"},
		},
		{
			name:         "count=true answers in Total-Count on the whole list",
			target:       "/api/standing-orders?count=true",
			wantStatus:   http.StatusOK,
			wantSections: bookOrder,
			wantTotal:    "6",
		},
		{
			name:        "a numeric limit is refused naming the resource, the missing key, and the way to page",
			target:      "/api/standing-orders?limit=10",
			wantStatus:  http.StatusBadRequest,
			wantMessage: "StandingOrders declares no primary key, so its list is served whole and does not page; drop the limit, or declare @primarykey to page",
		},
		{
			name:        "a sort does not make a page: a limit beside it is refused the same way",
			target:      "/api/standing-orders?sort=section&limit=2",
			wantStatus:  http.StatusBadRequest,
			wantMessage: "StandingOrders declares no primary key, so its list is served whole and does not page; drop the limit",
		},
		{
			name:        "a cursor is refused the same way",
			target:      "/api/standing-orders?cursor=v4.local.anything",
			wantStatus:  http.StatusBadRequest,
			wantMessage: "StandingOrders declares no primary key, so its list is served whole and does not page; drop the cursor, or declare @primarykey to page",
		},
		{
			name:       "a key-less list has no read route",
			target:     "/api/standing-orders/General",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := doRequestRecordedAs(t, h, "yeoman", http.MethodGet, tt.target, "")
			assertStatus(t, rec.Code, tt.wantStatus, rec.Body.Bytes())
			if tt.wantMessage != "" && !strings.Contains(rec.Body.String(), tt.wantMessage) {
				t.Errorf("body = %s, want it to contain %q", rec.Body.String(), tt.wantMessage)
			}
			if tt.wantStatus != http.StatusOK {
				return
			}
			rows := decodeRows(t, rec.Body.Bytes())
			got := make([]string, 0, len(rows))
			for _, row := range rows {
				got = append(got, cell[string](t, row, "section"))
			}
			if !slices.Equal(got, tt.wantSections) {
				t.Errorf("sections in order = %v, want %v", got, tt.wantSections)
			}
			if link := rec.Header().Get(resource.LinkHeader); link != "" {
				t.Errorf("Link = %q, want none: a whole list has no neighboring pages", link)
			}
			if total := rec.Header().Get(resource.TotalCountHeader); total != tt.wantTotal {
				t.Errorf("Total-Count = %q, want %q", total, tt.wantTotal)
			}
		})
	}
}

// TestKeylessList_gate pins that the list is gated on the List grant alone: the orders
// desk holds it, the cadet does not, and there is no Read grant to hold since the
// Collection registers no read for a key-less resource.
func TestKeylessList_gate(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	status, body := doRequestAs(t, h, "cadet", http.MethodGet, "/api/standing-orders", "")
	assertStatus(t, status, http.StatusForbidden, body)
}

// TestKeylessList_page pins the console's page over the key-less list as the library
// builds it: the config names the resource with virtual scroll and asks for nothing the
// page cannot have (no pageSize, no enableRowExpansion, no rowRoute), and the routes
// register it. The page's own spec (standingOrders.config.spec.ts) proves the refusals,
// the one whole-list request, and the rows by position against the generated client.
func TestKeylessList_page(t *testing.T) {
	t.Parallel()

	app := filepath.Join("..", "..", "web", "console", "src", "app")

	tests := []struct {
		name   string
		file   string
		want   string
		absent bool
	}{
		{name: "the page lists the standing orders", file: "configs/standingOrders.config.ts", want: "primaryResource: Resources.StandingOrders,"},
		{name: "with virtual scroll over the whole book", file: "configs/standingOrders.config.ts", want: "enableVirtualScroll: true,"},
		{name: "and asks for no page size, which the page would refuse", file: "configs/standingOrders.config.ts", want: "pageSize:", absent: true},
		{name: "and no row expansion, which the page would refuse", file: "configs/standingOrders.config.ts", want: "enableRowExpansion:", absent: true},
		{name: "and no row route, since a line has no key", file: "configs/standingOrders.config.ts", want: "rowRoute:", absent: true},
		{name: "the routes register the page", file: "app.routes.ts", want: "resourceRoutes(standingOrdersConfig, resourceMeta),"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source, err := os.ReadFile(filepath.Join(app, tt.file))
			if err != nil {
				t.Fatalf("os.ReadFile() error = %v", err)
			}
			if got := strings.Contains(string(source), tt.want); got == tt.absent {
				t.Errorf("%s contains %q = %v, want %v", tt.file, tt.want, got, !tt.absent)
			}
		})
	}
}

// TestKeylessList_metadata pins what the console's generated TypeScript says about the
// key-less list: an empty key tuple, the list operation alone, the generator-wide page
// (the struct may not declare @page), and readDisabled in the field metadata.
func TestKeylessList_metadata(t *testing.T) {
	t.Parallel()

	service := filepath.Join("..", "..", "web", "console", "src", "app", "core", "service")

	tests := []struct {
		name string
		file string
		want string
	}{
		{
			name: "the descriptor lists the one operation over an empty key tuple",
			file: "zz_gen_api.ts",
			want: "route: 'standing-orders',\n      scope: 'global',\n      consolidated: false,\n      keys: [],\n      operations: ['list'],\n      page: { default: 50 },",
		},
		{
			name: "the key type is the empty tuple, which the client's list query type reads to refuse a limit",
			file: "zz_gen_api.ts",
			want: "export type StandingOrdersKey = [];",
		},
		{
			name: "the handle offers list alone",
			file: "zz_gen_api.ts",
			want: "standingOrders: ResourceHandle<StandingOrders, StandingOrdersKey, 'list'>;",
		},
		{
			name: "the field metadata says the read is disabled",
			file: "zz_gen_resources.ts",
			want: "[Resources.StandingOrders]: {\n    route: 'standing-orders',\n    readDisabled: true,",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source, err := os.ReadFile(filepath.Join(service, tt.file))
			if err != nil {
				t.Fatalf("os.ReadFile() error = %v", err)
			}
			if !strings.Contains(string(source), tt.want) {
				t.Errorf("%s lacks %q", tt.file, tt.want)
			}
		})
	}
}
