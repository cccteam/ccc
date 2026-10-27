package deploy

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

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
		fmt.Fprintln(out, skipped(env))

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
	templates, err := readRevisionTemplates(w)
	if err != nil {
		return err
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
		revision, uri, err := deployService(ctx, run, build, templates, region, service, image, labels, tag, out)
		if err != nil {
			return err
		}
		revisions = append(revisions, revision)
		if url := taggedURL(uri, tag); url != "" {
			urls = append(urls, region+"="+url)
		}
	}
	if err := w.WriteRevisions(revisions); err != nil {
		return err
	}

	return nextFacts(w, env, build, urls, out)
}

// deployService puts the release's revision on one service: the service repaired when a
// failed earlier deploy left it inconsistent, its template replaced by the release's own
// (the stack's revision_templates output, built on the image), the revision created
// without traffic under the tag. It answers the revision and the service's URL.
func deployService(ctx context.Context, run Run, build *Build, templates *RevisionTemplates, region, service, image string, labels map[string]string, tag string, out io.Writer) (Revision, string, error) {
	fmt.Fprintf(out, "--- Service [%s] in region [%s] ---\n", service, region)
	template, ok := templates.Services[service]
	if !ok {
		return Revision{}, "", errors.Newf("the release's revision templates name no service %s (they name %s): the stack applied names the services the build deploys to", service, strings.Join(slices.Sorted(maps.Keys(templates.Services)), ", "))
	}
	name := serviceName(build.Substitutions[projectSub], region, service)
	doc, err := repair(ctx, run, name, service, out)
	if err != nil {
		return Revision{}, "", err
	}
	doc[keyTemplate] = template.document(image)
	fmt.Fprintf(out, "Deploying revision to [%s] without traffic, under the release's own template (%d variable(s), %d secret mount(s))...\n", service, len(template.Env), len(template.Secrets))
	revision, uri, err := deployRevision(ctx, run, name, doc, image, labels, tag)
	if err != nil {
		return Revision{}, "", errors.Wrapf(err, "the deployment to region %s", region)
	}
	fmt.Fprintf(out, "Revision [%s] deployed to [%s] in [%s] under the tag [%s]\n", revision, service, region, tag)

	return Revision{Region: region, Service: service, Revision: revision}, uri, nil
}

// nextFacts leaves what the hook before traffic calls: the next revision's public URL
// through the load balancer (none for a pull request, whose environment is its own),
// the tagged run.app URL per region, which the services' ingress answers from inside
// alone, and each site's next URL when the stack names the sites' hostnames.
func nextFacts(w Workspace, env map[string]string, build *Build, urls []string, out io.Writer) error {
	facts := map[string]string{
		nextURLFact:      nextRevisionURL(canonicalHostname(env, build), nextHostname(env, build), build.Substitutions[prNumberSub]),
		revisionURLsFact: strings.Join(urls, ","),
	}
	if sites := canonicalHostnames(env, build); sites != "" {
		facts[nextURLsFact] = nextURLs(sites, nextHostnames(env, build), build.Substitutions[prNumberSub])
	}
	if err := w.Append(facts); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s=%s %s=%s", nextURLFact, facts[nextURLFact], revisionURLsFact, facts[revisionURLsFact])
	if facts[nextURLsFact] != "" {
		fmt.Fprintf(out, " %s=%s", nextURLsFact, facts[nextURLsFact])
	}
	fmt.Fprintln(out)

	return nil
}

