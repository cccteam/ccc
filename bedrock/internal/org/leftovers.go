// leftovers.go lists the grants a hand apply leaves a person with and which are
// removed by hand once the apply trigger applies the layers: Owner on each project it
// created, which Google gives a project's creator; Folder Admin and Folder Editor on each
// folder it created, which Google gives a folder's creator; and the roles the seed grants
// the break-glass account on the organization's folder for the second layer's first
// apply. The one grant that stays is the break-glass account's Folder Admin on the
// organization's folder, its key to everything below (0-bootstrap/README.md, "Trimming
// the break-glass account"), and org check reports every other one until it is removed.
// On the organization's folder it reports as well every grant to a principal outside the
// organization, neither the break-glass account nor an identity in one of its projects:
// the folder is the whole of the organization's reach, nothing of the organization's
// grants there to an outsider, and a creating identity's grants come off at the
// creation's end (a hosting organization created by its root organization).

package org

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"cloud.google.com/go/iam/apiv1/iampb"
	resourcemanager "cloud.google.com/go/resourcemanager/apiv3"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/iterator"
)

const (
	// OwnerRole is the role a project's creator receives on it.
	OwnerRole = "roles/owner"
	// FolderAdminRole is one of the two roles a folder's creator receives on it; the
	// break-glass account keeps it on the organization's folder.
	FolderAdminRole = "roles/resourcemanager.folderAdmin"
	// FolderEditorRole is the other role a folder's creator receives on it.
	FolderEditorRole = "roles/resourcemanager.folderEditor"
	// BigQueryAdminRole is the role the break-glass account keeps on a root
	// organization's root project for the billing export's console step
	// (BreakGlassProjectRoles).
	BigQueryAdminRole = "roles/bigquery.admin"
	// ServiceUsageConsumerRole is the role the break-glass account keeps on a root
	// organization's root project for the quota its checks bill to it
	// (BreakGlassProjectRoles).
	ServiceUsageConsumerRole = "roles/serviceusage.serviceUsageConsumer"
	// userPrefix starts a member that is a person.
	userPrefix = "user:"
	// serviceAccountPrefix starts a member that is a service account.
	serviceAccountPrefix = "serviceAccount:"
	// identityDomainSuffix ends a service account's address after its project's id.
	identityDomainSuffix = ".iam.gserviceaccount.com"
	// googleAgentDomainPrefix starts the domain of one of Google's own service agents.
	googleAgentDomainPrefix = "gcp-sa-"
)

// The two kinds of place a leftover grant sits on.
const (
	OnProject = "project"
	OnFolder  = "folder"
)

// Binding is one role of an IAM policy with its members.
type Binding struct {
	Role    string
	Members []string
}

// Folder is one folder under the organization: its numeric id and its display name.
type Folder struct {
	ID   string
	Name string
}

// PolicyReader reads IAM policies and finds the organization's folders. NewPolicyReader
// is the real one, over Cloud Resource Manager; tests pass a fake.
type PolicyReader interface {
	// ProjectPolicy is the project's bindings, by role.
	ProjectPolicy(ctx context.Context, project string) ([]Binding, error)
	// OrganizationFolder is the id of the folder named name directly under the
	// organization (its numeric id), or empty when there is none.
	OrganizationFolder(ctx context.Context, organizationID, name string) (string, error)
	// ChildFolders are the folders directly under the folder (its numeric id).
	ChildFolders(ctx context.Context, folder string) ([]Folder, error)
	// FolderPolicy is the folder's bindings, by role.
	FolderPolicy(ctx context.Context, folder string) ([]Binding, error)
	// Close releases the connection.
	Close() error
}

// PolicyReaderFunc opens a PolicyReader.
type PolicyReaderFunc func(ctx context.Context) (PolicyReader, error)

// Leftover is one grant a person still holds from a hand apply.
type Leftover struct {
	// On is where the grant sits: a project or a folder.
	On string
	// Key names the place: a project's key in the placement (boot, tst), a folder's
	// display name.
	Key string
	// ID is the project's id or the folder's numeric id.
	ID string
	// Member is the person, as an IAM member (user:...).
	Member string
	// Role is the role held.
	Role string
	// Outside says the member is a principal outside the organization, on its folder:
	// not the break-glass account, not an identity in one of the organization's
	// projects. The other leftovers are a person's, from a hand apply.
	Outside bool
}

// Place names where the grant sits, for a line about it.
func (l Leftover) Place() string {
	return fmt.Sprintf("%s %s (%s)", l.On, l.Key, l.ID)
}

