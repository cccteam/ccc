// orgretire.go is org retire <app>: a torn-down, destroyed application leaves the
// organization. Its GitHub repository is archived and, with the settings 1-org declared
// on it, taken out of 1-org's state, so that the layers rendered without the application
// delete nothing on GitHub (1-org never deletes a repository: prevent_destroy); then the
// entry leaves placement.json, the foundation renders without it, and the pull request
// the run leaves has the apply delete the application's cloud side.

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/layers"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/where"
)

const (
	orgRetireUse = "retire"
	orgRetireCmd = "org retire"
)

// newOrgRetire is org retire <app>.
func newOrgRetire(d deps) *cobra.Command {
	var dir string
	var yes bool
	cmd := &cobra.Command{
		Use:   orgRetireUse + " <app>",
		Short: "Retire a torn-down, destroyed application: its repository archived and out of 1-org's state, its entry out of the placement",
		Long: `retire takes an application that is out of service everywhere and whose stacks are destroyed out of
the organization, by a person at their own computer with their own sign-in (gcloud auth
application-default login; GITHUB_TOKEN, or gh's token, for the archive and for 1-org's GitHub
provider: an owner of the GitHub organization, or an admin of the repository).

The repository outlives the application. retire archives it on GitHub (read-only, with its code,
pull requests and releases; a person unarchives it in the repository's settings) and takes it,
with the settings 1-org declared on it (its rulesets, its operations environments and their
deployment policies), out of 1-org's state: an archived repository refuses every change, so
the provider could not delete them, and the layer never deletes a repository (prevent_destroy).
They stay on GitHub as they were, frozen with the archive. Then the entry leaves placement.json
(applications, registration, outOfService, hostnames), the foundation renders without the
application, the journal gets its line, and the working tree is left for a pull request: the
plan run shows the application's cloud side going (its identities and their grants, its
registry entries, its secret containers' grants), and the apply trigger applies it. Nothing
here is undone by bedrock: a retired application is registered again as a new one (bedrock
org register), with a repository of its own.

It refuses an application still in service somewhere (bedrock org teardown <app> first, and
the pull request that takes its hostnames out of 2-net applied), and one whose stack still
stands in an environment (bedrock destroy --all from its checkout, which takes the final
backups). The prefix typed back gates the run (--yes stands in for it where there is no
terminal).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.retireOrgApplication(cmd, dir, args[0], yes)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")
	cmd.Flags().BoolVar(&yes, confirmFlag, false, "go on without the application's code typed back, where there is no terminal to type it at")

	return cmd
}

// retireOrgApplication is the run: the checks that the application is out of service
// and destroyed, the warning and the code typed back, then the repository and the
// placement.
func (d deps) retireOrgApplication(cmd *cobra.Command, dir, app string, yes bool) error {
	out, ctx := cmd.OutOrStdout(), cmd.Context()
	p, err := orgPlacement(dir, "")
	if err != nil {
		return err
	}
	path := orgPlacementPath(dir, "")
	if err := d.orgPinned(p, path); err != nil {
		return err
	}
	if !p.Registered(app) {
		return errors.Newf("%s is not registered (applications in placement.json): nothing to retire", app)
	}
	if err := retireOutOfService(p, app); err != nil {
		return err
	}
	if err := d.retireUnrouted(ctx, p, app); err != nil {
		return err
	}
	if err := d.retireDestroyed(ctx, dir, p, app, out); err != nil {
		return err
	}
	fmt.Fprintf(out, "bedrock org retire %s takes %s out of the %s organization. Its repository %s/%s is archived on GitHub (read-only, with its code, pull requests and releases) and taken out of 1-org's state with its rulesets and operations environments, which stay on the archived repository as they are; then %s leaves placement.json and the foundation renders without it, on the pull request this run prepares: its apply deletes %s's identities and their grants, its registry entries and its secret containers' grants. Nothing here is undone by bedrock: a person unarchives the repository in its settings, and a retired application is registered again as a new one.\n\n", app, app, p.Prefix, p.GithubOrganization, p.Repository(app), app, app)
	ok, err := d.confirmTeardown(cmd, orgRetireCmd+" "+app, app, yes)
	if err != nil || !ok {
		return err
	}
	if err := d.retireRepository(ctx, dir, p, app, out); err != nil {
		return err
	}

	return retirePlacement(dir, path, p, app, out)
}

// retireRepository archives the application's repository on GitHub and takes it, with
// the settings 1-org declared on it, out of 1-org's state.
func (d deps) retireRepository(ctx context.Context, dir string, p *org.Placement, app string, out io.Writer) error {
	repository := p.Repository(app)
	token, err := d.githubToken(ctx)
	if err != nil {
		return errors.Wrap(err, "the archive and 1-org's GitHub provider need a token: an owner of the GitHub organization's, or an admin of the repository's")
	}
	gh, err := d.github(ctx)
	if err != nil {
		return err
	}
	if err := gh.Archive(ctx, p.GithubOrganization, repository); err != nil {
		return errors.Wrapf(err, "archiving %s/%s", p.GithubOrganization, repository)
	}
	fmt.Fprintf(out, "Archived %s/%s: read-only on GitHub, with its code, pull requests and releases.\n", p.GithubOrganization, repository)
	var exec deploy.Runner
	if d.deploy != nil {
		exec = d.deploy.Exec
	}
	taken, err := layers.RetireRepository(ctx, exec, dir, app, token, out)
	if err != nil {
		return err
	}
	if len(taken) == 0 {
		fmt.Fprintf(out, "1-org's state held nothing of %s's repository: nothing to take out.\n", app)
	} else {
		fmt.Fprintf(out, "Took %s out of 1-org's state: the repository and its settings stay on GitHub as they are.\n", strings.Join(taken, ", "))
	}

	return nil
}

// retirePlacement takes the application's stack directory out where the organization
// holds the stacks, and its entry out of the placement; renders the foundation, writes
// the journal line and says what is next.
func retirePlacement(dir, path string, p *org.Placement, app string, out io.Writer) error {
	repository := p.Repository(app)
	if where.HasApplicationLayers(dir) {
		layer := where.LayerDir(dir, app)
		if err := os.RemoveAll(layer); err != nil {
			return errors.Wrapf(err, "os.RemoveAll(): %s's stack under %s", app, layer)
		}
		fmt.Fprintf(out, "Removed %s's stack from the repository (%s): its state in the bucket is empty since the destroy.\n", app, layer)
	}
	if err := p.Retire(app); err != nil {
		return err
	}
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
	fmt.Fprintf(out, "Removed %s from %s and rendered the %s foundation into %s: %d owned file(s) written; the layers' applications.auto.tfvars name %s no more.\n", app, path, p.Prefix, dir, written.Owned, app)
	line := fmt.Sprintf("- %s: bedrock org retire %s: %s/%s archived and out of 1-org's state with its settings; %s leaves placement.json on this pull request, whose apply deletes its identities, grants and registry entries. Its data went with bedrock destroy; the final backups a destroy takes stay on the instance until their retention ends.\n", time.Now().UTC().Format("2006-01-02 15:04Z"), app, p.GithubOrganization, repository, app)
	if err := appendJournalLine(dir, line); err != nil {
		return err
	}
	fmt.Fprintf(out, "A line went into %s.\n\nNext: commit placement.json, %s and the rendered files, open the pull request and merge it; the plan run shows %s's identities, grants and registry entries destroyed and nothing on GitHub, and the apply trigger applies it.\n", orgJournal, orgJournal, app)

	return nil
}

// retireOutOfService refuses an application the placement still serves somewhere: a
// retirement starts with bedrock org teardown <app> for every environment, or the
// organization's teardown.
func retireOutOfService(p *org.Placement, app string) error {
	if p.Teardown {
		return nil
	}
	var served []string
	for _, env := range org.Environments {
		if !slices.Contains(p.OutOfService[app], env) {
			served = append(served, env)
		}
	}
	if len(served) > 0 {
		return errors.Newf("%s is in service in %s (outOfService in placement.json): run bedrock org teardown %s first, merge the pull request it leaves and let the apply trigger take its hostnames out of 2-net, then bedrock destroy --all from its checkout, then retire", app, strings.Join(served, ", "), app)
	}

	return nil
}

// retireUnrouted refuses while 2-net's state still routes one of the application's
// hostnames: the teardown's pull request is not applied yet.
func (d deps) retireUnrouted(ctx context.Context, p *org.Placement, app string) error {
	hosts, err := deploy.NetHosts(ctx, d.deploy, p.StateBucket)
	if err != nil {
		return err
	}
	var routed []string
	for _, env := range org.Environments {
		for _, host := range p.ApplicationHostnames(app, env) {
			if _, ok := hosts[host]; ok {
				routed = append(routed, host)
			}
		}
	}
	if len(routed) > 0 {
		return errors.Newf("2-net still routes %s's %s: the pull request bedrock org teardown %s left is not applied yet; merge it, let the apply trigger apply 2-net, then retire", app, strings.Join(routed, ", "), app)
	}

	return nil
}

// retireDestroyed refuses while the application's stack stands in an environment: the
// destroy from its checkout takes the final backups and the data with it, and retire
// touches no data.
func (d deps) retireDestroyed(ctx context.Context, dir string, p *org.Placement, app string, out io.Writer) error {
	standings, err := deploy.ApplicationStanding(ctx, d.deploy, p.StateBucket, app, org.Environments)
	if err != nil {
		return err
	}
	var standing []string
	for _, s := range standings {
		if s.Stack {
			standing = append(standing, s.Env)
		}
	}
	if len(standing) > 0 {
		from := "its checkout"
		if where.HasApplicationLayers(dir) {
			from = "the organization repository (bedrock destroy --all --layer " + where.LayerDir(dir, app) + ")"
		}

		return errors.Newf("%s's stack stands in %s: run bedrock destroy --all from %s first, which takes the final backups and the data down, then retire", app, strings.Join(standing, ", "), from)
	}
	fmt.Fprintf(out, "%s: out of service everywhere, 2-net routes nothing to it, and no stack stands in %s.\n", app, strings.Join(org.Environments, ", "))

	return nil
}

// retireNotice says, for each application out of service in every environment, that
// bedrock org retire is the step that lets its entry go once its stacks are destroyed:
// the entry stays until then, since 1-org never deletes a repository (prevent_destroy),
// and org check gates every plan and apply.
func retireNotice(p *org.Placement) string {
	var b strings.Builder
	for _, app := range p.Codes() {
		if len(p.OutOfService[app]) == len(org.Environments) {
			fmt.Fprintf(&b, "%s is out of service in every environment (outOfService in placement.json): once bedrock destroy --all has taken its stacks down from its checkout, bedrock org retire %s archives its repository, takes it out of 1-org's state and removes the entry; until then the entry stays, since 1-org never deletes a repository (prevent_destroy).\n", app, app)
		}
	}

	return b.String()
}
