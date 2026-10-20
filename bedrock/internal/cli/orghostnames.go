// orghostnames.go is org hostnames: the hostnames an application's stack serves, read
// from its state in every environment and recorded in the organization's placement, so
// 2-net's hosts follow a label the application changed in its own placement. The
// registration's completion records them the first time (org register --next).

package cli

import (
	"fmt"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

const (
	orgHostnamesUse = "hostnames"
	orgHostnamesCmd = "org hostnames"
)

// newOrgHostnames is org hostnames.
func newOrgHostnames(d deps) *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   orgHostnamesUse + " <app>",
		Short: "Record the hostnames an application's stack serves, read from its state in every environment, so 2-net's host rules follow a changed label",
		Long: `hostnames records what an application's stack serves, by a person at their own computer with their own
sign-in (gcloud auth application-default login). An application names each site's hostname label in
its own placement (infrastructure/placement.json, hostnames; the code by default), and its stack
serves <label>-<env> under the apps domain, the bare <label> in production, each with its -next
twin. The organization never invents a hostname: when the registration completes (bedrock org
register <app> --next) it records the hostnames from the stack's state in every environment, and
2-net's host rules render from the record. After the application changes a label and its stacks
are applied again, this command reads the states again, records what they serve now, refuses a
hostname another application serves, renders 2-net, writes the journal line and leaves the working
tree for a pull request: its merge has the apply trigger move the host rules. Until then the old
hostnames answer and the new ones do not.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.recordApplicationHostnames(cmd, dir, args[0])
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")

	return cmd
}

// recordApplicationHostnames reads the application's hostnames from its stacks' states,
// records them, renders and leaves the pull request.
func (d deps) recordApplicationHostnames(cmd *cobra.Command, dir, app string) error {
	out, ctx := cmd.OutOrStdout(), cmd.Context()
	p, err := orgPlacement(dir, "")
	if err != nil {
		return err
	}
	path := orgPlacementPath(dir, "")
	if err := d.orgPinned(p, path); err != nil {
		return err
	}
	repository := p.Repository(app)
	if !p.Registered(app) {
		return errors.Newf("application %q is not registered (bedrock org register %s)", app, app)
	}
	if p.Step(app) != "" {
		return errors.Newf("%s is at the %s step of its registration: its hostnames are recorded when the registration completes (bedrock org register %s --next)", repository, p.Step(app), app)
	}
	changed, err := d.recordHostnames(ctx, p, app, repository)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Fprintf(out, "%s's hostnames are recorded as its stacks serve them (%s); nothing to change.\n", repository, hostnamesSummary(p, app))

		return nil
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
	fmt.Fprintf(out, "Recorded %s's hostnames in %s (%s) and rendered the %s foundation into %s: %d owned file(s) written; 2-net's applications.auto.tfvars routes them.\n", repository, path, hostnamesSummary(p, app), p.Prefix, dir, written.Owned)
	line := fmt.Sprintf("- %s: bedrock org hostnames %s: %s's hostnames recorded from its stacks' states (%s); 2-net's host rules follow on this pull request.\n", time.Now().UTC().Format("2006-01-02 15:04Z"), app, repository, hostnamesSummary(p, app))
	if err := appendJournalLine(dir, line); err != nil {
		return err
	}
	fmt.Fprintf(out, "A line went into %s.\n\nNext: commit placement.json, %s and the rendered files, open the pull request and merge it; the plan run shows the host rules moved and the apply trigger applies it. The new hostnames answer once it has, and the old ones stop.\n", orgJournal, orgJournal)

	return nil
}
