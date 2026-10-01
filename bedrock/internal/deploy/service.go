package deploy

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cccteam/ccc/bedrock/internal/derive"

	"github.com/go-playground/errors/v5"
)

// The traffic target types Cloud Run names, and the condition state of a service at rest.
const (
	targetRevision     = "TRAFFIC_TARGET_ALLOCATION_TYPE_REVISION"
	targetLatest       = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"
	conditionSucceeded = "CONDITION_SUCCEEDED"
	fullTraffic        = float64(100)
)

// serviceName is the resource name of a service in a region.
func serviceName(project, region, service string) string {
	return "projects/" + project + "/locations/" + region + "/services/" + service
}

// Deploy puts a new revision of the service in every region, receiving no traffic yet.
// The service itself is the infrastructure's (created with its runtime identity, ingress
// and scaling); only the image and the pipeline's labels change. Before each update the
// service is checked for the state a failed earlier deploy leaves behind, traffic pointed
// at a revision that is not ready, and repaired by moving traffic back to the last ready
// revision, as CCC's deployments do. The traffic that serves now is pinned by revision
// name so the new revision takes none; a revision tag, when there is one, names the new
// revision under its own URL. One line per region is left for the next steps: region,
// service, revision.
func Deploy(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if env[skipDeploy] == trueValue {
		fmt.Fprintln(out, tornDown)

		return nil
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	if env[imageFact] == "" || env[digestFact] == "" {
		return errors.Newf("%s names no image digest (IMAGE, IMAGE_DIGEST): the image build writes it", EnvironmentFile)
	}
	image := env[imageFact] + "@" + env[digestFact]
	entries := strings.Split(env[services], ",")
	if env[services] == "" {
		return errors.Newf("%s names no services (SERVICES): the resolve step writes them", EnvironmentFile)
	}
	run, err := clients.Run(ctx)
	if err != nil {
		return err
	}
	labels := pipelineLabels(build, env[versionFact])
	// The new revision carries a tag from the start: the pull request's, when it is
	// served under one, else "next", the tag the stack's next backend serves before
	// traffic moves; the tag names one revision, so each deploy moves it.
	tag := env[revisionTagFact]
	if tag == "" {
		tag = nextTag
	}
	revisions := make([]Revision, 0, len(entries))
	urls := make([]string, 0, len(entries))
	for _, entry := range entries {
		region, service, err := target(services, entry)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "--- Service [%s] in region [%s] ---\n", service, region)
		name := serviceName(build.Substitutions[projectSub], region, service)
		doc, err := repair(ctx, run, name, service, out)
		if err != nil {
			return err
		}
		if clearMaintenance(doc) {
			fmt.Fprintf(out, "The maintenance revision's %s is cleared on the new revision: it serves the application.\n", derive.MaintenanceVariable)
		}
		fmt.Fprintf(out, "Deploying revision to [%s] without traffic...\n", service)
		revision, uri, err := deployRevision(ctx, run, name, doc, image, labels, tag)
		if err != nil {
			return errors.Wrapf(err, "the deployment to region %s", region)
		}
		fmt.Fprintf(out, "Revision [%s] deployed to [%s] in [%s] under the tag [%s]\n", revision, service, region, tag)
		revisions = append(revisions, Revision{Region: region, Service: service, Revision: revision})
		if url := taggedURL(uri, tag); url != "" {
			urls = append(urls, region+"="+url)
		}
	}
	if err := w.WriteRevisions(revisions); err != nil {
		return err
	}
	// What the hook before traffic calls: the next revision's public URL through the load
	// balancer (none for a pull request, whose environment is its own), and the tagged
	// run.app URL per region, which the services' ingress answers from inside alone.
	facts := map[string]string{
		nextURLFact:      nextURL(build.Substitutions[hostnameSub], build.Substitutions[prNumberSub]),
		revisionURLsFact: strings.Join(urls, ","),
	}
	if err := w.Append(facts); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s=%s %s=%s\n", nextURLFact, facts[nextURLFact], revisionURLsFact, facts[revisionURLsFact])

	return nil
}

// The tag every deploy puts on its new revision when the build serves under no other,
// and the facts the deploy leaves for the hook before traffic.
const (
	nextTag          = "next"
	nextURLFact      = "NEXT_URL"
	revisionURLsFact = "REVISION_URLS"
	hostnameSub      = "_HOSTNAME"
)

