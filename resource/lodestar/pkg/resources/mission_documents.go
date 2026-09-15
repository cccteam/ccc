package resources

import (
	"time"

	"github.com/cccteam/ccc"
)

type (
	// Provenance is where a document came from, kept beside it as one JSON column: the
	// system that produced the file, that system's own reference for it, and when it
	// arrived there. It is a plain struct with json tags and nothing else: the generator
	// derives its TypeScript interface (MissionDocuments.Provenance, in both clients'
	// resources files) from these fields, and writes the Spanner methods that store it
	// (zz_gen_storage.go), so the application declares the shape once, here.
	//
	// Demonstrates: typescript.derived-object.
	Provenance struct {
		System     string    `json:"system"`
		Reference  string    `json:"reference,omitempty"`
		ReceivedAt time.Time `json:"receivedAt"`
	}

	// MissionDocument is a file attached to a mission: a brief, a chart, a manifest. The
	// row is what the transaction claims when AttachMissionDocument runs: the frame streams
	// each file to the store under a minted key, the body records the key here, and the
	// frame promotes it after commit. Reading the bytes back is the application's own
	// route (MissionDocumentContent); the resource serves the listing, on the console and
	// on the client portal, whose grant leaves storeKey and uploadedBy out. Provenance is
	// the document's origin as one JSON column typed by a plain struct.
	//
	// Demonstrates: @upload, outlet.shared, @suppress, typescript.derived-object.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(default, portal)
	// @suppress(patchHandler)
	// @order(UploadedAt desc)
	// @page(default: 25, max: 200)
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
		// Provenance is nullable: a document filed before the origin was recorded has none.
		Provenance *Provenance `spanner:"Provenance"`
	}
)