// The tag every deploy puts on its new revision when the build serves under no other,
// and the facts the deploy leaves for the hook before traffic.
const (
	nextTag          = "next"
	nextURLFact      = "NEXT_URL"
	revisionURLsFact = "REVISION_URLS"
	hostnameSub      = "_HOSTNAME"
	// canonicalHostnameFact is the environment's canonical hostname as the stack this
	// build applied names it (deploy stack apply reads it back), where the trigger's
	// _HOSTNAME is its stack's last apply.
	canonicalHostnameFact = "CANONICAL_HOSTNAME"
	// A sites-layout application's stack names each site's canonical hostname too
	// (_HOSTNAMES, site=hostname per site, read back as CANONICAL_HOSTNAMES), and the
	// deploy leaves each site's next URL for the hook (NEXT_URLS, site=url per site);
	// NEXT_URL stays the first site's. A flat-layout stack names neither.
	nextURLsFact           = "NEXT_URLS"
	hostnamesSub           = "_HOSTNAMES"
	canonicalHostnamesFact = "CANONICAL_HOSTNAMES"
	// A stack whose canonical hostname is on a vanity domain names the next revision's
	// hostname itself (_NEXT_HOSTNAME, on the apps domain, where the next backend's
	// twin and the wildcard certificate are; read back as NEXT_HOSTNAME), and each
	// site's in the sites layout (_NEXT_HOSTNAMES, site=hostname, read back as
	// NEXT_HOSTNAMES). A stack naming none has the next hostname derived from the
	// canonical one.
	nextHostnameSub   = "_NEXT_HOSTNAME"
	nextHostnameFact  = "NEXT_HOSTNAME"
	nextHostnamesSub  = "_NEXT_HOSTNAMES"
	nextHostnamesFact = "NEXT_HOSTNAMES"
)

// canonicalHostname is the environment's hostname the next revision's URL is named by: the
// stack's as this build applied it, else the trigger's (a build that applied no stack: a
// pull request's, whose environment is its own and has no next URL).
func canonicalHostname(env map[string]string, build *Build) string {
	if hostname := env[canonicalHostnameFact]; hostname != "" {
		return hostname
	}

	return build.Substitutions[hostnameSub]
}

// canonicalHostnames is each site's canonical hostname (site=hostname, comma-separated)
// the next URLs are named by, the way canonicalHostname is the first site's: the stack's
// as this build applied it, else the trigger's; empty for a flat-layout stack.
func canonicalHostnames(env map[string]string, build *Build) string {
	if sites := env[canonicalHostnamesFact]; sites != "" {
		return sites
	}

	return build.Substitutions[hostnamesSub]
}

// nextHostname is the next revision's hostname the stack names, when it names one: the
// stack's as this build applied it, else the trigger's; empty for a stack whose next
// hostname is derived from the canonical one.
func nextHostname(env map[string]string, build *Build) string {
	if hostname := env[nextHostnameFact]; hostname != "" {
		return hostname
	}

	return build.Substitutions[nextHostnameSub]
}

// nextHostnames is each site's next revision's hostname the stack names (site=hostname,
// comma-separated), the way nextHostname is the first site's; empty when it names none.
func nextHostnames(env map[string]string, build *Build) string {
	if sites := env[nextHostnamesFact]; sites != "" {
		return sites
	}

	return build.Substitutions[nextHostnamesSub]
}

// nextURLs is each site's next revision's public URL (site=url, comma-separated), from
// the next hostname the stack names for the site, else from the site's canonical
// hostname the way nextURL names one; a site without one is left out, and a
// pull-request build has none.
func nextURLs(sites, nextSites, pullRequest string) string {
	next := map[string]string{}
	for _, pair := range strings.Split(nextSites, ",") {
		if site, hostname, ok := strings.Cut(pair, "="); ok {
			next[site] = hostname
		}
	}
	var urls []string
	for _, pair := range strings.Split(sites, ",") {
		site, hostname, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		if url := nextRevisionURL(hostname, next[site], pullRequest); url != "" {
			urls = append(urls, site+"="+url)
		}
	}

	return strings.Join(urls, ",")
}

// nextRevisionURL is the next revision's public URL: the next hostname the stack names,
// when it names one (its canonical hostname is on a vanity domain, and the next revision
// stays on the apps domain), else the canonical hostname with -next on its first label
// (nextURL). Empty for a pull-request build.
func nextRevisionURL(canonical, next, pullRequest string) string {
	if next == "" || pullRequest != "" {
		return nextURL(canonical, pullRequest)
	}

	return "https://" + next + "/"
}

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

// deployRevision sends the service back with the image, the labels and its traffic
// pinned to what serves now, and answers the revision the change created and the
// service's URL. The template is the document's: the release's own (deploy service,
// built from the stack's revision_templates output) or, for a maintenance revision, the
// live service's with the switch set.
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
		fmt.Fprintln(out, skipped(env))

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
