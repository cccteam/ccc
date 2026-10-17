// live.go reads what each environment runs, for a command by hand (bedrock patch
// status): the stack's state in the state bucket names the records bucket, where the
// newest live record of a release says what the environment runs.

package deploy

import (
	"context"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// LiveRelease is what an environment runs: the release its newest live record names,
// when it was recorded and by which build. Applied says the environment's stack is
// applied (its state names the records bucket); Release is empty when nothing is live.
type LiveRelease struct {
	Env     string
	Applied bool
	Release string
	Since   string
	Build   string
}

// LiveReleases reads every environment of the placement, in promotion order.
func LiveReleases(ctx context.Context, clients *Clients, p *derive.Placement, app string) ([]LiveRelease, error) {
	store, err := clients.Storage(ctx)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	live := make([]LiveRelease, 0, len(p.Environments))
	for _, env := range p.Environments {
		l := LiveRelease{Env: env}
		state, ok, err := readState(ctx, store, p.StateBucket, statePrefix(app, env))
		if err != nil {
			return nil, err
		}
		if !ok || len(state.Outputs) == 0 {
			live = append(live, l)

			continue
		}
		subs, err := state.stringMap(substitutionsOutput)
		if err != nil {
			return nil, err
		}
		if subs[recordsBucket] == "" {
			live = append(live, l)

			continue
		}
		l.Applied = true
		r, err := newestLiveRelease(ctx, store, subs[recordsBucket], app, env)
		if err != nil {
			return nil, err
		}
		if r != nil {
			l.Release, l.Since, l.Build = r.Version, r.Timestamp, r.Build
		}
		live = append(live, l)
	}

	return live, nil
}
