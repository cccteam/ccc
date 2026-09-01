package computedresources

import (
	"context"
	"iter"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/pkg/resources"
	"github.com/go-playground/errors/v5"
)

type (
	// FleetSummary is headquarters' per-waystation rollup: open work orders and
	// requisitions awaiting approval across the ring. It is list-only — the read
	// handler is suppressed — and global: the fleet view is an HQ concern.
	//
	// @computed
	// @suppress(readHandler)
	FleetSummary struct {
		WaystationID        string `spanner:"WaystationId"` // @primarykey
		Name                string `spanner:"Name"`
		OpenWorkOrders      int64  `spanner:"OpenWorkOrders"`
		PendingRequisitions int64  `spanner:"PendingRequisitions"`
	}
)

// Resource implements resource.Resourcer; computed resources declare their resource
// name by hand (there is no generated file to carry it).
func (FleetSummary) Resource() accesstypes.Resource {
	return "FleetSummaries"
}

// ListFleetSummary computes one summary row per waystation.
func ListFleetSummary(ctx context.Context, _ *resource.QuerySet[FleetSummary], client resource.Client, _ *Client) iter.Seq2[*FleetSummary, error] {
	return func(yield func(*FleetSummary, error) bool) {
		for row, err := range resources.NewWaystationQuery().AddColumns(resources.NewWaystationColumns().All()).List(ctx, client) {
			if err != nil {
				yield(nil, errors.Wrap(err, "resources.WaystationQuery.List()"))

				return
			}

			summary := &FleetSummary{WaystationID: row.Data.ID, Name: row.Data.Name}

			orders := resources.NewWorkOrderQuery().
				AddColumns(resources.NewWorkOrderColumns().All()).
				Where(resources.NewWorkOrderQueryClause().WaystationID().Equal(row.Data.ID))
			for order, err := range orders.List(ctx, client) {
				if err != nil {
					yield(nil, errors.Wrap(err, "resources.WorkOrderQuery.List()"))

					return
				}
				switch resources.WorkOrderStatus(order.Data.StatusID) {
				case resources.CompletedWorkOrderStatus, resources.CancelledWorkOrderStatus:
				default:
					summary.OpenWorkOrders++
				}
			}

			requisitions := resources.NewRequisitionQuery().
				AddColumns(resources.NewRequisitionColumns().All()).
				Where(resources.NewRequisitionQueryClause().WaystationID().Equal(row.Data.ID))
			for requisition, err := range requisitions.List(ctx, client) {
				if err != nil {
					yield(nil, errors.Wrap(err, "resources.RequisitionQuery.List()"))

					return
				}
				if resources.RequisitionStatus(requisition.Data.StatusID) == resources.SubmittedRequisitionStatus {
					summary.PendingRequisitions++
				}
			}

			if !yield(summary, nil) {
				return
			}
		}
	}
}
