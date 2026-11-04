package resource

import (
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/civil"
	"github.com/go-playground/errors/v5"
)

// TimeZoneHeader names the request header that carries the person's time zone, an IANA
// name such as America/New_York, which the browser knows and the resource client sends
// on every request. A filter value written with no zone of its own, 2026-12-15 or
// 2026-12-15T09:30, is read in it; a request without the header reads such a value in
// the zone the column's date search fact declares, and in UTC where none does.
const TimeZoneHeader = "Time-Zone"

// dateUnit is the precision a date or time was written at, and so the span it names.
type dateUnit int

const (
	dateYear dateUnit = iota
	dateMonth
	dateDay
	dateMinute
	dateSecond
)

// writtenDate is a date, or a date and time, as a person writes it in a filter or the
// search box, read at the precision it was written: 2026 is the year, 2026-12 or 12/2026
// the month, 2026-12-15, 12/15/2026 or 20261215 the day, 2026-12-15T09:30 the minute
// and 2026-12-15T09:30:15 the second, a space serving for the T. A year or a month holds
// its first day, so date() is always a day.
type writtenDate struct {
	year, month, day     int
	hour, minute, second int
	unit                 dateUnit
}

// The written forms: ISO (a year, a year and month, a full date), US month/day/year and
// month/year, eight digits as YYYYMMDD, and an ISO date followed by a time of day. The
// search box's date role reads the day forms; a filter reads them all.
var (
	isoYearPattern         = regexp.MustCompile(`^(\d{4})$`)
	isoMonthPattern        = regexp.MustCompile(`^(\d{4})-(\d{1,2})$`)
	isoDatePattern         = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})$`)
	usDatePattern          = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{4})$`)
	usMonthPattern         = regexp.MustCompile(`^(\d{1,2})/(\d{4})$`)
	digitDatePattern       = regexp.MustCompile(`^(\d{4})(\d{2})(\d{2})$`)
	writtenDateTimePattern = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})[T ](\d{1,2}):(\d{2})(?::(\d{2}))?$`)
)

// parseWrittenDate reads text in the written forms; false where the text is none of
// them, or names a day or a time that does not exist.
func parseWrittenDate(text string) (writtenDate, bool) {
	text = strings.TrimSpace(text)
	if m := isoYearPattern.FindStringSubmatch(text); m != nil {
		return writtenDate{year: digits(m[1]), month: 1, day: 1, unit: dateYear}, true
	}
	if m := isoMonthPattern.FindStringSubmatch(text); m != nil {
		return writtenMonth(m[1], m[2])
	}
	if m := usMonthPattern.FindStringSubmatch(text); m != nil {
		return writtenMonth(m[2], m[1])
	}
	if m := isoDatePattern.FindStringSubmatch(text); m != nil {
		return writtenDay(m[1], m[2], m[3])
	}
	if m := usDatePattern.FindStringSubmatch(text); m != nil {
		return writtenDay(m[3], m[1], m[2])
	}
	if m := digitDatePattern.FindStringSubmatch(text); m != nil {
		return writtenDay(m[1], m[2], m[3])
	}
	if m := writtenDateTimePattern.FindStringSubmatch(text); m != nil {
		return writtenTime(m)
	}

	return writtenDate{}, false
}

// digits reads a run of digits a pattern matched; the patterns admit nothing else.
func digits(s string) int {
	n, _ := strconv.Atoi(s)

	return n
}

func writtenMonth(year, month string) (writtenDate, bool) {
	m := digits(month)
	if m < 1 || m > 12 {
		return writtenDate{}, false
	}

	return writtenDate{year: digits(year), month: m, day: 1, unit: dateMonth}, true
}

func writtenDay(year, month, day string) (writtenDate, bool) {
	d := writtenDate{year: digits(year), month: digits(month), day: digits(day), unit: dateDay}
	if !d.date().IsValid() {
		return writtenDate{}, false
	}

	return d, true
}

func writtenTime(m []string) (writtenDate, bool) {
	d, ok := writtenDay(m[1], m[2], m[3])
	if !ok {
		return writtenDate{}, false
	}
	d.hour, d.minute, d.unit = digits(m[4]), digits(m[5]), dateMinute
	if d.hour > 23 || d.minute > 59 {
		return writtenDate{}, false
	}
	if m[6] != "" {
		d.second, d.unit = digits(m[6]), dateSecond
		if d.second > 59 {
			return writtenDate{}, false
		}
	}

	return d, true
}

// date is the day written, or the first day of the month or year.
func (d writtenDate) date() civil.Date {
	return civil.Date{Year: d.year, Month: time.Month(d.month), Day: d.day}
}

// span answers the first instant of the written span in the zone and the first instant
// after it: a day runs from its midnight to the next, time.Date carrying the arithmetic
// across a daylight-saving change, and a minute or a second is itself.
func (d writtenDate) span(zone *time.Location) (start, end time.Time) {
	start = time.Date(d.year, time.Month(d.month), d.day, d.hour, d.minute, d.second, 0, zone)
	switch d.unit {
	case dateYear:
		return start, start.AddDate(1, 0, 0)
	case dateMonth:
		return start, start.AddDate(0, 1, 0)
	case dateDay:
		return start, start.AddDate(0, 0, 1)
	case dateMinute:
		return start, start.Add(time.Minute)
	default:
		return start, start.Add(time.Second)
	}
}

// days answers the first and last day of a written year or month, or the day itself;
// day zero of the following month is the month's last.
func (d writtenDate) days() (first, last civil.Date) {
	first = d.date()
	switch d.unit {
	case dateYear:
		return first, civil.Date{Year: d.year, Month: time.December, Day: 31}
	case dateMonth:
		return first, civil.DateOf(time.Date(d.year, time.Month(d.month)+1, 0, 0, 0, 0, 0, time.UTC))
	default:
		return first, first
	}
}

// dateExamples writes today's date in the zone in the forms a filter reads, for a
// refusal to show.
func dateExamples(zone *time.Location) (iso, us string) {
	today := time.Now().In(zone)

	return today.Format("2006-01-02"), today.Format("1/2/2006")
}

// zones holds the IANA zones loaded so far, by name.
var zones sync.Map

// loadZone answers the named IANA zone, loaded once. The empty name and Local, which
// time.LoadLocation reads as UTC and the server's own zone, name no zone of a person's.
func loadZone(name string) (*time.Location, error) {
	if zone, ok := zones.Load(name); ok {
		if loaded, ok := zone.(*time.Location); ok {
			return loaded, nil
		}
	}
	if name == "" || name == "Local" {
		return nil, errors.Newf("%q is not a time zone name", name)
	}
	zone, err := time.LoadLocation(name)
	if err != nil {
		return nil, errors.Wrapf(err, "time.LoadLocation(%q)", name)
	}
	zones.Store(name, zone)

	return zone, nil
}

// conditionField is the field a condition names, as its value readers see it: the name
// the person wrote, the Go type a value takes (a list's element type on a slice field),
// and the zone a written date on it is read in.
type conditionField struct {
	jsonName  string
	fieldType reflect.Type
	kind      reflect.Kind
	zone      *time.Location
}

// isDateColumn reports whether the field's values are days: a civil.Date or a nullable
// wrapper of one.
func (f conditionField) isDateColumn() bool {
	return f.fieldType != nil && nullableBaseType(f.fieldType) == reflect.TypeFor[civil.Date]()
}

// isWrittenDate reports whether a parsed value is a written span still to be expanded.
func isWrittenDate(value any) bool {
	_, ok := value.(writtenDate)

	return ok
}

// expandWrittenDates turns a condition whose value is a written span, a date, month or
// year on a timestamp column or a month or year on a date column, into the comparisons
// that mean it: eq inside the span, ne outside, lt before its start, lte before its end,
// gt from its end, gte from its start; the spans of an in list are ORed and those of a
// notin list ANDed, each read as eq or ne. A condition with no span, an instant on a
// timestamp column or a day on a date column, stays the one condition it was.
func expandWrittenDates(condition *Condition, field conditionField) ExpressionNode {
	switch condition.Operator {
	case inStr, notinStr:
		if !slices.ContainsFunc(condition.Values, isWrittenDate) {
			return &ConditionNode{Condition: *condition}
		}
		each, join := eqStr, OperatorOr
		if condition.Operator == notinStr {
			each, join = neStr, OperatorAnd
		}
		var tree ExpressionNode
		for _, value := range condition.Values {
			one := Condition{Field: condition.Field, Operator: each, Value: value}
			node := writtenDateComparison(&one, field)
			if tree == nil {
				tree = node

				continue
			}
			tree = &LogicalOpNode{Left: tree, Operator: join, Right: node}
		}

		return &GroupNode{Expression: tree}
	default:
		if !isWrittenDate(condition.Value) {
			return &ConditionNode{Condition: *condition}
		}

		return writtenDateComparison(condition, field)
	}
}

// writtenDateComparison renders one comparison against a written span. On a timestamp
// column the span is the half-open [start, end) in the field's zone, so lte keeps what
// is before the end and gt what is from it; on a date column it is the closed [first,
// last] of whole days. A value that is no span compares as itself.
func writtenDateComparison(condition *Condition, field conditionField) ExpressionNode {
	written, ok := condition.Value.(writtenDate)
	if !ok {
		return &ConditionNode{Condition: *condition}
	}
	leaf := func(operator string, value any) ExpressionNode {
		return &ConditionNode{Condition: Condition{Field: condition.Field, Operator: operator, Value: value}}
	}
	var low, high any
	within, beyond := ltStr, gteStr
	if field.isDateColumn() {
		low, high = written.days()
		within, beyond = lteStr, gtStr
	} else {
		low, high = written.span(field.zone)
	}

	switch condition.Operator {
	case eqStr:
		return &GroupNode{Expression: &LogicalOpNode{Left: leaf(gteStr, low), Operator: OperatorAnd, Right: leaf(within, high)}}
	case neStr:
		return &GroupNode{Expression: &LogicalOpNode{Left: leaf(ltStr, low), Operator: OperatorOr, Right: leaf(beyond, high)}}
	case ltStr:
		return leaf(ltStr, low)
	case lteStr:
		return leaf(within, high)
	case gtStr:
		return leaf(beyond, high)
	default:
		return leaf(gteStr, low)
	}
}
