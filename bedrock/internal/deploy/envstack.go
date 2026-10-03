// envstack.go plans, tests and applies the environment's application stack: in a tag
// build, as the apply identity, after the image build and before the migrations, so an
// infrastructure change rides the release that carries it under the release's own gate; in
// a pull-request build, a plan for every environment as that environment's plan identity,
// so the plan a reviewer approves is each environment's.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/check"
	"github.com/cccteam/ccc/bedrock/internal/derive"
)

const (
	// environmentsSub is the promotion order, comma-separated; planIdentitiesSub names
	// each environment's plan identity (env=email, comma-separated), the reader a
	// pull-request build plans that environment as.
	environmentsSub   = "_ENVIRONMENTS"
	planIdentitiesSub = "_PLAN_IDENTITIES"
	// fileStoresSub lists the file stores' buckets as the stack addresses them,
	// comma-separated, rendered from the same declarations as the buckets; absent when
	// the stack has no store.
	fileStoresSub = "_FILE_STORES"
	// StackPlanFile is the tag build's saved plan of the environment's stack, in the
	// workspace, which the apply applies.
	StackPlanFile = "stack.plan"
	// StackPlanJSONFile is that plan's JSON, beside it, whose summary the record carries.
	StackPlanJSONFile = "stack-plan.json"
	// stackPlanFact is the plan's summary line, appended for the steps after.
	stackPlanFact = "STACK_PLAN"
	// enabledState is a secret version's state when a revision can mount it.
	enabledState = "ENABLED"
	// pullRequestBuildNotice is a tag-build step's answer in a pull-request build.
	pullRequestBuildNotice = "Pull-request build: the stack is planned for every environment before the pull request's own, and applied nowhere."
)

// StackPlan is what a plan of the environment's stack would do: the counts tofu prints and
// each change. The deployment record carries the tag build's.
type StackPlan struct {
	Add     int           `json:"add"`
	Change  int           `json:"change"`
	Destroy int           `json:"destroy"`
	Changes []StackChange `json:"changes,omitempty"`
}

// StackChange is one planned change: the resource's address and tofu's actions.
type StackChange struct {
	Address string   `json:"address"`
	Actions []string `json:"actions"`
}

// Summary is the plan's one line, as tofu prints it.
func (p *StackPlan) Summary() string {
	return fmt.Sprintf("Plan: %d to add, %d to change, %d to destroy.", p.Add, p.Change, p.Destroy)
}

// print says what the plan does: the summary, then one change per line.
func (p *StackPlan) print(out io.Writer) {
	fmt.Fprintln(out, p.Summary())
	for _, c := range p.Changes {
		fmt.Fprintf(out, "  %s %s\n", strings.Join(c.Actions, "+"), c.Address)
	}
}

// stackPlan reads a plan's JSON (tofu show -json) into what it would do, no-ops and reads
// left out; a replace counts one to add and one to destroy, as tofu counts it.
func stackPlan(data []byte) (*StackPlan, error) {
	var plan planDocument
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): the plan's JSON")
	}
	p := &StackPlan{}
	for _, c := range plan.ResourceChanges {
		actions := slices.DeleteFunc(slices.Clone(c.Change.Actions), func(a string) bool {
			return a == "no-op" || a == "read"
		})
		if len(actions) == 0 {
			continue
		}
		for _, a := range actions {
			switch a {
			case actionCreate:
				p.Add++
			case actionUpdate:
				p.Change++
			case actionDelete:
				p.Destroy++
			}
		}
		p.Changes = append(p.Changes, StackChange{Address: c.Address, Actions: actions})
	}

	return p, nil
}

