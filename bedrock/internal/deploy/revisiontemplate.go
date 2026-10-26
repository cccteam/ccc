// revisiontemplate.go is the revision template of each service, and of the job process's
// job, as the release's stack says it: the stack's revision_templates output (identity,
// scaling, variables, secret mounts, as the stack evaluates them), which the deploy steps
// build every revision and job from with the release's image, so that a release runs
// under its own release's configuration, forward or back, and no stack apply starts a
// revision. A tag build reads the output off the stack it applied (deploy stack apply),
// or, in a rollback run, off the plan of the release's stack, which is read and not
// applied (deploy stack plan); a preview build reads it from its stack job's outputs
// (deploy pr-outputs). Each leaves it in the workspace for deploy service and deploy
// jobs.

package deploy

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"
)

const (
	// RevisionTemplatesFile is the workspace file holding the release's revision
	// templates, as the stack's output says them.
	RevisionTemplatesFile = "revision-templates.json"
	// revisionTemplatesOutput is the stack output they are read from.
	revisionTemplatesOutput = "revision_templates"
	// executionEnvironmentGen2 is the execution environment every revision and job runs
	// in, as the stack's templates set it.
	executionEnvironmentGen2 = "EXECUTION_ENVIRONMENT_GEN2"
	// h2cPortName names a service's port h2c, so that Cloud Run talks HTTP/2 to the
	// container.
	h2cPortName = "h2c"
)

// RevisionTemplates is the stack's revision_templates output: each service's revision
// template by the service's name, and the job process's job template, or nil for an
// application without a job process.
type RevisionTemplates struct {
	Services map[string]ServiceTemplate `json:"services"`
	Job      *JobTemplate               `json:"job"`
}

// ServiceTemplate is what one service's revision runs with.
type ServiceTemplate struct {
	ServiceAccount string `json:"serviceAccount"`
	// MaxInstances is the placement's cap for the environment, or nil for Cloud Run's
	// default.
	MaxInstances *int   `json:"maxInstances"`
	Timeout      string `json:"timeout"`
	Port         int    `json:"port"`
	// HTTP2 names the port h2c, so that Cloud Run talks HTTP/2 to the container: the
	// site's main package imports the framework's server.
	HTTP2 bool `json:"http2"`
	// Args are the container's arguments: the site's binary in the sites layout, none
	// in the flat one.
	Args   []string          `json:"args"`
	CPU    string            `json:"cpu"`
	Memory string            `json:"memory"`
	Labels map[string]string `json:"labels"`
	// Env are the variables with a value, and Secrets the ones mounted from Secret
	// Manager, by variable.
	Env     map[string]string          `json:"env"`
	Secrets map[string]SecretReference `json:"secrets"`
}

// SecretReference is a secret mount: the container's id and the pinned version.
type SecretReference struct {
	Secret  string `json:"secret"`
	Version string `json:"version"`
}

// JobTemplate is what the job process's job runs with.
type JobTemplate struct {
	ServiceAccount string                     `json:"serviceAccount"`
	Timeout        string                     `json:"timeout"`
	MaxRetries     int                        `json:"maxRetries"`
	CPU            string                     `json:"cpu"`
	Memory         string                     `json:"memory"`
	Command        []string                   `json:"command"`
	Labels         map[string]string          `json:"labels"`
	Env            map[string]string          `json:"env"`
	Secrets        map[string]SecretReference `json:"secrets"`
}

// parseRevisionTemplates reads the output's value.
func parseRevisionTemplates(value []byte) (*RevisionTemplates, error) {
	var t RevisionTemplates
	if err := json.Unmarshal(value, &t); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): the %s output", revisionTemplatesOutput)
	}
	if len(t.Services) == 0 {
		return nil, errors.Newf("the %s output names no service", revisionTemplatesOutput)
	}

	return &t, nil
}

