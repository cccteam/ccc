package resources

import (
	"time"

	"github.com/cccteam/ccc"
)

type (
	// Shipment is an inbound delivery, served on both the browser and automation
	// outlets — dock systems receive shipments by API key through the same generated
	// surface humans use. ArrivedAt doubles as the shipment's lifecycle marker: NULL
	// means in transit, and the quartermaster's update grant carries
	// `arrivedAt IS NULL`, so closed-out shipments go read-only.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(default, automation)
	Shipment struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		WaystationID string   `spanner:"WaystationId"`
		SupplierID   ccc.UUID `spanner:"SupplierId"`
		ManifestCode string   `spanner:"ManifestCode" conditions:"immutable"`
		// @attribute(arrivedAt)
		ArrivedAt *time.Time `spanner:"ArrivedAt"`
	}
)
