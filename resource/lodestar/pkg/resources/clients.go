package resources

import "github.com/cccteam/ccc"

type (
	// Client is who books missions: a global resource with PII contact fields and a bool
	// attribute. The ClientBrowser role lists clients under a `trusted = true` condition,
	// so untrusted outfits disappear from booking pickers while headquarters
	// (unconditional grants) still sees and manages them.
	//
	// Client is excluded from the consolidated patch handler, keeping a global standalone
	// PATCH surface in the demo.
	//
	// ContactEmail is additionally allow_filter: PII fields may never be filtered through
	// the URL (the value would land in access logs), but a POST-body filter is accepted;
	// the query-parameter suite pins both directions.
	//
	// Name is unique across every client (ClientsByName). Client is global, untracked,
	// and updated under unconditional grants, so an update reads nothing before the
	// commit: renaming one client to another's name is refused there as a duplicate
	// unique value (409), and an update under an id that does not exist is refused there
	// as a missing row (404), both in the resource's name.
	//
	// Insured records whether the outfit carries salvage cover: unknown until the booking
	// desk hears from the underwriter, then yes or no, recorded by headquarters from the
	// Clients page. The column is nullable and the field a pointer, so the row scans NULL;
	// the generated TypeScript types the field NullBoolean, its metadata says nullboolean,
	// the console renders the three-way picker beside Trusted's two-state checkbox, and a
	// null in a PATCH body writes the column back to NULL.
	//
	// Demonstrates: pii, pii.filter-placement, allow_filter, @attribute.bool, consolidation.exclusion, commit.constraint-refusal, nullboolean.
	//
	// @resource
	// @order(Name asc)
	// @page(default: 25, max: 200)
	Client struct {
		ID           ccc.UUID `spanner:"Id"`
		Name         string   `spanner:"Name"`
		ContactName  string   `spanner:"ContactName"  conditions:"pii"`
		ContactEmail string   `spanner:"ContactEmail" allow_filter:"true" conditions:"pii"`
		// @attribute(trusted)
		Trusted bool  `spanner:"Trusted"`
		Insured *bool `spanner:"Insured"`
	}
)
