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
	// Ships are also served on the automation outlet: the machine REST API composed
	// behind API-key authentication instead of the browser session.
	//
	// @resource
	// @outlet(default, automation)
	Ship struct {
		ID           ccc.UUID     `spanner:"Id"`
		RegistryCode string       `spanner:"RegistryCode" conditions:"immutable"`
		Name         string       `spanner:"Name"`
		DockingBayID ccc.NullUUID `spanner:"DockingBayId"`
		CargoValue   int64        `spanner:"CargoValue"`
		UpdatedAt    *time.Time   `spanner:"UpdatedAt"    output_only_update_fn:"resource.CommitTimestampPtr"`
	}
)
