package deploy

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// templateDoc is a template job as the API answers it, the parts the step copies and the
// ones it leaves behind.
func templateDoc(name, account string) map[string]any {
	return map[string]any{
		keyName:      name,
		"uid":        "u-1",
		"createTime": "2026-09-30T20:00:00Z",
		"labels":     map[string]any{"terraform": "true", "application": "harbor"},
		"template": map[string]any{
			"taskCount": float64(1),
			"template": map[string]any{
				"serviceAccount": account,
				"containers":     []any{map[string]any{"image": "placeholder", "command": []any{"/jobs"}, "env": []any{map[string]any{"name": "APP_SERVICE_NAME", "value": "harbor-jobs"}}}},
				"timeout":        "1800s",
			},
		},
	}
}

func TestJobs(t *testing.T) {
	t.Parallel()

	const (
		migrateTemplate = "projects/tst-project/locations/us-central1/jobs/harbor-migrate"
		migrateJob      = migrateTemplate + "-v1-2-3"
		jobsTemplate    = "projects/tst-project/locations/us-central1/jobs/harbor-jobs"
		jobsJob         = jobsTemplate + "-v1-2-3"
		migrateAccount  = "harbor-migrate@tst-project.iam.gserviceaccount.com"
		jobsAccount     = "harbor-jobs@tst-project.iam.gserviceaccount.com"
		environment     = "export SKIP_DEPLOY=\"\"\nexport RUN_MIGRATIONS=\"true\"\nexport MIGRATE_JOB=\"us-central1=harbor-migrate\"\nexport JOBS_JOB=\"us-central1=harbor-jobs\"\nexport VERSION=\"v1.2.3\"\nexport IMAGE=\"reg/harbor\"\nexport IMAGE_DIGEST=\"sha256:abc\"\n"
		build           = `{"id": "b-1", "substitutions": {"_PROJECT": "tst-project", "_ENV": "tst", "COMMIT_SHA": "deadbeef", "REPO_NAME": "harbor", "_PR_NUMBER": "7"}}`
	)
	wantLabels := map[string]any{"terraform": "true", "application": "harbor", managedByLabel: managedByValue, commitLabel: "deadbeef", buildIDLabel: "b-1", sourceRepoLabel: "harbor", environmentLabel: "tst", prNumberLabel: "7", versionLabel: "v1-2-3"}
	templateBindings := []any{map[string]any{"role": "roles/run.invoker", "members": []any{"serviceAccount:harbor-app@tst-project.iam.gserviceaccount.com"}}}
	templatePolicy := map[string]any{keyBindings: templateBindings, "version": float64(1), keyEtag: "etag-of-template"}
	templates := func() map[string]map[string]any {
		return map[string]map[string]any{migrateTemplate: templateDoc(migrateTemplate, migrateAccount), jobsTemplate: templateDoc(jobsTemplate, jobsAccount)}
	}
	existing := func() map[string]map[string]any {
		resources := templates()
		for _, name := range []string{migrateJob, jobsJob} {
			resources[name] = map[string]any{keyName: name, "template": map[string]any{"template": map[string]any{"containers": []any{map[string]any{"image": "reg/harbor@sha256:older"}}}}}
		}

		return resources
	}
	tests := []struct {
		name         string
		env          string
		run          *fakeRun
		policies     map[string]map[string]any
		wantOut      []string
		wantCreated  []string
		wantPatched  []string
		wantBindings []any
		wantErr      string
	}{
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			run:     newFakeRun(map[string]map[string]any{}),
			wantOut: []string{tornDown},
		},
		{
			name:     "both jobs are made from their templates on this image, named after the version, the job process's with the template's policy, and neither is run",
			env:      environment,
			run:      newFakeRun(templates()),
			policies: map[string]map[string]any{jobsTemplate: templatePolicy},
			wantOut: []string{
				"=== Making job [harbor-migrate-v1-2-3] from [harbor-migrate] on this image ===",
				"Job harbor-migrate-v1-2-3 created: deploy migrate runs it once and deletes it.",
				"=== Making job [harbor-jobs-v1-2-3] from [harbor-jobs] on this image ===",
				"Job harbor-jobs-v1-2-3 created: the revision this build deploys starts it through the Cloud Run API; the pipeline does not run it.",
				"Job harbor-jobs-v1-2-3 may be started by serviceAccount:harbor-app@tst-project.iam.gserviceaccount.com (roles/run.invoker), as the template's IAM policy says.",
			},
			wantCreated:  []string{migrateJob, jobsJob},
			wantBindings: templateBindings,
		},
		{
			name:         "a build without migrations makes no migrate job",
			env:          strings.Replace(environment, `RUN_MIGRATIONS="true"`, `RUN_MIGRATIONS="false"`, 1),
			run:          newFakeRun(templates()),
			policies:     map[string]map[string]any{jobsTemplate: templatePolicy},
			wantOut:      []string{"No migrate job: this build does not run migrations.", "Job harbor-jobs-v1-2-3 created"},
			wantCreated:  []string{jobsJob},
			wantBindings: templateBindings,
		},
		{
			name:        "an application without a job process gets its migrate job alone",
			env:         strings.Replace(environment, "export JOBS_JOB=\"us-central1=harbor-jobs\"\n", "export JOBS_JOB=\"\"\n", 1),
			run:         newFakeRun(templates()),
			wantOut:     []string{"Job harbor-migrate-v1-2-3 created: deploy migrate runs it once and deletes it.", "No job for a job process: the stack's substitutions name no template (_JOBS_JOB)"},
			wantCreated: []string{migrateJob},
		},
		{
			name:         "a version deployed before updates the jobs it made then, policy included",
			env:          environment,
			run:          newFakeRun(existing()),
			policies:     map[string]map[string]any{jobsTemplate: templatePolicy, jobsJob: {keyBindings: []any{map[string]any{"role": "roles/run.invoker", "members": []any{"serviceAccount:someone-else@tst-project.iam.gserviceaccount.com"}}}, keyEtag: "etag-of-job"}},
			wantOut:      []string{"Job harbor-migrate-v1-2-3 updated: deploy migrate runs it once and deletes it.", "Job harbor-jobs-v1-2-3 updated: the revision this build deploys starts it", "may be started by serviceAccount:harbor-app@tst-project.iam.gserviceaccount.com (roles/run.invoker)"},
			wantPatched:  []string{migrateJob, jobsJob},
			wantBindings: templateBindings,
		},
		{
			name:        "a template that grants nothing leaves the job with no starter",
			env:         environment,
			run:         newFakeRun(templates()),
			wantOut:     []string{"Job harbor-jobs-v1-2-3 has no starter: the template's IAM policy grants nothing."},
			wantCreated: []string{migrateJob, jobsJob},
		},
		{
			name:    "a template the project lacks is refused",
			env:     environment,
			run:     newFakeRun(map[string]map[string]any{}),
			wantErr: "Cloud Run answered HTTP 404 to GET /v2/" + migrateTemplate,
		},
		{
			name:    "a build without a version is refused",
			env:     strings.Replace(environment, "export VERSION=\"v1.2.3\"\n", "", 1),
			run:     newFakeRun(templates()),
			wantErr: "environment.sh names no version (VERSION): the resolve step writes it",
		},
		{
			name:    "a workspace without the digest is refused",
			env:     strings.Replace(environment, "export IMAGE_DIGEST=\"sha256:abc\"\n", "", 1),
			run:     newFakeRun(map[string]map[string]any{}),
			wantErr: "environment.sh names no image digest (IMAGE, IMAGE_DIGEST): the image build writes it",
		},
		{
			name:    "a job that is not region=name is refused",
			env:     strings.Replace(environment, "us-central1=harbor-jobs", "harbor-jobs", 1),
			run:     newFakeRun(templates()),
			wantErr: `JOBS_JOB "harbor-jobs" is not region=name`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: build})
			for name, policy := range tt.policies {
				tt.run.policies[name] = policy
			}
			var out strings.Builder
			err := Jobs(t.Context(), &Clients{Run: tt.run.open}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Jobs() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out.String())
				}

				return
			}
			if err != nil {
				t.Fatalf("Jobs() error = %v; output:\n%s", err, out.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if len(tt.run.ran) != 0 {
				t.Errorf("the step ran a job: %v", tt.run.ran)
			}
			if diff := cmp.Diff(tt.wantCreated, tt.run.created); diff != "" {
				t.Errorf("created mismatch (-want +got):\n%s", diff)
			}
			var patched []string
			for _, name := range []string{migrateJob, jobsJob} {
				if tt.run.patched[name] != nil {
					patched = append(patched, name)
				}
			}
			if diff := cmp.Diff(tt.wantPatched, patched); diff != "" {
				t.Errorf("patched mismatch (-want +got):\n%s", diff)
			}
			made := append(append([]string{}, tt.wantCreated...), tt.wantPatched...)
			for _, name := range made {
				job := tt.run.resources[name]
				if tt.run.patched[name] != nil {
					job = tt.run.patched[name]
				}
				task, _ := field(job, "template.template").(map[string]any)
				container, err := firstContainer(task)
				if err != nil || container["image"] != "reg/harbor@sha256:abc" {
					t.Errorf("%s image = %v, want reg/harbor@sha256:abc", shortName(name), container["image"])
				}
				if diff := cmp.Diff(wantLabels, job["labels"]); diff != "" {
					t.Errorf("%s labels mismatch (-want +got):\n%s", shortName(name), diff)
				}
				for _, key := range []string{"uid", "createTime"} {
					if _, ok := job[key]; ok {
						t.Errorf("%s carries the template's %s", shortName(name), key)
					}
				}
			}
			if len(made) > 0 && !strings.Contains(tt.wantOut[0], "No migrate job") {
				if text(tt.run.resources[migrateJob], "template.template.serviceAccount") != migrateAccount && tt.run.patched[migrateJob] == nil {
					t.Errorf("the migrate copy lost the template's identity: %v", tt.run.resources[migrateJob])
				}
			}
			for _, template := range []string{migrateTemplate, jobsTemplate} {
				if doc := tt.run.resources[template]; doc != nil && text(doc, "template.template.containers.0.image") == "reg/harbor@sha256:abc" {
					t.Errorf("the template %s took the image", shortName(template))
				}
			}
			if _, ok := tt.run.policies[migrateJob]; ok {
				t.Error("the migrate job's policy was set: nothing but the pipeline runs it")
			}
			if tt.wantBindings == nil {
				if tt.run.policies[jobsJob] != nil && tt.policies[jobsTemplate] != nil {
					t.Errorf("the job process's policy was set from nothing: %v", tt.run.policies[jobsJob])
				}

				return
			}
			if diff := cmp.Diff([]string{jobsJob}, tt.run.policySets); diff != "" {
				t.Errorf("policies set mismatch (-want +got):\n%s", diff)
			}
			set := tt.run.policies[jobsJob]
			if diff := cmp.Diff(tt.wantBindings, set[keyBindings]); diff != "" {
				t.Errorf("job bindings mismatch (-want +got):\n%s", diff)
			}
			if set["version"] != tt.policies[jobsTemplate]["version"] {
				t.Errorf("job policy version = %v, want the template's %v", set["version"], tt.policies[jobsTemplate]["version"])
			}
			if tt.run.policies[jobsTemplate][keyEtag] != tt.policies[jobsTemplate][keyEtag] {
				t.Error("the template's policy was set")
			}
		})
	}
}
