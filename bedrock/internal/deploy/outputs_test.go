package deploy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReadStackOutputs(t *testing.T) {
	t.Parallel()

	const (
		object = "gs://records/quill/stacks/tst/pr7/c9.json"
		named  = "gs://records/quill/stacks/tst/pr7/c9.json"
	)
	// outputs is the stack job's object, as the job wrote it: applied for the comment
	// given, with the mode and the status a case sets.
	outputs := func(comment int64, change func(o *StackOutputs)) string {
		o := StackOutputs{
			App: "quill", Env: tstEnvironment, PullRequest: "7", Commit: "c9", Build: "b-9", Comment: comment, Status: StackApplied,
			Services: "us-central1=quill-pr7", JobsJob: "us-central1=quill-pr7-jobs", Hostname: "quill-pr7.example.dev",
			MigrateEnv: `{"APP_SERVICE_NAME":"quill-migrate"}`, MigrateDatabases: `["projects/p/instances/i/databases/quill-pr7-db"]`,
			BuildArguments: map[string]string{"FIREBASE_API_KEY": "AIzaPr7Key"}, Timestamp: "2026-10-12T10:00:00Z",
		}
		if change != nil {
			change(&o)
		}
		data, err := json.Marshal(o)
		if err != nil {
			panic(err)
		}

		return string(data)
	}
	failed := func(o *StackOutputs) {
		o.Status, o.Reason, o.Services = StackFailed, "Build REJECTED: the plan touches resources that are not pull request 7's", ""
	}
	destroyed := func(o *StackOutputs) {
		o.Status, o.Services = StackDestroyed, ""
	}
	tests := []struct {
		name string
		// env is the environment file beyond the comment's id; subs the build's
		// substitutions (jobSubs when nil).
		env  string
		subs map[string]string
		// object is the object as the step starts ("" for none); later the object after
		// laterAfter waits ("" for none).
		object     string
		later      string
		laterAfter int
		wantFacts  map[string]string
		// wantAbsent are facts the environment file must not carry.
		wantAbsent []string
		wantOut    []string
		wantNotOut []string
		wantSlept  int
		wantErr    string
	}{
		{
			name:      "the object is there: its facts go to the environment file for the steps after, the build arguments' values kept out of the log",
			object:    outputs(1, nil),
			wantFacts: map[string]string{services: "us-central1=quill-pr7", jobsJobFact: "us-central1=quill-pr7-jobs", prHostnameFact: "quill-pr7.example.dev", migrateEnvFact: `{"APP_SERVICE_NAME":"quill-migrate"}`, migrateDatabasesFact: `["projects/p/instances/i/databases/quill-pr7-db"]`, sharedDBFact: "", reloadDBFact: "", reloadReasonFact: "", "_BUILD_ARG_FIREBASE_API_KEY": "AIzaPr7Key"},
			wantOut: []string{
				"The pull request's stack (job b-9) names SERVICES=us-central1=quill-pr7 JOBS_JOB=us-central1=quill-pr7-jobs PR_HOSTNAME=quill-pr7.example.dev, and the migrate command's settings (MIGRATE_ENV); the site runs against its own database.",
				"The image build passes the pull request's own build arguments: FIREBASE_API_KEY.",
			},
			wantNotOut: []string{"AIzaPr7Key", "Waiting"},
		},
		{
			name:       "the job finishes while the build waits",
			later:      outputs(1, nil),
			laterAfter: 2,
			wantFacts:  map[string]string{services: "us-central1=quill-pr7"},
			wantOut: []string{
				"Waiting for the pull request's stack job to write " + named + " (the stack's pr-stack trigger runs it on the same /gcbrun comment), up to 10m0s.",
				"The stack job wrote " + named + " after 30s.",
			},
			wantSlept: 2,
		},
		{
			name:       "an object written for another /gcbrun comment on the same commit is waited past",
			object:     outputs(3, nil),
			later:      outputs(1, nil),
			laterAfter: 1,
			wantFacts:  map[string]string{services: "us-central1=quill-pr7"},
			wantOut:    []string{named + " was written for another /gcbrun comment (3; this build's is 1): waiting for the job of this comment, up to 10m0s.", "The stack job wrote " + named + " after 15s."},
			wantSlept:  1,
		},
		{
			name: "a shared database is said",
			object: outputs(1, func(o *StackOutputs) {
				o.SharedDatabase, o.MigrateDatabases = true, ""
			}),
			wantFacts:  map[string]string{sharedDBFact: trueValue, reloadDBFact: ""},
			wantAbsent: []string{migrateDatabasesFact},
			wantOut:    []string{"the site runs against tst's database (shared-db)."},
		},
		{
			name: "a recreated database is said with its reason",
			object: outputs(1, func(o *StackOutputs) {
				o.DatabaseRecreated = "/gcbrun reload-db"
			}),
			wantFacts: map[string]string{reloadDBFact: trueValue, reloadReasonFact: "/gcbrun reload-db"},
			wantOut:   []string{"the site runs against its own database, recreated this build (/gcbrun reload-db)."},
		},
		{
			name:    "the job failed: this build stops at once with the job's reason",
			object:  outputs(1, failed),
			wantErr: "Build REJECTED: the pull request's stack job failed (build b-9): the plan touches resources that are not pull request 7's",
		},
		{
			name:       "/gcbrun down: the build waits for the destroy and deploys nothing",
			env:        "export DOWN=\"true\"\nexport SKIP_DEPLOY=\"true\"\n",
			later:      outputs(1, destroyed),
			laterAfter: 1,
			wantAbsent: []string{services},
			wantOut:    []string{"The stack job destroyed the pull request's environment (build b-9); nothing deploys."},
			wantSlept:  1,
		},
		{
			name:    "a destroy where this build was to deploy is refused",
			object:  outputs(1, destroyed),
			wantErr: "the pull request's stack job destroyed the environment (build b-9), where this build was to deploy into it: comment /gcbrun again to stand it up",
		},
		{
			name:    "an apply where this build was to tear the environment down is refused",
			env:     "export DOWN=\"true\"\n",
			object:  outputs(1, nil),
			wantErr: "the pull request's stack job applied the stack (build b-9), where this build was to tear it down (/gcbrun down): the two read different comments",
		},
		{
			name:      "the wait runs out",
			wantOut:   []string{"Waiting for the pull request's stack job to write " + named},
			wantSlept: 40,
			wantErr:   "the pull request's stack job has not written " + named + " after 10m0s: the stack's pr-stack trigger runs it on the same /gcbrun comment, as the apply identity, and it has not finished or never started.",
		},
		{
			name: "outputs naming no services are refused",
			object: outputs(1, func(o *StackOutputs) {
				o.Services = ""
			}),
			wantErr: "the stack job's outputs name no services (" + named + ")",
		},
		{
			name:    "a tag build says so and ends",
			subs:    map[string]string{tagSub: "v1.2.3", envSub: tstEnvironment},
			wantOut: []string{tagBuildNotice},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subs := tt.subs
			if subs == nil {
				subs = jobSubs()
			}
			w := workspaceFiles(t, map[string]string{EnvironmentFile: "export COMMENT_ID=\"1\"\n" + tt.env, BuildFile: buildFor(t, subs)})
			store := &memoryStore{objects: map[string]string{}}
			if tt.object != "" {
				store.objects[object] = tt.object
			}
			slept := 0
			sleep := func(_ context.Context, d time.Duration) error {
				slept++
				if d != stackOutputsPoll {
					t.Errorf("slept %s, want %s", d, stackOutputsPoll)
				}
				if tt.later != "" && slept == tt.laterAfter {
					store.objects[object] = tt.later
				}

				return nil
			}
			var out strings.Builder
			err := ReadStackOutputs(t.Context(), &Clients{Storage: store.open, Sleep: sleep}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ReadStackOutputs() error = %v, want %q\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("ReadStackOutputs() error = %v\n%s", err, out.String())
			}
			if slept != tt.wantSlept {
				t.Errorf("slept %d time(s), want %d", slept, tt.wantSlept)
			}
			containsAll(t, out.String(), tt.wantOut...)
			for _, unwanted := range tt.wantNotOut {
				if strings.Contains(out.String(), unwanted) {
					t.Errorf("output carries %q:\n%s", unwanted, out.String())
				}
			}
			env, err := w.Environment()
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range tt.wantFacts {
				if got, ok := env[name]; !ok || got != want {
					t.Errorf("%s = %q (present %t), want %q", name, got, ok, want)
				}
			}
			for _, name := range tt.wantAbsent {
				if got, ok := env[name]; ok {
					t.Errorf("%s = %q, want it left out", name, got)
				}
			}
		})
	}
}

func TestStackOutputsObject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                     string
		app, env, number, commit string
		want                     string
	}{
		{name: "a pull request's commit", app: "quill", env: "tst", number: "7", commit: "c9", want: "quill/stacks/tst/pr7/c9.json"},
		{name: "another commit of the same pull request is another object", app: "quill", env: "tst", number: "7", commit: "d0", want: "quill/stacks/tst/pr7/d0.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := StackOutputsObject(tt.app, tt.env, tt.number, tt.commit); got != tt.want {
				t.Errorf("StackOutputsObject() = %q, want %q", got, tt.want)
			}
		})
	}
}
