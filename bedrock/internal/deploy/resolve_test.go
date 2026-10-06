package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

// fakeBuilds is Builds in tests: one build, one token, and what was asked.
type fakeBuilds struct {
	build   string
	token   string
	mintErr error
	got     string
	asked   []string
}

func (b *fakeBuilds) open(context.Context) (Builds, error) {
	return b, nil
}

func (b *fakeBuilds) Get(_ context.Context, project, location, id string) ([]byte, error) {
	b.got = project + "/" + location + "/" + id

	return []byte(b.build), nil
}

func (b *fakeBuilds) ReadToken(_ context.Context, repository string) (string, error) {
	b.asked = append(b.asked, repository)
	if b.mintErr != nil {
		return "", b.mintErr
	}

	return b.token, nil
}

// fakeComments is the comment read in tests: the bodies, oldest first, and the call made.
type fakeComments struct {
	bodies []string
	err    error
	called string
}

func (c *fakeComments) read(_ context.Context, token, repo string, number int) ([]github.Comment, error) {
	c.called = fmt.Sprintf("%s %s %d", token, repo, number)
	if c.err != nil {
		return nil, c.err
	}
	all := make([]github.Comment, 0, len(c.bodies))
	for i, body := range c.bodies {
		all = append(all, github.Comment{ID: int64(i + 1), Body: body})
	}

	return all, nil
}

// trickyValue is a declared substitution's value with everything the shell quotes.
const trickyValue = `it's "on" $now` + "`"

// tagBuild is a tag build's substitutions; prBuild a pull request's. An override with an
// empty value removes the substitution.
func tagBuild(overrides map[string]string) map[string]string {
	subs := map[string]string{
		"TAG_NAME": "v1.2.3", "COMMIT_SHA": "deadbeefcafe", "SHORT_SHA": "deadbee",
		"REPO_FULL_NAME": "impulseframework/harbor", "_REPO_CONNECTION_NAME": "imp-tst-github", "_REPO_NAME": "harbor",
		"_ENV": "tst", "_APP": "harbor", "_PROJECT": "tst-project", "_REGISTRY": "us-central1-docker.pkg.dev/shr/reg",
		"_SERVICES": "us-central1=harbor-app", "_MIGRATE_ENV": `{"APP_SERVICE_NAME":"harbor-migrate"}`,
		"_DEFAULT_BRANCH": "master", "_WIDGET_MODE": trickyValue,
	}
	for name, value := range overrides {
		if value == "" {
			delete(subs, name)

			continue
		}
		subs[name] = value
	}

	return subs
}

func prBuild(overrides map[string]string) map[string]string {
	subs := tagBuild(map[string]string{"TAG_NAME": "", prNumberSub: "7", "_BASE_BRANCH": "master", "_HEAD_BRANCH": "widgets"})
	maps.Copy(subs, overrides)
	for name, value := range overrides {
		if value == "" {
			delete(subs, name)
		}
	}

	return subs
}

// known is the stack's contract in these tests.
var known = []string{"_ENV", "_APP", "_PROJECT", "_REGISTRY", "_SERVICES", "_MIGRATE_ENV", "_REPO_CONNECTION_NAME", "_REPO_NAME", "_RECORDS_BUCKET", "_MIGRATIONS_DIR", "_SEED", "_RESTORE", "_REQUESTER", "_MIGRATE_ACTION", "_MIGRATE_TABLE", "_MIGRATE_VERSION", "_BUILD_SECRETS"}

func buildFor(t *testing.T, subs map[string]string) string {
	t.Helper()

	data, err := json.Marshal(Build{ID: "b-1", Substitutions: subs})
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// outcome is what a test compares of the facts.
type outcome struct {
	Version, Release, Image, ImageTag, CommitTag, Comment, Token string
	SharedDB, ReloadDB, Down, RunMigrations, ShiftTraffic, Seed  bool
	// MaintenanceOff is the maintenance instruction (bedrock maintenance off).
	MaintenanceOff                                  bool
	ReloadReason, Restore, Requester, RestoreReason string
	// RestoreDatabase and RestoreDatabaseBackup are production's live database and the
	// backup a rollback restored it from, for a restore from production's backup.
	RestoreDatabase, RestoreDatabaseBackup string
	// KeepsReleaseBackups says the environment is on the checkout placement's
	// releaseBackups list (production alone unless it says otherwise).
	KeepsReleaseBackups bool
	// Rollback and RollbackReason are a rollback run's instruction.
	Rollback, RollbackReason string
	// Migration is the migration operation in words, empty for none.
	Migration string
	// Declared are the declared substitutions' names and Values their values, as the
	// checkout declares them; BuildSecrets the build secrets the image build reads.
	Declared     []string
	Values       map[string]string
	BuildSecrets string
	// BuildArguments are the build arguments the environment file exports from the
	// trigger, by name.
	BuildArguments []string
}

func summarize(f *Facts) outcome {
	o := outcome{
		Version: f.Version, Release: f.Release, Image: f.Image, ImageTag: f.ImageTag, CommitTag: f.CommitTag, Comment: f.Comment, Token: f.Token,
		SharedDB: f.SharedDB, ReloadDB: f.ReloadDB, Down: f.Down, RunMigrations: f.RunMigrations, ShiftTraffic: f.ShiftTraffic, Seed: f.Seed, KeepsReleaseBackups: f.KeepsReleaseBackups,
		Rollback: f.Rollback, RollbackReason: f.RollbackReason, MaintenanceOff: f.MaintenanceOff,
		ReloadReason: f.ReloadReason, Restore: f.Restore, Requester: f.Requester, RestoreReason: f.RestoreReason, Declared: f.Declared,
		RestoreDatabase: f.RestoreDatabase, RestoreDatabaseBackup: f.RestoreDatabaseBackup,
		Values: f.declared, BuildSecrets: f.BuildSecrets, BuildArguments: f.BuildArguments,
	}
	if f.Migration != nil {
		o.Migration = f.Migration.String()
	}

	return o
}

// The records and trees the stale-database cases use: pull request 7's last build
// applied 000003_Sites, and the tree either still carries it or has renumbered it.
const (
	sitesUp      = "schema/migrations/000003_Sites.up.sql"
	sitesContent = "create table Sites"
)

func recordWith(build, timestamp string, applied ...Migration) string {
	data, err := json.Marshal(Record{App: "harbor", Env: "tst", Build: build, Timestamp: timestamp, Status: Preview, Migrations: applied})
	if err != nil {
		panic(err)
	}

	return string(data)
}

// liveRecordWith is a release's live record in env: what the environment runs, with the
// migration and seed files its build applied. The seed-change cases compare the tree
// with it.
func liveRecordWith(env, version, build, timestamp string, applied ...Migration) string {
	data, err := json.Marshal(Record{App: "harbor", Env: env, Version: version, Build: build, Timestamp: timestamp, Status: Live, Migrations: applied})
	if err != nil {
		panic(err)
	}

	return string(data)
}

// The seed the seed-change cases use: the environment's live release v1.2.2 applied
// 000001_Seed with "insert a", and the tree either still carries it so or changed it.
const (
	seedUp      = "schema/devseed/000001_Seed.up.sql"
	seedContent = "insert a"
	// seedChangedReason is the reason the build gives when the live release's seed is
	// no longer in the tree as applied.
	seedChangedReason = "the seed changed since v1.2.2 applied it (build b-0): schema/devseed/000001_Seed.up.sql, not in the tree as applied (edited, renumbered or removed since), so the database is recreated and the migrations and the seed apply from the start"
)

// stagingTag is a tag build in stg with the records the staging rehearsal reads named:
// tst's as the previous environment's, production's by the buckets and plan identities.
func stagingTag(overrides map[string]string) map[string]string {
	subs := tagBuild(map[string]string{"_ENV": "stg", "_RECORDS_BUCKET": "stg-records", "_PREVIOUS_ENV": "tst", "_PREVIOUS_RECORDS_BUCKET": "tst-records", "_RECORDS_BUCKETS": "tst=tst-records,stg=stg-records,prd=prd-records", "_MIGRATIONS_DIR": "schema/migrations"})
	for name, value := range overrides {
		if value == "" {
			delete(subs, name)

			continue
		}
		subs[name] = value
	}

	return subs
}

// The records the rehearsal cases read: tst applied the release v1.2.3 with migrations 1
// and 2, production runs v1.2.2 with migration 1 on its third database generation, and
// staging runs v1.2.2 too.
var (
	initUp        = Migration{Dir: "schema/migrations", Name: "000001_Init.up.sql", Hash: hashOf("create table a")}
	widgetsUp     = Migration{Dir: "schema/migrations", Name: "000002_Widgets.up.sql", Hash: hashOf("create table widgets")}
	failedUp      = Migration{Dir: "schema/migrations", Name: "000003_Failed.up.sql", Hash: hashOf("alter table a")}
	productionRec = func(applied ...Migration) string {
		data, err := json.Marshal(Record{App: "harbor", Env: "prd", Version: "v1.2.2", Build: "b-5", Timestamp: "2026-10-05T10:00:00Z", Status: Live, Migrations: applied, Database: &DatabaseRef{Name: "projects/p-spn/instances/shared-spanner/databases/harbor-prd-db-3", Generation: 3}, Restore: &Restore{Kind: "projects/p-spn/instances/shared-spanner/backups/harbor-prd-db-2-pre-v1-2-2", Backup: "projects/p-spn/instances/shared-spanner/backups/harbor-prd-db-2-pre-v1-2-2", Database: "projects/p-spn/instances/shared-spanner/databases/harbor-prd-db-3", Generation: 3, PreviousGeneration: 2}})
		if err != nil {
			panic(err)
		}

		return string(data)
	}
	rehearsing = map[string]string{
		"gs://tst-records/harbor/tst/v1.2.3/b-7.json": liveRecordWith("tst", "v1.2.3", "b-7", "2026-10-06T01:00:00Z", initUp, widgetsUp),
		"gs://prd-records/harbor/prd/v1.2.2/b-5.json": productionRec(initUp),
		"gs://stg-records/harbor/stg/v1.2.2/b-6.json": liveRecordWith("stg", "v1.2.2", "b-6", "2026-10-05T11:00:00Z", initUp),
	}
	rehearsalTree     = map[string]string{"schema/migrations/000001_Init.up.sql": "create table a", "schema/migrations/000002_Widgets.up.sql": "create table widgets"}
	rehearsalVersions = "tst applied version 2, build b-7; prd runs version 1, release v1.2.2, build b-5"
)

// seededTag is a tag build in an environment on the placement's seed list, with the
// records bucket and the migrations directory the comparison reads.
func seededTag(overrides map[string]string) map[string]string {
	subs := tagBuild(map[string]string{"_RECORDS_BUCKET": "records", "_MIGRATIONS_DIR": "schema/migrations", seedSub: trueValue})
	for name, value := range overrides {
		if value == "" {
			delete(subs, name)

			continue
		}
		subs[name] = value
	}

	return subs
}

// placementPath is the placement's path in the checkout, beside the stack.
var placementPath = stackDir + "/" + placementFile

// placementSeeding is the checkout's placement with the seed list given, as JSON: what a
// release build reads whether it seeds from.
func placementSeeding(seed string) string {
	return strings.TrimSuffix(testPlacement(""), "}\n") + `, "seed": ` + seed + "}\n"
}

// tfvarsPath is the stack's terraform.tfvars in the checkout.
var tfvarsPath = stackDir + "/terraform.tfvars"

// stackTfvarsWith is the checkout's terraform.tfvars declaring, in every environment, the
// substitutions and the build secrets' pins given (each an HCL object).
func stackTfvarsWith(substitutions, buildSecrets string) string {
	var b strings.Builder
	for _, key := range []string{"substitutions", "build_secrets"} {
		body := substitutions
		if key == "build_secrets" {
			body = buildSecrets
		}
		b.WriteString(key + " = {\n")
		for _, env := range environments {
			b.WriteString("  " + env + " = " + body + "\n")
		}
		b.WriteString("}\n")
	}

	return b.String()
}

// widgetMode is the declared substitution tagBuild's trigger carries, as the checkout
// declares it (the shell's quotes in HCL's); declaringWidgetMode the checkout declaring it.
var (
	widgetMode          = `{ _WIDGET_MODE = "it's \"on\" $now` + "`" + `" }`
	declaringWidgetMode = stackTfvarsWith(widgetMode, "{}")
)

// seedingTst is a placement whose seed list names tst; seedingNone one whose list is empty.
var (
	seedingTst  = placementSeeding(`["tst"]`)
	seedingNone = placementSeeding(`[]`)
)

// declaringArguments is a placement declaring the build arguments harbor's does: the
// Firebase web API key and the environment project.
var declaringArguments = strings.TrimSuffix(testPlacement(""), "}\n") + `, "buildArguments": {"FIREBASE_API_KEY": "firebaseApiKey", "PROJECT_ID": "projectId"}}` + "\n"

// liveSeeded is tst's live record of v1.2.2 with the seed applied.
var liveSeeded = map[string]string{"gs://records/harbor/tst/v1.2.2/b-0.json": liveRecordWith("tst", "v1.2.2", "b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/migrations", Name: "000003_Sites.up.sql", Hash: hashOf(sitesContent)}, Migration{Dir: "schema/devseed", Name: "000001_Seed.up.sql", Hash: hashOf(seedContent)})}

