package integration

// This suite pins search over the demo world (resource README, section 4). Clients
// carries the box, the three text matches, the @searchAlso branch to the client
// contact's display name, the PII placement (a box over a resource whose box searches a
// PII field posts), the quoted value, and the permission rules: the ClientBrowser role
// (the marshal) reads name alone under trusted = true, so its box drops the contact
// fields and the branch without a word, matches a name only on a trusted row, and an
// explicit match on a field it may not read is a 403. Missions carries the date role
// (the deadline's day as YYYYMMDD) and the partitioned index (the same term in two
// sectors answers each sector's own rows); Hangars the prefix fact alone (a case-folded
// startswith and nothing else, no box); ClientRosters the view whose field names the
// table column it projects; MissionBoards the view over two tables' indexes, read
// through one key set per term, so its box joins the rest of the filter with AND alone;
// SectorHazardBoards the computed resource, whose text fields match in memory. The
// expected rows are computed from the unfiltered lists with a plain substring, so the
// suite pins the box against the data, never a row count typed in. The generated
// TypeScript metadata is pinned last.
//
// Demonstrates: search.box, search.match, search.prefix, search.date, search.partitioned, @searchAlso, search.pii-posts, search.denied-field, search.conditional-field, search.key-set, search.view, search.memory, search.quoted-value, metadata.search.

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
)

