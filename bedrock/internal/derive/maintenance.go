// maintenance.go holds the placement's maintenance windows: the time each environment
// may take a release that interrupts service, and the arithmetic that says when the
// next one opens.

package derive

import (
	"encoding/json"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

// The words a maintenance setting is written with: the one-word setting of an
// environment that takes a window release at any time, and the two values of releases
// (only a breaking release waits for the window, the default; every release does).
const (
	MaintenanceAnytime = "anytime"
	ReleasesBreaking   = "breaking"
	ReleasesAll        = "all"
)

// The layouts the slots' values are read with: a time of day and a date.
const (
	clockLayout = "15:04"
	dateLayout  = "2006-01-02"
)

// clockRE reads a time of day, HH:MM on a 24-hour clock.
var clockRE = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

// weekdays are the days a weekly slot names, Monday to Sunday as the time package
// spells them.
var weekdays = weekdayNames()

// weekdayNames maps each day's name to the day.
func weekdayNames() map[string]time.Weekday {
	names := make(map[string]time.Weekday, 7)
	for day := time.Sunday; day <= time.Saturday; day++ {
		names[day.String()] = day
	}

	return names
}

// MaintenanceWindow is one environment's maintenance setting: the window in which the
// environment takes a release that interrupts service. Written as the word "anytime",
// or as an object naming the client's time zone, its weekly slots, its dated slots (a
// one-off the client agreed to) and which releases wait for it.
type MaintenanceWindow struct {
	// Anytime says the environment takes a window release at any time: the one-word
	// setting, and every environment's default but production's.
	Anytime bool
	// TimeZone is the IANA zone the slots' times are read in (America/Chicago): the
	// client's wall clock, through its daylight-saving changes.
	TimeZone string
	// Weekly are the slots that open every week; Dates the slots that open once.
	Weekly []WeeklySlot
	Dates  []DatedSlot
	// Releases says which releases wait for the window: breaking (the default when
	// absent) or all. Maintenance mode is a breaking release's whichever it says.
	Releases string
}

// WeeklySlot is a slot that opens every week: on a day, from a time of day to another.
// A slot whose to is not after its from crosses midnight and ends the next day.
type WeeklySlot struct {
	Day  string `json:"day"`
	From string `json:"from"`
	To   string `json:"to"`
}

// DatedSlot is a slot that opens once, on a date, from a time of day to a later one.
type DatedSlot struct {
	On   string `json:"on"`
	From string `json:"from"`
	To   string `json:"to"`
}

// maintenanceObject is the object form of a setting as the placement spells it.
type maintenanceObject struct {
	TimeZone string       `json:"timeZone,omitempty"`
	Weekly   []WeeklySlot `json:"weekly,omitempty"`
	Dates    []DatedSlot  `json:"dates,omitempty"`
	Releases string       `json:"releases,omitempty"`
}

// MarshalJSON writes the setting as the placement spells it: the word for anytime, the
// object otherwise.
func (w MaintenanceWindow) MarshalJSON() ([]byte, error) {
	var data []byte
	var err error
	if w.Anytime {
		data, err = json.Marshal(MaintenanceAnytime)
	} else {
		data, err = json.Marshal(maintenanceObject{TimeZone: w.TimeZone, Weekly: w.Weekly, Dates: w.Dates, Releases: w.Releases})
	}
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return data, nil
}

// UnmarshalJSON reads the word or the object; any other word, and any other field, is
// refused.
func (w *MaintenanceWindow) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, `"`) {
		var word string
		if err := json.Unmarshal(data, &word); err != nil {
			return errors.Wrap(err, "json.Unmarshal()")
		}
		if word != MaintenanceAnytime {
			return errors.Newf("%q is not a maintenance setting: the word %q, or an object with timeZone, weekly, dates and releases", word, MaintenanceAnytime)
		}
		*w = MaintenanceWindow{Anytime: true}

		return nil
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.DisallowUnknownFields()
	var o maintenanceObject
	if err := dec.Decode(&o); err != nil {
		return errors.Wrap(err, "a maintenance setting is the word \"anytime\" or an object with timeZone, weekly, dates and releases")
	}
	*w = MaintenanceWindow{TimeZone: o.TimeZone, Weekly: o.Weekly, Dates: o.Dates, Releases: o.Releases}

	return nil
}

