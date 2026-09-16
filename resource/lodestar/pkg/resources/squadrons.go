package resources

import "github.com/cccteam/ccc"

type (
	// Squadron is a flight unit. Its table carries no tenancy column: the @domain binding
	// leaves through the WingId foreign key and resolves the sector one hop away (via:
	// names remote Go field segments), the single-hop form of join-path tenancy. The same
	// column is the wing attribute the Wing Commander's grant compares against
	// subject.wings.
	//
	// Callsigns are the handles a squadron answers to on the open channel, filed by the
	// sector marshal: a nullable ARRAY<STRING(16)> column typed by the plain []string,
	// where NULL and [] say different things. NULL is a squadron that has not filed yet;
	// [] is one that flies silent and says so. A Go slice has one form, so the field's
	// nullability is the column's: the generator leaves slices out of its nullability
	// check, the metadata says required: false, and the generated request struct carries
	// nullable:"true", so a null in a PATCH clears the filing, while a null into a NOT
	// NULL array column (Ships.CargoBays, which carries no tag) is refused at decode as
	// cannot be null. The Spanner client reads NULL into a nil slice and writes nil as
	// NULL, and the wire carries nil as null, so nothing else moves; a pointer to the
	// slice would fail the client's first read and is refused at generation naming the
	// plain slice.
	//
	// Demonstrates: @domain.join-path, @attribute, decode.nullable-slice.
	//
	// @resource
	// @permissionScope(domain)
	// @order(Name asc)
	// @page(default: 25, max: 200)
	Squadron struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain(via: SectorID)
		// @attribute(wing)
		WingID    ccc.UUID `spanner:"WingId"`
		Name      string   `spanner:"Name"`
		Callsigns []string `spanner:"Callsigns"`
	}
)
