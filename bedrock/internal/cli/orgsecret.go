// orgsecret.go is the organization form of secret add and pin: run in the organization's
// infrastructure repository, they take one of the organization's secrets (the
// infrastructure and deployer GitHub Apps' private keys, both in the boot project) in
// place of an application's environment and variable.

package cli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/prompt"
	"github.com/cccteam/ccc/bedrock/internal/where"
)

// The secret command's two verbs, as the refusals name them.
const (
	addVerb = "add"
	pinVerb = "pin"
)

// orgRepository is the organization's infrastructure repository the command runs in: its
// root (--dir as given, else the repository root found from the working directory) and
// its placement, when the placement.json there names an organization (organizationId).
// An application's repository has none, and nothing is returned for it, nor where no
// repository is found: the application form reports that as it always has.
func (d deps) orgRepository(dir string) (root string, p *org.Placement, err error) {
	root = dir
	if root == "" {
		start := d.cwd
		if start == "" {
			start, err = os.Getwd()
			if err != nil {
				return "", nil, errors.Wrap(err, "os.Getwd()")
			}
		}
		root = repoRootOf(start)
		if root == "" {
			return "", nil, nil
		}
	}
	file := filepath.Join(root, orgPlacementFile)
	isOrg, err := org.IsOrganizationPlacement(file)
	if err != nil || !isOrg {
		return "", nil, err
	}
	p, err = org.ReadPlacement(file)
	if err != nil {
		return "", nil, err
	}

	return root, p, nil
}

// repoRootOf is the repository root above start, or empty when there is none.
func repoRootOf(start string) string {
	root, err := where.RepoRoot(start)
	if err != nil {
		return ""
	}

	return root
}

// refuseOrgSecret refuses an organization secret's name given in an application's
// repository, where the arguments are an environment and a variable.
func refuseOrgSecret(args []string, verb string) error {
	if len(args) == 0 || !slices.Contains(org.Secrets, args[0]) {
		return nil
	}

	return errors.Newf("%s is an organization secret, added and pinned from the organization's infrastructure repository (the one whose placement.json names the organization, organizationId); in an application's repository secret %s takes [env] [VARIABLE]", args[0], verb)
}

// orgFlags refuses the flags that name an application's layer, which an organization
// secret has none of.
func orgFlags(f *pinFlags) error {
	if f.app != "" || f.layer != "" {
		return errors.New("--app and --layer name an application's layer: an organization secret takes neither")
	}

	return nil
}

// orgSecretArgs is the organization secret's name, from the arguments or asked at the
// terminal; next is the index of the argument after it. An environment in the secret's
// place is an application's argument, refused here with what the repository takes.
func orgSecretArgs(args []string, ask *prompt.Prompter, p *org.Placement, verb string) (name string, next int, err error) {
	name, err = argument(args, 0, ask, "organization secret", "Which secret?", func() ([]prompt.Choice, error) {
		return plain(org.Secrets, nil)
	})
	if err != nil {
		return "", 0, err
	}
	if slices.Contains(org.Environments, name) {
		return "", 0, errors.Newf("this is the organization's infrastructure repository (placement.json names organization %s): secret %s takes an organization secret here, %s or %s; an application's secret ([env] [VARIABLE]) is %s from the application's repository", p.OrganizationID, verb, org.InfrastructureKey, org.DeployerKey, verbPast(verb))
	}
	if err := org.ValidateSecret(name); err != nil {
		return "", 0, err
	}

	return name, 1, nil
}

// verbPast is the command's verb in a sentence: added, or pinned.
func verbPast(verb string) string {
	if verb == addVerb {
		return "added"
	}

	return "pinned"
}

// tooManyOrgArgs refuses arguments past the ones the secret takes: both organization
// secrets live in the boot project and take no environment. use spells the command line
// the secret takes.
func tooManyOrgArgs(args []string, name, use string, want int) error {
	if len(args) <= want {
		return nil
	}

	return errors.Newf("%s lives in the boot project and takes no environment: %s (given %s)", name, use, strings.Join(args, " "))
}

// orgSecretPlace is the secret's project and container: --project and --container as
// given, else the boot project the placement records and the container 0-bootstrap
// names.
func orgSecretPlace(p *org.Placement, f *pinFlags, name string) (project, container string, err error) {
	project, container = f.project, f.container
	if project == "" {
		project, err = p.SecretProject()
		if err != nil {
			return "", "", err
		}
	}
	if container == "" {
		container = p.SecretContainer(name)
	}

	return project, container, nil
}

// addOrgSecret is secret add's organization form: the value stored as a new version of
// the secret's container.
func (d deps) addOrgSecret(cmd *cobra.Command, args []string, f *addFlags, p *org.Placement) error {
	if err := orgFlags(&f.pinFlags); err != nil {
		return err
	}
	name, next, err := orgSecretArgs(args, d.asker(cmd), p, addVerb)
	if err != nil {
		return err
	}
	if err := tooManyOrgArgs(args, name, "bedrock secret add "+name, next); err != nil {
		return err
	}
	project, container, err := orgSecretPlace(p, &f.pinFlags, name)
	if err != nil {
		return err
	}
	value, err := d.readValue(cmd, f.fromFile, name)
	if err != nil {
		return err
	}
	r, err := org.AddSecret(cmd.Context(), d.secrets, &org.SecretAddRequest{Secret: name, Project: project, Container: container, Value: value})
	if err != nil {
		return err
	}
	r.Write(cmd.OutOrStdout())

	return nil
}

// pinOrgSecret is secret pin's organization form: the version written where the layers
// read it, after Secret Manager confirms it is enabled.
func (d deps) pinOrgSecret(cmd *cobra.Command, args []string, f *pinFlags, root string, p *org.Placement) error {
	if err := orgFlags(f); err != nil {
		return err
	}
	ask := d.asker(cmd)
	name, next, err := orgSecretArgs(args, ask, p, pinVerb)
	if err != nil {
		return err
	}
	if err := tooManyOrgArgs(args, name, "bedrock secret pin "+name+" <version>", next+1); err != nil {
		return err
	}
	project, container, err := orgSecretPlace(p, f, name)
	if err != nil {
		return err
	}
	version, err := d.resolveVersion(cmd.Context(), args, next, ask, project, container, "the triggers' runs could not read it")
	if err != nil {
		return err
	}
	r, err := org.PinSecret(cmd.Context(), d.secrets, p, &org.SecretPinRequest{Secret: name, Version: version, Dir: root, Project: project, Container: container})
	if err != nil {
		return err
	}
	r.Write(cmd.OutOrStdout())

	return nil
}

// completeOrgSecret lists what the next argument of the organization form can be: the
// secrets, and, for pin, the container's versions with their states; nothing when it
// cannot be told.
func (d deps) completeOrgSecret(ctx context.Context, args []string, f *pinFlags, p *org.Placement, versions bool) []string {
	if len(args) == 0 {
		return org.Secrets
	}
	name := args[0]
	if org.ValidateSecret(name) != nil {
		return nil
	}
	if !versions || len(args) != 1 {
		return nil
	}
	project, container, err := orgSecretPlace(p, f, name)
	if err != nil {
		return nil
	}
	found, err := where.Versions(ctx, d.secrets, project, container)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(found))
	for _, v := range found {
		names = append(names, v.Name+"\t"+v.State)
	}

	return names
}
