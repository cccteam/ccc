// owners.go lists the people holding roles/owner on the environment projects: the grant
// a project's creator receives, which the first apply of 1-org by hand leaves the
// bootstrap administrator with on every project it creates. The grant is temporary by
// design (the layers' identities hold what the layers need, and a person's access to an
// environment comes from its team group), and org check reports it until it is removed.

package org

import (
	"context"
	"strings"

	"cloud.google.com/go/iam/apiv1/iampb"
	resourcemanager "cloud.google.com/go/resourcemanager/apiv3"
	"github.com/go-playground/errors/v5"
)

const (
	// OwnerRole is the role a project's creator receives on it.
	OwnerRole = "roles/owner"
	// userPrefix starts a member that is a person.
	userPrefix = "user:"
)

// Binding is one role of a project's IAM policy with its members.
type Binding struct {
	Role    string
	Members []string
}

// PolicyReader reads a project's IAM policy. NewPolicyReader is the real one, over Cloud
// Resource Manager; tests pass a fake.
type PolicyReader interface {
	// ProjectPolicy is the project's bindings, by role.
	ProjectPolicy(ctx context.Context, project string) ([]Binding, error)
	// Close releases the connection.
	Close() error
}

// PolicyReaderFunc opens a PolicyReader.
type PolicyReaderFunc func(ctx context.Context) (PolicyReader, error)

// Owner is one person holding roles/owner on an environment project.
type Owner struct {
	Environment string
	Project     string
	Member      string
}

// Owners lists, for each environment project the placement records, in promotion order,
// every person (a user: member) holding roles/owner on it. Unrecorded names the
// environments the placement records no project for, which are not read.
func Owners(ctx context.Context, p *Placement, read PolicyReader) (owners []Owner, unrecorded []string, err error) {
	for _, env := range Environments {
		project := p.Projects[env]
		if project == "" {
			unrecorded = append(unrecorded, env)

			continue
		}
		bindings, err := read.ProjectPolicy(ctx, project)
		if err != nil {
			return nil, nil, err
		}
		for _, b := range bindings {
			if b.Role != OwnerRole {
				continue
			}
			for _, member := range b.Members {
				if strings.HasPrefix(member, userPrefix) {
					owners = append(owners, Owner{Environment: env, Project: project, Member: member})
				}
			}
		}
	}

	return owners, unrecorded, nil
}

// policies is the PolicyReader over the Cloud Resource Manager API.
type policies struct {
	client *resourcemanager.ProjectsClient
}

// NewPolicyReader opens the Cloud Resource Manager client with Application Default
// Credentials; without any, the open fails and says so.
func NewPolicyReader(ctx context.Context) (PolicyReader, error) {
	client, err := resourcemanager.NewProjectsClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "resourcemanager.NewProjectsClient()")
	}

	return &policies{client: client}, nil
}

// ProjectPolicy reads the project's IAM policy.
func (c *policies) ProjectPolicy(ctx context.Context, project string) ([]Binding, error) {
	policy, err := c.client.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Resource: "projects/" + project})
	if err != nil {
		return nil, errors.Wrapf(err, "resourcemanager.ProjectsClient.GetIamPolicy(): %s", project)
	}
	bindings := make([]Binding, 0, len(policy.GetBindings()))
	for _, b := range policy.GetBindings() {
		bindings = append(bindings, Binding{Role: b.GetRole(), Members: b.GetMembers()})
	}

	return bindings, nil
}

// Close releases the API connection.
func (c *policies) Close() error {
	if err := c.client.Close(); err != nil {
		return errors.Wrap(err, "resourcemanager.ProjectsClient.Close()")
	}

	return nil
}
