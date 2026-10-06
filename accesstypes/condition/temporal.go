package condition

import (
	"fmt"
	"strings"
	"sync"
	"time"

	// The temporal functions convert the decision instant to a zone's wall
	// clock at fold time. Embedding the timezone database makes that answer a
	// property of the build, never of which zoneinfo files a host has
	// installed — a check must not fail closed because a container image is
	// minimal.
	_ "time/tzdata"
)

// The temporal functions (design plan §05, decided 2026-09-03) are
// fact-anchored: timeOfDay(now, zone) and dayOfWeek(now, zone) read the
// environment's instant through a zone's wall clock, so they always fold in
// the engine and never lower to SQL — no database dialect renders timezone
// arithmetic, and the one-face invariant holds because the fold is the only
// face. The conversion only ever goes instant → wall clock, so DST has no
// ambiguous case: every instant has exactly one reading in a zone.

// dayNames are dayOfWeek's closed value vocabulary, indexed by time.Weekday
// (Sunday = 0).
var dayNames = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// ValidDayName reports whether s is one of dayOfWeek's seven values —
// deploy-time validation (MigrateRoles) shares the vocabulary through this.
func ValidDayName(s string) bool {
	for _, name := range dayNames {
		if s == name {
			return true
		}
	}

	return false
}

// ParseTimeOfDay parses timeOfDay's literal form — 24-hour 'HH:MM' — to
// minutes since midnight. Deploy-time validation (MigrateRoles) shares the
// format through this.
func ParseTimeOfDay(s string) (int, error) {
	parsed, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("condition: %q is not a 24-hour HH:MM time of day", s)
	}

	return parsed.Hour()*60 + parsed.Minute(), nil
}

// LoadZone resolves a temporal function's zone name against the embedded
// timezone database — deploy-time validation (MigrateRoles) shares the
// resolution through this. Results are cached: the standard library reloads
// zone data on every lookup, and folding runs per check.
func LoadZone(name string) (*time.Location, error) {
	if cached, ok := zoneCache.Load(name); ok {
		if loc, isLocation := cached.(*time.Location); isLocation {
			return loc, nil
		}
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("condition: %q is not a timezone name the timezone database knows", name)
	}
	zoneCache.Store(name, loc)

	return loc, nil
}

var zoneCache sync.Map

// location resolves the Ref's zone argument: the Environment's zone fact for
// the bare word local, the named zone otherwise.
func (r Ref) location(f Facts) (*time.Location, error) {
	if r.ZoneLocal {
		if f.zone == nil {
			return nil, fmt.Errorf("condition: %q references local, which the environment does not carry", r.String())
		}

		return f.zone, nil
	}

	return LoadZone(r.Zone)
}

// foldTemporalComparison evaluates a temporal function's relational
// comparison against the decision instant's wall-clock reading.
func foldTemporalComparison(c *Comparison, f Facts) (Expr, error) {
	if !f.hasNow {
		return nil, fmt.Errorf("condition: %q references now, which the environment does not carry", c.String())
	}
	loc, err := c.Left.location(f)
	if err != nil {
		return nil, err
	}
	literal, ok := c.Right.(StringLiteral)
	if !ok {
		return nil, fmt.Errorf("condition: %s compares against a quoted literal, not %q, in %q", c.Left.Func, c.Right.String(), c.String())
	}
	local := f.now.In(loc)

	switch c.Left.Func {
	case FuncTimeOfDay:
		right, err := ParseTimeOfDay(literal.Value)
		if err != nil {
			return nil, fmt.Errorf("%w in %q", err, c.String())
		}
		left := local.Hour()*60 + local.Minute()
		result, err := compareOrdered(left, c.Op, right)
		if err != nil {
			return nil, fmt.Errorf("%w in %q", err, c.String())
		}

		return Truth{Value: result}, nil

	case FuncDayOfWeek:
		if !ValidDayName(literal.Value) {
			return nil, fmt.Errorf("condition: %q is not a day name (%s) in %q", literal.Value, strings.Join(dayNames[:], ", "), c.String())
		}
		day := dayNames[local.Weekday()]
		switch c.Op {
		case Eq:
			return Truth{Value: day == literal.Value}, nil
		case NotEq:
			return Truth{Value: day != literal.Value}, nil
		default:
			return nil, fmt.Errorf("condition: %s supports =, != and [NOT] IN, not %q, in %q", FuncDayOfWeek, c.Op, c.String())
		}

	default:
		return nil, fmt.Errorf("condition: unknown temporal function %q in %q", c.Left.Func, c.String())
	}
}

// foldTemporalIn evaluates dayOfWeek's membership in its literal day list —
// the only temporal IN the parser admits.
func foldTemporalIn(in *In, f Facts) (Expr, error) {
	if !f.hasNow {
		return nil, fmt.Errorf("condition: %q references now, which the environment does not carry", in.String())
	}
	loc, err := in.Left.location(f)
	if err != nil {
		return nil, err
	}
	day := dayNames[f.now.In(loc).Weekday()]

	member := false
	for _, lit := range in.Literals {
		literal, ok := lit.(StringLiteral)
		if !ok {
			return nil, fmt.Errorf("condition: %s tests membership in quoted day names, not %q, in %q", FuncDayOfWeek, lit.String(), in.String())
		}
		if !ValidDayName(literal.Value) {
			return nil, fmt.Errorf("condition: %q is not a day name (%s) in %q", literal.Value, strings.Join(dayNames[:], ", "), in.String())
		}
		if literal.Value == day {
			member = true
		}
	}
	if in.Negated {
		member = !member
	}

	return Truth{Value: member}, nil
}

// compareOrdered applies a relational operator to two ordered integers.
func compareOrdered(left int, op CompareOp, right int) (bool, error) {
	switch op {
	case Eq:
		return left == right, nil
	case NotEq:
		return left != right, nil
	case Less:
		return left < right, nil
	case LessEq:
		return left <= right, nil
	case Greater:
		return left > right, nil
	case GreaterEq:
		return left >= right, nil
	default:
		return false, fmt.Errorf("condition: unsupported operator %q", op)
	}
}
