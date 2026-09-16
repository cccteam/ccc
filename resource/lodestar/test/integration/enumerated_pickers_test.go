// Demonstrates: @enumerate, @enumerate.plain-column, @enumerate.key-view, @enumerate.enum-table, @enumerate.computed, picker.read-disabled, picker.config-driven, virtual.keyed-read.
package integration

// This suite pins the server side of the enumerated pickers: the resources the
// generated metadata names for Mission's declared fields answer the personas who edit
// missions and refuse the cadet, the briefing compiles on a named template, and the
// generated TypeScript carries the declarations exactly — a resource-backed picker for
// the plain column and the foreign key's view, the fixed values for the enum table,
// and the computed catalog on the request field. The roster declares a maximum, so a
// picker pages it and reads the chosen client by key: the view, keyed by @primarykey,
// serves that read. The template catalog declares none, so a picker reads it whole
// with limit=all and resolves the chosen sheet from the list; its read stays suppressed.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// TestEnumeratedPickerResources lists the picker resources as the personas the console
// walk uses: the marshal holds List on both, the cadet on neither.
func TestEnumeratedPickerResources(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	tests := []struct {
		name       string
		user       accesstypes.User
		target     string
		wantStatus int
		wantRows   int
		check      func(t *testing.T, respBody []byte)
	}{
		{
			// Four clients, every one on the roster of every sector; the roster carries
			// what Clients lacks — the contact count — and what the picker stores, the id.
			name: "the marshal lists the client roster of Anvil", user: "marshal",
			target: sectorPath(anvil, "client-rosters?columns=id,name,contactCount,sectorMissions"), wantStatus: http.StatusOK, wantRows: 4,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				rows := rowsByID(t, decodeRows(t, respBody), "id")
				if got := rows[clientHalvardID]["contactCount"]; got != float64(1) {
					t.Errorf("Halvard Freight contactCount = %v, want 1", got)
				}
				if got := rows[clientBastionRelayID]["contactCount"]; got != float64(0) {
					t.Errorf("Bastion Relay Station contactCount = %v, want 0", got)
				}
				if got := rows[clientHalvardID]["sectorMissions"]; got == float64(0) {
					t.Errorf("Halvard Freight sectorMissions in Anvil = %v, want the seeded missions", got)
				}
			},
		},
		{
			// The roster is bounded (@page max), so its picker pages it and reads the
			// chosen client by key: the view declares its @primarykey and serves the read.
			name: "the roster, a keyed view, serves a read: the marshal reads Halvard Freight's row", user: "marshal",
			target: sectorPath(anvil, "client-rosters/"+clientHalvardID+"?columns=id,name,contactCount"), wantStatus: http.StatusOK,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				row := decodeRow(t, respBody)
				if got := row["name"]; got != "Halvard Freight" {
					t.Errorf("name = %v, want Halvard Freight", got)
				}
				if got := row["contactCount"]; got != float64(1) {
					t.Errorf("contactCount = %v, want 1", got)
				}
			},
		},
		{
			name: "the cadet holds no Read on the roster, so the read is refused", user: "cadet",
			target: sectorPath(anvil, "client-rosters/"+clientHalvardID), wantStatus: http.StatusForbidden,
		},
		{
			// The catalog declares no maximum and no order: a picker reads it whole.
			name: "the marshal lists the briefing template catalog whole", user: "marshal",
			target: "/api/briefing-templates?columns=id,name&limit=all", wantStatus: http.StatusOK, wantRows: 4,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				if got := rowsByID(t, decodeRows(t, respBody), "id")["standard"]["name"]; got != "Standard sheet" {
					t.Errorf("standard template name = %v, want Standard sheet", got)
				}
			},
		},
		{
			name: "the catalog has no read route: the display resolves from the list", user: "marshal",
			target: "/api/briefing-templates/standard", wantStatus: http.StatusNotFound,
		},
		{
			name: "the marshal's missions carry the template a plain column names", user: "marshal",
			target: sectorPath(anvil, "missions/"+missionHaulerID+"?columns=briefingTemplateId"), wantStatus: http.StatusOK,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				if got := decodeRow(t, respBody)["briefingTemplateId"]; got != "standard" {
					t.Errorf("briefingTemplateId = %v, want standard", got)
				}
			},
		},
		{name: "the cadet holds no List on the roster, so the picker's request is refused", user: "cadet", target: sectorPath(anvil, "client-rosters?columns=id,name"), wantStatus: http.StatusForbidden},
		{name: "the cadet holds no List on the catalog, so that picker's request is refused too", user: "cadet", target: "/api/briefing-templates?columns=id,name&limit=all", wantStatus: http.StatusForbidden},
		{name: "the dispatcher, who edits missions, lists the roster", user: "dispatcher", target: sectorPath(anvil, "client-rosters?columns=id,name"), wantStatus: http.StatusOK, wantRows: 4},
		{name: "the dispatcher reads a roster row by key, as the paging picker does", user: "dispatcher", target: sectorPath(anvil, "client-rosters/"+clientHalvardID+"?columns=id,name"), wantStatus: http.StatusOK},
		{name: "the booking agent, who books missions, lists the catalog whole", user: "booking", target: "/api/briefing-templates?columns=id,name&limit=all", wantStatus: http.StatusOK, wantRows: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, tt.target, "")
			assertStatus(t, status, tt.wantStatus, body)
			if status != http.StatusOK {
				return
			}
			if tt.wantRows > 0 {
				if rows := decodeRows(t, body); len(rows) != tt.wantRows {
					t.Errorf("rows = %d, want %d: %s", len(rows), tt.wantRows, body)
				}
			}
			if tt.check != nil {
				tt.check(t, body)
			}
		})
	}
}

