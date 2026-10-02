package auth

import (
	"context"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/resource"
	"github.com/go-playground/errors/v5"
)

// Signals is what the policy signal asks of the live service: the signaler and the
// subscriber over the kinds the resource package declares. live.Service carries both, as
// the in-memory fake does.
type Signals interface {
	resource.Signaler
	resource.SignalSubscriber
}

// PolicySignal is the permission engines' change signal over the application's one
// signal channel: access.ChangeSignalFunc over the live service's Signal and Subscribe on
// the policy kind. The engine that wrote a role, a grant or a membership announces the
// kind after the write (a failed announce is the engine's to log; the write stands, and
// the heartbeat carries the change within a minute), and every engine on every instance
// watching the kind rereads its policy on the signal, so a membership written on one
// instance is served by the others at their next request. Both auths' engines take the
// one adapter, so the crew store and the members store ride the one listener on the one
// document the feature flags and the tenant roster ride.
//
// Demonstrates: live.signals.
func PolicySignal(signals Signals) access.ChangeSignal {
	return access.ChangeSignalFunc(
		func(ctx context.Context) error {
			if err := signals.Signal(ctx, resource.KindPolicy); err != nil {
				return errors.Wrap(err, "resource.Signaler.Signal()")
			}

			return nil
		},
		func(ctx context.Context, onChange func()) error {
			stop, err := signals.Subscribe(resource.KindPolicy, onChange)
			if err != nil {
				return errors.Wrap(err, "resource.SignalSubscriber.Subscribe()")
			}
			defer stop()
			<-ctx.Done()

			return nil
		},
	)
}
