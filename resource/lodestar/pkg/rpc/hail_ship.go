package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/go-playground/errors/v5"
)

// HailShip is the first-class Touch and the plain located-row form: the method moves
// no state but declares its target row (@target(Ship)), so the generated frame locates
// the ship within the sector, through its one-hop join-path tenancy, and evaluates the
// caller's Execute condition against it (the Pilot's grant carries `hangarZone !=
// 'quarantine'`); then the body fires the generated NewShipTouch: UpdatedAt bumps and
// the ship's log records who hailed, while no field of the ship changes. It declares
// No Content as its one status: a touch has nothing to say, so no body goes on the
// wire, and the answerless form is the only one that may say so.
//
// Demonstrates: touch, @target, execute-condition, @answers.no-content, @attribute.join-path.
//
// @rpc
// @permissionScope(domain)
// @answers(204)
type HailShip struct {
	// @target(Ship)
	ShipID ccc.UUID
}

// Execute runs inside the handler's transaction and answers nothing.
func (m *HailShip) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	if err := resources.NewShipTouch(m.ShipID).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.ShipTouch.Buffer()")
	}

	return nil
}