// PlanEnvironmentStack plans the environment's stack in a tag build, as the apply identity,
// after the image build and before the migrations: tofu init on the environment's state
// prefix, tofu plan saved to the workspace with its JSON beside it, the summary printed and
// appended (STACK_PLAN), then the tests. A failing plan or test stops the build with the
// stack unapplied. The plan is this build's own: the plan a reviewer approved on the pull
// request was bound to the state of that moment, and a release bundles several pull
// requests. A pull-request build plans every environment earlier instead.
func PlanEnvironmentStack(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	subs, ok, err := tagBuildStep(w, out)
	if err != nil || !ok {
		return err
	}
	identity := subs[applyIdentitySub]
	if identity == "" {
		return errors.Newf("%s names no apply identity (%s): the stack's triggers carry it", BuildFile, applyIdentitySub)
	}
	s := newEnvironmentStack(clients, w, identity, out)
	if err := s.initEnvironment(ctx, subs[appSub], subs[envSub], identity); err != nil {
		return err
	}
	plan := filepath.Join(string(w), StackPlanFile)
	args := []string{"plan", "-input=false", "-no-color", "-out=" + plan, varFlag, "environment=" + subs[envSub]}
	env, err := w.Environment()
	if err != nil {
		return err
	}
	// The plan is told the maintenance variable's live value, so that declared and live
	// agree and the apply never starts an application revision from the template while
	// the application is in maintenance: in this run (a restore run, after deploy
	// maintenance on), or from an earlier run that failed after its maintenance on, whose
	// database may hold no tables until the migrations run (a revision started then
	// cannot come up, and the apply waits on it). An entry of the env set cannot be
	// ignored on its own; the release's own revision is deployed with the variable
	// cleared, after the migrations.
	build, err := w.Build()
	if err != nil {
		return err
	}
	live, err := liveMaintenance(ctx, clients, build, env, out)
	if err != nil {
		return err
	}
	if live != "" {
		args = append(args, varFlag, "maintenance="+live)
	}
	switch env[restoreFact] {
	case "":
	case restoreBackup:
		if err := s.restoreFromBackup(ctx, subs, env, w); err != nil {
			return err
		}
	default:
		replace, err := s.replaceForRestore(ctx, subs, env, subs[seedSub] == trueValue, w)
		if err != nil {
			return err
		}
		args = append(args, replace...)
	}
	if err := s.tofu(ctx, args...); err != nil {
		return err
	}
	shown, err := s.tofuOutput(ctx, "show", "-json", plan)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(string(w), StackPlanJSONFile), shown, 0o600); err != nil {
		return errors.Wrapf(err, "os.WriteFile(): %s", StackPlanJSONFile)
	}
	p, err := stackPlan(shown)
	if err != nil {
		return err
	}
	p.print(out)
	if err := testStack(ctx, clients, s.dir, identity, subs, shown, out); err != nil {
		return err
	}

	return w.Append(map[string]string{stackPlanFact: p.Summary()})
}

// liveMaintenance is the value the plan declares for the maintenance variable: 1 in a
// run that is in maintenance, else what the first service the stack names carries live
// (1 when an earlier run's maintenance was never ended), else "", the default. A service
// the stack does not name or that is not deployed yet answers "".
func liveMaintenance(ctx context.Context, clients *Clients, build *Build, env map[string]string, out io.Writer) (string, error) {
	if env[maintenanceFact] == trueValue {
		return maintenanceOn, nil
	}
	service, on, err := serviceInMaintenance(ctx, clients, build)
	if err != nil || !on {
		return "", err
	}
	fmt.Fprintf(out, "%s is in maintenance from an earlier run (%s=%s on the service): the plan keeps it so, and the release's revision clears it after the migrations.\n", service, derive.MaintenanceVariable, maintenanceOn)

	return maintenanceOn, nil
}

// restoredFact lists what a restore run's plan replaces, comma-separated, for the record.
const (
	restoredFact = "RESTORE_REPLACED"
	// clearedFact lists what the restore run emptied instead of replacing: the Firestore
	// database whose documents the apply step deleted.
	clearedFact = "RESTORE_CLEARED"
	// firestoreOutput is the stack's output naming the Firestore database, when it has one;
	// firestoreAddress is the database's address in the stack.
	firestoreOutput  = "firestore_database"
	firestoreAddress = "google_firestore_database.firestore"
	// backupFact and backupTimeFact are the backup a production-backup restore restored
	// from and the moment its data is from, for the record.
	backupFact     = "RESTORE_BACKUP"
	backupTimeFact = "RESTORE_BACKUP_TIME"
)

// stateAttribute reads one attribute of a resource as tofu state show prints it
// (name = "value").
var stateAttribute = regexp.MustCompile(`(?m)^\s*(project|instance|name)\s*=\s*"([^"]*)"`)

