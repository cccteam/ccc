package computedresources

import (
	"context"
	"iter"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

type (
	// StandingOrder is one line of the service's standing orders: the directives every
	// crew member flies under, kept as ranked free text in a book that lives outside
	// the schema. A line has no identity of its own, so no field is @primarykey, and
	// that makes the resource the KEY-LESS LIST: a whole read-only list. One list
	// route serves every line the filter admits in one response, in the book's own
	// order (section by section) when no sort is asked and sorted when one is; there
	// is no read route, since a line has no key to read it by, no page (a numeric
	// limit or a cursor is refused with a 400 naming @primarykey as the way to page,
	// and @page would be refused at generation), and no row identity in the browser.
	// Declaring a key would bring all three back. The section is filterable, so a
	// narrowed book still arrives whole in one response.
	//
	// Demonstrates: computed.keyless, @computed.
	//
	// @computed
	StandingOrder struct {
		Section   string `spanner:"Section" allow_filter:"true"`
		Directive string `spanner:"Directive"`
	}
)

// Resource implements resource.Resourcer.
func (StandingOrder) Resource() accesstypes.Resource {
	return "StandingOrders"
}

// standingOrders is the book: its sequence is the order a sort-less list serves, section
// by section, which is neither the sections' alphabetical order nor any key's.
var standingOrders = []StandingOrder{
	{Section: "General", Directive: "The sector marshal's word is final while a mission is underway."},
	{Section: "General", Directive: "Every hail is answered, and every distress call is logged before it is judged."},
	{Section: "Flight", Directive: "No launch without a filed briefing sheet and a named flight lead."},
	{Section: "Flight", Directive: "A hazard-4 lane is flown by a certified pilot or not at all."},
	{Section: "Hangar", Directive: "A hull leaves the quarantine bay only after its inspection is signed."},
	{Section: "Salvage", Directive: "Bonded cargo is released once, to the consignee, against the bond."},
}

// ListStandingOrder yields the book; the generated handler applies the request's filter
// and sort over it, and there is no page to apply.
func ListStandingOrder(_ context.Context, _ *resource.QuerySet[StandingOrder], _ resource.Client, _ *Client) iter.Seq2[*StandingOrder, error] {
	return func(yield func(*StandingOrder, error) bool) {
		for i := range standingOrders {
			if !yield(&standingOrders[i], nil) {
				return
			}
		}
	}
}