func TestResolve(t *testing.T) {
	t.Parallel()

	const image = "us-central1-docker.pkg.dev/shr/reg/harbor"
	widget := map[string]string{"_WIDGET_MODE": trickyValue}
	tag := outcome{Version: "v1.2.3", Release: "v1.2.3", Image: image, ImageTag: "v1.2.3-tst", CommitTag: "deadbeefcafe-tst", Token: "tok", RunMigrations: true, ShiftTraffic: true, Declared: []string{"_WIDGET_MODE"}, Values: widget}
	pr := outcome{Version: "pr7@deadbee", Release: "pr7-deadbee", Image: image, ImageTag: "pr7-deadbee-tst", CommitTag: "deadbeefcafe-tst", Token: "tok", RunMigrations: true, ShiftTraffic: true, Seed: true, Declared: []string{"_WIDGET_MODE"}, Values: widget}
	// npmContainer is a build secret's container as the trigger names it.
	const npmContainer = "projects/tst-project/secrets/imp-tst-gbl-harbor-npm-token"
	withComment := func(o outcome, comment string, set func(o *outcome)) outcome {
		o.Comment = comment
		set(&o)

		return o
	}
	// seeded is a tag build's outcome where the placement in the checkout seeds tst.
	seeded := withComment(tag, "", func(o *outcome) { o.Seed = true })
	// staged is a tag build's outcome in stg (the staging rehearsal cases).
	staged := withComment(tag, "", func(o *outcome) { o.ImageTag, o.CommitTag = "v1.2.3-stg", "deadbeefcafe-stg" })
	tests := []struct {
		name     string
		subs     map[string]string
		comments []string
		// records are the records bucket's objects by gs:// path; tree the migration
		// files in the checkout. placement is the checkout's placement, one whose seed
		// list is empty when not given; noPlacement leaves the checkout without one.
		// tfvars is the checkout's terraform.tfvars, declaringWidgetMode when not given.
		records     map[string]string
		tree        map[string]string
		placement   string
		noPlacement bool
		// denied are the buckets the store refuses to list, as Cloud Storage refuses
		// a read without a grant.
		denied []string
		tfvars string
		// commentsErr fails the comment read; mintErr fails the token.
		commentsErr error
		mintErr     error
		want        outcome
		wantOut     []string
		// wantNotOut is what the log must not carry: a build argument's value among it.
		wantNotOut []string
		// wantAsked is the repository the token was minted for; wantCalled the comment read.
		wantAsked  string
		wantCalled string
		wantErr    string
	}{
		{
			name:      "a tag build names its release and the environment's tags",
			subs:      tagBuild(nil),
			want:      tag,
			wantOut:   []string{"Triggered by tag v1.2.3", "Seed: tst is not on the seed list of the placement in the checkout (infrastructure/placement.json).", "IMAGE=" + image + " IMAGE_TAG=v1.2.3-tst VERSION=v1.2.3 RELEASE=v1.2.3", "RUN_MIGRATIONS=true SHIFT_TRAFFIC=true REVISION_TAG=", "Declared substitutions for the hooks and the image build: _WIDGET_MODE"},
			wantAsked: "projects/tst-project/locations/us-central1/connections/imp-tst-github/repositories/harbor",
		},
		{
			name:    "a declared substitution is passed as the checkout declares it, where the trigger still carries its stack's last apply",
			subs:    tagBuild(nil),
			tfvars:  stackTfvarsWith(`{ _WIDGET_MODE = "dawn" }`, "{}"),
			want:    withComment(tag, "", func(o *outcome) { o.Values = map[string]string{"_WIDGET_MODE": "dawn"} }),
			wantOut: []string{"infrastructure/terraform.tfvars declares another value for _WIDGET_MODE than the trigger carries (its stack's last apply): this build passes the checkout's."},
		},
		{
			name:   "a substitution the checkout declares that the trigger does not carry yet is passed in the release that declares it",
			subs:   tagBuild(nil),
			tfvars: stackTfvarsWith(`{ _THEME = "dusk", _WIDGET_MODE = "it's \"on\" $now`+"`"+`" }`, "{}"),
			want: withComment(tag, "", func(o *outcome) {
				o.Declared, o.Values = []string{"_THEME", "_WIDGET_MODE"}, map[string]string{"_THEME": "dusk", "_WIDGET_MODE": trickyValue}
			}),
			wantOut: []string{"infrastructure/terraform.tfvars declares _THEME, which the trigger does not carry yet (its stack's last apply): this build passes it."},
		},
		{
			name:    "a substitution the trigger carries that the checkout no longer declares is left out in the release that drops it",
			subs:    tagBuild(nil),
			tfvars:  stackTfvarsWith("{}", "{}"),
			want:    withComment(tag, "", func(o *outcome) { o.Declared, o.Values = nil, map[string]string{} }),
			wantOut: []string{"The trigger carries _WIDGET_MODE, which infrastructure/terraform.tfvars no longer declares for tst: this build leaves it out."},
		},
		{
			name:    "a declared substitution the pipeline's contract carries is refused before anything is built",
			subs:    tagBuild(nil),
			tfvars:  stackTfvarsWith(`{ _SEED = "true" }`, "{}"),
			wantErr: "infrastructure/terraform.tfvars declares _SEED for tst, a substitution the pipeline's contract carries; rename it",
		},
		{
			name:    "a declared substitution that is not upper snake case is refused",
			subs:    tagBuild(nil),
			tfvars:  stackTfvarsWith(`{ _theme = "dusk" }`, "{}"),
			wantErr: `infrastructure/terraform.tfvars declares "_theme" for tst: a declared substitution starts with an underscore and is upper snake case (_NAME)`,
		},
		{
			name:    "a build secret is read at the checkout's pin, in the container the trigger names, where the trigger still pins its stack's last apply",
			subs:    tagBuild(map[string]string{buildSecretsSub: "NPM_TOKEN=" + npmContainer + "/versions/3"}),
			tfvars:  stackTfvarsWith(widgetMode, `{ NPM_TOKEN = "4" }`),
			want:    withComment(tag, "", func(o *outcome) { o.BuildSecrets = "NPM_TOKEN=" + npmContainer + "/versions/4" }),
			wantOut: []string{"infrastructure/terraform.tfvars pins build secret NPM_TOKEN at version 4, where the trigger (its stack's last apply) pins 3: the image build reads 4."},
		},
		{
			name:   "a build secret the checkout pins as the trigger does is read there",
			subs:   tagBuild(map[string]string{buildSecretsSub: "NPM_TOKEN=" + npmContainer + "/versions/3"}),
			tfvars: stackTfvarsWith(widgetMode, `{ NPM_TOKEN = "3" }`),
			want:   withComment(tag, "", func(o *outcome) { o.BuildSecrets = "NPM_TOKEN=" + npmContainer + "/versions/3" }),
		},
		{
			name:    "a build secret the checkout declares that the trigger does not carry yet is read from the next release on: its container comes with this build's apply",
			subs:    tagBuild(nil),
			tfvars:  stackTfvarsWith(widgetMode, `{ NPM_TOKEN = "1" }`),
			want:    tag,
			wantOut: []string{"infrastructure/terraform.tfvars declares build secret NPM_TOKEN, which the trigger does not carry yet: its container and the deploy identity's access come with this build's stack apply, after the image build, so the image build reads it from the next release on."},
		},
		{
			name:    "a build secret the trigger carries that the checkout no longer declares is left out",
			subs:    tagBuild(map[string]string{buildSecretsSub: "NPM_TOKEN=" + npmContainer + "/versions/3"}),
			want:    tag,
			wantOut: []string{"The trigger carries build secret NPM_TOKEN, which infrastructure/terraform.tfvars no longer declares for tst: the image build leaves it out."},
		},
		{
			name:    "a build secret pinned at latest is refused: a build reads one version",
			subs:    tagBuild(map[string]string{buildSecretsSub: "NPM_TOKEN=" + npmContainer + "/versions/3"}),
			tfvars:  stackTfvarsWith(widgetMode, `{ NPM_TOKEN = "latest" }`),
			wantErr: `infrastructure/terraform.tfvars pins build secret NPM_TOKEN at "latest" for tst: a build reads one version, a number, never latest`,
		},
		{
			name:    "a build secret whose name is not upper snake case is refused",
			subs:    tagBuild(nil),
			tfvars:  stackTfvarsWith(widgetMode, `{ npm_token = "1" }`),
			wantErr: `build secret "npm_token": a name in upper snake case`,
		},
		{
			name:     "a pull-request build reads the first environment's declarations and pins from its own tree",
			subs:     prBuild(map[string]string{buildSecretsSub: "NPM_TOKEN=" + npmContainer + "/versions/3"}),
			comments: []string{"/gcbrun"},
			tfvars:   stackTfvarsWith(`{ _WIDGET_MODE = "the pull request's" }`, `{ NPM_TOKEN = "5" }`),
			want: withComment(pr, "/gcbrun", func(o *outcome) {
				o.Values, o.BuildSecrets = map[string]string{"_WIDGET_MODE": "the pull request's"}, "NPM_TOKEN="+npmContainer+"/versions/5"
			}),
		},
		{
			name:      "a tag build exports the build arguments the checkout declares as the trigger carries them; one not carried yet comes from the next release on, one no longer declared is left out, and no value is logged",
			subs:      tagBuild(map[string]string{"_BUILD_ARG_FIREBASE_API_KEY": "AIzaTstKey", "_BUILD_ARG_OLD_ARG": "gone"}),
			placement: declaringArguments,
			want:      withComment(tag, "", func(o *outcome) { o.BuildArguments = []string{"FIREBASE_API_KEY"} }),
			wantOut: []string{
				"infrastructure/placement.json declares build argument PROJECT_ID (projectId), which the trigger does not carry yet: _BUILD_ARG_PROJECT_ID comes with this build's stack apply, after the image build, so the image build passes it from the next release on.",
				"The trigger carries _BUILD_ARG_OLD_ARG, which infrastructure/placement.json no longer declares (buildArguments): the image build leaves it out.",
				"Build arguments infrastructure/placement.json declares (buildArguments), as the trigger carries them: FIREBASE_API_KEY.",
			},
			wantNotOut: []string{"AIzaTstKey", "The trigger carries _BUILD_ARG_OLD_ARG, which infrastructure/terraform.tfvars"},
		},
		{
			name:       "a tag build whose trigger carries every declared build argument exports them all",
			subs:       tagBuild(map[string]string{"_BUILD_ARG_FIREBASE_API_KEY": "AIzaTstKey", "_BUILD_ARG_PROJECT_ID": "tst-project"}),
			placement:  declaringArguments,
			want:       withComment(tag, "", func(o *outcome) { o.BuildArguments = []string{"FIREBASE_API_KEY", "PROJECT_ID"} }),
			wantOut:    []string{"Build arguments infrastructure/placement.json declares (buildArguments), as the trigger carries them: FIREBASE_API_KEY, PROJECT_ID."},
			wantNotOut: []string{"does not carry yet", "no longer declares"},
		},
		{
			name:       "a pull-request build exports none of the trigger's build arguments: its image passes the pull request's own stack's",
			subs:       prBuild(map[string]string{"_BUILD_ARG_FIREBASE_API_KEY": "AIzaTstKey", "_BUILD_ARG_PROJECT_ID": "tst-project"}),
			comments:   []string{"/gcbrun"},
			placement:  declaringArguments,
			want:       withComment(pr, "/gcbrun", func(*outcome) {}),
			wantOut:    []string{"Build arguments infrastructure/placement.json declares (buildArguments): FIREBASE_API_KEY, PROJECT_ID; the image build passes the pull request's own, from its stack once applied (deploy pr-stack apply)."},
			wantNotOut: []string{"AIzaTstKey", "does not carry yet"},
		},
		{
			name:    "a declared substitution on the build arguments' prefix is refused before anything is built",
			subs:    tagBuild(nil),
			tfvars:  stackTfvarsWith(`{ _BUILD_ARG_FIREBASE_API_KEY = "AIzaByHand" }`, "{}"),
			wantErr: "infrastructure/terraform.tfvars declares _BUILD_ARG_FIREBASE_API_KEY for tst: a substitution starting with _BUILD_ARG_ carries a build argument infrastructure/placement.json declares (buildArguments), from the stack's own values; rename it",
		},
		{
			name:    "a restore run names what replaces the database and who asked",
			subs:    tagBuild(map[string]string{restoreSub: restoreEmpty, requesterSub: "octocat"}),
			want:    withComment(tag, "", func(o *outcome) { o.Restore, o.Requester = restoreEmpty, "octocat" }),
			wantOut: []string{"Restore run: tst's database is replaced (empty) before v1.2.3 deploys, asked for by octocat."},
		},
		{
			name: "production's backup is restored into stg",
			subs: tagBuild(map[string]string{restoreSub: restoreBackup, requesterSub: "octocat", "_ENV": "stg"}),
			want: withComment(tag, "", func(o *outcome) {
				o.Restore, o.Requester, o.ImageTag, o.CommitTag = restoreBackup, "octocat", "v1.2.3-stg", "deadbeefcafe-stg"
			}),
			wantOut: []string{"Restore run: stg's database is replaced (production-backup) before v1.2.3 deploys, asked for by octocat."},
		},
		{
			name:    "production is never emptied by a run",
			subs:    tagBuild(map[string]string{restoreSub: restoreEmpty, requesterSub: "octocat", "_ENV": "prd"}),
			wantErr: "_RESTORE=empty in prd: production's database is never emptied by a run; it is restored to a backup (bedrock restore --before, --at or --backup)",
		},
		{
			name:    "production's backup goes into stg alone",
			subs:    tagBuild(map[string]string{restoreSub: restoreBackup, requesterSub: "octocat"}),
			wantErr: "_RESTORE=production-backup in tst: production's backup is restored into stg, the environment on production's instance; tst is restored to an empty database (_RESTORE=empty)",
		},
		{
			name:    "an unknown restore is refused",
			subs:    tagBuild(map[string]string{restoreSub: "yesterday", requesterSub: "octocat"}),
			wantErr: `unknown _RESTORE "yesterday" (the restores are empty, production-backup, a backup's resource name (projects/<p>/instances/<i>/backups/<b>) and @<moment>)`,
		},
		{
			name:    "a restore names who asked",
			subs:    tagBuild(map[string]string{restoreSub: restoreEmpty}),
			wantErr: "_RESTORE=empty names no requester (_REQUESTER): a restore says who asked for it",
		},
		{
			name:    "a requester alone is a rerun: the tag build again, naming who asked",
			subs:    tagBuild(map[string]string{requesterSub: "octocat"}),
			want:    withComment(tag, "", func(o *outcome) { o.Requester = "octocat" }),
			wantOut: []string{"Rerun: v1.2.3 runs again in tst, asked for by octocat."},
		},
		{
			name: "a rerun reaches production",
			subs: tagBuild(map[string]string{requesterSub: "octocat", "_ENV": "prd"}),
			want: withComment(tag, "", func(o *outcome) {
				o.Requester, o.ImageTag, o.CommitTag, o.KeepsReleaseBackups = "octocat", "v1.2.3-prd", "deadbeefcafe-prd", true
			}),
			wantOut: []string{"Rerun: v1.2.3 runs again in prd, asked for by octocat.", "Release backup: prd keeps a backup of its database as of the cut, the moment before the migrations run (placement.json's releaseBackups); bedrock restore --before returns to it."},
		},
		{
			name:     "a pull-request build is not rerun through the door",
			subs:     prBuild(map[string]string{requesterSub: "octocat"}),
			comments: []string{"/gcbrun"},
			wantErr:  "_REQUESTER=octocat on a pull-request build: a rerun is a release build's; a pull request is built again with a /gcbrun comment",
		},
		{
			name:     "a pull-request build carries no restore",
			subs:     prBuild(map[string]string{restoreSub: restoreEmpty, requesterSub: "octocat"}),
			comments: []string{"/gcbrun"},
			wantErr:  "_RESTORE=empty on a pull-request build: a restore is a release build's instruction; a pull request's own database is recreated with /gcbrun reload-db",
		},
		{
			name: "a version operation is read and reported",
			subs: tagBuild(map[string]string{migrateActionSub: actionVersion, requesterSub: "octocat"}),
			want: withComment(tag, "", func(o *outcome) {
				o.Migration = "version: the migrate command prints the database's migration version and nothing else deploys, asked for by octocat"
			}),
			wantOut: []string{"Migration operation version: the migrate command prints the database's migration version and nothing else deploys, asked for by octocat."},
		},
		{
			name: "a rerun needs no requester",
			subs: tagBuild(map[string]string{migrateActionSub: actionRerun}),
			want: withComment(tag, "", func(o *outcome) {
				o.Migration = "rerun: the migrate command runs as it always does and the release continues"
			}),
			wantOut: []string{"Migration operation rerun: the migrate command runs as it always does and the release continues."},
		},
		{
			name: "a force names its table and version",
			subs: tagBuild(map[string]string{migrateActionSub: actionForce, migrateTableSub: tableData, migrateVersionSub: "40", requesterSub: "octocat"}),
			want: withComment(tag, "", func(o *outcome) {
				o.Migration = "force: the data migrations table is set to version 40, then the migrations run and the release continues, asked for by octocat"
			}),
			wantOut: []string{"Migration operation force: the data migrations table is set to version 40, then the migrations run and the release continues, asked for by octocat."},
		},
		{
			name:    "a force without a version is refused",
			subs:    tagBuild(map[string]string{migrateActionSub: actionForce, requesterSub: "octocat"}),
			wantErr: "_MIGRATE_ACTION=force names no version (_MIGRATE_VERSION): a force says which version the database is at, or -1 for no version",
		},
		{
			name:    "a force with a version that is not an integer is refused",
			subs:    tagBuild(map[string]string{migrateActionSub: actionForce, migrateVersionSub: "forty", requesterSub: "octocat"}),
			wantErr: `_MIGRATE_VERSION "forty" is not a version: an integer 0 or above, or -1 for no version`,
		},
		{
			name:    "a force names who asked",
			subs:    tagBuild(map[string]string{migrateActionSub: actionForce, migrateVersionSub: "40"}),
			wantErr: "_MIGRATE_ACTION=force names no requester (_REQUESTER): a force says who asked for it",
		},
		{
			name:    "an unknown operation is refused",
			subs:    tagBuild(map[string]string{migrateActionSub: "undo", requesterSub: "octocat"}),
			wantErr: `unknown _MIGRATE_ACTION "undo" (the actions are version, rerun and force)`,
		},
		{
			name:    "a version value goes with a force alone",
			subs:    tagBuild(map[string]string{migrateActionSub: actionVersion, migrateVersionSub: "40", requesterSub: "octocat"}),
			wantErr: "_MIGRATE_VERSION=40 with _MIGRATE_ACTION=version: a version goes with force",
		},
		{
			name:     "a pull-request build carries no migration operation",
			subs:     prBuild(map[string]string{migrateActionSub: actionVersion, requesterSub: "octocat"}),
			comments: []string{"/gcbrun"},
			wantErr:  "_MIGRATE_ACTION=version on a pull-request build: a migration operation is a release build's; a pull request's own database is recreated with /gcbrun reload-db",
		},
		{
			name:    "a migration operation never rides a restore",
			subs:    tagBuild(map[string]string{restoreSub: restoreEmpty, migrateActionSub: actionRerun, requesterSub: "octocat"}),
			wantErr: "_MIGRATE_ACTION=rerun with _RESTORE=empty: a restore replaces the database, so there is no migration state to operate on",
		},
		{
			name:    "a build without a connection is one no trigger started, and is refused",
			subs:    tagBuild(map[string]string{"_REPO_CONNECTION_NAME": ""}),
			wantErr: `_REPO_CONNECTION_NAME="" _REPO_NAME="harbor": every build starts from a trigger, which passes the environment's Cloud Build connection and the repository's link (both exist once 2-env holds the GitHub authorization); nothing is submitted by hand`,
		},
		{
			name:    "a build without the repository's link is refused the same way",
			subs:    tagBuild(map[string]string{"_REPO_NAME": ""}),
			wantErr: `_REPO_CONNECTION_NAME="imp-tst-github" _REPO_NAME="": every build starts from a trigger`,
		},
		{
			name:      "a build with nothing declared says so",
			subs:      tagBuild(map[string]string{"_WIDGET_MODE": ""}),
			tfvars:    stackTfvarsWith("{}", "{}"),
			want:      withComment(tag, "", func(o *outcome) { o.Declared, o.Values = nil, map[string]string{} }),
			wantOut:   []string{"Declared substitutions for the hooks and the image build: none"},
			wantAsked: "projects/tst-project/locations/us-central1/connections/imp-tst-github/repositories/harbor",
		},
		{
			name:       "a pull request's build reads its instruction",
			subs:       prBuild(nil),
			comments:   []string{"/gcbrun reload-db"},
			want:       withComment(pr, "/gcbrun reload-db", func(o *outcome) { o.ReloadDB = true; o.ReloadReason = "/gcbrun reload-db" }),
			wantOut:    []string{"Triggered by pull request 7", "COMMENT_BODY=/gcbrun reload-db", "IMAGE=" + image + " IMAGE_TAG=pr7-deadbee-tst VERSION=pr7@deadbee RELEASE=pr7-deadbee"},
			wantCalled: "tok impulseframework/harbor 7",
		},
		{
			name:     "shared-db runs no migrations",
			subs:     prBuild(nil),
			comments: []string{"/gcbrun shared-db"},
			want:     withComment(pr, "/gcbrun shared-db", func(o *outcome) { o.SharedDB = true; o.RunMigrations = false }),
			wantOut:  []string{"RUN_MIGRATIONS=false SHIFT_TRAFFIC=true REVISION_TAG="},
		},
		{
			name:     "a migration the last build applied is no longer in the tree: the database is recreated",
			subs:     prBuild(map[string]string{"_RECORDS_BUCKET": "records", "_MIGRATIONS_DIR": "schema/migrations"}),
			comments: []string{"/gcbrun"},
			records:  map[string]string{"gs://records/harbor/tst/pr7-0000abc/b-0.json": recordWith("b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/migrations", Name: "000003_Sites.up.sql", Hash: hashOf(sitesContent)})},
			tree:     map[string]string{"schema/migrations/000004_Sites.up.sql": sitesContent},
			want: withComment(pr, "/gcbrun", func(o *outcome) {
				o.ReloadDB = true
				o.ReloadReason = "the database applied schema/migrations/000003_Sites.up.sql (build b-0), which the tree no longer carries as applied: renumbered or changed since, so the database is recreated and the migrations apply afresh"
			}),
			wantOut: []string{"Reload: the database applied schema/migrations/000003_Sites.up.sql (build b-0)"},
		},
		{
			name:     "a migration changed since the last build applied it is the same case",
			subs:     prBuild(map[string]string{"_RECORDS_BUCKET": "records", "_MIGRATIONS_DIR": "schema/migrations"}),
			comments: []string{"/gcbrun"},
			records:  map[string]string{"gs://records/harbor/tst/pr7-0000abc/b-0.json": recordWith("b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/migrations", Name: "000003_Sites.up.sql", Hash: hashOf(sitesContent)})},
			tree:     map[string]string{sitesUp: sitesContent + ", edited"},
			want: withComment(pr, "/gcbrun", func(o *outcome) {
				o.ReloadDB = true
				o.ReloadReason = "the database applied schema/migrations/000003_Sites.up.sql (build b-0), which the tree no longer carries as applied: renumbered or changed since, so the database is recreated and the migrations apply afresh"
			}),
		},
		{
			name:     "the tree still carries what the last build applied: nothing is recreated",
			subs:     prBuild(map[string]string{"_RECORDS_BUCKET": "records", "_MIGRATIONS_DIR": "schema/migrations"}),
			comments: []string{"/gcbrun"},
			records:  map[string]string{"gs://records/harbor/tst/pr7-0000abc/b-0.json": recordWith("b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/migrations", Name: "000003_Sites.up.sql", Hash: hashOf(sitesContent)})},
			tree:     map[string]string{sitesUp: sitesContent, "schema/migrations/000004_Audit.up.sql": "create table Audit"},
			want:     withComment(pr, "/gcbrun", func(*outcome) {}),
		},
		{
			name:     "a seed file the last build applied changed: the database is recreated so the seed applies from the start",
			subs:     prBuild(map[string]string{"_RECORDS_BUCKET": "records", "_MIGRATIONS_DIR": "schema/migrations"}),
			comments: []string{"/gcbrun"},
			records:  map[string]string{"gs://records/harbor/tst/pr7-0000abc/b-0.json": recordWith("b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/migrations", Name: "000003_Sites.up.sql", Hash: hashOf(sitesContent)}, Migration{Dir: "schema/devseed", Name: "000001_Seed.up.sql", Hash: hashOf("insert a")})},
			tree:     map[string]string{sitesUp: sitesContent, "schema/devseed/000001_Seed.up.sql": "insert a, edited"},
			want: withComment(pr, "/gcbrun", func(o *outcome) {
				o.ReloadDB = true
				o.ReloadReason = "the database applied schema/devseed/000001_Seed.up.sql (build b-0), which the tree no longer carries as applied: renumbered or changed since, so the database is recreated and the migrations apply afresh"
			}),
			wantOut: []string{"Reload: the database applied schema/devseed/000001_Seed.up.sql (build b-0)"},
		},
		{
			name:     "a new seed file beside the applied ones changes nothing",
			subs:     prBuild(map[string]string{"_RECORDS_BUCKET": "records", "_MIGRATIONS_DIR": "schema/migrations"}),
			comments: []string{"/gcbrun"},
			records:  map[string]string{"gs://records/harbor/tst/pr7-0000abc/b-0.json": recordWith("b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/migrations", Name: "000003_Sites.up.sql", Hash: hashOf(sitesContent)}, Migration{Dir: "schema/devseed", Name: "000001_Seed.up.sql", Hash: hashOf("insert a")})},
			tree:     map[string]string{sitesUp: sitesContent, "schema/devseed/000001_Seed.up.sql": "insert a", "schema/devseed/000002_More.up.sql": "insert b"},
			want:     withComment(pr, "/gcbrun", func(*outcome) {}),
		},
		{
			name:      "a seed file the environment's live release applied changed: the release restores the database so the seed applies from the start",
			subs:      seededTag(nil),
			placement: seedingTst,
			records:   liveSeeded,
			tree:      map[string]string{sitesUp: sitesContent, seedUp: seedContent + ", edited"},
			want: withComment(seeded, "", func(o *outcome) {
				o.Restore, o.Requester, o.RestoreReason = restoreEmpty, "release v1.2.3", seedChangedReason
			}),
			wantOut: []string{"Restore run: tst's database is replaced (empty) before v1.2.3 deploys, " + seedChangedReason + "."},
		},
		{
			name:      "a seed file the live release applied is gone from the tree: the same case",
			subs:      seededTag(nil),
			placement: seedingTst,
			records:   liveSeeded,
			tree:      map[string]string{sitesUp: sitesContent},
			want: withComment(seeded, "", func(o *outcome) {
				o.Restore, o.Requester, o.RestoreReason = restoreEmpty, "release v1.2.3", seedChangedReason
			}),
		},
		{
			name:      "the tree carries the seed as the live release applied it: nothing is restored",
			subs:      seededTag(nil),
			placement: seedingTst,
			records:   liveSeeded,
			tree:      map[string]string{sitesUp: sitesContent, seedUp: seedContent},
			want:      seeded,
			wantOut:   []string{"Seed: tst is on the seed list of the placement in the checkout (infrastructure/placement.json)."},
		},
		{
			name:      "a new seed file beside the applied ones is a data migration the migrate command applies: nothing is restored",
			subs:      seededTag(nil),
			placement: seedingTst,
			records:   liveSeeded,
			tree:      map[string]string{sitesUp: sitesContent, seedUp: seedContent, "schema/devseed/000002_More.up.sql": "insert b"},
			want:      seeded,
		},
		{
			name:      "a changed schema migration is the hotfix check's concern, not the seed comparison's",
			subs:      seededTag(nil),
			placement: seedingTst,
			records:   liveSeeded,
			tree:      map[string]string{sitesUp: sitesContent + ", edited", seedUp: seedContent},
			want:      seeded,
		},
		{
			name:      "the release that puts the environment on the placement's seed list seeds in that release, though the trigger's _SEED still says false",
			subs:      seededTag(map[string]string{seedSub: "false"}),
			placement: seedingTst,
			records:   map[string]string{"gs://records/harbor/tst/v1.2.2/b-0.json": liveRecordWith("tst", "v1.2.2", "b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/migrations", Name: "000003_Sites.up.sql", Hash: hashOf(sitesContent)})},
			tree:      map[string]string{sitesUp: sitesContent, seedUp: seedContent},
			want:      seeded,
			wantOut:   []string{"Seed: tst is on the seed list of the placement in the checkout (infrastructure/placement.json).", "The trigger's _SEED=false is what the stack said at its last apply; the placement in the checkout decides for this release."},
		},
		{
			name:      "the placement's seed list decides the restore too: a changed seed restores where the trigger's _SEED still says false",
			subs:      seededTag(map[string]string{seedSub: "false"}),
			placement: seedingTst,
			records:   liveSeeded,
			tree:      map[string]string{sitesUp: sitesContent, seedUp: seedContent + ", edited"},
			want: withComment(seeded, "", func(o *outcome) {
				o.Restore, o.Requester, o.RestoreReason = restoreEmpty, "release v1.2.3", seedChangedReason
			}),
		},
		{
			name:      "the release that takes the environment off the placement's seed list stops seeding in that release, though the trigger's _SEED still says true: a changed seed is nothing to it",
			subs:      seededTag(nil),
			placement: seedingNone,
			records:   liveSeeded,
			tree:      map[string]string{sitesUp: sitesContent, seedUp: seedContent + ", edited"},
			want:      tag,
			wantOut:   []string{"Seed: tst is not on the seed list of the placement in the checkout (infrastructure/placement.json).", "The trigger's _SEED=true is what the stack said at its last apply; the placement in the checkout decides for this release."},
		},
		{
			name:        "a tag build reads the seed list from the checkout's placement: a checkout without one is refused",
			subs:        seededTag(nil),
			noPlacement: true,
			wantErr:     "the checkout's placement (infrastructure/placement.json), where the seed list is written",
		},
		{
			name:      "a restore asked for already replaces the database: the seed comparison yields to it",
			subs:      seededTag(map[string]string{restoreSub: restoreEmpty, requesterSub: "octocat"}),
			placement: seedingTst,
			records:   liveSeeded,
			tree:      map[string]string{sitesUp: sitesContent, seedUp: seedContent + ", edited"},
			want:      withComment(seeded, "", func(o *outcome) { o.Restore, o.Requester = restoreEmpty, "octocat" }),
			wantOut:   []string{"Restore run: tst's database is replaced (empty) before v1.2.3 deploys, asked for by octocat."},
		},
		{
			name:      "only a live record counts: a preview is a build whose traffic never shifted",
			subs:      seededTag(nil),
			placement: seedingTst,
			records:   map[string]string{"gs://records/harbor/tst/v1.2.2/b-0.json": recordWith("b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/devseed", Name: "000001_Seed.up.sql", Hash: hashOf(seedContent)})},
			tree:      map[string]string{sitesUp: sitesContent, seedUp: seedContent + ", edited"},
			want:      seeded,
		},
		{
			name:      "the newest live record decides: the seed as the latest release applied it",
			subs:      seededTag(nil),
			placement: seedingTst,
			records: map[string]string{
				"gs://records/harbor/tst/v1.2.1/b-9.json": liveRecordWith("tst", "v1.2.1", "b-9", "2026-09-26T05:00:00Z", Migration{Dir: "schema/devseed", Name: "000001_Seed.up.sql", Hash: hashOf("insert a, old")}),
				"gs://records/harbor/tst/v1.2.2/b-0.json": liveSeeded["gs://records/harbor/tst/v1.2.2/b-0.json"],
			},
			tree: map[string]string{sitesUp: sitesContent, seedUp: seedContent},
			want: seeded,
		},
		{
			name:      "a pull request's live record under the environment is its own environment's, not the release the environment runs",
			subs:      seededTag(nil),
			placement: seedingTst,
			records: map[string]string{
				"gs://records/harbor/tst/v1.2.2/b-0.json":      liveSeeded["gs://records/harbor/tst/v1.2.2/b-0.json"],
				"gs://records/harbor/tst/pr9-abc0123/b-8.json": liveRecordWith("tst", "pr9@abc0123", "b-8", "2026-09-28T05:00:00Z", Migration{Dir: "schema/devseed", Name: "000001_Seed.up.sql", Hash: hashOf("insert a, the pull request's")}),
			},
			tree: map[string]string{sitesUp: sitesContent, seedUp: seedContent},
			want: seeded,
		},
		{
			name:    "production is never restored by a run: its seed is not compared",
			subs:    seededTag(map[string]string{"_ENV": "prd"}),
			records: map[string]string{"gs://records/harbor/prd/v1.2.2/b-0.json": liveRecordWith("prd", "v1.2.2", "b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/devseed", Name: "000001_Seed.up.sql", Hash: hashOf(seedContent)})},
			tree:    map[string]string{sitesUp: sitesContent, seedUp: seedContent + ", edited"},
			want: withComment(tag, "", func(o *outcome) {
				o.ImageTag, o.CommitTag, o.KeepsReleaseBackups = "v1.2.3-prd", "deadbeefcafe-prd", true
			}),
		},
		{
			name: "a rollback names the release it leaves, who asked and why, runs no migration, and prints the statement first",
			subs: tagBuild(map[string]string{rollbackSub: "v1.2.4", reasonSub: "v1.2.4 mangled the invoices", requesterSub: "octocat", "_ENV": "prd"}),
			want: withComment(tag, "", func(o *outcome) {
				o.ImageTag, o.CommitTag, o.KeepsReleaseBackups = "v1.2.3-prd", "deadbeefcafe-prd", true
				o.Rollback, o.RollbackReason, o.Requester, o.RunMigrations = "v1.2.4", "v1.2.4 mangled the invoices", "octocat", false
			}),
			wantOut: []string{
				"=== ROLLBACK of prd: harbor returns to v1.2.3 from v1.2.4, asked for by octocat: v1.2.4 mangled the invoices ===",
				"Nothing of the database: it stays as v1.2.4 left it, every migration it holds applied, and v1.2.3 runs on it. No migration runs, no backup is taken, nothing is restored; v1.2.3's build runs again and deploys as a release does. The database is returned by bedrock restore, in a run of its own.",
			},
		},
		{
			name: "a rollback in an environment off the releaseBackups list is a rollback like any other: it needs no backup",
			subs: tagBuild(map[string]string{rollbackSub: "v1.2.4", reasonSub: "why", requesterSub: "octocat"}),
			want: withComment(tag, "", func(o *outcome) {
				o.Rollback, o.RollbackReason, o.Requester, o.RunMigrations = "v1.2.4", "why", "octocat", false
			}),
		},
		{
			name:    "a rollback on a pull-request build is refused",
			subs:    prBuild(map[string]string{rollbackSub: "v1.2.4", reasonSub: "why"}),
			wantErr: "_ROLLBACK=v1.2.4 on a pull-request build: a rollback is a release build's instruction",
		},
		{
			name:    "a rollback with a restore is refused: one run after the other",
			subs:    tagBuild(map[string]string{rollbackSub: "v1.2.4", reasonSub: "why", requesterSub: "octocat", restoreSub: restoreEmpty}),
			wantErr: "_ROLLBACK=v1.2.4 with _RESTORE=empty: a rollback returns the code and a restore the database; a run does one, and both means one run after the other",
		},
		{
			name:    "a rollback without a reason is refused",
			subs:    tagBuild(map[string]string{rollbackSub: "v1.2.4", requesterSub: "octocat", "_ENV": "prd"}),
			wantErr: "_ROLLBACK=v1.2.4 gives no reason (_REASON): a rollback says why it was asked for",
		},
		{
			name:    "a rollback without a requester is refused",
			subs:    tagBuild(map[string]string{rollbackSub: "v1.2.4", reasonSub: "why", "_ENV": "prd"}),
			wantErr: "_ROLLBACK=v1.2.4 names no requester (_REQUESTER): a rollback says who asked for it",
		},
		{
			name:    "a rollback whose release left is not a release tag is refused",
			subs:    tagBuild(map[string]string{rollbackSub: "last-week", reasonSub: "why", requesterSub: "octocat", "_ENV": "prd"}),
			wantErr: `_ROLLBACK="last-week" is not a release tag (v<major>.<minor>.<patch>): the release the environment leaves`,
		},
		{
			name:    "a rollback that leaves the release it is of is refused",
			subs:    tagBuild(map[string]string{rollbackSub: "v1.2.3", reasonSub: "why", requesterSub: "octocat", "_ENV": "prd"}),
			wantErr: "_ROLLBACK=v1.2.3 is the release this build is of: a rollback returns to an earlier release",
		},
		{
			name: "a restore to a backup in production names it, who asked and why",
			subs: tagBuild(map[string]string{restoreSub: "projects/spn/instances/i/backups/imp-prd-gbl-harbor-db-pre-v1-2-4", requesterSub: "octocat", reasonSub: "the migration mangled the invoices", "_ENV": "prd"}),
			want: withComment(tag, "", func(o *outcome) {
				o.ImageTag, o.CommitTag, o.KeepsReleaseBackups = "v1.2.3-prd", "deadbeefcafe-prd", true
				o.Restore, o.Requester, o.RestoreReason = "projects/spn/instances/i/backups/imp-prd-gbl-harbor-db-pre-v1-2-4", "octocat", "the migration mangled the invoices"
			}),
			wantOut: []string{"Restore run: prd's database is replaced (projects/spn/instances/i/backups/imp-prd-gbl-harbor-db-pre-v1-2-4) before v1.2.3 deploys, asked for by octocat."},
		},
		{
			name: "a restore to a moment names the generation the backup is taken of",
			subs: tagBuild(map[string]string{restoreSub: "@2026-10-05T04:00:00Z", restoreDatabaseSub: "imp-prd-gbl-harbor-db", requesterSub: "octocat", "_ENV": "prd"}),
			want: withComment(tag, "", func(o *outcome) {
				o.ImageTag, o.CommitTag, o.KeepsReleaseBackups = "v1.2.3-prd", "deadbeefcafe-prd", true
				o.Restore, o.Requester, o.RestoreDatabase = "@2026-10-05T04:00:00Z", "octocat", "imp-prd-gbl-harbor-db"
			}),
		},
		{
			name:    "production is never emptied by a run",
			subs:    tagBuild(map[string]string{restoreSub: restoreEmpty, requesterSub: "octocat", "_ENV": "prd"}),
			wantErr: "_RESTORE=empty in prd: production's database is never emptied by a run; it is restored to a backup (bedrock restore --before, --at or --backup)",
		},
		{
			name:    "a restore moment that is not RFC 3339 is refused",
			subs:    tagBuild(map[string]string{restoreSub: "@yesterday", requesterSub: "octocat", "_ENV": "prd"}),
			wantErr: "_RESTORE=@yesterday: the moment after @ is not RFC 3339 (2026-10-05T04:30:00Z)",
		},
		{
			name:    "a restore instruction that is none of the four is refused",
			subs:    tagBuild(map[string]string{restoreSub: "last-week", requesterSub: "octocat", "_ENV": "prd"}),
			wantErr: `unknown _RESTORE "last-week" (the restores are empty, production-backup, a backup's resource name (projects/<p>/instances/<i>/backups/<b>) and @<moment>)`,
		},
		{
			name: "the maintenance instruction makes the run take the application out of maintenance and deploy nothing",
			subs: tagBuild(map[string]string{maintenanceSub: "off", requesterSub: "octocat", "_ENV": "prd"}),
			want: withComment(tag, "", func(o *outcome) {
				o.ImageTag, o.CommitTag, o.KeepsReleaseBackups = "v1.2.3-prd", "deadbeefcafe-prd", true
				o.MaintenanceOff, o.Requester, o.RunMigrations, o.ShiftTraffic = true, "octocat", false, false
			}),
			wantOut: []string{"RUN_MIGRATIONS=false SHIFT_TRAFFIC=false"},
		},
		{
			name:    "an unknown maintenance instruction is refused",
			subs:    tagBuild(map[string]string{maintenanceSub: "on", requesterSub: "octocat"}),
			wantErr: `unknown _MAINTENANCE "on" (the instruction is off)`,
		},
		{
			name:    "the maintenance instruction on a pull-request build is refused",
			subs:    prBuild(map[string]string{maintenanceSub: "off"}),
			wantErr: "_MAINTENANCE=off on a pull-request build: a pull-request build never goes into maintenance",
		},
		{
			name:    "the maintenance instruction with a restore is refused",
			subs:    tagBuild(map[string]string{maintenanceSub: "off", requesterSub: "octocat", restoreSub: restoreEmpty}),
			wantErr: "_MAINTENANCE=off with a restore, a rollback or a migration operation: the run that ends a maintenance does nothing else",
		},
		{
			name:    "the maintenance instruction without a requester is refused",
			subs:    tagBuild(map[string]string{maintenanceSub: "off"}),
			wantErr: "_MAINTENANCE=off names no requester (_REQUESTER): the run says who asked for it",
		},
		{
			name:      "an environment the placement's releaseBackups list names keeps a backup as of the cut, production or not",
			subs:      tagBuild(nil),
			placement: strings.TrimSuffix(testPlacement(""), "}\n") + `, "releaseBackups": ["tst"]}` + "\n",
			want:      withComment(tag, "", func(o *outcome) { o.KeepsReleaseBackups = true }),
			wantOut:   []string{"Release backup: tst keeps a backup of its database as of the cut, the moment before the migrations run (placement.json's releaseBackups); bedrock restore --before returns to it."},
		},
		{
			name:    "an environment off the list keeps none, and the log names the list",
			subs:    tagBuild(nil),
			want:    withComment(tag, "", func(*outcome) {}),
			wantOut: []string{"Release backup: tst is not on the placement's releaseBackups list (prd), so this run keeps no backup as of the cut and bedrock restore --before does not serve it there."},
		},
		{
			name:     "a pull-request build compares the tree with its own records, not with the environment's live release",
			subs:     prBuild(map[string]string{"_RECORDS_BUCKET": "records", "_MIGRATIONS_DIR": "schema/migrations", seedSub: trueValue}),
			comments: []string{"/gcbrun"},
			records:  liveSeeded,
			tree:     map[string]string{sitesUp: sitesContent, seedUp: seedContent + ", edited"},
			want:     withComment(pr, "/gcbrun", func(*outcome) {}),
		},
		{
			name:     "the newest record decides, an older one is history",
			subs:     prBuild(map[string]string{"_RECORDS_BUCKET": "records", "_MIGRATIONS_DIR": "schema/migrations"}),
			comments: []string{"/gcbrun"},
			records: map[string]string{
				"gs://records/harbor/tst/pr7-0000abc/b-0.json": recordWith("b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/migrations", Name: "000003_Sites.up.sql", Hash: hashOf(sitesContent)}),
				"gs://records/harbor/tst/pr7-0000def/b-1.json": recordWith("b-1", "2026-09-27T06:00:00Z", Migration{Dir: "schema/migrations", Name: "000004_Sites.up.sql", Hash: hashOf(sitesContent)}),
				"gs://records/harbor/tst/pr7-0000fff/b-2.json": recordWith("b-2", "2026-09-27T06:30:00Z"),
			},
			tree: map[string]string{"schema/migrations/000004_Sites.up.sql": sitesContent},
			want: withComment(pr, "/gcbrun", func(*outcome) {}),
		},
		{
			name:     "shared-db never recreates: there is no database of the pull request's own",
			subs:     prBuild(map[string]string{"_RECORDS_BUCKET": "records", "_MIGRATIONS_DIR": "schema/migrations"}),
			comments: []string{"/gcbrun shared-db"},
			records:  map[string]string{"gs://records/harbor/tst/pr7-0000abc/b-0.json": recordWith("b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/migrations", Name: "000003_Sites.up.sql", Hash: hashOf(sitesContent)})},
			tree:     map[string]string{"schema/migrations/000004_Sites.up.sql": sitesContent},
			want:     withComment(pr, "/gcbrun shared-db", func(o *outcome) { o.SharedDB = true; o.RunMigrations = false }),
		},
		{
			name:     "the first build has no record and decides nothing",
			subs:     prBuild(map[string]string{"_RECORDS_BUCKET": "records", "_MIGRATIONS_DIR": "schema/migrations"}),
			comments: []string{"/gcbrun"},
			tree:     map[string]string{sitesUp: sitesContent},
			want:     withComment(pr, "/gcbrun", func(*outcome) {}),
		},
		{
			name:     "down tears the environment down",
			subs:     prBuild(nil),
			comments: []string{"/gcbrun down"},
			want:     withComment(pr, "/gcbrun down", func(o *outcome) { o.Down = true }),
		},
		{
			name:     "the latest /gcbrun comment is the instruction",
			subs:     prBuild(nil),
			comments: []string{"/gcbrun shared-db", "looks good", "/gcbrun", "deployed pr7"},
			want:     withComment(pr, "/gcbrun", func(*outcome) {}),
		},
		{
			name:     "shared-db with reload-db is refused",
			subs:     prBuild(nil),
			comments: []string{"/gcbrun shared-db reload-db"},
			wantErr:  "/gcbrun shared-db with reload-db: reload-db recreates the pull request's own database, and in shared mode there is none",
		},
		{
			name:     "an unknown option stops the build",
			subs:     prBuild(nil),
			comments: []string{"/gcbrun deploy-now"},
			wantErr:  `unknown /gcbrun option "deploy-now" (the options are shared-db, reload-db and down)`,
		},
		{
			name:     "no /gcbrun comment is refused",
			subs:     prBuild(nil),
			comments: []string{"looks good"},
			wantErr:  "no /gcbrun comment found on pull request 7",
		},
		{
			name:        "a failed comment read stops the build",
			subs:        prBuild(nil),
			commentsErr: errors.New("GitHub GET answered 502"),
			wantErr:     "GitHub GET answered 502",
		},
		{
			name:    "a pull request deploys only to tst",
			subs:    prBuild(map[string]string{"_ENV": "stg"}),
			wantErr: "a pull-request build deploys only to tst (this trigger's _ENV is stg)",
		},
		{
			name:    "a pull-request build without a connection is refused too",
			subs:    prBuild(map[string]string{"_REPO_CONNECTION_NAME": ""}),
			wantErr: `_REPO_CONNECTION_NAME="" _REPO_NAME="harbor": every build starts from a trigger`,
		},
		{
			name:    "a pull request number is a number",
			subs:    prBuild(map[string]string{"_PR_NUMBER": "seven"}),
			wantErr: `_PR_NUMBER "seven" is not a pull request number`,
		},
		{
			name:    "neither a tag nor a pull request is refused",
			subs:    tagBuild(map[string]string{"TAG_NAME": ""}),
			wantErr: "neither TAG_NAME nor _PR_NUMBER is set; a build is a tag's or a pull request's",
		},
		{
			name:    "an unknown environment is refused",
			subs:    tagBuild(map[string]string{"_ENV": "dev"}),
			wantErr: `_ENV must be tst, stg, or prd (got "dev")`,
		},
		{
			name:    "a build that names no services is refused",
			subs:    tagBuild(map[string]string{"_SERVICES": ""}),
			wantErr: "_SERVICES names the Cloud Run services this build updates; it is empty",
		},
		{
			name:    "staging rehearsal: a release with a migration production has not applied restores staging from production's newest backup, named by production's record",
			subs:    stagingTag(nil),
			records: rehearsing,
			tree:    rehearsalTree,
			want: withComment(staged, "", func(o *outcome) {
				o.Restore, o.Requester = restoreBackup, "release v1.2.3"
				o.RestoreReason = "v1.2.3 carries migrations prd has not applied (" + rehearsalVersions + "), so stg's database is restored from production's newest backup and the release runs against production's data before production does"
				o.RestoreDatabase, o.RestoreDatabaseBackup = "projects/p-spn/instances/shared-spanner/databases/harbor-prd-db-3", "projects/p-spn/instances/shared-spanner/backups/harbor-prd-db-2-pre-v1-2-2"
			}),
			wantOut: []string{"Restore run: stg's database is replaced (production-backup) before v1.2.3 deploys, v1.2.3 carries migrations prd has not applied (" + rehearsalVersions + ")"},
		},
		{
			name: "staging rehearsal: a release whose migrations production has applied deploys to staging as it stands",
			subs: stagingTag(nil),
			records: map[string]string{
				"gs://tst-records/harbor/tst/v1.2.3/b-7.json": liveRecordWith("tst", "v1.2.3", "b-7", "2026-10-06T01:00:00Z", initUp),
				"gs://prd-records/harbor/prd/v1.2.2/b-5.json": productionRec(initUp),
				"gs://stg-records/harbor/stg/v1.2.2/b-6.json": liveRecordWith("stg", "v1.2.2", "b-6", "2026-10-05T11:00:00Z", initUp),
			},
			tree:    rehearsalTree,
			want:    staged,
			wantOut: []string{"Staging rehearsal: v1.2.3 carries no migration prd has not applied (tst applied version 1, build b-7; prd runs version 1, release v1.2.2, build b-5); stg deploys as it stands, on its database as it is."},
		},
		{
			name: "staging rehearsal: staging ahead of the release (a failed release's migration) is restored to production's state",
			subs: stagingTag(nil),
			records: map[string]string{
				"gs://tst-records/harbor/tst/v1.2.3/b-7.json": liveRecordWith("tst", "v1.2.3", "b-7", "2026-10-06T01:00:00Z", initUp),
				"gs://prd-records/harbor/prd/v1.2.2/b-5.json": productionRec(initUp),
				"gs://stg-records/harbor/stg/v1.2.9/b-8.json": liveRecordWith("stg", "v1.2.9", "b-8", "2026-10-06T00:30:00Z", initUp, failedUp),
			},
			tree: rehearsalTree,
			want: withComment(staged, "", func(o *outcome) {
				o.Restore, o.Requester = restoreBackup, "release v1.2.3"
				o.RestoreReason = "stg's database holds schema/migrations/000003_Failed.up.sql (in v1.2.9's record), which v1.2.3 does not carry, so stg's database is restored from production's newest backup to production's state (tst applied version 1, build b-7; prd runs version 1, release v1.2.2, build b-5) before the release deploys"
				o.RestoreDatabase, o.RestoreDatabaseBackup = "projects/p-spn/instances/shared-spanner/databases/harbor-prd-db-3", "projects/p-spn/instances/shared-spanner/backups/harbor-prd-db-2-pre-v1-2-2"
			}),
			wantOut: []string{"Restore run: stg's database is replaced (production-backup) before v1.2.3 deploys, stg's database holds schema/migrations/000003_Failed.up.sql"},
		},
		{
			name:    "staging rehearsal: a trigger that names no records buckets reads nothing, and staging deploys as it stands",
			subs:    stagingTag(map[string]string{"_RECORDS_BUCKETS": ""}),
			records: rehearsing,
			tree:    rehearsalTree,
			want:    staged,
			wantOut: []string{"Staging rehearsal: the records of tst and prd are not named (_PREVIOUS_ENV, _PREVIOUS_RECORDS_BUCKET, _RECORDS_BUCKETS); stg deploys as it stands."},
		},
		{
			name:    "staging rehearsal: production's records denied to the build are said and left, and staging deploys as it stands",
			subs:    stagingTag(nil),
			records: rehearsing,
			denied:  []string{"prd-records"},
			tree:    rehearsalTree,
			want:    staged,
			wantOut: []string{"Staging rehearsal: prd's deployment records could not be read as the build (", "googleapi: Error 403: stg-deploy@p.iam does not have storage.objects.list access to the Google Cloud Storage bucket prd-records); stg deploys as it stands. Production's records bucket lets this environment's deploy identity read once production's 2-env is applied at this bedrock."},
		},
		{
			name:    "staging rehearsal: no tst record of the release yet is left to the release check",
			subs:    stagingTag(nil),
			records: map[string]string{"gs://prd-records/harbor/prd/v1.2.2/b-5.json": productionRec(initUp)},
			tree:    rehearsalTree,
			want:    staged,
			wantOut: []string{"Staging rehearsal: tst has no live deployment record of v1.2.3 yet; the release check decides whether v1.2.3 may reach stg."},
		},
		{
			name:    "staging rehearsal: production without a live record has no data to run against",
			subs:    stagingTag(nil),
			records: map[string]string{"gs://tst-records/harbor/tst/v1.2.3/b-7.json": liveRecordWith("tst", "v1.2.3", "b-7", "2026-10-06T01:00:00Z", initUp, widgetsUp)},
			tree:    rehearsalTree,
			want:    staged,
			wantOut: []string{"Staging rehearsal: prd has no live deployment record, so there is no production data to run v1.2.3 against; stg deploys as it stands."},
		},
		{
			name:       "staging rehearsal: a build in tst decides nothing",
			subs:       tagBuild(map[string]string{"_RECORDS_BUCKET": "tst-records", "_RECORDS_BUCKETS": "tst=tst-records,prd=prd-records", "_PLAN_IDENTITIES": "prd=prd-plan@p.iam"}),
			records:    rehearsing,
			tree:       rehearsalTree,
			want:       tag,
			wantNotOut: []string{"Staging rehearsal", "Restore run"},
		},
		{
			name:    "a failed mint says which connection",
			subs:    tagBuild(nil),
			mintErr: errors.New("Cloud Build answered HTTP 403 to POST /v2/...: denied"),
			wantErr: "could not mint a GitHub token from connection imp-tst-github",
		},
		{
			name:    "a build without the repository's full name is refused before the token is minted",
			subs:    tagBuild(map[string]string{"REPO_FULL_NAME": ""}),
			wantErr: "REPO_FULL_NAME is not set: a trigger passes it as <organization>/<repository>, and every build starts from a trigger",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builds := &fakeBuilds{build: buildFor(t, tt.subs), token: "tok", mintErr: tt.mintErr}
			comments := &fakeComments{bodies: tt.comments, err: tt.commentsErr}
			store := &memoryStore{objects: tt.records, denied: tt.denied}
			clients := &Clients{Builds: builds.open, Comments: comments.read, Storage: store.open, StorageAs: store.openAs}
			tree := maps.Clone(tt.tree)
			if tree == nil {
				tree = map[string]string{}
			}
			if !tt.noPlacement {
				tree[placementPath] = tt.placement
				if tt.placement == "" {
					tree[placementPath] = seedingNone
				}
			}
			tree[tfvarsPath] = tt.tfvars
			if tt.tfvars == "" {
				tree[tfvarsPath] = declaringWidgetMode
			}
			req := &ResolveRequest{BuildID: "b-1", Project: "tst-project", Location: "us-central1", Known: known, Source: string(workspaceFiles(t, tree))}
			var out strings.Builder
			facts, err := Resolve(t.Context(), clients, req, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Resolve() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Resolve() error = %v; output:\n%s", err, out.String())
			}
			if diff := cmp.Diff(tt.want, summarize(facts)); diff != "" {
				t.Errorf("facts mismatch (-want +got):\n%s", diff)
			}
			if builds.got != "tst-project/us-central1/b-1" {
				t.Errorf("read build %q, want tst-project/us-central1/b-1", builds.got)
			}
			if tt.wantAsked != "" && (len(builds.asked) != 1 || builds.asked[0] != tt.wantAsked) {
				t.Errorf("minted for %v, want %q", builds.asked, tt.wantAsked)
			}
			if tt.wantCalled != "" && comments.called != tt.wantCalled {
				t.Errorf("comments read with %q, want %q", comments.called, tt.wantCalled)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			for _, unwanted := range tt.wantNotOut {
				if strings.Contains(out.String(), unwanted) {
					t.Errorf("output carries %q:\n%s", unwanted, out.String())
				}
			}
		})
	}
}

func TestResolveRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     ResolveRequest
		wantErr string
	}{
		{name: "the build's id", req: ResolveRequest{Project: "p", Location: "l"}, wantErr: "BUILD_ID is not set"},
		{name: "the project", req: ResolveRequest{BuildID: "b", Location: "l"}, wantErr: "PROJECT_ID is not set"},
		{name: "the location", req: ResolveRequest{BuildID: "b", Project: "p"}, wantErr: "LOCATION is not set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builds := &fakeBuilds{build: buildFor(t, tagBuild(nil)), token: "tok"}
			_, err := Resolve(t.Context(), &Clients{Builds: builds.open, Comments: (&fakeComments{}).read}, &tt.req, &strings.Builder{})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Resolve() error = %v, wantErr %q", err, tt.wantErr)
			}
			if builds.got != "" {
				t.Errorf("the build was read (%s) before the request was checked", builds.got)
			}
		})
	}
}

// TestFactsWrite proves the workspace files: the environment file reads back through
// Workspace.Environment and through a real shell, the build arguments file is the array
// the image build sources, and the build file is the build as the API answered it.
func TestFactsWrite(t *testing.T) {
	t.Parallel()

	const npmContainer = "projects/p/secrets/imp-tst-gbl-harbor-npm-token"
	tests := []struct {
		name     string
		subs     map[string]string
		comments []string
		// tfvars is the checkout's terraform.tfvars, placement its placement (seedingTst
		// when empty).
		tfvars    string
		placement string
		wantEnv   map[string]string
		// wantAbsent are substitutions the environment file leaves out.
		wantAbsent []string
		wantArgs   string
		// wantShell is what bash prints for the declared value, sourced from the environment file.
		wantShell string
	}{
		{
			name:      "a tag build with a declared substitution",
			subs:      tagBuild(nil),
			tfvars:    declaringWidgetMode,
			wantEnv:   map[string]string{"GITHUB_TOKEN": "tok", "SERVICES": "us-central1=harbor-app", sharedDBFact: "", "SKIP_DEPLOY": "", "IMAGE_TAG": "v1.2.3-tst", "COMMIT_TAG": "deadbeefcafe-tst", "RELEASE": "v1.2.3", runMigrationsFact: "true", "SHIFT_TRAFFIC": "true", seedFact: "true", "_ENV": "tst", "_MIGRATE_ENV": `{"APP_SERVICE_NAME":"harbor-migrate"}`, "_WIDGET_MODE": trickyValue},
			wantArgs:  "_WIDGET_MODE=" + trickyValue + "\n",
			wantShell: trickyValue + "\n",
		},
		{
			name:       "the checkout's declarations and pins are what the files carry: a changed value, a dropped declaration left out, a moved pin",
			subs:       tagBuild(map[string]string{"_STALE": "old", buildSecretsSub: "NPM_TOKEN=" + npmContainer + "/versions/3"}),
			tfvars:     stackTfvarsWith(`{ _WIDGET_MODE = "dawn" }`, `{ NPM_TOKEN = "4" }`),
			wantEnv:    map[string]string{"_WIDGET_MODE": "dawn", buildSecretsFact: "NPM_TOKEN=" + npmContainer + "/versions/4", buildSecretsSub: "NPM_TOKEN=" + npmContainer + "/versions/3", seedFact: "true"},
			wantAbsent: []string{"_STALE"},
			wantArgs:   "_WIDGET_MODE=dawn\n",
			wantShell:  "dawn\n",
		},
		{
			name:       "a tag build exports the build arguments the checkout declares as the trigger carries them, and leaves out one it no longer declares",
			subs:       tagBuild(map[string]string{"_BUILD_ARG_FIREBASE_API_KEY": "AIza'Tst", "_BUILD_ARG_PROJECT_ID": "tst-project", "_BUILD_ARG_OLD_ARG": "gone"}),
			tfvars:     declaringWidgetMode,
			placement:  declaringArguments,
			wantEnv:    map[string]string{"_BUILD_ARG_FIREBASE_API_KEY": "AIza'Tst", "_BUILD_ARG_PROJECT_ID": "tst-project"},
			wantAbsent: []string{"_BUILD_ARG_OLD_ARG"},
			wantArgs:   "_WIDGET_MODE=" + trickyValue + "\n",
			wantShell:  trickyValue + "\n",
		},
		{
			name:       "a pull-request build exports none of the trigger's build arguments",
			subs:       prBuild(map[string]string{"_WIDGET_MODE": "", "_BUILD_ARG_FIREBASE_API_KEY": "AIzaTst"}),
			tfvars:     stackTfvarsWith("{}", "{}"),
			placement:  declaringArguments,
			comments:   []string{"/gcbrun"},
			wantEnv:    map[string]string{prNumberSub: "7"},
			wantAbsent: []string{"_BUILD_ARG_FIREBASE_API_KEY"},
			wantArgs:   "",
			wantShell:  "\n",
		},
		{
			name:      "a pull request in shared mode",
			subs:      prBuild(map[string]string{"_WIDGET_MODE": ""}),
			tfvars:    stackTfvarsWith("{}", "{}"),
			comments:  []string{"/gcbrun shared-db"},
			wantEnv:   map[string]string{sharedDBFact: "true", "RELOAD_DB": "", "DOWN": "", runMigrationsFact: "false", seedFact: "true", "VERSION": "pr7@deadbee", prNumberSub: "7"},
			wantArgs:  "",
			wantShell: "\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builds := &fakeBuilds{build: buildFor(t, tt.subs), token: "tok"}
			comments := &fakeComments{bodies: tt.comments}
			placement := tt.placement
			if placement == "" {
				placement = seedingTst
			}
			source := workspaceFiles(t, map[string]string{placementPath: placement, tfvarsPath: tt.tfvars})
			facts, err := Resolve(t.Context(), &Clients{Builds: builds.open, Comments: comments.read}, &ResolveRequest{BuildID: "b-1", Project: "p", Location: "l", Known: known, Source: string(source)}, &strings.Builder{})
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			dir := t.TempDir()
			if err := facts.Write(Workspace(dir)); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			env, err := Workspace(dir).Environment()
			if err != nil {
				t.Fatalf("Environment() error = %v", err)
			}
			for name, want := range tt.wantEnv {
				if got, ok := env[name]; !ok || got != want {
					t.Errorf("%s = %q (present %t), want %q", name, got, ok, want)
				}
			}
			for _, name := range tt.wantAbsent {
				if got, ok := env[name]; ok {
					t.Errorf("%s = %q, want it left out", name, got)
				}
			}
			environment, err := os.ReadFile(filepath.Join(dir, EnvironmentFile))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(environment), "export GITHUB_TOKEN=\"tok\"\nexport SERVICES=") {
				t.Errorf("environment.sh starts with %q", strings.SplitN(string(environment), "\n", 3)[:2])
			}
			args, err := os.ReadFile(filepath.Join(dir, BuildArgsFile))
			if err != nil {
				t.Fatal(err)
			}
			if string(args) != tt.wantArgs {
				t.Errorf("%s = %q, want %q", BuildArgsFile, args, tt.wantArgs)
			}
			read, err := Workspace(dir).BuildArgs()
			if err != nil {
				t.Fatalf("BuildArgs() error = %v", err)
			}
			if got := strings.Join(read, "\n"); got != strings.TrimSuffix(tt.wantArgs, "\n") {
				t.Errorf("BuildArgs() = %q, want the file's lines %q", got, tt.wantArgs)
			}
			build, err := Workspace(dir).Build()
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			if build.ID != "b-1" || build.Substitutions["_ENV"] != "tst" {
				t.Errorf("build.json reads back as %+v", build)
			}
			if _, err := exec.LookPath("bash"); err != nil {
				t.Skip("no bash to source the files with")
			}
			shell := exec.CommandContext(t.Context(), "bash", "-c", `source ./environment.sh && printf '%s\n' "${_WIDGET_MODE:-}"`)
			shell.Dir = dir
			got, err := shell.CombinedOutput()
			if err != nil {
				t.Fatalf("bash: %v\n%s", err, got)
			}
			if string(got) != tt.wantShell {
				t.Errorf("bash read %q, want %q", got, tt.wantShell)
			}
		})
	}
}

