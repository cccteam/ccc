package resource

import (
	"sync/atomic"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// localZone is the zone the bare word local resolves to inside a temporal
// grant condition (design plan §05), attached to every request Environment
// once wired.
var localZone atomic.Pointer[time.Location]

// SetLocalZone wires the timezone the bare word local resolves to inside a
// temporal grant condition — timeOfDay(now, local), dayOfWeek(now, local)
// (design plan §05). Call it once at application startup, before serving;
// where the zone comes from is the application's business (a config constant
// today; a session- or tenant-resolved source rides the same seam later).
// Unwired, a condition using local fails its check loudly at first use — the
// same posture as a missing now — while explicit-zone conditions need no
// wiring at all.
func SetLocalZone(zone *time.Location) {
	localZone.Store(zone)
}

// newRequestEnvironment samples the request's decision context. The decoders
// are the single construction point: each decoded artifact carries one
// Environment, so the instant a permission check folds conditions against is
// the identical instant later bound into SQL — the two consumers can never
// disagree about a window boundary.
func newRequestEnvironment() accesstypes.Environment {
	env := accesstypes.NewEnvironment().WithNow(time.Now())
	if zone := localZone.Load(); zone != nil {
		env = env.WithZone(zone)
	}

	return env
}

// errConditionalAtDecode reports a Conditional decision reaching a
// decode-time-only permission check (RPC methods and computed resources).
// Conditions evaluate at the data layer, and these paths have no library
// execution underneath them to evaluate one, so a condition here is
// unenforceable: the deployment invariant is that MigrateRoles rejects
// row-referencing conditions on RPC and computed grants, and row-free
// conditions fold inside the check itself. Reaching this is a 500-class
// invariant breach — never an allow, never a 403.
func errConditionalAtDecode(perm accesstypes.Permission, conditional []accesstypes.Resource) error {
	return errors.Newf("invariant breach: conditional grants for (%s) on %s reached a decode-time-only permission check, where conditions cannot be evaluated", perm, conditional)
}
