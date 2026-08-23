// Package reservedmarkerfixture provides a fixture whose @manualAddResource constant
// value carries the reserved marker character ':' — extraction must reject it, because
// a caller-authored resource name may never collide with an access-defined sentinel.
package reservedmarkerfixture

import "github.com/cccteam/ccc/accesstypes"

// @manualAddResource(Execute)
const MarkedThing accesstypes.Resource = "access:things"
