// google.go is the founding's seam to Google Cloud: what the founding identity makes
// and grants under the new organization's folder (Cloud Resource Manager, Cloud
// Billing, Service Usage, IAM, Cloud Storage) and the shared secrets' policies in the
// root organization's boot project (Secret Manager), each as the founding identity
// (impersonated by the apply trigger's runner), or a fake in tests.

package found

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/iam"
	admin "cloud.google.com/go/iam/admin/apiv1"
	"cloud.google.com/go/iam/admin/apiv1/adminpb"
	"cloud.google.com/go/iam/apiv1/iampb"
	resourcemanager "cloud.google.com/go/resourcemanager/apiv3"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/storage"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/cloudbilling/v1"
	"google.golang.org/api/impersonate"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/api/serviceusage/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Google is what the founding asks Google Cloud, as the founding identity.
type Google interface {
	// FindFolder is the numeric id of the folder named displayName directly under the
	// parent (organizations/N or folders/N), empty when there is none.
	FindFolder(ctx context.Context, parent, displayName string) (string, error)
	// CreateFolder makes the folder under the parent and answers its numeric id.
	CreateFolder(ctx context.Context, parent, displayName string) (string, error)
	// GrantFolder adds the member to the role on the folder's policy; a member already
	// in the role is left as it is.
	GrantFolder(ctx context.Context, folder, role, member string) error
	// RevokeFolder takes the member out of each of the roles on the folder's policy;
	// a role the member is not in is left as it is.
	RevokeFolder(ctx context.Context, folder, member string, roles []string) error
	// ProjectExists reports whether the project exists (and is readable).
	ProjectExists(ctx context.Context, id string) (bool, error)
	// CreateProject makes the project under the folder (folders/N), with the display
	// name and the labels.
	CreateProject(ctx context.Context, id, displayName, folder string, labels map[string]string) error
	// ProjectNumber is the project's number.
	ProjectNumber(ctx context.Context, id string) (string, error)
	// RevokeProject takes the member out of each of the roles on the project's policy.
	RevokeProject(ctx context.Context, id, member string, roles []string) error
	// LinkBilling links the project to the billing account.
	LinkBilling(ctx context.Context, id, billingAccount string) error
	// EnableServices enables the APIs in the project and waits for them.
	EnableServices(ctx context.Context, id string, services []string) error
	// ServiceAccountExists reports whether the service account (by email) exists.
	ServiceAccountExists(ctx context.Context, email string) (bool, error)
	// CreateServiceAccount makes the service account in the project.
	CreateServiceAccount(ctx context.Context, project, accountID, displayName, description string) error
	// BucketExists reports whether the bucket exists.
	BucketExists(ctx context.Context, name string) (bool, error)
	// CreateBucket makes the bucket in the project and region: uniform access, public
	// access prevented, versioning on, with the labels.
	CreateBucket(ctx context.Context, project, name, location string, labels map[string]string) error
	// GrantSecret adds the member to the role on the secret's policy
	// (projects/P[/locations/L]/secrets/N); a member already in the role is left.
	GrantSecret(ctx context.Context, secret, role, member string) error
	// Close releases the connections.
	Close() error
}

// GoogleAsFunc opens Google as an identity the process's credentials may impersonate.
type GoogleAsFunc func(ctx context.Context, identity string) (Google, error)

// cloudPlatformScope is the OAuth scope every Google Cloud API accepts.
const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// google is the real seam over the Google APIs.
type google struct {
	folders  *resourcemanager.FoldersClient
	projects *resourcemanager.ProjectsClient
	iam      *admin.IamClient
	billing  *cloudbilling.APIService
	services *serviceusage.Service
	storage  *storage.Client
	secrets  *secretmanager.Client
	opts     []option.ClientOption
	regional map[string]*secretmanager.Client
}

// NewGoogleAs opens the Google clients as the identity.
func NewGoogleAs(ctx context.Context, identity string) (Google, error) {
	source, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{TargetPrincipal: identity, Scopes: []string{cloudPlatformScope}})
	if err != nil {
		return nil, errors.Wrapf(err, "impersonate.CredentialsTokenSource(): %s", identity)
	}

	return newGoogle(ctx, option.WithTokenSource(source))
}

