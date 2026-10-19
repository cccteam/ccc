package conditiontest

import (
	"github.com/cccteam/ccc/accesstypes/condition"
)

// The reference evaluator lives in package condition (condition.Evaluate over a
// condition.Image), where the library-composed rows of the user-management surface
// are judged by it; this package keeps its names, so the differential suites that
// compare it with the resource layer's lowering read as they did.

// Truth3 is condition.Truth3: a three-valued truth value.
type Truth3 = condition.Truth3

// The three truth values, condition's.
const (
	False   = condition.False
	Unknown = condition.Unknown
	True    = condition.True
)

// Date is condition.Date: a civil date, compared as a calendar day.
type Date = condition.Date

// Image is condition.Image: everything one evaluation reads.
type Image = condition.Image

// ParseDate is condition.ParseDate: the language's date literal form, YYYY-MM-DD.
var ParseDate = condition.ParseDate

// Evaluate is condition.Evaluate: the expression's three-valued truth over the image.
var Evaluate = condition.Evaluate
