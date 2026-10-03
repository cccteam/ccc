package deploy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// fakeClock is the clients' clock in tests: Now answers it, and Sleep advances it
// instead of waiting, keeping every sleep.
type fakeClock struct {
	now   time.Time
	slept []time.Duration
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.now = c.now.Add(d)
	c.slept = append(c.slept, d)

	return nil
}

// chicago is a wall-clock moment of 2026 in Chicago, the test placements' zone.
func chicago(month time.Month, day, hour, minute int) time.Time {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		panic(err)
	}

	return time.Date(2026, month, day, hour, minute, 0, 0, loc)
}

// testPlacement is a checkout's placement with the environments tst, stg and prd and the
// maintenance block given (JSON), or none.
func testPlacement(maintenance string) string {
	p := `{"prefix": "imp", "environments": ["tst", "stg", "prd"], "regions": [{"name": "us-central1", "code": "uc1"}], "appsDomain": "example.dev", "hostedDomain": "example.com", "stateBucket": "b", "placeholderImage": "i", "defaultBranch": "master", "repository": "quill", "releaseApp": "release-app"`
	if maintenance != "" {
		p += `, "maintenance": ` + maintenance
	}

	return p + "}\n"
}

// releaseFile is a release file with the outlets given (JSON).
func releaseFile(outlets string) string {
	return `{"outlets": ` + outlets + "}\n"
}