// restoreFromBackup is a production-backup restore: before the plan, the environment's
// database is dropped and restored, under its own name, from the most recent backup of
// production's database on the instance the two share, as the apply identity (which holds
// database admin on that instance). The database's address keeps its state entry, so the
// plan then finds the restored database and recreates the memberships the drop took with
// it; the migrations then apply whatever production's backup predates. The backup and the
// moment its data is from are appended for the record (RESTORE_BACKUP, RESTORE_BACKUP_TIME).
func (s *stack) restoreFromBackup(ctx context.Context, subs, facts map[string]string, w Workspace) error {
	app, env, requester := subs[appSub], subs[envSub], facts[requesterFact]
	address := "google_spanner_database." + app + "[0]"
	shown, err := s.tofuOutput(ctx, "state", "show", address)
	if err != nil {
		return errors.Newf("%s=%s: %s's stack holds no database to restore (%s is not in its state)", restoreSub, restoreBackup, env, address)
	}
	attributes := map[string]string{}
	for _, m := range stateAttribute.FindAllStringSubmatch(string(shown), -1) {
		attributes[m[1]] = m[2]
	}
	if attributes["project"] == "" || attributes["instance"] == "" || attributes["name"] == "" {
		return errors.Newf("%s=%s: the database %s could not be read from the state (project, instance, name)", restoreSub, restoreBackup, address)
	}
	environments := strings.Split(subs[environmentsSub], ",")
	production := environments[len(environments)-1]
	productionDB := strings.Replace(attributes["name"], "-"+env+"-", "-"+production+"-", 1)
	if productionDB == attributes["name"] || production == "" || production == env {
		return errors.Newf("%s=%s: production's database cannot be named from %s's (%s): the environments are %s", restoreSub, restoreBackup, env, attributes["name"], subs[environmentsSub])
	}
	instance := "projects/" + attributes["project"] + "/instances/" + attributes["instance"]
	database := instance + "/databases/" + attributes["name"]
	store, err := s.clients.SpannerAs(ctx, s.identity)
	if err != nil {
		return err
	}
	backup, err := store.LatestBackup(ctx, instance, instance+"/databases/"+productionDB)
	if err != nil {
		return errors.Wrapf(err, "listing the backups of %s", productionDB)
	}
	if backup == nil {
		return errors.Newf("%s=%s: %s has no READY backup of production's database %s; %s keeps its database", restoreSub, restoreBackup, attributes["instance"], productionDB, env)
	}
	fmt.Fprintf(s.out, "=== Restore (%s, asked for by %s): %s's database %s is dropped and restored from production's backup %s (data as of %s); the migrations production's backup predates then apply ===\n", restoreBackup, requester, env, attributes["name"], path.Base(backup.Name), backup.VersionTime)
	if err := store.DropDatabase(ctx, database); err != nil {
		return errors.Wrapf(err, "dropping %s", database)
	}
	fmt.Fprintf(s.out, "Dropped %s.\n", attributes["name"])
	if err := store.RestoreDatabase(ctx, instance, attributes["name"], backup.Name); err != nil {
		return errors.Wrapf(err, "restoring %s from %s", attributes["name"], backup.Name)
	}
	fmt.Fprintf(s.out, "Restored %s from %s; the plan recreates its memberships.\n", attributes["name"], path.Base(backup.Name))

	return w.Append(map[string]string{restoredFact: address, backupFact: backup.Name, backupTimeFact: backup.VersionTime})
}

// replaceForRestore is a restore run's -replace of what the environment's database
// holds: the Spanner database, which the migrations (and the seed, in an environment the
// placement's seed list names) then fill afresh, and, in tst, the file stores' buckets
// (every one the stack's _FILE_STORES names), whose objects refer to rows that are gone
// (stg's are kept: production's backup predates some of them, and the record says so).
// The Firestore database, whose documents refer to those rows too, is not replaced:
// Firestore keeps a deleted database's id unavailable for minutes, so the apply step
// deletes its documents instead (clearFirestore). Each when the stack has it; what is
// replaced is noted for the record. The restore from production's backup is a different
// path (restoreFromBackup): the database is dropped and restored before the plan, not
// replaced.
func (s *stack) replaceForRestore(ctx context.Context, subs, facts map[string]string, seeded bool, w Workspace) ([]string, error) {
	app, env, kind, requester := subs[appSub], subs[envSub], facts[restoreFact], facts[requesterFact]
	listed, err := s.tofuOutput(ctx, "state", "list")
	if err != nil {
		return nil, err
	}
	addresses := []string{"google_spanner_database." + app + "[0]"}
	if env == tstEnvironment {
		addresses = append(addresses, fileStoreAddresses(subs)...)
	}
	var replace, replaced []string
	for _, address := range addresses {
		if hasLine(string(listed), address) {
			replace = append(replace, "-replace="+address)
			replaced = append(replaced, address)
		}
	}
	if len(replace) == 0 {
		return nil, errors.Newf("%s=%s: %s's stack holds no database to replace (nothing of %s in its state)", restoreSub, kind, env, strings.Join(addresses, ", "))
	}
	refill := "the migrations then apply afresh"
	if seeded {
		refill = "the migrations and the seed then apply afresh"
	}
	fmt.Fprintf(s.out, "=== Restore (%s, asked for by %s): %s is replaced in %s's stack; %s ===\n", kind, requester, strings.Join(replaced, ", "), env, refill)
	if hasLine(string(listed), firestoreAddress) {
		fmt.Fprintf(s.out, "The Firestore database stays (Firestore keeps a deleted database's id unavailable for minutes); the apply deletes its documents instead.\n")
	}
	if err := w.Append(map[string]string{restoredFact: strings.Join(replaced, ",")}); err != nil {
		return nil, err
	}

	return replace, nil
}

