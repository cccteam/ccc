package rpc

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/go-playground/errors/v5"
)

type (
	// ReleaseConsignment stamps a consignment's release to its owner. It is served on
	// both the default and droids outlets — a droid releases cargo by API key through
	// the same generated surface humans use. The once-only rule is the grant, not the
	// body: the Supercargo's and the droid's Execute grants carry `releasedAt IS NULL`,
	// evaluated by the frame against the located row (@target(Consignment)), so a
	// second release is the frame's uniform Forbidden and the body shrinks to the
	// stamp.
	//
	// @rpc
	// @permissionScope(domain)
	// @outlet(default, droids)
	ReleaseConsignment struct {
		// @target(Consignment)
		ConsignmentID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *ReleaseConsignment) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	now := time.Now().UTC()
	if err := resources.NewConsignmentUpdatePatch(m.ConsignmentID).SetReleasedAt(&now).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.ConsignmentUpdatePatch.Buffer()")
	}

	return nil
}
