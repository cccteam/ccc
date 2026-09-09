package rpc

import (
	"context"
	"net/http"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/go-playground/errors/v5"
)

type (
	// HailShip is the first-class Touch and the plain located-row form: the method moves
	// no state but declares its target row (@target(Ship)), so the generated frame locates
	// the ship within the sector, through its one-hop join-path tenancy, and evaluates the
	// caller's Execute condition against it (the Pilot's grant carries `hangarZone !=
	// 'quarantine'`); then the body fires the generated NewShipTouch: UpdatedAt bumps and
	// the ship's log records who hailed, while no field of the ship changes. It declares
	// No Content as its one status: a touch has nothing to say, so no body goes on the
	// wire. The result is a pointer returned nil, the form the frame writes as No Content
	// (the answerless form does not generate today; see the round-3 build report).
	//
	// Demonstrates: touch, @target, execute-condition, @answers.no-content, @attribute.join-path.
	//
	// @rpc
	// @permissionScope(domain)
	// @answers(204)
	HailShip struct {
		// @target(Ship)
		ShipID ccc.UUID
	}

	// Hailed is the empty answer: never written, since a hail answers No Content.
	Hailed struct{}
)

// HTTPStatus is No Content; a hail has nothing to say.
func (*Hailed) HTTPStatus() int {
	return http.StatusNoContent
}

// Execute runs inside the handler's transaction and answers nothing.
func (m *HailShip) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) (*Hailed, error) {
	if err := resources.NewShipTouch(m.ShipID).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return nil, errors.Wrap(err, "resources.ShipTouch.Buffer()")
	}

	return nil, nil
}
