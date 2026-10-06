// Package featureduplicate declares one flag name twice, which the generator refuses.
package featureduplicate

import "github.com/cccteam/ccc/resource"

const (
	// Debriefs is the first declaration.
	Debriefs resource.Feature = "debriefs"
	// Reports declares the same name again.
	Reports resource.Feature = "debriefs"
)
