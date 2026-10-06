// window.go is the maintenance window in the run: whether the release needs it, the
// check at the start that refuses what can never proceed, the wait inside the run until
// it opens, the second look before maintenance goes on, the pull request's preview, and
// what the record keeps of it.

package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/semver"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// The facts the window leaves: whether the run waits for the window at all, whether the
// release is breaking (the maintenance steps key off this alone), why, and once the gate
// let the run through, when, how long it waited and which opening let it in.
const (
	windowNeededFact   = "WINDOW_NEEDED"
	windowBreakingFact = "WINDOW_BREAKING"
	windowReasonFact   = "WINDOW_REASON"
	windowOpenedFact   = "WINDOW_OPENED"
	windowWaitedFact   = "WINDOW_WAITED"
	windowSlotFact     = "WINDOW_SLOT"
	// placementFile is the placement in the checkout, beside the stack: the window
	// setting of the commit the release was tagged on.
	placementFile = "placement.json"
	// windowReserve is what the gate keeps of the build's time for the steps after the
	// wait, at their own timeouts: maintenance on, the hooks, the migrations, the
	// revision, traffic, maintenance off, the sweep and the record come to under three
	// hours.
	windowReserve = 3 * time.Hour
	// pipelineCeiling is Cloud Build's longest build, which the release pipeline's
	// whole-build timeout is; the budget when the build names no timeout.
	pipelineCeiling = 24 * time.Hour
	// waitChunk bounds one sleep of the wait, so a canceled build ends soon and the clock
	// is read again.
	waitChunk = 10 * time.Minute
	// slotInMaintenance names the opening of an environment that is in maintenance
	// already: the gate counts the window as open.
	slotInMaintenance = "in maintenance"
	// slotRollback is the slot a rollback run opens for itself: it goes now, whatever the
	// window, behind the maintenance page where the return is breaking.
	slotRollback = "rollback"
	// whenLayout spells a moment in the window's zone.
	whenLayout = "Monday 2006-01-02 15:04 MST"
)

// windowDecision is what the release check decided of the window for one deploy.
type windowDecision struct {
	// Breaking says the release turns away the release the environment runs: its oldest
	// answered release is newer than the live one, or it answers its own release alone.
	// A breaking release deploys behind the maintenance page.
	Breaking bool
	// Needed says the run waits for the environment's window: a breaking release, or
	// any release where the setting says all.
	Needed bool
	// Reason says why, in a sentence.
	Reason string
}

// decideWindow decides the deploy of tag into env: oldest is what the release file
// declares (nil when no outlet declares an oldest answered release), live the
// environment's newest live record (nil when it runs nothing live), setting the
// environment's window. An empty tag is a release not cut yet (a pull request's
// preview), which an outlet answering its own release alone turns every live release
// away from.
func decideWindow(oldest *derive.OldestAnswered, tag string, live *Record, setting *derive.MaintenanceWindow, env string) windowDecision {
	d := windowDecision{}
	switch {
	case oldest == nil:
		d.Reason = "no outlet declares an oldest answered release, so the release turns no running release away"
	case live == nil:
		d.Reason = env + " runs no release live, so there is nothing for the release to turn away"
	case oldest.This:
		d.Breaking = tag == "" || semver.Compare(tag, live.Version) > 0
		if d.Breaking {
			d.Reason = oldest.String() + ", and " + env + " runs " + live.Version + ", which it turns away"
		} else {
			d.Reason = oldest.String() + ", and " + env + " runs it already (" + live.Version + "), so a rerun turns nothing away"
		}
	default:
		d.Breaking = semver.Compare("v"+oldest.Release, live.Version) > 0
		if d.Breaking {
			d.Reason = oldest.String() + ", and " + env + " runs " + live.Version + ", which it turns away"
		} else {
			d.Reason = oldest.String() + ", which " + env + "'s " + live.Version + " is not older than"
		}
	}
	d.Needed = d.Breaking
	if !d.Breaking && setting.AllReleases() {
		d.Needed = true
		d.Reason += "; every release waits for " + env + "'s window (releases: all)"
	}

	return d
}

