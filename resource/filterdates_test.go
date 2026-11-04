package resource

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc/accesstypes"
)

// TestParseWrittenDate pins the forms a filter reads a date or time in, and what each
// names: the day in ISO, US or eight-digit form, the month, the year, and a day with a
// time of day to the minute or the second; and what is refused, a day or a time that
// does not exist and anything else.
func TestParseWrittenDate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text   string
		want   writtenDate
		wantOK bool
	}{
		{text: "2026", want: writtenDate{year: 2026, month: 1, day: 1, unit: dateYear}, wantOK: true},
		{text: "2026-12", want: writtenDate{year: 2026, month: 12, day: 1, unit: dateMonth}, wantOK: true},
		{text: "12/2026", want: writtenDate{year: 2026, month: 12, day: 1, unit: dateMonth}, wantOK: true},
		{text: "2026-12-15", want: writtenDate{year: 2026, month: 12, day: 15, unit: dateDay}, wantOK: true},
		{text: "2026-2-5", want: writtenDate{year: 2026, month: 2, day: 5, unit: dateDay}, wantOK: true},
		{text: "12/15/2026", want: writtenDate{year: 2026, month: 12, day: 15, unit: dateDay}, wantOK: true},
		{text: "20261215", want: writtenDate{year: 2026, month: 12, day: 15, unit: dateDay}, wantOK: true},
		{text: " 2026-12-15 ", want: writtenDate{year: 2026, month: 12, day: 15, unit: dateDay}, wantOK: true},
		{text: "2026-12-15T09:30", want: writtenDate{year: 2026, month: 12, day: 15, hour: 9, minute: 30, unit: dateMinute}, wantOK: true},
		{text: "2026-12-15 09:30", want: writtenDate{year: 2026, month: 12, day: 15, hour: 9, minute: 30, unit: dateMinute}, wantOK: true},
		{text: "2026-12-15T09:30:15", want: writtenDate{year: 2026, month: 12, day: 15, hour: 9, minute: 30, second: 15, unit: dateSecond}, wantOK: true},
		{text: "2028-02-29", want: writtenDate{year: 2028, month: 2, day: 29, unit: dateDay}, wantOK: true},
		{text: "2026-02-29"},
		{text: "2026-13"},
		{text: "13/2026"},
		{text: "2026-12-15T24:00"},
		{text: "2026-12-15T09:60"},
		{text: "2026-12-15T09:30:60"},
		{text: "2026-12-15T09:30:00Z"},
		{text: "15/12/2026"},
		{text: "yesterday"},
		{text: "202612"},
		{text: ""},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()

			got, ok := parseWrittenDate(tt.text)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("parseWrittenDate(%q) = %+v, %v, want %+v, %v", tt.text, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// TestWrittenDate_Spans pins what a written value spans: on a timestamp column the
// half-open run of instants in the zone, a day that loses an hour to daylight saving
// included; on a date column the first and last day.
func TestWrittenDate_Spans(t *testing.T) {
	t.Parallel()

	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	day := func(text string) writtenDate {
		t.Helper()
		written, ok := parseWrittenDate(text)
		if !ok {
			t.Fatalf("parseWrittenDate(%q) refused", text)
		}

		return written
	}

	start, end := day("2026-03-08").span(newYork)
	if !start.Equal(time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC)) || !end.Equal(time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC)) {
		t.Errorf("the day daylight saving starts spans %v to %v, want 05:00Z to 04:00Z the next day", start, end)
	}
	start, end = day("2026-12").span(time.UTC)
	if !start.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) || !end.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("December spans %v to %v", start, end)
	}
	start, end = day("2026-12-15T09:30").span(time.UTC)
	if end.Sub(start) != time.Minute {
		t.Errorf("a minute spans %v", end.Sub(start))
	}

	first, last := day("2026-02").days()
	if first != (civil.Date{Year: 2026, Month: 2, Day: 1}) || last != (civil.Date{Year: 2026, Month: 2, Day: 28}) {
		t.Errorf("February 2026 = %v to %v", first, last)
	}
	first, last = day("2028").days()
	if first != (civil.Date{Year: 2028, Month: 1, Day: 1}) || last != (civil.Date{Year: 2028, Month: 12, Day: 31}) {
		t.Errorf("2028 = %v to %v", first, last)
	}
}

