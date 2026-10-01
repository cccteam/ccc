package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

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
	// stgLive is stg's live record of a release, listing the files its build applied.
	stgLive := func(version string, applied ...migrationFile) string {
		var list []string
		for _, m := range applied {
			list = append(list, `{"dir": "`+m.dir+`", "name": "`+m.name+`", "hash": "`+m.hash()+`"}`)
		}

		return `{"app": "quill", "env": "stg", "version": "` + version + `", "status": "live", "timestamp": "2026-09-28T05:30:00Z", "build": "b-5", "migrations": [` + strings.Join(list, ", ") + `]}`
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
		bedrock    string
		wantOut    []string
		wantAbsent []string
		// wantFact is WINDOW_RELEASE after the step: true for a window release, else empty.
		wantFact string
		wantErr  string
	}{
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
			wantOut:  []string{"Release v1.2.3 validated: cut by release-app[bot]", "Window release: the release notes of v1.2.3 carry a breaking-changes section", "Gate passed: v1.2.3 is live in tst"},
			wantFact: trueValue,
		},
		{
			name: "a release whose notes mention breaking changes in prose is not one",
			env:  connected,
			repo: func(r *githubtest.Repo) {
				r.Releases["v1.2.3"] = github.Release{TagName: "v1.2.3", Author: github.User{Login: "release-app[bot]"}, Body: "### Features\n\n* no breaking changes this time\n"}
			},
			objects:    live,
			wantAbsent: []string{"Window release"},
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
			wantErr: "Build REJECTED: stg's database holds schema/migrations/000002_Refits.up.sql (applied by v1.3.0), which hotfix v1.2.4 does not carry; restore stg to v1.2.4 first: a restore run replaces the database and skips this check.",
		},
		{
			name:    "a hotfix is refused where a migration's content differs from what the database applied",
			env:     connected,
			subs:    map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			objects: map[string]string{"gs://tst-records/quill/tst/v1.2.4/b-1.json": hotfixLive, "gs://stg-records/quill/stg/v1.3.0/b-5.json": stgLive("v1.3.0", first)},
			files:   map[string]string{first.path(): "create table other"},
			wantErr: "Build REJECTED: stg's database holds schema/migrations/000001_Init.up.sql as v1.3.0 applied it, with other content than hotfix v1.2.4 carries; restore stg to v1.2.4 first: a restore run replaces the database and skips this check.",
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
			wantErr: "Build REJECTED: stg's database holds schema/migrations/000002_Refits.up.sql (applied by v1.3.0), which hotfix v1.2.4 does not carry; restore stg to v1.2.4 first: a restore run replaces the database and skips this check.",
		},
		{
			name:    "the refusal names the up file, whatever order the record lists the files in",
			env:     connected,
			subs:    map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			objects: map[string]string{"gs://tst-records/quill/tst/v1.2.4/b-1.json": hotfixLive, "gs://stg-records/quill/stg/v1.3.0/b-5.json": stgLive("v1.3.0", first, refitsDown, refits)},
			files:   map[string]string{first.path(): first.content},
			wantErr: "Build REJECTED: stg's database holds schema/migrations/000002_Refits.up.sql (applied by v1.3.0), which hotfix v1.2.4 does not carry; restore stg to v1.2.4 first: a restore run replaces the database and skips this check.",
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
			wantErr: "Build REJECTED: stg's database holds schema/migrations/000002_Refits.up.sql (applied by v1.3.0), which hotfix v1.2.5 does not carry; restore stg to v1.2.5 first: a restore run replaces the database and skips this check.",
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
			clients := &Clients{Storage: store.open, GitHub: func(string) *github.Client {
				return srv.Client()
			}}
			files := map[string]string{EnvironmentFile: tt.env, BuildFile: releaseBuild(t, tt.subs)}
			for name, content := range tt.files {
				files[name] = content
			}
			w := workspaceFiles(t, files)
			bedrock := tt.bedrock
			if bedrock == "" {
				bedrock = "v0.4.0"
			}
			var out strings.Builder
			err := ValidateRelease(t.Context(), clients, w, bedrock, &out)
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
		})
	}
}
