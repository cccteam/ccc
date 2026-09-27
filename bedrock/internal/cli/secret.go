// secret.go is the secret command: a secret value added ahead of the release that reads
// it, and the secret versions an environment runs, pinned in the placement.

package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/derive"
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
	// secretCommand is the command's name.
	secretCommand = "secret"
)

func newSecret(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   secretCommand,
		Short: "Add secret values and pin the versions an environment runs",
		Long: `secret holds the commands that add a secret value to Secret Manager ahead of the release
that reads it (add), and that pin, in an application layer's placement, the version of a
secret an environment runs (pin); the layer's cloud-run.tf mounts that version.`,
	}
	cmd.AddCommand(newSecretAdd(d))
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
repository root, or its infrastructure directory; --dir overrides), the application (the
application repository's own stack in its infrastructure directory, or the one layer under
3-app; --app chooses among several), the environment project (the active project
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
	cmd.Flags().StringVar(&f.app, "app", "", "the application whose secret is pinned (default: the one application layer)")
	cmd.Flags().StringVar(&f.layer, "layer", "", "the layer whose placement holds the pins (default: the application's layer)")
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

// pinPlace is where a pin goes: the infrastructure root, the application, its layer
// (relative to the root, and as a directory), and the application's source directory
// when known.
type pinPlace struct {
	infraRoot string
	appDir    string
	app       string
	layer     string
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
	layer := f.layer
	if layer == "" {
		layer = where.Layer(infraRoot, application)
	}

	return &pinPlace{infraRoot: infraRoot, appDir: appDir, app: application, layer: layer, layerDir: filepath.Join(infraRoot, layer)}, nil
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
	// A build-time secret (the placement's build_secrets for the environment) is pinned
	// the same way, under its own map.
	build, err := secret.BuildSecretNames(place.layerDir, env)
	if err != nil {
		return nil, err
	}
	variable, err := argument(args, 1, ask, "variable", "Which variable?", func() ([]prompt.Choice, error) {
		variables, err := where.Variables(place.appDir, place.layerDir)
		if err != nil {
			return nil, err
		}

		return plain(append(variables, build...), nil)
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
		Layer:     place.layer,
		Project:   project,
		Container: container,
		Build:     slices.Contains(build, variable),
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
		build, err := secret.BuildSecretNames(place.layerDir, args[0])
		if err != nil {
			return nil
		}

		return append(variables, build...)
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

// addFlags are the flags of secret add: the overrides secret pin takes, and where the
// value comes from.
type addFlags struct {
	pinFlags
	fromFile string
}

func newSecretAdd(d deps) *cobra.Command {
	var f addFlags

	cmd := &cobra.Command{
		Use:   "add [env] [VARIABLE]",
		Short: "Add a secret value as a new version, creating the container ahead of the release",
		Long: `add stores a secret value in Secret Manager as a new version of the container the application
stack names for the variable in the environment (<prefix>-<env>-gbl-<app>-<kebab name>:
imp-tst-gbl-quill-mail-api-key for APP_MAIL_API_KEY of quill in tst), creating the container
when the project has none by that name. An operator runs it ahead of the release that first
reads the secret: the stack's next apply adopts a container that exists (its secret-manager.tf
imports every declared container the project holds and the state does not), so the value is in
place before the code that needs it is deployed. Nothing is pinned or rolled out here: the
command prints the secret pin to make, and the pull request that pins the version carries the
change through the plan.

The value is read from --from-file (a path, or - for standard input), from standard input
when it is not a terminal, else asked for at the terminal without echo. It is stored as
given, including a trailing newline when the file has one: write the file without one for a
key or a token, and keep it for a document such as a credentials JSON.

Run from anywhere inside the repository, it finds the rest as secret pin does: the
infrastructure root (--dir overrides), the application (--app), the environment project (the
active project labeled environment=<env> and terraform_source_path=1-org; --project
overrides) and the container's name, from the placement beside the layer (placement.json;
without one, the container carrying the layer's label whose name ends with the variable's
kebab case; --container overrides). The variable must be one the code declares as a secret, or
a build-time secret the placement declares for the environment (build_secrets in
terraform.tfvars; the stack grants the deploy identity accessor on its container and the
pipeline passes it to the image build as a BuildKit secret);
the choices are listed when it is left out.

It is refused when an argument has the wrong shape (<env> is tst, stg or prd; <VARIABLE> is
upper snake case under APP_), when the code declares no such secret variable, when the
project cannot be told apart, when the container's name cannot be found, and when the
value is empty. Creating a container needs secretmanager.secrets.create in the environment
project and adding a version secretmanager.versions.add: the secretOperator role 1-org
defines, granted on the environment project to 2-env's secret_operators.`,
		Args: cobra.MaximumNArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) >= 2 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}

			return d.completePin(cmd.Context(), args, &f.pinFlags), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := d.resolveAdd(cmd, args, &f)
			if err != nil {
				return err
			}
			r, err := secret.Add(cmd.Context(), d.secrets, req)
			if err != nil {
				return err
			}
			r.Write(cmd.OutOrStdout())

			return nil
		},
	}

	cmd.Flags().StringVar(&f.dir, "dir", "", "the infrastructure root (default: found from the working directory)")
	cmd.Flags().StringVar(&f.app, "app", "", "the application whose secret is added (default: the one application layer)")
	cmd.Flags().StringVar(&f.layer, "layer", "", "the layer whose placement names the container (default: the application's layer)")
	cmd.Flags().StringVar(&f.project, "project", "", "the environment project the container lives in (default: found by its labels)")
	cmd.Flags().StringVar(&f.container, "container", "", "the container's name in the project (default: the stack's derivation from the placement)")
	cmd.Flags().StringVar(&f.fromFile, "from-file", "", "the file holding the value, or - for standard input (default: standard input when it is not a terminal, else asked without echo)")
	_ = cmd.MarkFlagDirname("dir")
	_ = cmd.MarkFlagFilename("from-file")

	return cmd
}

// resolveAdd fills the request from the arguments, the flags and what is found, asking
// at the terminal for an argument left out. The variable is checked against the secrets
// the code declares before anything is looked up online with it.
func (d deps) resolveAdd(cmd *cobra.Command, args []string, f *addFlags) (*secret.AddRequest, error) {
	ctx := cmd.Context()
	ask := d.asker(cmd)
	place, err := d.place(&f.pinFlags)
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
	declared, err := where.Variables(place.appDir, place.layerDir)
	if err != nil {
		return nil, err
	}
	// A build-time secret the placement declares for the environment (build_secrets)
	// gets its container and value the same way; the stack grants the deploy identity
	// accessor on it and the pipeline passes it to the image build.
	build, err := secret.BuildSecretNames(place.layerDir, env)
	if err != nil {
		return nil, err
	}
	variable, err := argument(args, 1, ask, "variable", "Which variable?", func() ([]prompt.Choice, error) {
		return plain(append(slices.Clone(declared), build...), nil)
	})
	if err != nil {
		return nil, err
	}
	if err := secret.ValidateVariable(variable); err != nil {
		return nil, err
	}
	if !slices.Contains(declared, variable) && !slices.Contains(build, variable) {
		declares := "none"
		if len(build) > 0 {
			declares = strings.Join(build, ", ")
		}

		return nil, errors.Newf("the code declares no secret variable %s (it declares %s): a container is added only for a secret the application reads, or for a build secret the placement declares for %s (it declares %s)", variable, strings.Join(declared, ", "), env, declares)
	}
	project, err := d.resolveProject(ctx, f.project, env)
	if err != nil {
		return nil, err
	}
	container, labels, err := d.resolveNewContainer(ctx, f.container, project, place, env, variable)
	if err != nil {
		return nil, err
	}
	value, err := d.readValue(cmd, f.fromFile, variable)
	if err != nil {
		return nil, err
	}

	return &secret.AddRequest{
		App:       place.app,
		Env:       env,
		Variable:  variable,
		Project:   project,
		Container: container,
		Labels:    labels,
		Value:     value,
	}, nil
}

// resolveNewContainer is the container a value goes into, and the labels it is created
// with when the project lacks it: --container as given, else the stack's derivation from
// the placement beside the layer, else, without a placement, the one container carrying
// the layer's label whose name ends with the variable's kebab case.
func (d deps) resolveNewContainer(ctx context.Context, flag, project string, place *pinPlace, env, variable string) (container string, labels map[string]string, err error) {
	p, err := where.Placement(place.layerDir)
	if err != nil {
		return "", nil, err
	}
	labels = secret.ContainerLabels(place.app, env, "", variable, nil)
	if p != nil {
		labels = secret.ContainerLabels(place.app, env, p.Repository, variable, p.Labels)
	}
	switch {
	case flag != "":
		return flag, labels, nil
	case p != nil:
		return containerName(p, env, place.app, variable), labels, nil
	default:
		container, err = d.resolveContainer(ctx, "", project, place.app, variable)
		if err != nil {
			return "", nil, err
		}

		return container, labels, nil
	}
}

// containerName is the stack's derivation of a container's name:
// <prefix>-<env>-gbl-<app>-<kebab name>.
func containerName(p *derive.Placement, env, application, variable string) string {
	return p.Prefix + "-" + env + "-gbl-" + application + secret.ContainerSuffix(variable)
}

// readValue is the secret value: the file --from-file names (- for standard input),
// standard input when it is not a terminal, else asked for at the terminal without echo.
func (d deps) readValue(cmd *cobra.Command, fromFile, variable string) ([]byte, error) {
	switch {
	case fromFile == "-":
		return readAll(cmd.InOrStdin(), "standard input")
	case fromFile != "":
		value, err := os.ReadFile(fromFile)
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}

		return value, nil
	case !d.interactive():
		return readAll(cmd.InOrStdin(), "standard input")
	default:
		return d.readSecret(cmd.ErrOrStderr(), "Value of "+variable+" (not echoed): ")
	}
}

// readAll reads the reader to its end.
func readAll(r io.Reader, what string) ([]byte, error) {
	value, err := io.ReadAll(r)
	if err != nil {
		return nil, errors.Wrapf(err, "io.ReadAll(): %s", what)
	}

	return value, nil
}