// TestParser_WrittenDates pins the tree a written date becomes on a timestamp and on a
// date column, each operator reading the span as the person means it: eq inside, ne
// outside, lt before the start, lte before the end, gt from the end, gte from the start,
// an in list ORed and a notin list ANDed; an RFC 3339 instant and a day on a date column
// stay one comparison. Without a zone the timestamps read in UTC.
func TestParser_WrittenDates(t *testing.T) {
	t.Parallel()

	utc := func(year, month, day, hour, minute, second int) time.Time {
		return time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
	}
	date := func(year, month, day int) civil.Date {
		return civil.Date{Year: year, Month: time.Month(month), Day: day}
	}
	leaf := func(field, operator string, value any) ExpressionNode {
		return &ConditionNode{Condition: Condition{Field: field, Operator: operator, Value: value}}
	}
	and := func(left, right ExpressionNode) ExpressionNode {
		return &GroupNode{Expression: &LogicalOpNode{Left: left, Operator: OperatorAnd, Right: right}}
	}
	or := func(left, right ExpressionNode) ExpressionNode {
		return &GroupNode{Expression: &LogicalOpNode{Left: left, Operator: OperatorOr, Right: right}}
	}

	tests := []struct {
		name         string
		filterString string
		wantNode     ExpressionNode
	}{
		{name: "a day on a timestamp column is the whole day", filterString: "deadline:eq:2026-12-15", wantNode: and(leaf("Deadline", gteStr, utc(2026, 12, 15, 0, 0, 0)), leaf("Deadline", ltStr, utc(2026, 12, 16, 0, 0, 0)))},
		{name: "a US date is the same day", filterString: "deadline:eq:12/15/2026", wantNode: and(leaf("Deadline", gteStr, utc(2026, 12, 15, 0, 0, 0)), leaf("Deadline", ltStr, utc(2026, 12, 16, 0, 0, 0)))},
		{name: "eight digits are the same day", filterString: "deadline:eq:20261215", wantNode: and(leaf("Deadline", gteStr, utc(2026, 12, 15, 0, 0, 0)), leaf("Deadline", ltStr, utc(2026, 12, 16, 0, 0, 0)))},
		{name: "ne a day is outside it", filterString: "deadline:ne:2026-12-15", wantNode: or(leaf("Deadline", ltStr, utc(2026, 12, 15, 0, 0, 0)), leaf("Deadline", gteStr, utc(2026, 12, 16, 0, 0, 0)))},
		{name: "lt a month is before its start", filterString: "deadline:lt:2026-12", wantNode: leaf("Deadline", ltStr, utc(2026, 12, 1, 0, 0, 0))},
		{name: "lte a month is before its end", filterString: "deadline:lte:12/2026", wantNode: leaf("Deadline", ltStr, utc(2027, 1, 1, 0, 0, 0))},
		{name: "gt a year is from its end", filterString: "deadline:gt:2026", wantNode: leaf("Deadline", gteStr, utc(2027, 1, 1, 0, 0, 0))},
		{name: "gte a day is from its start", filterString: "deadline:gte:2026-12-15", wantNode: leaf("Deadline", gteStr, utc(2026, 12, 15, 0, 0, 0))},
		{name: "a minute is the whole minute", filterString: "deadline:eq:2026-12-15T09:30", wantNode: and(leaf("Deadline", gteStr, utc(2026, 12, 15, 9, 30, 0)), leaf("Deadline", ltStr, utc(2026, 12, 15, 9, 31, 0)))},
		{name: "a second, written with a space, is the whole second", filterString: `deadline:eq:"2026-12-15 09:30:15"`, wantNode: and(leaf("Deadline", gteStr, utc(2026, 12, 15, 9, 30, 15)), leaf("Deadline", ltStr, utc(2026, 12, 15, 9, 30, 16)))},
		{name: "an RFC 3339 instant stays an instant", filterString: "deadline:eq:2026-12-15T09:30:00Z", wantNode: leaf("Deadline", eqStr, utc(2026, 12, 15, 9, 30, 0))},
		{name: "an in list ORs the spans, an instant among them", filterString: "deadline:in:(2026-12-15,2026-12-16T09:30:00Z)", wantNode: &GroupNode{Expression: &LogicalOpNode{Left: and(leaf("Deadline", gteStr, utc(2026, 12, 15, 0, 0, 0)), leaf("Deadline", ltStr, utc(2026, 12, 16, 0, 0, 0))), Operator: OperatorOr, Right: leaf("Deadline", eqStr, utc(2026, 12, 16, 9, 30, 0))}}},
		{name: "a notin list ANDs the outsides", filterString: "deadline:notin:(2026-12,2027)", wantNode: &GroupNode{Expression: &LogicalOpNode{Left: or(leaf("Deadline", ltStr, utc(2026, 12, 1, 0, 0, 0)), leaf("Deadline", gteStr, utc(2027, 1, 1, 0, 0, 0))), Operator: OperatorAnd, Right: or(leaf("Deadline", ltStr, utc(2027, 1, 1, 0, 0, 0)), leaf("Deadline", gteStr, utc(2028, 1, 1, 0, 0, 0)))}}},
		{name: "an in list of instants stays one condition", filterString: "deadline:in:(2026-12-15T09:30:00Z,2026-12-16T09:30:00Z)", wantNode: &ConditionNode{Condition: Condition{Field: "Deadline", Operator: inStr, Values: []any{utc(2026, 12, 15, 9, 30, 0), utc(2026, 12, 16, 9, 30, 0)}}}},
		{name: "a day on a date column is the day", filterString: "released_on:eq:12/15/2026", wantNode: leaf("ReleasedOn", eqStr, date(2026, 12, 15))},
		{name: "a month on a date column is its days", filterString: "released_on:eq:2026-02", wantNode: and(leaf("ReleasedOn", gteStr, date(2026, 2, 1)), leaf("ReleasedOn", lteStr, date(2026, 2, 28)))},
		{name: "ne a year on a date column is outside its days", filterString: "released_on:ne:2026", wantNode: or(leaf("ReleasedOn", ltStr, date(2026, 1, 1)), leaf("ReleasedOn", gtStr, date(2026, 12, 31)))},
		{name: "gt a month on a date column is after its last day", filterString: "released_on:gt:2/2026", wantNode: leaf("ReleasedOn", gtStr, date(2026, 2, 28))},
		{name: "lte a year on a date column is through its last day", filterString: "released_on:lte:2026", wantNode: leaf("ReleasedOn", lteStr, date(2026, 12, 31))},
		{name: "lt a month on a date column is before its first day", filterString: "released_on:lt:2026-02", wantNode: leaf("ReleasedOn", ltStr, date(2026, 2, 1))},
		{name: "gte a leap year's month on a date column", filterString: "released_on:gte:2028-02", wantNode: leaf("ReleasedOn", gteStr, date(2028, 2, 1))},
		{name: "an in list on a date column, a day and a year", filterString: "released_on:in:(2026-12-01,2027)", wantNode: &GroupNode{Expression: &LogicalOpNode{Left: leaf("ReleasedOn", eqStr, date(2026, 12, 1)), Operator: OperatorOr, Right: and(leaf("ReleasedOn", gteStr, date(2027, 1, 1)), leaf("ReleasedOn", lteStr, date(2027, 12, 31)))}}},
		{name: "a span beside another condition keeps its own parentheses", filterString: "status:eq:open,deadline:ne:2026-12-15", wantNode: &LogicalOpNode{Left: leaf("Status", eqStr, "open"), Operator: OperatorAnd, Right: or(leaf("Deadline", ltStr, utc(2026, 12, 15, 0, 0, 0)), leaf("Deadline", gteStr, utc(2026, 12, 16, 0, 0, 0)))}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parser, err := NewFilterParser(NewFilterLexer(tt.filterString), defaultTestJSONToSQLNameMap)
			if err != nil {
				t.Fatalf("NewFilterParser() error = %v", err)
			}
			got, err := parser.Parse(SpannerDBType)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.filterString, err)
			}
			if got.String() != tt.wantNode.String() {
				t.Errorf("Parse(%q)\n got = %s\nwant = %s", tt.filterString, got, tt.wantNode)
			}
		})
	}
}

