package integration

// This suite pins the server side of the library's config-driven list: the requests the
// Missions console page makes over the mission board, exactly as the list component
// sends them. A first page with no limit and no sort answers at the view's declared
// page size in its declared order with the count; Previous and Next are the Link
// relations; a header click is a sort parameter; a filter on a column the metadata marks
// filterable is served, one on a column it does not is refused naming the column; a
// companion-only (allow_filter) column filters only beside an indexed one; a page size
// over the declared maximum is refused naming it. The generated metadata carries the
// filterable flag the browser draws its controls from, and the descriptor the order and
// page sizes the store pages by — a view's, declared with @order and @page, as a table's.
//
// Demonstrates: list.server-paged, metadata.filterable.

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource"
)

// boardColumns is the request the Missions page composes: its configured columns and the view's key.
const boardColumns = "columns=title,clientName,squadronName,statusId,deadline,daysLeft,id"

// TestListPage_serverPaged walks the mission board as the library list does, as the
// marshal, who lists everything at Anvil.
func TestListPage_serverPaged(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	// The first page as the list asks for it: the columns, the count, no limit and no
	// sort, so the view's @page default and @order apply.
	first := doRequestRecordedAs(t, h, "marshal", http.MethodGet, sectorPath(anvil, "mission-boards?"+boardColumns+"&count=true"), "")
	assertStatus(t, first.Code, http.StatusOK, first.Body.Bytes())
	rows := decodeRows(t, first.Body.Bytes())
	if len(rows) != 25 {
		t.Fatalf("first page rows = %d, want the view's declared default of 25", len(rows))
	}
	if got := first.Header().Get(resource.TotalCountHeader); got != "30" {
		t.Errorf("Total-Count = %q, want 30, every Anvil mission", got)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1]["deadline"].(string) > rows[i]["deadline"].(string) {
			t.Fatalf("first page is not in the declared deadline order at row %d: %v > %v", i, rows[i-1]["deadline"], rows[i]["deadline"])
		}
	}
	rels := linkRelations(t, first.Header().Get(resource.LinkHeader))
	if _, ok := rels["prev"]; ok {
		t.Errorf("the first page carries a prev relation: %v", rels)
	}
	next, ok := rels["next"]
	if !ok {
		t.Fatalf("the first page carries no next relation: %q", first.Header().Get(resource.LinkHeader))
	}

	// Next: the relation followed as issued answers the remaining rows, with prev and
	// no next, and no count.
	second := doRequestRecordedAs(t, h, "marshal", http.MethodGet, next, "")
	assertStatus(t, second.Code, http.StatusOK, second.Body.Bytes())
	if got := len(decodeRows(t, second.Body.Bytes())); got != 5 {
		t.Errorf("second page rows = %d, want 5", got)
	}
	if got := second.Header().Get(resource.TotalCountHeader); got != "" {
		t.Errorf("second page Total-Count = %q, want none", got)
	}
	rels = linkRelations(t, second.Header().Get(resource.LinkHeader))
	if _, ok := rels["prev"]; !ok {
		t.Errorf("the second page carries no prev relation: %v", rels)
	}
	if _, ok := rels["next"]; ok {
		t.Errorf("the last page carries a next relation: %v", rels)
	}

	tests := []struct {
		name       string
		target     string
		wantStatus int
		wantBody   string
		check      func(t *testing.T, rr http.Header, rows []map[string]any)
	}{
		{
			// A header click: the sort travels as a parameter and the server orders
			// every row of the board, not the rows the browser holds.
			name:       "a sort by title is served in that order",
			target:     sectorPath(anvil, "mission-boards?"+boardColumns+"&sort=title:asc&count=true"),
			wantStatus: http.StatusOK,
			check: func(t *testing.T, _ http.Header, rows []map[string]any) {
				t.Helper()
				for i := 1; i < len(rows); i++ {
					if rows[i-1]["title"].(string) > rows[i]["title"].(string) {
						t.Errorf("rows are not in title order at %d: %v > %v", i, rows[i-1]["title"], rows[i]["title"])
					}
				}
			},
		},
		{
			// A filter on an indexed column, the metadata's filterable: 'always'.
			name:       "a filter on an indexed column narrows the board and its total",
			target:     sectorPath(anvil, "mission-boards?"+boardColumns+"&filter=statusId:eq:open&count=true"),
			wantStatus: http.StatusOK,
			check: func(t *testing.T, header http.Header, rows []map[string]any) {
				t.Helper()
				for _, row := range rows {
					if row["statusId"] != "open" {
						t.Errorf("row %v is not open", row["title"])
					}
				}
				total, err := strconv.Atoi(header.Get(resource.TotalCountHeader))
				if err != nil || total < len(rows) || total >= 30 {
					t.Errorf("Total-Count = %q, want the open missions alone (at least the page, fewer than 30)", header.Get(resource.TotalCountHeader))
				}
			},
		},
		{
			// The companion rule: days left is computed in the SQL, indexed by nothing,
			// and declared allow_filter (filterable: 'withIndexed'), so a filter naming
			// it is accepted only beside one on an indexed column — the grid's control
			// waits until then.
			name:       "a companion-only filter alone is refused",
			target:     sectorPath(anvil, "mission-boards?"+boardColumns+"&filter=daysLeft:gt:1"),
			wantStatus: http.StatusBadRequest,
			wantBody:   "at least one column that is indexed",
		},
		{
			name:       "a companion filter beside an indexed one is served",
			target:     sectorPath(anvil, "mission-boards?"+boardColumns+"&filter=statusId:eq:open,daysLeft:gt:1&count=true"),
			wantStatus: http.StatusOK,
			check: func(t *testing.T, _ http.Header, rows []map[string]any) {
				t.Helper()
				for _, row := range rows {
					if row["statusId"] != "open" || row["daysLeft"].(float64) <= 1 {
						t.Errorf("row %v (%v, %v days) is outside the filter", row["title"], row["statusId"], row["daysLeft"])
					}
				}
			},
		},
		{
			// A column the metadata marks nothing on — Missions' notes, a plain column —
			// draws no control, and a filter naming it is refused naming the column.
			name:       "a filter on a column the metadata does not mark is refused naming it",
			target:     sectorPath(anvil, "missions?columns=id,title,notes&filter=notes:isnotnull"),
			wantStatus: http.StatusBadRequest,
			wantBody:   "'notes' is not filterable",
		},
		{
			// A configured pageSize over the view's maximum is the server's 400, which the
			// list shows in the server's words.
			name:       "a page size over the declared maximum is refused naming it",
			target:     sectorPath(anvil, "mission-boards?"+boardColumns+"&limit=201"),
			wantStatus: http.StatusBadRequest,
			wantBody:   "200",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rr := doRequestRecordedAs(t, h, "marshal", http.MethodGet, tt.target, "")
			assertStatus(t, rr.Code, tt.wantStatus, rr.Body.Bytes())
			if tt.wantBody != "" && !strings.Contains(rr.Body.String(), tt.wantBody) {
				t.Errorf("body = %s, want it to name %q", rr.Body.String(), tt.wantBody)
			}
			if tt.check != nil && rr.Code == http.StatusOK {
				tt.check(t, rr.Header(), decodeRows(t, rr.Body.Bytes()))
			}
		})
	}
}

