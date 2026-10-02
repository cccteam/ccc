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
		"_SERVICES": "us-central1=harbor-app", "_MIGRATE_JOB": "us-central1=harbor-migrate",
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
var known = []string{"_ENV", "_APP", "_PROJECT", "_REGISTRY", "_SERVICES", "_MIGRATE_JOB", "_REPO_CONNECTION_NAME", "_REPO_NAME", "_RECORDS_BUCKET", "_MIGRATIONS_DIR", "_SEED", "_RESTORE", "_REQUESTER", "_MIGRATE_ACTION", "_MIGRATE_TABLE", "_MIGRATE_VERSION", "_MIGRATE_LOGS"}

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
	Version, Release, Image, ImageTag, CommitTag, Comment, Token  string
	SharedDB, ReloadDB, Down, RunMigrations, ShiftTraffic, Notice bool
	ReloadReason, Restore, Requester, RestoreReason               string
	// Migration is the migration operation in words, empty for none.
	Migration string
	Declared  []string
}

func summarize(f *Facts) outcome {
	o := outcome{
		Version: f.Version, Release: f.Release, Image: f.Image, ImageTag: f.ImageTag, CommitTag: f.CommitTag, Comment: f.Comment, Token: f.Token,
		SharedDB: f.SharedDB, ReloadDB: f.ReloadDB, Down: f.Down, RunMigrations: f.RunMigrations, ShiftTraffic: f.ShiftTraffic, Notice: f.Notice != "",
		ReloadReason: f.ReloadReason, Restore: f.Restore, Requester: f.Requester, RestoreReason: f.RestoreReason, Declared: f.Declared,
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

// liveSeeded is tst's live record of v1.2.2 with the seed applied.
var liveSeeded = map[string]string{"gs://records/harbor/tst/v1.2.2/b-0.json": liveRecordWith("tst", "v1.2.2", "b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/migrations", Name: "000003_Sites.up.sql", Hash: hashOf(sitesContent)}, Migration{Dir: "schema/devseed", Name: "000001_Seed.up.sql", Hash: hashOf(seedContent)})}

func TestResolve(t *testing.T) {
	t.Parallel()

	const image = "us-central1-docker.pkg.dev/shr/reg/harbor"
	tag := outcome{Version: "v1.2.3", Release: "v1.2.3", Image: image, ImageTag: "v1.2.3-tst", CommitTag: "deadbeefcafe-tst", Token: "tok", RunMigrations: true, ShiftTraffic: true, Declared: []string{"_WIDGET_MODE"}}
	pr := outcome{Version: "pr7@deadbee", Release: "pr7-deadbee", Image: image, ImageTag: "pr7-deadbee-tst", CommitTag: "deadbeefcafe-tst", Token: "tok", RunMigrations: true, ShiftTraffic: true, Declared: []string{"_WIDGET_MODE"}}
	withComment := func(o outcome, comment string, set func(o *outcome)) outcome {
		o.Comment = comment
		set(&o)

		return o
	}
	tests := []struct {
		name     string
		subs     map[string]string
		comments []string
		// records are the records bucket's objects by gs:// path; tree the migration
		// files in the checkout.
		records map[string]string
		tree    map[string]string
		// commentsErr fails the comment read; mintErr fails the token.
		commentsErr error
		mintErr     error
		want        outcome
		wantOut     []string
		// wantAsked is the repository the token was minted for; wantCalled the comment read.
		wantAsked  string
		wantCalled string
		wantErr    string
	}{
		{
			name:      "a tag build names its release and the environment's tags",
			subs:      tagBuild(nil),
			want:      tag,
			wantOut:   []string{"Triggered by tag v1.2.3", "IMAGE=" + image + " IMAGE_TAG=v1.2.3-tst VERSION=v1.2.3 RELEASE=v1.2.3", "RUN_MIGRATIONS=true SHIFT_TRAFFIC=true REVISION_TAG=", "Declared substitutions for the hooks and the image build: _WIDGET_MODE"},
			wantAsked: "projects/tst-project/locations/us-central1/connections/imp-tst-github/repositories/harbor",
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
			name:    "production is never restored by a run",
			subs:    tagBuild(map[string]string{restoreSub: restoreEmpty, requesterSub: "octocat", "_ENV": "prd"}),
			wantErr: "_RESTORE=empty in prd: production is never restored by a run",
		},
		{
			name:    "production's backup goes into stg alone",
			subs:    tagBuild(map[string]string{restoreSub: restoreBackup, requesterSub: "octocat"}),
			wantErr: "_RESTORE=production-backup in tst: production's backup is restored into stg, the environment on production's instance; tst is restored to an empty database (_RESTORE=empty)",
		},
		{
			name:    "an unknown restore is refused",
			subs:    tagBuild(map[string]string{restoreSub: "yesterday", requesterSub: "octocat"}),
			wantErr: `unknown _RESTORE "yesterday" (the restores are empty and production-backup)`,
		},
		{
			name:    "a restore names who asked",
			subs:    tagBuild(map[string]string{restoreSub: restoreEmpty}),
			wantErr: "_RESTORE=empty names no requester (_REQUESTER): a restore says who asked for it",
		},
		{
			name:    "a requester without a restore or a migration operation is a mistake",
			subs:    tagBuild(map[string]string{requesterSub: "octocat"}),
			wantErr: "_REQUESTER names octocat but _RESTORE and _MIGRATE_ACTION are empty: a requester comes with a restore or a migration operation",
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
				o.Migration = "version: the migrate job prints the database's migration version and nothing else deploys, asked for by octocat"
			}),
			wantOut: []string{"Migration operation version: the migrate job prints the database's migration version and nothing else deploys, asked for by octocat."},
		},
		{
			name: "a rerun needs no requester",
			subs: tagBuild(map[string]string{migrateActionSub: actionRerun}),
			want: withComment(tag, "", func(o *outcome) {
				o.Migration = "rerun: the migrate job runs as it always does and the release continues"
			}),
			wantOut: []string{"Migration operation rerun: the migrate job runs as it always does and the release continues."},
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
			name:    "a hand-submitted build without a connection skips the GitHub checks",
			subs:    tagBuild(map[string]string{"_REPO_CONNECTION_NAME": "CONNECTION_NOT_AUTHORIZED_IN_2-ENV"}),
			want:    withComment(tag, "", func(o *outcome) { o.Token = ""; o.Notice = true }),
			wantOut: []string{"Notice: no Cloud Build connection in tst yet; the branch check and the pull-request comment are skipped (hand-submitted build)."},
		},
		{
			name:      "a build with nothing declared says so",
			subs:      tagBuild(map[string]string{"_WIDGET_MODE": ""}),
			want:      withComment(tag, "", func(o *outcome) { o.Declared = nil }),
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
			name:    "a seed file the environment's live release applied changed: the release restores the database so the seed applies from the start",
			subs:    seededTag(nil),
			records: liveSeeded,
			tree:    map[string]string{sitesUp: sitesContent, seedUp: seedContent + ", edited"},
			want: withComment(tag, "", func(o *outcome) {
				o.Restore, o.Requester, o.RestoreReason = restoreEmpty, "release v1.2.3", seedChangedReason
			}),
			wantOut: []string{"Restore run: tst's database is replaced (empty) before v1.2.3 deploys, " + seedChangedReason + "."},
		},
		{
			name:    "a seed file the live release applied is gone from the tree: the same case",
			subs:    seededTag(nil),
			records: liveSeeded,
			tree:    map[string]string{sitesUp: sitesContent},
			want: withComment(tag, "", func(o *outcome) {
				o.Restore, o.Requester, o.RestoreReason = restoreEmpty, "release v1.2.3", seedChangedReason
			}),
		},
		{
			name:    "the tree carries the seed as the live release applied it: nothing is restored",
			subs:    seededTag(nil),
			records: liveSeeded,
			tree:    map[string]string{sitesUp: sitesContent, seedUp: seedContent},
			want:    tag,
		},
		{
			name:    "a new seed file beside the applied ones is a data migration the migrate job applies: nothing is restored",
			subs:    seededTag(nil),
			records: liveSeeded,
			tree:    map[string]string{sitesUp: sitesContent, seedUp: seedContent, "schema/devseed/000002_More.up.sql": "insert b"},
			want:    tag,
		},
		{
			name:    "a changed schema migration is the hotfix check's concern, not the seed comparison's",
			subs:    seededTag(nil),
			records: liveSeeded,
			tree:    map[string]string{sitesUp: sitesContent + ", edited", seedUp: seedContent},
			want:    tag,
		},
		{
			name:    "an environment off the seed list never applied the seed: a changed seed is nothing to it",
			subs:    seededTag(map[string]string{seedSub: "false"}),
			records: liveSeeded,
			tree:    map[string]string{sitesUp: sitesContent, seedUp: seedContent + ", edited"},
			want:    tag,
		},
		{
			name:    "a restore asked for already replaces the database: the seed comparison yields to it",
			subs:    seededTag(map[string]string{restoreSub: restoreEmpty, requesterSub: "octocat"}),
			records: liveSeeded,
			tree:    map[string]string{sitesUp: sitesContent, seedUp: seedContent + ", edited"},
			want:    withComment(tag, "", func(o *outcome) { o.Restore, o.Requester = restoreEmpty, "octocat" }),
			wantOut: []string{"Restore run: tst's database is replaced (empty) before v1.2.3 deploys, asked for by octocat."},
		},
		{
			name:    "only a live record counts: a preview is a build whose traffic never shifted",
			subs:    seededTag(nil),
			records: map[string]string{"gs://records/harbor/tst/v1.2.2/b-0.json": recordWith("b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/devseed", Name: "000001_Seed.up.sql", Hash: hashOf(seedContent)})},
			tree:    map[string]string{sitesUp: sitesContent, seedUp: seedContent + ", edited"},
			want:    tag,
		},
		{
			name: "the newest live record decides: the seed as the latest release applied it",
			subs: seededTag(nil),
			records: map[string]string{
				"gs://records/harbor/tst/v1.2.1/b-9.json": liveRecordWith("tst", "v1.2.1", "b-9", "2026-09-26T05:00:00Z", Migration{Dir: "schema/devseed", Name: "000001_Seed.up.sql", Hash: hashOf("insert a, old")}),
				"gs://records/harbor/tst/v1.2.2/b-0.json": liveSeeded["gs://records/harbor/tst/v1.2.2/b-0.json"],
			},
			tree: map[string]string{sitesUp: sitesContent, seedUp: seedContent},
			want: tag,
		},
		{
			name: "a pull request's live record under the environment is its own environment's, not the release the environment runs",
			subs: seededTag(nil),
			records: map[string]string{
				"gs://records/harbor/tst/v1.2.2/b-0.json":      liveSeeded["gs://records/harbor/tst/v1.2.2/b-0.json"],
				"gs://records/harbor/tst/pr9-abc0123/b-8.json": liveRecordWith("tst", "pr9@abc0123", "b-8", "2026-09-28T05:00:00Z", Migration{Dir: "schema/devseed", Name: "000001_Seed.up.sql", Hash: hashOf("insert a, the pull request's")}),
			},
			tree: map[string]string{sitesUp: sitesContent, seedUp: seedContent},
			want: tag,
		},
		{
			name:    "production is never restored by a run: its seed is not compared",
			subs:    seededTag(map[string]string{"_ENV": "prd"}),
			records: map[string]string{"gs://records/harbor/prd/v1.2.2/b-0.json": liveRecordWith("prd", "v1.2.2", "b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/devseed", Name: "000001_Seed.up.sql", Hash: hashOf(seedContent)})},
			tree:    map[string]string{sitesUp: sitesContent, seedUp: seedContent + ", edited"},
			want:    withComment(tag, "", func(o *outcome) { o.ImageTag, o.CommitTag = "v1.2.3-prd", "deadbeefcafe-prd" }),
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
			name:    "a pull request needs the connection",
			subs:    prBuild(map[string]string{"_REPO_CONNECTION_NAME": "CONNECTION_NOT_AUTHORIZED_IN_2-ENV"}),
			wantErr: "a pull-request build needs the Cloud Build connection to read its /gcbrun comment",
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
			name:    "a build that names no migrate job is refused",
			subs:    tagBuild(map[string]string{"_MIGRATE_JOB": ""}),
			wantErr: "_SERVICES and _MIGRATE_JOB name the Cloud Run services and the migrate job this build updates; one is empty",
		},
		{
			name:    "a failed mint says which connection",
			subs:    tagBuild(nil),
			mintErr: errors.New("Cloud Build answered HTTP 403 to POST /v2/...: denied"),
			wantErr: "could not mint a GitHub token from connection imp-tst-github",
		},
		{
			name:    "a hand-submitted build with a connection names the repository",
			subs:    tagBuild(map[string]string{"REPO_FULL_NAME": ""}),
			wantErr: "REPO_FULL_NAME is not set; a hand-submitted build passes it as <organization>/<repository>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builds := &fakeBuilds{build: buildFor(t, tt.subs), token: "tok", mintErr: tt.mintErr}
			comments := &fakeComments{bodies: tt.comments, err: tt.commentsErr}
			store := &memoryStore{objects: tt.records}
			clients := &Clients{Builds: builds.open, Comments: comments.read, Storage: store.open}
			req := &ResolveRequest{BuildID: "b-1", Project: "tst-project", Location: "us-central1", Known: known, Source: string(workspaceFiles(t, tt.tree))}
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

	tests := []struct {
		name     string
		subs     map[string]string
		comments []string
		wantEnv  map[string]string
		wantArgs string
		// wantShell is what bash prints for the declared value, sourced from the environment file.
		wantShell string
	}{
		{
			name:      "a tag build with a declared substitution",
			subs:      tagBuild(nil),
			wantEnv:   map[string]string{"GITHUB_TOKEN": "tok", "SERVICES": "us-central1=harbor-app", "MIGRATE_JOB": "us-central1=harbor-migrate", sharedDBFact: "", "SKIP_DEPLOY": "", "IMAGE_TAG": "v1.2.3-tst", "COMMIT_TAG": "deadbeefcafe-tst", "RELEASE": "v1.2.3", runMigrationsFact: "true", "SHIFT_TRAFFIC": "true", "_ENV": "tst", "_WIDGET_MODE": trickyValue},
			wantArgs:  "_WIDGET_MODE=" + trickyValue + "\n",
			wantShell: trickyValue + "\n",
		},
		{
			name:      "a pull request in shared mode",
			subs:      prBuild(map[string]string{"_WIDGET_MODE": ""}),
			comments:  []string{"/gcbrun shared-db"},
			wantEnv:   map[string]string{sharedDBFact: "true", "RELOAD_DB": "", "DOWN": "", runMigrationsFact: "false", "VERSION": "pr7@deadbee", prNumberSub: "7"},
			wantArgs:  "",
			wantShell: "\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builds := &fakeBuilds{build: buildFor(t, tt.subs), token: "tok"}
			comments := &fakeComments{bodies: tt.comments}
			facts, err := Resolve(t.Context(), &Clients{Builds: builds.open, Comments: comments.read}, &ResolveRequest{BuildID: "b-1", Project: "p", Location: "l", Known: known}, &strings.Builder{})
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