func newGoogle(ctx context.Context, opts ...option.ClientOption) (Google, error) {
	g := &google{opts: opts, regional: map[string]*secretmanager.Client{}}
	var err error
	if g.folders, err = resourcemanager.NewFoldersClient(ctx, opts...); err != nil {
		return nil, errors.Wrap(err, "resourcemanager.NewFoldersClient()")
	}
	if g.projects, err = resourcemanager.NewProjectsClient(ctx, opts...); err != nil {
		return nil, errors.Wrap(err, "resourcemanager.NewProjectsClient()")
	}
	if g.iam, err = admin.NewIamClient(ctx, opts...); err != nil {
		return nil, errors.Wrap(err, "admin.NewIamClient()")
	}
	if g.billing, err = cloudbilling.NewService(ctx, opts...); err != nil {
		return nil, errors.Wrap(err, "cloudbilling.NewService()")
	}
	if g.services, err = serviceusage.NewService(ctx, opts...); err != nil {
		return nil, errors.Wrap(err, "serviceusage.NewService()")
	}
	if g.storage, err = storage.NewClient(ctx, opts...); err != nil {
		return nil, errors.Wrap(err, "storage.NewClient()")
	}
	if g.secrets, err = secretmanager.NewClient(ctx, opts...); err != nil {
		return nil, errors.Wrap(err, "secretmanager.NewClient()")
	}

	return g, nil
}

func (g *google) FindFolder(ctx context.Context, parent, displayName string) (string, error) {
	query := fmt.Sprintf("parent=%s AND displayName=%s", parent, displayName)
	it := g.folders.SearchFolders(ctx, &resourcemanagerpb.SearchFoldersRequest{Query: query})
	for {
		folder, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return "", nil
		}
		if err != nil {
			return "", errors.Wrapf(err, "resourcemanager.FoldersClient.SearchFolders(): %s", query)
		}
		if folder.GetDisplayName() == displayName {
			return strings.TrimPrefix(folder.GetName(), "folders/"), nil
		}
	}
}

func (g *google) CreateFolder(ctx context.Context, parent, displayName string) (string, error) {
	op, err := g.folders.CreateFolder(ctx, &resourcemanagerpb.CreateFolderRequest{Folder: &resourcemanagerpb.Folder{Parent: parent, DisplayName: displayName}})
	if err != nil {
		return "", errors.Wrapf(err, "resourcemanager.FoldersClient.CreateFolder(): %s under %s", displayName, parent)
	}
	folder, err := op.Wait(ctx)
	if err != nil {
		return "", errors.Wrapf(err, "the creation of folder %s under %s", displayName, parent)
	}

	return strings.TrimPrefix(folder.GetName(), "folders/"), nil
}

func (g *google) GrantFolder(ctx context.Context, folder, role, member string) error {
	return grant(ctx, g.folderPolicy("folders/"+folder), role, member)
}

func (g *google) RevokeFolder(ctx context.Context, folder, member string, roles []string) error {
	return revoke(ctx, g.folderPolicy("folders/"+folder), member, roles)
}

func (g *google) RevokeProject(ctx context.Context, id, member string, roles []string) error {
	return revoke(ctx, g.projectPolicy("projects/"+id), member, roles)
}

// policy is one resource's IAM policy: read whole, set whole.
type policy struct {
	resource string
	get      func(ctx context.Context) (*iampb.Policy, error)
	set      func(ctx context.Context, p *iampb.Policy) error
}

func (g *google) folderPolicy(resource string) policy {
	return policy{
		resource: resource,
		get: func(ctx context.Context) (*iampb.Policy, error) {
			p, err := g.folders.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Resource: resource})

			return p, errors.Wrapf(err, "resourcemanager.FoldersClient.GetIamPolicy(): %s", resource)
		},
		set: func(ctx context.Context, p *iampb.Policy) error {
			_, err := g.folders.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{Resource: resource, Policy: p})

			return errors.Wrapf(err, "resourcemanager.FoldersClient.SetIamPolicy(): %s", resource)
		},
	}
}