// fileStoreAddresses are the file stores' buckets as the stack addresses them, from the
// substitution the stack renders beside the buckets (_FILE_STORES); none when the stack
// has no store.
func fileStoreAddresses(subs map[string]string) []string {
	var addresses []string
	for _, address := range strings.Split(subs[fileStoresSub], ",") {
		if address = strings.TrimSpace(address); address != "" {
			addresses = append(addresses, address)
		}
	}

	return addresses
}

// ApplyEnvironmentStack applies the plan PlanEnvironmentStack saved, in a tag build, as the
// apply identity, and says what it did; a plan with no change applies nothing.
func ApplyEnvironmentStack(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	subs, ok, err := tagBuildStep(w, out)
	if err != nil || !ok {
		return err
	}
	data, err := os.ReadFile(filepath.Join(string(w), StackPlanJSONFile))
	if err != nil {
		return errors.Wrapf(err, "os.ReadFile(): %s (deploy stack plan writes it)", StackPlanJSONFile)
	}
	p, err := stackPlan(data)
	if err != nil {
		return err
	}
	s := newEnvironmentStack(clients, w, subs[applyIdentitySub], out)
	if len(p.Changes) == 0 {
		fmt.Fprintf(out, "Nothing to apply: %s's stack matches the code.\n", subs[envSub])
	} else {
		if err := s.tofu(ctx, "apply", "-input=false", "-no-color", filepath.Join(string(w), StackPlanFile)); err != nil {
			return err
		}
		fmt.Fprintf(out, "Applied %s's stack: %d added, %d changed, %d destroyed.\n", subs[envSub], p.Add, p.Change, p.Destroy)
	}
	if err := s.migrateSettings(ctx, w); err != nil {
		return err
	}
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if env[restoreFact] == "" {
		return nil
	}

	return s.clearFirestore(ctx, subs, w)
}

// migrateSettings reads the variables the migrate command runs with off the stack's
// substitutions output as the stack now stands (_MIGRATE_ENV: the levels the command
// constructs, for this environment), and leaves them for the migrate step (MIGRATE_ENV).
// The trigger's copy is the last apply's: a release that declares a new variable of those
// levels changes them in this very apply, and the first release after the stack began to
// carry them finds them nowhere else.
func (s *stack) migrateSettings(ctx context.Context, w Workspace) error {
	facts, err := s.facts(ctx)
	if err != nil {
		return err
	}
	settings := facts[migrateEnvFact]
	if settings == "" {
		return errors.Newf("the stack's substitutions output names no %s, the migrate command's settings: the stack is rendered by an older bedrock than the pipeline's, which bedrock check refuses", migrateEnvSub)
	}
	fmt.Fprintf(s.out, "The migrate command's settings are read from the stack as applied (%s).\n", migrateEnvSub)

	return w.Append(map[string]string{migrateEnvFact: settings})
}