func TestCloudBuild(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/projects/p/locations/l/builds/b-1":
			fmt.Fprint(w, `{"id":"b-1","substitutions":{"_ENV":"tst"}}`)
		case "POST /v2/projects/p/locations/l/connections/c/repositories/r:accessReadToken":
			fmt.Fprint(w, `{"token":"tok","expirationTime":"2026-09-27T06:00:00Z"}`)
		case "POST /v2/projects/p/locations/l/connections/c/repositories/empty:accessReadToken":
			fmt.Fprint(w, `{"token":""}`)
		case "POST /v2/projects/p/locations/l/connections/c/repositories/denied:accessReadToken":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":{"code":403,"message":"Permission denied on connection"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "no such build")
		}
	}))
	t.Cleanup(srv.Close)
	tests := []struct {
		name    string
		call    func(ctx context.Context, c Builds) (string, error)
		want    string
		wantErr string
	}{
		{
			name: "a build is read as the API answers it",
			call: func(ctx context.Context, c Builds) (string, error) {
				data, err := c.Get(ctx, "p", "l", "b-1")

				return string(data), err
			},
			want: `{"id":"b-1","substitutions":{"_ENV":"tst"}}`,
		},
		{
			name: "a token is minted for the repository",
			call: func(ctx context.Context, c Builds) (string, error) {
				return c.ReadToken(ctx, "projects/p/locations/l/connections/c/repositories/r")
			},
			want: "tok",
		},
		{
			name: "an empty token is refused",
			call: func(ctx context.Context, c Builds) (string, error) {
				return c.ReadToken(ctx, "projects/p/locations/l/connections/c/repositories/empty")
			},
			wantErr: "Cloud Build minted no token for projects/p/locations/l/connections/c/repositories/empty",
		},
		{
			name: "a refusal carries the status and the message",
			call: func(ctx context.Context, c Builds) (string, error) {
				return c.ReadToken(ctx, "projects/p/locations/l/connections/c/repositories/denied")
			},
			wantErr: "Cloud Build answered HTTP 403 to POST /v2/projects/p/locations/l/connections/c/repositories/denied:accessReadToken: Permission denied on connection",
		},
		{
			name: "a plain refusal carries its text",
			call: func(ctx context.Context, c Builds) (string, error) {
				data, err := c.Get(ctx, "p", "l", "b-2")

				return string(data), err
			},
			wantErr: "Cloud Build answered HTTP 404 to GET /v1/projects/p/locations/l/builds/b-2: no such build",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.call(t.Context(), &cloudBuild{http: srv.Client(), base: srv.URL})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGitHubComments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		repo    string
		wantErr string
	}{
		{name: "no slash", repo: "harbor", wantErr: `REPO_FULL_NAME "harbor" is not <organization>/<repository>`},
		{name: "no repository", repo: "impulseframework/", wantErr: `REPO_FULL_NAME "impulseframework/" is not <organization>/<repository>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := GitHubComments(t.Context(), "tok", tt.repo, 7)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("GitHubComments() error = %v, wantErr %q", err, tt.wantErr)
			}
		})
	}
}

// TestFactsApproval: a build that waited for its approval in Cloud Build names who approved
// it, when and with what comment, exported for the record; a build that needed none, or
// whose approval is not decided, names nobody.
func TestFactsApproval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		approval     string
		wantApprover string
		wantAt       string
		wantComment  string
	}{
		{
			name:         "an approved build names its approver, the moment and the comment",
			approval:     `{"state":"APPROVED","result":{"approverAccount":"approver@example.com","approvalTime":"2026-10-05T03:05:00Z","decision":"APPROVED","comment":"go"}}`,
			wantApprover: "approver@example.com",
			wantAt:       "2026-10-05T03:05:00Z",
			wantComment:  "go",
		},
		{name: "a build that needed no approval names nobody"},
		{name: "an approval not yet decided names nobody", approval: `{"state":"PENDING","config":{"approvalRequired":true}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subs, err := json.Marshal(tagBuild(nil))
			if err != nil {
				t.Fatal(err)
			}
			doc := `{"id":"b-1","substitutions":` + string(subs)
			if tt.approval != "" {
				doc += `,"approval":` + tt.approval
			}
			doc += "}"
			f, err := newFacts([]byte(doc))
			if err != nil {
				t.Fatalf("newFacts() error = %v", err)
			}
			if f.Approver != tt.wantApprover || f.ApprovedAt != tt.wantAt || f.ApprovalComment != tt.wantComment {
				t.Errorf("approval = %q at %q (%q), want %q at %q (%q)", f.Approver, f.ApprovedAt, f.ApprovalComment, tt.wantApprover, tt.wantAt, tt.wantComment)
			}
			for name, want := range map[string]string{"APPROVER": tt.wantApprover, "APPROVED_AT": tt.wantAt, "APPROVAL_COMMENT": tt.wantComment} {
				if line := "export " + name + "=" + strconv.Quote(want) + "\n"; !strings.Contains(f.environment(), line) {
					t.Errorf("environment() lacks %q", line)
				}
			}
			var out strings.Builder
			f.report(&out)
			if got, want := strings.Contains(out.String(), "Approved in Cloud Build by approver@example.com at 2026-10-05T03:05:00Z"), tt.wantApprover != ""; got != want {
				t.Errorf("report names the approver: %v, want %v:\n%s", got, want, out.String())
			}
		})
	}
}
