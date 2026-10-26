package deploy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// The revision templates the tests' stacks say: one service per region of a flat
// application (harbor-app in both regions, as the deploy tests name it), and the job
// process's job.
const (
	testServiceTemplateJSON = `{"serviceAccount": "harbor-app@tst-project.iam.gserviceaccount.com", "maxInstances": 3, "timeout": "300s", "port": 8080, "http2": true, "args": [], "cpu": "1", "memory": "512Mi", "labels": {"terraform": "true", "application": "harbor"}, "env": {"APP_SERVICE_NAME": "harbor-app", "APP_FILE_STORE": "gs://files"}, "secrets": {"APP_COOKIE_KEY": {"secret": "imp-tst-gbl-harbor-cookie-key", "version": "3"}}}`
	testJobTemplateJSON     = `{"serviceAccount": "harbor-jobs@tst-project.iam.gserviceaccount.com", "timeout": "1800s", "maxRetries": 0, "cpu": "1", "memory": "512Mi", "command": ["/jobs"], "labels": {"terraform": "true", "application": "harbor"}, "env": {"APP_SERVICE_NAME": "harbor-jobs"}, "secrets": {}}`
)

// revisionTemplatesJSON is the stack's revision_templates output value, with the
// service template under each name given and the job template when withJob.
func revisionTemplatesJSON(withJob bool, services ...string) string {
	entries := make([]string, 0, len(services))
	for _, name := range services {
		entries = append(entries, `"`+name+`": `+testServiceTemplateJSON)
	}
	job := "null"
	if withJob {
		job = testJobTemplateJSON
	}

	return `{"services": {` + strings.Join(entries, ", ") + `}, "job": ` + job + `}`
}

// testRevisionTemplates is the output parsed, for the workspace file the deploy steps read.
func testRevisionTemplates(t *testing.T, withJob bool, services ...string) *RevisionTemplates {
	t.Helper()

	templates, err := parseRevisionTemplates([]byte(revisionTemplatesJSON(withJob, services...)))
	if err != nil {
		t.Fatal(err)
	}

	return templates
}

func TestServiceTemplateDocument(t *testing.T) {
	t.Parallel()

	templates := testRevisionTemplates(t, false, "harbor-app")
	tpl := templates.Services["harbor-app"]
	got := asJSON(t, tpl.document("reg/harbor@sha256:abc"))
	want := asJSON(t, parseJSON(t, `{
		"serviceAccount": "harbor-app@tst-project.iam.gserviceaccount.com",
		"executionEnvironment": "EXECUTION_ENVIRONMENT_GEN2",
		"timeout": "300s",
		"labels": {"terraform": "true", "application": "harbor"},
		"scaling": {"maxInstanceCount": 3},
		"containers": [{
			"image": "reg/harbor@sha256:abc",
			"ports": [{"containerPort": 8080, "name": "h2c"}],
			"resources": {"limits": {"cpu": "1", "memory": "512Mi"}, "cpuIdle": true, "startupCpuBoost": true},
			"env": [
				{"name": "APP_COOKIE_KEY", "valueSource": {"secretKeyRef": {"secret": "imp-tst-gbl-harbor-cookie-key", "version": "3"}}},
				{"name": "APP_FILE_STORE", "value": "gs://files"},
				{"name": "APP_SERVICE_NAME", "value": "harbor-app"}
			]
		}]
	}`))
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("document() mismatch (-want +got):\n%s", diff)
	}

	// No cap, an HTTP/1 port and a site's arguments: no scaling, no port name, the
	// arguments on the container.
	tpl.MaxInstances, tpl.HTTP2, tpl.Args = nil, false, []string{"/console"}
	doc := tpl.document("img")
	if _, ok := doc["scaling"]; ok {
		t.Errorf("document() sets scaling without a cap: %v", doc["scaling"])
	}
	containers, _ := doc["containers"].([]any)
	container, _ := containers[0].(map[string]any)
	if diff := cmp.Diff([]any{map[string]any{"containerPort": 8080}}, container["ports"]); diff != "" {
		t.Errorf("ports mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]any{"/console"}, container["args"]); diff != "" {
		t.Errorf("args mismatch (-want +got):\n%s", diff)
	}
}

