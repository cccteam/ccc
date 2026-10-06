package check

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cccteam/ccc/impulse/app"
)

// changeSignal verifies that every permission engine the application constructs is
// handed a change signal: each access.New call outside tests carries an
// access.WithChangeSignal option, the signal the engine announces its policy writes on
// and follows the other instances' from, over the application's live service, so a role,
// grant or membership written on one instance reaches every instance's snapshot at once
// rather than at the engine's next heartbeat. The compiler holds the signal's shape;
// this holds that no engine is constructed without one.
type changeSignal struct{}

// changeSignalName is the check's name.
const changeSignalName = "change-signal"

func (changeSignal) Name() string { return changeSignalName }

func (changeSignal) Describe() string {
	return "every permission engine constructed outside tests (access.New) is handed a change signal (access.WithChangeSignal) over the application's live service, so a policy write on one instance reaches every instance's snapshot at once"
}

// Meaning explains the obligation for the handoff brief.
func (changeSignal) Meaning() string {
	return "A permission engine keeps a snapshot of its policy and rereads it at a heartbeat; the change signal is how the instance that wrote a role, a grant or a membership tells the others to reread now. The application's live service carries it: one signals document per application with a field per kind of change, and the engine's kind is `resource.KindPolicy`. Every `access.New` passes `access.WithChangeSignal(access.ChangeSignalFunc(announce, watch))`: announce signals the kind (`signals.Signal(ctx, resource.KindPolicy)`), watch subscribes to it (`signals.Subscribe(resource.KindPolicy, onChange)`), defers the stop it is handed and waits on `ctx.Done()`. The skeletons' auth packages take the live service as `Settings.Signals` and build both halves from it (`announcePolicy`, `watchPolicy`), the data level passes the service it opened, and the test harnesses pass `live.NewFake()`. Each line under the check names a package constructing an engine without the option, with the constructions by file and line."
}

func (c changeSignal) Run(_ context.Context, env *Env) Result {
	a := env.App
	if len(a.Engines) == 0 {
		return skip(c.Name(), "no permission engine is constructed (no access.New outside tests)")
	}
	byPackage := map[string][]app.Engine{}
	for _, e := range a.Engines {
		byPackage[e.Package] = append(byPackage[e.Package], e)
	}
	packages := make([]string, 0, len(byPackage))
	for p := range byPackage {
		packages = append(packages, p)
	}
	sort.Strings(packages)
	var details []string
	failing := 0
	for _, p := range packages {
		found := c.packageFindings(p, byPackage[p])
		if len(found) > 0 {
			failing++
		}
		details = append(details, found...)
	}
	if failing > 0 {
		return fail(c.Name(), fmt.Sprintf("%d package(s) construct a permission engine without a change signal", failing), details...)
	}

	return pass(c.Name(), fmt.Sprintf("%d permission engine(s) constructed, each handed a change signal: %s", len(a.Engines), strings.Join(packages, ", ")))
}

// packageFindings are the findings for one package: its constructions handed no change
// signal, and its constructions forwarding options the check cannot read; none when every
// construction carries the option.
func (changeSignal) packageFindings(pkg string, engines []app.Engine) []string {
	var unsignaled, forwarded []string
	for _, e := range engines {
		switch {
		case e.ChangeSignal:
		case e.OptionsForwarded:
			forwarded = append(forwarded, fmt.Sprintf("%s:%d", e.File, e.Line))
		default:
			unsignaled = append(unsignaled, fmt.Sprintf("%s:%d", e.File, e.Line))
		}
	}
	var findings []string
	if len(unsignaled) > 0 {
		findings = append(findings, fmt.Sprintf("%s: access.New at %s is handed no change signal; pass access.WithChangeSignal(access.ChangeSignalFunc(announce, watch)) over the application's live service (the policy kind), so a policy write on one instance reaches every instance at once", pkg, strings.Join(unsignaled, ", ")))
	}
	if len(forwarded) > 0 {
		findings = append(findings, fmt.Sprintf("%s: access.New at %s forwards its options (opts...), so whether a change signal is among them cannot be read here; pass access.WithChangeSignal in this call", pkg, strings.Join(forwarded, ", ")))
	}

	return findings
}
