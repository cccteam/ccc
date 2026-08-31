package resource

import (
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// newRequestEnvironment samples the request's decision context. The decoders
// are the single construction point: each decoded artifact carries one
// Environment, so the instant a permission check folds conditions against is
// the identical instant later bound into SQL — the two consumers can never
// disagree about a window boundary.
func newRequestEnvironment() accesstypes.Environment {
	return accesstypes.NewEnvironment().WithNow(time.Now())
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
