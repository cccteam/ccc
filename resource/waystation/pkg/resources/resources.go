// Package resources provides the resource types for the waystation.
package resources

import (
	"context"
	"fmt"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/session/sessioninfo"
	"github.com/shopspring/decimal"
)

func defaultConfig() resource.Config {
	return resource.Config{
		TrackChanges: false,
	}
}

// currentUser is a FieldDefaultFunc that stamps the requesting user onto rows the
// server attributes to their author (WorkOrder.CreatedBy, Requisition.RequestedBy).
// The fields are output_only, so the wire can never supply them — authorship always
// comes from the session, which is also the value grant conditions compare subject
// against. The automation outlet binds requests to its service identity, so the
// session is present on every mutation path.
func currentUser(ctx context.Context, _ resource.ReadWriteTransaction) (any, error) {
	return sessioninfo.FromCtx(ctx).Username, nil
}

// defaultTotalCost starts every requisition at zero: line items accumulate value and
// SubmitRequisition recomputes and freezes the total inside the transition — the
// client can never assert its own total.
//
// NUMERIC columns require the jtwatson/decimal fork (see the replace directive in
// go.mod): it adds the Spanner Encoder/Decoder support upstream shopspring lacks.
func defaultTotalCost(_ context.Context, _ resource.ReadWriteTransaction) (any, error) {
	return decimal.Zero, nil
}

// defaultCaseNumber issues the server-assigned incident case number; the field is
// output_only so a client-supplied value is rejected rather than ignored.
func defaultCaseNumber(_ context.Context, _ resource.ReadWriteTransaction) (any, error) {
	id, err := ccc.NewUUID()
	if err != nil {
		return nil, fmt.Errorf("generating case number: %w", err)
	}

	return fmt.Sprintf("IR-%s", id.String()[:8]), nil
}
