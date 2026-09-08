package resources

import (
	"time"

	"github.com/cccteam/ccc"
)

type (
	// MissionDocument is a file attached to a mission: a brief, a chart, a manifest.
	// The row is what the transaction claims when AttachMissionDocument runs: the
	// frame streams each file to the store under a minted key, the body records the
	// key here, and the frame promotes it after commit. Reading the bytes back is the
	// application's own route (MissionDocumentContent); the resource serves the
	// listing, on the console and on the client portal, whose grant leaves storeKey
	// and uploadedBy out.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(default, portal)
	// @suppress(patchHandler)
	// @order(UploadedAt desc)
	MissionDocument struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain(via: SectorID)
		// @attribute(client, via: ClientID)
		MissionID   ccc.UUID  `spanner:"MissionId"`
		Title       string    `spanner:"Title"`
		FileName    string    `spanner:"FileName"`
		ContentType string    `spanner:"ContentType"`
		Size        int64     `spanner:"Size"`
		StoreKey    string    `spanner:"StoreKey"`
		UploadedBy  string    `spanner:"UploadedBy"`
		UploadedAt  time.Time `spanner:"UploadedAt"`
	}
)