// windowFacts are the facts of a decision.
func (d windowDecision) facts() map[string]string {
	return map[string]string{windowNeededFact: flag(d.Needed), windowBreakingFact: flag(d.Breaking), windowReasonFact: d.Reason}
}

// checkoutPlacement reads the placement of the checkout: the stack's, beside the
// infrastructure files, at the commit the build runs. written says what the caller
// reads from it, for the error when it does not read ("the seed list is written").
func checkoutPlacement(w Workspace, written string) (*derive.Placement, error) {
	p, err := derive.ReadPlacement(filepath.Join(string(w), stackDir, placementFile))
	if err != nil {
		return nil, errors.Wrapf(err, "the checkout's placement (%s), where %s", filepath.Join(stackDir, placementFile), written)
	}

	return p, nil
}

// readOldest reads the release file from the checkout's router package and answers
// what the outlets declare at the oldest, nil when none does: no router directory
// (the pipeline names none), no file (the application predates it), or no outlet with
// the option. Each case is said on out; a file of the wrong shape is an error.
func readOldest(w Workspace, routerDir string, out io.Writer) (*derive.OldestAnswered, error) {
	if routerDir == "" {
		fmt.Fprintln(out, "No release file to read: the pipeline names no router directory (the application declares no GenerateRoutes), so no outlet declares an oldest answered release.")

		return nil, nil
	}
	f, err := derive.ReadReleaseFile(filepath.Join(string(w), filepath.FromSlash(routerDir)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(out, "Warning: no release file at %s in the checkout, so no outlet declares an oldest answered release and no release is breaking; the resource generator writes it beside the router.\n", filepath.ToSlash(filepath.Join(routerDir, derive.ReleaseFileName)))

			return nil, nil
		}

		return nil, err
	}
	oldest, ok := f.OldestAnswered()
	if !ok {
		fmt.Fprintf(out, "The release file %s declares no oldest answered release on any outlet: every release that sends its version is answered.\n", filepath.ToSlash(filepath.Join(routerDir, derive.ReleaseFileName)))

		return nil, nil
	}

	return &oldest, nil
}

