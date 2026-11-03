package integration

// This suite pins the search box beside the column filters: the request the library's
// list page composes when both are on the screen, the page's own filter, the complete
// column filters and the box as conjuncts of one top-level AND, the first two
// parenthesized and the box bare, (page),(columns),*:search:"…". Over the mission board,
// as the Missions page sends it, the box narrows beside a status filter, beside a
// deadline comparison (an RFC 3339 timestamp, what the grid sends when it is typed), and
// beside the days-left companion, which the box alone admits as the filter's indexed
// touch while days left alone stays refused; two parenthesized groups ride before the
// box the way a page filter does; the count and the cursor pages carry the whole filter,
// in the view's declared order. Over Clients, as the Clients page sends it, the filter is
// placed as a whole: headquarters, whose box reaches the contact fields, posts the box
// beside a name filter or a contact-name match and is refused the URL, while the
// ClientBrowser role, whose grants deny every PII field the box would search, keeps the
// same shape in the URL; a field the caller may not read drops out of the box beside a
// filter as it does alone; and the child-table branch rides beside a filter. The expected
// rows are computed from the unfiltered lists wherever the list carries the matched text.
//
// Demonstrates: search.beside-filters, search.beside-filters.pii.

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
)

const (
	// clientColumns is the request the Clients page composes for headquarters: its
	// configured columns and the key.
	clientColumns = "columns=name,contactName,trusted,id"
	// browsedColumns is the same page's request for the ClientBrowser role, whose grant
	// leaves the contact name out of the list.
	browsedColumns = "columns=name,trusted,id"
)