// TestCompileBriefing_template compiles on a named template: the request field names
// the computed catalog, the body validates the id against it, and the hazard-first
// sheet carries the board whether or not it was asked for.
func TestCompileBriefing_template(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	tests := []struct {
		name         string
		body         string
		wantStatus   int
		wantTemplate string
		wantHazards  bool
	}{
		{name: "an empty template compiles the standard sheet", body: `{}`, wantStatus: http.StatusOK, wantTemplate: "Standard sheet"},
		{name: "a named template compiles on it", body: `{"templateId":"client-facing"}`, wantStatus: http.StatusOK, wantTemplate: "Client summary"},
		{name: "the hazard-first sheet folds the board in unasked", body: `{"templateId":"hazard-first","includeHazards":false}`, wantStatus: http.StatusOK, wantTemplate: "Hazard-first sheet", wantHazards: true},
		{name: "an id outside the catalog is refused", body: `{"templateId":"legacy-9"}`, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, "marshal", http.MethodPost, sectorPath(anvil, "compile-briefing"), tt.body)
			assertStatus(t, status, tt.wantStatus, body)
			if status != http.StatusOK {
				return
			}
			var got struct {
				Template    string `json:"template"`
				HazardBoard []any  `json:"hazardBoard"`
			}
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("json.Unmarshal(%s) error = %v", body, err)
			}
			if got.Template != tt.wantTemplate {
				t.Errorf("template = %q, want %q", got.Template, tt.wantTemplate)
			}
			if (len(got.HazardBoard) > 0) != tt.wantHazards {
				t.Errorf("hazard board = %d lines, want lines %v", len(got.HazardBoard), tt.wantHazards)
			}
		})
	}
}

// TestEnumeratedMetadata pins the console's generated metadata for the declared
// pickers: which resource each field lists is stated there, and only there.
func TestEnumeratedMetadata(t *testing.T) {
	t.Parallel()

	service := filepath.Join("..", "..", "web", "console", "src", "app", "core", "service")

	tests := []struct {
		name string
		file string
		want string
	}{
		{
			name: "a plain column names the computed catalog its picker lists",
			file: "zz_gen_resources.ts",
			want: "{ fieldName: 'briefingTemplateId', displayType: 'enumerated', required: false, isIndex: false, maxLength: 64, enumeratedResource: Resources.BriefingTemplates }",
		},
		{
			name: "a foreign key names the view over its target, not the target",
			file: "zz_gen_resources.ts",
			want: "{ fieldName: 'clientId', displayType: 'enumerated', required: true, isIndex: true, filterable: 'always', enumeratedResource: Resources.ClientRosters }",
		},
		{
			name: "a request field naming an enum table carries the values inline",
			file: "zz_gen_methods.ts",
			want: `{ fieldName: 'reasonId', displayType: 'enumerated', enumeration: [{ id: "aborted", display: "Aborted" }, { id: "recalled", display: "Recalled" }, { id: "solar_weather", display: "Solar weather" }, { id: "unrecoverable", display: "Unrecoverable" }] }`,
		},
		{
			name: "a request field names the computed catalog",
			file: "zz_gen_methods.ts",
			want: "{ fieldName: 'templateId', displayType: 'enumerated', enumeratedResource: Resources.BriefingTemplates }",
		},
		{
			name: "the catalog has no read handler, which the picker's display path reads",
			file: "zz_gen_resources.ts",
			want: "[Resources.BriefingTemplates]: {\n    route: 'briefing-templates',\n    readDisabled: true,",
		},
		{
			name: "the keyed view serves a read, so its metadata says nothing of readDisabled",
			file: "zz_gen_resources.ts",
			want: "[Resources.ClientRosters]: {\n    route: 'sectors/{sectorID}/client-rosters',\n    fields: [",
		},
		{
			name: "the keyed view's descriptor lists the read operation beside the list",
			file: "zz_gen_api.ts",
			want: "route: 'client-rosters',\n      scope: 'domain',\n      consolidated: false,\n      keys: ['id'],\n      operations: ['list', 'read'],",
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