// windowCheck is the release check's part on the maintenance window, the run's first
// look at it: it decides whether the release is breaking and whether the run waits for
// the window, leaves the facts, and refuses what can never proceed, so nothing is built
// for a run that would stop at the gate: production without a setting when the release
// is breaking, a window whose next opening is further away than the build can wait, and
// a window with no opening ahead. A restore run and an environment in maintenance from an
// earlier run pass: the gate counts the window as open.
func windowCheck(ctx context.Context, clients *Clients, w Workspace, build *Build, env map[string]string, routerDir string, out io.Writer) error {
	subs := build.Substitutions
	tag, environment := subs[tagSub], subs[envSub]
	placement, err := checkoutPlacement(w, "the maintenance windows are written")
	if err != nil {
		return err
	}
	oldest, err := readOldest(w, routerDir, out)
	if err != nil {
		return err
	}
	live, err := liveRelease(ctx, clients.Storage, subs)
	if err != nil {
		return err
	}
	setting, written := placement.MaintenanceSetting(environment)
	d := decideWindow(oldest, tag, live, &setting, environment)
	if err := w.Append(d.facts()); err != nil {
		return err
	}
	if !d.Needed {
		fmt.Fprintf(out, "No maintenance window: %s; %s deploys at any time.\n", d.Reason, tag)

		return nil
	}
	if env[rollbackFact] != "" {
		if d.Breaking {
			fmt.Fprintf(out, "The gate is open to a rollback: %s returns to %s now, behind the maintenance page since the return is breaking for %s (%s).\n", environment, tag, environment, d.Reason)
		} else {
			fmt.Fprintf(out, "The gate is open to a rollback: %s returns to %s now, the rolling way.\n", environment, tag)
		}

		return nil
	}
	if !written {
		return errors.Newf("%s%s has no maintenance setting in %s and %s is a breaking release (%s). Write \"maintenance\": {%q: %q} for a release at any time, or the client's windows, and release again.", rejected, environment, filepath.Join(stackDir, placementFile), tag, d.Reason, environment, derive.MaintenanceAnytime)
	}
	kind := "breaking release"
	if !d.Breaking {
		kind = "release"
	}
	fmt.Fprintf(out, "Maintenance window: %s is a %s for %s (%s); the window is %s.\n", tag, kind, environment, d.Reason, setting.String())
	if env[restoreFact] != "" {
		fmt.Fprintf(out, "The gate is open to a restore run: %s's database is replaced behind the maintenance page whatever the window says.\n", environment)

		return nil
	}
	now := clients.now()
	o, err := setting.Opening(now)
	if err != nil {
		return err
	}
	if o.Open {
		fmt.Fprintf(out, "%s's window is open now (%s): the run proceeds once the image is built and the jobs are made.\n", environment, openSlot(o))

		return nil
	}
	if service, on, err := serviceInMaintenance(ctx, clients, build); err != nil || on {
		if on {
			fmt.Fprintf(out, "%s is in maintenance from an earlier run (%s=%s on %s): the gate counts the window as open, so the rerun proceeds without waiting for the next one.\n", environment, derive.MaintenanceVariable, maintenanceOn, service)
		}

		return err
	}
	if err := withinBudget(environment, o, build, now); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s's window next opens %s (in %s, %s): the image is built and the jobs are made now, and the run waits for the window before maintenance begins.\n", environment, when(o.From), until(o.From, now), o.Slot)

	return nil
}

// withinBudget refuses an opening the build cannot wait for: none ahead, or one further
// away than the build's remaining time less the reserve for the steps after the wait.
func withinBudget(environment string, o derive.Opening, build *Build, now time.Time) error {
	if o.From.IsZero() {
		return errors.Newf("%s%s's maintenance window has no opening ahead (its dated slots have passed and it has no weekly slot): write the next slot in %s and release again.", rejected, environment, filepath.Join(stackDir, placementFile))
	}
	budget := waitBudget(build, now)
	if o.From.After(budget) {
		return errors.Newf("%s%s's maintenance window next opens %s (in %s, %s), further away than this run can wait (until %s, the build's timeout less %s for the steps after the window): start the release on the day of the window.", rejected, environment, when(o.From), until(o.From, now), o.Slot, when(budget.In(o.From.Location())), windowReserve)
	}

	return nil
}

// waitBudget is the moment until which the run may wait for the window: the build's
// start plus its timeout (Cloud Build counts the timeout from the start), less the
// reserve for the steps after the wait. A build that names neither counts from now with
// the pipeline's ceiling.
func waitBudget(build *Build, now time.Time) time.Time {
	start := now
	if t, err := time.Parse(time.RFC3339Nano, build.StartTime); err == nil {
		start = t
	}
	timeout := pipelineCeiling
	if d, err := time.ParseDuration(build.Timeout); err == nil && d > 0 {
		timeout = d
	}

	return start.Add(timeout - windowReserve)
}

// liveRelease is the environment's newest live record of a release, read as the deploy
// identity from the environment's records bucket; nil when it runs nothing live.
func liveRelease(ctx context.Context, open StoreFunc, subs map[string]string) (*Record, error) {
	store, err := open(ctx)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	return newestLiveRelease(ctx, store, subs[recordsBucket], subs[appSub], subs[envSub])
}

