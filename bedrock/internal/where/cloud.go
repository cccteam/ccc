// cloud.go finds, in Google Cloud, what a pin names: the environment project by its
// labels, the secret container by the layer's label and the variable, and the versions
// the container holds.

package where

import (
	"context"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	resourcemanager "cloud.google.com/go/resourcemanager/apiv3"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/iterator"

	"github.com/cccteam/ccc/bedrock/internal/secret"
)

const (
	// Active is the state Cloud Resource Manager reports for a project that is in use.
	Active = "ACTIVE"
	// labelPrefix and labelJoin build a label query: labels.<key>=<value>, joined by
	// AND, the form both search APIs read.
	labelPrefix = "labels."
	labelJoin   = " AND "
)

// Project is what Cloud Resource Manager says about one project.
type Project struct {
	// ID is the project ID, what a --project flag takes.
	ID string
	// DisplayName is the project's name as the console shows it.
	DisplayName string
	// State is ACTIVE for a project in use, else DELETE_REQUESTED.
	State string
	// Labels are the project's labels.
	Labels map[string]string
}

// ProjectClient searches the projects the caller can see. The functions here open one
// per call through a ProjectClientFunc; tests pass a fake.
type ProjectClient interface {
	// SearchProjects lists the projects matching the query, in the API's order.
	SearchProjects(ctx context.Context, query string) ([]Project, error)
}

// ProjectClientFunc opens a ProjectClient. NewProjects is the real one.
type ProjectClientFunc func(ctx context.Context) (ProjectClient, error)

// projects is the ProjectClient over the Cloud Resource Manager API.
type projects struct {
	client *resourcemanager.ProjectsClient
}

// NewProjects opens the Cloud Resource Manager client with Application Default
// Credentials; the search is billed to the credentials' own quota project.
func NewProjects(ctx context.Context) (ProjectClient, error) {
	client, err := resourcemanager.NewProjectsClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "resourcemanager.NewProjectsClient()")
	}

	return &projects{client: client}, nil
}

// SearchProjects asks the API for the projects matching the query.
func (c *projects) SearchProjects(ctx context.Context, query string) ([]Project, error) {
	var found []Project
	it := c.client.SearchProjects(ctx, &resourcemanagerpb.SearchProjectsRequest{Query: query})
	for {
		p, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return found, nil
		}
		if err != nil {
			return nil, errors.Wrapf(err, "resourcemanager.ProjectsClient.SearchProjects(): %q", query)
		}
		found = append(found, Project{ID: p.GetProjectId(), DisplayName: p.GetDisplayName(), State: p.GetState().String(), Labels: p.GetLabels()})
	}
}

// Close releases the API connection.
func (c *projects) Close() error {
	if err := c.client.Close(); err != nil {
		return errors.Wrap(err, "resourcemanager.ProjectsClient.Close()")
	}

	return nil
}

// ProjectByLabels is the ID of the one active project carrying every label. The search
// is by the labels, and the answers are checked against them again, so a looser match by
// the API cannot pass for the one project. None and several are refused, the several
// listed.
func ProjectByLabels(ctx context.Context, open ProjectClientFunc, labels map[string]string) (string, error) {
	client, err := open(ctx)
	if err != nil {
		return "", err
	}
	if c, ok := client.(io.Closer); ok {
		defer c.Close()
	}
	found, err := client.SearchProjects(ctx, labelQuery(labels))
	if err != nil {
		return "", err
	}
	var matching []Project
	for _, p := range found {
		if p.State == Active && carries(p.Labels, labels) {
			matching = append(matching, p)
		}
	}
	switch len(matching) {
	case 0:
		return "", errors.Newf("no active project carries the labels %s: pass --project", labelList(labels))
	case 1:
		return matching[0].ID, nil
	default:
		return "", errors.Newf("several active projects carry the labels %s: %s; pass --project", labelList(labels), projectList(matching))
	}
}

