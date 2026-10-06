package resources

import (
	"time"

	"github.com/cccteam/ccc"
)

type (
	// Commendation is a citation on a pilot's record for a sortie flown well: the
	// commendations desk, a global resource headquarters files into and the crew reads.
	// It is the one resource behind a feature flag: @feature(Commendations) puts every
	// route of it behind the flag the resources package declares (features.go), so while
	// the flag is off the desk does not exist for the API or the browser (its routes
	// answer the router's own 404, its arm of the consolidated patch answers as an
	// unknown resource, and the permission digest leaves it and its fields out), and
	// when the Adjutant turns it on through SetFeature every instance serves it within a
	// moment, with no release and no migration. The table and the grants ship with the
	// release either way; the flag decides whether the desk is there.
	//
	// AwardedBy and AwardedAt are the server's: the session's user and the commit
	// timestamp, written at create and never by the wire. PilotId leads an index with
	// the award time, so the desk lists one pilot's citations in award order off the
	// index, and the field is filterable in the browser.
	//
	// Demonstrates: @feature.
	//
	// @resource
	// @feature(Commendations)
	// @order(AwardedAt desc)
	// @page(default: 25, max: 200)
	Commendation struct {
		ID        ccc.UUID  `spanner:"Id"`
		PilotID   ccc.UUID  `spanner:"PilotId"`
		Citation  string    `spanner:"Citation"`
		AwardedBy string    `spanner:"AwardedBy" conditions:"output_only" default_create_fn:"currentUser"`
		AwardedAt time.Time `spanner:"AwardedAt" conditions:"output_only" default_create_fn:"resource.CommitTimestamp"`
	}
)
