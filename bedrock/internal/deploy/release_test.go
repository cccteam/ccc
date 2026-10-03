package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// releaseBuild is a tag build's substitutions in stg, after tst; an override with an
// empty value removes the substitution.
func releaseBuild(t *testing.T, overrides map[string]string) string {
	t.Helper()

	subs := map[string]string{
		tagSub: "v1.2.3", commitSub: "c3", repoFullNameSub: "acme/quill", releaseActorsSub: "release-app[bot],other",
		defaultBranchSub: "master", appSub: "quill", envSub: stgEnvironment, previousEnvSub: tstEnvironment, previousRecordsSub: "tst-records",
		recordsBucket: "stg-records", migrationsSub: "schema/migrations",
	}
	for name, value := range overrides {
		if value == "" {
			delete(subs, name)

			continue
		}
		subs[name] = value
	}

	return buildFor(t, subs)
}

// The release file's place in the release tests' checkout.
const (
	routerDir       = "pkg/router"
	releaseFilePath = routerDir + "/zz_gen_release.json"
)

// seededPlacement is the test placement with the seed list given (a JSON list of
// environments).
func seededPlacement(seed string) string {
	return strings.TrimSuffix(testPlacement(""), "}\n") + `, "seed": ` + seed + "}\n"
}

// liveIn is a live record of version in env.
func liveIn(env, version string) string {
	return `{"app": "quill", "env": "` + env + `", "version": "` + version + `", "status": "live", "timestamp": "2026-09-28T05:30:00Z", "build": "b-5"}`
}

// mergeObjects is the union of the records.
func mergeObjects(sets ...map[string]string) map[string]string {
	merged := map[string]string{}
	for _, set := range sets {
		for path, content := range set {
			merged[path] = content
		}
	}

	return merged
}

// migrationFile is a migration file as a record lists it and a checkout carries it.
type migrationFile struct {
	dir, name, content string
}

// path is the file's root-relative path.
func (m migrationFile) path() string {
	return m.dir + "/" + m.name
}

// hash is the content's hash as a record carries it.
func (m migrationFile) hash() string {
	sum := sha256.Sum256([]byte(m.content))

	return hex.EncodeToString(sum[:8])
}

// quillRepo is the repository the release checks read: master at c4 over c3, c2, c1;
// v1.2.3 released at c3 by the release app; hotfix/1.2.x at h1, branched from c3.
func quillRepo() *githubtest.Repo {
	return &githubtest.Repo{
		Refs: map[string]github.Object{
			"refs/heads/master":       {Type: "commit", SHA: "c4"},
			"refs/heads/hotfix/1.2.x": {Type: "commit", SHA: "h1"},
			"refs/tags/v1.2.3":        {Type: "commit", SHA: "c3"},
			"refs/tags/v1.2.4":        {Type: "commit", SHA: "h1"},
			"refs/tags/v1.3.0-rc1":    {Type: "commit", SHA: "h1"},
		},
		Ancestry: map[string][]string{
			"c4": {"c4", "c3", "c2", "c1"}, "c3": {"c3", "c2", "c1"}, "h1": {"h1", "c3", "c2", "c1"}, "h2": {"h2", "h1", "c3", "c2", "c1"},
		},
		MergeBase: map[string]string{"c4 h1": "c3", "c4 h2": "c3"},
		Releases: map[string]github.Release{
			"v1.2.3":     {TagName: "v1.2.3", Author: github.User{Login: "release-app[bot]"}},
			"v1.2.4":     {TagName: "v1.2.4", Author: github.User{Login: "release-app[bot]"}},
			"v1.3.0-rc1": {TagName: "v1.3.0-rc1", Author: github.User{Login: "release-app[bot]"}},
		},
	}
}

