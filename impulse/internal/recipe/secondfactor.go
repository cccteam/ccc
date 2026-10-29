package recipe

import (
	"context"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/transition"
)

// SecondFactor ends every auth package's sign-in rule with the second factor where it
// does not: the 0.5.26 step had each auth's New set requireSecondFactor, which impulse
// wrote into new auth packages and the step's walk never into an existing one, so an
// application upgraded through it kept the sign-in codes and trusted devices tables and
// the pending sign-in's code handlers and asked nobody for the code. The rule is
// written as the auth template writes it: .Then(requireSecondFactor) chained on the one
// SetSignInPolicy call's policy, or the call set on the construction after its error
// check when the package makes none, and the function at the end of the file with its
// imports when the package has none returning signin.RequireMFA. A package whose rule
// ends with the second factor is left alone, so the recipe is safe to run twice; the
// second-factor check holds the rule from here on.
type SecondFactor struct{}

// Name is the recipe's short name.
func (SecondFactor) Name() string { return "second-factor" }

// Meaning says what the change means in this framework.
func (SecondFactor) Meaning() string { return check.SecondFactorMeaning }

// Detect names each auth package whose sign-in rule does not end with the second factor.
func (SecondFactor) Detect(_ context.Context, a *app.App) ([]string, error) {
	rules, err := check.AuthRules(a)
	if err != nil {
		return nil, err
	}
	var found []string
	for i := range rules {
		found = append(found, rules[i].Findings()...)
	}

	return found, nil
}

// Apply writes the rule into every auth package the detector names, one edit per
// package, and leaves to the agent the package it cannot edit, with the reason.
func (SecondFactor) Apply(_ context.Context, a *app.App, _ check.Execer) (*transition.Change, error) {
	rules, err := check.AuthRules(a)
	if err != nil {
		return nil, err
	}
	ch := &transition.Change{}
	for i := range rules {
		r := &rules[i]
		if r.Auth == nil || r.Ends() {
			continue
		}
		did, skipped, err := check.AddSecondFactorRule(a, r)
		if err != nil {
			return nil, err
		}
		ch.Did = append(ch.Did, did...)
		if skipped != "" {
			ch.Skipped = append(ch.Skipped, skipped)
		}
	}

	return ch, nil
}
