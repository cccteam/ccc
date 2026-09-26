// secret.go is the secret command: the secret versions an environment runs, pinned in
// the placement.

package cli

import (
	"context"
	"path/filepath"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/prompt"
	"github.com/cccteam/ccc/bedrock/internal/secret"
	"github.com/cccteam/ccc/bedrock/internal/where"
)

const (
	// The labels the environment project and the secret containers are found by: the
	// environment, and the layer that created them (1-org for the projects, 3-app-<app>
	// for the containers, a label value admitting no slash).
	environmentLabel = "environment"
	sourcePathLabel  = "terraform_source_path"
	orgSourcePath    = "1-org"
	appSourcePath    = "3-app-"
	// secretFlag is the older name of --container, kept hidden.
	secretFlag = "secret"
)

func newSecret(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Pin secret versions through the placement",
		Long: `secret holds the commands that pin, in an application layer's placement, the version of a
secret an environment runs; the layer's cloud-run.tf mounts that version.`,
	}
	cmd.AddCommand(newSecretPin(d))

	return cmd
}

// pinFlags are the flags of secret pin, each an override of what the command finds.
type pinFlags struct {
	dir       string
	app       string
	layer     string
	project   string
	container string
}

func newSecretPin(d deps) *cobra.Command {
	var f pinFlags

	cmd := &cobra.Command{
		Use:   "pin [env] [VARIABLE] [version]",
		Short: "Pin the version of a secret an environment runs",
		Long: `pin writes the version of a secret an environment runs into the application layer's
terraform.tfvars under secret_versions.<env>.<VARIABLE>. The layer's cloud-run.tf mounts the
pinned version in the environment's revision template; nothing is rolled out here. The pull
request's plan for that environment shows the template change and nothing elsewhere, and
after the apply the release is re-run in the environment to move traffic to the new revision.

Run from anywhere inside the repository, it finds the rest: the infrastructure root (the
repository root, or its infrastructure directory; --dir overrides), the application (the one
layer under 3-app; --app chooses among several), the environment project (the active project
labeled environment=<env> and terraform_source_path=1-org; --project overrides) and the
secret container (the one labeled terraform_source_path=3-app-<app> whose name ends with the
variable's kebab case, -cookie-key for APP_COOKIE_KEY; --container overrides). Secret Manager
is asked, with Application Default Credentials, whether the version exists and is enabled
before the placement is edited.

An argument left out is asked for at the terminal, with the choices listed: the environments
the placement pins for, the secret variables the code declares (read from the code beside the
infrastructure directory, else from the layer's locals.tf), and the container's versions with
their state, newest first. When standard input is not a terminal, a missing argument is
refused with the same choices listed. The arguments complete in the shell the same way
(bedrock completion <shell>).

It is refused when an argument has the wrong shape (<env> is tst, stg or prd; <VARIABLE> is
upper snake case under APP_; <version> is a positive integer or latest), when the project or
the container cannot be told apart (none or several found), when the placement has no
secret_versions map or that map has no <env> entry, and when the version is disabled or
absent. A variable already pinned to the version leaves the file as it is.`,
		Args: cobra.MaximumNArgs(3),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			return d.completePin(cmd.Context(), args, &f), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := d.resolvePin(cmd, args, &f)
			if err != nil {
				return err
			}
			r, err := secret.Pin(cmd.Context(), d.secrets, req)
			if err != nil {
				return err
			}
			r.Write(cmd.OutOrStdout())

			return nil
		},
	}

	cmd.Flags().StringVar(&f.dir, "dir", "", "the infrastructure root (default: found from the working directory)")
	cmd.Flags().StringVar(&f.app, "app", "", "the application whose secret is pinned (default: the one layer under 3-app)")
	cmd.Flags().StringVar(&f.layer, "layer", "", "the layer whose placement holds the pins (default: 3-app/<app>)")
	cmd.Flags().StringVar(&f.project, "project", "", "the environment project to verify the version in (default: found by its labels)")
	cmd.Flags().StringVar(&f.container, "container", "", "the secret container's name in the project (default: found by its labels and the variable)")
	cmd.Flags().StringVar(&f.container, secretFlag, "", "an older name for --container")
	_ = cmd.Flags().MarkHidden(secretFlag)
	_ = cmd.MarkFlagDirname("dir")
	_ = cmd.RegisterFlagCompletionFunc("app", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		infraRoot, _, err := d.infrastructureRoot(f.dir)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		apps, err := where.Applications(infraRoot)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		return apps, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

// pinPlace is where a pin goes: the infrastructure root, the application, its layer,
// and the application's source directory when known.
type pinPlace struct {
	infraRoot string
	appDir    string
	app       string
	layerDir  string
}

// place finds where the pin goes from the flags and the working directory.
func (d deps) place(f *pinFlags) (*pinPlace, error) {
	infraRoot, appDir, err := d.infrastructureRoot(f.dir)
	if err != nil {
		return nil, err
	}
	application := f.app
	if application == "" {
		application, err = where.SingleApplication(infraRoot)
		if err != nil {
			return nil, err
		}
	}
	layerDir := where.LayerDir(infraRoot, application)
	if f.layer != "" {
		layerDir = filepath.Join(infraRoot, f.layer)
	}

	return &pinPlace{infraRoot: infraRoot, appDir: appDir, app: application, layerDir: layerDir}, nil
}

// resolvePin fills the request from the arguments, the flags and what is found, asking
// at the terminal for an argument left out. Each argument's shape is checked as it comes,
// before anything is looked up online with it.
func (d deps) resolvePin(cmd *cobra.Command, args []string, f *pinFlags) (*secret.Request, error) {
	ctx := cmd.Context()
	ask := d.asker(cmd)
	place, err := d.place(f)
	if err != nil {
		return nil, err
	}
	env, err := argument(args, 0, ask, "environment", "Which environment?", func() ([]prompt.Choice, error) {
		return plain(where.LayerEnvironments(place.layerDir))
	})
	if err != nil {
		return nil, err
	}
	if err := secret.ValidateEnv(env); err != nil {
		return nil, err
	}
	variable, err := argument(args, 1, ask, "variable", "Which variable?", func() ([]prompt.Choice, error) {
		return plain(where.Variables(place.appDir, place.layerDir))
	})
	if err != nil {
		return nil, err
	}
	if err := secret.ValidateVariable(variable); err != nil {
		return nil, err
	}
	project, err := d.resolveProject(ctx, f.project, env)
	if err != nil {
		return nil, err
	}
	container, err := d.resolveContainer(ctx, f.container, project, place.app, variable)
	if err != nil {
		return nil, err
	}
	version, err := d.resolveVersion(ctx, args, ask, project, container)
	if err != nil {
		return nil, err
	}

	return &secret.Request{
		App:       place.app,
		Env:       env,
		Variable:  variable,
		Version:   version,
		Dir:       place.infraRoot,
		Layer:     f.layer,
		Project:   project,
		Container: container,
	}, nil
}

// resolveProject is the environment project: --project as given, else the one active
// project labeled with the environment and the organization layer.
func (d deps) resolveProject(ctx context.Context, flag, env string) (string, error) {
	if flag != "" {
		return flag, nil
	}

	return where.ProjectByLabels(ctx, d.projects, map[string]string{environmentLabel: env, sourcePathLabel: orgSourcePath})
}

// resolveContainer is the secret container: --container as given, else the one
// container in the project labeled with the application layer whose name ends with the
// variable's kebab case.
func (d deps) resolveContainer(ctx context.Context, flag, project, application, variable string) (string, error) {
	if flag != "" {
		return flag, nil
	}

	return where.ContainerByLabels(ctx, d.secrets, project, map[string]string{sourcePathLabel: appSourcePath + application}, secret.ContainerSuffix(variable))
}

// resolveVersion is the version: the third argument as given, else the one chosen at
// the terminal among the container's versions, newest first, each with its state. A
// version chosen that is not enabled is refused: the environment could not mount it.
func (d deps) resolveVersion(ctx context.Context, args []string, ask *prompt.Prompter, project, container string) (string, error) {
	states := map[string]string{}
	version, err := argument(args, 2, ask, "version", "Which version of "+container+"?", func() ([]prompt.Choice, error) {
		versions, err := where.Versions(ctx, d.secrets, project, container)
		if err != nil {
			return nil, err
		}
		choices := make([]prompt.Choice, 0, len(versions))
		for _, v := range versions {
			states[v.Name] = v.State
			choices = append(choices, prompt.Choice{Value: v.Name, Note: v.State})
		}

		return choices, nil
	})
	if err != nil {
		return "", err
	}
	if err := secret.ValidateVersion(version); err != nil {
		return "", err
	}
	if state, chosen := states[version]; chosen && state != secret.Enabled {
		return "", errors.Newf("version %s of %s in project %s is %s, not %s: the environment could not mount it; choose an enabled version", version, container, project, state, secret.Enabled)
	}

	return version, nil
}

// completePin lists what the next argument of secret pin can be, through the same
// discovery the command runs, or nothing when it cannot be told: a failed discovery
// completes nothing rather than erroring. Versions carry their state as the description.
func (d deps) completePin(ctx context.Context, args []string, f *pinFlags) []string {
	place, err := d.place(f)
	if err != nil {
		return nil
	}
	switch len(args) {
	case 0:
		envs, err := where.LayerEnvironments(place.layerDir)
		if err != nil {
			return nil
		}

		return envs
	case 1:
		variables, err := where.Variables(place.appDir, place.layerDir)
		if err != nil {
			return nil
		}

		return variables
	case 2:
		return d.completeVersions(ctx, f, place.app, args[0], args[1])
	default:
		return nil
	}
}

// completeVersions lists the container's versions for completion, each with its state
// as the description, or nothing when the project, the container or the versions cannot
// be found.
func (d deps) completeVersions(ctx context.Context, f *pinFlags, application, env, variable string) []string {
	project, err := d.resolveProject(ctx, f.project, env)
	if err != nil {
		return nil
	}
	container, err := d.resolveContainer(ctx, f.container, project, application, variable)
	if err != nil {
		return nil
	}
	versions, err := where.Versions(ctx, d.secrets, project, container)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(versions))
	for _, v := range versions {
		names = append(names, v.Name+"\t"+v.State)
	}

	return names
}