func TestJobTemplateDocument(t *testing.T) {
	t.Parallel()

	templates := testRevisionTemplates(t, true, "harbor-app")
	got := templates.Job.document("reg/harbor@sha256:abc")
	labels := map[string]any{"terraform": "true", "application": "harbor"}
	want := map[string]any{
		"labels": labels,
		"template": map[string]any{
			"taskCount": 1,
			"labels":    labels,
			"template": map[string]any{
				"serviceAccount":       "harbor-jobs@tst-project.iam.gserviceaccount.com",
				"executionEnvironment": "EXECUTION_ENVIRONMENT_GEN2",
				"timeout":              "1800s",
				"maxRetries":           0,
				"containers": []any{map[string]any{
					"image":     "reg/harbor@sha256:abc",
					"command":   []any{"/jobs"},
					"resources": map[string]any{"limits": map[string]any{"cpu": "1", "memory": "512Mi"}},
					"env":       []any{map[string]any{"name": "APP_SERVICE_NAME", "value": "harbor-jobs"}},
				}},
			},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("document() mismatch (-want +got):\n%s", diff)
	}
}

func TestParseRevisionTemplates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{name: "the output, services and the job", value: revisionTemplatesJSON(true, "harbor-app")},
		{name: "no job", value: revisionTemplatesJSON(false, "harbor-app")},
		{name: "no service is refused", value: `{"services": {}, "job": null}`, wantErr: "names no service"},
		{name: "not JSON is refused", value: "{", wantErr: "the revision_templates output"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseRevisionTemplates([]byte(tt.value))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseRevisionTemplates() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("parseRevisionTemplates() error = %v", err)
			}
			if _, ok := got.Services["harbor-app"]; !ok {
				t.Errorf("Services lacks harbor-app: %v", got.Services)
			}
		})
	}
}

func TestPlannedRevisionTemplates(t *testing.T) {
	t.Parallel()

	planWith := func(planned, changes string) string {
		return `{"resource_changes": [], "output_changes": {` + changes + `}, "planned_values": {"outputs": {` + planned + `}}}`
	}
	known := `"revision_templates": {"sensitive": false, "value": ` + revisionTemplatesJSON(false, "harbor-app") + `}`
	tests := []struct {
		name    string
		plan    string
		wantErr string
	}{
		{name: "the output as the plan evaluates it", plan: planWith(known, `"revision_templates": {"actions": ["update"], "before": {}, "after": `+revisionTemplatesJSON(false, "harbor-app")+`, "after_unknown": false}`)},
		{name: "an output the plan does not change", plan: planWith(known, "")},
		{name: "a value known only after the apply is refused", plan: planWith(known, `"revision_templates": {"actions": ["update"], "before": {}, "after": {"services": {"harbor-app": {"env": {}}}}, "after_unknown": {"services": {"harbor-app": {"env": {"APP_FILE_STORE": true}}}}}`), wantErr: "not known until the apply"},
		{name: "a plan without the output is refused", plan: planWith("", ""), wantErr: "the plan has no revision_templates output"},
		{name: "not JSON is refused", plan: "{", wantErr: "the plan's JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := plannedRevisionTemplates([]byte(tt.plan))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("plannedRevisionTemplates() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("plannedRevisionTemplates() error = %v", err)
			}
			if _, ok := got.Services["harbor-app"]; !ok {
				t.Errorf("Services lacks harbor-app")
			}
		})
	}
}