// clearFirestore deletes every document of the environment's Firestore database in a
// restore run, as the apply identity, which owns the environment's data stores: the
// documents refer to rows the restore replaced. The database itself stays, since Firestore
// keeps a deleted database's id unavailable for minutes. A stack without a Firestore
// database (no firestore_database output) has nothing to clear. What was cleared is
// appended (RESTORE_CLEARED) for the record.
func (s *stack) clearFirestore(ctx context.Context, subs map[string]string, w Workspace) error {
	name, err := s.tofuOutput(ctx, "output", "-raw", firestoreOutput)
	if err != nil {
		fmt.Fprintf(s.out, "No Firestore database to clear: the stack has no %s output (%v).\n", firestoreOutput, errors.Cause(err))

		return nil
	}
	database := "projects/" + subs[projectSub] + "/databases/" + strings.TrimSpace(string(name))
	store, err := s.clients.FirestoreAs(ctx, s.identity)
	if err != nil {
		return err
	}
	if err := store.DeleteAllDocuments(ctx, database); err != nil {
		return errors.Wrapf(err, "deleting the documents of %s", database)
	}
	fmt.Fprintf(s.out, "Restore: every document of the Firestore database %s is deleted; its documents referred to rows the restore replaced.\n", strings.TrimSpace(string(name)))

	return w.Append(map[string]string{clearedFact: firestoreAddress})
}

// PlanEnvironments plans the stack for every environment in a pull-request build, each as
// that environment's plan identity, a reader, without the state lock: the plan the
// reviewer approves is each environment's, and a pull-request build in tst can never
// change another environment. Each plan runs the tests; one comment on the pull request
// carries every summary; a failing plan or test stops the build, which is the required
// check, and is said on the pull request. A tag build plans its own environment later
// instead (deploy stack plan). Triggers that carry no promotion order (a stack not yet
// applied with a bedrock that writes it, which the first release on it does) plan
// nothing, with a notice: the pull request that moves an application here builds.
func PlanEnvironments(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, build, ok, err := pullRequestStep(w, out)
	if err != nil || !ok {
		return err
	}
	subs := build.Substitutions
	if env[downFact] == trueValue {
		fmt.Fprintln(out, "/gcbrun down: nothing to plan for the environments.")

		return nil
	}
	if subs[environmentsSub] == "" {
		fmt.Fprintf(out, "The triggers carry no %s: the stack has not been applied with this bedrock yet, which the first release on it does; the environments' plans wait for it.\n", environmentsSub)

		return nil
	}
	identities, err := planIdentities(subs)
	if err != nil {
		return err
	}
	var summaries []string
	for _, e := range strings.Split(subs[environmentsSub], ",") {
		identity := identities[e]
		s := newEnvironmentStack(clients, w, identity, out)
		if err := s.initEnvironment(ctx, subs[appSub], e, identity); err != nil {
			return err
		}
		plan := filepath.Join(string(w), "environment-"+e+".plan")
		if err := s.tofu(ctx, "plan", "-input=false", "-no-color", "-lock=false", "-out="+plan, varFlag, "environment="+e); err != nil {
			return refuse(ctx, clients, build, env, fmt.Sprintf("The plan of %s's stack failed; the build log says why (build %s).", e, build.ID), err, out)
		}
		shown, err := s.tofuOutput(ctx, "show", "-json", plan)
		if err != nil {
			return err
		}
		p, err := stackPlan(shown)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s: ", e)
		p.print(out)
		if err := testStack(ctx, clients, s.dir, identity, subs, shown, out); err != nil {
			return refuse(ctx, clients, build, env, fmt.Sprintf("The plan of %s's stack failed a test (build %s): %v", e, build.ID, errors.Cause(err)), err, out)
		}
		summaries = append(summaries, fmt.Sprintf("**%s**: %s%s", e, p.Summary(), changeList(p)))
	}
	// The pull request's own stack is initialized next, against another prefix.
	if err := os.RemoveAll(filepath.Join(string(w), stackDir, ".terraform")); err != nil {
		return errors.Wrap(err, "os.RemoveAll()")
	}
	pr, err := speaker(ctx, clients, build, env, false)
	if err != nil || pr == nil {
		return err
	}

	return pr.comment(ctx, fmt.Sprintf("The stack's plan for each environment (build %s), the same tests passed in each:\n\n%s", build.ID, strings.Join(summaries, "\n")), out)
}

// changeList is a plan's changes for a comment: one line each, after the summary; none
// for a plan with no change.
func changeList(p *StackPlan) string {
	if len(p.Changes) == 0 {
		return ""
	}
	var lines []string
	for _, c := range p.Changes {
		lines = append(lines, "  - `"+strings.Join(c.Actions, "+")+" "+c.Address+"`")
	}

	return "\n" + strings.Join(lines, "\n")
}

