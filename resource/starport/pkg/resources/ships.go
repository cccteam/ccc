package resources

import (
	"time"

	"github.com/cccteam/ccc"
)

type (
	// Ship is a fully tagged resource: every non-primary-key field carries an explicit
	// perm tag, so the field-permission default (fail open today, fail closed in the
	// future) is never consulted. The invariant integration suite depends on this.
	//
	// @resource
	Ship struct {
		ID           ccc.UUID     `spanner:"Id"`
		RegistryCode string       `spanner:"RegistryCode" conditions:"immutable"                              perm:"Read,List,Create"`
		Name         string       `spanner:"Name"         perm:"Read,List,Create,Update"`
		DockingBayID ccc.NullUUID `spanner:"DockingBayId" perm:"Read,List,Create,Update"`
		CargoValue   int64        `spanner:"CargoValue"   perm:"Read,List,Create,Update"`
		UpdatedAt    *time.Time   `spanner:"UpdatedAt"    output_only_update_fn:"resource.CommitTimestampPtr" perm:"Read,List"`
	}
)
