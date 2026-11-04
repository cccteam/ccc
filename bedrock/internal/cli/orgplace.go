// orgplace.go is bedrock org place: the environments a registered application is placed
// in, changed after its registration, in either direction.

package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

func newOrgPlace(d deps) *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   "place <code> <environment>...",
		Short: "Set the environments a registered application is placed in, a prefix of the promotion order",
		Long: `place sets the environments an application registered in full is placed in: a prefix of
the promotion order (tst; tst stg; tst stg prd), named in full, recorded in its entry in
placement.json (environments). Registration created its things in the environments it
was registered with; place is how a proof application that proved itself in the first
environment reaches the next, and how an application leaves one.

An environment added starts at the first step of the application's registration there, as
bedrock org register starts every environment: this render carries 2-env's identities,
database, repository link and triggers for it and 1-org's GitHub Environment, the pull
request's apply makes them, and bedrock org register <code> --next moves on as a
registration does: the grants there, then the stack's first apply there through the
operations workflow, then the hostnames there. The application's own placement
(infrastructure/placement.json in its repository) lists the same environments, with the
environment's project under projects and its secret versions pinned; the stack is rendered
again there (bedrock render) before its first apply in the environment, and its release
line reaches the environment from the next release on. The capacity count counts the
databases the environment adds, and refuses a placement past an instance.

An environment dropped must be out of service already (bedrock org teardown <code>
<environment>, its pull request applied, then bedrock teardown from the application's
checkout there), so that nothing the layers own there is left behind; place refuses while
the application is in service there. This render then leaves its identities, grants, link,
triggers and GitHub Environment there for the pull request's apply to delete, and the
application's own placement lists the shorter prefix. Run from the repository root, or
name it with --dir.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.placeApplication(cmd, dir, args[0], args[1:])
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the repository root")

	return cmd
}

// placeApplication changes where the application is placed: the placement written and
// rendered, the capacity counted, the journal line, and what follows printed.
func (d deps) placeApplication(cmd *cobra.Command, dir, app string, envs []string) error {
	out := cmd.OutOrStdout()
	p, err := orgPlacement(dir, "")
	if err != nil {
		return err
	}
	if err := hostingOnly(p, "place"); err != nil {
		return err
	}
	path := orgPlacementPath(dir, "")
	if err := d.orgPinned(p, path); err != nil {
		return err
	}
	repository := p.Repository(app)
	added, dropped, err := p.Place(app, envs)
	if err != nil {
		return err
	}
	if len(added) > 0 {
		capacities, err := p.InstanceCapacities(dir)
		if err != nil {
			return err
		}
		var exceeded []string
		for _, c := range capacities {
			if c.Exceeded() {
				exceeded = append(exceeded, c.Problem())
			}
		}
		if len(exceeded) > 0 {
			return errors.Newf("place %s: %s", app, strings.Join(exceeded, "; "))
		}
	}
	if err := p.Write(path); err != nil {
		return err
	}
	files, err := org.Render(p)
	if err != nil {
		return err
	}
	written, err := org.Write(p, files, dir)
	if err != nil {
		return err
	}
	placed := strings.Join(p.PlacedEnvironments(app), ", ")
	fmt.Fprintf(out, "Placed %s in %s (environments in %s) and rendered the %s foundation into %s: %d owned file(s) written.\n", repository, placed, path, p.Prefix, dir, written.Owned)
	if len(added) > 0 {
		fmt.Fprint(out, placeSequence(p, app, repository, added))
	}
	if len(dropped) > 0 {
		fmt.Fprintf(out, `
%s leaves %s: the pull request's apply deletes its identities, repository link, triggers
and grants there (2-env, 1-org, 2-shr, 2-spn) and its GitHub Environment, and 2-net routes
nothing to it there already. Then, in %s's repository, set environments in
infrastructure/placement.json to %s, drop the environment's entries (projects, pinned
secret versions, hostnames), run bedrock render, and merge: its release line ends at %s.
`, repository, strings.Join(dropped, ", "), repository, hclStrings(p.PlacedEnvironments(app)), p.PlacedEnvironments(app)[len(p.PlacedEnvironments(app))-1])
	}
	line := fmt.Sprintf("- %s: bedrock org place %s %s: %s is placed in %s (environments in placement.json)%s%s.\n", time.Now().UTC().Format("2006-01-02 15:04Z"), app, strings.Join(envs, " "), repository, placed, placeChange(" adding ", added), placeChange(" dropping ", dropped))
	if err := appendJournalLine(dir, line); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nA line went into %s.\n", orgJournal)

	return nil
}

// placeChange is the journal's phrase for the environments a placement adds or drops;
// nothing for none.
func placeChange(verb string, envs []string) string {
	if len(envs) == 0 {
		return ""
	}

	return "," + verb + strings.Join(envs, ", ")
}

// hclStrings is a list of environments as a placement file writes it.
func hclStrings(envs []string) string {
	return `["` + strings.Join(envs, `", "`) + `"]`
}

// placeSequence says how the application reaches the layers in the environments added,
// as applySequence says it for a registration: the pull requests in order and the one run
// of the apply trigger by hand, the application named by its repository.
func placeSequence(p *org.Placement, app, repository string, added []string) string {
	envs := strings.Join(added, ", ")

	return fmt.Sprintf(`
%[4]s reaches %[5]s through the Cloud Build triggers, as pull requests into %[1]s (each merge
applies the layers it touches, in layer order). placement.json records how far the
registration there has gone (registration), the layers render what that step may apply,
and every pull request carries every rendered file:
  1. This render, %[4]s at its identities in %[5]s: its GitHub Environment (1-org), then its
     identities, database, repository link and triggers there (2-env).
  2. The apply trigger by hand with 2-env, in a terminal, for the environment before each
     one added and for %[5]s:
     %[3]s
     the environment before grants the new environment's deploy identity read on its
     records bucket, from state the first pass did not have.
  3. bedrock org register %[2]s --next, then the pull request it renders: the public-invoker
     tag (1-org), the registry grants (2-shr) and the database admins (2-spn) in %[5]s, on
     identities that exist now.
  4. In %[4]s's repository: environments in infrastructure/placement.json lists %[6]s, with
     the environment's project under projects (bedrock org render prints the ids) and its
     secret versions pinned; bedrock render, merge, then the stack's first apply in %[5]s
     through its operations workflow (Actions, operations, apply, with the environment).
  5. bedrock org register %[2]s --next once more, then the pull request it renders: the
     hostnames in %[5]s (2-net), onto the backends the stack created.
  6. After 2-net's apply: bedrock domain check. The next release promotes into %[5]s.
`, p.GithubDefaultBranch, app, triggerRun(p, ""), repository, envs, hclStrings(p.PlacedEnvironments(app)))
}
