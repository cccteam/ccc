package integration

// This suite pins a filter value on a date or timestamp column read as a person writes
// it. On the mission board's deadline, a timestamp, a day (2026-12-15 or 12/15/2026), a
// month or a minute is the whole of it: eq inside the span, ne outside, lte before its
// end, gte from its start, an in list ORing the days; a day is read in the person's zone,
// sent in the request's Time-Zone header, and in UTC without one. On the consignments'
// expiry date, a date column filterable beside the indexed release date, a day is the
// day and a month or a year its days, with no zone to read; the release timestamp, which
// declares no zone, reads a written year in UTC. What cannot be read is refused in plain
// words naming the forms, a time of day on a date column among them, and a header naming
// no zone is refused in the header's name. The expected rows are computed from the
// unfiltered lists.
//
// Demonstrates: filter.written-dates.

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

func TestFilterWrittenDates(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	kiritimati, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Fatal(err)
	}
	idKey := func(row map[string]any) string { return cell[string](t, row, "id") }
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
	either := func(keep ...func(map[string]any) bool) func(map[string]any) bool {
		return func(row map[string]any) bool {
			for _, k := range keep {
				if k(row) {
					return true
				}
			}

			return false
		}
	}
	not := func(keep func(map[string]any) bool) func(map[string]any) bool {
		return func(row map[string]any) bool { return !keep(row) }
	}
	usForm := func(day civil.Date) string {
		return fmt.Sprintf("%d/%d/%d", day.Month, day.Day, day.Year)
	}

	// The board, as the marshal: every Anvil mission with its deadline, in the view's
	// declared order. The middle board's deadline picks the day, the month and the minute
	// the cases write, and the day the same instant falls on fourteen hours ahead of UTC.
	boards := listRows(t, h, "marshal", sectorPath(anvil, "mission-boards?limit=200"))
	if len(boards) < 8 {
		t.Fatalf("the seed holds %d Anvil boards, want at least 8", len(boards))
	}
	deadlineOf := func(row map[string]any) time.Time {
		at, err := time.Parse(time.RFC3339Nano, cell[string](t, row, "deadline"))
		if err != nil {
			t.Fatalf("deadline %q: %v", row["deadline"], err)
		}

		return at
	}
	middle := deadlineOf(boards[len(boards)/2])
	day := civil.DateOf(middle.UTC())
	nextDay := day.AddDays(1)
	farDay := civil.DateOf(middle.In(kiritimati))
	monthText := fmt.Sprintf("%04d-%02d", day.Year, day.Month)
	monthEnd := time.Date(day.Year, day.Month+1, 1, 0, 0, 0, 0, time.UTC)
	minute := middle.UTC().Truncate(time.Minute)
	farDayStart := time.Date(farDay.Year, farDay.Month, farDay.Day, 0, 0, 0, 0, kiritimati)
	onDay := func(zone *time.Location, d civil.Date) func(map[string]any) bool {
		return func(row map[string]any) bool { return civil.DateOf(deadlineOf(row).In(zone)) == d }
	}
	before := func(at time.Time) func(map[string]any) bool {
		return func(row map[string]any) bool { return deadlineOf(row).Before(at) }
	}
	from := func(at time.Time) func(map[string]any) bool {
		return func(row map[string]any) bool { return !deadlineOf(row).Before(at) }
	}
	boardsURL := func(expression string) string {
		return sectorPath(anvil, "mission-boards?limit=200&"+filter(expression))
	}

	// The hold, as the supercargo: the consignments with their expiry date and release
	// timestamp. The middle released hold's expiry picks the day, month and year.
	holds := listRows(t, h, "supercargo", sectorPath(anvil, "consignments?limit=100"))
	expiresOf := func(row map[string]any) civil.Date {
		on, err := civil.ParseDate(cell[string](t, row, "expiresOn"))
		if err != nil {
			t.Fatalf("expiresOn %q: %v", row["expiresOn"], err)
		}

		return on
	}
	released := func(row map[string]any) bool {
		at, ok := row["releasedAt"]

		return ok && at != nil
	}
	releasedAtOf := func(row map[string]any) time.Time {
		at, err := time.Parse(time.RFC3339Nano, cell[string](t, row, "releasedAt"))
		if err != nil {
			t.Fatalf("releasedAt %q: %v", row["releasedAt"], err)
		}

		return at
	}
	releasedHolds := selectRows(holds, released)
	if len(releasedHolds) < 3 {
		t.Fatalf("the seed holds %d released Anvil consignments, want at least 3", len(releasedHolds))
	}
	pivot := expiresOf(releasedHolds[len(releasedHolds)/2])
	pivotMonth := fmt.Sprintf("%04d-%02d", pivot.Year, pivot.Month)
	monthFirst := civil.Date{Year: pivot.Year, Month: pivot.Month, Day: 1}
	monthLast := civil.DateOf(time.Date(pivot.Year, pivot.Month+1, 0, 0, 0, 0, 0, time.UTC))
	expiresIn := func(first, last civil.Date) func(map[string]any) bool {
		return func(row map[string]any) bool {
			on := expiresOf(row)

			return !on.Before(first) && !on.After(last)
		}
	}
	expiresBy := func(last civil.Date) func(map[string]any) bool {
		return func(row map[string]any) bool { return !expiresOf(row).After(last) }
	}
	expiresFrom := func(first civil.Date) func(map[string]any) bool {
		return func(row map[string]any) bool { return !expiresOf(row).Before(first) }
	}
	slices.SortFunc(releasedHolds, func(a, b map[string]any) int { return releasedAtOf(a).Compare(releasedAtOf(b)) })
	releaseYear := releasedAtOf(releasedHolds[0]).UTC().Year()
	releasedFrom := func(at time.Time) func(map[string]any) bool {
		return func(row map[string]any) bool { return released(row) && !releasedAtOf(row).Before(at) }
	}
	holdsURL := func(expression string) string {
		return sectorPath(anvil, "consignments?limit=100&"+filter(expression))
	}

	tests := []struct {
		name       string
		user       accesstypes.User
		target     string
		zone       string // the person's zone, sent as the Time-Zone header
		wantStatus int
		wantBody   string           // a 4xx answer's text
		wantRows   []map[string]any // the rows expected, computed from the lists
	}{
		// The board's deadline, a timestamp with no declared zone.
		{name: "a day on the deadline is the whole UTC day", user: "marshal", target: boardsURL("deadline:eq:" + day.String()), wantStatus: http.StatusOK, wantRows: selectRows(boards, onDay(time.UTC, day))},
		{name: "the US form is the same day", user: "marshal", target: boardsURL("deadline:eq:" + usForm(day)), wantStatus: http.StatusOK, wantRows: selectRows(boards, onDay(time.UTC, day))},
		{name: "ne the day is every other board", user: "marshal", target: boardsURL("deadline:ne:" + day.String()), wantStatus: http.StatusOK, wantRows: selectRows(boards, not(onDay(time.UTC, day)))},
		{name: "lte the month is before the month's end", user: "marshal", target: boardsURL("deadline:lte:" + monthText), wantStatus: http.StatusOK, wantRows: selectRows(boards, before(monthEnd))},
		{name: "a minute is the whole minute", user: "marshal", target: boardsURL("deadline:eq:" + minute.Format("2006-01-02T15:04")), wantStatus: http.StatusOK, wantRows: selectRows(boards, all(from(minute), before(minute.Add(time.Minute))))},
		{name: "an in list ORs two days", user: "marshal", target: boardsURL("deadline:in:(" + day.String() + "," + nextDay.String() + ")"), wantStatus: http.StatusOK, wantRows: selectRows(boards, either(onDay(time.UTC, day), onDay(time.UTC, nextDay)))},
		{name: "the day in the person's zone, fourteen hours ahead of UTC", user: "marshal", target: boardsURL("deadline:eq:" + farDay.String()), zone: "Pacific/Kiritimati", wantStatus: http.StatusOK, wantRows: selectRows(boards, onDay(kiritimati, farDay))},
		{name: "gte the day in the person's zone is from that zone's midnight", user: "marshal", target: boardsURL("deadline:gte:" + farDay.String()), zone: "Pacific/Kiritimati", wantStatus: http.StatusOK, wantRows: selectRows(boards, from(farDayStart))},
		{name: "a word is refused naming the forms", user: "marshal", target: boardsURL("deadline:lt:yesterday"), wantStatus: http.StatusBadRequest, wantBody: "deadline: 'yesterday' is not a date. Write it as "},
		{name: "a header naming no zone is refused in the header's name", user: "marshal", target: boardsURL("deadline:eq:" + day.String()), zone: "Mars/Olympus", wantStatus: http.StatusBadRequest, wantBody: "Time-Zone: 'Mars/Olympus' is not a time zone name. Write it as America/New_York."},

		// The hold's expiry, a date column, beside the indexed release timestamp.
		{name: "a month on the expiry date is its days", user: "supercargo", target: holdsURL("releasedAt:isnotnull,expiresOn:eq:" + pivotMonth), wantStatus: http.StatusOK, wantRows: selectRows(holds, all(released, expiresIn(monthFirst, monthLast)))},
		{name: "lte a US day on the expiry date is through that day", user: "supercargo", target: holdsURL("releasedAt:isnotnull,expiresOn:lte:" + usForm(pivot)), wantStatus: http.StatusOK, wantRows: selectRows(holds, all(released, expiresBy(pivot)))},
		{name: "gte a year on the expiry date is from its first day", user: "supercargo", target: holdsURL(fmt.Sprintf("releasedAt:isnotnull,expiresOn:gte:%d", pivot.Year)), wantStatus: http.StatusOK, wantRows: selectRows(holds, all(released, expiresFrom(civil.Date{Year: pivot.Year, Month: 1, Day: 1})))},
		{name: "an in list on the expiry date, a day and a year", user: "supercargo", target: holdsURL(fmt.Sprintf("releasedAt:isnotnull,expiresOn:in:(%s,%d)", pivot, pivot.Year+1)), wantStatus: http.StatusOK, wantRows: selectRows(holds, all(released, either(expiresIn(pivot, pivot), expiresIn(civil.Date{Year: pivot.Year + 1, Month: 1, Day: 1}, civil.Date{Year: pivot.Year + 1, Month: 12, Day: 31}))))},
		{name: "the release timestamp reads a written year in UTC", user: "supercargo", target: holdsURL(fmt.Sprintf("releasedAt:gte:%d", releaseYear)), wantStatus: http.StatusOK, wantRows: selectRows(holds, releasedFrom(time.Date(releaseYear, 1, 1, 0, 0, 0, 0, time.UTC)))},
		{name: "a time of day on a date column is refused", user: "supercargo", target: holdsURL("releasedAt:isnotnull,expiresOn:eq:2026-12-15T09:30"), wantStatus: http.StatusBadRequest, wantBody: "expiresOn: '2026-12-15T09:30' is not a date. Write it as "},
		{name: "the expiry date alone is still a companion filter", user: "supercargo", target: holdsURL("expiresOn:eq:" + pivotMonth), wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			headers := map[string]string{}
			if tt.zone != "" {
				headers[resource.TimeZoneHeader] = tt.zone
			}
			rr := doRequestRecordedWithHeaders(t, h, tt.user, http.MethodGet, tt.target, "", headers)
			status, body := rr.Code, rr.Body.Bytes()
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantBody != "" && !strings.Contains(string(body), tt.wantBody) {
				t.Errorf("body = %s, want it to say %q", body, tt.wantBody)
			}
			if status != http.StatusOK {
				return
			}
			got := keysOf(decodeRows(t, body), idKey)
			want := keysOf(tt.wantRows, idKey)
			if len(want) == 0 {
				t.Fatalf("the case expects no rows; a case over the computed sets must expect some")
			}
			if !slices.Equal(got, want) {
				t.Errorf("rows = %v, want %v", got, want)
			}
		})
	}
}