func TestValidateRelease(t *testing.T) {
	t.Parallel()

	const (
		connected   = "export GITHUB_TOKEN=\"test-token\"\nexport SKIP_DEPLOY=\"\"\n"
		liveRecord  = `{"app": "quill", "env": "tst", "version": "v1.2.3", "status": "live", "timestamp": "2026-09-27T05:30:00Z", "build": "b-0"}`
		previewOnly = `{"app": "quill", "env": "tst", "version": "v1.2.3", "status": "preview", "timestamp": "2026-09-27T05:00:00Z", "build": "b-9"}`
	)
	live := map[string]string{"gs://tst-records/quill/tst/v1.2.3/b-0.json": liveRecord, "gs://tst-records/quill/tst/v1.2.3/b-9.json": previewOnly}
	// hotfixLive is the hotfix v1.2.4 live in tst, for the gate; first and refits are two
	// migration files the environment's records may list, as the build may carry them.
	hotfixLive := strings.Replace(liveRecord, "v1.2.3", "v1.2.4", 1)
	first := migrationFile{dir: "schema/migrations", name: "000001_Init.up.sql", content: "create table t"}
	refits := migrationFile{dir: "schema/migrations", name: "000002_Refits.up.sql", content: "alter table t"}
	refitsDown := migrationFile{dir: "schema/migrations", name: "000002_Refits.down.sql", content: "alter table t drop"}
	// seed is a seed file a seeded environment's record lists beside the schema files.
	seed := migrationFile{dir: "schema/devseed", name: "000001_Seed.up.sql", content: "insert a"}
	// stgLive is stg's live record of a release, listing the files its build applied;
	// tstLive is the same record in tst.
	stgLive := func(version string, applied ...migrationFile) string {
		var list []string
		for _, m := range applied {
			list = append(list, `{"dir": "`+m.dir+`", "name": "`+m.name+`", "hash": "`+m.hash()+`"}`)
		}

		return `{"app": "quill", "env": "stg", "version": "` + version + `", "status": "live", "timestamp": "2026-09-28T05:30:00Z", "build": "b-5", "migrations": [` + strings.Join(list, ", ") + `]}`
	}
	tstLive := func(version string, applied ...migrationFile) string {
		return strings.Replace(stgLive(version, applied...), `"env": "stg"`, `"env": "tst"`, 1)
	}
	// seededPR is a pull request against hotfix/1.2.x previewed in tst alone.
	seededPR := map[string]string{
		tagSub: "", prNumberSub: "7", baseBranchSub: "hotfix/1.2.x", environmentsSub: "tst",
		planIdentitiesSub: "tst=plan-tst@x.iam", recordsBucketsSub: "tst=tst-records",
	}
	tests := []struct {
		name string
		env  string
		subs map[string]string
		// repo changes the repository; objects the records (the previous environment's,
		// and this environment's for a hotfix); files the migration files the checkout
		// carries.
		repo    func(r *githubtest.Repo)
		objects map[string]string
		files   map[string]string
		// bedrock is the running bedrock's version; empty, a release.
		bedrock string
		// routerDir is the router package the release file is read from; placement the
		// checkout's placement (the default has no maintenance setting); now the clock
		// (the default is a Monday morning in October, Chicago time).
		routerDir  string
		placement  string
		now        time.Time
		wantOut    []string
		wantAbsent []string
		// wantFact is WINDOW_RELEASE after the step: true when the notes carry a
		// breaking-changes section, else empty. wantWindow are the window facts the step
		// leaves, by name, checked when set.
		wantFact   string
		wantWindow map[string]string
		// wantIdentities are the identities the records were read as, in order.
		wantIdentities []string
		wantErr        string
	}{
		{
			name: "a pull request against a hotfix line previews each environment's answer, read as its plan identity",
			env:  connected,
			subs: map[string]string{
				tagSub: "", prNumberSub: "7", baseBranchSub: "hotfix/1.2.x", environmentsSub: "tst,stg,prd",
				planIdentitiesSub: "tst=plan-tst@x.iam,stg=plan-stg@x.iam,prd=plan-prd@x.iam",
				recordsBucketsSub: "tst=tst-records,stg=stg-records,prd=prd-records",
			},
			objects: map[string]string{
				"gs://tst-records/quill/tst/v1.2.4/b-1.json": hotfixLive,
				"gs://stg-records/quill/stg/v1.3.0/b-5.json": stgLive("v1.3.0", first, refits),
				"gs://prd-records/quill/prd/v1.2.3/b-7.json": strings.Replace(stgLive("v1.2.3", first), `"env": "stg"`, `"env": "prd"`, 1),
			},
			files: map[string]string{first.path(): first.content},
			wantOut: []string{
				"Hotfix preview: this pull request is against hotfix/1.2.x, and each environment's release check will say this to the line's next release",
				"tst: would take the hotfix; its database holds nothing this pull request does not carry (0 file(s) recorded by v1.2.4, build b-0).",
				"stg: WILL REFUSE the hotfix: stg's database holds schema/migrations/000002_Refits.up.sql (in v1.3.0's record), which this pull request does not carry; restore stg to the hotfix first: a restore run replaces the database and skips this check.",
				"prd: would take the hotfix; its database holds nothing this pull request does not carry (1 file(s) recorded by v1.2.3, build b-5).",
				"Pull-request build: no release to validate.",
			},
			// The hotfix preview and the window preview each read every environment's record.
			wantIdentities: []string{"plan-tst@x.iam", "plan-stg@x.iam", "plan-prd@x.iam", "plan-tst@x.iam", "plan-stg@x.iam", "plan-prd@x.iam"},
		},
		{
			name: "a pull request previews the window in every environment, read as its plan identity",
			env:  connected,
			subs: map[string]string{
				tagSub: "", prNumberSub: "7", baseBranchSub: "master", environmentsSub: "tst,stg,prd",
				planIdentitiesSub: "tst=plan-tst@x.iam,stg=plan-stg@x.iam,prd=plan-prd@x.iam",
				recordsBucketsSub: "tst=tst-records,stg=stg-records,prd=prd-records",
			},
			routerDir: routerDir,
			placement: testPlacement(`{"stg": {"timeZone": "America/Chicago", "weekly": [{"day": "Sunday", "from": "02:00", "to": "04:00"}]}}`),
			objects: map[string]string{
				"gs://tst-records/quill/tst/v1.4.0/b-1.json": liveIn("tst", "v1.4.0"),
				"gs://stg-records/quill/stg/v1.5.0/b-5.json": liveIn("stg", "v1.5.0"),
				"gs://prd-records/quill/prd/v1.4.0/b-7.json": liveIn("prd", "v1.4.0"),
			},
			files: map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "1.5.0"}, "api": {"kind": "api-key"}}`)},
			wantOut: []string{
				"Window preview: whether the release this pull request becomes part of turns away the release each environment runs",
				"tst: breaking (the default outlet answers 1.5.0 at the oldest, and tst runs v1.4.0, which it turns away); tst takes it at any time, behind the maintenance page.",
				"stg: not breaking (the default outlet answers 1.5.0 at the oldest, which stg's v1.5.0 is not older than); the release deploys at any time.",
				"prd: breaking (the default outlet answers 1.5.0 at the oldest, and prd runs v1.4.0, which it turns away); prd has no maintenance setting, so the release WILL BE REFUSED at the start of its run there until placement.json names one (\"anytime\" is a setting).",
				"Pull-request build: no release to validate.",
			},
			wantIdentities: []string{"plan-tst@x.iam", "plan-stg@x.iam", "plan-prd@x.iam"},
		},
		{
			name: "a pull request's preview says where every release waits for the window and where it is held with the maintenance page",
			env:  connected,
			subs: map[string]string{
				tagSub: "", prNumberSub: "7", baseBranchSub: "master", environmentsSub: "stg,prd",
				planIdentitiesSub: "stg=plan-stg@x.iam,prd=plan-prd@x.iam", recordsBucketsSub: "stg=stg-records,prd=prd-records",
			},
			routerDir: routerDir,
			placement: testPlacement(`{"stg": {"timeZone": "America/Chicago", "weekly": [{"day": "Sunday", "from": "02:00", "to": "04:00"}], "releases": "all"}, "prd": {"timeZone": "America/Chicago", "weekly": [{"day": "Sunday", "from": "02:00", "to": "04:00"}]}}`),
			objects: map[string]string{
				"gs://stg-records/quill/stg/v1.5.0/b-5.json": liveIn("stg", "v1.5.0"),
				"gs://prd-records/quill/prd/v1.4.0/b-7.json": liveIn("prd", "v1.4.0"),
			},
			files: map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "1.5.0"}}`)},
			wantOut: []string{
				"stg: not breaking (the default outlet answers 1.5.0 at the oldest, which stg's v1.5.0 is not older than; every release waits for stg's window (releases: all)); the run waits for stg's window (Sunday 02:00 to 04:00 America/Chicago, every release) and deploys the rolling way.",
				"prd: breaking (the default outlet answers 1.5.0 at the oldest, and prd runs v1.4.0, which it turns away); the run waits for prd's window (Sunday 02:00 to 04:00 America/Chicago) and deploys behind the maintenance page.",
			},
		},
		{
			name:      "a breaking release into an environment that takes one at any time passes the gate open",
			env:       connected,
			routerDir: routerDir,
			objects:   mergeObjects(live, map[string]string{"gs://stg-records/quill/stg/v1.2.2/b-3.json": liveIn("stg", "v1.2.2")}),
			files:     map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "1.2.3"}}`)},
			wantOut: []string{
				"Gate passed: v1.2.3 is live in tst",
				"Maintenance window: v1.2.3 is a breaking release for stg (the default outlet answers 1.2.3 at the oldest, and stg runs v1.2.2, which it turns away); the window is anytime.",
				"stg's window is open now (anytime): the run proceeds once the image is built and the jobs are made.",
			},
			wantWindow: map[string]string{windowNeededFact: trueValue, windowBreakingFact: trueValue, windowReasonFact: "the default outlet answers 1.2.3 at the oldest, and stg runs v1.2.2, which it turns away"},
		},
		{
			name:       "a release whose oldest answered release the environment runs already deploys at any time",
			env:        connected,
			routerDir:  routerDir,
			objects:    mergeObjects(live, map[string]string{"gs://stg-records/quill/stg/v1.2.2/b-3.json": liveIn("stg", "v1.2.2")}),
			files:      map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "1.2.2"}}`)},
			wantOut:    []string{"No maintenance window: the default outlet answers 1.2.2 at the oldest, which stg's v1.2.2 is not older than; v1.2.3 deploys at any time."},
			wantWindow: map[string]string{windowNeededFact: "", windowBreakingFact: ""},
		},
		{
			name:       "an environment that runs nothing live has nothing to turn away",
			env:        connected,
			routerDir:  routerDir,
			objects:    live,
			files:      map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "this"}}`)},
			wantOut:    []string{"No maintenance window: stg runs no release live, so there is nothing for the release to turn away; v1.2.3 deploys at any time."},
			wantWindow: map[string]string{windowNeededFact: "", windowBreakingFact: ""},
		},
		{
			name:       "an outlet that answers its own release alone makes the release breaking",
			env:        connected,
			routerDir:  routerDir,
			objects:    mergeObjects(live, map[string]string{"gs://stg-records/quill/stg/v1.2.2/b-3.json": liveIn("stg", "v1.2.2")}),
			files:      map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": ""}, "portal": {"oldestAnswered": "this"}}`)},
			wantOut:    []string{"Maintenance window: v1.2.3 is a breaking release for stg (the portal outlet answers this release alone (oldest answered: this), and stg runs v1.2.2, which it turns away); the window is anytime."},
			wantWindow: map[string]string{windowNeededFact: trueValue, windowBreakingFact: trueValue},
		},
		{
			name:      "a skipped step is breaking: production on an older release than the oldest answered one",
			env:       "export GITHUB_TOKEN=\"\"\nexport SKIP_DEPLOY=\"\"\n",
			subs:      map[string]string{tagSub: "v1.6.0", envSub: prdEnvironment, previousEnvSub: stgEnvironment, previousRecordsSub: "stg-records", recordsBucket: "prd-records"},
			routerDir: routerDir,
			placement: testPlacement(`{"prd": "anytime"}`),
			objects:   map[string]string{"gs://stg-records/quill/stg/v1.6.0/b-5.json": liveIn("stg", "v1.6.0"), "gs://prd-records/quill/prd/v1.4.0/b-7.json": liveIn("prd", "v1.4.0")},
			files:     map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "1.5.0"}}`)},
			wantOut:   []string{"Maintenance window: v1.6.0 is a breaking release for prd (the default outlet answers 1.5.0 at the oldest, and prd runs v1.4.0, which it turns away); the window is anytime."},
		},
		{
			name:      "the same release once production runs the oldest answered one deploys at any time",
			env:       "export GITHUB_TOKEN=\"\"\nexport SKIP_DEPLOY=\"\"\n",
			subs:      map[string]string{tagSub: "v1.6.0", envSub: prdEnvironment, previousEnvSub: stgEnvironment, previousRecordsSub: "stg-records", recordsBucket: "prd-records"},
			routerDir: routerDir,
			objects:   map[string]string{"gs://stg-records/quill/stg/v1.6.0/b-5.json": liveIn("stg", "v1.6.0"), "gs://prd-records/quill/prd/v1.5.0/b-8.json": liveIn("prd", "v1.5.0")},
			files:     map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "1.5.0"}}`)},
			wantOut:   []string{"No maintenance window: the default outlet answers 1.5.0 at the oldest, which prd's v1.5.0 is not older than; v1.6.0 deploys at any time."},
		},
		{
			name:      "production without a maintenance setting refuses a breaking release at the start of the run",
			env:       "export GITHUB_TOKEN=\"\"\nexport SKIP_DEPLOY=\"\"\n",
			subs:      map[string]string{tagSub: "v1.6.0", envSub: prdEnvironment, previousEnvSub: stgEnvironment, previousRecordsSub: "stg-records", recordsBucket: "prd-records"},
			routerDir: routerDir,
			objects:   map[string]string{"gs://stg-records/quill/stg/v1.6.0/b-5.json": liveIn("stg", "v1.6.0"), "gs://prd-records/quill/prd/v1.4.0/b-7.json": liveIn("prd", "v1.4.0")},
			files:     map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "1.5.0"}}`)},
			wantErr:   `Build REJECTED: prd has no maintenance setting in infrastructure/placement.json and v1.6.0 is a breaking release (the default outlet answers 1.5.0 at the oldest, and prd runs v1.4.0, which it turns away). Write "maintenance": {"prd": "anytime"} for a release at any time, or the client's windows, and release again.`,
		},
		{
			name:      "under releases all an ordinary release waits for the window too, without maintenance mode",
			env:       connected,
			routerDir: routerDir,
			placement: testPlacement(`{"stg": {"timeZone": "America/Chicago", "weekly": [{"day": "Sunday", "from": "02:00", "to": "04:00"}], "releases": "all"}}`),
			now:       chicago(10, 4, 2, 30),
			objects:   mergeObjects(live, map[string]string{"gs://stg-records/quill/stg/v1.2.2/b-3.json": liveIn("stg", "v1.2.2")}),
			files:     map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "1.2.2"}}`)},
			wantOut: []string{
				"Maintenance window: v1.2.3 is a release for stg (the default outlet answers 1.2.2 at the oldest, which stg's v1.2.2 is not older than; every release waits for stg's window (releases: all)); the window is Sunday 02:00 to 04:00 America/Chicago, every release.",
				"stg's window is open now (Sunday 02:00 to 04:00 America/Chicago, until Sunday 2026-10-04 04:00 CDT)",
			},
			wantWindow: map[string]string{windowNeededFact: trueValue, windowBreakingFact: ""},
		},
		{
			name:      "a window that opens within the build's time makes the run build first and wait",
			env:       connected,
			routerDir: routerDir,
			placement: testPlacement(`{"stg": {"timeZone": "America/Chicago", "weekly": [{"day": "Sunday", "from": "02:00", "to": "04:00"}]}}`),
			now:       chicago(10, 3, 20, 0),
			objects:   mergeObjects(live, map[string]string{"gs://stg-records/quill/stg/v1.2.2/b-3.json": liveIn("stg", "v1.2.2")}),
			files:     map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "1.2.3"}}`)},
			wantOut:   []string{"stg's window next opens Sunday 2026-10-04 02:00 CDT (in 6h, Sunday 02:00 to 04:00 America/Chicago): the image is built and the jobs are made now, and the run waits for the window before maintenance begins."},
		},
		{
			name:      "a window further away than the run can wait is refused at the start, naming the opening",
			env:       connected,
			routerDir: routerDir,
			placement: testPlacement(`{"stg": {"timeZone": "America/Chicago", "weekly": [{"day": "Sunday", "from": "02:00", "to": "04:00"}]}}`),
			now:       chicago(10, 5, 10, 0),
			objects:   mergeObjects(live, map[string]string{"gs://stg-records/quill/stg/v1.2.2/b-3.json": liveIn("stg", "v1.2.2")}),
			files:     map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "1.2.3"}}`)},
			wantErr:   "Build REJECTED: stg's maintenance window next opens Sunday 2026-10-11 02:00 CDT (in 136h, Sunday 02:00 to 04:00 America/Chicago), further away than this run can wait (until Tuesday 2026-10-06 07:00 CDT, the build's timeout less 3h0m0s for the steps after the window): start the release on the day of the window.",
		},
		{
			name:      "a dated slot lets a release in on its date",
			env:       connected,
			routerDir: routerDir,
			placement: testPlacement(`{"stg": {"timeZone": "America/Chicago", "dates": [{"on": "2026-11-15", "from": "22:00", "to": "23:30"}]}}`),
			now:       chicago(11, 15, 22, 10),
			objects:   mergeObjects(live, map[string]string{"gs://stg-records/quill/stg/v1.2.2/b-3.json": liveIn("stg", "v1.2.2")}),
			files:     map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "1.2.3"}}`)},
			wantOut:   []string{"stg's window is open now (the dated slot 2026-11-15 22:00 to 23:30 America/Chicago, until Sunday 2026-11-15 23:30 CST)"},
		},
		{
			name:      "a window with no opening ahead is refused",
			env:       connected,
			routerDir: routerDir,
			placement: testPlacement(`{"stg": {"timeZone": "America/Chicago", "dates": [{"on": "2026-09-15", "from": "22:00", "to": "23:30"}]}}`),
			objects:   mergeObjects(live, map[string]string{"gs://stg-records/quill/stg/v1.2.2/b-3.json": liveIn("stg", "v1.2.2")}),
			files:     map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "1.2.3"}}`)},
			wantErr:   "Build REJECTED: stg's maintenance window has no opening ahead (its dated slots have passed and it has no weekly slot): write the next slot in infrastructure/placement.json and release again.",
		},
		{
			name:      "a restore run passes the gate whatever the window says",
			env:       connected + "export RESTORE=\"empty\"\n",
			routerDir: routerDir,
			placement: testPlacement(`{"stg": {"timeZone": "America/Chicago", "weekly": [{"day": "Sunday", "from": "02:00", "to": "04:00"}]}}`),
			objects:   mergeObjects(live, map[string]string{"gs://stg-records/quill/stg/v1.2.2/b-3.json": liveIn("stg", "v1.2.2")}),
			files:     map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "1.2.3"}}`)},
			wantOut:   []string{"The gate is open to a restore run: stg's database is replaced behind the maintenance page whatever the window says."},
		},
		{
			name:      "an environment in maintenance from an earlier run passes the gate with the window closed",
			env:       connected,
			subs:      map[string]string{servicesSub: "us-central1=quill-app", projectSub: "stg-project"},
			routerDir: routerDir,
			placement: testPlacement(`{"stg": {"timeZone": "America/Chicago", "weekly": [{"day": "Sunday", "from": "02:00", "to": "04:00"}]}}`),
			objects:   mergeObjects(live, map[string]string{"gs://stg-records/quill/stg/v1.2.2/b-3.json": liveIn("stg", "v1.2.2")}),
			files:     map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "1.2.3"}}`)},
			wantOut:   []string{"stg is in maintenance from an earlier run (APP_MAINTENANCE=1 on quill-app): the gate counts the window as open, so the rerun proceeds without waiting for the next one."},
		},
		{
			name:       "without a release file no release is breaking, and the log says so",
			env:        connected,
			routerDir:  routerDir,
			objects:    mergeObjects(live, map[string]string{"gs://stg-records/quill/stg/v1.2.2/b-3.json": liveIn("stg", "v1.2.2")}),
			wantOut:    []string{"Warning: no release file at pkg/router/zz_gen_release.json in the checkout, so no outlet declares an oldest answered release and no release is breaking; the resource generator writes it beside the router.", "No maintenance window: no outlet declares an oldest answered release, so the release turns no running release away; v1.2.3 deploys at any time."},
			wantWindow: map[string]string{windowNeededFact: "", windowBreakingFact: ""},
		},
		{
			name:      "a release file that does not read stops the run",
			env:       connected,
			routerDir: routerDir,
			objects:   live,
			files:     map[string]string{releaseFilePath: releaseFile(`{"default": {"oldestAnswered": "soon"}}`)},
			wantErr:   `outlet default answers "soon" at the oldest, which is not a release (1.5.0), "this" or ""`,
		},
		{
			name: "a pull request against a hotfix line of another release line is told production will refuse it at the door",
			env:  connected,
			subs: map[string]string{
				tagSub: "", prNumberSub: "7", baseBranchSub: "hotfix/1.1.x", environmentsSub: "prd",
				planIdentitiesSub: "prd=plan-prd@x.iam", recordsBucketsSub: "prd=prd-records",
			},
			objects: map[string]string{"gs://prd-records/quill/prd/v1.2.3/b-7.json": strings.Replace(stgLive("v1.2.3"), `"env": "stg"`, `"env": "prd"`, 1)},
			wantOut: []string{"prd: WILL REFUSE the hotfix at production's door: production runs v1.2.3, line 1.2; this hotfix is on hotfix/1.1.x. A hotfix is based on the release production runs."},
		},
		{
			name: "production holding files the line lacks is still answered at the door first, never with a restore",
			env:  connected,
			subs: map[string]string{
				tagSub: "", prNumberSub: "7", baseBranchSub: "hotfix/1.2.x", environmentsSub: "prd",
				planIdentitiesSub: "prd=plan-prd@x.iam", recordsBucketsSub: "prd=prd-records",
			},
			objects:    map[string]string{"gs://prd-records/quill/prd/v1.3.0/b-7.json": strings.Replace(stgLive("v1.3.0", first, refits), `"env": "stg"`, `"env": "prd"`, 1)},
			files:      map[string]string{first.path(): first.content},
			wantOut:    []string{"prd: WILL REFUSE the hotfix at production's door: production runs v1.3.0, line 1.3; this hotfix is on hotfix/1.2.x. A hotfix is based on the release production runs."},
			wantAbsent: []string{"restore prd"},
		},
		{
			name: "a pull request behind production on its own line is told to start the line from production's release",
			env:  connected,
			subs: map[string]string{
				tagSub: "", prNumberSub: "7", baseBranchSub: "hotfix/1.2.x", environmentsSub: "prd",
				planIdentitiesSub: "prd=plan-prd@x.iam", recordsBucketsSub: "prd=prd-records",
			},
			objects: map[string]string{"gs://prd-records/quill/prd/v1.2.5/b-7.json": strings.Replace(stgLive("v1.2.5", first, refits), `"env": "stg"`, `"env": "prd"`, 1)},
			files:   map[string]string{first.path(): first.content},
			wantOut: []string{"prd: WILL REFUSE the hotfix: prd's database holds schema/migrations/000002_Refits.up.sql (in v1.2.5's record), which this pull request does not carry; production is never restored by a run: a hotfix is based on the release production runs, so start the line from v1.2.5."},
		},
		{
			name:      "a seeded environment whose seed is not in the tree as applied is told the release's build restores it itself and takes the hotfix, before any schema file is compared",
			env:       connected,
			subs:      seededPR,
			placement: seededPlacement(`["tst"]`),
			objects:   map[string]string{"gs://tst-records/quill/tst/v1.3.0/b-5.json": tstLive("v1.3.0", first, refits, seed)},
			files:     map[string]string{first.path(): first.content, seed.path(): seed.content + ", edited"},
			wantOut: []string{
				"tst: would take the hotfix, after the restore the release's build decides itself: the seed changed since v1.3.0 applied it (build b-5): schema/devseed/000001_Seed.up.sql, not in the tree as applied (edited, renumbered or removed since), so the database is recreated and the migrations and the seed apply from the start, and what the database holds is not compared with the hotfix.",
			},
			wantAbsent: []string{"WILL REFUSE", "000002_Refits"},
		},
		{
			name:       "a seeded environment whose seed matches the tree is answered on its schema files: refused on one the pull request does not carry",
			env:        connected,
			subs:       seededPR,
			placement:  seededPlacement(`["tst"]`),
			objects:    map[string]string{"gs://tst-records/quill/tst/v1.3.0/b-5.json": tstLive("v1.3.0", first, refits, seed)},
			files:      map[string]string{first.path(): first.content, seed.path(): seed.content},
			wantOut:    []string{"tst: WILL REFUSE the hotfix: tst's database holds schema/migrations/000002_Refits.up.sql (in v1.3.0's record), which this pull request does not carry; restore tst to the hotfix first: a restore run replaces the database and skips this check."},
			wantAbsent: []string{"the restore the release's build decides itself"},
		},
		{
			name:      "a seeded environment whose seed matches the tree and whose schema files are all in it would take the hotfix",
			env:       connected,
			subs:      seededPR,
			placement: seededPlacement(`["tst"]`),
			objects:   map[string]string{"gs://tst-records/quill/tst/v1.3.0/b-5.json": tstLive("v1.3.0", first, seed)},
			files:     map[string]string{first.path(): first.content, seed.path(): seed.content},
			wantOut:   []string{"tst: would take the hotfix; its database holds nothing this pull request does not carry (2 file(s) recorded by v1.3.0, build b-5)."},
		},
		{
			name:      "an environment off the seed list holding a changed seed file is answered as before: refused on the file",
			env:       connected,
			subs:      seededPR,
			placement: seededPlacement(`["stg"]`),
			objects:   map[string]string{"gs://tst-records/quill/tst/v1.3.0/b-5.json": tstLive("v1.3.0", first, seed)},
			files:     map[string]string{first.path(): first.content, seed.path(): seed.content + ", edited"},
			wantOut:   []string{"tst: WILL REFUSE the hotfix: tst's database holds schema/devseed/000001_Seed.up.sql with other content than this pull request carries (by v1.3.0's record); restore tst to the hotfix first: a restore run replaces the database and skips this check."},
		},
		{
			name: "a pull request against a hotfix line whose environment's records cannot be read is told so and goes on",
			env:  connected,
			subs: map[string]string{
				tagSub: "", prNumberSub: "7", baseBranchSub: "hotfix/1.2.x", environmentsSub: "tst,stg",
				planIdentitiesSub: "tst=plan-tst@x.iam", recordsBucketsSub: "tst=tst-records,stg=stg-records",
			},
			wantOut: []string{"tst: no live deployment record; nothing to be behind.", "stg: its records bucket or plan identity is not named (_RECORDS_BUCKETS, _PLAN_IDENTITIES); nothing read.", "Pull-request build: no release to validate."},
		},
		{
			name:       "a pull-request build has no release to validate",
			env:        connected,
			subs:       map[string]string{tagSub: ""},
			bedrock:    "v0.0.0-lab.1.0.20260928222237-58b211dce544",
			wantOut:    []string{"Pull-request build: no release to validate."},
			wantAbsent: []string{"This tag build runs bedrock"},
		},
		{
			name:    "a tag build run by a commit pin says so and is not refused for it",
			env:     connected,
			objects: live,
			bedrock: "v0.0.0-lab.1.0.20260928222237-58b211dce544",
			wantOut: []string{
				"This tag build runs bedrock v0.0.0-lab.1.0.20260928222237-58b211dce544, a commit pin: bedrock built from an unreleased commit (placement.json bedrockVersion).",
				"Gate passed: v1.2.3 is live in tst",
			},
		},
		{
			name:       "a tag build run by a release names it",
			env:        connected,
			objects:    live,
			wantOut:    []string{"This tag build runs bedrock v0.4.0.\n", "Gate passed: v1.2.3 is live in tst"},
			wantAbsent: []string{"a commit pin"},
		},
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			wantOut: []string{tornDown},
		},
		{
			name: "a release whose notes carry a breaking-changes section is a window release",
			env:  connected,
			repo: func(r *githubtest.Repo) {
				r.Releases["v1.2.3"] = github.Release{TagName: "v1.2.3", Author: github.User{Login: "release-app[bot]"}, Body: "## [1.2.3](https://example.test) (2026-10-01)\n\n### ⚠ BREAKING CHANGES\n\n* **storage:** the uploads bucket is replaced\n\n### Features\n\n* **storage:** replace the uploads bucket\n"}
			},
			objects:  live,
			wantOut:  []string{"Release v1.2.3 validated: cut by release-app[bot]", "Breaking changes: the release notes of v1.2.3 carry a breaking-changes section", "whether the release deploys inside the maintenance window is decided below from the release file's oldest answered release, not from the notes.", "Gate passed: v1.2.3 is live in tst"},
			wantFact: trueValue,
		},
		{
			name: "a release whose notes mention breaking changes in prose is not one",
			env:  connected,
			repo: func(r *githubtest.Repo) {
				r.Releases["v1.2.3"] = github.Release{TagName: "v1.2.3", Author: github.User{Login: "release-app[bot]"}, Body: "### Features\n\n* no breaking changes this time\n"}
			},
			objects:    live,
			wantAbsent: []string{"Breaking changes"},
		},
		{
			name:    "a release on the default branch, live in the previous environment",
			env:     connected,
			objects: live,
			wantOut: []string{"Release v1.2.3 validated: cut by release-app[bot]", "Tag v1.2.3 validated: its commit is on master", "Gate passed: v1.2.3 is live in tst (since 2026-09-27T05:30:00Z, build b-0)"},
		},
		{
			name: "the branch at the commit is identical",
			env:  connected,
			repo: func(r *githubtest.Repo) {
				r.Refs["refs/heads/master"] = github.Object{Type: "commit", SHA: "c3"}
			},
			objects: live,
			wantOut: []string{"Tag v1.2.3 validated: its commit is on master"},
		},
		{
			name:    "the first environment has no gate",
			env:     connected,
			subs:    map[string]string{previousEnvSub: "", previousRecordsSub: "", envSub: tstEnvironment},
			wantOut: []string{"Tag v1.2.3 validated: its commit is on master"},
		},
		{
			name:    "a hand-submitted build without a token skips the GitHub checks",
			env:     "export GITHUB_TOKEN=\"\"\nexport SKIP_DEPLOY=\"\"\n",
			objects: live,
			wantOut: []string{"Gate passed: v1.2.3 is live in tst"},
		},
		{
			name: "a tag without a release is refused",
			env:  connected,
			repo: func(r *githubtest.Repo) {
				delete(r.Releases, "v1.2.3")
			},
			wantErr: "Build REJECTED: tag v1.2.3 has no GitHub Release (HTTP 404); a release is cut by release-please as the release app, never by a tag alone.",
		},
		{
			name: "a release by someone else is refused",
			env:  connected,
			repo: func(r *githubtest.Repo) {
				r.Releases["v1.2.3"] = github.Release{TagName: "v1.2.3", Author: github.User{Login: "mallory"}}
			},
			wantErr: "Build REJECTED: the GitHub Release for v1.2.3 was made by mallory, not by an accepted release actor (release-app[bot],other).",
		},
		{
			name:    "a hotfix at the tip of its line is validated, and an environment without a live record has nothing to be behind",
			env:     connected,
			subs:    map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			objects: map[string]string{"gs://tst-records/quill/tst/v1.2.4/b-1.json": hotfixLive},
			wantOut: []string{"Tag v1.2.4 validated as a hotfix: the tip of hotfix/1.2.x, branched from release v1.2.3 on master", "Gate passed: v1.2.4 is live in tst", "Hotfix check: stg has no live deployment record; nothing for v1.2.4 to be behind."},
		},
		{
			name:    "a hotfix is refused where the database holds a migration it does not carry, naming the restore",
			env:     connected,
			subs:    map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			objects: map[string]string{"gs://tst-records/quill/tst/v1.2.4/b-1.json": hotfixLive, "gs://stg-records/quill/stg/v1.3.0/b-5.json": stgLive("v1.3.0", first, refits)},
			files:   map[string]string{first.path(): first.content},
			wantErr: "Build REJECTED: stg's database holds schema/migrations/000002_Refits.up.sql (in v1.3.0's record), which hotfix v1.2.4 does not carry; restore stg to v1.2.4 first: a restore run replaces the database and skips this check.",
		},
		{
			name:    "a hotfix is refused where a migration's content differs from what the database applied",
			env:     connected,
			subs:    map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			objects: map[string]string{"gs://tst-records/quill/tst/v1.2.4/b-1.json": hotfixLive, "gs://stg-records/quill/stg/v1.3.0/b-5.json": stgLive("v1.3.0", first)},
			files:   map[string]string{first.path(): "create table other"},
			wantErr: "Build REJECTED: stg's database holds schema/migrations/000001_Init.up.sql with other content than hotfix v1.2.4 carries (by v1.3.0's record); restore stg to v1.2.4 first: a restore run replaces the database and skips this check.",
		},
		{
			name: "a pull request's live record under the environment is its own environment's, not the release stg runs",
			env:  connected,
			subs: map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			objects: map[string]string{
				"gs://tst-records/quill/tst/v1.2.4/b-1.json":      hotfixLive,
				"gs://stg-records/quill/stg/v1.3.0/b-5.json":      stgLive("v1.3.0", first, refits),
				"gs://stg-records/quill/stg/pr9-abc0123/b-8.json": strings.NewReplacer("v1.3.0", "pr9@abc0123", "2026-09-28", "2026-09-29", "b-5", "b-8").Replace(stgLive("v1.3.0", first)),
			},
			files:   map[string]string{first.path(): first.content},
			wantErr: "Build REJECTED: stg's database holds schema/migrations/000002_Refits.up.sql (in v1.3.0's record), which hotfix v1.2.4 does not carry; restore stg to v1.2.4 first: a restore run replaces the database and skips this check.",
		},
		{
			name:    "the refusal names the up file, whatever order the record lists the files in",
			env:     connected,
			subs:    map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			objects: map[string]string{"gs://tst-records/quill/tst/v1.2.4/b-1.json": hotfixLive, "gs://stg-records/quill/stg/v1.3.0/b-5.json": stgLive("v1.3.0", first, refitsDown, refits)},
			files:   map[string]string{first.path(): first.content},
			wantErr: "Build REJECTED: stg's database holds schema/migrations/000002_Refits.up.sql (in v1.3.0's record), which hotfix v1.2.4 does not carry; restore stg to v1.2.4 first: a restore run replaces the database and skips this check.",
		},
		{
			name:    "a hotfix deploys where every applied file is in it, whatever release the environment runs",
			env:     connected,
			subs:    map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			objects: map[string]string{"gs://tst-records/quill/tst/v1.2.4/b-1.json": hotfixLive, "gs://stg-records/quill/stg/v1.3.0/b-5.json": stgLive("v1.3.0", first)},
			files:   map[string]string{first.path(): first.content, refits.path(): refits.content},
			wantOut: []string{"Hotfix check passed: stg's database holds nothing v1.2.4 does not carry (1 file(s) recorded by v1.3.0, build b-5)"},
		},
		{
			name:       "a restore run is not checked: the database is about to be replaced",
			env:        connected + "export RESTORE=\"empty\"\nexport RESTORE_REQUESTER=\"octocat\"\n",
			subs:       map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			objects:    map[string]string{"gs://tst-records/quill/tst/v1.2.4/b-1.json": hotfixLive, "gs://stg-records/quill/stg/v1.3.0/b-5.json": stgLive("v1.3.0", first, refits)},
			files:      map[string]string{first.path(): first.content},
			wantOut:    []string{"Gate passed: v1.2.4 is live in tst", "Restore run: stg's database is replaced before v1.2.4 deploys, so what it holds is not compared with the hotfix."},
			wantAbsent: []string{"Hotfix check"},
		},
		{
			name: "the newest live record is the one compared, not a preview or an older release",
			env:  connected,
			subs: map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			objects: map[string]string{
				"gs://tst-records/quill/tst/v1.2.4/b-1.json": hotfixLive,
				"gs://stg-records/quill/stg/v1.2.3/b-3.json": strings.Replace(stgLive("v1.2.3", first, refits), "2026-09-28", "2026-09-20", 1),
				"gs://stg-records/quill/stg/v1.3.0/b-5.json": stgLive("v1.3.0", first),
				"gs://stg-records/quill/stg/v1.4.0/b-7.json": strings.Replace(strings.Replace(stgLive("v1.4.0", first, refits), "2026-09-28", "2026-09-29", 1), Live, Preview, 1),
			},
			files:   map[string]string{first.path(): first.content},
			wantOut: []string{"Hotfix check passed: stg's database holds nothing v1.2.4 does not carry (1 file(s) recorded by v1.3.0, build b-5)"},
		},
		{
			name: "the restore named is the hotfix itself",
			env:  connected,
			subs: map[string]string{tagSub: "v1.2.5", commitSub: "h2"},
			repo: func(r *githubtest.Repo) {
				r.Refs["refs/heads/hotfix/1.2.x"] = github.Object{Type: "commit", SHA: "h2"}
				r.Refs["refs/tags/v1.2.5"] = github.Object{Type: "commit", SHA: "h2"}
				r.Releases["v1.2.5"] = github.Release{TagName: "v1.2.5", Author: github.User{Login: "release-app[bot]"}}
			},
			objects: map[string]string{"gs://tst-records/quill/tst/v1.2.5/b-1.json": strings.Replace(hotfixLive, "v1.2.4", "v1.2.5", 1), "gs://stg-records/quill/stg/v1.3.0/b-5.json": stgLive("v1.3.0", first, refits)},
			files:   map[string]string{first.path(): first.content},
			wantErr: "Build REJECTED: stg's database holds schema/migrations/000002_Refits.up.sql (in v1.3.0's record), which hotfix v1.2.5 does not carry; restore stg to v1.2.5 first: a restore run replaces the database and skips this check.",
		},
		{
			name:       "a release on the default branch is not checked against the environment's database",
			env:        connected,
			objects:    map[string]string{"gs://tst-records/quill/tst/v1.2.3/b-0.json": liveRecord, "gs://stg-records/quill/stg/v1.3.0/b-5.json": stgLive("v1.3.0", first, refits)},
			files:      map[string]string{first.path(): first.content},
			wantOut:    []string{"Tag v1.2.3 validated: its commit is on master", "Gate passed: v1.2.3 is live in tst"},
			wantAbsent: []string{"Hotfix check"},
		},
		{
			name: "a hotfix from a line production does not run is refused at production's door",
			env:  connected,
			subs: map[string]string{tagSub: "v1.2.4", commitSub: "h1", envSub: prdEnvironment, previousEnvSub: stgEnvironment, previousRecordsSub: "stg-records", recordsBucket: "prd-records"},
			objects: map[string]string{
				"gs://stg-records/quill/stg/v1.2.4/b-1.json": strings.Replace(hotfixLive, `"env": "tst"`, `"env": "stg"`, 1),
				"gs://prd-records/quill/prd/v1.3.0/b-5.json": strings.Replace(stgLive("v1.3.0", first), `"env": "stg"`, `"env": "prd"`, 1),
			},
			files:   map[string]string{first.path(): first.content},
			wantErr: "Build REJECTED: production runs v1.3.0, line 1.3; hotfix v1.2.4 is on line 1.2. A hotfix is based on the release production runs.",
		},
		{
			name: "a hotfix from another line is refused at production's door before production's files are compared",
			env:  connected,
			subs: map[string]string{tagSub: "v1.2.4", commitSub: "h1", envSub: prdEnvironment, previousEnvSub: stgEnvironment, previousRecordsSub: "stg-records", recordsBucket: "prd-records"},
			objects: map[string]string{
				"gs://stg-records/quill/stg/v1.2.4/b-1.json": strings.Replace(hotfixLive, `"env": "tst"`, `"env": "stg"`, 1),
				"gs://prd-records/quill/prd/v1.3.0/b-5.json": strings.Replace(stgLive("v1.3.0", first, refits), `"env": "stg"`, `"env": "prd"`, 1),
			},
			files:   map[string]string{first.path(): first.content},
			wantErr: "Build REJECTED: production runs v1.3.0, line 1.3; hotfix v1.2.4 is on line 1.2. A hotfix is based on the release production runs.",
		},
		{
			name: "a hotfix behind production on its own line is refused with production's release named, never a restore",
			env:  connected,
			subs: map[string]string{tagSub: "v1.2.4", commitSub: "h1", envSub: prdEnvironment, previousEnvSub: stgEnvironment, previousRecordsSub: "stg-records", recordsBucket: "prd-records"},
			objects: map[string]string{
				"gs://stg-records/quill/stg/v1.2.4/b-1.json": strings.Replace(hotfixLive, `"env": "tst"`, `"env": "stg"`, 1),
				"gs://prd-records/quill/prd/v1.2.5/b-5.json": strings.Replace(stgLive("v1.2.5", first, refits), `"env": "stg"`, `"env": "prd"`, 1),
			},
			files:   map[string]string{first.path(): first.content},
			wantErr: "Build REJECTED: prd's database holds schema/migrations/000002_Refits.up.sql (in v1.2.5's record), which hotfix v1.2.4 does not carry; production is never restored by a run: a hotfix is based on the release production runs, so start the line from v1.2.5.",
		},
		{
			name: "a hotfix on production's line passes its door",
			env:  connected,
			subs: map[string]string{tagSub: "v1.2.4", commitSub: "h1", envSub: prdEnvironment, previousEnvSub: stgEnvironment, previousRecordsSub: "stg-records", recordsBucket: "prd-records"},
			objects: map[string]string{
				"gs://stg-records/quill/stg/v1.2.4/b-1.json": strings.Replace(hotfixLive, `"env": "tst"`, `"env": "stg"`, 1),
				"gs://prd-records/quill/prd/v1.2.3/b-5.json": strings.Replace(stgLive("v1.2.3", first), `"env": "stg"`, `"env": "prd"`, 1),
			},
			files:   map[string]string{first.path(): first.content},
			wantOut: []string{"Hotfix check passed: prd's database holds nothing v1.2.4 does not carry (1 file(s) recorded by v1.2.3, build b-5)"},
		},
		{
			name:    "a tag off the branch that is not a version is refused",
			env:     connected,
			subs:    map[string]string{tagSub: "v1.3.0-rc1", commitSub: "h1"},
			wantErr: "Build REJECTED: tag v1.3.0-rc1 is not on master (compare status: diverged) and is not a v<major>.<minor>.<patch> tag a hotfix line could carry.",
		},
		{
			name: "a hotfix line without a branch is refused",
			env:  connected,
			subs: map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			repo: func(r *githubtest.Repo) {
				delete(r.Refs, "refs/heads/hotfix/1.2.x")
			},
			wantErr: "Build REJECTED: tag v1.2.4 is not on master (compare status: diverged) and there is no branch hotfix/1.2.x it could be a hotfix of.",
		},
		{
			name: "a hotfix tag behind its branch's tip is refused",
			env:  connected,
			subs: map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			repo: func(r *githubtest.Repo) {
				r.Refs["refs/heads/hotfix/1.2.x"] = github.Object{Type: "commit", SHA: "h2"}
			},
			wantErr: "Build REJECTED: tag v1.2.4 (commit h1) is not at the tip of hotfix/1.2.x (h2); a hotfix release is the branch's tip.",
		},
		{
			name: "a hotfix line that starts at no release is refused",
			env:  connected,
			subs: map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			repo: func(r *githubtest.Repo) {
				delete(r.Refs, "refs/tags/v1.2.3")
			},
			wantErr: "Build REJECTED: hotfix/1.2.x branches from c3 on master, which carries no v1.2.* release tag; a hotfix line starts at a release.",
		},
		{
			name:    "no record in the previous environment is refused",
			env:     connected,
			wantErr: "Build REJECTED: release v1.2.3 has no deployment record in tst (nothing under gs://tst-records/quill/tst/v1.2.3/); a release reaches stg after it is live in tst.",
		},
		{
			name:    "records with none live are refused",
			env:     connected,
			objects: map[string]string{"gs://tst-records/quill/tst/v1.2.3/b-9.json": previewOnly},
			wantErr: "Build REJECTED: release v1.2.3 has deployment records in tst but none is live: traffic never shifted to it there.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := githubtest.New(t)
			repo := quillRepo()
			if tt.repo != nil {
				tt.repo(repo)
			}
			srv.AddRepo("acme", "quill", repo)
			store := &memoryStore{objects: map[string]string{}}
			for path, content := range tt.objects {
				store.objects[path] = content
			}
			inMaintenance := serviceDoc("projects/stg-project/locations/us-central1/services/quill-app")
			container, _ := inMaintenance["template"].(map[string]any)["containers"].([]any)[0].(map[string]any)
			container["env"] = []any{map[string]any{keyName: "APP_MAINTENANCE", keyValue: "1"}}
			run := newFakeRun(map[string]map[string]any{"projects/stg-project/locations/us-central1/services/quill-app": inMaintenance})
			clock := &fakeClock{now: tt.now}
			if clock.now.IsZero() {
				clock.now = chicago(10, 5, 9, 0)
			}
			clients := &Clients{Storage: store.open, StorageAs: store.openAs, Run: run.open, Now: clock.Now, Sleep: clock.Sleep, GitHub: func(string) *github.Client {
				return srv.Client()
			}}
			placement := tt.placement
			if placement == "" {
				placement = testPlacement("")
			}
			files := map[string]string{EnvironmentFile: tt.env, BuildFile: releaseBuild(t, tt.subs), stackDir + "/" + placementFile: placement}
			for name, content := range tt.files {
				files[name] = content
			}
			w := workspaceFiles(t, files)
			bedrock := tt.bedrock
			if bedrock == "" {
				bedrock = "v0.4.0"
			}
			var out strings.Builder
			err := ValidateRelease(t.Context(), clients, w, bedrock, tt.routerDir, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ValidateRelease() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out.String())
				}

				return
			}
			if err != nil {
				t.Fatalf("ValidateRelease() error = %v; output:\n%s", err, out.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(out.String(), absent) {
					t.Errorf("output has %q:\n%s", absent, out.String())
				}
			}
			facts, err := w.Environment()
			if err != nil {
				t.Fatal(err)
			}
			if facts[windowReleaseFact] != tt.wantFact {
				t.Errorf("%s = %q, want %q", windowReleaseFact, facts[windowReleaseFact], tt.wantFact)
			}
			for name, want := range tt.wantWindow {
				if facts[name] != want {
					t.Errorf("%s = %q, want %q", name, facts[name], want)
				}
			}
			if tt.wantIdentities != nil && strings.Join(store.identities, ",") != strings.Join(tt.wantIdentities, ",") {
				t.Errorf("records read as %v, want %v", store.identities, tt.wantIdentities)
			}
		})
	}
}
