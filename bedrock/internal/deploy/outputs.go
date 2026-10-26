// outputs.go is what the pull request's stack job leaves for the preview build, and the
// preview's read of it. The job (deploy pr-stack, in the stack's pr-stack trigger, as the
// apply identity) writes one object per pull request and commit into the records bucket,
// <app>/stacks/<env>/pr<N>/<commit>.json: what the stack says once applied (the services,
// the hostname, the job, the migrate command's settings, the build arguments, the
// database mode), that it was destroyed, or that the job failed and why. Only the apply
// identity may write under <app>/stacks/; the deploy identity reads. The preview build
// (deploy pr-outputs, in cloudbuild.yaml) reads the object where it first needs the
// stack, waiting for the job when the object is not there yet, and takes the job's
// failure as its own.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/render"
)

const (
	// stacksSegment is the segment under the application's prefix in the records bucket
	// where the stack job writes: the one prefix the apply identity may write and the
	// deploy identity may not (2-env's records bucket policy).
	stacksSegment = "stacks"
	// The wait for the object: the stack job starts with the preview build on the same
	// comment and usually finishes before the image build does, so the wait is normally
	// nothing; ten minutes covers a plan and apply that started late, or a destroy.
	stackOutputsWait = 10 * time.Minute
	stackOutputsPoll = 15 * time.Second
	// commentIDFact is the id of the /gcbrun comment the preview build ran on, which
	// resolve exports: the preview reads the object the job wrote for the same comment.
	commentIDFact = "COMMENT_ID"
)

// The statuses an outputs object carries.
const (
	StackApplied   = "applied"
	StackDestroyed = "destroyed"
	StackFailed    = "failed"
)

// StackOutputs is the stack job's object: what the pull request's stack says once
// applied, or that the job destroyed it or failed.
type StackOutputs struct {
	App         string `json:"app"`
	Env         string `json:"env"`
	PullRequest string `json:"pullRequest"`
	Commit      string `json:"commit"`
	// Build is the job's build id, and Comment the id of the /gcbrun comment it acted
	// on (0 for a refresh on a push, which read no comment).
	Build   string `json:"build"`
	Comment int64  `json:"comment"`
	// Status is applied, destroyed or failed; Reason says why the job failed.
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	Timestamp string `json:"timestamp"`
	// What the applied stack says, as the deploy steps read it: the services
	// (region=service per region), the job process's template job, the hostname, the
	// migrate command's settings, secrets (their pinned versions, never values) and
	// databases, and the build arguments the placement
	// declares, by the argument's name.
	Services         string            `json:"services,omitempty"`
	JobsJob          string            `json:"jobsJob,omitempty"`
	Hostname         string            `json:"hostname,omitempty"`
	Hostnames        string            `json:"hostnames,omitempty"`
	MigrateEnv       string            `json:"migrateEnv,omitempty"`
	MigrateSecrets   string            `json:"migrateSecrets,omitempty"`
	MigrateDatabases string            `json:"migrateDatabases,omitempty"`
	BuildArguments   map[string]string `json:"buildArguments,omitempty"`
	// RevisionTemplates is the stack's revision_templates output as applied: what the
	// preview build's deploy service and deploy jobs build the revisions and the job
	// from.
	RevisionTemplates *RevisionTemplates `json:"revisionTemplates,omitempty"`
	// SharedDatabase says the stack has no database of its own (/gcbrun shared-db);
	// DatabaseRecreated says the job replaced the pull request's database, and why.
	SharedDatabase    bool   `json:"sharedDatabase,omitempty"`
	DatabaseRecreated string `json:"databaseRecreated,omitempty"`
}

// StackOutputsObject is the object's name in the records bucket for the pull request and
// the commit.
func StackOutputsObject(app, env, number, commit string) string {
	return app + "/" + stacksSegment + "/" + env + "/pr" + number + "/" + commit + ".json"
}

// newStackOutputs starts the job's object for its build.
func newStackOutputs(build *Build) *StackOutputs {
	subs := build.Substitutions

	return &StackOutputs{App: subs[appSub], Env: subs[envSub], PullRequest: subs[prNumberSub], Commit: subs[commitSub], Build: build.ID}
}