// revisionTemplatesOf reads the output off the stack's outputs (tofu output -json, by
// name), refusing a stack without it in plain words: applied by a bedrock before the
// output, or never.
func revisionTemplatesOf(outputs map[string]json.RawMessage) (*RevisionTemplates, error) {
	value, ok := outputs[revisionTemplatesOutput]
	if !ok {
		return nil, errors.Newf("the stack's state has no %s output: never applied, or applied by a bedrock before the output; the operations workflow's apply action applies the stack with this one", revisionTemplatesOutput)
	}

	return parseRevisionTemplates(value)
}

// plannedRevisionTemplates reads the output off a plan's JSON (tofu show -json), as the
// plan evaluated the release's stack: what a rollback run deploys, since it applies no
// stack. A plan without the output (a stack rendered by a bedrock before it) and one
// whose value is not known until the apply are refused.
func plannedRevisionTemplates(plan []byte) (*RevisionTemplates, error) {
	var doc planDocument
	if err := json.Unmarshal(plan, &doc); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): the plan's JSON")
	}
	if change, ok := doc.OutputChanges[revisionTemplatesOutput]; ok && unknown(change.AfterUnknown) {
		return nil, errors.Newf("the plan's %s output is not known until the apply: the release's stack makes something a revision runs with, which a rollback run never applies", revisionTemplatesOutput)
	}
	output, ok := doc.PlannedValues.Outputs[revisionTemplatesOutput]
	if !ok || len(output.Value) == 0 {
		return nil, errors.Newf("the plan has no %s output: the release's stack was rendered by a bedrock before the output, and its revision templates cannot be read without applying it", revisionTemplatesOutput)
	}

	return parseRevisionTemplates(output.Value)
}

// unknown reports a plan's after_unknown marking anything unknown: true, or an object
// with any true in it.
func unknown(v any) bool {
	switch v := v.(type) {
	case bool:
		return v
	case map[string]any:
		for _, inner := range v {
			if unknown(inner) {
				return true
			}
		}
	case []any:
		for _, inner := range v {
			if unknown(inner) {
				return true
			}
		}
	}

	return false
}

// writeRevisionTemplates leaves the templates in the workspace for the deploy steps.
func writeRevisionTemplates(w Workspace, t *RevisionTemplates) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return errors.Wrap(err, "json.MarshalIndent()")
	}
	if err := os.WriteFile(filepath.Join(string(w), RevisionTemplatesFile), data, 0o600); err != nil {
		return errors.Wrapf(err, "os.WriteFile(): %s", RevisionTemplatesFile)
	}

	return nil
}

// readRevisionTemplates reads the templates a stack step left in the workspace, saying
// which step writes them when none did.
func readRevisionTemplates(w Workspace) (*RevisionTemplates, error) {
	data, err := os.ReadFile(filepath.Join(string(w), RevisionTemplatesFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.Newf("the workspace has no revision templates (%s): deploy stack apply writes them off the stack it applied (deploy stack plan in a rollback run), and deploy pr-outputs off the pull request's stack job", RevisionTemplatesFile)
		}

		return nil, errors.Wrapf(err, "os.ReadFile(): %s", RevisionTemplatesFile)
	}

	return parseRevisionTemplates(data)
}

// document is the revision template as the Cloud Run API takes it, on the image: the
// identity, the execution environment, the timeout, the scaling when the placement caps
// the environment, the stack's labels, and the one container with its arguments, its
// port, its resources and its variables, the plain ones and the secret mounts together
// in name order.
func (t *ServiceTemplate) document(image string) map[string]any {
	port := map[string]any{"containerPort": t.Port}
	if t.HTTP2 {
		port[keyName] = h2cPortName
	}
	container := map[string]any{
		"image":     image,
		"ports":     []any{port},
		"resources": map[string]any{"limits": map[string]any{"cpu": t.CPU, "memory": t.Memory}, "cpuIdle": true, "startupCpuBoost": true},
		"env":       envEntries(t.Env, t.Secrets),
	}
	if len(t.Args) > 0 {
		container["args"] = anyStrings(t.Args)
	}
	template := map[string]any{
		"serviceAccount":       t.ServiceAccount,
		"executionEnvironment": executionEnvironmentGen2,
		"timeout":              t.Timeout,
		keyLabels:              anyLabels(t.Labels),
		"containers":           []any{container},
	}
	if t.MaxInstances != nil {
		template["scaling"] = map[string]any{"maxInstanceCount": *t.MaxInstances}
	}

	return template
}