// Leftovers lists the grants the setup left behind: on every project the placement
// records (projects), in its order, each person holding Owner and each other role the
// break-glass account holds; on the organization's folder, each role the break-glass
// account holds but Folder Admin, the one it keeps, and each grant to a principal
// outside the organization (Outside); on each folder under it, each person holding
// Folder Admin or Folder Editor and each other role the break-glass account holds.
// Unrecorded names the environments the placement records no project for, which are
// not read (a root organization has none); a missing organization folder is reported as
// none found, not as an error, since the check runs before the seed too.
func Leftovers(ctx context.Context, p *Placement, read PolicyReader) (leftovers []Leftover, unrecorded []string, folderFound bool, err error) {
	account := userPrefix + p.BreakGlassAccount
	if !p.IsRoot() {
		for _, env := range Environments {
			if p.Projects[env] == "" {
				unrecorded = append(unrecorded, env)
			}
		}
	}
	leftovers, err = projectLeftovers(ctx, p, read, account)
	if err != nil {
		return nil, nil, false, err
	}
	folder, err := read.OrganizationFolder(ctx, p.OrganizationID, p.Prefix)
	if err != nil {
		return nil, nil, false, err
	}
	if folder == "" {
		return leftovers, unrecorded, false, nil
	}
	onFolders, err := folderLeftovers(ctx, p, read, account, folder)
	if err != nil {
		return nil, nil, false, err
	}

	return append(leftovers, onFolders...), unrecorded, true, nil
}

// projectLeftovers is the grants left on the projects the placement records, in its
// order: each person's Owner and each other role the break-glass account holds, but
// the ones it keeps on a root project (BreakGlassProjectRoles).
func projectLeftovers(ctx context.Context, p *Placement, read PolicyReader, account string) ([]Leftover, error) {
	var leftovers []Leftover
	for _, key := range p.ProjectKeys() {
		project := p.Projects[key]
		if project == "" {
			continue
		}
		bindings, err := read.ProjectPolicy(ctx, project)
		if err != nil {
			return nil, err
		}
		for _, b := range bindings {
			for _, member := range b.Members {
				if (b.Role == OwnerRole && strings.HasPrefix(member, userPrefix)) || (b.Role != OwnerRole && member == account && !p.keepsProjectRole(key, b.Role)) {
					leftovers = append(leftovers, Leftover{On: OnProject, Key: key, ID: project, Member: member, Role: b.Role})
				}
			}
		}
	}

	return leftovers, nil
}

// folderLeftovers is the grants left on the organization's folder (folder, its numeric
// id), each role the break-glass account holds there but Folder Admin, the one it keeps,
// and each grant to a principal outside the organization; and on each folder under it,
// each person's Folder Admin or Folder Editor and each other role the break-glass
// account holds.
func folderLeftovers(ctx context.Context, p *Placement, read PolicyReader, account, folder string) ([]Leftover, error) {
	bindings, err := read.FolderPolicy(ctx, folder)
	if err != nil {
		return nil, err
	}
	var leftovers []Leftover
	for _, b := range bindings {
		for _, member := range b.Members {
			switch {
			case member == account:
				if b.Role != FolderAdminRole {
					leftovers = append(leftovers, Leftover{On: OnFolder, Key: p.Prefix, ID: folder, Member: member, Role: b.Role})
				}
			case !p.OwnIdentity(member):
				leftovers = append(leftovers, Leftover{On: OnFolder, Key: p.Prefix, ID: folder, Member: member, Role: b.Role, Outside: true})
			}
		}
	}
	children, err := read.ChildFolders(ctx, folder)
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		bindings, err := read.FolderPolicy(ctx, child.ID)
		if err != nil {
			return nil, err
		}
		for _, b := range bindings {
			creator := b.Role == FolderAdminRole || b.Role == FolderEditorRole
			for _, member := range b.Members {
				if (creator && strings.HasPrefix(member, userPrefix)) || (!creator && member == account) {
					leftovers = append(leftovers, Leftover{On: OnFolder, Key: child.Name, ID: child.ID, Member: member, Role: b.Role})
				}
			}
		}
	}

	return leftovers, nil
}

// OwnIdentity reports whether the member is an identity of the organization's own: a
// service account in one of its projects (serviceAccount:<name>@<project>.iam.gserviceaccount.com,
// the project one the placement records), or one of Google's own service agents
// (@gcp-sa-<service>.iam.gserviceaccount.com), which Google grants where a service needs
// them. Everything else on the organization's folder is outside the organization.
func (p *Placement) OwnIdentity(member string) bool {
	email, ok := strings.CutPrefix(member, serviceAccountPrefix)
	if !ok {
		return false
	}
	_, domain, ok := strings.Cut(email, "@")
	if !ok {
		return false
	}
	if strings.HasPrefix(domain, googleAgentDomainPrefix) {
		return true
	}
	project, ok := strings.CutSuffix(domain, identityDomainSuffix)
	if !ok {
		return false
	}
	for _, own := range p.Projects {
		if own == project {
			return true
		}
	}

	return false
}