func TestTemplateChanges(t *testing.T) {
	t.Parallel()

	before := testRevisionTemplates(t, true, "harbor-app")
	changed := testRevisionTemplates(t, true, "harbor-app")
	svc := changed.Services["harbor-app"]
	svc.Env = map[string]string{"APP_SERVICE_NAME": "harbor-app", "APP_NEW": "x"}
	five := 5
	svc.MaxInstances = &five
	svc.Secrets = parseSecrets(t, `{"APP_COOKIE_KEY": {"secret": "imp-tst-gbl-harbor-cookie-key", "version": "4"}, "APP_MAIL_KEY": {"secret": "imp-tst-gbl-harbor-mail-key", "version": "1"}}`)
	changed.Services["harbor-app"] = svc
	changed.Services["harbor-portal"] = testRevisionTemplates(t, false, "harbor-portal").Services["harbor-portal"]
	changed.Job.Timeout = "900s"
	planOf := func(before, after any, unknown any) string {
		doc := map[string]any{"resource_changes": []any{}, "output_changes": map[string]any{revisionTemplatesOutput: map[string]any{"actions": []string{"update"}, "before": before, "after": after, "after_unknown": unknown}}}
		data, _ := json.Marshal(doc)

		return string(data)
	}
	tests := []struct {
		name string
		plan string
		want []string
	}{
		{
			name: "the variables, the secret mounts, the scaling, a new service and the job, in order",
			plan: planOf(before, changed, false),
			want: []string{
				"harbor-app: -APP_FILE_STORE; +APP_NEW=x; secret APP_COOKIE_KEY version 3 -> 4; +secret APP_MAIL_KEY (version 1); max instances 3 -> 5",
				"harbor-portal: a new service; its revision runs with 2 variable(s) and 1 secret mount(s)",
				"the job process's job: timeout and retries 1800s/0 -> 900s/0",
			},
		},
		{
			name: "a stack without the output yet: every service new",
			plan: planOf(nil, before, false),
			want: []string{"harbor-app: a new service; its revision runs with 2 variable(s) and 1 secret mount(s)", "the job process's job: new"},
		},
		{
			name: "a value known after the apply is noted",
			plan: planOf(before, changed, map[string]any{"services": map[string]any{"harbor-app": map[string]any{"env": map[string]any{"APP_NEW": true}}}}),
			want: []string{
				"harbor-app: -APP_FILE_STORE; +APP_NEW=x; secret APP_COOKIE_KEY version 3 -> 4; +secret APP_MAIL_KEY (version 1); max instances 3 -> 5",
				"harbor-portal: a new service; its revision runs with 2 variable(s) and 1 secret mount(s)",
				"the job process's job: timeout and retries 1800s/0 -> 900s/0",
				"some of the templates' values are known after the apply (a resource the release makes)",
			},
		},
		{name: "an output the plan does not change says nothing", plan: `{"resource_changes": [], "output_changes": {"revision_templates": {"actions": ["no-op"]}}}`},
		{name: "a plan without the output says nothing", plan: stackPlanJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := templateChanges([]byte(tt.plan))
			if err != nil {
				t.Fatalf("templateChanges() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("templateChanges() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// parseJSON reads a JSON document into a value, and asJSON writes one out as the API
// would receive it, so that expectations are read from JSON text.
func parseJSON(t *testing.T, text string) any {
	t.Helper()

	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatal(err)
	}

	return v
}

func asJSON(t *testing.T, v any) string {
	t.Helper()

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// parseSecrets reads secret mounts from JSON text.
func parseSecrets(t *testing.T, text string) map[string]SecretReference {
	t.Helper()

	var secrets map[string]SecretReference
	if err := json.Unmarshal([]byte(text), &secrets); err != nil {
		t.Fatal(err)
	}

	return secrets
}

func TestReadRevisionTemplatesMissing(t *testing.T) {
	t.Parallel()

	w := workspaceFiles(t, map[string]string{})
	_, err := readRevisionTemplates(w)
	if err == nil || !strings.Contains(err.Error(), "the workspace has no revision templates (revision-templates.json)") {
		t.Fatalf("readRevisionTemplates() error = %v", err)
	}
	templates := testRevisionTemplates(t, true, "harbor-app")
	if err := writeRevisionTemplates(w, templates); err != nil {
		t.Fatal(err)
	}
	got, err := readRevisionTemplates(w)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(templates, got); diff != "" {
		t.Errorf("round trip mismatch (-want +got):\n%s", diff)
	}
}