// document is the job as the Cloud Run API takes it, on the image: one task, the
// stack's labels on the job and its template, and the template with the identity, the
// execution environment, the timeout, the retries and the one container with its
// command, its resources and its variables.
func (t *JobTemplate) document(image string) map[string]any {
	container := map[string]any{
		"image":     image,
		"command":   anyStrings(t.Command),
		"resources": map[string]any{"limits": map[string]any{"cpu": t.CPU, "memory": t.Memory}},
		"env":       envEntries(t.Env, t.Secrets),
	}
	task := map[string]any{
		"serviceAccount":       t.ServiceAccount,
		"executionEnvironment": executionEnvironmentGen2,
		"timeout":              t.Timeout,
		"maxRetries":           t.MaxRetries,
		"containers":           []any{container},
	}

	return map[string]any{
		keyLabels:   anyLabels(t.Labels),
		keyTemplate: map[string]any{"taskCount": 1, keyLabels: anyLabels(t.Labels), keyTemplate: task},
	}
}

// envEntries are the container's variables as the API takes them, in name order: a
// plain variable with its value, a secret mounted by reference at its version.
func envEntries(env map[string]string, secrets map[string]SecretReference) []any {
	names := slices.Sorted(maps.Keys(env))
	for name := range secrets {
		if _, plain := env[name]; !plain {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	entries := make([]any, 0, len(names))
	for _, name := range names {
		if ref, ok := secrets[name]; ok {
			entries = append(entries, map[string]any{keyName: name, "valueSource": map[string]any{"secretKeyRef": map[string]any{"secret": ref.Secret, "version": ref.Version}}})

			continue
		}
		entries = append(entries, map[string]any{keyName: name, keyValue: env[name]})
	}

	return entries
}

func anyStrings(values []string) []any {
	out := make([]any, 0, len(values))
	for _, v := range values {
		out = append(out, v)
	}

	return out
}

func anyLabels(labels map[string]string) map[string]any {
	out := make(map[string]any, len(labels))
	for k, v := range labels {
		out[k] = v
	}

	return out
}

// templateChanges reads a plan's JSON for what the release changes in the revision
// templates against the stack as last applied (the output's before and after), one line
// per service and one for the job: the variables added, removed and changed, the secret
// mounts moved, the scaling and the identity. None when the output does not change, and
// a note when a value is known only after the apply. What the pull request's plan used
// to show as a change to the service resource, since the template left it.
func templateChanges(plan []byte) ([]string, error) {
	var doc planDocument
	if err := json.Unmarshal(plan, &doc); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): the plan's JSON")
	}
	change, ok := doc.OutputChanges[revisionTemplatesOutput]
	if !ok || slices.Contains(change.Actions, "no-op") {
		return nil, nil
	}
	before, after := templatesValue(change.Before), templatesValue(change.After)
	var lines []string
	for _, name := range slices.Sorted(maps.Keys(after.Services)) {
		from, was := before.Services[name]
		to := after.Services[name]
		if !was {
			lines = append(lines, fmt.Sprintf("%s: a new service; its revision runs with %d variable(s) and %d secret mount(s)", name, len(to.Env), len(to.Secrets)))

			continue
		}
		if diff := serviceDiff(&from, &to); diff != "" {
			lines = append(lines, name+": "+diff)
		}
	}
	for name := range before.Services {
		if _, still := after.Services[name]; !still {
			lines = append(lines, name+": no longer a service of the stack")
		}
	}
	if diff := jobDiff(before.Job, after.Job); diff != "" {
		lines = append(lines, "the job process's job: "+diff)
	}
	if unknown(change.AfterUnknown) {
		lines = append(lines, "some of the templates' values are known after the apply (a resource the release makes)")
	}

	return lines, nil
}

