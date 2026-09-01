package resources

type (
	// WorkOrderStatus enumerates the maintenance workflow's states; the generated
	// constants are the vocabulary the RPC transition bodies use, so an edge rule
	// like scheduled -> in_progress is spelled in checked constants, never strings.
	//
	// @enumerate(WorkOrderStatuses)
	WorkOrderStatus string

	// RequisitionStatus enumerates the purchasing workflow's states.
	//
	// @enumerate(RequisitionStatuses)
	RequisitionStatus string

	// ItemCategory enumerates catalog item categories.
	//
	// @enumerate(ItemCategories)
	ItemCategory string

	// DeclineReason enumerates the reasons a requisition can be declined.
	//
	// @enumerate(DeclineReasons)
	DeclineReason string
)