func (g *google) projectPolicy(resource string) policy {
	return policy{
		resource: resource,
		get: func(ctx context.Context) (*iampb.Policy, error) {
			p, err := g.projects.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Resource: resource})

			return p, errors.Wrapf(err, "resourcemanager.ProjectsClient.GetIamPolicy(): %s", resource)
		},
		set: func(ctx context.Context, p *iampb.Policy) error {
			_, err := g.projects.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{Resource: resource, Policy: p})

			return errors.Wrapf(err, "resourcemanager.ProjectsClient.SetIamPolicy(): %s", resource)
		},
	}
}

// grant adds the member to the role on the policy, unless it is there.
func grant(ctx context.Context, p policy, role, member string) error {
	current, err := p.get(ctx)
	if err != nil {
		return err
	}
	for _, b := range current.GetBindings() {
		if b.GetRole() == role && b.GetCondition() == nil {
			if contains(b.GetMembers(), member) {
				return nil
			}
			b.Members = append(b.Members, member)

			return p.set(ctx, current)
		}
	}
	current.Bindings = append(current.Bindings, &iampb.Binding{Role: role, Members: []string{member}})

	return p.set(ctx, current)
}

// revoke takes the member out of each of the roles on the policy, unless it is in none.
func revoke(ctx context.Context, p policy, member string, roles []string) error {
	policy, err := p.get(ctx)
	if err != nil {
		return err
	}
	changed := false
	kept := make([]*iampb.Binding, 0, len(policy.GetBindings()))
	for _, b := range policy.GetBindings() {
		if !contains(roles, b.GetRole()) || b.GetCondition() != nil {
			kept = append(kept, b)

			continue
		}
		members := make([]string, 0, len(b.GetMembers()))
		for _, m := range b.GetMembers() {
			if m == member {
				changed = true

				continue
			}
			members = append(members, m)
		}
		if len(members) > 0 {
			b.Members = members
			kept = append(kept, b)
		}
	}
	if !changed {
		return nil
	}
	policy.Bindings = kept

	return p.set(ctx, policy)
}

func (g *google) ProjectExists(ctx context.Context, id string) (bool, error) {
	_, err := g.projects.GetProject(ctx, &resourcemanagerpb.GetProjectRequest{Name: "projects/" + id})
	if status.Code(err) == codes.NotFound || status.Code(err) == codes.PermissionDenied {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrapf(err, "resourcemanager.ProjectsClient.GetProject(): %s", id)
	}

	return true, nil
}

func (g *google) CreateProject(ctx context.Context, id, displayName, folder string, labels map[string]string) error {
	op, err := g.projects.CreateProject(ctx, &resourcemanagerpb.CreateProjectRequest{Project: &resourcemanagerpb.Project{ProjectId: id, DisplayName: displayName, Parent: "folders/" + folder, Labels: labels}})
	if err != nil {
		return errors.Wrapf(err, "resourcemanager.ProjectsClient.CreateProject(): %s", id)
	}
	if _, err := op.Wait(ctx); err != nil {
		return errors.Wrapf(err, "the creation of project %s", id)
	}

	return nil
}

func (g *google) ProjectNumber(ctx context.Context, id string) (string, error) {
	project, err := g.projects.GetProject(ctx, &resourcemanagerpb.GetProjectRequest{Name: "projects/" + id})
	if err != nil {
		return "", errors.Wrapf(err, "resourcemanager.ProjectsClient.GetProject(): %s", id)
	}

	return strings.TrimPrefix(project.GetName(), "projects/"), nil
}

func (g *google) LinkBilling(ctx context.Context, id, billingAccount string) error {
	_, err := g.billing.Projects.UpdateBillingInfo("projects/"+id, &cloudbilling.ProjectBillingInfo{BillingAccountName: "billingAccounts/" + billingAccount}).Context(ctx).Do()
	if err != nil {
		return errors.Wrapf(err, "cloudbilling.Projects.UpdateBillingInfo(): %s to %s", id, billingAccount)
	}

	return nil
}

// enablePoll is how often the founding asks after a batch enable's operation.
const enablePoll = 5 * time.Second

func (g *google) EnableServices(ctx context.Context, id string, services []string) error {
	op, err := g.services.Services.BatchEnable("projects/"+id, &serviceusage.BatchEnableServicesRequest{ServiceIds: services}).Context(ctx).Do()
	if err != nil {
		return errors.Wrapf(err, "serviceusage.Services.BatchEnable(): %s", id)
	}
	for !op.Done {
		if err := sleep(ctx, enablePoll); err != nil {
			return err
		}
		if op, err = g.services.Operations.Get(op.Name).Context(ctx).Do(); err != nil {
			return errors.Wrapf(err, "serviceusage.Operations.Get(): %s", op.Name)
		}
	}
	if op.Error != nil {
		return errors.Newf("enabling the APIs of %s: %s", id, op.Error.Message)
	}

	return nil
}

