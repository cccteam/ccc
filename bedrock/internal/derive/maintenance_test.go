package derive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// inChicago is a wall-clock moment of 2026 in Chicago, the tests' zone.
func inChicago(t *testing.T, month time.Month, day, hour, minute int) time.Time {
	t.Helper()

	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}

	return time.Date(2026, month, day, hour, minute, 0, 0, loc)
}

// sunday is the weekly slot the tests use: Sunday 02:00 to 04:00 Chicago time.
func sunday(releases string) MaintenanceWindow {
	return MaintenanceWindow{TimeZone: "America/Chicago", Weekly: []WeeklySlot{{Day: "Sunday", From: "02:00", To: "04:00"}}, Releases: releases}
}

func TestMaintenanceValidate(t *testing.T) {
	t.Parallel()

	valid := func() Placement {
		return Placement{
			Prefix: "imp", Environments: []string{"tst", "stg", "prd"}, Regions: []Region{{Name: "us-central1", Code: "uc1"}},
			AppsDomain: "lab.example.com", HostedDomain: "example.com", StateBucket: "b", PlaceholderImage: "i",
			DefaultBranch: "main", Repository: "harbor", ReleaseApp: "acme-release",
		}
	}
	tests := []struct {
		name        string
		maintenance map[string]MaintenanceWindow
		wantErr     string
	}{
		{name: "no setting at all"},
		{name: "anytime", maintenance: map[string]MaintenanceWindow{"prd": {Anytime: true}}},
		{name: "a weekly slot in a zone", maintenance: map[string]MaintenanceWindow{"prd": sunday("")}},
		{name: "a weekly slot crossing midnight", maintenance: map[string]MaintenanceWindow{"prd": {TimeZone: "America/Chicago", Weekly: []WeeklySlot{{Day: "Saturday", From: "22:00", To: "02:00"}}}}},
		{name: "a dated slot alone", maintenance: map[string]MaintenanceWindow{"stg": {TimeZone: "Europe/Paris", Dates: []DatedSlot{{On: "2026-11-15", From: "22:00", To: "23:30"}}}}},
		{name: "every release", maintenance: map[string]MaintenanceWindow{"prd": sunday(ReleasesAll)}},
		{name: "breaking spelled out", maintenance: map[string]MaintenanceWindow{"prd": sunday(ReleasesBreaking)}},
		{name: "an unknown environment", maintenance: map[string]MaintenanceWindow{"qa": {Anytime: true}}, wantErr: `maintenance names "qa", which is not one of the environments (tst, stg, prd)`},
		{name: "releases that is neither", maintenance: map[string]MaintenanceWindow{"prd": sunday("always")}, wantErr: `maintenance.prd: releases "always" is not breaking or all`},
		{name: "no time zone", maintenance: map[string]MaintenanceWindow{"prd": {Weekly: []WeeklySlot{{Day: "Sunday", From: "02:00", To: "04:00"}}}}, wantErr: "maintenance.prd names no timeZone: the IANA zone the slots' times are read in (America/Chicago)"},
		{name: "a time zone that does not load", maintenance: map[string]MaintenanceWindow{"prd": {TimeZone: "Mars/Olympus", Weekly: []WeeklySlot{{Day: "Sunday", From: "02:00", To: "04:00"}}}}, wantErr: `maintenance.prd: time zone "Mars/Olympus" does not load: an IANA zone name (America/Chicago)`},
		{name: "no slot", maintenance: map[string]MaintenanceWindow{"prd": {TimeZone: "America/Chicago"}}, wantErr: `maintenance.prd names no weekly or dated slot and never opens: write "anytime" for no window`},
		{name: "a day that is not one", maintenance: map[string]MaintenanceWindow{"prd": {TimeZone: "America/Chicago", Weekly: []WeeklySlot{{Day: "Funday", From: "02:00", To: "04:00"}}}}, wantErr: `maintenance.prd.weekly[0]: day "Funday" is not a day of the week (Monday to Sunday)`},
		{name: "a from that is not a time of day", maintenance: map[string]MaintenanceWindow{"prd": {TimeZone: "America/Chicago", Weekly: []WeeklySlot{{Day: "Sunday", From: "2am", To: "04:00"}}}}, wantErr: `maintenance.prd.weekly[0]: from "2am" is not a time of day (HH:MM, 24-hour)`},
		{name: "a to past the day", maintenance: map[string]MaintenanceWindow{"prd": {TimeZone: "America/Chicago", Weekly: []WeeklySlot{{Day: "Sunday", From: "02:00", To: "24:00"}}}}, wantErr: `maintenance.prd.weekly[0]: to "24:00" is not a time of day (HH:MM, 24-hour)`},
		{name: "a weekly slot that never opens", maintenance: map[string]MaintenanceWindow{"prd": {TimeZone: "America/Chicago", Weekly: []WeeklySlot{{Day: "Sunday", From: "02:00", To: "02:00"}}}}, wantErr: "maintenance.prd.weekly[0]: from 02:00 to 02:00 never opens"},
		{name: "a date that is not one", maintenance: map[string]MaintenanceWindow{"prd": {TimeZone: "America/Chicago", Dates: []DatedSlot{{On: "15 Nov 2026", From: "22:00", To: "23:30"}}}}, wantErr: `maintenance.prd.dates[0]: on "15 Nov 2026" is not a date (YYYY-MM-DD)`},
		{name: "a dated slot whose from is not before its to, named by its place", maintenance: map[string]MaintenanceWindow{"prd": {TimeZone: "America/Chicago", Dates: []DatedSlot{{On: "2026-11-15", From: "22:00", To: "23:30"}, {On: "2026-11-15", From: "23:00", To: "01:00"}}}}, wantErr: "maintenance.prd.dates[1]: from 23:00 is not before to 01:00; a dated slot is on one date, so a slot past midnight is the next date's own"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := valid()
			p.Maintenance = tt.maintenance
			err := p.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() error = %v, want none", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %v, wantErr %q", err, tt.wantErr)
			}
		})
	}
}

func TestMaintenanceJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		json string
		want MaintenanceWindow
		// wantWritten is the setting as WritePlacement spells it again.
		wantWritten string
		wantErr     string
	}{
		{name: "the word", json: `"anytime"`, want: MaintenanceWindow{Anytime: true}, wantWritten: `"anytime"`},
		{
			name:        "the object, in the file's order",
			json:        `{"timeZone": "America/Chicago", "weekly": [{"day": "Sunday", "from": "02:00", "to": "04:00"}], "dates": [{"on": "2026-11-15", "from": "22:00", "to": "23:30"}], "releases": "all"}`,
			want:        MaintenanceWindow{TimeZone: "America/Chicago", Weekly: []WeeklySlot{{Day: "Sunday", From: "02:00", To: "04:00"}}, Dates: []DatedSlot{{On: "2026-11-15", From: "22:00", To: "23:30"}}, Releases: ReleasesAll},
			wantWritten: `{"timeZone":"America/Chicago","weekly":[{"day":"Sunday","from":"02:00","to":"04:00"}],"dates":[{"on":"2026-11-15","from":"22:00","to":"23:30"}],"releases":"all"}`,
		},
		{
			name:        "an object with slots alone leaves releases out",
			json:        `{"timeZone": "America/Chicago", "weekly": [{"day": "Sunday", "from": "02:00", "to": "04:00"}]}`,
			want:        sunday(""),
			wantWritten: `{"timeZone":"America/Chicago","weekly":[{"day":"Sunday","from":"02:00","to":"04:00"}]}`,
		},
		{name: "another word", json: `"sometimes"`, wantErr: `"sometimes" is not a maintenance setting: the word "anytime", or an object with timeZone, weekly, dates and releases`},
		{name: "an unknown field", json: `{"timeZone": "America/Chicago", "daily": []}`, wantErr: `a maintenance setting is the word "anytime" or an object with timeZone, weekly, dates and releases`},
		{name: "a number", json: `7`, wantErr: "a maintenance setting is the word"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got MaintenanceWindow
			err := json.Unmarshal([]byte(tt.json), &got)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Unmarshal() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Unmarshal() mismatch (-want +got):\n%s", diff)
			}
			written, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(written) != tt.wantWritten {
				t.Errorf("Marshal() = %s, want %s", written, tt.wantWritten)
			}
		})
	}
}

