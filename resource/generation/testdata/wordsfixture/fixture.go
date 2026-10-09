// Package wordsfixture holds one @rpc struct per request log word and trace setting shape
// the extraction accepts or refuses: a method's own words on @rpc, a scheduled method's on
// @schedule, and each declaration a method cannot carry.
package wordsfixture

import (
	"context"

	"github.com/cccteam/ccc/resource"
)

// Client stands in for the application's RPC client.
type Client struct{}

type (
	// @rpc(log: onEvent)
	OnEventHail struct {
		Note string
	}

	// @rpc(log: sampled, fraction: 0.01, trace: capped, rate: 0.1)
	SampledHail struct {
		Note string
	}

	// @rpc(trace: off)
	QuietHail struct {
		Note string
	}

	// @rpc(max: 64KB, log: never)
	BoundedHail struct {
		Note string
	}

	// @rpc(log: sometimes)
	BadWord struct {
		Note string
	}

	// @rpc(fraction: 0.5)
	FractionAlone struct {
		Note string
	}

	// @rpc(log: onEvent)
	// @schedule("30 3 * * *")
	ScheduledWord struct{}

	// @rpc(trace: off)
	// @schedule("30 3 * * *")
	ScheduledTrace struct{}

	// @rpc(log: onEvent)
	// @suppress(allHandlers)
	SuppressedWord struct {
		Note string
	}

	// @rpc
	// @schedule("30 3 * * *", log: onEvent)
	ScheduledOnEvent struct{}

	// @rpc
	// @schedule("30 3 * * *", zone: "America/Denver", log: sampled, fraction: 0.5)
	ScheduledSampled struct{}

	// @rpc
	// @schedule("30 3 * * *", log: loud)
	ScheduledBadWord struct{}

	// @rpc
	// @schedule("30 3 * * *", trace: off)
	ScheduledTraceArgument struct{}
)

func (*OnEventHail) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*SampledHail) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*QuietHail) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*BoundedHail) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*BadWord) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*FractionAlone) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*ScheduledWord) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*ScheduledTrace) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*SuppressedWord) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*ScheduledOnEvent) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*ScheduledSampled) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*ScheduledBadWord) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*ScheduledTraceArgument) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
