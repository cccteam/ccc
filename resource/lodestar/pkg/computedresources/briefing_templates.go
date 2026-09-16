package computedresources

import (
	"context"
	"iter"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

type (
	// BriefingTemplate is a catalog row served from Go: the sheets a briefing can be
	// compiled on. It stands in for a catalog held outside the schema — a template
	// service, say — so nothing in the database references it: Mission.BriefingTemplateID
	// is a plain column that names this resource with a field-scope @enumerate, and the
	// picker lists these rows. The keyed read route is suppressed, as a service that only
	// lists its catalog would have no read to offer; the browser resolves a picked
	// template's display from the option list instead. Mission is served on the portal
	// outlet too, and a declared enumeration must be on every outlet its field is, so
	// the catalog is as well.
	//
	// The catalog declares no @order and no maximum: read whole (limit=all), it keeps
	// its sheets in the sequence a template service would rank them, the standard sheet
	// first, passed through untouched, neither by name nor by key; a request sort still
	// sorts and pages them, and a page asked without a sort is refused, since every
	// paged request carries an order.
	//
	// Demonstrates: @enumerate.plain-column, picker.read-disabled, @computed, @suppress, outlet.shared, order.none.
	//
	// @computed
	// @suppress(readHandler)
	// @outlet(default, portal)
	BriefingTemplate struct {
		ID       string `spanner:"Id"` // @primarykey
		Name     string `spanner:"Name"`
		Audience string `spanner:"Audience"`
		Summary  string `spanner:"Summary"`
	}
)

// Resource implements resource.Resourcer.
func (BriefingTemplate) Resource() accesstypes.Resource {
	return "BriefingTemplates"
}

// StandardBriefingTemplate is the sheet a briefing compiles on when none is named.
const StandardBriefingTemplate = "standard"

// briefingTemplates is the fixed catalog: identifiers the program reasons about only by
// listing them, never as constants. Its sequence is the catalog's own ranking, the order
// a sort-less list serves.
var briefingTemplates = []BriefingTemplate{
	{ID: StandardBriefingTemplate, Name: "Standard sheet", Audience: "sector crew", Summary: "Mission counts, the fees the caller may see, and the overdue list."},
	{ID: "hazard-first", Name: "Hazard-first sheet", Audience: "flight leads", Summary: "The hazard board folded in ahead of the mission counts."},
	{ID: "client-facing", Name: "Client summary", Audience: "clients", Summary: "Open missions and deadlines, written for the client's desk."},
	{ID: "dispatch", Name: "Dispatch brief", Audience: "dispatchers", Summary: "The live missions and the squadrons flying them."},
}

// BriefingTemplateByID finds a catalog row by its identifier.
func BriefingTemplateByID(id string) (BriefingTemplate, bool) {
	for _, template := range briefingTemplates {
		if template.ID == id {
			return template, true
		}
	}

	return BriefingTemplate{}, false
}

// ListBriefingTemplate yields the catalog; the generated handler applies the request's
// filter, sort, and page over it.
func ListBriefingTemplate(_ context.Context, _ *resource.QuerySet[BriefingTemplate], _ resource.Client, _ *Client) iter.Seq2[*BriefingTemplate, error] {
	return func(yield func(*BriefingTemplate, error) bool) {
		for i := range briefingTemplates {
			if !yield(&briefingTemplates[i], nil) {
				return
			}
		}
	}
}