// validate checks the setting's shape, naming it as the placement does (maintenance.prd).
func (w *MaintenanceWindow) validate(name string) error {
	switch w.Releases {
	case "", ReleasesBreaking, ReleasesAll:
	default:
		return errors.Newf("%s: releases %q is not %s or %s", name, w.Releases, ReleasesBreaking, ReleasesAll)
	}
	if w.Anytime {
		return nil
	}
	if w.TimeZone == "" {
		return errors.Newf("%s names no timeZone: the IANA zone the slots' times are read in (America/Chicago)", name)
	}
	if _, err := time.LoadLocation(w.TimeZone); err != nil {
		return errors.Newf("%s: time zone %q does not load: an IANA zone name (America/Chicago)", name, w.TimeZone)
	}
	if len(w.Weekly) == 0 && len(w.Dates) == 0 {
		return errors.Newf("%s names no weekly or dated slot and never opens: write %q for no window", name, MaintenanceAnytime)
	}
	for i, s := range w.Weekly {
		slot := name + ".weekly[" + strconv.Itoa(i) + "]"
		if _, ok := weekdays[s.Day]; !ok {
			return errors.Newf("%s: day %q is not a day of the week (Monday to Sunday)", slot, s.Day)
		}
		if err := validClock(slot, s.From, s.To); err != nil {
			return err
		}
		if s.From == s.To {
			return errors.Newf("%s: from %s to %s never opens", slot, s.From, s.To)
		}
	}
	for i, s := range w.Dates {
		slot := name + ".dates[" + strconv.Itoa(i) + "]"
		if _, err := time.Parse(dateLayout, s.On); err != nil {
			return errors.Newf("%s: on %q is not a date (YYYY-MM-DD)", slot, s.On)
		}
		if err := validClock(slot, s.From, s.To); err != nil {
			return err
		}
		if s.From >= s.To {
			return errors.Newf("%s: from %s is not before to %s; a dated slot is on one date, so a slot past midnight is the next date's own", slot, s.From, s.To)
		}
	}

	return nil
}

// validClock refuses a from or to that is not a time of day.
func validClock(slot, from, to string) error {
	for _, v := range []struct{ field, value string }{{"from", from}, {"to", to}} {
		if !clockRE.MatchString(v.value) {
			return errors.Newf("%s: %s %q is not a time of day (HH:MM, 24-hour)", slot, v.field, v.value)
		}
	}

	return nil
}

// AllReleases reports whether every release waits for the window, not only a breaking
// one.
func (w *MaintenanceWindow) AllReleases() bool {
	return w.Releases == ReleasesAll
}

// String spells the setting for a log line: anytime, or the slots in their zone.
func (w *MaintenanceWindow) String() string {
	if w.Anytime {
		return MaintenanceAnytime
	}
	slots := make([]string, 0, len(w.Weekly)+len(w.Dates))
	for _, s := range w.Weekly {
		slots = append(slots, s.Day+" "+s.From+" to "+s.To)
	}
	for _, s := range w.Dates {
		slots = append(slots, s.On+" "+s.From+" to "+s.To)
	}
	text := strings.Join(slots, ", ") + " " + w.TimeZone
	if w.AllReleases() {
		text += ", every release"
	}

	return text
}

// PassedDates lists the dated slots that have passed by now: a one-off that is over
// and may be removed.
func (w *MaintenanceWindow) PassedDates(now time.Time) []DatedSlot {
	loc, err := time.LoadLocation(w.TimeZone)
	if err != nil {
		return nil
	}
	var passed []DatedSlot
	for _, s := range w.Dates {
		if _, to, ok := datedBounds(s, loc); ok && !to.After(now) {
			passed = append(passed, s)
		}
	}

	return passed
}

// Opening is what a window says at one moment: open now, in a slot that runs from From
// to To, or closed, with the next slot's bounds (zero when no slot lies ahead).
type Opening struct {
	Open bool
	From time.Time
	To   time.Time
	// Slot names the slot: the weekly one with its zone, the date of a dated one, or
	// anytime.
	Slot string
}

// slot is one occurrence of a slot in time.
type slot struct {
	from, to time.Time
	name     string
}

