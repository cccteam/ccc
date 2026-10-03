// jobs.go makes each build's job of the job process from the stack's template job, for an
// application with one, right after the image build and before anything touches the
// database.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/go-playground/errors/v5"
)

// Jobs creates this build's job for the job process (cmd/jobs), when the application has
// one: a copy of the template job the stack owns (named by the substitution _JOBS_JOB;
// never run, never deployed to), named <template>-<version key>, put on this build's image
// with the pipeline's labels and given the template's IAM policy: the image the build made
// names it to the site (APP_JOBS_JOB), so the revision this build deploys starts a job of
// its own code, and a traffic rollback to an earlier revision starts that revision's job;
// only the running service starts it, the pipeline never does. The step runs right after
// the image build, before the migrations and before anything the run waits for: making a
// job touches no data, so a failure here stops the run with the database untouched. A
// build of a version this environment deployed before updates the job it made then, the
// same code. An application without a job process has nothing to make, and the pipeline
// has no step for it; a torn-down pull-request environment has nothing to make either.
func Jobs(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if env[skipDeploy] == trueValue {
		fmt.Fprintln(out, skipped(env))

		return nil
	}
	if env[jobsJobFact] == "" {
		fmt.Fprintln(out, "No job to make: the stack's substitutions name no template for a job process (_JOBS_JOB), the application having no cmd/jobs.")

		return nil
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	image, err := builtImage(env)
	if err != nil {
		return err
	}
	run, err := clients.Run(ctx)
	if err != nil {
		return err
	}
	project := build.Substitutions[projectSub]
	template, name, err := buildJob(project, env, jobsJobFact)
	if err != nil {
		return err
	}
	verb, err := makeJob(ctx, run, template, name, image, pipelineLabels(build, env[versionFact]), out)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Job %s %s: the revision this build deploys starts it through the Cloud Run API; the pipeline does not run it.\n", shortName(name), verb)
	starters, err := copyPolicy(ctx, run, template, name)
	if err != nil {
		return err
	}
	if starters == "" {
		fmt.Fprintf(out, "Job %s has no starter: the template's IAM policy grants nothing. The stack grants run.invoker on the template to the site's identity when the site declares the job variable.\n", shortName(name))
	} else {
		fmt.Fprintf(out, "Job %s may be started by %s, as the template's IAM policy says.\n", shortName(name), starters)
	}

	return nil
}

// makeJob creates or updates the build's job from the template on the image with the
// labels, and answers which it did.
func makeJob(ctx context.Context, run Run, template, name, image string, labels map[string]string, out io.Writer) (string, error) {
	fmt.Fprintf(out, "=== Making job [%s] from [%s] on this image ===\n", shortName(name), shortName(template))
	doc, err := run.Get(ctx, template)
	if err != nil {
		return "", err
	}
	job, err := jobFromTemplate(doc, image, labels)
	if err != nil {
		return "", errors.Wrapf(err, "template job %s", shortName(template))
	}
	switch _, err := run.Get(ctx, name); {
	case err == nil:
		if _, err := run.Patch(ctx, name, job); err != nil {
			return "", err
		}

		return "updated", nil
	case isNotFound(err):
		parent, id := parentAndID(name)
		if _, err := run.CreateJob(ctx, parent, id, job); err != nil {
			return "", err
		}

		return "created", nil
	default:
		return "", err
	}
}

// buildJob names the template job the fact's substitution names (region=name) and this
// build's copy of it, as the Cloud Run API names them.
func buildJob(project string, env map[string]string, fact string) (template, name string, err error) {
	region, templateName, err := target(fact, env[fact])
	if err != nil {
		return "", "", err
	}
	key := versionKey(env[versionFact])
	if key == "" {
		return "", "", errors.Newf("%s names no version (VERSION): the resolve step writes it", EnvironmentFile)
	}
	prefix := jobPrefix(project, region, templateName)

	return strings.TrimSuffix(prefix, "-"), prefix + key, nil
}

// jobPrefix is what the names of a template job's copies start with: the template's
// resource name and a hyphen.
func jobPrefix(project, region, template string) string {
	return "projects/" + project + "/locations/" + region + "/jobs/" + template + "-"
}

// versionKey is the build's version as a name: lowercase, every run of characters
// outside a-z and 0-9 one hyphen, none at either end (v0.1.15 is v0-1-15, pr39@abc1234
// is pr39-abc1234).
func versionKey(version string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(version) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			hyphen = false
		case !hyphen && b.Len() > 0:
			b.WriteByte('-')
			hyphen = true
		}
	}

	return strings.TrimSuffix(b.String(), "-")
}

// parentAndID splits a job's resource name into its parent collection and its id.
func parentAndID(name string) (parent, id string) {
	i := strings.LastIndex(name, "/jobs/")
	if i < 0 {
		return "", name
	}

	return name[:i], name[i+len("/jobs/"):]
}

// jobFromTemplate is the job to create or update from the template job's document: its
// template, labels and annotations copied, the image set and the labels overlaid. The
// rest of the document is the API's own (name, times, conditions) and is left behind.
func jobFromTemplate(doc map[string]any, image string, labels map[string]string) (map[string]any, error) {
	job := map[string]any{}
	for _, key := range []string{keyTemplate, keyLabels, "annotations"} {
		if value, ok := doc[key]; ok {
			copied, err := deepCopy(value)
			if err != nil {
				return nil, err
			}
			job[key] = copied
		}
	}
	task, _ := field(job, keyTemplate+"."+keyTemplate).(map[string]any)
	container, err := firstContainer(task)
	if err != nil {
		return nil, err
	}
	container["image"] = image
	setLabels(job, labels)

	return job, nil
}

// deepCopy copies a document's value through its JSON form.
func deepCopy(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}
	var copied any
	if err := json.Unmarshal(data, &copied); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal()")
	}

	return copied, nil
}

// copyPolicy puts the template job's IAM policy on the build's job, bindings and version,
// under the etag of the job's own policy, and answers who holds what on it (role by role,
// its members), or empty when the template grants nothing.
func copyPolicy(ctx context.Context, run Run, template, name string) (string, error) {
	source, err := run.GetIamPolicy(ctx, template)
	if err != nil {
		return "", err
	}
	current, err := run.GetIamPolicy(ctx, name)
	if err != nil {
		return "", err
	}
	bindings, _ := source[keyBindings].([]any)
	policy := map[string]any{keyBindings: bindings, keyEtag: current[keyEtag]}
	if version, ok := source["version"]; ok {
		policy["version"] = version
	}
	if _, err := run.SetIamPolicy(ctx, name, policy); err != nil {
		return "", err
	}
	var holders []string
	for _, entry := range bindings {
		binding, _ := entry.(map[string]any)
		members, _ := binding["members"].([]any)
		names := make([]string, 0, len(members))
		for _, member := range members {
			names = append(names, fmt.Sprint(member))
		}
		holders = append(holders, strings.Join(names, ", ")+" ("+text(binding, "role")+")")
	}

	return strings.Join(holders, "; "), nil
}