// applied fills the object from the facts the apply read off the stack (stackFacts) and
// the job's instruction.
func (o *StackOutputs) applied(f *Facts, facts map[string]string) {
	o.Status = StackApplied
	o.Services, o.JobsJob, o.Hostname, o.Hostnames = facts[services], facts[jobsJobFact], facts[prHostnameFact], facts[prHostnamesFact]
	o.MigrateEnv, o.MigrateSecrets, o.MigrateDatabases = facts[migrateEnvFact], facts[migrateSecretsFact], facts[migrateDatabasesFact]
	for _, name := range stackArgumentNames(facts) {
		if o.BuildArguments == nil {
			o.BuildArguments = map[string]string{}
		}
		o.BuildArguments[name] = facts[derive.BuildArgumentSubstitution(name)]
	}
	o.SharedDatabase = f.SharedDB
}

// fail marks the object as the job's failure, with its reason.
func (o *StackOutputs) fail(err error) {
	o.Status, o.Reason = StackFailed, err.Error()
}

// facts are what the preview build's steps read of an applied stack, by the facts'
// names: the services, the hostname, the job, the migrate command's settings, secrets
// and databases, the database mode and the build arguments as their substitutions.
func (o *StackOutputs) facts() map[string]string {
	facts := map[string]string{
		services: o.Services, prHostnameFact: o.Hostname,
		sharedDBFact: flag(o.SharedDatabase), reloadDBFact: flag(o.DatabaseRecreated != ""), reloadReasonFact: o.DatabaseRecreated,
	}
	for fact, value := range map[string]string{jobsJobFact: o.JobsJob, prHostnamesFact: o.Hostnames, migrateEnvFact: o.MigrateEnv, migrateSecretsFact: o.MigrateSecrets, migrateDatabasesFact: o.MigrateDatabases} {
		if value != "" {
			facts[fact] = value
		}
	}
	for name, value := range o.BuildArguments {
		facts[derive.BuildArgumentSubstitution(name)] = value
	}

	return facts
}

// writeStackOutputs writes the job's object into the records bucket and says where.
func writeStackOutputs(ctx context.Context, open StoreFunc, build *Build, o *StackOutputs, now time.Time, out io.Writer) error {
	o.Timestamp = now.UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return errors.Wrap(err, "json.MarshalIndent()")
	}
	store, err := open(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	bucket, object := build.Substitutions[recordsBucket], StackOutputsObject(o.App, o.Env, o.PullRequest, o.Commit)
	if err := store.Write(ctx, bucket, object, append(data, '\n')); err != nil {
		return err
	}
	fmt.Fprintf(out, "The stack's outputs (%s) are at gs://%s/%s for the preview build.\n", o.Status, bucket, object)

	return nil
}