// Opening says whether the window is open at now and, when it is not, when it next
// opens. The slots are read on the client's wall clock in the setting's zone, so a
// weekly slot keeps its hour across a daylight-saving change; a weekly slot whose to is
// not after its from ends the next day. Among slots open at once, the one that closes
// last is answered; among slots ahead, the first to open.
func (w *MaintenanceWindow) Opening(now time.Time) (Opening, error) {
	if w.Anytime {
		return Opening{Open: true, Slot: MaintenanceAnytime}, nil
	}
	loc, err := time.LoadLocation(w.TimeZone)
	if err != nil {
		return Opening{}, errors.Wrapf(err, "time.LoadLocation(): %s", w.TimeZone)
	}
	var open, next *slot
	for _, s := range w.occurrences(now, loc) {
		switch {
		case !s.from.After(now) && s.to.After(now):
			if open == nil || s.to.After(open.to) {
				open = &s
			}
		case s.from.After(now):
			if next == nil || s.from.Before(next.from) {
				next = &s
			}
		}
	}
	switch {
	case open != nil:
		return Opening{Open: true, From: open.from, To: open.to, Slot: open.name}, nil
	case next != nil:
		return Opening{From: next.from, To: next.to, Slot: next.name}, nil
	default:
		return Opening{}, nil
	}
}

// occurrences lists each slot's occurrences around now: a weekly slot's from the day
// before now to a week ahead, so that one crossing midnight into today and the next
// week's are both seen, and every dated slot.
func (w *MaintenanceWindow) occurrences(now time.Time, loc *time.Location) []slot {
	const daysAhead = 8
	var occurrences []slot
	today := now.In(loc)
	for _, s := range w.Weekly {
		day := weekdays[s.Day]
		for offset := -1; offset <= daysAhead; offset++ {
			date := today.AddDate(0, 0, offset)
			if date.Weekday() != day {
				continue
			}
			from := at(date, s.From, loc)
			to := at(date, s.To, loc)
			if !to.After(from) {
				to = at(date.AddDate(0, 0, 1), s.To, loc)
			}
			occurrences = append(occurrences, slot{from: from, to: to, name: s.Day + " " + s.From + " to " + s.To + " " + w.TimeZone})
		}
	}
	for _, s := range w.Dates {
		if from, to, ok := datedBounds(s, loc); ok {
			occurrences = append(occurrences, slot{from: from, to: to, name: "the dated slot " + s.On + " " + s.From + " to " + s.To + " " + w.TimeZone})
		}
	}

	return occurrences
}

// datedBounds are a dated slot's bounds in the zone; ok is false for a date or a time
// that does not read, which validation refused already.
func datedBounds(s DatedSlot, loc *time.Location) (from, to time.Time, ok bool) {
	date, err := time.ParseInLocation(dateLayout, s.On, loc)
	if err != nil || !clockRE.MatchString(s.From) || !clockRE.MatchString(s.To) {
		return time.Time{}, time.Time{}, false
	}

	return at(date, s.From, loc), at(date, s.To, loc), true
}

// at is the time of day (HH:MM) on the date, in the zone.
func at(date time.Time, clock string, loc *time.Location) time.Time {
	m := clockRE.FindStringSubmatch(clock)
	if m == nil {
		return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	}
	hour, _ := strconv.Atoi(m[1])
	minute, _ := strconv.Atoi(m[2])

	return time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, loc)
}

// MaintenanceSetting is env's maintenance setting and whether the placement writes
// one. Every environment but production is anytime unless its setting is written;
// production has no default, so a breaking release to production is refused until its
// setting is written, and "anytime" is a setting to write.
func (p *Placement) MaintenanceSetting(env string) (MaintenanceWindow, bool) {
	if w, ok := p.Maintenance[env]; ok {
		return w, true
	}
	if env != p.Production() {
		return MaintenanceWindow{Anytime: true}, true
	}

	return MaintenanceWindow{}, false
}

// validateMaintenance checks every maintenance setting: for an environment the
// placement lists, and of a shape that opens.
func (p *Placement) validateMaintenance() error {
	envs := make([]string, 0, len(p.Maintenance))
	for env := range p.Maintenance {
		envs = append(envs, env)
	}
	sort.Strings(envs)
	for _, env := range envs {
		if !slices.Contains(p.Environments, env) {
			return errors.Newf("maintenance names %q, which is not one of the environments (%s)", env, strings.Join(p.Environments, ", "))
		}
		w := p.Maintenance[env]
		if err := w.validate("maintenance." + env); err != nil {
			return err
		}
	}

	return nil
}
