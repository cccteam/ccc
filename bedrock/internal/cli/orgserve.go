// orgserve.go is org serve: the way back into service, for one application in the
// environments named or for the organization. It refuses while a stack is still torn
// down, removes the placement's entry, renders 2-net with the hostnames back, writes the
// journal line and leaves the working tree for a pull request.

package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

const (
	orgServeUse = "serve"
	orgServeCmd = "org serve"
)

// newOrgServe is org serve.
func newOrgServe(d deps) *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   orgServeUse + " [<app> [<env>...]]",
		Short: "Put an application, or the organization, back in service: its hostnames return to 2-net on the pull request the run prepares",
		Long: `serve is the way back after a teardown, by a person at their own computer with their own sign-in
(gcloud auth application-default login). With an application named it puts that application back in
service in the environments named, or in every environment it is out of service in: each must have
its stack applied again (the operations workflow's apply action) and a release run since (bedrock
rerun <env> <release>), since a host names a backend service that must exist; the run reads the
state bucket and the records and refuses an environment still torn down. Then it removes the
environments from outOfService in placement.json, renders 2-net with the hostnames back, writes the
journal line and leaves the working tree for a pull request: its merge has the apply trigger put
the host rules back.

With no application it is the organization's way back: every registered application's stacks
applied and run again the same way, then teardown removed from placement.json, 2-net rendered with
its listeners and every host back (serving = true), the journal line, and the pull request.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return d.serveApplication(cmd, dir, args[0], args[1:])
			}

			return d.serveOrg(cmd, dir)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")

	return cmd
}

// serveApplication puts the application back in service in the environments named, or
// every one it is out of service in.
func (d deps) serveApplication(cmd *cobra.Command, dir, app string, envs []string) error {
	out, ctx := cmd.OutOrStdout(), cmd.Context()
	p, err := orgPlacement(dir, "")
	if err != nil {
		return err
	}
	path := orgPlacementPath(dir, "")
	if err := d.orgPinned(p, path); err != nil {
		return err
	}
	if p.Teardown {
		return errors.Newf("the %s organization is torn down (teardown in placement.json): bedrock org serve, with no application, is its way back", p.Prefix)
	}
	if len(envs) == 0 {
		envs = p.OutOfService[app]
	}
	removed, err := p.PutInService(app, envs)
	if err != nil {
		return err
	}
	if err := refuseTornDown(ctx, d.deploy, p.StateBucket, app, removed); err != nil {
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
	fmt.Fprintf(out, "Recorded %s in service in %s (outOfService in %s) and rendered the %s foundation into %s: %d owned file(s) written; 2-net's applications.auto.tfvars routes %s's hostnames there again.\n", app, strings.Join(removed, ", "), path, p.Prefix, dir, written.Owned, app)
	line := fmt.Sprintf("- %s: bedrock org serve %s: %s's hostnames in %s return to 2-net on this pull request (outOfService in placement.json).\n", time.Now().UTC().Format("2006-01-02 15:04Z"), app, app, strings.Join(removed, ", "))
	if err := appendJournalLine(dir, line); err != nil {
		return err
	}
	fmt.Fprintf(out, "A line went into %s.\n\nNext: commit placement.json, %s and the rendered files, open the pull request and merge it; the plan run shows %s's host rules added to 2-net and the apply trigger applies it. The hostnames answer once it has.\n", orgJournal, orgJournal, app)

	return nil
}

// serveOrg is the organization's way back: every application's stacks applied and run
// again, teardown off the placement, 2-net rendered serving, the journal line.
func (d deps) serveOrg(cmd *cobra.Command, dir string) error {
	out, ctx := cmd.OutOrStdout(), cmd.Context()
	p, err := orgPlacement(dir, "")
	if err != nil {
		return err
	}
	path := orgPlacementPath(dir, "")
	if err := d.orgPinned(p, path); err != nil {
		return err
	}
	if !p.Teardown {
		return errors.Newf("the %s organization is in service (no teardown in placement.json); an application out of service is put back with bedrock org serve <app>", p.Prefix)
	}
	for _, app := range p.Served() {
		if err := refuseTornDown(ctx, d.deploy, p.StateBucket, app, servedEnvironments(p, app)); err != nil {
			return err
		}
	}
	p.Teardown = false
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
	fmt.Fprintf(out, "Removed teardown from %s and rendered the %s foundation into %s: %d owned file(s) written; 2-net's applications.auto.tfvars routes every host again and sets serving = true.\n", path, p.Prefix, dir, written.Owned)
	line := fmt.Sprintf("- %s: bedrock org serve: the hosts and the load balancer's listeners return to 2-net on this pull request (teardown removed from placement.json); the applications (%s) are applied and run again.\n", time.Now().UTC().Format("2006-01-02 15:04Z"), applicationsList(p))
	if err := appendJournalLine(dir, line); err != nil {
		return err
	}
	fmt.Fprintf(out, "A line went into %s.\n\nNext: commit placement.json, %s and the rendered files, open the pull request and merge it; the plan run shows 2-net's listeners and host rules added and the apply trigger applies it. The hostnames answer once it has.\n", orgJournal, orgJournal)

	return nil
}

// servedEnvironments are the environments 2-net serves the application in once the
// placement is back in service: every one not out of service on its own.
func servedEnvironments(p *org.Placement, app string) []string {
	var envs []string
	for _, env := range org.Environments {
		if !slices.Contains(p.OutOfService[app], env) {
			envs = append(envs, env)
		}
	}

	return envs
}

// refuseTornDown refuses the way back for an environment whose stack is not back: no
// stack applied, or a teardown of it with no release run since.
func refuseTornDown(ctx context.Context, clients *deploy.Clients, bucket, app string, envs []string) error {
	standings, err := deploy.ApplicationStanding(ctx, clients, bucket, app, envs)
	if err != nil {
		return err
	}
	for _, s := range standings {
		switch {
		case !s.Stack:
			return errors.Newf("%s has no stack in %s: a host names a backend service that must exist. Run the operations workflow's apply action there, then bedrock rerun %s <release>, then bedrock org serve again; or bedrock org teardown %s %s leaves it out", app, s.Env, s.Env, app, s.Env)
		case s.TornDown:
			return errors.Newf("%s is torn down in %s, with no release run since: run the operations workflow's apply action there, then bedrock rerun %s <release>, then bedrock org serve again", app, s.Env, s.Env)
		}
	}

	return nil
}