// WaitForWindow holds the run at the gate until the environment's maintenance window opens,
// when the release needs it (WINDOW_NEEDED): the image is built and the jobs are made
// by now, so the wait costs nothing and the window holds only maintenance, the
// migrations and the rollout. It prints when the run will proceed and waits, reading
// the clock again at most every ten minutes; when the window opens it leaves when, how
// long it waited and which opening let it in. A run in maintenance already (a restore
// run, or a rerun after a window release that failed) passes at once. An opening
// further away than the build can wait stops the run here, before anything changes.
func WaitForWindow(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if env[skipDeploy] == trueValue {
		fmt.Fprintln(out, skipped(env))

		return nil
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	if env[windowNeededFact] != trueValue {
		fmt.Fprintln(out, "No window to wait for: "+noWindow(env, build)+".")

		return nil
	}
	environment := build.Substitutions[envSub]
	if env[maintenanceFact] == trueValue {
		fmt.Fprintf(out, "The gate is open: %s is in maintenance already (this run put it there before its database was replaced).\n", environment)

		return w.Append(openedFacts(clients.now(), 0, slotInMaintenance))
	}
	if env[rollbackFact] != "" {
		fmt.Fprintf(out, "The gate is open: a rollback goes now, so %s returns to %s without waiting for a window.\n", environment, build.Substitutions[tagSub])

		return w.Append(openedFacts(clients.now(), 0, slotRollback))
	}
	if service, on, err := serviceInMaintenance(ctx, clients, build); err != nil || on {
		if on {
			fmt.Fprintf(out, "The gate is open: %s is in maintenance from an earlier run (%s=%s on %s), so the rerun proceeds without waiting for the next window.\n", environment, derive.MaintenanceVariable, maintenanceOn, service)

			return w.Append(openedFacts(clients.now(), 0, slotInMaintenance))
		}

		return err
	}
	setting, err := writtenSetting(w, environment)
	if err != nil {
		return err
	}
	arrived := clients.now()
	announced := false
	for {
		now := clients.now()
		o, err := setting.Opening(now)
		if err != nil {
			return err
		}
		if o.Open {
			waited := now.Sub(arrived).Round(time.Second)
			fmt.Fprintf(out, "%s's maintenance window is open (%s); the run waited %s and proceeds into maintenance, the migrations and the rollout.\n", environment, openSlot(o), waited)

			return w.Append(openedFacts(now, waited, o.Slot))
		}
		if err := withinBudget(environment, o, build, now); err != nil {
			return err
		}
		if !announced {
			fmt.Fprintf(out, "Waiting for %s's maintenance window: it opens %s (in %s, %s); the run proceeds then.\n", environment, when(o.From), until(o.From, now), o.Slot)
			announced = true
		}
		if err := clients.sleep()(ctx, min(o.From.Sub(now), waitChunk)); err != nil {
			return err
		}
	}
}

// now is the clients' clock, the real one when none is set; the window's tests inject
// one that the sleep advances.
func (c *Clients) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}

	return time.Now()
}

// openedFacts are the facts the gate leaves when it lets the run through.
func openedFacts(now time.Time, waited time.Duration, slot string) map[string]string {
	facts := map[string]string{windowOpenedFact: now.UTC().Format(time.RFC3339), windowSlotFact: slot, windowWaitedFact: ""}
	if waited > 0 {
		facts[windowWaitedFact] = waited.String()
	}

	return facts
}

// noWindow says why a run waits for no window: the reason the release check left, or
// the kind of build.
func noWindow(env map[string]string, build *Build) string {
	switch {
	case build.Substitutions[prNumberSub] != "":
		return "a pull-request build never waits for one"
	case env[windowReasonFact] != "":
		return env[windowReasonFact]
	default:
		return "the release check left no decision, so the release deploys at any time"
	}
}

// writtenSetting is the environment's window as the checkout's placement writes it; the
// release check refused a run that needs the window without one.
func writtenSetting(w Workspace, environment string) (*derive.MaintenanceWindow, error) {
	placement, err := checkoutPlacement(w, "the maintenance windows are written")
	if err != nil {
		return nil, err
	}
	setting, ok := placement.MaintenanceSetting(environment)
	if !ok {
		return nil, errors.Newf("%s has no maintenance setting in %s, which the release check refuses at the start of a run that needs the window", environment, filepath.Join(stackDir, placementFile))
	}

	return &setting, nil
}