func TestDecideWindow(t *testing.T) {
	t.Parallel()

	release := func(v string) *derive.OldestAnswered {
		return &derive.OldestAnswered{Release: v, Outlet: "default"}
	}
	this := &derive.OldestAnswered{This: true, Outlet: "portal"}
	live := func(v string) *Record {
		return &Record{Version: v, Status: Live}
	}
	anytime := &derive.MaintenanceWindow{Anytime: true}
	all := &derive.MaintenanceWindow{TimeZone: "America/Chicago", Weekly: []derive.WeeklySlot{{Day: "Sunday", From: "02:00", To: "04:00"}}, Releases: derive.ReleasesAll}
	tests := []struct {
		name    string
		oldest  *derive.OldestAnswered
		tag     string
		live    *Record
		setting *derive.MaintenanceWindow
		want    windowDecision
	}{
		{
			name: "1.5.0 with oldest 1.5.0 needs the window with production on 1.4.0", oldest: release("1.5.0"), tag: "v1.5.0", live: live("v1.4.0"), setting: anytime,
			want: windowDecision{Breaking: true, Needed: true, Reason: "the default outlet answers 1.5.0 at the oldest, and prd runs v1.4.0, which it turns away"},
		},
		{
			name: "1.6.0 with oldest 1.5.0 does not, once production runs 1.5.0", oldest: release("1.5.0"), tag: "v1.6.0", live: live("v1.5.0"), setting: anytime,
			want: windowDecision{Reason: "the default outlet answers 1.5.0 at the oldest, which prd's v1.5.0 is not older than"},
		},
		{
			name: "the same 1.6.0 does with production still on 1.4.0, a rolling step skipped", oldest: release("1.5.0"), tag: "v1.6.0", live: live("v1.4.0"), setting: anytime,
			want: windowDecision{Breaking: true, Needed: true, Reason: "the default outlet answers 1.5.0 at the oldest, and prd runs v1.4.0, which it turns away"},
		},
		{
			name: "an outlet answering its own release alone is breaking", oldest: this, tag: "v1.5.0", live: live("v1.4.0"), setting: anytime,
			want: windowDecision{Breaking: true, Needed: true, Reason: "the portal outlet answers this release alone (oldest answered: this), and prd runs v1.4.0, which it turns away"},
		},
		{
			name: "a rerun of the release the environment runs turns nothing away, even answering itself alone", oldest: this, tag: "v1.5.0", live: live("v1.5.0"), setting: anytime,
			want: windowDecision{Reason: "the portal outlet answers this release alone (oldest answered: this), and prd runs it already (v1.5.0), so a rerun turns nothing away"},
		},
		{
			name: "a release not cut yet that answers itself alone turns every live release away", oldest: this, tag: "", live: live("v1.5.0"), setting: anytime,
			want: windowDecision{Breaking: true, Needed: true, Reason: "the portal outlet answers this release alone (oldest answered: this), and prd runs v1.5.0, which it turns away"},
		},
		{
			name: "no outlet declaring an oldest answered release is never breaking", oldest: nil, tag: "v1.5.0", live: live("v1.4.0"), setting: anytime,
			want: windowDecision{Reason: "no outlet declares an oldest answered release, so the release turns no running release away"},
		},
		{
			name: "an environment running nothing live has nothing to turn away", oldest: this, tag: "v1.5.0", live: nil, setting: anytime,
			want: windowDecision{Reason: "prd runs no release live, so there is nothing for the release to turn away"},
		},
		{
			name: "under all an ordinary release waits for the window without being breaking", oldest: release("1.5.0"), tag: "v1.6.0", live: live("v1.5.0"), setting: all,
			want: windowDecision{Needed: true, Reason: "the default outlet answers 1.5.0 at the oldest, which prd's v1.5.0 is not older than; every release waits for prd's window (releases: all)"},
		},
		{
			name: "under all a breaking release is breaking, the reason unchanged", oldest: release("1.5.0"), tag: "v1.5.0", live: live("v1.4.0"), setting: all,
			want: windowDecision{Breaking: true, Needed: true, Reason: "the default outlet answers 1.5.0 at the oldest, and prd runs v1.4.0, which it turns away"},
		},
		{
			name: "under all with nothing declared every release still waits", oldest: nil, tag: "v1.5.0", live: live("v1.4.0"), setting: all,
			want: windowDecision{Needed: true, Reason: "no outlet declares an oldest answered release, so the release turns no running release away; every release waits for prd's window (releases: all)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, decideWindow(tt.oldest, tt.tag, tt.live, tt.setting, prdEnvironment), cmp.AllowUnexported(windowDecision{})); diff != "" {
				t.Errorf("decideWindow() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWaitForWindow(t *testing.T) {
	t.Parallel()

	const (
		central = "projects/tst-project/locations/us-central1/services/harbor-app"
		weekly  = `{"tst": {"timeZone": "America/Chicago", "weekly": [{"day": "Sunday", "from": "02:00", "to": "04:00"}]}}`
		needed  = "export SKIP_DEPLOY=\"\"\nexport VERSION=\"v0.2.2\"\nexport WINDOW_NEEDED=\"true\"\nexport WINDOW_BREAKING=\"true\"\nexport WINDOW_REASON=\"the default outlet answers 0.2.2 at the oldest, and tst runs v0.2.1, which it turns away\"\n"
	)
	saturdayEvening := chicago(10, 3, 20, 0)
	subs := map[string]string{"_PROJECT": "tst-project", "_ENV": "tst", "_SERVICES": "us-central1=harbor-app"}
	tests := []struct {
		name string
		env  string
		subs map[string]string
		// placement is the checkout's maintenance block; now the clock at the step's
		// start; build the build's start and timeout; inMaintenance sets the maintenance
		// variable on the live service.
		placement     string
		now           time.Time
		build         Build
		inMaintenance bool
		wantOut       []string
		// wantFacts are the facts the step leaves (an empty value: left empty, or not
		// left); wantSleeps how many times it slept; wantErr the refusal.
		wantFacts  map[string]string
		wantSleeps int
		wantErr    string
	}{
		{
			name:      "a run that waits for no window says why",
			env:       "export SKIP_DEPLOY=\"\"\nexport WINDOW_NEEDED=\"\"\nexport WINDOW_REASON=\"the default outlet answers 0.2.1 at the oldest, which tst's v0.2.1 is not older than\"\n",
			subs:      subs,
			placement: weekly,
			now:       saturdayEvening,
			wantOut:   []string{"No window to wait for: the default outlet answers 0.2.1 at the oldest, which tst's v0.2.1 is not older than."},
			wantFacts: map[string]string{windowOpenedFact: ""},
		},
		{
			name:      "a pull-request build never waits",
			env:       "export SKIP_DEPLOY=\"\"\n",
			subs:      map[string]string{"_PROJECT": "tst-project", "_ENV": "tst", "_PR_NUMBER": "7"},
			placement: weekly,
			now:       saturdayEvening,
			wantOut:   []string{"No window to wait for: a pull-request build never waits for one."},
		},
		{
			name:      "an open window lets the run through at once",
			env:       needed,
			subs:      subs,
			placement: `{"tst": "anytime"}`,
			now:       saturdayEvening,
			wantOut:   []string{"tst's maintenance window is open (anytime); the run waited 0s and proceeds into maintenance, the migrations and the rollout."},
			wantFacts: map[string]string{windowOpenedFact: "2026-10-04T01:00:00Z", windowWaitedFact: "", windowSlotFact: "anytime"},
		},
		{
			name:      "a closed window makes the run wait, printing when it proceeds, and lets it through when it opens",
			env:       needed,
			subs:      subs,
			placement: weekly,
			now:       saturdayEvening,
			build:     Build{StartTime: saturdayEvening.UTC().Format(time.RFC3339), Timeout: "86400s"},
			wantOut: []string{
				"Waiting for tst's maintenance window: it opens Sunday 2026-10-04 02:00 CDT (in 6h, Sunday 02:00 to 04:00 America/Chicago); the run proceeds then.",
				"tst's maintenance window is open (Sunday 02:00 to 04:00 America/Chicago, until Sunday 2026-10-04 04:00 CDT); the run waited 6h0m0s and proceeds into maintenance, the migrations and the rollout.",
			},
			wantFacts:  map[string]string{windowOpenedFact: "2026-10-04T07:00:00Z", windowWaitedFact: "6h0m0s", windowSlotFact: "Sunday 02:00 to 04:00 America/Chicago"},
			wantSleeps: 36,
		},
		{
			name:      "an opening further away than the build can wait stops the run before anything changes",
			env:       needed,
			subs:      subs,
			placement: weekly,
			now:       chicago(10, 5, 10, 0),
			build:     Build{StartTime: chicago(10, 5, 9, 30).UTC().Format(time.RFC3339), Timeout: "86400s"},
			wantErr:   "Build REJECTED: tst's maintenance window next opens Sunday 2026-10-11 02:00 CDT (in 136h, Sunday 02:00 to 04:00 America/Chicago), further away than this run can wait (until Tuesday 2026-10-06 06:30 CDT, the build's timeout less 3h0m0s for the steps after the window): start the release on the day of the window.",
		},
		{
			name:      "a budget that runs out while the run waits stops it",
			env:       needed,
			subs:      subs,
			placement: weekly,
			now:       saturdayEvening,
			build:     Build{StartTime: saturdayEvening.Add(-20 * time.Hour).UTC().Format(time.RFC3339), Timeout: "86400s"},
			wantErr:   "further away than this run can wait (until Saturday 2026-10-03 21:00 CDT",
		},
		{
			name:      "a window with no opening ahead stops the run",
			env:       needed,
			subs:      subs,
			placement: `{"tst": {"timeZone": "America/Chicago", "dates": [{"on": "2026-09-15", "from": "22:00", "to": "23:30"}]}}`,
			now:       saturdayEvening,
			wantErr:   "Build REJECTED: tst's maintenance window has no opening ahead (its dated slots have passed and it has no weekly slot): write the next slot in infrastructure/placement.json and release again.",
		},
		{
			name:      "a run in maintenance already passes at once",
			env:       needed + "export MAINTENANCE=\"true\"\n",
			subs:      subs,
			placement: weekly,
			now:       saturdayEvening,
			wantOut:   []string{"The gate is open: tst is in maintenance already (this run put it there before its database was replaced)."},
			wantFacts: map[string]string{windowOpenedFact: "2026-10-04T01:00:00Z", windowSlotFact: slotInMaintenance},
		},
		{
			name:          "an environment in maintenance from an earlier run passes with the window closed",
			env:           needed,
			subs:          subs,
			placement:     weekly,
			now:           saturdayEvening,
			inMaintenance: true,
			wantOut:       []string{"The gate is open: tst is in maintenance from an earlier run (APP_MAINTENANCE=1 on harbor-app), so the rerun proceeds without waiting for the next window."},
			wantFacts:     map[string]string{windowOpenedFact: "2026-10-04T01:00:00Z", windowSlotFact: slotInMaintenance},
		},
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			subs:    subs,
			now:     saturdayEvening,
			wantOut: []string{tornDown},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			build := tt.build
			build.ID, build.Substitutions = "b-1", tt.subs
			files := map[string]string{EnvironmentFile: tt.env, BuildFile: buildJSONOf(t, &build)}
			if tt.placement != "" {
				files[stackDir+"/"+placementFile] = testPlacement(tt.placement)
			}
			w := workspaceFiles(t, files)
			doc := serviceDoc(central)
			if tt.inMaintenance {
				setMaintenanceVariable(t, doc)
			}
			run := newFakeRun(map[string]map[string]any{central: doc})
			clock := &fakeClock{now: tt.now}
			var out strings.Builder
			err := WaitForWindow(t.Context(), &Clients{Run: run.open, Now: clock.Now, Sleep: clock.Sleep}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("WaitForWindow() error = %v, want %q; output:\n%s", err, tt.wantErr, out.String())
				}

				return
			}
			if err != nil {
				t.Fatalf("WaitForWindow() error = %v; output:\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			if len(clock.slept) != tt.wantSleeps {
				t.Errorf("slept %d time(s), want %d", len(clock.slept), tt.wantSleeps)
			}
			facts, err := w.Environment()
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range tt.wantFacts {
				if facts[name] != want {
					t.Errorf("%s = %q, want %q", name, facts[name], want)
				}
			}
		})
	}
}

// buildJSONOf is the build file for a build.
func buildJSONOf(t *testing.T, b *Build) string {
	t.Helper()

	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// setMaintenanceVariable puts the maintenance variable, set, on the service's template.
func setMaintenanceVariable(t *testing.T, doc map[string]any) {
	t.Helper()

	template, _ := doc[keyTemplate].(map[string]any)
	container, err := firstContainer(template)
	if err != nil {
		t.Fatal(err)
	}
	container["env"] = []any{map[string]any{keyName: derive.MaintenanceVariable, keyValue: maintenanceOn}}
}

func TestUntil(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{name: "seconds are under a minute", at: now.Add(20 * time.Second), want: "under a minute"},
		{name: "minutes alone", at: now.Add(45 * time.Minute), want: "45m"},
		{name: "whole hours", at: now.Add(6 * time.Hour), want: "6h"},
		{name: "hours and minutes, the minutes padded", at: now.Add(time.Hour + 5*time.Minute), want: "1h05m"},
		{name: "days stay hours", at: now.Add(136 * time.Hour), want: "136h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := until(tt.at, now); got != tt.want {
				t.Errorf("until() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWaitBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		build Build
		want  time.Time
	}{
		{name: "the build's start plus its timeout, less the reserve", build: Build{StartTime: "2026-10-03T10:00:00Z", Timeout: "86400s"}, want: time.Date(2026, 10, 4, 7, 0, 0, 0, time.UTC)},
		{name: "a build that names neither counts from now with the ceiling", build: Build{}, want: now.Add(21 * time.Hour)},
		{name: "a start that does not read counts from now", build: Build{StartTime: "yesterday", Timeout: "3600s"}, want: now.Add(-2 * time.Hour)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := waitBudget(&tt.build, now); !got.Equal(tt.want) {
				t.Errorf("waitBudget() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestWindowOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want *Window
	}{
		{name: "a run that needed no window records none", env: map[string]string{windowBreakingFact: ""}},
		{
			name: "a breaking release records the window whole",
			env:  map[string]string{windowNeededFact: trueValue, windowBreakingFact: trueValue, windowReasonFact: "why", windowSlotFact: "anytime", windowOpenedFact: "2026-10-04T07:00:00Z", windowWaitedFact: "6h0m0s"},
			want: &Window{Breaking: true, Reason: "why", Slot: "anytime", Opened: "2026-10-04T07:00:00Z", Waited: "6h0m0s"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, windowOf(tt.env)); diff != "" {
				t.Errorf("windowOf() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestReadOldest reads the release file the way the release check does: from the router
// directory of the checkout, with no directory, no file, no option and the file.
func TestReadOldest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		routerDir string
		file      string
		wantOut   string
		want      *derive.OldestAnswered
		wantErr   string
	}{
		{name: "no router directory", wantOut: "No release file to read: the pipeline names no router directory"},
		{name: "no file", routerDir: routerDir, wantOut: "Warning: no release file at pkg/router/zz_gen_release.json in the checkout"},
		{name: "no outlet with the option", routerDir: routerDir, file: releaseFile(`{"default": {"oldestAnswered": ""}, "api": {"kind": "api-key"}}`), wantOut: "The release file pkg/router/zz_gen_release.json declares no oldest answered release on any outlet"},
		{name: "the newest over the outlets", routerDir: routerDir, file: releaseFile(`{"default": {"oldestAnswered": "1.5.0"}, "portal": {"oldestAnswered": "1.6.0"}}`), want: &derive.OldestAnswered{Release: "1.6.0", Outlet: "portal"}},
		{name: "a file that does not read", routerDir: routerDir, file: "{", wantErr: "is not the shape the generator writes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{}
			if tt.file != "" {
				files[releaseFilePath] = tt.file
			}
			w := workspaceFiles(t, files)
			var out strings.Builder
			got, err := readOldest(w, tt.routerDir, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("readOldest() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("readOldest() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("readOldest() mismatch (-want +got):\n%s", diff)
			}
			if tt.wantOut != "" && !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("output lacks %q:\n%s", tt.wantOut, out.String())
			}
			if _, err := os.Stat(filepath.Join(string(w), releaseFilePath)); tt.file != "" && err != nil {
				t.Errorf("the release file was not left in place: %v", err)
			}
		})
	}
}
