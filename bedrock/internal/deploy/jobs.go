// jobs.go makes each build's job for the job process: a copy of the stack's template job on
// this build's image, named after the build's version, which the running service starts.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/go-playground/errors/v5"
)

// Jobs creates this build's job for the job process (cmd/jobs): a copy of the template
// job the stack owns (named by _JOBS_JOB, never run, never deployed to), named
// <template>-<version key> and put on this build's image with the pipeline's labels. The
// image the build made names that job to the site (APP_JOBS_JOB), so the revision this
// build deploys starts a job of its own code, and a traffic rollback to an earlier
// revision starts that revision's job. The pipeline never runs it: only the running
// service starts the job process. A build of a version this environment deployed before
// updates the job it made then, the same code. The template's IAM policy goes with it:
// the stack grants the site's identity run.invoker on the template, and the copy is what
// lets the site start the build's job. After the migrations, so a run the service starts
// from here on sees the migrated schema; before the service, so the job stands when its
// revision serves. A torn-down pull-request environment has nothing to make.
func Jobs(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if env[skipDeploy] == trueValue {
		fmt.Fprintln(out, tornDown)

		return nil
	}
	if env[jobsJobFact] == "" {
		return errors.Newf("%s names no job: the stack's substitutions carry _JOBS_JOB when the application has a job process (cmd/jobs), which this step is for; render and apply the stack", jobsJobFact)
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	image, err := builtImage(env)
	if err != nil {
		return err
	}
	template, name, err := buildJob(build.Substitutions[projectSub], env)
	if err != nil {
		return err
	}
	run, err := clients.Run(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "=== Making job [%s] from [%s] on this image ===\n", shortName(name), shortName(template))
	doc, err := run.Get(ctx, template)
	if err != nil {
		return err
	}
	job, err := jobFromTemplate(doc, image, pipelineLabels(build, env[versionFact]))
	if err != nil {
		return errors.Wrapf(err, "template job %s", shortName(template))
	}
	verb := "updated"
	switch _, err := run.Get(ctx, name); {
	case err == nil:
		if _, err := run.Patch(ctx, name, job); err != nil {
			return err
		}
	case isNotFound(err):
		verb = "created"
		parent, id := parentAndID(name)
		if _, err := run.CreateJob(ctx, parent, id, job); err != nil {
			return err
		}
	default:
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

// buildJob names the template job and this build's job, as the Cloud Run API names them,
// from the template the stack's substitutions name (_JOBS_JOB, region=name) and the
// build's version.
func buildJob(project string, env map[string]string) (template, name string, err error) {
	region, templateName, err := target(jobsJobFact, env[jobsJobFact])
	if err != nil {
		return "", "", err
	}
	key := versionKey(env[versionFact])
	if key == "" {
		return "", "", errors.Newf("%s names no version (VERSION): the resolve step writes it", EnvironmentFile)
	}
	parent := "projects/" + project + "/locations/" + region

	return parent + "/jobs/" + templateName, parent + "/jobs/" + templateName + "-" + key, nil
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