// ReadStackOutputs is the preview build's read of what the pull request's stack job
// applied (deploy pr-outputs): the object for this build's commit and /gcbrun comment,
// waited for while the job runs, whose facts go to the environment file for the steps
// after. A job that failed stops this build with the job's reason; a destroy is what a
// /gcbrun down build waits for, and nothing else. A tag build says so and ends.
func ReadStackOutputs(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, build, ok, err := pullRequestStep(w, out)
	if err != nil || !ok {
		return err
	}
	subs := build.Substitutions
	comment, _ := strconv.ParseInt(env[commentIDFact], 10, 64)
	store, err := clients.Storage(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	bucket, object := subs[recordsBucket], StackOutputsObject(subs[appSub], subs[envSub], subs[prNumberSub], subs[commitSub])
	o, err := awaitStackOutputs(ctx, clients, store, bucket, object, comment, out)
	if err != nil {
		return err
	}
	down := env[downFact] == trueValue
	switch {
	case o.Status == StackFailed:
		return errors.Newf("%sthe pull request's stack job failed (build %s): %s", rejected, o.Build, strings.TrimPrefix(o.Reason, rejected))
	case o.Status == StackDestroyed && !down:
		return errors.Newf("the pull request's stack job destroyed the environment (build %s), where this build was to deploy into it: comment /gcbrun again to stand it up", o.Build)
	case o.Status == StackDestroyed:
		fmt.Fprintf(out, "The stack job destroyed the pull request's environment (build %s); nothing deploys.\n", o.Build)

		return nil
	case down:
		return errors.Newf("the pull request's stack job applied the stack (build %s), where this build was to tear it down (/gcbrun down): the two read different comments", o.Build)
	case o.Services == "":
		return errors.Newf("the stack job's outputs name no services (gs://%s/%s)", bucket, object)
	}
	if o.RevisionTemplates == nil {
		return errors.Newf("the stack job's outputs carry no revision templates (gs://%s/%s): the pull request's stack was applied by a bedrock before them, and this build cannot deploy without them; comment %s again once the pin moves", bucket, object, gcbrun)
	}
	facts := o.facts()
	if err := w.Append(facts); err != nil {
		return err
	}
	if err := writeRevisionTemplates(w, o.RevisionTemplates); err != nil {
		return err
	}
	fmt.Fprintf(out, "The pull request's stack (job %s) names %s=%s", o.Build, services, o.Services)
	if o.JobsJob != "" {
		fmt.Fprintf(out, " %s=%s", jobsJobFact, o.JobsJob)
	}
	fmt.Fprintf(out, " %s=%s, and the migrate command's settings and secrets (%s, %s); the site runs against %s.\n", prHostnameFact, o.Hostname, migrateEnvFact, migrateSecretsFact, databaseMode(facts, subs[envSub]))
	if names := slices.Sorted(maps.Keys(o.BuildArguments)); len(names) > 0 {
		fmt.Fprintf(out, "The image build passes the pull request's own build arguments: %s.\n", strings.Join(names, ", "))
	}

	return nil
}

// awaitStackOutputs reads the object once the job has written it for the comment, trying
// again every poll until the wait runs out: a stale object, written for another /gcbrun
// comment on the same commit, is waited past.
func awaitStackOutputs(ctx context.Context, clients *Clients, store Store, bucket, object string, comment int64, out io.Writer) (*StackOutputs, error) {
	var waited time.Duration
	for {
		o, err := readStackOutputs(ctx, store, bucket, object)
		if err != nil {
			return nil, err
		}
		switch {
		case o != nil && o.Comment == comment:
			if waited > 0 {
				fmt.Fprintf(out, "The stack job wrote gs://%s/%s after %s.\n", bucket, object, waited)
			}

			return o, nil
		case o != nil && waited == 0:
			fmt.Fprintf(out, "gs://%s/%s was written for another /gcbrun comment (%d; this build's is %d): waiting for the job of this comment, up to %s.\n", bucket, object, o.Comment, comment, stackOutputsWait)
		case waited == 0:
			fmt.Fprintf(out, "Waiting for the pull request's stack job to write gs://%s/%s (the stack's %s trigger runs it on the same /gcbrun comment), up to %s.\n", bucket, object, render.StackTrigger, stackOutputsWait)
		}
		if waited >= stackOutputsWait {
			return nil, errors.Newf("the pull request's stack job has not written gs://%s/%s after %s: the stack's %s trigger runs it on the same /gcbrun comment, as the apply identity, and it has not finished or never started. The trigger exists once the stack is applied with this bedrock in %s (the first time by hand; a release's tag build after that), and its build is in the Cloud Build history beside this one", bucket, object, waited, render.StackTrigger, tstEnvironment)
		}
		if err := clients.sleep()(ctx, stackOutputsPoll); err != nil {
			return nil, err
		}
		waited += stackOutputsPoll
	}
}

// readStackOutputs reads the object, or answers nil when it is not there yet.
func readStackOutputs(ctx context.Context, store Store, bucket, object string) (*StackOutputs, error) {
	names, err := store.List(ctx, bucket, object)
	if err != nil {
		return nil, errors.Wrapf(err, "looking for gs://%s/%s", bucket, object)
	}
	if !slices.Contains(names, object) {
		return nil, nil
	}
	data, err := store.Read(ctx, bucket, object)
	if err != nil {
		return nil, err
	}
	var o StackOutputs
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): gs://%s/%s", bucket, object)
	}

	return &o, nil
}
