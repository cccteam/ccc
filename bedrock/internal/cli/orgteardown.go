// orgteardown.go is org teardown: the organization's applications out of service, in
// two runs. The first takes the hosts and the load balancer's listeners out of 2-net
// through the placement and the pull request it prepares; the second, once 2-net
// routes nothing, tears every registered application down to what it keeps.

package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/where"
)

const (
	orgTeardownUse = "teardown"
	orgTeardownCmd = "org teardown"
	// orgJournal is the organization repository's journal, which each run writes a
	// line to.
	orgJournal = "JOURNAL.md"
)

// newOrgTeardown is org teardown.
func newOrgTeardown(d deps) *cobra.Command {
	var dir string
	var yes bool
	cmd := &cobra.Command{
		Use:   orgTeardownUse,
		Short: "Take the organization's applications out of service: the hosts and the load balancer's listeners out of 2-net, then every application torn down to what it keeps",
		Long: `teardown takes the organization's applications out of service and keeps the foundation: the
projects, the folders, the identities, the roles and policies, the state, the triggers, the Spanner
instances, the registry, the connection and the keys stay, and so does every application's data
(its database and backups, its file stores, its secret containers, its records). It runs twice, by
a person at their own computer with their own sign-in (gcloud auth application-default login).

The first run sets teardown in placement.json and renders 2-net with no host and serving = false
(2-net/applications.auto.tfvars), writes the journal line, and leaves the working tree for a pull
request: its merge has the apply trigger take the hosts and the load balancer's listeners (the
forwarding rules, the proxies, the URL maps and the sink) out of 2-net, while the address, the
certificate and its DNS authorization stay, so the IP never changes and no certificate is issued
again. The hosts go first because a backend service cannot be deleted while the URL map names it.

The second run, once 2-net's state routes nothing, tears every registered application down to
what it keeps, the way bedrock teardown --all does: each environment in promotion order, then the
pull-request stacks, with the environments' summaries shown before the prefix is typed back. When
the applications' stacks live under 3-app/ the run does it; when each application carries its own
stack (infrastructure/ in its repository) the run reads the state bucket and the records, says
which environments still stand and the command to run from each checkout (bedrock teardown
--all), and writes the journal line once none does. The prefix typed back gates each run (--yes
stands in for it where there is no terminal).

The inverse is the same loop the other way: each stack applied again with the operations
workflow's apply action per environment, teardown removed from placement.json on a pull request
(a host names a backend service that must exist), then bedrock rerun <env> <release> per
environment.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return d.teardownOrg(cmd, dir, yes)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")
	cmd.Flags().BoolVar(&yes, confirmFlag, false, "go on without the prefix typed back, where there is no terminal to type it at")

	return cmd
}

// teardownOrg is the run: the network's part while the placement does not say torn
// down, the applications' part once it does.
func (d deps) teardownOrg(cmd *cobra.Command, dir string, yes bool) error {
	p, err := orgPlacement(dir, "")
	if err != nil {
		return err
	}
	if !p.Teardown {
		return d.teardownOrgNet(cmd, dir, p, yes)
	}

	return d.teardownOrgApplications(cmd, dir, p, yes)
}

// teardownOrgNet is the first run: the placement flipped, 2-net rendered without its
// hosts and listeners, the journal line, and the pull request left to the person.
func (d deps) teardownOrgNet(cmd *cobra.Command, dir string, p *org.Placement, yes bool) error {
	out := cmd.OutOrStdout()
	path := orgPlacementPath(dir, "")
	if err := d.orgPinned(p, path); err != nil {
		return err
	}
	fmt.Fprintf(out, "bedrock org teardown takes the %s organization's applications out of service. This run takes the hosts and the load balancer's listeners (the forwarding rules, the proxies, the URL maps and the sink) out of 2-net, on the pull request it prepares; the address, the certificate and its DNS authorization stay. The next run, once the merge's apply has 2-net routing nothing, tears every registered application (%s) down to what it keeps. Nothing here is undone by bedrock: the inverse is each stack applied again with the operations workflow's apply action, teardown removed from placement.json on a pull request, and a rerun per environment.\n\n", p.Prefix, applicationsList(p))
	ok, err := d.confirmTeardown(cmd, orgTeardownCmd, p.Prefix, yes)
	if err != nil || !ok {
		return err
	}
	p.Teardown = true
	if err := p.Write(path); err != nil {
		return err
	}
	files, err := org.Render(p)
	if err != nil {
		return err
	}
	written, err := org.Write(files, dir)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Recorded teardown in %s and rendered the %s foundation into %s: %d owned file(s) written; 2-net's applications.auto.tfvars now routes no host and sets serving = false.\n", path, p.Prefix, dir, written.Owned)
	line := fmt.Sprintf("- %s: bedrock org teardown: the hosts and the load balancer's listeners leave 2-net on this pull request (teardown in placement.json); the address, the certificate and its DNS authorization stay. The applications follow (bedrock org teardown again, after the apply).\n", time.Now().UTC().Format("2006-01-02 15:04Z"))
	if err := appendJournalLine(dir, line); err != nil {
		return err
	}
	fmt.Fprintf(out, "A line went into %s.\n\nNext: commit placement.json, %s and the rendered files, open the pull request and merge it; the plan run shows 2-net's listeners destroyed and the apply trigger applies it. Then run bedrock org teardown again for the applications.\n", orgJournal, orgJournal)

	return nil
}

// teardownOrgApplications is the second run: 2-net must route nothing, then every
// registered application is torn down where the run reaches its stack, or accounted for
// where it cannot.
func (d deps) teardownOrgApplications(cmd *cobra.Command, dir string, p *org.Placement, yes bool) error {
	out, ctx := cmd.OutOrStdout(), cmd.Context()
	hosts, err := deploy.NetHosts(ctx, d.deploy, p.StateBucket)
	if err != nil {
		return err
	}
	if len(hosts) > 0 {
		return errors.Newf("2-net still routes %d host(s) (%s): the pull request that takes them out is not applied yet; merge it, let the apply trigger apply 2-net, then run bedrock org teardown again", len(hosts), strings.Join(sortedKeys(hosts), ", "))
	}
	if len(p.Applications) == 0 {
		fmt.Fprintf(out, "The %s organization registers no application: nothing to tear down beyond 2-net, which routes nothing.\n", p.Prefix)

		return nil
	}
	if where.HasApplicationLayers(dir) {
		return d.teardownOrgLayers(cmd, dir, p, yes)
	}

	return d.teardownOrgAccount(ctx, dir, p, out)
}

// teardownOrgLayers tears every application under 3-app down, each environment in
// promotion order then its pull-request stacks, after one look at the whole and the
// prefix typed back.
func (d deps) teardownOrgLayers(cmd *cobra.Command, dir string, p *org.Placement, yes bool) error {
	out, ctx := cmd.OutOrStdout(), cmd.Context()
	dataDir, err := os.MkdirTemp("", "bedrock-org-teardown-")
	if err != nil {
		return errors.Wrap(err, "os.MkdirTemp()")
	}
	defer os.RemoveAll(dataDir)
	var prepared []*deploy.ApplicationTeardown
	var summary strings.Builder
	for _, app := range p.Applications {
		layerDir := where.LayerDir(dir, app)
		ap, err := where.Placement(layerDir)
		if err != nil {
			return err
		}
		if ap == nil {
			return errors.Newf("%s has no %s: the application's stack under %s was not rendered by bedrock", app, placementFile, layerDir)
		}
		req := &deploy.TeardownRequest{Dir: layerDir, DataDir: dataDir, Placement: ap, App: app, By: takenDownBy(), Now: time.Now}
		a, err := deploy.PrepareApplication(ctx, d.deploy, req, out)
		if err != nil {
			return err
		}
		if a.Empty() {
			fmt.Fprintf(out, "%s: nothing stands.\n", app)

			continue
		}
		prepared = append(prepared, a)
		fmt.Fprintf(&summary, "%s:\n%s", app, a.Summary())
	}
	if len(prepared) == 0 {
		return d.teardownOrgDone(dir, p, out)
	}
	fmt.Fprintf(out, "bedrock org teardown tears the %s organization's applications down to what they keep (the database and every generation of it, the file stores, the secret containers, the identities, the Firestore database, the task queue, the migrate logs and the stack's id stay; the services, the jobs, the triggers, the schedules, the backend services and the Cloud Armor policy go), and destroys the pull-request stacks whole:\n\n%s\nNothing here is undone by bedrock.\n\n", p.Prefix, summary.String())
	ok, err := d.confirmTeardown(cmd, orgTeardownCmd, p.Prefix, yes)
	if err != nil || !ok {
		return err
	}
	for _, a := range prepared {
		if err := a.Run(ctx, out); err != nil {
			return err
		}
	}

	return d.teardownOrgDone(dir, p, out)
}

// teardownOrgAccount says, for applications whose stacks live in their own
// repositories, which environments still stand and what to run from each checkout, and
// finishes the teardown once none does.
func (d deps) teardownOrgAccount(ctx context.Context, dir string, p *org.Placement, out io.Writer) error {
	standing := 0
	for _, app := range p.Applications {
		standings, err := deploy.ApplicationStanding(ctx, d.deploy, p.StateBucket, app, org.Environments)
		if err != nil {
			return err
		}
		for _, s := range standings {
			switch {
			case !s.Stack:
				fmt.Fprintf(out, "%s %s: no stack.\n", app, s.Env)
			case s.TornDown:
				fmt.Fprintf(out, "%s %s: torn down.\n", app, s.Env)
			case s.Release != "":
				fmt.Fprintf(out, "%s %s: stands (%s live).\n", app, s.Env, s.Release)
				standing++
			default:
				fmt.Fprintf(out, "%s %s: stands.\n", app, s.Env)
				standing++
			}
		}
		if slices.ContainsFunc(standings, func(s deploy.Standing) bool { return s.Stack && !s.TornDown }) {
			fmt.Fprintf(out, "From %s's checkout: bedrock teardown --all\n", app)
		}
	}
	if standing > 0 {
		fmt.Fprintf(out, "\n%d environment(s) stand: run the command above from each application's checkout, then bedrock org teardown again.\n", standing)

		return nil
	}

	return d.teardownOrgDone(dir, p, out)
}

// teardownOrgDone writes the journal line that ends the teardown and says what the
// organization is left with.
func (deps) teardownOrgDone(dir string, p *org.Placement, out io.Writer) error {
	line := fmt.Sprintf("- %s: bedrock org teardown: every application torn down to what it keeps (%s); 2-net routes nothing. The inverse: each stack applied again with the operations workflow's apply action, teardown removed from placement.json on a pull request, a rerun per environment.\n", time.Now().UTC().Format("2006-01-02 15:04Z"), applicationsList(p))
	if err := appendJournalLine(dir, line); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nThe %s organization is torn down: its applications (%s) keep their data, identities and records, and nothing serves; a line went into %s. The foundation stands (bedrock org destroy deletes it).\n", p.Prefix, applicationsList(p), orgJournal)

	return nil
}

// applicationsList names the registered applications, or none.
func applicationsList(p *org.Placement) string {
	if len(p.Applications) == 0 {
		return "none registered"
	}

	return strings.Join(p.Applications, ", ")
}

// sortedKeys are the map's keys, sorted.
func sortedKeys(m map[string]string) []string {
	return slices.Sorted(maps.Keys(m))
}

// appendJournalLine adds the line to the repository's journal, creating it when absent.
func appendJournalLine(dir, line string) error {
	f, err := os.OpenFile(filepath.Join(dir, orgJournal), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.Wrap(err, "os.OpenFile(): the journal")
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return errors.Wrap(err, "os.File.WriteString(): the journal")
	}

	return nil
}