// TestListPage_metadata pins what the console's generated TypeScript tells the list: the
// filterable flag per field, by the eligibility the server applies, and the declared
// order and page sizes in the descriptor, so the store sends no sort where the view
// declares one and reads the page size from the descriptor, never a literal.
func TestListPage_metadata(t *testing.T) {
	t.Parallel()

	service := filepath.Join("..", "..", "web", "console", "src", "app", "core", "service")
	resources, err := os.ReadFile(filepath.Join(service, "zz_gen_resources.ts"))
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	api, err := os.ReadFile(filepath.Join(service, "zz_gen_api.ts"))
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}

	tests := []struct {
		name    string
		source  []byte
		want    string
		absence bool
	}{
		{name: "an indexed view column filters always", source: resources, want: "{ fieldName: 'title', displayType: 'string', required: true, isIndex: true, filterable: 'always' }"},
		{name: "a view's allow_filter column, computed in the SQL, filters beside an indexed one", source: resources, want: "{ fieldName: 'daysLeft', displayType: 'number', required: true, isIndex: false, filterable: 'withIndexed' }"},
		{name: "a table allow_filter column filters beside an indexed one", source: resources, want: "{ fieldName: 'fee', displayType: 'number', required: true, isIndex: false, filterable: 'withIndexed' }"},
		{name: "a plain column carries no filterable, so the grid draws no control", source: resources, want: "{ fieldName: 'notes', displayType: 'string', required: false, isIndex: false }"},
		{name: "a computed resource's allow_filter column filters always", source: resources, want: "{ fieldName: 'shipName', displayType: 'string', required: false, isIndex: false, filterable: 'always' }"},
		{name: "the board's descriptor carries its declared page sizes and order", source: api, want: "route: 'mission-boards',\n      scope: 'domain',\n      consolidated: false,\n      keys: ['id'],\n      operations: ['list'],\n      page: { default: 25, max: 200 },\n      order: [{ field: 'deadline', direction: 'asc' }],"},
		{name: "a view's declared descending order reaches the descriptor too", source: api, want: "route: 'open-missions-by-squadrons',\n      scope: 'domain',\n      consolidated: false,\n      keys: ['squadronId', 'sectorId'],\n      operations: ['list'],\n      page: { default: 25, max: 200 },\n      order: [{ field: 'openMissions', direction: 'desc' }],"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := strings.Contains(string(tt.source), tt.want); got == tt.absence {
				t.Errorf("generated source contains %q = %v, want %v", tt.want, got, !tt.absence)
			}
		})
	}
}