// TestMaintenanceRoundTrip reads a placement with maintenance settings and writes it
// again the way bedrock upgrade does: the settings survive as written.
func TestMaintenanceRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// maintenance is the block in the file; want what the placement holds.
		maintenance string
		want        map[string]MaintenanceWindow
	}{
		{name: "no block", want: nil},
		{name: "a word and an object", maintenance: `{"tst": "anytime", "prd": {"timeZone": "America/Chicago", "weekly": [{"day": "Sunday", "from": "02:00", "to": "04:00"}], "releases": "all"}}`, want: map[string]MaintenanceWindow{"tst": {Anytime: true}, "prd": sunday(ReleasesAll)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			text := `{"prefix": "imp", "environments": ["tst", "stg", "prd"], "regions": [{"name": "us-central1", "code": "uc1"}], "appsDomain": "example.dev", "hostedDomain": "example.com", "stateBucket": "b", "placeholderImage": "i", "defaultBranch": "master", "repository": "quill", "releaseApp": "release-app"`
			if tt.maintenance != "" {
				text += `, "maintenance": ` + tt.maintenance
			}
			file := filepath.Join(t.TempDir(), "placement.json")
			if err := os.WriteFile(file, []byte(text+"}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			p, err := ReadPlacement(file)
			if err != nil {
				t.Fatalf("ReadPlacement() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, p.Maintenance); diff != "" {
				t.Errorf("Maintenance mismatch (-want +got):\n%s", diff)
			}
			if err := WritePlacement(file, p); err != nil {
				t.Fatalf("WritePlacement() error = %v", err)
			}
			again, err := ReadPlacement(file)
			if err != nil {
				t.Fatalf("ReadPlacement() after the write error = %v", err)
			}
			if diff := cmp.Diff(p.Maintenance, again.Maintenance); diff != "" {
				t.Errorf("the written placement reads back differently (-want +got):\n%s", diff)
			}
			written, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if tt.maintenance != "" && !strings.Contains(string(written), "\"tst\": \"anytime\"") {
				t.Errorf("the word is not written as a word:\n%s", written)
			}
			if tt.maintenance == "" && strings.Contains(string(written), "maintenance") {
				t.Errorf("an absent block is written:\n%s", written)
			}
		})
	}
}

func TestMaintenanceSetting(t *testing.T) {
	t.Parallel()

	p := &Placement{Environments: []string{"tst", "stg", "prd"}, Maintenance: map[string]MaintenanceWindow{"stg": sunday("")}}
	tests := []struct {
		name    string
		env     string
		want    MaintenanceWindow
		written bool
	}{
		{name: "an environment below production is anytime unless written", env: "tst", want: MaintenanceWindow{Anytime: true}, written: true},
		{name: "a written setting is answered as written", env: "stg", want: sunday(""), written: true},
		{name: "production has no default", env: "prd", written: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, written := p.MaintenanceSetting(tt.env)
			if written != tt.written {
				t.Errorf("MaintenanceSetting() written = %v, want %v", written, tt.written)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("MaintenanceSetting() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMaintenanceOpening(t *testing.T) {
	t.Parallel()

	midnight := MaintenanceWindow{TimeZone: "America/Chicago", Weekly: []WeeklySlot{{Day: "Saturday", From: "22:00", To: "02:00"}}}
	dated := MaintenanceWindow{TimeZone: "America/Chicago", Dates: []DatedSlot{{On: "2026-11-15", From: "22:00", To: "23:30"}}}
	overlapping := MaintenanceWindow{TimeZone: "America/Chicago", Weekly: []WeeklySlot{{Day: "Sunday", From: "02:00", To: "04:00"}, {Day: "Sunday", From: "03:00", To: "06:00"}}}
	both := MaintenanceWindow{TimeZone: "America/Chicago", Weekly: []WeeklySlot{{Day: "Sunday", From: "02:00", To: "04:00"}}, Dates: []DatedSlot{{On: "2026-10-07", From: "22:00", To: "23:00"}}}
	tests := []struct {
		name   string
		window MaintenanceWindow
		now    time.Time
		// want is the opening, its times compared as instants.
		want Opening
	}{
		{name: "anytime is always open", window: MaintenanceWindow{Anytime: true}, now: inChicago(t, 10, 5, 10, 0), want: Opening{Open: true, Slot: "anytime"}},
		{
			name: "inside a weekly slot", window: sunday(""), now: inChicago(t, 10, 4, 3, 0),
			want: Opening{Open: true, From: inChicago(t, 10, 4, 2, 0), To: inChicago(t, 10, 4, 4, 0), Slot: "Sunday 02:00 to 04:00 America/Chicago"},
		},
		{
			name: "at the slot's start it is open", window: sunday(""), now: inChicago(t, 10, 4, 2, 0),
			want: Opening{Open: true, From: inChicago(t, 10, 4, 2, 0), To: inChicago(t, 10, 4, 4, 0), Slot: "Sunday 02:00 to 04:00 America/Chicago"},
		},
		{
			name: "at the slot's end it has closed, and the next week's is ahead", window: sunday(""), now: inChicago(t, 10, 4, 4, 0),
			want: Opening{From: inChicago(t, 10, 11, 2, 0), To: inChicago(t, 10, 11, 4, 0), Slot: "Sunday 02:00 to 04:00 America/Chicago"},
		},
		{
			name: "the evening before, the slot is hours ahead", window: sunday(""), now: inChicago(t, 10, 3, 20, 0),
			want: Opening{From: inChicago(t, 10, 4, 2, 0), To: inChicago(t, 10, 4, 4, 0), Slot: "Sunday 02:00 to 04:00 America/Chicago"},
		},
		{
			name: "the slot keeps its wall-clock hour across the daylight-saving change, before it", window: sunday(""), now: time.Date(2026, 10, 4, 6, 0, 0, 0, time.UTC),
			want: Opening{From: time.Date(2026, 10, 4, 7, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC), Slot: "Sunday 02:00 to 04:00 America/Chicago"},
		},
		{
			name: "and after it, an hour later in UTC", window: sunday(""), now: time.Date(2026, 12, 6, 7, 0, 0, 0, time.UTC),
			want: Opening{From: time.Date(2026, 12, 6, 8, 0, 0, 0, time.UTC), To: time.Date(2026, 12, 6, 10, 0, 0, 0, time.UTC), Slot: "Sunday 02:00 to 04:00 America/Chicago"},
		},
		{
			name: "a slot crossing midnight is open after midnight", window: midnight, now: inChicago(t, 10, 4, 1, 0),
			want: Opening{Open: true, From: inChicago(t, 10, 3, 22, 0), To: inChicago(t, 10, 4, 2, 0), Slot: "Saturday 22:00 to 02:00 America/Chicago"},
		},
		{
			name: "a slot crossing midnight ends the next day", window: midnight, now: inChicago(t, 10, 3, 21, 0),
			want: Opening{From: inChicago(t, 10, 3, 22, 0), To: inChicago(t, 10, 4, 2, 0), Slot: "Saturday 22:00 to 02:00 America/Chicago"},
		},
		{
			name: "a dated slot before its hour", window: dated, now: inChicago(t, 11, 15, 21, 0),
			want: Opening{From: inChicago(t, 11, 15, 22, 0), To: inChicago(t, 11, 15, 23, 30), Slot: "the dated slot 2026-11-15 22:00 to 23:30 America/Chicago"},
		},
		{
			name: "a dated slot during its hour", window: dated, now: inChicago(t, 11, 15, 22, 30),
			want: Opening{Open: true, From: inChicago(t, 11, 15, 22, 0), To: inChicago(t, 11, 15, 23, 30), Slot: "the dated slot 2026-11-15 22:00 to 23:30 America/Chicago"},
		},
		{name: "a dated slot after its hour has nothing ahead", window: dated, now: inChicago(t, 11, 15, 23, 45), want: Opening{}},
		{
			name: "overlapping slots answer the one closing last", window: overlapping, now: inChicago(t, 10, 4, 3, 30),
			want: Opening{Open: true, From: inChicago(t, 10, 4, 3, 0), To: inChicago(t, 10, 4, 6, 0), Slot: "Sunday 03:00 to 06:00 America/Chicago"},
		},
		{
			name: "a dated slot ahead of the next weekly one is answered first", window: both, now: inChicago(t, 10, 5, 10, 0),
			want: Opening{From: inChicago(t, 10, 7, 22, 0), To: inChicago(t, 10, 7, 23, 0), Slot: "the dated slot 2026-10-07 22:00 to 23:00 America/Chicago"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.window.Opening(tt.now)
			if err != nil {
				t.Fatalf("Opening() error = %v", err)
			}
			if got.Open != tt.want.Open || !got.From.Equal(tt.want.From) || !got.To.Equal(tt.want.To) || got.Slot != tt.want.Slot {
				t.Errorf("Opening() = {Open %v, From %s, To %s, Slot %q}, want {Open %v, From %s, To %s, Slot %q}", got.Open, got.From, got.To, got.Slot, tt.want.Open, tt.want.From, tt.want.To, tt.want.Slot)
			}
		})
	}
}

func TestMaintenancePassedDates(t *testing.T) {
	t.Parallel()

	window := MaintenanceWindow{TimeZone: "America/Chicago", Dates: []DatedSlot{{On: "2026-09-15", From: "22:00", To: "23:30"}, {On: "2026-11-15", From: "22:00", To: "23:30"}}}
	tests := []struct {
		name string
		now  time.Time
		want []DatedSlot
	}{
		{name: "before both, none has passed", now: inChicago(t, 9, 1, 0, 0)},
		{name: "between them, the first has", now: inChicago(t, 10, 5, 10, 0), want: []DatedSlot{{On: "2026-09-15", From: "22:00", To: "23:30"}}},
		{name: "a slot still open has not passed", now: inChicago(t, 11, 15, 23, 0), want: []DatedSlot{{On: "2026-09-15", From: "22:00", To: "23:30"}}},
		{name: "after both, both have", now: inChicago(t, 12, 1, 0, 0), want: []DatedSlot{{On: "2026-09-15", From: "22:00", To: "23:30"}, {On: "2026-11-15", From: "22:00", To: "23:30"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, window.PassedDates(tt.now)); diff != "" {
				t.Errorf("PassedDates() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMaintenanceString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		window MaintenanceWindow
		want   string
	}{
		{name: "anytime", window: MaintenanceWindow{Anytime: true}, want: "anytime"},
		{name: "a weekly slot", window: sunday(""), want: "Sunday 02:00 to 04:00 America/Chicago"},
		{name: "every kind of slot, every release", window: MaintenanceWindow{TimeZone: "America/Chicago", Weekly: []WeeklySlot{{Day: "Sunday", From: "02:00", To: "04:00"}}, Dates: []DatedSlot{{On: "2026-11-15", From: "22:00", To: "23:30"}}, Releases: ReleasesAll}, want: "Sunday 02:00 to 04:00, 2026-11-15 22:00 to 23:30 America/Chicago, every release"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.window.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
