package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// ReceiveShipment stamps a shipment's arrival — a shipment only arrives once. It
	// is served on both outlets: dock automation receives shipments by API key
	// through the same generated surface humans use.
	//
	// @rpc
	// @permissionScope(domain)
	// @outlet(default, automation)
	ReceiveShipment struct {
		ShipmentID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *ReceiveShipment) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	return receiveShipment(ctx, txn, m.ShipmentID)
}
