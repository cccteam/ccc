package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// WorkOrderTask is a checklist item: interleaved in WorkOrders with a compound
	// primary key, so creates supply the full key (no server-generated UUID). The
	// @stateRoot marker on the anchoring foreign key makes it a member of the
	// WorkOrder workflow: the uniform `state` binding is synthesized as a join path
	// through the chain, so `state = 'in_progress'` in a grant condition reads
	// identically here and on the root.
	//
	// @resource
	// @permissionScope(domain)
	WorkOrderTask struct {
		// The parent-key column is named Id because Spanner interleaving requires the
		// child's leading key column to carry the parent's key column name; the Go
		// field keeps the readable name. The task's tenant is its work order's
		// station, declared as a join-path @domain through the same anchoring key.
		//
		// @stateRoot(WorkOrder)
		// @domain(via: WaystationID)
		WorkOrderID  ccc.UUID `spanner:"Id"`
		TaskNumber   int64    `spanner:"TaskNumber"`
		Instructions string   `spanner:"Instructions"`
		Done         bool     `spanner:"Done"`
	}
)