// TestParser_WrittenDateZones pins the zone a written date on a timestamp column is
// read in: the request's where the browser sent one, else the zone the column's date
// search fact declares, else UTC; a declared zone that does not load falls to UTC too.
func TestParser_WrittenDateZones(t *testing.T) {
	t.Parallel()

	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	withDeclaredZone := func(zone string) map[jsonFieldName]FilterFieldInfo {
		fields := map[jsonFieldName]FilterFieldInfo{}
		for name, info := range defaultTestJSONToSQLNameMap {
			fields[name] = info
		}
		deadline := fields["deadline"]
		deadline.Search = &SearchSpec{Token: "Deadline_Search", Index: "DeadlinesSearch", Role: SearchRoleDate, Zone: zone}
		fields["deadline"] = deadline

		return fields
	}

	tests := []struct {
		name         string
		filterString string
		fields       map[jsonFieldName]FilterFieldInfo
		zone         *time.Location
		wantStart    time.Time
		wantEnd      time.Time
	}{
		{name: "no zone anywhere reads UTC", filterString: "deadline:eq:2026-12-15", fields: defaultTestJSONToSQLNameMap, wantStart: time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC), wantEnd: time.Date(2026, 12, 16, 0, 0, 0, 0, time.UTC)},
		{name: "the request's zone reads the person's day", filterString: "deadline:eq:2026-12-15", fields: defaultTestJSONToSQLNameMap, zone: newYork, wantStart: time.Date(2026, 12, 15, 5, 0, 0, 0, time.UTC), wantEnd: time.Date(2026, 12, 16, 5, 0, 0, 0, time.UTC)},
		{name: "the request's zone carries a daylight-saving change", filterString: "deadline:eq:2026-03-08", fields: defaultTestJSONToSQLNameMap, zone: newYork, wantStart: time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC), wantEnd: time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC)},
		{name: "the column's declared zone serves without a request zone", filterString: "deadline:eq:2026-12-15", fields: withDeclaredZone("Asia/Tokyo"), wantStart: time.Date(2026, 12, 14, 15, 0, 0, 0, time.UTC), wantEnd: time.Date(2026, 12, 15, 15, 0, 0, 0, time.UTC)},
		{name: "the request's zone outranks the declared one", filterString: "deadline:eq:2026-12-15", fields: withDeclaredZone("Asia/Tokyo"), zone: newYork, wantStart: time.Date(2026, 12, 15, 5, 0, 0, 0, time.UTC), wantEnd: time.Date(2026, 12, 16, 5, 0, 0, 0, time.UTC)},
		{name: "a declared zone that does not load falls to UTC", filterString: "deadline:eq:2026-12-15", fields: withDeclaredZone("Nowhere/Here"), wantStart: time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC), wantEnd: time.Date(2026, 12, 16, 0, 0, 0, 0, time.UTC)},
		{name: "a minute in the request's zone", filterString: "deadline:eq:2026-12-15T09:30", fields: defaultTestJSONToSQLNameMap, zone: tokyo, wantStart: time.Date(2026, 12, 15, 0, 30, 0, 0, time.UTC), wantEnd: time.Date(2026, 12, 15, 0, 31, 0, 0, time.UTC)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parser, err := NewFilterParser(NewFilterLexer(tt.filterString), tt.fields)
			if err != nil {
				t.Fatalf("NewFilterParser() error = %v", err)
			}
			if tt.zone != nil {
				parser.WithZone(tt.zone)
			}
			got, err := parser.Parse(SpannerDBType)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.filterString, err)
			}
			bounds := leaves(got)
			if len(bounds) != 2 {
				t.Fatalf("Parse(%q) = %s, want two comparisons", tt.filterString, got)
			}
			start, _ := bounds[0].Condition.Value.(time.Time)
			end, _ := bounds[1].Condition.Value.(time.Time)
			if !start.Equal(tt.wantStart) || !end.Equal(tt.wantEnd) {
				t.Errorf("Parse(%q) spans %v to %v, want %v to %v", tt.filterString, start.UTC(), end.UTC(), tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestParser_WrittenDateRefusals pins the plain refusal of a value a date or timestamp
// column cannot read, naming the field as written and the forms with today's date.
func TestParser_WrittenDateRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		filterString string
		wantErr      string
	}{
		{name: "a word on a timestamp column", filterString: "deadline:lt:yesterday", wantErr: "deadline: 'yesterday' is not a date. Write it as " + todayISO() + ", " + todayUS() + ", or " + todayISO() + "T09:30 with the time."},
		{name: "a day that does not exist", filterString: "deadline:eq:2026-02-29", wantErr: "deadline: '2026-02-29' is not a date. Write it as " + todayISO()},
		{name: "an hour that does not exist", filterString: "deadline:eq:2026-12-15T24:00", wantErr: "deadline: '2026-12-15T24:00' is not a date."},
		{name: "a word inside an in list", filterString: "deadline:in:(2026-12-15,soon)", wantErr: "deadline: 'soon' is not a date."},
		{name: "a time of day on a date column", filterString: "released_on:eq:2026-12-15T09:30", wantErr: "released_on: '2026-12-15T09:30' is not a date. Write it as " + todayISO() + " or " + todayUS() + "."},
		{name: "a day-first date on a date column", filterString: "released_on:eq:15/12/2026", wantErr: "released_on: '15/12/2026' is not a date."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parser, err := NewFilterParser(NewFilterLexer(tt.filterString), defaultTestJSONToSQLNameMap)
			if err != nil {
				t.Fatalf("NewFilterParser() error = %v", err)
			}
			_, err = parser.Parse(SpannerDBType)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse(%q) error = %v, want it to say %q", tt.filterString, err, tt.wantErr)
			}
		})
	}
}

