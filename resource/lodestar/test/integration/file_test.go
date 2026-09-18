// Demonstrates: @file.rendered.
package integration

// This suite pins the rendered file: ExpenseManifests is a keyed @computed struct with a
// struct-scope @file, so the generator serves GET .../expense-manifests/{missionId}/content
// by calling ExpenseManifestContent, which renders the mission's booked expenses as a
// text/csv sheet at request time. The gate is Read on the resource and a Read grant on
// content: the purser holds both, the marshal neither. The sheet's digest is its validator,
// so a kept copy hears 304 until an expense is booked; a mission that is not the sector's
// is 404, as on the read route.

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// sortiePodStandbyID is the pod mission's second sortie, the medevac standby, with a fuel
// expense of 300 and no note.
const sortiePodStandbyID = "90000000-0000-4000-8000-000000000012"

func TestExpenseManifest_renderedFile(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)
	// The pod mission is completed, so no suite books an expense against its two
	// sorties while this one reads the manifest: the recovery flight's tow gear and the
	// medevac standby's fuel.
	target := sectorPath(anvil, "expense-manifests/"+missionPodID+"/content")

	status, body := doRequestAs(t, h, "purser", http.MethodGet, sectorPath(anvil, "expense-manifests/"+missionPodID), "")
	assertStatus(t, status, http.StatusOK, body)
	row := decodeRow(t, body)
	if cell[string](t, row, "title") != "Recover the Lantern cargo pod" || cell[float64](t, row, "sorties") != 2 || cell[string](t, row, "expenses") != "1800" {
		t.Fatalf("pod manifest = %v, want the pod mission's title, two sorties, and 1800 in booked expenses", row)
	}

	first := fileRequestAs(t, h, "purser", target, nil)
	assertStatus(t, first.Code, http.StatusOK, first.Body.Bytes())
	etag := first.Header().Get("ETag")

	tests := []struct {
		name        string
		user        accesstypes.User
		target      string
		headers     map[string]string
		wantStatus  int
		wantHeaders map[string]string
		wantLines   int
		wantMessage string
	}{
		{
			name:       "the purser downloads the pod mission's sheet: a header and one line per booked expense",
			user:       "purser",
			target:     target,
			wantStatus: http.StatusOK,
			wantHeaders: map[string]string{
				"Content-Type":        "text/csv",
				"Content-Disposition": fmt.Sprintf(`inline; filename=manifest-%s.csv`, missionPodID),
				"Cache-Control":       "private, no-cache",
				"ETag":                etag,
			},
			wantLines: 3,
		},
		{
			name:        "the sheet's digest is its validator: a kept copy hears 304",
			user:        "purser",
			target:      target,
			headers:     map[string]string{"If-None-Match": etag},
			wantStatus:  http.StatusNotModified,
			wantHeaders: map[string]string{"ETag": etag},
		},
		{
			name:        "the marshal holds no Read on the manifests",
			user:        "marshal",
			target:      target,
			wantStatus:  http.StatusForbidden,
			wantMessage: "does not have (Read) on [ExpenseManifests",
		},
		{
			name:       "the cadet neither",
			user:       "cadet",
			target:     target,
			wantStatus: http.StatusForbidden,
		},
		{
			name:        "a mission that is not the sector's is 404 in the row's words",
			user:        "purser",
			target:      sectorPath(anvil, "expense-manifests/"+missionBeaconID+"/content"),
			wantStatus:  http.StatusNotFound,
			wantMessage: "ExpenseManifest " + missionBeaconID + " has no content",
		},
		{
			name:       "a sector the purser holds no role in is concealed",
			user:       "purser",
			target:     sectorPath(bastion, "expense-manifests/"+missionBeaconID+"/content"),
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rr := fileRequestAs(t, h, tt.user, tt.target, tt.headers)
			assertStatus(t, rr.Code, tt.wantStatus, rr.Body.Bytes())
			for name, want := range tt.wantHeaders {
				if got := rr.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			if tt.wantMessage != "" && !strings.Contains(rr.Body.String(), tt.wantMessage) {
				t.Errorf("body = %s, want the message %q", rr.Body.String(), tt.wantMessage)
			}
			if tt.wantLines == 0 {
				return
			}
			records, err := csv.NewReader(strings.NewReader(rr.Body.String())).ReadAll()
			if err != nil {
				t.Fatalf("csv.Reader.ReadAll() error = %v on:\n%s", err, rr.Body.String())
			}
			if len(records) != tt.wantLines {
				t.Errorf("sheet has %d lines, want %d:\n%s", len(records), tt.wantLines, rr.Body.String())
			}
			if got := strings.Join(records[0], ","); got != "sortie,pilot,launchedAt,category,amount,note" {
				t.Errorf("header = %q", got)
			}
			lines := map[string][]string{}
			for _, record := range records[1:] {
				lines[record[3]] = record
			}
			if got := lines["tow_gear"]; len(got) == 0 || got[0] != sortiePodID || got[1] != "veteran" || got[4] != "1500" || got[5] != "Grapple replacement" {
				t.Errorf("tow gear line = %v, want the recovery sortie flown by veteran, 1500, its note", got)
			}
			if got := lines["fuel"]; len(got) == 0 || got[0] != sortiePodStandbyID || got[1] != "lead" || got[4] != "300" || got[5] != "" {
				t.Errorf("fuel line = %v, want the standby sortie flown by lead, 300, no note", got)
			}
		})
	}
}
