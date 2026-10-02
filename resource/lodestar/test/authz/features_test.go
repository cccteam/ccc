package authz

import (
	"context"
	"sync"
	"testing"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	initiator "github.com/cccteam/db-initiator"
	"github.com/go-playground/errors/v5"
)

// featureState is one test database's feature flag setup: done once, its outcome kept
// for every handler built over that database after it.
type featureState struct {
	once sync.Once
	err  error
}

// featureStates holds the setup per test database (*initiator.SpannerDB).
var featureStates sync.Map

// putDeclaredFeaturesOn turns every flag the resources package declares on in a test
// database nothing has written flags into yet, once per database. The generated
// authorization matrix drives every generated route, a gated one included, and pins the
// permission gate behind each; a flag that is off answers 404 before that gate is
// reached, so the matrix runs with every flag on, the state in which its answers hold.
// A database whose FeatureFlags table already holds a declared flag is left as it is:
// the generated gate tests write every flag through MigrateFeatures and flip one between
// their two states before they build a handler, and they must drive the App in the state
// they wrote. The setup is serialized per database, so parallel matrix cases over one
// database never see the flags half-written.
func putDeclaredFeaturesOn(t *testing.T, db *initiator.SpannerDB) {
	t.Helper()

	held, _ := featureStates.LoadOrStore(db, &featureState{})
	state, ok := held.(*featureState)
	if !ok {
		t.Fatalf("featureStates holds a %T, want *featureState", held)
	}
	state.once.Do(func() {
		state.err = enableDeclaredFeatures(t.Context(), db)
	})
	if state.err != nil {
		t.Fatalf("putting the declared feature flags on: %v", state.err)
	}
}

// enableDeclaredFeatures is putDeclaredFeaturesOn's one run per database.
func enableDeclaredFeatures(ctx context.Context, db *initiator.SpannerDB) error {
	client := resource.NewSpannerClient(db.Client)
	declared := resources.Features()
	flags, err := resource.LoadFeatures(ctx, client)
	if err != nil {
		return errors.Wrap(err, "resource.LoadFeatures()")
	}
	for _, d := range declared {
		if _, written := flags.Flag(d.Name); written {
			return nil
		}
	}
	if err := resource.MigrateFeatures(ctx, client, declared); err != nil {
		return errors.Wrap(err, "resource.MigrateFeatures()")
	}
	for _, d := range declared {
		if err := resource.SetFeatureEnabled(ctx, client, d.Name, true, resource.ProcessEvent("newTestHandler")); err != nil {
			return errors.Wrapf(err, "resource.SetFeatureEnabled(%s)", d.Name)
		}
	}

	return nil
}
