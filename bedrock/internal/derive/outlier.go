// outlier.go holds the outlier detection thresholds of the load balancer's backend
// services. A serverless network endpoint group has no health check, so a backend service
// over both regions' groups keeps sending a failing region its share of the requests
// until outlier detection takes the region out: the load balancer counts each group's
// errors in a row and ejects a group that reaches the threshold for a while. bedrock
// turns it on in every backend it renders, with the thresholds below unless a placement
// sets its own.

package derive

import (
	"github.com/go-playground/errors/v5"
)

// The thresholds' defaults: five errors in a row eject a region's group, counted every
// second; every detection ejects (100 percent enforcing); at most half the groups are out
// at once, so one region of two; and a group is out for thirty seconds the first time.
const (
	DefaultConsecutiveErrors          = 5
	DefaultEnforcingConsecutiveErrors = 100
	DefaultMaxEjectionPercent         = 50
	DefaultIntervalSeconds            = 1
	DefaultBaseEjectionSeconds        = 30

	// fullPercent is the top of a percentage.
	fullPercent = 100
)

// OutlierDetection is a placement's outlier detection thresholds (outlierDetection): each
// field the placement leaves out takes its default, and a field written is a whole number
// that keeps detection on (none may be zero, which would turn the ejection off).
type OutlierDetection struct {
	// ConsecutiveErrors is how many errors in a row (a 5xx answer, or a request that
	// gets none) eject a region's endpoint group: at least 1, default 5.
	ConsecutiveErrors *int `json:"consecutiveErrors,omitempty"`
	// EnforcingConsecutiveErrors is the chance, in percent, that a group that reaches
	// ConsecutiveErrors is ejected: 1 to 100, default 100, every detection.
	EnforcingConsecutiveErrors *int `json:"enforcingConsecutiveErrors,omitempty"`
	// MaxEjectionPercent is the most of the backend's groups out at once, in percent: 1
	// to 100, default 50, one region of two.
	MaxEjectionPercent *int `json:"maxEjectionPercent,omitempty"`
	// IntervalSeconds is how often, in seconds, the load balancer looks at the counts,
	// ejecting groups and returning those whose time is up: at least 1, default 1.
	IntervalSeconds *int `json:"intervalSeconds,omitempty"`
	// BaseEjectionSeconds is how long, in seconds, a group stays out the first time;
	// each further ejection multiplies it by the number of times the group was ejected:
	// at least 1, default 30.
	BaseEjectionSeconds *int `json:"baseEjectionSeconds,omitempty"`
}

// Outlier is the thresholds a backend service is rendered with: the placement's where it
// sets them, the defaults elsewhere.
type Outlier struct {
	ConsecutiveErrors          int
	EnforcingConsecutiveErrors int
	MaxEjectionPercent         int
	IntervalSeconds            int
	BaseEjectionSeconds        int
}

// DefaultOutlier is the thresholds of a placement that sets none.
func DefaultOutlier() Outlier {
	return Outlier{
		ConsecutiveErrors:          DefaultConsecutiveErrors,
		EnforcingConsecutiveErrors: DefaultEnforcingConsecutiveErrors,
		MaxEjectionPercent:         DefaultMaxEjectionPercent,
		IntervalSeconds:            DefaultIntervalSeconds,
		BaseEjectionSeconds:        DefaultBaseEjectionSeconds,
	}
}

// IsDefault reports thresholds that are the defaults, whether a placement left them out
// or wrote them.
func (o Outlier) IsDefault() bool {
	return o == DefaultOutlier()
}

// Resolved is the thresholds with the defaults in place of every field left out; a nil
// OutlierDetection is the defaults.
func (o *OutlierDetection) Resolved() Outlier {
	out := DefaultOutlier()
	if o == nil {
		return out
	}
	for _, f := range []struct {
		set *int
		to  *int
	}{
		{set: o.ConsecutiveErrors, to: &out.ConsecutiveErrors},
		{set: o.EnforcingConsecutiveErrors, to: &out.EnforcingConsecutiveErrors},
		{set: o.MaxEjectionPercent, to: &out.MaxEjectionPercent},
		{set: o.IntervalSeconds, to: &out.IntervalSeconds},
		{set: o.BaseEjectionSeconds, to: &out.BaseEjectionSeconds},
	} {
		if f.set != nil {
			*f.to = *f.set
		}
	}

	return out
}

// Clone is a copy that shares nothing with o; nil stays nil.
func (o *OutlierDetection) Clone() *OutlierDetection {
	if o == nil {
		return nil
	}

	return &OutlierDetection{
		ConsecutiveErrors:          cloneInt(o.ConsecutiveErrors),
		EnforcingConsecutiveErrors: cloneInt(o.EnforcingConsecutiveErrors),
		MaxEjectionPercent:         cloneInt(o.MaxEjectionPercent),
		IntervalSeconds:            cloneInt(o.IntervalSeconds),
		BaseEjectionSeconds:        cloneInt(o.BaseEjectionSeconds),
	}
}

// cloneInt is a copy of the pointed-to number; nil stays nil.
func cloneInt(n *int) *int {
	if n == nil {
		return nil
	}
	v := *n

	return &v
}

// Validate refuses a threshold written outside what keeps detection on: a count or a
// time below 1, a percentage outside 1 to 100. Nothing written is the defaults.
func (o *OutlierDetection) Validate() error {
	if o == nil {
		return nil
	}
	for _, f := range []struct {
		name string
		v    *int
		max  int
		what string
	}{
		{name: "consecutiveErrors", v: o.ConsecutiveErrors, what: "errors in a row, at least 1"},
		{name: "enforcingConsecutiveErrors", v: o.EnforcingConsecutiveErrors, max: fullPercent, what: "a percentage from 1 to 100; 0 would eject no region"},
		{name: "maxEjectionPercent", v: o.MaxEjectionPercent, max: fullPercent, what: "a percentage from 1 to 100; 0 would eject no region"},
		{name: "intervalSeconds", v: o.IntervalSeconds, what: "seconds, at least 1"},
		{name: "baseEjectionSeconds", v: o.BaseEjectionSeconds, what: "seconds, at least 1"},
	} {
		if f.v == nil {
			continue
		}
		if *f.v < 1 || (f.max > 0 && *f.v > f.max) {
			return errors.Newf("outlierDetection.%s %d is outside what keeps outlier detection on (%s); leave it out for its default", f.name, *f.v, f.what)
		}
	}

	return nil
}