// taggedURL is the revision's own URL under its tag: the service's run.app URL with the
// tag and three dashes before its host. Empty when the service reports no URL.
func taggedURL(uri, tag string) string {
	host, ok := strings.CutPrefix(uri, "https://")
	if !ok || host == "" || tag == "" {
		return ""
	}

	return "https://" + tag + "---" + host
}

// nextURL is the next revision's public URL: the environment's hostname with -next on its
// first label, through the load balancer, whose next backend serves the revision tagged
// next. Empty for a pull-request build, which has an environment of its own and no next
// backend, and without a hostname.
func nextURL(hostname, pullRequest string) string {
	if hostname == "" || pullRequest != "" {
		return ""
	}
	label, rest, found := strings.Cut(hostname, ".")
	if !found {
		return ""
	}

	return "https://" + label + "-next." + rest + "/"
}

// repair reads the service and, when its traffic points at a revision that is not ready,
// moves the traffic back to the last ready revision and checks again. It answers the
// service as it stands.
func repair(ctx context.Context, run Run, name, service string, out io.Writer) (map[string]any, error) {
	doc, err := run.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	lastReady := inconsistentLastReady(doc)
	if lastReady == "" {
		fmt.Fprintf(out, "HEALTH CHECK PASSED: service [%s] is consistent.\n", service)

		return doc, nil
	}
	fmt.Fprintf(out, "PROBLEM: service [%s] is in an inconsistent state; moving traffic back to [%s]...\n", service, lastReady)
	traffic := []any{map[string]any{keyType: targetRevision, keyRevision: lastReady, keyPercent: fullTraffic}}
	if _, err := run.Patch(ctx, name, map[string]any{keyName: name, keyTraffic: traffic}, "traffic"); err != nil {
		// The move has been known to report an error while the traffic moved, so the
		// state is checked again instead.
		fmt.Fprintf(out, "The move reported an error (%v); checking whether the traffic moved anyway...\n", errors.Cause(err))
	}
	if doc, err = run.Get(ctx, name); err != nil {
		return nil, err
	}
	if still := inconsistentLastReady(doc); still != "" {
		return nil, errors.Newf("FIX FAILED: service [%s] is still inconsistent [%s].", service, still)
	}
	fmt.Fprintf(out, "FIX VERIFIED: traffic for [%s] is consistent again.\n", service)

	return doc, nil
}

// inconsistentLastReady is the last ready revision of a service whose traffic points at a
// revision that is not ready: the service is not at rest and its first traffic target
// names a revision other than the one serving. Empty when the service is consistent, or
// when there is no ready revision to move to.
func inconsistentLastReady(doc map[string]any) string {
	if text(doc, "terminalCondition.state") == conditionSucceeded {
		return ""
	}
	wanted, _ := doc[keyTraffic].([]any)
	serving, _ := doc["trafficStatuses"].([]any)
	if len(wanted) == 0 || len(serving) == 0 {
		return ""
	}
	first, _ := wanted[0].(map[string]any)
	status, _ := serving[0].(map[string]any)
	if text(first, keyRevision) == text(status, keyRevision) {
		return ""
	}

	return shortName(text(doc, "latestReadyRevision"))
}

// clearMaintenance empties the maintenance variable in the service's template when a
// maintenance revision left it set, so that the revision deployed from the template
// serves the application: the template is the live service's, maintenance revision
// included. True when it was set.
func clearMaintenance(doc map[string]any) bool {
	template, _ := doc[keyTemplate].(map[string]any)
	container, err := firstContainer(template)
	if err != nil {
		return false
	}
	vars, _ := container["env"].([]any)
	cleared := false
	for _, entry := range vars {
		v, _ := entry.(map[string]any)
		if text(v, keyName) == derive.MaintenanceVariable && text(v, keyValue) != "" {
			v[keyValue] = ""
			cleared = true
		}
	}

	return cleared
}

