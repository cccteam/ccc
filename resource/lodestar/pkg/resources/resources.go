// Package resources provides the resource types for Lodestar: the rescue and salvage
// service's sectors, clients, ships, crews, missions, refits, and cargo. Every
// permission rule in the application is either structural (an annotation here) or a
// conditional grant in cmd/bootstrap/demo_access.json; no handler carries one.
package resources

import (
	"context"
	"fmt"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/session/sessioninfo"
)

func defaultConfig() resource.Config {
	return resource.Config{
		TrackChanges: false,
	}
}

// currentUser is a FieldDefaultFunc that stamps the requesting user onto rows the
// server attributes to their author (Mission.BookedBy, DistressCall.FiledBy,
// Refit.OpenedBy). The fields are output_only, so the wire can never supply them —
// authorship always comes from the session, which is also the value grant conditions
// compare subject against (bookedBy = subject, filedBy = subject). The droid outlet
// binds requests to its service identity, so the session is present on every
// mutation path.
func currentUser(ctx context.Context, _ resource.ReadWriteTransaction) (any, error) {
	return sessioninfo.FromCtx(ctx).Username, nil
}

// defaultCaseNumber issues the server-assigned distress-call case number; the field
// is output_only so a client-supplied value is rejected rather than ignored.
func defaultCaseNumber(_ context.Context, _ resource.ReadWriteTransaction) (any, error) {
	id, err := ccc.NewUUID()
	if err != nil {
		return nil, fmt.Errorf("generating case number: %w", err)
	}

	return fmt.Sprintf("DC-%s", id.String()[:8]), nil
}