// projectList names the projects in a message: <id> (<display name>), comma separated.
func projectList(found []Project) string {
	names := make([]string, 0, len(found))
	for _, p := range found {
		names = append(names, p.ID+" ("+p.DisplayName+")")
	}

	return strings.Join(names, ", ")
}

// carries reports whether the labels hold every wanted label at its value.
func carries(labels, wanted map[string]string) bool {
	for key, value := range wanted {
		if labels[key] != value {
			return false
		}
	}

	return true
}

// labelQuery is the search query for the labels: labels.<key>=<value> for each, joined
// by AND, the keys sorted so the query is the same every time.
func labelQuery(labels map[string]string) string {
	terms := make([]string, 0, len(labels))
	for _, key := range sortedKeys(labels) {
		terms = append(terms, labelPrefix+key+"="+labels[key])
	}

	return strings.Join(terms, labelJoin)
}

// labelList names the labels in a message: <key>=<value>, comma separated, keys sorted.
func labelList(labels map[string]string) string {
	terms := make([]string, 0, len(labels))
	for _, key := range sortedKeys(labels) {
		terms = append(terms, key+"="+labels[key])
	}

	return strings.Join(terms, ", ")
}

// sortedKeys lists the keys of the labels, sorted.
func sortedKeys(labels map[string]string) []string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys
}

// ContainerByLabels is the name of the one secret container in the project that carries
// every label and ends with the suffix (the variable's, from secret.ContainerSuffix).
// None and several are refused, the several listed, and none lists what the labels did
// find.
func ContainerByLabels(ctx context.Context, open secret.ClientFunc, project string, labels map[string]string, suffix string) (string, error) {
	client, err := open(ctx, project)
	if err != nil {
		return "", err
	}
	if c, ok := client.(io.Closer); ok {
		defer c.Close()
	}
	names, err := client.ListSecrets(ctx, project, labelQuery(labels))
	if err != nil {
		return "", err
	}
	var matching []string
	for _, name := range names {
		if strings.HasSuffix(name, suffix) {
			matching = append(matching, name)
		}
	}
	switch len(matching) {
	case 0:
		if len(names) == 0 {
			return "", errors.Newf("no secret container in project %s carries the labels %s: pass --container", project, labelList(labels))
		}

		return "", errors.Newf("no secret container in project %s carrying the labels %s ends with %s (found %s): pass --container", project, labelList(labels), suffix, strings.Join(names, ", "))
	case 1:
		return matching[0], nil
	default:
		return "", errors.Newf("several secret containers in project %s carrying the labels %s end with %s: %s; pass --container", project, labelList(labels), suffix, strings.Join(matching, ", "))
	}
}

// VersionInfo is one version of a secret container, for choosing among them.
type VersionInfo struct {
	// Name is the version's number, the last segment of its resource name: what a pin
	// holds.
	Name string
	// State is ENABLED when the version can be mounted, else DISABLED or DESTROYED.
	State string
}

// Versions lists the versions of the container in the project, newest (the highest
// number) first.
func Versions(ctx context.Context, open secret.ClientFunc, project, container string) ([]VersionInfo, error) {
	client, err := open(ctx, project)
	if err != nil {
		return nil, err
	}
	if c, ok := client.(io.Closer); ok {
		defer c.Close()
	}
	versions, err := client.ListSecretVersions(ctx, project, container)
	if err != nil {
		return nil, err
	}
	infos := make([]VersionInfo, 0, len(versions))
	for _, v := range versions {
		infos = append(infos, VersionInfo{Name: path.Base(v.Name), State: v.State})
	}
	sort.SliceStable(infos, func(i, j int) bool {
		return number(infos[i].Name) > number(infos[j].Name)
	})

	return infos, nil
}

// number is the version's number, or 0 for one that is not a number.
func number(name string) int {
	n, err := strconv.Atoi(name)
	if err != nil {
		return 0
	}

	return n
}
