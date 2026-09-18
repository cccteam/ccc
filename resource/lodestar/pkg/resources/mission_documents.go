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
	// commit claims it. The row says which stored object is its file, and the generator
	// serves that file under the row's read route: StoreKey's @file makes
	// GET .../mission-documents/{id}/content answer the bytes, typed by ContentType and
	// named by FileName, gated by Read on MissionDocuments and a Read grant on content,
	// the route's own field. The key column itself is off the wire both ways: no list or
	// read returns it, no create or update accepts it, and no TypeScript interface names
	// it; NOT NULL, it leaves the resource no Create, since a row is added by the upload
	// method that stores its file. The resource serves the listing on the console and on
	// the client portal, whose grant holds no content, so the client lists documents and
	// cannot download them. Provenance is the document's origin as one JSON column typed
	// by a plain struct. Digest is the SHA-256 of the stored bytes, a BYTES(32) column the
	// method computes from the file the frame streamed: a []byte is one leaf to the
	// generator, a string in both clients' interfaces (encoding/json carries it as base64)
	// with display type bytes, never a number[].
	//
	// Demonstrates: @upload, @file.stored, outlet.shared, @suppress, typescript.derived-object, typescript.byte-slice.
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
		MissionID   ccc.UUID `spanner:"MissionId"`
		Title       string   `spanner:"Title"`
		FileName    string   `spanner:"FileName"`
		ContentType string   `spanner:"ContentType"`
		Size        int64    `spanner:"Size"`
		// @file(name: FileName, type: ContentType)
		StoreKey   string    `spanner:"StoreKey"`
		UploadedBy string    `spanner:"UploadedBy"`
		UploadedAt time.Time `spanner:"UploadedAt"`
		// Provenance is nullable: a document filed before the origin was recorded has none.
		Provenance *Provenance `spanner:"Provenance"`
		Digest     []byte      `spanner:"Digest"`
	}
)