// planIdentities reads each environment's plan identity off the substitutions, and
// refuses an environment of the promotion order that names none: its 2-env is not applied
// with a bedrock that makes the plan identities.
func planIdentities(subs map[string]string) (map[string]string, error) {
	identities := map[string]string{}
	for _, pair := range strings.Split(subs[planIdentitiesSub], ",") {
		env, identity, _ := strings.Cut(pair, "=")
		if env != "" {
			identities[env] = identity
		}
	}
	for _, e := range strings.Split(subs[environmentsSub], ",") {
		if e == "" {
			return nil, errors.Newf("%s names an empty environment in %s: the stack's triggers carry the promotion order", BuildFile, environmentsSub)
		}
		if identities[e] == "" {
			return nil, errors.Newf("%s names no plan identity for %s (%s): apply 2-env for %s with this bedrock, which makes one per application, then the application's stack, whose triggers carry it", BuildFile, e, planIdentitiesSub, e)
		}
	}

	return identities, nil
}

// refuse says a refusal on the pull request (with the repository's token when the
// environment has no deployer app) and answers the error.
func refuse(ctx context.Context, clients *Clients, build *Build, env map[string]string, body string, cause error, out io.Writer) error {
	pr, err := speaker(ctx, clients, build, env, true)
	if err != nil {
		return err
	}
	if pr != nil {
		if err := pr.comment(ctx, body, out); err != nil {
			return err
		}
	}

	return cause
}

// tagBuildStep reads the workspace for a step that runs only in a tag build; ok is false,
// said on out, in a pull-request build.
func tagBuildStep(w Workspace, out io.Writer) (subs map[string]string, ok bool, err error) {
	build, err := w.Build()
	if err != nil {
		return nil, false, err
	}
	if build.Substitutions[prNumberSub] != "" {
		fmt.Fprintln(out, pullRequestBuildNotice)

		return build.Substitutions, false, nil
	}

	return build.Substitutions, true, nil
}

// newEnvironmentStack is the environment's stack in the checkout, driven through tofu as
// the identity (the apply identity in a tag build, an environment's plan identity in a
// pull-request build).
func newEnvironmentStack(clients *Clients, w Workspace, identity string, out io.Writer) *stack {
	return &stack{
		run:      clients.Exec,
		dir:      filepath.Join(string(w), stackDir),
		env:      []string{"GOOGLE_IMPERSONATE_SERVICE_ACCOUNT=" + identity},
		out:      out,
		clients:  clients,
		identity: identity,
	}
}

// initEnvironment points the stack at the environment's state prefix, 3-app/<app>/<env>,
// afresh: the backend cache of an earlier init, another environment's or a pull request's,
// is removed first.
func (s *stack) initEnvironment(ctx context.Context, app, env, identity string) error {
	if err := os.RemoveAll(filepath.Join(s.dir, ".terraform")); err != nil {
		return errors.Wrap(err, "os.RemoveAll()")
	}
	prefix := "3-app/" + app + "/" + env
	fmt.Fprintf(s.out, "=== %s's stack at %s as %s ===\n", env, prefix, identity)

	return s.tofu(ctx, "init", "-input=false", "-no-color",
		"-backend-config=prefix="+prefix,
		"-backend-config=impersonate_service_account="+identity)
}