// windowStillOpen is the second look, just before maintenance goes on: the window must
// be open now, or the environment in maintenance already, else the run stops with
// nothing changed, naming the next opening. It answers the opening that lets the run in.
func windowStillOpen(ctx context.Context, clients *Clients, w Workspace, build *Build, env map[string]string) (string, error) {
	environment := build.Substitutions[envSub]
	if env[windowSlotFact] == slotInMaintenance {
		return slotInMaintenance, nil
	}
	if _, on, err := serviceInMaintenance(ctx, clients, build); err != nil || on {
		return slotInMaintenance, err
	}
	setting, err := writtenSetting(w, environment)
	if err != nil {
		return "", err
	}
	o, err := setting.Opening(clients.now())
	if err != nil {
		return "", err
	}
	if !o.Open {
		next := "no opening lies ahead"
		if !o.From.IsZero() {
			next = "it next opens " + when(o.From)
		}

		return "", errors.Newf("%s%s's maintenance window closed before maintenance went on (%s); nothing changed, and the release deploys inside the next window: run it again then.", rejected, environment, next)
	}

	return o.Slot, nil
}

// serviceInMaintenance reports whether the environment's first service carries the
// maintenance variable set live, from an earlier run that put it into maintenance and
// did not finish, and names the service. A build that names no service, or a service
// not deployed yet, is not in maintenance.
func serviceInMaintenance(ctx context.Context, clients *Clients, build *Build) (service string, on bool, err error) {
	entries := build.Substitutions[servicesSub]
	if entries == "" || clients.Run == nil {
		return "", false, nil
	}
	region, service, err := target(servicesSub, strings.Split(entries, ",")[0])
	if err != nil {
		return "", false, err
	}
	run, err := clients.Run(ctx)
	if err != nil {
		return "", false, err
	}
	doc, err := run.Get(ctx, serviceName(build.Substitutions[projectSub], region, service))
	if err != nil {
		if isNotFound(err) {
			return service, false, nil
		}

		return "", false, errors.Wrapf(err, "reading the service %s for its maintenance variable", service)
	}
	template, _ := doc[keyTemplate].(map[string]any)
	containers, _ := template["containers"].([]any)
	if len(containers) == 0 {
		return service, false, nil
	}
	container, _ := containers[0].(map[string]any)
	vars, _ := container["env"].([]any)
	for _, entry := range vars {
		if v, _ := entry.(map[string]any); text(v, keyName) == derive.MaintenanceVariable && text(v, keyValue) == maintenanceOn {
			return service, true, nil
		}
	}

	return service, false, nil
}

// windowPreview is a pull-request build's look ahead at the maintenance window: what
// the release this pull request will be part of turns away in each environment, read
// from the checkout's release file and each environment's live deployment record as
// that environment's plan identity, and which environments will hold it for their
// window. It warns and never refuses.
func windowPreview(ctx context.Context, open StoreAsFunc, w Workspace, subs map[string]string, routerDir string, out io.Writer) error {
	fmt.Fprintln(out, "Window preview: whether the release this pull request becomes part of turns away the release each environment runs, and which environments hold it for their maintenance window:")
	oldest, err := readOldest(w, routerDir, out)
	if err != nil {
		return err
	}
	placement, err := checkoutPlacement(w, "the maintenance windows are written")
	if err != nil {
		return err
	}
	buckets, identities := pairs(subs[recordsBucketsSub]), pairs(subs[planIdentitiesSub])
	for _, env := range strings.Split(subs[environmentsSub], ",") {
		if env == "" {
			continue
		}
		bucket, identity := buckets[env], identities[env]
		if bucket == "" || identity == "" {
			fmt.Fprintf(out, "  %s: its records bucket or plan identity is not named (%s, %s); nothing read.\n", env, recordsBucketsSub, planIdentitiesSub)

			continue
		}
		live, err := previewLive(ctx, open, subs[appSub], env, bucket, identity)
		if err != nil {
			fmt.Fprintf(out, "  %s: its records could not be read as %s: %v\n", env, identity, err)

			continue
		}
		fmt.Fprintf(out, "  %s\n", previewLine(oldest, live, placement, env))
	}

	return nil
}