// deployRevision sends the service back with the image, the labels and its traffic
// pinned to what serves now, and answers the revision the change created and the
// service's URL.
func deployRevision(ctx context.Context, run Run, name string, doc map[string]any, image string, labels map[string]string, tag string) (revision, uri string, err error) {
	template, _ := doc[keyTemplate].(map[string]any)
	container, err := firstContainer(template)
	if err != nil {
		return "", "", err
	}
	container["image"] = image
	setLabels(doc, labels)
	setLabels(template, labels)
	if pinned := pinnedTraffic(doc, tag); pinned != nil {
		doc[keyTraffic] = pinned
	}
	settled, err := run.Patch(ctx, name, doc)
	if err != nil {
		return "", "", err
	}
	revision = shortName(text(settled, "latestCreatedRevision"))
	if revision == "" {
		return "", "", errors.New("the revision name could not be read from the service")
	}

	return revision, text(settled, "uri"), nil
}

// pinnedTraffic is the service's traffic as it serves now, every target named by its
// revision (a target on the latest revision becomes that revision), so a new revision
// takes no traffic; a tag names the new revision, taken off the target that carried it
// while that target keeps the traffic it serves. Nil when the service serves nothing
// yet, which leaves its traffic alone.
func pinnedTraffic(doc map[string]any, tag string) []any {
	serving, _ := doc["trafficStatuses"].([]any)
	if len(serving) == 0 {
		return nil
	}
	ready := shortName(text(doc, "latestReadyRevision"))
	pinned := make([]any, 0, len(serving)+1)
	for _, entry := range serving {
		status, _ := entry.(map[string]any)
		// After a shift the serving revision is also the latest, so Cloud Run reports its
		// traffic and the tag on the latest revision as one status: the tag moves to the
		// new revision, and the traffic stays where it is.
		moving := tag != "" && text(status, keyTag) == tag
		if percent, _ := status[keyPercent].(float64); moving && percent == 0 {
			continue
		}
		// A status names the revision it serves; a service whose latest allocation has
		// never moved (a fresh one on its placeholder image) reports no name, and the
		// pin then takes the latest ready revision. With none ready, nothing can be
		// pinned, and the allocation stays as it was.
		revision := text(status, keyRevision)
		if revision == "" {
			revision = ready
		}
		target := map[string]any{keyType: targetRevision, keyRevision: revision, keyPercent: status[keyPercent]}
		if revision == "" {
			target = map[string]any{keyType: targetLatest, keyPercent: status[keyPercent]}
		}
		if t := text(status, keyTag); t != "" && !moving {
			target[keyTag] = t
		}
		pinned = append(pinned, target)
	}
	if tag != "" {
		pinned = append(pinned, map[string]any{keyType: targetLatest, keyPercent: float64(0), keyTag: tag})
	}

	return pinned
}

// ShiftTraffic moves every region to 100 percent on its new revision, keeping the tags
// other revisions carry. A pull-request revision served under a tag alone leaves the
// traffic where it is.
func ShiftTraffic(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if env[skipDeploy] == trueValue {
		fmt.Fprintln(out, tornDown)

		return nil
	}
	revisions, err := w.Revisions()
	if err != nil || len(revisions) == 0 {
		return errors.Newf("%s is missing or empty; no revision was deployed", RevisionsFile)
	}
	if env[shiftTraffic] != trueValue {
		fmt.Fprintln(out, "Leaving traffic unchanged: a pull-request revision is served under its tag only.")

		return nil
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	run, err := clients.Run(ctx)
	if err != nil {
		return err
	}
	for _, r := range revisions {
		fmt.Fprintf(out, "--- Shifting traffic in [%s] to [%s] ---\n", r.Region, r.Revision)
		name := serviceName(build.Substitutions[projectSub], r.Region, r.Service)
		doc, err := run.Get(ctx, name)
		if err != nil {
			return err
		}
		traffic := []any{map[string]any{keyType: targetRevision, keyRevision: r.Revision, keyPercent: fullTraffic}}
		current, _ := doc[keyTraffic].([]any)
		for _, entry := range current {
			t, _ := entry.(map[string]any)
			if text(t, keyTag) != "" && text(t, keyRevision) != r.Revision {
				traffic = append(traffic, map[string]any{keyType: t[keyType], keyRevision: t[keyRevision], keyPercent: float64(0), keyTag: t[keyTag]})
			}
		}
		if _, err := run.Patch(ctx, name, map[string]any{keyName: name, keyTraffic: traffic}, "traffic"); err != nil {
			return err
		}
	}

	return nil
}
