package rpc

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type (
	// ReleaseConsignment stamps a consignment's release to its owner. It is served on both
	// the default and droids outlets: a droid releases cargo by API key through the same
	// generated surface humans use. The once-only rule is the grant, not the body: the
	// Supercargo's and the droid's Execute grants carry `releasedAt IS NULL`, evaluated by
	// the frame against the located row (@target(Consignment)), so a second release is the
	// frame's uniform Forbidden.
	//
	// The body reads the manifest ARMED (Enforce(caller)) before it stamps, so the read
	// runs under the caller's own Read grant: the droid sees only the fields its grant
	// names, and a caller with no Read on the manifest is refused before anything is
	// written. It answers a typed receipt.
	//
	// Demonstrates: @target, execute-condition, outlet.shared, rpc.armed-read, rpc.typed-result.
	//
	// @rpc
	// @permissionScope(domain)
	// @outlet(default, droids)
	ReleaseConsignment struct {
		// @target(Consignment)
		ConsignmentID ccc.UUID
	}

	// Released is the receipt: which bond was released and when.
	Released struct {
		ConsignmentID ccc.UUID
		BondCode      string
		ReleasedAt    time.Time
	}
)

// Execute runs inside the handler's transaction.
func (m *ReleaseConsignment) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) (*Released, error) {
	manifest, err := resources.NewConsignmentQuery().
		AddColumns(resources.NewConsignmentColumns().ID().BondCode()).
		SetID(m.ConsignmentID).
		Enforce(resource.CallerFrom(ctx)).
		Read(ctx, txn)
	if err != nil {
		return nil, errors.Wrap(err, "resources.ConsignmentQuery.Read()")
	}
	if manifest == nil {
		return nil, httpio.NewNotFoundMessagef("consignment %s does not exist", m.ConsignmentID)
	}

	now := time.Now().UTC()
	if err := resources.NewConsignmentUpdatePatch(m.ConsignmentID).SetReleasedAt(&now).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return nil, errors.Wrap(err, "resources.ConsignmentUpdatePatch.Buffer()")
	}

	return &Released{ConsignmentID: m.ConsignmentID, BondCode: manifest.Data.BondCode, ReleasedAt: now}, nil
}