// previewLive is the environment's newest live record read as its plan identity.
func previewLive(ctx context.Context, open StoreAsFunc, app, env, bucket, identity string) (*Record, error) {
	store, err := open(ctx, identity)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	return newestLiveRelease(ctx, store, bucket, app, env)
}

// previewLine is one environment's answer in the window preview.
func previewLine(oldest *derive.OldestAnswered, live *Record, placement *derive.Placement, env string) string {
	setting, written := placement.MaintenanceSetting(env)
	d := decideWindow(oldest, "", live, &setting, env)
	kind := "not breaking"
	if d.Breaking {
		kind = "breaking"
	}
	switch {
	case !d.Needed:
		return fmt.Sprintf("%s: %s (%s); the release deploys at any time.", env, kind, d.Reason)
	case !written:
		return fmt.Sprintf("%s: %s (%s); %s has no maintenance setting, so the release WILL BE REFUSED at the start of its run there until placement.json names one (\"anytime\" is a setting).", env, kind, d.Reason, env)
	case setting.Anytime && d.Breaking:
		return fmt.Sprintf("%s: %s (%s); %s takes it at any time, behind the maintenance page.", env, kind, d.Reason, env)
	case setting.Anytime:
		return fmt.Sprintf("%s: %s (%s); %s takes it at any time.", env, kind, d.Reason, env)
	case d.Breaking:
		return fmt.Sprintf("%s: %s (%s); the run waits for %s's window (%s) and deploys behind the maintenance page.", env, kind, d.Reason, env, setting.String())
	default:
		return fmt.Sprintf("%s: %s (%s); the run waits for %s's window (%s) and deploys the rolling way.", env, kind, d.Reason, env, setting.String())
	}
}

// when spells a moment in its own zone.
func when(t time.Time) string {
	return t.Format(whenLayout)
}

// openSlot names an open slot with its closing time; anytime never closes.
func openSlot(o derive.Opening) string {
	if o.To.IsZero() {
		return o.Slot
	}

	return o.Slot + ", until " + when(o.To)
}

// until spells how far ahead t is from now, to the minute.
func until(t, now time.Time) string {
	d := t.Sub(now).Round(time.Minute)
	if d < time.Minute {
		return "under a minute"
	}
	hours := int(d / time.Hour)
	minutes := int(d%time.Hour) / int(time.Minute)
	switch {
	case hours == 0:
		return fmt.Sprintf("%dm", minutes)
	case minutes == 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dh%02dm", hours, minutes)
	}
}

// Window is a run's maintenance window as the record keeps it: present when the
// release needed the window.
type Window struct {
	// Breaking says the release turned away the release the environment ran, and so
	// deployed behind the maintenance page.
	Breaking bool `json:"breaking"`
	// Reason says why the run needed the window.
	Reason string `json:"reason"`
	// Slot is the opening that let the run in: a weekly slot with its zone, a dated
	// slot, anytime, or in maintenance for an environment in maintenance already.
	Slot string `json:"slot,omitempty"`
	// Opened is when the gate let the run through, RFC3339.
	Opened string `json:"opened,omitempty"`
	// Waited is how long the run waited at the gate; empty when the window was open on
	// arrival.
	Waited string `json:"waited,omitempty"`
}

// windowOf reads the window a run went through from its facts; nil for a run that
// needed none.
func windowOf(env map[string]string) *Window {
	if env[windowNeededFact] != trueValue {
		return nil
	}

	return &Window{Breaking: env[windowBreakingFact] == trueValue, Reason: env[windowReasonFact], Slot: env[windowSlotFact], Opened: env[windowOpenedFact], Waited: env[windowWaitedFact]}
}