func (g *google) ServiceAccountExists(ctx context.Context, email string) (bool, error) {
	_, err := g.iam.GetServiceAccount(ctx, &adminpb.GetServiceAccountRequest{Name: "projects/-/serviceAccounts/" + email})
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrapf(err, "admin.IamClient.GetServiceAccount(): %s", email)
	}

	return true, nil
}

func (g *google) CreateServiceAccount(ctx context.Context, project, accountID, displayName, description string) error {
	_, err := g.iam.CreateServiceAccount(ctx, &adminpb.CreateServiceAccountRequest{
		Name:           "projects/" + project,
		AccountId:      accountID,
		ServiceAccount: &adminpb.ServiceAccount{DisplayName: displayName, Description: description},
	})
	if err != nil {
		return errors.Wrapf(err, "admin.IamClient.CreateServiceAccount(): %s in %s", accountID, project)
	}

	return nil
}

func (g *google) BucketExists(ctx context.Context, name string) (bool, error) {
	_, err := g.storage.Bucket(name).Attrs(ctx)
	if errors.Is(err, storage.ErrBucketNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrapf(err, "storage.BucketHandle.Attrs(): %s", name)
	}

	return true, nil
}

func (g *google) CreateBucket(ctx context.Context, project, name, location string, labels map[string]string) error {
	attrs := &storage.BucketAttrs{
		Location:                 location,
		Labels:                   labels,
		UniformBucketLevelAccess: storage.UniformBucketLevelAccess{Enabled: true},
		PublicAccessPrevention:   storage.PublicAccessPreventionEnforced,
		VersioningEnabled:        true,
	}
	if err := g.storage.Bucket(name).Create(ctx, project, attrs); err != nil {
		return errors.Wrapf(err, "storage.BucketHandle.Create(): %s in %s", name, project)
	}

	return nil
}

func (g *google) GrantSecret(ctx context.Context, secret, role, member string) error {
	client, err := g.secretClient(ctx, secret)
	if err != nil {
		return err
	}
	handle := client.IAM(secret)
	policy, err := handle.Policy(ctx)
	if err != nil {
		return errors.Wrapf(err, "iam.Handle.Policy(): %s", secret)
	}
	if policy.HasRole(member, iam.RoleName(role)) {
		return nil
	}
	policy.Add(member, iam.RoleName(role))
	if err := handle.SetPolicy(ctx, policy); err != nil {
		return errors.Wrapf(err, "iam.Handle.SetPolicy(): %s", secret)
	}

	return nil
}

// secretClient is the Secret Manager client for the secret: the global one, or the
// regional endpoint's for a regional secret (projects/P/locations/L/secrets/N), which
// the global endpoint does not serve.
func (g *google) secretClient(ctx context.Context, secret string) (*secretmanager.Client, error) {
	parts := strings.Split(secret, "/")
	if len(parts) < 6 || parts[2] != "locations" {
		return g.secrets, nil
	}
	location := parts[3]
	if client, ok := g.regional[location]; ok {
		return client, nil
	}
	opts := append([]option.ClientOption{option.WithEndpoint("secretmanager." + location + ".rep.googleapis.com:443")}, g.opts...)
	client, err := secretmanager.NewClient(ctx, opts...)
	if err != nil {
		return nil, errors.Wrapf(err, "secretmanager.NewClient(): %s", location)
	}
	g.regional[location] = client

	return client, nil
}

func (g *google) Close() error {
	for _, c := range g.regional {
		_ = c.Close()
	}
	_ = g.secrets.Close()
	_ = g.storage.Close()
	_ = g.iam.Close()
	_ = g.projects.Close()
	if err := g.folders.Close(); err != nil {
		return errors.Wrap(err, "resourcemanager.FoldersClient.Close()")
	}

	return nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}

	return false
}

// sleep waits, or stops when the context does.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "context.Context.Err()")
	case <-timer.C:
		return nil
	}
}