// TestSearchBesideFilters sends the two pages' combined requests and pins their answers
// against the unfiltered lists.
func TestSearchBesideFilters(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	provesGrant(t, crew.Roles(), "Headquarters", "List", "Clients", "")
	provesGrant(t, crew.Roles(), "ClientBrowser", "List", "Clients", "trusted = true")

	// The unfiltered lists the expectations are computed from: the board as the marshal,
	// the clients as headquarters and as the client browser.
	boards := listRows(t, h, "marshal", sectorPath(anvil, "mission-boards?limit=200"))
	clients := listRows(t, h, "governor", consoleAPI+"/clients")
	browsed := listRows(t, h, "cadet", consoleAPI+"/clients")
	if len(boards) != 30 || len(clients) != 4 || len(browsed) != 3 {
		t.Fatalf("seed: boards = %d, clients = %d, browsed = %d", len(boards), len(clients), len(browsed))
	}

	boardMatch := func(term string) func(map[string]any) bool {
		return func(row map[string]any) bool { return has(row, "title", term) || has(row, "clientName", term) }
	}
	clientMatch := func(term string) func(map[string]any) bool {
		return func(row map[string]any) bool {
			return has(row, "name", term) || has(row, "contactName", term) || has(row, "contactEmail", term)
		}
	}
	prefixed := func(term string) func(map[string]any) bool {
		return func(row map[string]any) bool {
			return strings.HasPrefix(strings.ToLower(cell[string](t, row, "name")), term)
		}
	}
	contactNamed := func(term string) func(map[string]any) bool {
		return func(row map[string]any) bool { return has(row, "contactName", term) }
	}
	open := func(row map[string]any) bool { return cell[string](t, row, "statusId") == "open" }
	// The grid's One of over the name, two clients, as the grid sends it: a value with a
	// space and no grammar separator travels unquoted.
	nameList := `name:in:(Halvard Freight,Meridian Survey Office)`
	namedOneOf := func(row map[string]any) bool {
		return slices.Contains([]string{"Halvard Freight", "Meridian Survey Office"}, cell[string](t, row, "name"))
	}
	kindOf := func(kinds ...string) func(map[string]any) bool {
		return func(row map[string]any) bool { return slices.Contains(kinds, cell[string](t, row, "kindId")) }
	}
	daysLeftAtLeast := func(n float64) func(map[string]any) bool {
		return func(row map[string]any) bool { return cell[float64](t, row, "daysLeft") >= n }
	}
	deadlineOf := func(row map[string]any) time.Time {
		at, err := time.Parse(time.RFC3339Nano, cell[string](t, row, "deadline"))
		if err != nil {
			t.Fatalf("deadline %q: %v", row["deadline"], err)
		}

		return at
	}
	all := func(keep ...func(map[string]any) bool) func(map[string]any) bool {
		return func(row map[string]any) bool {
			for _, k := range keep {
				if !k(row) {
					return false
				}
			}

			return true
		}
	}
	idKey := func(row map[string]any) string { return cell[string](t, row, "id") }
	nameKey := func(row map[string]any) string { return cell[string](t, row, "name") }

	// The board's term, station, meets both indexes: the relay station's missions through
	// the client's name (its suffix, the anchors Name's tokens carry) and one title through
	// the missions' index.
	surveyed := selectRows(boards, boardMatch("station"))
	if len(surveyed) < 4 {
		t.Fatalf("the seed holds %d Anvil boards naming station, want at least 4", len(surveyed))
	}

	// The deadline bound the comparison case types, the middle of those boards' deadlines
	// truncated to the second, as the grid sends what is typed in a timestamp column's
	// menu: an RFC 3339 timestamp. The days-left threshold is the middle of their days.
	byDeadline := slices.Clone(surveyed)
	slices.SortFunc(byDeadline, func(a, b map[string]any) int { return deadlineOf(a).Compare(deadlineOf(b)) })
	bound := deadlineOf(byDeadline[len(byDeadline)/2]).Truncate(time.Second)
	fromBound := func(row map[string]any) bool { return !deadlineOf(row).Before(bound) }
	days := make([]float64, 0, len(surveyed))
	for _, row := range surveyed {
		days = append(days, cell[float64](t, row, "daysLeft"))
	}
	slices.Sort(days)
	daysThreshold := days[len(days)/2]

	// Every filter must narrow the term, and never to nothing: those boards are open and
	// closed, due before and after the bound, near and far, of several kinds.
	for name, subset := range map[string][]map[string]any{
		"open":                      selectRows(surveyed, open),
		"due from the bound":        selectRows(surveyed, fromBound),
		"with the days left":        selectRows(surveyed, daysLeftAtLeast(daysThreshold)),
		"open escort or courier":    selectRows(surveyed, all(kindOf("escort", "courier"), open)),
		"named clients (unrelated)": selectRows(clients, all(namedOneOf, clientMatch("example"))),
	} {
		whole := len(surveyed)
		if name == "named clients (unrelated)" {
			whole = len(selectRows(clients, clientMatch("example")))
		}
		if len(subset) == 0 || len(subset) == whole {
			t.Fatalf("the seed leaves %d of %d rows %s; the case needs the filter to narrow the box", len(subset), whole, name)
		}
	}

	boardsURL := func(expression string) string {
		return sectorPath(anvil, "mission-boards?"+boardColumns+"&count=true&"+filter(expression))
	}
	clientsPage := consoleAPI + "/clients?" + clientColumns + "&count=true"
	browsedPage := consoleAPI + "/clients?" + browsedColumns + "&count=true"
	posted := func(expression string) string { return fmt.Sprintf(`{"filter":%q}`, expression) }

	tests := []struct {
		name       string
		user       accesstypes.User
		method     string
		target     string
		body       string
		wantStatus int
		wantBody   string           // a 4xx answer's text, when it matters
		wantRows   []map[string]any // the rows expected, computed from the lists
		key        func(map[string]any) string
		wantNames  []string // the names expected, typed where the matched text is not in the list
	}{
		// The mission board, as the marshal: the box beside the column filters, the
		// column filters parenthesized and the box bare, as the page composes them.
		{name: "the box beside a status filter", user: "marshal", target: boardsURL(`(statusId:eq:open),*:search:"station"`), wantStatus: http.StatusOK, wantRows: selectRows(boards, all(open, boardMatch("station"))), key: idKey},
		{name: "the box beside a deadline comparison, the timestamp as the grid sends what is typed", user: "marshal", target: boardsURL(`(deadline:gte:` + bound.Format(time.RFC3339) + `),*:search:"station"`), wantStatus: http.StatusOK, wantRows: selectRows(boards, all(fromBound, boardMatch("station"))), key: idKey},
		{name: "the box alone admits the days-left companion: the box is the filter's indexed touch", user: "marshal", target: boardsURL(fmt.Sprintf(`(daysLeft:gte:%d),*:search:"station"`, int(daysThreshold))), wantStatus: http.StatusOK, wantRows: selectRows(boards, all(daysLeftAtLeast(daysThreshold), boardMatch("station"))), key: idKey},
		{name: "days left alone stays refused", user: "marshal", target: boardsURL(`daysLeft:gte:10`), wantStatus: http.StatusBadRequest, wantBody: "at least one column that is indexed"},
		{name: "two parenthesized groups before the box, the shape a page filter makes", user: "marshal", target: boardsURL(`(kindId:in:(escort,courier)),(statusId:eq:open),*:search:"station"`), wantStatus: http.StatusOK, wantRows: selectRows(boards, all(kindOf("escort", "courier"), open, boardMatch("station"))), key: idKey},
		// Clients, as headquarters: the box reaches the contact fields, so the whole
		// filter travels in the body, and the URL form is refused. The column filters are
		// the page's: the name (indexed) and the contact name (a listed match); trusted is
		// neither indexed nor allow_filter, so its column draws no control.
		{name: "headquarters posts the box beside a name filter", user: "governor", method: http.MethodPost, target: clientsPage, body: posted(`(` + nameList + `),*:search:"example"`), wantStatus: http.StatusOK, wantRows: selectRows(clients, all(namedOneOf, clientMatch("example"))), key: nameKey},
		{name: "the same filter in the URL is refused for headquarters", user: "governor", target: clientsPage + "&" + filter(`(`+nameList+`),*:search:"example"`), wantStatus: http.StatusBadRequest, wantBody: "request body"},
		{name: "a contact-name match beside the name filter and the box, posted", user: "governor", method: http.MethodPost, target: clientsPage, body: posted(`(` + nameList + `,contactName:contains:marrow),*:search:"example"`), wantStatus: http.StatusOK, wantRows: selectRows(clients, all(namedOneOf, contactNamed("marrow"), clientMatch("example"))), key: nameKey},
		{name: "the child-table branch rides beside a filter: the contact's display name finds the client", user: "governor", method: http.MethodPost, target: clientsPage, body: posted(`(name:startswith:h),*:search:"client"`), wantStatus: http.StatusOK, wantNames: []string{"Halvard Freight"}},
		{name: "headquarters finds the contact's name through the box beside a name prefix", user: "governor", method: http.MethodPost, target: clientsPage, body: posted(`(name:startswith:m),*:search:"marrow"`), wantStatus: http.StatusOK, wantRows: selectRows(clients, all(prefixed("m"), contactNamed("marrow"))), key: nameKey},
		// Clients, as the cadet, a client browser: the box searches the name alone, so the
		// same shape stays in the URL, and the fields the role may not read never answer.
		{name: "the client browser keeps the box beside a name prefix in the URL: no PII field is reached", user: "cadet", target: browsedPage + "&" + filter(`(name:startswith:m),*:search:"office"`), wantStatus: http.StatusOK, wantRows: selectRows(browsed, all(prefixed("m"), boardMatchName("office"))), key: nameKey},
		{name: "a field the client browser may not read drops out of the box beside a filter: the contact's name finds nothing", user: "cadet", target: browsedPage + "&" + filter(`(name:startswith:m),*:search:"marrow"`), wantStatus: http.StatusOK, wantNames: []string{}},
		{name: "an explicit match on that field beside the box is a 403", user: "cadet", method: http.MethodPost, target: browsedPage, body: posted(`(contactName:contains:marrow),*:search:"office"`), wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			status, body := doRequestAs(t, h, tt.user, method, tt.target, tt.body)
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantBody != "" && !strings.Contains(string(body), tt.wantBody) {
				t.Errorf("body = %s, want it to say %q", body, tt.wantBody)
			}
			if status != http.StatusOK {
				return
			}
			rows := decodeRows(t, body)
			if tt.wantNames != nil {
				got := keysOf(rows, nameKey)
				if !slices.Equal(got, tt.wantNames) {
					t.Errorf("names = %v, want %v", got, tt.wantNames)
				}

				return
			}
			got := keysOf(rows, tt.key)
			want := keysOf(tt.wantRows, tt.key)
			if len(want) == 0 {
				t.Fatalf("the case expects no rows; a case over the computed sets must expect some")
			}
			if !slices.Equal(got, want) {
				t.Errorf("rows = %v, want %v", got, want)
			}
		})
	}

	t.Run("the count and the cursor carry the whole filter, in the declared order", func(t *testing.T) {
		t.Parallel()

		want := keysOf(selectRows(boards, all(open, boardMatch("station"))), idKey)
		target := sectorPath(anvil, "mission-boards?"+boardColumns+"&count=true&limit=2&"+filter(`(statusId:eq:open),*:search:"station"`))
		page := doRequestRecordedAs(t, h, "marshal", http.MethodGet, target, "")
		assertStatus(t, page.Code, http.StatusOK, page.Body.Bytes())
		if got := page.Header().Get(resource.TotalCountHeader); got != fmt.Sprint(len(want)) {
			t.Errorf("Total-Count = %q, want %d", got, len(want))
		}
		var got []string
		var last time.Time
		for pages := 1; ; pages++ {
			if pages > len(want) {
				t.Fatalf("the cursor never ends: %d pages over %d rows", pages, len(want))
			}
			for _, row := range decodeRows(t, page.Body.Bytes()) {
				if at := deadlineOf(row); at.Before(last) {
					t.Errorf("row %v breaks the declared deadline order: %v after %v", row["title"], at, last)
				} else {
					last = at
				}
				got = append(got, idKey(row))
			}
			next, ok := linkRelations(t, page.Header().Get(resource.LinkHeader))["next"]
			if !ok {
				break
			}
			page = doRequestRecordedAs(t, h, "marshal", http.MethodGet, next, "")
			assertStatus(t, page.Code, http.StatusOK, page.Body.Bytes())
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("the pages = %v, want %v", got, want)
		}
	})
}

// boardMatchName is the box over a client browser's list, which carries the name alone.
func boardMatchName(term string) func(map[string]any) bool {
	return func(row map[string]any) bool { return has(row, "name", term) }
}