// writtenDateSQLMap names the columns of a timestamp and a date field on both dialects.
var writtenDateSQLMap = map[jsonFieldName]FilterFieldInfo{
	"status":      {dbColumnNames: map[DBType]string{SpannerDBType: "Status", PostgresDBType: "Status"}, Kind: reflect.String, Indexed: true},
	"deadline":    {dbColumnNames: map[DBType]string{SpannerDBType: "Deadline", PostgresDBType: "Deadline"}, Kind: reflect.Struct, FieldType: reflect.TypeFor[time.Time](), Indexed: true},
	"released_on": {dbColumnNames: map[DBType]string{SpannerDBType: "ReleasedOn", PostgresDBType: "ReleasedOn"}, Kind: reflect.Struct, FieldType: reflect.TypeFor[civil.Date](), Indexed: true},
}

// TestSQLGenerator_WrittenDates pins the SQL a written span renders on both dialects:
// plain comparisons on typed parameters, the span's pair in its own parentheses.
func TestSQLGenerator_WrittenDates(t *testing.T) {
	t.Parallel()

	utc := func(year, month, day, hour, minute int) time.Time {
		return time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.UTC)
	}
	tests := []struct {
		name         string
		filterString string
		dialect      SQLDialect
		wantSQL      string
		wantParams   map[string]any
	}{
		{name: "a day on Spanner", filterString: "deadline:eq:2026-12-15", dialect: Spanner, wantSQL: "(`Deadline` >= @_p1 AND `Deadline` < @_p2)", wantParams: map[string]any{"_p1": utc(2026, 12, 15, 0, 0), "_p2": utc(2026, 12, 16, 0, 0)}},
		{name: "a day on PostgreSQL", filterString: "deadline:eq:2026-12-15", dialect: PostgreSQL, wantSQL: `("Deadline" >= @_p1 AND "Deadline" < @_p2)`, wantParams: map[string]any{"_p1": utc(2026, 12, 15, 0, 0), "_p2": utc(2026, 12, 16, 0, 0)}},
		{name: "ne a month on a date column", filterString: "released_on:ne:2026-12", dialect: Spanner, wantSQL: "(`ReleasedOn` < @_p1 OR `ReleasedOn` > @_p2)", wantParams: map[string]any{"_p1": civil.Date{Year: 2026, Month: 12, Day: 1}, "_p2": civil.Date{Year: 2026, Month: 12, Day: 31}}},
		{name: "lte a month is one comparison", filterString: "deadline:lte:2026-12", dialect: PostgreSQL, wantSQL: `"Deadline" < @_p1`, wantParams: map[string]any{"_p1": utc(2027, 1, 1, 0, 0)}},
		{name: "an in list with a span and an instant", filterString: "deadline:in:(2026-12-15,2026-12-16T09:30:00Z)", dialect: Spanner, wantSQL: "((`Deadline` >= @_p1 AND `Deadline` < @_p2) OR `Deadline` = @_p3)", wantParams: map[string]any{"_p1": utc(2026, 12, 15, 0, 0), "_p2": utc(2026, 12, 16, 0, 0), "_p3": utc(2026, 12, 16, 9, 30)}},
		{name: "a span's parentheses hold beside another condition", filterString: "status:eq:open,deadline:ne:2026-12-15", dialect: Spanner, wantSQL: "`Status` = @_p1 AND (`Deadline` < @_p2 OR `Deadline` >= @_p3)", wantParams: map[string]any{"_p1": "open", "_p2": utc(2026, 12, 15, 0, 0), "_p3": utc(2026, 12, 16, 0, 0)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parser, err := NewFilterParser(NewFilterLexer(tt.filterString), writtenDateSQLMap)
			if err != nil {
				t.Fatalf("NewFilterParser() error = %v", err)
			}
			var gotSQL string
			var gotParams any
			switch tt.dialect {
			case PostgreSQL:
				ast, err := parser.Parse(PostgresDBType)
				if err != nil {
					t.Fatalf("Parse() error = %v", err)
				}
				gotSQL, gotParams, err = NewPostgreSQLGenerator().GenerateSQL(ast)
				if err != nil {
					t.Fatalf("GenerateSQL() error = %v", err)
				}
			case Spanner:
				ast, err := parser.Parse(SpannerDBType)
				if err != nil {
					t.Fatalf("Parse() error = %v", err)
				}
				gotSQL, gotParams, err = NewSpannerGenerator().GenerateSQL(ast)
				if err != nil {
					t.Fatalf("GenerateSQL() error = %v", err)
				}
			}
			if gotSQL != tt.wantSQL {
				t.Errorf("GenerateSQL() = %q, want %q", gotSQL, tt.wantSQL)
			}
			params, _ := gotParams.(map[string]any)
			for name, want := range tt.wantParams {
				got := params[name]
				if wantTime, ok := want.(time.Time); ok {
					gotTime, _ := got.(time.Time)
					if !gotTime.Equal(wantTime) {
						t.Errorf("param %s = %v, want %v", name, got, want)
					}

					continue
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("param %s = %#v, want %#v", name, got, want)
				}
			}
			if len(params) != len(tt.wantParams) {
				t.Errorf("GenerateSQL() bound %d parameters, want %d: %#v", len(params), len(tt.wantParams), params)
			}
		})
	}
}