// policies is the PolicyReader over the Cloud Resource Manager API.
type policies struct {
	projects *resourcemanager.ProjectsClient
	folders  *resourcemanager.FoldersClient
}

// NewPolicyReader opens the Cloud Resource Manager clients with Application Default
// Credentials; without any, the open fails and says so.
func NewPolicyReader(ctx context.Context) (PolicyReader, error) {
	projects, err := resourcemanager.NewProjectsClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "resourcemanager.NewProjectsClient()")
	}
	folders, err := resourcemanager.NewFoldersClient(ctx)
	if err != nil {
		_ = projects.Close()

		return nil, errors.Wrap(err, "resourcemanager.NewFoldersClient()")
	}

	return &policies{projects: projects, folders: folders}, nil
}

// ProjectPolicy reads the project's IAM policy.
func (c *policies) ProjectPolicy(ctx context.Context, project string) ([]Binding, error) {
	policy, err := c.projects.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Resource: "projects/" + project})
	if err != nil {
		return nil, errors.Wrapf(err, "resourcemanager.ProjectsClient.GetIamPolicy(): %s", project)
	}

	return policyBindings(policy), nil
}

// OrganizationFolder finds the active folder named name directly under the organization;
// one deleted and waiting out its thirty days keeps its name and is not it.
func (c *policies) OrganizationFolder(ctx context.Context, organizationID, name string) (string, error) {
	query := fmt.Sprintf("parent=organizations/%s AND displayName=%s AND state=ACTIVE", organizationID, name)
	it := c.folders.SearchFolders(ctx, &resourcemanagerpb.SearchFoldersRequest{Query: query})
	for {
		folder, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return "", nil
		}
		if err != nil {
			return "", errors.Wrapf(err, "resourcemanager.FoldersClient.SearchFolders(): %s", query)
		}
		if folder.GetDisplayName() == name && folder.GetState() == resourcemanagerpb.Folder_ACTIVE {
			return strings.TrimPrefix(folder.GetName(), "folders/"), nil
		}
	}
}

// ChildFolders lists the folders directly under the folder.
func (c *policies) ChildFolders(ctx context.Context, folder string) ([]Folder, error) {
	it := c.folders.ListFolders(ctx, &resourcemanagerpb.ListFoldersRequest{Parent: "folders/" + folder})
	var children []Folder
	for {
		child, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return children, nil
		}
		if err != nil {
			return nil, errors.Wrapf(err, "resourcemanager.FoldersClient.ListFolders(): %s", folder)
		}
		children = append(children, Folder{ID: strings.TrimPrefix(child.GetName(), "folders/"), Name: child.GetDisplayName()})
	}
}

// FolderPolicy reads the folder's IAM policy.
func (c *policies) FolderPolicy(ctx context.Context, folder string) ([]Binding, error) {
	policy, err := c.folders.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Resource: "folders/" + folder})
	if err != nil {
		return nil, errors.Wrapf(err, "resourcemanager.FoldersClient.GetIamPolicy(): %s", folder)
	}

	return policyBindings(policy), nil
}

// policyBindings is the policy's bindings as Bindings.
func policyBindings(policy *iampb.Policy) []Binding {
	bindings := make([]Binding, 0, len(policy.GetBindings()))
	for _, b := range policy.GetBindings() {
		bindings = append(bindings, Binding{Role: b.GetRole(), Members: b.GetMembers()})
	}

	return bindings
}

// Close releases the API connections.
func (c *policies) Close() error {
	if err := c.projects.Close(); err != nil {
		_ = c.folders.Close()

		return errors.Wrap(err, "resourcemanager.ProjectsClient.Close()")
	}
	if err := c.folders.Close(); err != nil {
		return errors.Wrap(err, "resourcemanager.FoldersClient.Close()")
	}

	return nil
}

// BreakGlassProjectRoles are the roles the break-glass account keeps on a root
// organization's root project beside its Folder Admin on the folder, which the apply
// keeps and the leftover report passes over: BigQuery Admin, with which it points the
// billing account's export at the dataset the bootstrap made, and Service Usage
// Consumer, so a check or a recovery run at its terminal bills its calls' quota to the
// project. A hosting organization keeps it nothing on a project.
func (p *Placement) BreakGlassProjectRoles() []string {
	if !p.IsRoot() {
		return nil
	}

	return []string{BigQueryAdminRole, ServiceUsageConsumerRole}
}

// keepsProjectRole reports whether the role is one the break-glass account keeps on the
// project with the key: the root project's kept roles alone.
func (p *Placement) keepsProjectRole(key, role string) bool {
	return key == RootProjectKey && slices.Contains(p.BreakGlassProjectRoles(), role)
}
