package resources

import (
	"encoding/json"

	"github.com/cccteam/ccc"
	"github.com/go-playground/errors/v5"
)

// Position is where a distress call was made, as a GeoJSON Point. Its shape lives
// outside Go, in the GeoJSON specification and the geojson TypeScript package, so the
// type declares its TypeScript type once, here: both clients' resources files import
// Point from geojson, and the column is typed Point. The Go side keeps the raw JSON and
// writes it as it is, so the two JSON methods are the whole implementation; the Spanner
// methods that store it are generated (zz_gen_storage.go).
//
// Demonstrates: typescript.imported-type.
//
// @typescript(Point, from: "geojson")
type Position json.RawMessage

// MarshalJSON writes the point as it was received.
func (p Position) MarshalJSON() ([]byte, error) {
	b, err := json.RawMessage(p).MarshalJSON()
	if err != nil {
		return nil, errors.Wrap(err, "json.RawMessage.MarshalJSON()")
	}

	return b, nil
}

// UnmarshalJSON keeps the point as it was received.
func (p *Position) UnmarshalJSON(b []byte) error {
	if err := (*json.RawMessage)(p).UnmarshalJSON(b); err != nil {
		return errors.Wrap(err, "json.RawMessage.UnmarshalJSON()")
	}

	return nil
}

type (
	// DistressCall is an incoming call before it becomes a mission, and the
	// field-direction showcase: CallerContact is PII (rejected in URL filters, flagged in
	// TS metadata) and nullable, since the Cadet's partial-width Create grant (summary,
	// severity) files calls without it, so the column must accept the narrowed create;
	// Transcript is input_only (accepted on mutations, never serialized back); CaseNumber
	// is output_only with a server-issued DC- default; FiledBy is output_only, stamped
	// from the session, and the attribute the portal's filedBy = subject grant reads.
	//
	// Served on the portal outlet too: Client Cleo files calls through a three-field form,
	// the one PII field an external user writes. Position is where the call was made, a
	// GeoJSON Point whose TypeScript type the Position type declares.
	//
	// Demonstrates: pii, input_only, output_only, default_create_fn, create-form-narrowing, outlet.shared, typescript.imported-type.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(default, portal)
	// @order(Severity desc)
	// @page(default: 10, max: 100)
	DistressCall struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		SectorID      string  `spanner:"SectorId"`
		Summary       string  `spanner:"Summary"`
		Severity      int64   `spanner:"Severity"`
		CallerContact *string `spanner:"CallerContact" conditions:"pii"`
		Transcript    *string `spanner:"Transcript"    conditions:"input_only"`
		CaseNumber    string  `spanner:"CaseNumber"    conditions:"output_only" default_create_fn:"defaultCaseNumber"`
		// @attribute(filedBy)
		FiledBy string `spanner:"FiledBy" conditions:"output_only" default_create_fn:"currentUser"`
		// Position is nullable: a call relayed by voice may name no point.
		Position *Position `spanner:"Position"`
	}
)
