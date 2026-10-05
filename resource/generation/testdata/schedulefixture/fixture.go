// Package schedulefixture holds one @rpc struct per @schedule shape the extraction
// accepts or refuses: a schedule with and without its zone, in both Execute forms, and
// each cron expression, zone and declaration a scheduled method cannot carry.
package schedulefixture

import (
	"context"

	"github.com/cccteam/ccc/resource"
)

// Client stands in for the application's RPC client.
type Client struct{}

// Pruned is what a scheduled method may answer with.
type Pruned struct {
	Deleted int64
}

type (
	// @rpc
	// @schedule("30 3 * * *", zone: "America/Denver")
	PruneLogs struct{}

	// @rpc
	// @schedule("*/15 * * * *")
	SweepEveryQuarterHour struct{}

	// @rpc
	// @schedule("0 6,18 1-15 JAN-MAR MON-FRI")
	SendDigest struct{}

	// @rpc
	// @schedule("0 4 * * SUN")
	TallyWeek struct{}

	// @rpc
	// @schedule("0 3 * *")
	FourFields struct{}

	// @rpc
	// @schedule("0 24 * * *")
	HourTooLarge struct{}

	// @rpc
	// @schedule("5/15 * * * *")
	SteppedValue struct{}

	// @rpc
	// @schedule("*/0 * * * *")
	ZeroStep struct{}

	// @rpc
	// @schedule("0 3 * FOO *")
	UnknownMonth struct{}

	// @rpc
	// @schedule("0 5-3 * * *")
	BackwardsRange struct{}

	// @rpc
	// @schedule("0  3 * * *")
	DoubleSpace struct{}

	// @rpc
	// @schedule("0 3 * * *", zone: "Mars/Olympus_Mons")
	UnknownZone struct{}

	// @rpc
	// @schedule("0 3 * * *", zone: "Local")
	LocalZone struct{}

	// @rpc
	// @schedule("0 3 * * *", tz: "UTC")
	UnknownArgument struct{}

	// @rpc
	// @schedule("0 3 * * *")
	TakesInput struct {
		Note string
	}

	// @rpc
	// @schedule("0 3 * * *")
	// @permissionScope(domain)
	DomainScoped struct{}

	// @rpc
	// @schedule("0 3 * * *")
	// @permissionScope(global)
	GlobalScoped struct{}

	// @rpc
	// @schedule("0 3 * * *")
	// @outlet(default)
	OnOutlet struct{}

	// @rpc
	// @schedule("0 3 * * *")
	// @formerly(PruneOld)
	Renamed struct{}

	// @rpc
	// @schedule("0 3 * * *")
	// @suppress(allHandlers)
	Suppressed struct{}

	// @rpc
	// @schedule("0 3 * * *")
	// @upload(max: 5MB)
	Uploads struct{}

	// @rpc
	// @schedule("0 3 * * *")
	Gated struct{}

	// @resource
	// @schedule("0 3 * * *")
	Report struct {
		ID string `spanner:"Id"`
	}
)

func (*PruneLogs) Execute(context.Context, resource.ReadWriteTransaction, *Client) (*Pruned, error) {
	return &Pruned{}, nil
}

func (*SweepEveryQuarterHour) Execute(context.Context, resource.Client, *Client) error {
	return nil
}

func (*SendDigest) Execute(context.Context, resource.Client, *Client) error {
	return nil
}

func (*TallyWeek) Execute(context.Context, resource.Client, *Client) error {
	return nil
}

func (*FourFields) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*HourTooLarge) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*SteppedValue) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*ZeroStep) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*UnknownMonth) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*BackwardsRange) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*DoubleSpace) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*UnknownZone) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*LocalZone) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*UnknownArgument) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*TakesInput) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*DomainScoped) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*GlobalScoped) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*OnOutlet) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*Renamed) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*Suppressed) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*Uploads) Execute(context.Context, resource.ReadWriteTransaction, resource.Files, *Client) error {
	return nil
}

func (*Gated) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