// testStack runs the tests a plan of the environment's stack passes before it is applied,
// the same on a pull request and in the tag build: no authoritative IAM resource in the
// stack (a *_iam_binding or *_iam_policy replaces every member of its role on each apply)
// other than the file stores' bucket policies, named from the buckets' addresses the
// trigger carries (_FILE_STORES; the stack sets each of those buckets' whole permission
// list on purpose, and a pull-request stack makes buckets of its own), and every secret
// version a planned revision template pins exists and is enabled, read from Secret
// Manager as the identity the plan ran as, so a revision never fails to start on a mount
// the plan could not see. The migrations' sequence rule is GuardMigrations', earlier in
// the same build.
func testStack(ctx context.Context, clients *Clients, dir, identity string, subs map[string]string, plan []byte, out io.Writer) error {
	found, err := check.ScanAuthoritative(dir, fileStorePolicies(subs, plan))
	if err != nil {
		return err
	}
	if len(found) > 0 {
		lines := make([]string, 0, len(found))
		for _, a := range found {
			lines = append(lines, fmt.Sprintf("%s:%d %s", a.Path, a.Line, a.Address))
		}

		return errors.Newf("%sthe stack declares an authoritative IAM resource, which replaces every member of its role on each apply (a file store's bucket policy, storage.tf's, is the one admitted): %s", rejected, strings.Join(lines, ", "))
	}
	mounts, err := plannedMounts(plan, subs[projectSub])
	if err != nil {
		return err
	}
	if len(mounts) == 0 {
		fmt.Fprintln(out, "Tests passed: no authoritative IAM resource other than a file store's bucket policy; no secret version pinned.")

		return nil
	}
	secrets, err := clients.SecretsAs(ctx, identity)
	if err != nil {
		return err
	}
	defer secrets.Close()
	for _, m := range mounts {
		state, err := secrets.State(ctx, m)
		if err != nil {
			return errors.Newf("%sa planned revision template pins secret version %s, which could not be read (%v); every pinned version exists and is enabled before the stack is applied", rejected, m, err)
		}
		if state != enabledState {
			return errors.Newf("%sa planned revision template pins secret version %s, which is %s; a revision would fail to start on it", rejected, m, state)
		}
	}
	fmt.Fprintf(out, "Tests passed: no authoritative IAM resource other than a file store's bucket policy; %d pinned secret version(s) exist and are enabled.\n", len(mounts))

	return nil
}

// fileStorePolicies are the file stores' bucket policies as the stack addresses them,
// from the buckets' addresses in _FILE_STORES, the trigger's and the planned stack's
// substitutions output both (a pull request that declares a store plans its policy before
// any trigger carries the bucket, which the lab found when the first stack with a file
// store was refused its own policy): the one authoritative IAM resource per store the test
// admits.
func fileStorePolicies(subs map[string]string, plan []byte) []string {
	var policies []string
	for _, address := range append(fileStoreAddresses(subs), plannedFileStores(plan)...) {
		if policy := derive.BucketPolicyAddress(address); policy != "" && !slices.Contains(policies, policy) {
			policies = append(policies, policy)
		}
	}

	return policies
}

// plannedFileStores are the buckets the planned stack names in its substitutions output
// (_FILE_STORES): none when the plan carries no such output or its value is not known yet.
func plannedFileStores(plan []byte) []string {
	var doc planDocument
	if err := json.Unmarshal(plan, &doc); err != nil {
		return nil
	}
	after, ok := doc.OutputChanges["substitutions"].After.(map[string]any)
	if !ok {
		return nil
	}
	value, _ := after[fileStoresSub].(string)

	return fileStoreAddresses(map[string]string{fileStoresSub: value})
}

// plannedMounts are the secret versions the planned Cloud Run services and jobs pin
// (template, containers, env, value_source, secret_key_ref), as version resource names,
// sorted and unique; a secret named without its project is the planned resource's
// project's, else the build's.
func plannedMounts(data []byte, project string) ([]string, error) {
	var plan planDocument
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): the plan's JSON")
	}
	var mounts []string
	for _, c := range plan.ResourceChanges {
		if (c.Type != "google_cloud_run_v2_service" && c.Type != "google_cloud_run_v2_job") || c.Change.After == nil {
			continue
		}
		owner := project
		if p, ok := c.Change.After["project"].(string); ok && p != "" {
			owner = p
		}
		for _, ref := range secretRefs(c.Change.After) {
			secret, _ := ref["secret"].(string)
			version, _ := ref["version"].(string)
			if secret == "" || version == "" {
				continue
			}
			if !strings.Contains(secret, "/") {
				secret = "projects/" + owner + "/secrets/" + secret
			}
			if name := secret + "/versions/" + version; !slices.Contains(mounts, name) {
				mounts = append(mounts, name)
			}
		}
	}
	sort.Strings(mounts)

	return mounts, nil
}

// secretRefs walks a planned value for every secret_key_ref block, at any depth.
func secretRefs(v any) []map[string]any {
	var refs []map[string]any
	switch x := v.(type) {
	case map[string]any:
		for key, child := range x {
			if key != "secret_key_ref" {
				refs = append(refs, secretRefs(child)...)

				continue
			}
			list, _ := child.([]any)
			for _, e := range list {
				if m, ok := e.(map[string]any); ok {
					refs = append(refs, m)
				}
			}
		}
	case []any:
		for _, e := range x {
			refs = append(refs, secretRefs(e)...)
		}
	}

	return refs
}
