// add.go is secret add: a value stored as a new version of the container the stack
// names for a variable, the container created first when the project has none.

package secret

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/go-playground/errors/v5"
)

// AddRequest is what secret add was asked to do.
type AddRequest struct {
	// App is the application whose secret this is.
	App string
	// Env is the environment the container lives in.
	Env string
	// Variable is the environment variable the secret feeds.
	Variable string
	// Project is the environment project the container lives in.
	Project string
	// Container is the container's name in the project: the stack's derivation,
	// <prefix>-<env>-gbl-<app>-<kebab name>.
	Container string
	// Labels are the labels a container created here carries (ContainerLabels): the
	// ones the stack puts on it, so the apply that adopts the container changes nothing
	// and secret pin finds it by them.
	Labels map[string]string
	// Value is the secret value, stored as given.
	Value []byte
}

// validate checks the arguments before anything is asked of Secret Manager.
func (req *AddRequest) validate() error {
	if err := ValidateApp(req.App); err != nil {
		return err
	}
	if err := ValidateEnv(req.Env); err != nil {
		return err
	}
	if err := ValidateVariable(req.Variable); err != nil {
		return err
	}
	if req.Project == "" {
		return errors.New("the environment project is needed: pass --project <id>")
	}
	if req.Container == "" {
		return errors.New("the container's name is needed: pass --container <name>")
	}
	if len(req.Value) == 0 {
		return errors.Newf("the value of %s is empty: nothing to add", req.Variable)
	}

	return nil
}

// The labels the stack puts on every resource (its locals.tf's base_labels) and on a
// container (its secret-manager.tf).
const (
	managedLabel     = "terraform"
	sourcePathLabel  = "terraform_source_path"
	repositoryLabel  = "source_repo"
	environmentLabel = "environment"
	applicationLabel = "application"
	variableLabel    = "variable"
	// managed is the managed label's value.
	managed = "true"
)

// ContainerLabels are the labels a container created ahead of the apply carries: the
// ones the stack puts on every resource (its locals.tf's base_labels: managed, the layer,
// the repository, the environment, the application), the organization's extra labels,
// and the variable, as the stack's secret-manager.tf sets them.
func ContainerLabels(app, env, repository, variable string, extra map[string]string) map[string]string {
	labels := map[string]string{
		managedLabel:     managed,
		sourcePathLabel:  layerRoot + "-" + app,
		environmentLabel: env,
		applicationLabel: app,
		variableLabel:    strings.ToLower(variable),
	}
	if repository != "" {
		labels[repositoryLabel] = repository
	}
	for k, v := range extra {
		labels[k] = v
	}

	return labels
}

// AddResult is what secret add found and did.
type AddResult struct {
	// App, Env and Variable are the secret, as asked.
	App      string
	Env      string
	Variable string
	// Project and Container are where the version went.
	Project   string
	Container string
	// Created is true when the container did not exist and was created here.
	Created bool
	// Version is the number of the version added.
	Version string
}

// Add stores the value as a new version of the container in the environment project,
// creating the container first when Secret Manager has none by that name. Nothing is
// pinned or rolled out here: the result names the pin to make.
func Add(ctx context.Context, open ClientFunc, req *AddRequest) (*AddResult, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	client, err := open(ctx, req.Project)
	if err != nil {
		return nil, err
	}
	if c, ok := client.(io.Closer); ok {
		defer c.Close()
	}
	name := "projects/" + req.Project + "/secrets/" + req.Container
	r := &AddResult{App: req.App, Env: req.Env, Variable: req.Variable, Project: req.Project, Container: req.Container}
	if _, exists, err := client.GetSecret(ctx, name); err != nil {
		return nil, err
	} else if !exists {
		if err := client.CreateSecret(ctx, req.Project, req.Container, req.Labels); err != nil {
			return nil, err
		}
		r.Created = true
	}
	version, err := client.AddSecretVersion(ctx, name, req.Value)
	if err != nil {
		return nil, err
	}
	r.Version = version

	return r, nil
}

// Write prints what was done and what to do next, the way a person reads it.
func (r *AddResult) Write(w io.Writer) {
	if r.Created {
		fmt.Fprintf(w, "Created the container %s in project %s; the next apply of the %s stack in %s adopts it.\n", r.Container, r.Project, r.App, r.Env)
	}
	fmt.Fprintf(w, "Added version %s of %s in project %s, for %s of %s in %s.\n", r.Version, r.Container, r.Project, r.Variable, r.App, r.Env)
	fmt.Fprintf(w, "Pin it: bedrock secret pin %s %s %s\n", r.Env, r.Variable, r.Version)
}