// decodeBoardInZone decodes a computed request over the board fixture as a browser in
// the zone would send it, the person's zone in the request's Time-Zone header.
func decodeBoardInZone(t *testing.T, target, zone string) (*QuerySet[boardRow], error) {
	t.Helper()

	resSet, err := NewSet[boardRow, boardRequest](accesstypes.List)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	decoder, err := NewQueryDecoder[boardRow, boardRequest](resSet)
	if err != nil {
		t.Fatalf("NewQueryDecoder() error = %v", err)
	}
	decoder.WithPaging(Paging{}).WithCursorKey(mustCursorKey(t, testCookieKey))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
	if zone != "" {
		req.Header.Set(TimeZoneHeader, zone)
	}
	qSet, err := decoder.DecodeWithoutPermissions(req)
	if err != nil {
		return nil, err
	}
	qSet.scope = testScope
	qSet.dbType = SpannerDBType

	return qSet, nil
}

// TestFilterShape_Match_WrittenDates pins the in-memory evaluator on written dates over
// a computed resource's timestamp field: the fixture's rows are recorded at 14:00, 13:00
// and 12:00 UTC on 2026-09-04, and the person's zone, sent in the header, moves the day
// they fall on.
func TestFilterShape_Match_WrittenDates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		filter string
		zone   string
		want   []string
	}{
		{name: "the day, in UTC without a zone", filter: "recordedAt:eq:2026-09-04", want: []string{"Kingfisher/hull", "Kingfisher/reactor", "Lantern/hull"}},
		{name: "the day in the US form", filter: "recordedAt:eq:9/4/2026", want: []string{"Kingfisher/hull", "Kingfisher/reactor", "Lantern/hull"}},
		{name: "the next day", filter: "recordedAt:eq:2026-09-05", want: nil},
		{name: "a minute", filter: "recordedAt:eq:2026-09-04T13:00", want: []string{"Kingfisher/reactor"}},
		{name: "gt a minute is from its end", filter: "recordedAt:gt:2026-09-04T13:00", want: []string{"Kingfisher/hull"}},
		{name: "lte a minute is before its end", filter: "recordedAt:lte:2026-09-04T13:00", want: []string{"Kingfisher/reactor", "Lantern/hull"}},
		{name: "ne a minute", filter: "recordedAt:ne:2026-09-04T13:00", want: []string{"Kingfisher/hull", "Lantern/hull"}},
		{name: "in over two minutes", filter: "recordedAt:in:(2026-09-04T12:00,2026-09-04T14:00)", want: []string{"Kingfisher/hull", "Lantern/hull"}},
		{name: "notin over two minutes", filter: "recordedAt:notin:(2026-09-04T12:00,2026-09-04T14:00)", want: []string{"Kingfisher/reactor"}},
		{name: "lt the month", filter: "recordedAt:lt:2026-09", want: nil},
		{name: "gte the year", filter: "recordedAt:gte:2026", want: []string{"Kingfisher/hull", "Kingfisher/reactor", "Lantern/hull"}},
		{name: "the day in a zone fourteen hours ahead, where the rows fall on the 5th", filter: "recordedAt:eq:2026-09-04", zone: "Pacific/Kiritimati", want: nil},
		{name: "the 5th in that zone", filter: "recordedAt:eq:2026-09-05", zone: "Pacific/Kiritimati", want: []string{"Kingfisher/hull", "Kingfisher/reactor", "Lantern/hull"}},
		{name: "the day on the US west coast", filter: "recordedAt:eq:2026-09-04", zone: "America/Los_Angeles", want: []string{"Kingfisher/hull", "Kingfisher/reactor", "Lantern/hull"}},
		{name: "a minute in Tokyo", filter: "recordedAt:eq:2026-09-04T22:00", zone: "Asia/Tokyo", want: []string{"Kingfisher/reactor"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			qSet, err := decodeBoardInZone(t, "/?filter="+tt.filter+"&limit=all", tt.zone)
			if err != nil {
				t.Fatalf("decode error = %v", err)
			}
			var got []string
			for _, row := range boardRows() {
				ok, err := qSet.Filter().Match(row)
				if err != nil {
					t.Fatalf("Match() error = %v", err)
				}
				if ok {
					got = append(got, row.ShipName+"/"+row.Subsystem)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Match() kept %v, want %v", got, tt.want)
			}
		})
	}
}

// TestQueryDecoder_TimeZoneHeader pins the header's refusal: a value that is no zone
// name is a 400 in the header's name, and a header on a request without a filter is
// nothing to read.
func TestQueryDecoder_TimeZoneHeader(t *testing.T) {
	t.Parallel()

	if _, err := decodeBoardInZone(t, "/?filter=recordedAt:eq:2026-09-04&limit=all", "Mars/Olympus"); err == nil || !strings.Contains(err.Error(), "Time-Zone: 'Mars/Olympus' is not a time zone name. Write it as America/New_York.") {
		t.Errorf("an unknown zone error = %v, want the header's refusal", err)
	}
	if _, err := decodeBoardInZone(t, "/?filter=recordedAt:eq:2026-09-04&limit=all", "Local"); err == nil || !strings.Contains(err.Error(), "Time-Zone: 'Local' is not a time zone name.") {
		t.Errorf("Local error = %v, want the header's refusal", err)
	}
	if _, err := decodeBoardInZone(t, "/?limit=all", "Mars/Olympus"); err != nil {
		t.Errorf("a request without a filter error = %v, want none", err)
	}
}