// templatesValue reads an output's before or after, as the plan carries it; empty for
// null (no output in the state yet).
func templatesValue(v any) *RevisionTemplates {
	t := &RevisionTemplates{Services: map[string]ServiceTemplate{}}
	data, err := json.Marshal(v)
	if err != nil || v == nil {
		return t
	}
	_ = json.Unmarshal(data, t)
	if t.Services == nil {
		t.Services = map[string]ServiceTemplate{}
	}

	return t
}

// serviceDiff says what changes between two service templates; empty for none.
func serviceDiff(from, to *ServiceTemplate) string {
	parts := envDiff(from.Env, to.Env, from.Secrets, to.Secrets)
	if from.ServiceAccount != to.ServiceAccount {
		parts = append(parts, "identity "+from.ServiceAccount+" -> "+to.ServiceAccount)
	}
	if instances(from.MaxInstances) != instances(to.MaxInstances) {
		parts = append(parts, "max instances "+instances(from.MaxInstances)+" -> "+instances(to.MaxInstances))
	}
	if from.CPU != to.CPU || from.Memory != to.Memory {
		parts = append(parts, fmt.Sprintf("resources %s/%s -> %s/%s", from.CPU, from.Memory, to.CPU, to.Memory))
	}
	if from.Timeout != to.Timeout {
		parts = append(parts, "timeout "+from.Timeout+" -> "+to.Timeout)
	}
	if from.HTTP2 != to.HTTP2 || from.Port != to.Port || !slices.Equal(from.Args, to.Args) {
		parts = append(parts, "the port or the arguments change")
	}

	return strings.Join(parts, "; ")
}

// jobDiff says what changes between two job templates; empty for none.
func jobDiff(from, to *JobTemplate) string {
	switch {
	case from == nil && to == nil:
		return ""
	case from == nil:
		return "new"
	case to == nil:
		return "gone"
	}
	parts := envDiff(from.Env, to.Env, from.Secrets, to.Secrets)
	if from.ServiceAccount != to.ServiceAccount {
		parts = append(parts, "identity "+from.ServiceAccount+" -> "+to.ServiceAccount)
	}
	if from.Timeout != to.Timeout || from.MaxRetries != to.MaxRetries {
		parts = append(parts, fmt.Sprintf("timeout and retries %s/%d -> %s/%d", from.Timeout, from.MaxRetries, to.Timeout, to.MaxRetries))
	}
	if from.CPU != to.CPU || from.Memory != to.Memory {
		parts = append(parts, fmt.Sprintf("resources %s/%s -> %s/%s", from.CPU, from.Memory, to.CPU, to.Memory))
	}

	return strings.Join(parts, "; ")
}

// envDiff lists the variables added, removed and changed, and the secret mounts added,
// removed and moved to another version, in name order.
func envDiff(fromEnv, toEnv map[string]string, fromSecrets, toSecrets map[string]SecretReference) []string {
	var parts []string
	names := slices.Sorted(maps.Keys(fromEnv))
	for name := range toEnv {
		if _, had := fromEnv[name]; !had {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		from, had := fromEnv[name]
		to, has := toEnv[name]
		switch {
		case !had:
			parts = append(parts, "+"+name+"="+to)
		case !has:
			parts = append(parts, "-"+name)
		case from != to:
			parts = append(parts, name+" "+from+" -> "+to)
		}
	}
	mounts := slices.Sorted(maps.Keys(fromSecrets))
	for name := range toSecrets {
		if _, had := fromSecrets[name]; !had {
			mounts = append(mounts, name)
		}
	}
	slices.Sort(mounts)
	for _, name := range mounts {
		from, had := fromSecrets[name]
		to, has := toSecrets[name]
		switch {
		case !had:
			parts = append(parts, "+secret "+name+" (version "+to.Version+")")
		case !has:
			parts = append(parts, "-secret "+name)
		case from != to:
			parts = append(parts, "secret "+name+" version "+from.Version+" -> "+to.Version)
		}
	}

	return parts
}

func instances(n *int) string {
	if n == nil {
		return "default"
	}

	return strconv.Itoa(*n)
}
