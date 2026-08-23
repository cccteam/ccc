package resources

import (
	"time"

	"github.com/cccteam/ccc"
)

type (
	// Ship is a structurally enforced resource: no field carries any permission
	// annotation, and every non-primary-key field requires the endpoint's permission
	// on its field resource. The invariant integration suite depends on this.
	//
	// @resource
	Ship struct {
		ID           ccc.UUID     `spanner:"Id"`
		RegistryCode string       `spanner:"RegistryCode" conditions:"immutable"`
		Name         string       `spanner:"Name"`
		DockingBayID ccc.NullUUID `spanner:"DockingBayId"`
		CargoValue   int64        `spanner:"CargoValue"`
		UpdatedAt    *time.Time   `spanner:"UpdatedAt"    output_only_update_fn:"resource.CommitTimestampPtr"`
	}
)