func TestSearch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, h, _ := sharedWorld(t)

	// The grants this suite rides, pinned to the role file: headquarters reads every
	// client field and the contacts, the client browser name alone on trusted rows.
	provesGrant(t, crew.Roles(), "Headquarters", "List", "Clients", "")
	provesGrant(t, crew.Roles(), "Headquarters", "List", "ClientContacts", "")
	provesGrant(t, crew.Roles(), "ClientBrowser", "List", "Clients", "trusted = true")

	// The unfiltered lists the expectations are computed from.
	clients := listRows(t, h, "governor", consoleAPI+"/clients")
	missions := listRows(t, h, "marshal", sectorPath(anvil, "missions?limit=200"))
	boards := listRows(t, h, "marshal", sectorPath(anvil, "mission-boards?limit=200"))
	hazards := listRows(t, h, "marshal", sectorPath(anvil, "sector-hazard-boards?limit=all"))
	if len(clients) != 4 || len(missions) != 30 || len(boards) != 30 || len(hazards) == 0 {
		t.Fatalf("seed: clients = %d, missions = %d, boards = %d, hazards = %d", len(clients), len(missions), len(boards), len(hazards))
	}

	// The deadline the date case searches for: mission 1's, read from the table and
	// written the way the token column holds it (the day in UTC as YYYYMMDD).
	deadline := readColumn[time.Time](ctx, t, db, "Missions", spanner.Key{missionID(1)}, "Deadline")
	day := deadline.UTC().Format("20060102")
	onDay := func(row map[string]any) bool {
		at, err := time.Parse(time.RFC3339Nano, cell[string](t, row, "deadline"))
		if err != nil {
			t.Fatalf("deadline %q: %v", row["deadline"], err)
		}

		return at.UTC().Format("20060102") == day
	}
	titled := func(term string) func(map[string]any) bool {
		return func(row map[string]any) bool { return has(row, "title", term) }
	}
	open := func(row map[string]any) bool { return cell[string](t, row, "statusId") == "open" }
	both := func(a, b func(map[string]any) bool) func(map[string]any) bool {
		return func(row map[string]any) bool { return a(row) && b(row) }
	}
	either := func(a, b func(map[string]any) bool) func(map[string]any) bool {
		return func(row map[string]any) bool { return a(row) || b(row) }
	}
	boardMatch := func(term string) func(map[string]any) bool {
		return func(row map[string]any) bool { return has(row, "title", term) || has(row, "clientName", term) }
	}
	shipNamed := func(term string) func(map[string]any) bool {
		return func(row map[string]any) bool { return has(row, "shipName", term) }
	}
	// SectorHazardBoards keys by ship and subsystem; the others by id.
	hazardKey := func(row map[string]any) string {
		return cell[string](t, row, "shipName") + "/" + cell[string](t, row, "subsystem")
	}
	idKey := func(row map[string]any) string { return cell[string](t, row, "id") }

	if n := len(selectRows(missions, titled("escort"))); n < 3 {
		t.Fatalf("the seed holds %d Anvil missions titled with escort, want at least 3 for the paging case", n)
	}
	if n := len(selectRows(boards, boardMatch("halvard"))); n < 2 {
		t.Fatalf("the seed holds %d Anvil boards naming halvard, want at least 2", n)
	}

	tests := []struct {
		name       string
		user       accesstypes.User
		method     string
		target     string
		body       string
		wantStatus int
		wantBody   string           // a 4xx answer's text, when it matters
		wantRows   []map[string]any // the rows expected, keyed by key
		key        func(map[string]any) string
		wantNames  []string // the expected names, for the global clients
	}{
		// The box over Clients, posted: every term found in some field the index covers.
		{name: "the box finds a client through the contact's display name alone (@searchAlso): no client field says client", user: "governor", method: http.MethodPost, target: consoleAPI + "/clients", body: box("client"), wantStatus: http.StatusOK, wantNames: []string{"Halvard Freight"}},
		{name: "the box ANDs its terms: halvard names one client, example every contact email", user: "governor", method: http.MethodPost, target: consoleAPI + "/clients", body: box("halvard example"), wantStatus: http.StatusOK, wantNames: []string{"Halvard Freight"}},
		{name: "a term found in the contact email alone admits every client (the PII field joins the box)", user: "governor", method: http.MethodPost, target: consoleAPI + "/clients", body: box("example"), wantStatus: http.StatusOK, wantNames: []string{"Bastion Relay Station", "Halvard Freight", "Meridian Survey Office", "Vellum Medical Cooperative"}},
		{name: "a term is read case-insensitively and may meet several fields", user: "governor", method: http.MethodPost, target: consoleAPI + "/clients", body: box("INES"), wantStatus: http.StatusOK, wantNames: []string{"Meridian Survey Office"}},
		{name: "a term nothing holds answers an empty list", user: "governor", method: http.MethodPost, target: consoleAPI + "/clients", body: box("zephyr"), wantStatus: http.StatusOK, wantNames: []string{}},
		{name: "a second box in one filter is refused", user: "governor", method: http.MethodPost, target: consoleAPI + "/clients", body: `{"filter":"*:search:\"a\",*:search:\"b\""}`, wantStatus: http.StatusBadRequest},
		{name: "the box joins the rest of the filter with AND: a term every client meets, narrowed by a match on the name", user: "governor", method: http.MethodPost, target: consoleAPI + "/clients", body: `{"filter":"name:startswith:merid,*:search:\"example\""}`, wantStatus: http.StatusOK, wantNames: []string{"Meridian Survey Office"}},
		// The PII placement: the box searches ContactEmail, so a filter carrying it posts.
		{name: "a box over a resource whose box searches a PII field is refused in the URL", user: "governor", target: consoleAPI + "/clients?" + filter(`*:search:"halvard"`), wantStatus: http.StatusBadRequest, wantBody: "request body"},
		{name: "an explicit match on the PII field is refused in the URL too", user: "governor", target: consoleAPI + "/clients?" + filter(`contactEmail:contains:halvard`), wantStatus: http.StatusBadRequest},
		{name: "an explicit match on the PII field, posted, answers", user: "governor", method: http.MethodPost, target: consoleAPI + "/clients", body: `{"filter":"contactEmail:contains:halvard"}`, wantStatus: http.StatusOK, wantNames: []string{"Halvard Freight"}},
		// The matches: Name's tokens are anchored, the contact fields' are not.
		{name: "startswith on an anchored field, in the URL since it names no PII field", user: "governor", target: consoleAPI + "/clients?" + filter(`name:startswith:halv`), wantStatus: http.StatusOK, wantNames: []string{"Halvard Freight"}},
		{name: "startswith is case-insensitive", user: "governor", target: consoleAPI + "/clients?" + filter(`name:startswith:HALV`), wantStatus: http.StatusOK, wantNames: []string{"Halvard Freight"}},
		{name: "endswith on an anchored field", user: "governor", target: consoleAPI + "/clients?" + filter(`name:endswith:freight`), wantStatus: http.StatusOK, wantNames: []string{"Halvard Freight"}},
		{name: "contains on an anchored field", user: "governor", target: consoleAPI + "/clients?" + filter(`name:contains:survey`), wantStatus: http.StatusOK, wantNames: []string{"Meridian Survey Office"}},
		{name: "a match stands alone as the filter's indexed touch (contactName carries no index tag)", user: "governor", method: http.MethodPost, target: consoleAPI + "/clients", body: `{"filter":"contactName:contains:marrow"}`, wantStatus: http.StatusOK, wantNames: []string{"Meridian Survey Office"}},
		{name: "startswith on a field whose tokens carry no anchors is refused at decode", user: "governor", method: http.MethodPost, target: consoleAPI + "/clients", body: `{"filter":"contactName:startswith:cleo"}`, wantStatus: http.StatusBadRequest},
		{name: "a match on a field with no search fact is refused at decode", user: "governor", target: consoleAPI + "/clients?" + filter(`trusted:contains:tru`), wantStatus: http.StatusBadRequest},
		// The quoted value.
		{name: "a quoted value carries a space", user: "governor", target: consoleAPI + "/clients?" + filter(`name:eq:"Halvard Freight"`), wantStatus: http.StatusOK, wantNames: []string{"Halvard Freight"}},
		{name: "a quoted value carries the grammar's separators", user: "governor", target: consoleAPI + "/clients?" + filter(`name:eq:"Halvard, Freight|Co (Ltd)"`), wantStatus: http.StatusOK, wantNames: []string{}},
		{name: "a quoted value inside an in list", user: "governor", target: consoleAPI + "/clients?" + filter(`name:in:("Halvard Freight","Meridian Survey Office")`), wantStatus: http.StatusOK, wantNames: []string{"Halvard Freight", "Meridian Survey Office"}},
		// The client browser: name alone, under trusted = true, no contacts.
		{name: "a box field the caller may not read drops out without a word: the marshal's box finds no client by the contact name", user: "marshal", method: http.MethodPost, target: consoleAPI + "/clients", body: box("cleo"), wantStatus: http.StatusOK, wantNames: []string{}},
		{name: "a branch the caller may not read drops out too: the marshal's box finds no client by the contact's display name", user: "marshal", method: http.MethodPost, target: consoleAPI + "/clients", body: box("client"), wantStatus: http.StatusOK, wantNames: []string{}},
		{name: "the field the marshal holds still answers", user: "marshal", method: http.MethodPost, target: consoleAPI + "/clients", body: box("halvard"), wantStatus: http.StatusOK, wantNames: []string{"Halvard Freight"}},
		{name: "a conditional field matches only rows meeting the condition: the untrusted relay station is not found by the marshal", user: "marshal", method: http.MethodPost, target: consoleAPI + "/clients", body: box("bastion"), wantStatus: http.StatusOK, wantNames: []string{}},
		{name: "and is found by headquarters, whose grant carries no condition", user: "governor", method: http.MethodPost, target: consoleAPI + "/clients", body: box("bastion"), wantStatus: http.StatusOK, wantNames: []string{"Bastion Relay Station"}},
		{name: "an explicit match on a field the caller may not read is a 403", user: "marshal", method: http.MethodPost, target: consoleAPI + "/clients", body: `{"filter":"contactEmail:contains:halvard"}`, wantStatus: http.StatusForbidden},
		// Missions: text and a date, partitioned by the sector.
		{name: "the box over Missions finds a title", user: "marshal", target: sectorPath(anvil, "missions?"+filter(`*:search:"beacon"`)), wantStatus: http.StatusOK, wantRows: selectRows(missions, titled("beacon")), key: idKey},
		{name: "a partitioned index answers the sector's own rows: the same term in Bastion", user: "governor", target: sectorPath(bastion, "missions?"+filter(`*:search:"beacon"`)), wantStatus: http.StatusOK, wantRows: []map[string]any{{"id": missionID(9)}}, key: idKey},
		{name: "a date in the box: the deadline's day as YYYYMMDD", user: "marshal", target: sectorPath(anvil, "missions?"+filter(`*:search:"`+day+`"`)), wantStatus: http.StatusOK, wantRows: selectRows(missions, onDay), key: idKey},
		{name: "a date field answers no explicit match", user: "marshal", target: sectorPath(anvil, "missions?"+filter(`deadline:contains:`+day[:4])), wantStatus: http.StatusBadRequest},
		{name: "contains on the title", user: "marshal", target: sectorPath(anvil, "missions?"+filter(`title:contains:beacon`)), wantStatus: http.StatusOK, wantRows: selectRows(missions, titled("beacon")), key: idKey},
		{name: "the box beside another condition", user: "marshal", target: sectorPath(anvil, "missions?"+filter(`statusId:eq:open,*:search:"escort"`)), wantStatus: http.StatusOK, wantRows: selectRows(missions, both(open, titled("escort"))), key: idKey},
		{name: "a box over one index may sit inside an OR", user: "marshal", target: sectorPath(anvil, "missions?"+filter(`statusId:eq:open|*:search:"escort"`)), wantStatus: http.StatusOK, wantRows: selectRows(missions, either(open, titled("escort"))), key: idKey},
		// Hangars: the prefix fact alone.
		{name: "startswith through the lowercased stored column, case-folded", user: "marshal", target: sectorPath(anvil, "hangars?"+filter(`name:startswith:ANVIL`)), wantStatus: http.StatusOK, wantNames: []string{"Anvil Dock One"}},
		{name: "the prefix fact answers startswith alone: contains is refused", user: "marshal", target: sectorPath(anvil, "hangars?"+filter(`name:contains:dock`)), wantStatus: http.StatusBadRequest},
		{name: "a table with no search index has no box", user: "marshal", target: sectorPath(anvil, "hangars?"+filter(`*:search:"anvil"`)), wantStatus: http.StatusBadRequest},
		// ClientRosters: the view names the table column it projects.
		{name: "the box over a view rides the table's index", user: "marshal", target: sectorPath(anvil, "client-rosters?"+filter(`*:search:"meridian"`)), wantStatus: http.StatusOK, wantNames: []string{"Meridian Survey Office"}},
		{name: "the view's field answers the table column's anchored matches", user: "marshal", target: sectorPath(anvil, "client-rosters?"+filter(`name:endswith:office`)), wantStatus: http.StatusOK, wantNames: []string{"Meridian Survey Office"}},
		// MissionBoards: two indexes, one key set per term.
		{name: "a box over two indexes: a term met in the client's name or the title", user: "marshal", target: sectorPath(anvil, "mission-boards?"+filter(`*:search:"halvard"`)), wantStatus: http.StatusOK, wantRows: selectRows(boards, boardMatch("halvard")), key: idKey},
		{name: "two terms over two indexes", user: "marshal", target: sectorPath(anvil, "mission-boards?"+filter(`*:search:"escort meridian"`)), wantStatus: http.StatusOK, wantRows: selectRows(boards, both(boardMatch("escort"), boardMatch("meridian"))), key: idKey},
		{name: "the key set joins the rest of the filter with AND", user: "marshal", target: sectorPath(anvil, "mission-boards?"+filter(`statusId:eq:open,*:search:"halvard"`)), wantStatus: http.StatusOK, wantRows: selectRows(boards, both(open, boardMatch("halvard"))), key: idKey},
		{name: "a box over two indexes inside an OR is refused, naming the way out", user: "marshal", target: sectorPath(anvil, "mission-boards?"+filter(`statusId:eq:open|*:search:"halvard"`)), wantStatus: http.StatusBadRequest, wantBody: "move the box out of the OR"},
		// SectorHazardBoards: in memory.
		{name: "a computed resource's text field matches in memory", user: "marshal", target: sectorPath(anvil, "sector-hazard-boards?limit=all&"+filter(`shipName:contains:KING`)), wantStatus: http.StatusOK, wantRows: selectRows(hazards, shipNamed("king")), key: hazardKey},
		{name: "a computed resource's box, every term in some text field", user: "marshal", target: sectorPath(anvil, "sector-hazard-boards?limit=all&"+filter(`*:search:"king hull"`)), wantStatus: http.StatusOK, wantRows: selectRows(hazards, both(shipNamed("king"), func(row map[string]any) bool { return has(row, "subsystem", "hull") })), key: hazardKey},
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
				var names []string
				for _, row := range rows {
					names = append(names, cell[string](t, row, "name"))
				}
				slices.Sort(names)
				if !slices.Equal(names, tt.wantNames) {
					t.Errorf("names = %v, want %v", names, tt.wantNames)
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

	t.Run("count and the cursor carry the box like any filter", func(t *testing.T) {
		t.Parallel()

		want := keysOf(selectRows(missions, titled("escort")), idKey)
		first := doRequestRecordedAs(t, h, "marshal", http.MethodGet, sectorPath(anvil, "missions?"+filter(`*:search:"escort"`)+"&limit=2&count=true"), "")
		assertStatus(t, first.Code, http.StatusOK, first.Body.Bytes())
		if got := first.Header().Get(resource.TotalCountHeader); got != fmt.Sprint(len(want)) {
			t.Errorf("Total-Count = %q, want %d", got, len(want))
		}
		got := keysOf(decodeRows(t, first.Body.Bytes()), idKey)
		next, ok := linkRelations(t, first.Header().Get(resource.LinkHeader))["next"]
		for pages := 1; ok; pages++ {
			if pages > len(want) {
				t.Fatalf("the cursor never ends: %d pages over %d rows", pages, len(want))
			}
			page := doRequestRecordedAs(t, h, "marshal", http.MethodGet, next, "")
			assertStatus(t, page.Code, http.StatusOK, page.Body.Bytes())
			got = append(got, keysOf(decodeRows(t, page.Body.Bytes()), idKey)...)
			next, ok = linkRelations(t, page.Header().Get(resource.LinkHeader))["next"]
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("the pages = %v, want %v", got, want)
		}
	})

	t.Run("the generated metadata says which matches a column answers and what the box searches", func(t *testing.T) {
		t.Parallel()

		service := filepath.Join("..", "..", "web", "console", "src", "app", "core", "service")
		data, err := os.ReadFile(filepath.Join(service, "zz_gen_resources.ts"))
		if err != nil {
			t.Fatalf("os.ReadFile() error = %v", err)
		}
		source := string(data)
		for _, want := range []string{
			// An anchored token column answers the three matches; an indexed field filters always.
			"{ fieldName: 'name', displayType: 'string', required: true, isIndex: true, filterable: 'always', search: ['contains', 'startswith', 'endswith'] }",
			// A plain token column answers contains alone; the field filters beside an indexed one, and its match stands alone.
			"{ fieldName: 'contactName', displayType: 'string', required: true, isIndex: false, filterable: 'withIndexed', search: ['contains'], pii: true }",
			// The box's fields in declaration order, the branch under its dotted name.
			"searchFields: ['name', 'contactName', 'contactEmail', 'clientContact.displayName'],",
			"searchBranches: [{ field: 'clientContact.displayName', resource: Resources.ClientContacts, fieldName: 'displayName' }],",
			// The prefix fact alone.
			"{ fieldName: 'name', displayType: 'string', required: true, isIndex: true, filterable: 'always', search: ['startswith'] }",
			// A date joins the box and answers no match.
			"searchFields: ['title', 'deadline'],",
			// The view over two tables' indexes.
			"searchFields: ['title', 'clientName'],",
			// A computed resource's text fields answer the three matches in memory.
			"{ fieldName: 'shipName', displayType: 'string', required: false, isIndex: false, filterable: 'always', search: ['contains', 'startswith', 'endswith'] }",
			"searchFields: ['subsystem', 'shipName'],",
		} {
			if !strings.Contains(source, want) {
				t.Errorf("zz_gen_resources.ts lacks %q", want)
			}
		}
		if strings.Contains(source, "{ fieldName: 'deadline', displayType: 'date', required: true, isIndex: true, filterable: 'always', search:") {
			t.Errorf("zz_gen_resources.ts gives the deadline a search list; a date answers no explicit match")
		}
	})
}

// box is a posted filter body carrying the search box over the text.
func box(text string) string {
	return fmt.Sprintf(`{"filter":"*:search:\"%s\""}`, text)
}

// filter encodes a filter expression as the query string.
func filter(expression string) string {
	return url.Values{"filter": {expression}}.Encode()
}

// listRows lists a route as the user and returns its rows.
func listRows(t *testing.T, h http.Handler, user accesstypes.User, target string) []map[string]any {
	t.Helper()

	status, body := doRequestAs(t, h, user, http.MethodGet, target, "")
	assertStatus(t, status, http.StatusOK, body)

	return decodeRows(t, body)
}

// has reports whether the row's text cell contains the term, case-insensitively: the
// plain substring the box and the matches are pinned against.
func has(row map[string]any, field, term string) bool {
	text, _ := row[field].(string)

	return strings.Contains(strings.ToLower(text), strings.ToLower(term))
}

// selectRows keeps the rows the predicate admits.
func selectRows(rows []map[string]any, keep func(map[string]any) bool) []map[string]any {
	var out []map[string]any
	for _, row := range rows {
		if keep(row) {
			out = append(out, row)
		}
	}

	return out
}

// keysOf is the rows' keys, sorted.
func keysOf(rows []map[string]any, key func(map[string]any) string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, key(row))
	}
	slices.Sort(out)

	return out
}
