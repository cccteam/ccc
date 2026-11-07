// googleimpl.go is the engine's seams over the Google APIs, each opened with the run's
// Application Default Credentials.

package root

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/impersonate"
	siteverification "google.golang.org/api/siteverification/v1"

	"google.golang.org/api/option"

	orgpolicy "cloud.google.com/go/orgpolicy/apiv2"
	"cloud.google.com/go/orgpolicy/apiv2/orgpolicypb"
	"google.golang.org/protobuf/types/known/structpb"

	"cloud.google.com/go/iam"
	admin "cloud.google.com/go/iam/admin/apiv1"
	"cloud.google.com/go/iam/admin/apiv1/adminpb"
	"cloud.google.com/go/iam/apiv1/iampb"
	resourcemanager "cloud.google.com/go/resourcemanager/apiv3"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"cloud.google.com/go/storage"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/cloudbilling/v1"
	cloudbuild "google.golang.org/api/cloudbuild/v1"
	cloudbuildv2 "google.golang.org/api/cloudbuild/v2"
	"google.golang.org/api/dns/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	"google.golang.org/api/serviceusage/v1"
	"google.golang.org/genproto/googleapis/type/expr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

// ErrLocked is Lock's answer when the lock object is there already.
var ErrLocked = errors.New("the lock is held")

// operationPoll is how often a long-running operation is asked after.
const operationPoll = 5 * time.Second

// --- Secret Manager ---

type secrets struct {
	client *secretmanager.Client
}

// NewSecrets opens Secret Manager.
func NewSecrets(ctx context.Context) (Secrets, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "secretmanager.NewClient()")
	}

	return &secrets{client: client}, nil
}

func (s *secrets) Secret(ctx context.Context, name string) (*Secret, error) {
	got, err := s.client.GetSecret(ctx, &secretmanagerpb.GetSecretRequest{Name: name})
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "secretmanager.Client.GetSecret(): %s", name)
	}
	aliases := map[string]string{}
	for alias, version := range got.GetVersionAliases() {
		aliases[alias] = strconv.FormatInt(version, 10)
	}

	return &Secret{Labels: got.GetLabels(), Aliases: aliases}, nil
}

func (s *secrets) CreateSecret(ctx context.Context, project, id string, labels map[string]string) error {
	_, err := s.client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
		Parent:   "projects/" + project,
		SecretId: id,
		Secret: &secretmanagerpb.Secret{
			Labels:      labels,
			Replication: &secretmanagerpb.Replication{Replication: &secretmanagerpb.Replication_Automatic_{Automatic: &secretmanagerpb.Replication_Automatic{}}},
		},
	})
	if err != nil {
		return errors.Wrapf(err, "secretmanager.Client.CreateSecret(): %s in %s", id, project)
	}

	return nil
}

func (s *secrets) SetSecretAliases(ctx context.Context, name string, aliases map[string]string) error {
	numbered := map[string]int64{}
	for alias, version := range aliases {
		n, err := strconv.ParseInt(version, 10, 64)
		if err != nil {
			return errors.Newf("the alias %s of %s names version %q, not a number", alias, name, version)
		}
		numbered[alias] = n
	}
	_, err := s.client.UpdateSecret(ctx, &secretmanagerpb.UpdateSecretRequest{
		Secret:     &secretmanagerpb.Secret{Name: name, VersionAliases: numbered},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"version_aliases"}},
	})
	if err != nil {
		return errors.Wrapf(err, "secretmanager.Client.UpdateSecret(): the aliases of %s", name)
	}

	return nil
}

func (s *secrets) AddSecretVersion(ctx context.Context, name string, payload []byte) (string, error) {
	v, err := s.client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{Parent: name, Payload: &secretmanagerpb.SecretPayload{Data: payload}})
	if err != nil {
		return "", errors.Wrapf(err, "secretmanager.Client.AddSecretVersion(): %s", name)
	}

	return v.GetName()[strings.LastIndex(v.GetName(), "/")+1:], nil
}

func (s *secrets) SecretVersions(ctx context.Context, name string) ([]SecretVersion, error) {
	var versions []SecretVersion
	it := s.client.ListSecretVersions(ctx, &secretmanagerpb.ListSecretVersionsRequest{Parent: name})
	for {
		v, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return versions, nil
		}
		if err != nil {
			return nil, errors.Wrapf(err, "secretmanager.Client.ListSecretVersions(): %s", name)
		}
		versions = append(versions, SecretVersion{Version: v.GetName()[strings.LastIndex(v.GetName(), "/")+1:], State: v.GetState().String()})
	}
}

func (s *secrets) AccessSecretVersion(ctx context.Context, version string) ([]byte, error) {
	got, err := s.client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{Name: version})
	if err != nil {
		return nil, errors.Wrapf(err, "secretmanager.Client.AccessSecretVersion(): %s", version)
	}

	return got.GetPayload().GetData(), nil
}

func (s *secrets) SecretPolicy(ctx context.Context, name string) ([]org.Binding, error) {
	policy, err := s.client.IAM(name).Policy(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "iam.Handle.Policy(): %s", name)
	}

	return bindingsOf(policy), nil
}

func (s *secrets) SetSecretPolicy(ctx context.Context, name string, bindings []org.Binding) error {
	handle := s.client.IAM(name)
	policy, err := handle.Policy(ctx)
	if err != nil {
		return errors.Wrapf(err, "iam.Handle.Policy(): %s", name)
	}
	for _, role := range policy.Roles() {
		for _, member := range policy.Members(role) {
			policy.Remove(member, role)
		}
	}
	for _, b := range bindings {
		for _, member := range b.Members {
			policy.Add(member, iam.RoleName(b.Role))
		}
	}
	if err := handle.SetPolicy(ctx, policy); err != nil {
		return errors.Wrapf(err, "iam.Handle.SetPolicy(): %s", name)
	}

	return nil
}

func (s *secrets) Close() error {
	if err := s.client.Close(); err != nil {
		return errors.Wrap(err, "secretmanager.Client.Close()")
	}

	return nil
}

// bindingsOf reads a policy's roles and members as bindings, in role order.
func bindingsOf(policy *iam.Policy) []org.Binding {
	roles := policy.Roles()
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	bindings := make([]org.Binding, 0, len(roles))
	for _, role := range roles {
		members := slices.Clone(policy.Members(role))
		sort.Strings(members)
		bindings = append(bindings, org.Binding{Role: string(role), Members: members})
	}

	return bindings
}

// --- Cloud DNS ---

type cloudDNS struct {
	service *dns.Service
}

// NewDNS opens Cloud DNS.
func NewDNS(ctx context.Context) (DNS, error) {
	service, err := dns.NewService(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "dns.NewService()")
	}

	return &cloudDNS{service: service}, nil
}

// NewDNSAs opens Cloud DNS as the identity, impersonated by the process's credentials:
// the bootstrap makes the zones as the applier.
func NewDNSAs(ctx context.Context, identity string) (DNS, error) {
	source, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{TargetPrincipal: identity, Scopes: []string{cloudPlatformScope}})
	if err != nil {
		return nil, errors.Wrapf(err, "impersonate.CredentialsTokenSource(): %s", identity)
	}
	service, err := dns.NewService(ctx, option.WithTokenSource(source))
	if err != nil {
		return nil, errors.Wrapf(err, "dns.NewService(): as %s", identity)
	}

	return &cloudDNS{service: service}, nil
}

// NewVerifierAs opens Google's webmaster verification as the identity, impersonated with
// the verification's own scope beside Cloud Platform's: the applier impersonates the
// creating identity, and itself, since a token for a Google API outside Cloud Platform's
// scope comes from IAM Credentials and not from the build's own credentials.
func NewVerifierAs(ctx context.Context, identity string) (Verifier, error) {
	source, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{TargetPrincipal: identity, Scopes: []string{cloudPlatformScope, siteverification.SiteverificationScope}})
	if err != nil {
		return nil, errors.Wrapf(err, "impersonate.CredentialsTokenSource(): %s", identity)
	}
	service, err := siteverification.NewService(ctx, option.WithTokenSource(source))
	if err != nil {
		return nil, errors.Wrapf(err, "siteverification.NewService(): as %s", identity)
	}

	return &siteVerifier{service: service}, nil
}

// siteVerifier is the real seam over the Site Verification API: a domain is an
// INET_DOMAIN resource, verified by a DNS_TXT record at its apex.
type siteVerifier struct {
	service *siteverification.Service
}

const (
	inetDomain = "INET_DOMAIN"
	dnsTXT     = "DNS_TXT"
)

func (v *siteVerifier) Owned(ctx context.Context, domain string) (bool, error) {
	list, err := v.service.WebResource.List().Context(ctx).Do()
	if err != nil {
		return false, errors.Wrap(err, "siteverification: WebResource.List()")
	}
	for _, r := range list.Items {
		if r.Site != nil && r.Site.Type == inetDomain && strings.TrimSuffix(r.Site.Identifier, ".") == domain {
			return true, nil
		}
	}

	return false, nil
}

func (v *siteVerifier) Token(ctx context.Context, domain string) (string, error) {
	res, err := v.service.WebResource.GetToken(&siteverification.SiteVerificationWebResourceGettokenRequest{
		Site:               &siteverification.SiteVerificationWebResourceGettokenRequestSite{Type: inetDomain, Identifier: domain},
		VerificationMethod: dnsTXT,
	}).Context(ctx).Do()
	if err != nil {
		return "", errors.Wrapf(err, "siteverification: WebResource.GetToken(): %s", domain)
	}

	return res.Token, nil
}

func (v *siteVerifier) Verify(ctx context.Context, domain string) (seen bool, err error) {
	_, err = v.service.WebResource.Insert(dnsTXT, &siteverification.SiteVerificationWebResourceResource{
		Site: &siteverification.SiteVerificationWebResourceResourceSite{Type: inetDomain, Identifier: domain},
	}).Context(ctx).Do()
	var gerr *googleapi.Error
	if errors.As(err, &gerr) && gerr.Code == http.StatusBadRequest {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrapf(err, "siteverification: WebResource.Insert(): %s", domain)
	}

	return true, nil
}

func (*siteVerifier) Close() error {
	return nil
}

func (d *cloudDNS) Zones(ctx context.Context, project string) ([]Zone, error) {
	var zones []Zone
	err := d.service.ManagedZones.List(project).Pages(ctx, func(page *dns.ManagedZonesListResponse) error {
		for _, z := range page.ManagedZones {
			zones = append(zones, Zone{Name: z.Name, DNSName: z.DnsName, Description: z.Description, Labels: z.Labels, DNSSEC: z.DnssecConfig != nil && z.DnssecConfig.State == "on", NameServers: z.NameServers})
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "dns.ManagedZonesService.List(): %s", project)
	}

	return zones, nil
}

func (d *cloudDNS) CreateZone(ctx context.Context, project string, zone Zone) error {
	z := &dns.ManagedZone{Name: zone.Name, DnsName: zone.DNSName, Description: zone.Description, Labels: zone.Labels, Visibility: "public"}
	if zone.DNSSEC {
		z.DnssecConfig = &dns.ManagedZoneDnsSecConfig{State: "on"}
	}
	if _, err := d.service.ManagedZones.Create(project, z).Context(ctx).Do(); err != nil {
		return errors.Wrapf(err, "dns.ManagedZonesService.Create(): %s in %s", zone.Name, project)
	}

	return nil
}

func (d *cloudDNS) RecordSets(ctx context.Context, project, zone string) ([]RecordSet, error) {
	var records []RecordSet
	err := d.service.ResourceRecordSets.List(project, zone).Pages(ctx, func(page *dns.ResourceRecordSetsListResponse) error {
		for _, rr := range page.Rrsets {
			records = append(records, RecordSet{Name: rr.Name, Type: rr.Type, TTL: rr.Ttl, Data: rr.Rrdatas})
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "dns.ResourceRecordSetsService.List(): %s in %s", zone, project)
	}

	return records, nil
}

func (d *cloudDNS) ChangeRecordSets(ctx context.Context, project, zone string, additions, deletions []RecordSet) error {
	change := &dns.Change{}
	for _, r := range additions {
		change.Additions = append(change.Additions, &dns.ResourceRecordSet{Name: r.Name, Type: r.Type, Ttl: r.TTL, Rrdatas: r.Data})
	}
	for _, r := range deletions {
		change.Deletions = append(change.Deletions, &dns.ResourceRecordSet{Name: r.Name, Type: r.Type, Ttl: r.TTL, Rrdatas: r.Data})
	}
	if _, err := d.service.Changes.Create(project, zone, change).Context(ctx).Do(); err != nil {
		return errors.Wrapf(err, "dns.ChangesService.Create(): %s in %s", zone, project)
	}

	return nil
}

func (*cloudDNS) Close() error {
	return nil
}

// --- Cloud Build ---

type builds struct {
	v1 *cloudbuild.Service
	v2 *cloudbuildv2.Service
}

// NewBuilds opens Cloud Build, both the triggers' API and the connections'.
func NewBuilds(ctx context.Context) (Builds, error) {
	v1, err := cloudbuild.NewService(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "cloudbuild.NewService()")
	}
	v2, err := cloudbuildv2.NewService(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "cloudbuildv2.NewService()")
	}

	return &builds{v1: v1, v2: v2}, nil
}

func connectionName(project, location, name string) string {
	return "projects/" + project + "/locations/" + location + "/connections/" + name
}

func (b *builds) Connection(ctx context.Context, project, location, name string) (*Connection, error) {
	c, err := b.v2.Projects.Locations.Connections.Get(connectionName(project, location, name)).Context(ctx).Do()
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "cloudbuildv2.ProjectsLocationsConnectionsService.Get(): %s", name)
	}
	out := &Connection{}
	if c.GithubConfig != nil {
		out.InstallationID = strconv.FormatInt(c.GithubConfig.AppInstallationId, 10)
		if c.GithubConfig.AuthorizerCredential != nil {
			out.TokenSecretVersion = c.GithubConfig.AuthorizerCredential.OauthTokenSecretVersion
		}
	}

	return out, nil
}

func (b *builds) connectionBody(c Connection) (*cloudbuildv2.Connection, error) {
	installation, err := strconv.ParseInt(c.InstallationID, 10, 64)
	if err != nil {
		return nil, errors.Newf("cloudBuildInstallationId %q is not a number", c.InstallationID)
	}

	return &cloudbuildv2.Connection{GithubConfig: &cloudbuildv2.GitHubConfig{AppInstallationId: installation, AuthorizerCredential: &cloudbuildv2.OAuthCredential{OauthTokenSecretVersion: c.TokenSecretVersion}}}, nil
}

func (b *builds) CreateConnection(ctx context.Context, project, location, name string, c Connection) error {
	body, err := b.connectionBody(c)
	if err != nil {
		return err
	}
	op, err := b.v2.Projects.Locations.Connections.Create("projects/"+project+"/locations/"+location, body).ConnectionId(name).Context(ctx).Do()
	if err != nil {
		return errors.Wrapf(err, "cloudbuildv2.ProjectsLocationsConnectionsService.Create(): %s", name)
	}

	return b.await(ctx, op, "the creation of connection "+name)
}

func (b *builds) UpdateConnection(ctx context.Context, project, location, name string, c Connection) error {
	body, err := b.connectionBody(c)
	if err != nil {
		return err
	}
	op, err := b.v2.Projects.Locations.Connections.Patch(connectionName(project, location, name), body).UpdateMask("github_config").Context(ctx).Do()
	if err != nil {
		return errors.Wrapf(err, "cloudbuildv2.ProjectsLocationsConnectionsService.Patch(): %s", name)
	}

	return b.await(ctx, op, "the update of connection "+name)
}

// await waits for the operation to finish.
func (b *builds) await(ctx context.Context, op *cloudbuildv2.Operation, what string) error {
	for !op.Done {
		if err := sleep(ctx, operationPoll); err != nil {
			return err
		}
		latest, err := b.v2.Projects.Locations.Operations.Get(op.Name).Context(ctx).Do()
		if err != nil {
			return errors.Wrapf(err, "cloudbuildv2.ProjectsLocationsOperationsService.Get(): %s", what)
		}
		op = latest
	}
	if op.Error != nil {
		return errors.Newf("%s: %s", what, op.Error.Message)
	}

	return nil
}

func (b *builds) RepositoryLink(ctx context.Context, project, location, connection, name string) (string, error) {
	r, err := b.v2.Projects.Locations.Connections.Repositories.Get(connectionName(project, location, connection) + "/repositories/" + name).Context(ctx).Do()
	if isNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", errors.Wrapf(err, "cloudbuildv2.ProjectsLocationsConnectionsRepositoriesService.Get(): %s", name)
	}

	return r.RemoteUri, nil
}

func (b *builds) CreateRepositoryLink(ctx context.Context, project, location, connection, name, remoteURI string) error {
	op, err := b.v2.Projects.Locations.Connections.Repositories.Create(connectionName(project, location, connection), &cloudbuildv2.Repository{RemoteUri: remoteURI}).RepositoryId(name).Context(ctx).Do()
	if err != nil {
		return errors.Wrapf(err, "cloudbuildv2.ProjectsLocationsConnectionsRepositoriesService.Create(): %s", name)
	}

	return b.await(ctx, op, "the link of repository "+name)
}

func (b *builds) Trigger(ctx context.Context, project, location, name string) (*Trigger, error) {
	var found *cloudbuild.BuildTrigger
	err := b.v1.Projects.Locations.Triggers.List("projects/"+project+"/locations/"+location).Pages(ctx, func(page *cloudbuild.ListBuildTriggersResponse) error {
		for _, t := range page.Triggers {
			if t.Name == name {
				found = t
			}
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "cloudbuild.ProjectsLocationsTriggersService.List(): %s", location)
	}
	if found == nil {
		return nil, nil
	}

	return triggerOf(found), nil
}

// triggerOf reads a trigger as the engine compares it.
func triggerOf(t *cloudbuild.BuildTrigger) *Trigger {
	out := &Trigger{ID: t.Id, Name: t.Name, Description: t.Description, ServiceAccount: t.ServiceAccount, Substitutions: t.Substitutions}
	if t.RepositoryEventConfig != nil {
		out.Repository = t.RepositoryEventConfig.Repository
		if t.RepositoryEventConfig.PullRequest != nil {
			out.PullRequestBranch = t.RepositoryEventConfig.PullRequest.Branch
		}
		if t.RepositoryEventConfig.Push != nil {
			out.PushBranch = t.RepositoryEventConfig.Push.Branch
		}
	}
	if t.Build != nil {
		out.TimeoutSeconds = seconds(t.Build.Timeout)
		for _, s := range t.Build.Steps {
			env := s.Env
			if env == nil {
				env = []string{}
			}
			out.Steps = append(out.Steps, org.TriggerStep{ID: s.Id, Image: s.Name, Entrypoint: s.Entrypoint, Args: s.Args, Env: env, TimeoutSeconds: seconds(s.Timeout)})
		}
	}

	return out
}

// seconds reads a duration as the API writes it (300s).
func seconds(d string) int {
	n, _ := strconv.Atoi(strings.TrimSuffix(d, "s"))

	return n
}

// buildTrigger writes a trigger as the API takes it.
func buildTrigger(t *Trigger) *cloudbuild.BuildTrigger {
	steps := make([]*cloudbuild.BuildStep, 0, len(t.Steps))
	for _, s := range t.Steps {
		steps = append(steps, &cloudbuild.BuildStep{Id: s.ID, Name: s.Image, Entrypoint: s.Entrypoint, Args: s.Args, Env: s.Env, Timeout: fmt.Sprintf("%ds", s.TimeoutSeconds)})
	}
	config := &cloudbuild.RepositoryEventConfig{Repository: t.Repository}
	if t.PullRequestBranch != "" {
		config.PullRequest = &cloudbuild.PullRequestFilter{Branch: t.PullRequestBranch}
	}
	if t.PushBranch != "" {
		config.Push = &cloudbuild.PushFilter{Branch: t.PushBranch}
	}

	return &cloudbuild.BuildTrigger{
		Id: t.ID, Name: t.Name, Description: t.Description, ServiceAccount: t.ServiceAccount, Substitutions: t.Substitutions,
		IncludeBuildLogs: "INCLUDE_BUILD_LOGS_WITH_STATUS", RepositoryEventConfig: config,
		Build: &cloudbuild.Build{Steps: steps, Timeout: fmt.Sprintf("%ds", t.TimeoutSeconds), Options: &cloudbuild.BuildOptions{Logging: "CLOUD_LOGGING_ONLY"}},
	}
}

func (b *builds) CreateTrigger(ctx context.Context, project, location string, t *Trigger) error {
	if _, err := b.v1.Projects.Locations.Triggers.Create("projects/"+project+"/locations/"+location, buildTrigger(t)).Context(ctx).Do(); err != nil {
		return errors.Wrapf(err, "cloudbuild.ProjectsLocationsTriggersService.Create(): %s", t.Name)
	}

	return nil
}

func (b *builds) UpdateTrigger(ctx context.Context, project, location string, t *Trigger) error {
	name := "projects/" + project + "/locations/" + location + "/triggers/" + t.ID
	if _, err := b.v1.Projects.Locations.Triggers.Patch(name, buildTrigger(t)).Context(ctx).Do(); err != nil {
		return errors.Wrapf(err, "cloudbuild.ProjectsLocationsTriggersService.Patch(): %s", t.Name)
	}

	return nil
}

func (b *builds) ConnectionPolicy(ctx context.Context, project, location, name string) ([]org.Binding, error) {
	policy, err := b.v2.Projects.Locations.Connections.GetIamPolicy(connectionName(project, location, name)).Context(ctx).Do()
	if err != nil {
		return nil, errors.Wrapf(err, "cloudbuildv2.ProjectsLocationsConnectionsService.GetIamPolicy(): %s", name)
	}
	var bindings []org.Binding
	for _, b := range policy.Bindings {
		if b.Condition == nil {
			bindings = append(bindings, org.Binding{Role: b.Role, Members: b.Members})
		}
	}

	return bindings, nil
}

func (b *builds) GrantConnection(ctx context.Context, project, location, name, role, member string) error {
	resource := connectionName(project, location, name)
	policy, err := b.v2.Projects.Locations.Connections.GetIamPolicy(resource).Context(ctx).Do()
	if err != nil {
		return errors.Wrapf(err, "cloudbuildv2.ProjectsLocationsConnectionsService.GetIamPolicy(): %s", name)
	}
	added := false
	for _, binding := range policy.Bindings {
		if binding.Role == role && binding.Condition == nil {
			if slices.Contains(binding.Members, member) {
				return nil
			}
			binding.Members = append(binding.Members, member)
			added = true
		}
	}
	if !added {
		policy.Bindings = append(policy.Bindings, &cloudbuildv2.Binding{Role: role, Members: []string{member}})
	}
	if _, err := b.v2.Projects.Locations.Connections.SetIamPolicy(resource, &cloudbuildv2.SetIamPolicyRequest{Policy: policy}).Context(ctx).Do(); err != nil {
		return errors.Wrapf(err, "cloudbuildv2.ProjectsLocationsConnectionsService.SetIamPolicy(): %s", name)
	}

	return nil
}

func (*builds) Close() error {
	return nil
}

// --- IAM and the rest ---

type iamClients struct {
	admin         *admin.IamClient
	projects      *resourcemanager.ProjectsClient
	folders       *resourcemanager.FoldersClient
	organizations *resourcemanager.OrganizationsClient
	billing       *cloudbilling.APIService
	storage       *storage.Client
	services      *serviceusage.Service
}

// NewIAM opens IAM's role administration, Cloud Resource Manager, Cloud Billing, Cloud
// Storage and Service Usage.
func NewIAM(ctx context.Context) (IAM, error) {
	c := &iamClients{}
	var err error
	if c.admin, err = admin.NewIamClient(ctx); err != nil {
		return nil, errors.Wrap(err, "admin.NewIamClient()")
	}
	if c.projects, err = resourcemanager.NewProjectsClient(ctx); err != nil {
		return nil, errors.Wrap(err, "resourcemanager.NewProjectsClient()")
	}
	if c.folders, err = resourcemanager.NewFoldersClient(ctx); err != nil {
		return nil, errors.Wrap(err, "resourcemanager.NewFoldersClient()")
	}
	if c.organizations, err = resourcemanager.NewOrganizationsClient(ctx); err != nil {
		return nil, errors.Wrap(err, "resourcemanager.NewOrganizationsClient()")
	}
	if c.billing, err = cloudbilling.NewService(ctx); err != nil {
		return nil, errors.Wrap(err, "cloudbilling.NewService()")
	}
	if c.storage, err = storage.NewClient(ctx); err != nil {
		return nil, errors.Wrap(err, "storage.NewClient()")
	}
	if c.services, err = serviceusage.NewService(ctx); err != nil {
		return nil, errors.Wrap(err, "serviceusage.NewService()")
	}

	return c, nil
}

func (c *iamClients) Role(ctx context.Context, project, id string) (*Role, error) {
	role, err := c.admin.GetRole(ctx, &adminpb.GetRoleRequest{Name: "projects/" + project + "/roles/" + id})
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "admin.IamClient.GetRole(): %s in %s", id, project)
	}
	if role.GetDeleted() {
		return nil, errors.Newf("the custom role %s in %s is deleted; Google keeps the id for a while, and it cannot be made again until then: undelete it (gcloud iam roles undelete %s --project %s)", id, project, id, project)
	}
	permissions := slices.Clone(role.GetIncludedPermissions())
	sort.Strings(permissions)

	return &Role{Title: role.GetTitle(), Description: role.GetDescription(), Permissions: permissions}, nil
}

func (c *iamClients) CreateRole(ctx context.Context, project, id string, role Role) error {
	_, err := c.admin.CreateRole(ctx, &adminpb.CreateRoleRequest{
		Parent: "projects/" + project, RoleId: id,
		Role: &adminpb.Role{Title: role.Title, Description: role.Description, IncludedPermissions: role.Permissions, Stage: adminpb.Role_GA},
	})
	if err != nil {
		return errors.Wrapf(err, "admin.IamClient.CreateRole(): %s in %s", id, project)
	}

	return nil
}

func (c *iamClients) UpdateRole(ctx context.Context, project, id string, role Role) error {
	_, err := c.admin.UpdateRole(ctx, &adminpb.UpdateRoleRequest{
		Name:       "projects/" + project + "/roles/" + id,
		Role:       &adminpb.Role{Title: role.Title, Description: role.Description, IncludedPermissions: role.Permissions},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title", "description", "included_permissions"}},
	})
	if err != nil {
		return errors.Wrapf(err, "admin.IamClient.UpdateRole(): %s in %s", id, project)
	}

	return nil
}

// policy is one resource's IAM policy: read whole, set whole.
type policy struct {
	get func(ctx context.Context) (*iampb.Policy, error)
	set func(ctx context.Context, p *iampb.Policy) error
}

func (c *iamClients) projectPolicy(project string) policy {
	resource := "projects/" + project

	return policy{
		get: func(ctx context.Context) (*iampb.Policy, error) {
			p, err := c.projects.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Resource: resource})

			return p, wrapf(err, "resourcemanager.ProjectsClient.GetIamPolicy(): %s", resource)
		},
		set: func(ctx context.Context, p *iampb.Policy) error {
			_, err := c.projects.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{Resource: resource, Policy: p})

			return wrapf(err, "resourcemanager.ProjectsClient.SetIamPolicy(): %s", resource)
		},
	}
}

func (c *iamClients) folderPolicy(folder string) policy {
	resource := "folders/" + folder

	return policy{
		get: func(ctx context.Context) (*iampb.Policy, error) {
			p, err := c.folders.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Resource: resource})

			return p, wrapf(err, "resourcemanager.FoldersClient.GetIamPolicy(): %s", resource)
		},
		set: func(ctx context.Context, p *iampb.Policy) error {
			_, err := c.folders.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{Resource: resource, Policy: p})

			return wrapf(err, "resourcemanager.FoldersClient.SetIamPolicy(): %s", resource)
		},
	}
}

func (c *iamClients) organizationPolicy(organizationID string) policy {
	resource := "organizations/" + organizationID

	return policy{
		get: func(ctx context.Context) (*iampb.Policy, error) {
			p, err := c.organizations.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Resource: resource})

			return p, wrapf(err, "resourcemanager.OrganizationsClient.GetIamPolicy(): %s", resource)
		},
		set: func(ctx context.Context, p *iampb.Policy) error {
			_, err := c.organizations.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{Resource: resource, Policy: p})

			return wrapf(err, "resourcemanager.OrganizationsClient.SetIamPolicy(): %s", resource)
		},
	}
}

func (c *iamClients) serviceAccountPolicy(email string) policy {
	resource := "projects/-/serviceAccounts/" + email

	return policy{
		get: func(ctx context.Context) (*iampb.Policy, error) {
			p, err := c.admin.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Resource: resource})
			if err != nil {
				return nil, errors.Wrapf(err, "admin.IamClient.GetIamPolicy(): %s", email)
			}

			return p.InternalProto, nil
		},
		set: func(ctx context.Context, p *iampb.Policy) error {
			_, err := c.admin.SetIamPolicy(ctx, &admin.SetIamPolicyRequest{Resource: resource, Policy: &iam.Policy{InternalProto: p}})

			return wrapf(err, "admin.IamClient.SetIamPolicy(): %s", email)
		},
	}
}

// read is the policy's bindings without conditions.
func (p policy) read(ctx context.Context) ([]org.Binding, error) {
	current, err := p.get(ctx)
	if err != nil {
		return nil, err
	}
	var bindings []org.Binding
	for _, b := range current.GetBindings() {
		if b.GetCondition() == nil {
			members := slices.Clone(b.GetMembers())
			sort.Strings(members)
			bindings = append(bindings, org.Binding{Role: b.GetRole(), Members: members})
		}
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].Role < bindings[j].Role })

	return bindings, nil
}

// grant adds the member to the role, unless it is there.
func (p policy) grant(ctx context.Context, role, member string) error {
	current, err := p.get(ctx)
	if err != nil {
		return err
	}
	for _, b := range current.GetBindings() {
		if b.GetRole() == role && b.GetCondition() == nil {
			if slices.Contains(b.GetMembers(), member) {
				return nil
			}
			b.Members = append(b.Members, member)

			return p.set(ctx, current)
		}
	}
	current.Bindings = append(current.Bindings, &iampb.Binding{Role: role, Members: []string{member}})

	return p.set(ctx, current)
}

// revoke takes the member out of each role, unless it is in none.
func (p policy) revoke(ctx context.Context, member string, roles []string) error {
	current, err := p.get(ctx)
	if err != nil {
		return err
	}
	changed := false
	kept := make([]*iampb.Binding, 0, len(current.GetBindings()))
	for _, b := range current.GetBindings() {
		if !slices.Contains(roles, b.GetRole()) || b.GetCondition() != nil || !slices.Contains(b.GetMembers(), member) {
			kept = append(kept, b)

			continue
		}
		changed = true
		members := slices.DeleteFunc(slices.Clone(b.GetMembers()), func(m string) bool { return m == member })
		if len(members) > 0 {
			b.Members = members
			kept = append(kept, b)
		}
	}
	if !changed {
		return nil
	}
	current.Bindings = kept

	return p.set(ctx, current)
}

func (c *iamClients) ProjectPolicy(ctx context.Context, project string) ([]org.Binding, error) {
	return c.projectPolicy(project).read(ctx)
}

func (c *iamClients) FolderPolicy(ctx context.Context, folder string) ([]org.Binding, error) {
	return c.folderPolicy(folder).read(ctx)
}

func (c *iamClients) OrganizationPolicy(ctx context.Context, organizationID string) ([]org.Binding, error) {
	return c.organizationPolicy(organizationID).read(ctx)
}

func (c *iamClients) GrantOrganization(ctx context.Context, organizationID, role, member string) error {
	return c.organizationPolicy(organizationID).grant(ctx, role, member)
}

func (c *iamClients) RevokeOrganization(ctx context.Context, organizationID, member string, roles []string) error {
	return c.organizationPolicy(organizationID).revoke(ctx, member, roles)
}

func (c *iamClients) ServiceAccountPolicy(ctx context.Context, email string) ([]org.Binding, error) {
	return c.serviceAccountPolicy(email).read(ctx)
}

func (c *iamClients) GrantServiceAccount(ctx context.Context, email, role, member string) error {
	return c.serviceAccountPolicy(email).grant(ctx, role, member)
}

func (c *iamClients) RevokeServiceAccount(ctx context.Context, email, role, member string) error {
	return c.serviceAccountPolicy(email).revoke(ctx, member, []string{role})
}

func (c *iamClients) BillingPolicy(ctx context.Context, account string) ([]org.Binding, error) {
	p, err := c.billing.BillingAccounts.GetIamPolicy("billingAccounts/" + account).Context(ctx).Do()
	if err != nil {
		return nil, errors.Wrapf(err, "cloudbilling.BillingAccountsService.GetIamPolicy(): %s", account)
	}
	var bindings []org.Binding
	for _, b := range p.Bindings {
		if b.Condition == nil {
			members := slices.Clone(b.Members)
			sort.Strings(members)
			bindings = append(bindings, org.Binding{Role: b.Role, Members: members})
		}
	}

	return bindings, nil
}

func (c *iamClients) GrantBilling(ctx context.Context, account, role, member string) error {
	resource := "billingAccounts/" + account
	p, err := c.billing.BillingAccounts.GetIamPolicy(resource).Context(ctx).Do()
	if err != nil {
		return errors.Wrapf(err, "cloudbilling.BillingAccountsService.GetIamPolicy(): %s", account)
	}
	added := false
	for _, b := range p.Bindings {
		if b.Role == role && b.Condition == nil {
			if slices.Contains(b.Members, member) {
				return nil
			}
			b.Members = append(b.Members, member)
			added = true
		}
	}
	if !added {
		p.Bindings = append(p.Bindings, &cloudbilling.Binding{Role: role, Members: []string{member}})
	}
	if _, err := c.billing.BillingAccounts.SetIamPolicy(resource, &cloudbilling.SetIamPolicyRequest{Policy: p}).Context(ctx).Do(); err != nil {
		return errors.Wrapf(err, "cloudbilling.BillingAccountsService.SetIamPolicy(): %s", account)
	}

	return nil
}

func (c *iamClients) RevokeBilling(ctx context.Context, account, member string, roles []string) error {
	resource := "billingAccounts/" + account
	p, err := c.billing.BillingAccounts.GetIamPolicy(resource).Context(ctx).Do()
	if err != nil {
		return errors.Wrapf(err, "cloudbilling.BillingAccountsService.GetIamPolicy(): %s", account)
	}
	changed := false
	kept := make([]*cloudbilling.Binding, 0, len(p.Bindings))
	for _, b := range p.Bindings {
		if !slices.Contains(roles, b.Role) || b.Condition != nil || !slices.Contains(b.Members, member) {
			kept = append(kept, b)

			continue
		}
		changed = true
		members := slices.DeleteFunc(slices.Clone(b.Members), func(m string) bool { return m == member })
		if len(members) > 0 {
			b.Members = members
			kept = append(kept, b)
		}
	}
	if !changed {
		return nil
	}
	p.Bindings = kept
	if _, err := c.billing.BillingAccounts.SetIamPolicy(resource, &cloudbilling.SetIamPolicyRequest{Policy: p}).Context(ctx).Do(); err != nil {
		return errors.Wrapf(err, "cloudbilling.BillingAccountsService.SetIamPolicy(): %s", account)
	}

	return nil
}

func (c *iamClients) BucketBindings(ctx context.Context, bucket string) ([]ConditionedBinding, error) {
	p, err := c.storage.Bucket(bucket).IAM().V3().Policy(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "iam.Handle3.Policy(): %s", bucket)
	}
	bindings := make([]ConditionedBinding, 0, len(p.Bindings))
	for _, b := range p.Bindings {
		cb := ConditionedBinding{Role: b.GetRole(), Members: slices.Clone(b.GetMembers())}
		if cond := b.GetCondition(); cond != nil {
			cb.Condition = &Condition{Title: cond.GetTitle(), Description: cond.GetDescription(), Expression: cond.GetExpression()}
		}
		bindings = append(bindings, cb)
	}

	return bindings, nil
}

func (c *iamClients) GrantBucket(ctx context.Context, bucket, role, member string, condition *Condition) error {
	handle := c.storage.Bucket(bucket).IAM().V3()
	p, err := handle.Policy(ctx)
	if err != nil {
		return errors.Wrapf(err, "iam.Handle3.Policy(): %s", bucket)
	}
	for _, b := range p.Bindings {
		if b.GetRole() != role || !sameCondition(b, condition) {
			continue
		}
		if slices.Contains(b.GetMembers(), member) {
			return nil
		}
		b.Members = append(b.Members, member)
		if err := handle.SetPolicy(ctx, p); err != nil {
			return errors.Wrapf(err, "iam.Handle3.SetPolicy(): %s", bucket)
		}

		return nil
	}
	binding := &iampb.Binding{Role: role, Members: []string{member}}
	if condition != nil {
		binding.Condition = &expr.Expr{Title: condition.Title, Description: condition.Description, Expression: condition.Expression}
	}
	p.Bindings = append(p.Bindings, binding)
	if err := handle.SetPolicy(ctx, p); err != nil {
		return errors.Wrapf(err, "iam.Handle3.SetPolicy(): %s", bucket)
	}

	return nil
}

// sameCondition reports whether the binding's condition is the one wanted: none and
// none, or the same title.
func sameCondition(b *iampb.Binding, condition *Condition) bool {
	if b.GetCondition() == nil {
		return condition == nil
	}

	return condition != nil && b.GetCondition().GetTitle() == condition.Title
}

func (c *iamClients) EnabledServices(ctx context.Context, project string) ([]string, error) {
	var enabled []string
	err := c.services.Services.List("projects/"+project).Filter("state:ENABLED").Pages(ctx, func(page *serviceusage.ListServicesResponse) error {
		for _, s := range page.Services {
			enabled = append(enabled, s.Name[strings.LastIndex(s.Name, "/")+1:])
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "serviceusage.ServicesService.List(): %s", project)
	}
	sort.Strings(enabled)

	return enabled, nil
}

func (c *iamClients) ProjectBillingAccount(ctx context.Context, project string) (string, error) {
	info, err := c.billing.Projects.GetBillingInfo("projects/" + project).Context(ctx).Do()
	if err != nil {
		return "", errors.Wrapf(err, "cloudbilling.ProjectsService.GetBillingInfo(): %s", project)
	}

	return strings.TrimPrefix(info.BillingAccountName, "billingAccounts/"), nil
}

func (c *iamClients) DeleteProject(ctx context.Context, project string) error {
	op, err := c.projects.DeleteProject(ctx, &resourcemanagerpb.DeleteProjectRequest{Name: "projects/" + project})
	if err != nil {
		return errors.Wrapf(err, "resourcemanager.ProjectsClient.DeleteProject(): %s", project)
	}
	if _, err := op.Wait(ctx); err != nil {
		return errors.Wrapf(err, "the deletion of project %s", project)
	}

	return nil
}

func (c *iamClients) DeleteFolder(ctx context.Context, folder string) error {
	op, err := c.folders.DeleteFolder(ctx, &resourcemanagerpb.DeleteFolderRequest{Name: "folders/" + folder})
	if err != nil {
		return errors.Wrapf(err, "resourcemanager.FoldersClient.DeleteFolder(): %s", folder)
	}
	if _, err := op.Wait(ctx); err != nil {
		return errors.Wrapf(err, "the deletion of folder %s", folder)
	}

	return nil
}

func (c *iamClients) Close() error {
	_ = c.storage.Close()
	_ = c.organizations.Close()
	_ = c.folders.Close()
	_ = c.projects.Close()
	if err := c.admin.Close(); err != nil {
		return errors.Wrap(err, "admin.IamClient.Close()")
	}

	return nil
}

// --- the lock ---

type locks struct {
	client *storage.Client
}

// NewLocks opens Cloud Storage for the lock.
func NewLocks(ctx context.Context) (Locks, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "storage.NewClient()")
	}

	return &locks{client: client}, nil
}

func (l *locks) Lock(ctx context.Context, bucket, object string, data []byte) ([]byte, error) {
	w := l.client.Bucket(bucket).Object(object).If(storage.Conditions{DoesNotExist: true}).NewWriter(ctx)
	w.ContentType = "application/json"
	if _, err := w.Write(data); err != nil {
		_ = w.Close()

		return nil, errors.Wrapf(err, "storage.Writer.Write(): gs://%s/%s", bucket, object)
	}
	err := w.Close()
	if err == nil {
		return nil, nil
	}
	var apiErr *googleapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != http.StatusPreconditionFailed {
		return nil, errors.Wrapf(err, "storage.Writer.Close(): gs://%s/%s", bucket, object)
	}
	r, err := l.client.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "storage.ObjectHandle.NewReader(): the lock gs://%s/%s", bucket, object)
	}
	defer r.Close()
	held, err := io.ReadAll(r)
	if err != nil {
		return nil, errors.Wrapf(err, "reading the lock gs://%s/%s", bucket, object)
	}

	return held, ErrLocked
}

func (l *locks) Unlock(ctx context.Context, bucket, object string) error {
	err := l.client.Bucket(bucket).Object(object).Delete(ctx)
	if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
		return errors.Wrapf(err, "storage.ObjectHandle.Delete(): the lock gs://%s/%s", bucket, object)
	}

	return nil
}

func (l *locks) Close() error {
	if err := l.client.Close(); err != nil {
		return errors.Wrap(err, "storage.Client.Close()")
	}

	return nil
}

// isNotFound reports a REST API's 404.
func isNotFound(err error) bool {
	var apiErr *googleapi.Error

	return errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound
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

// wrapf is errors.Wrapf for an error that may be nil, which it leaves nil: a policy's
// read or write that went through has no error to wrap.
func wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}

	return errors.Wrapf(err, format, args...)
}

// NewOrgPolicies opens the Org Policy client with the application default credentials.
func NewOrgPolicies(ctx context.Context, quotaProject string) (OrgPolicies, error) {
	c, err := orgpolicy.NewClient(ctx, option.WithQuotaProject(quotaProject))
	if err != nil {
		return nil, errors.Wrap(err, "orgpolicy.NewClient()")
	}

	return &orgPolicies{c: c}, nil
}

type orgPolicies struct {
	c *orgpolicy.Client
}

func (o *orgPolicies) Policy(ctx context.Context, name string) (*Policy, bool, error) {
	got, err := o.c.GetPolicy(ctx, &orgpolicypb.GetPolicyRequest{Name: name})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, false, nil
		}

		return nil, false, errors.Wrapf(err, "orgpolicy.Client.GetPolicy(): %s", name)
	}

	return policyFromProto(got), true, nil
}

func (o *orgPolicies) CreatePolicy(ctx context.Context, parent string, policy *Policy) error {
	_, err := o.c.CreatePolicy(ctx, &orgpolicypb.CreatePolicyRequest{Parent: parent, Policy: policyToProto(policy)})

	return wrapf(err, "orgpolicy.Client.CreatePolicy(): %s", policy.Name)
}

func (o *orgPolicies) UpdatePolicy(ctx context.Context, policy *Policy) error {
	_, err := o.c.UpdatePolicy(ctx, &orgpolicypb.UpdatePolicyRequest{Policy: policyToProto(policy)})

	return wrapf(err, "orgpolicy.Client.UpdatePolicy(): %s", policy.Name)
}

func (o *orgPolicies) Close() error {
	return wrapf(o.c.Close(), "orgpolicy.Client.Close()")
}

// policyToProto is the policy as the API takes it: its rules, replacing the parent's.
func policyToProto(p *Policy) *orgpolicypb.Policy {
	spec := &orgpolicypb.PolicySpec{InheritFromParent: false, Reset_: false}
	for _, r := range p.Rules {
		rule := &orgpolicypb.PolicySpec_PolicyRule{}
		switch {
		case r.Enforce != nil:
			rule.Kind = &orgpolicypb.PolicySpec_PolicyRule_Enforce{Enforce: *r.Enforce}
		case r.DenyAll:
			rule.Kind = &orgpolicypb.PolicySpec_PolicyRule_DenyAll{DenyAll: true}
		case r.AllowAll:
			rule.Kind = &orgpolicypb.PolicySpec_PolicyRule_AllowAll{AllowAll: true}
		default:
			rule.Kind = &orgpolicypb.PolicySpec_PolicyRule_Values{Values: &orgpolicypb.PolicySpec_PolicyRule_StringValues{AllowedValues: r.AllowedValues, DeniedValues: r.DeniedValues}}
		}
		if r.Condition != nil {
			rule.Condition = &expr.Expr{Title: r.Condition.Title, Description: r.Condition.Description, Expression: r.Condition.Expression}
		}
		if r.Parameters != "" {
			params := &structpb.Struct{}
			// The parameters are JSON the engine composed; a failure here would be
			// bedrock's own, and the API refuses the policy then.
			_ = params.UnmarshalJSON([]byte(r.Parameters))
			rule.Parameters = params
		}
		spec.Rules = append(spec.Rules, rule)
	}

	return &orgpolicypb.Policy{Name: p.Name, Spec: spec}
}

// policyFromProto is the policy as read, in the engine's shape, for the comparison.
func policyFromProto(p *orgpolicypb.Policy) *Policy {
	out := &Policy{Name: p.GetName()}
	for _, r := range p.GetSpec().GetRules() {
		rule := PolicyRule{}
		switch k := r.GetKind().(type) {
		case *orgpolicypb.PolicySpec_PolicyRule_Enforce:
			enforce := k.Enforce
			rule.Enforce = &enforce
		case *orgpolicypb.PolicySpec_PolicyRule_DenyAll:
			rule.DenyAll = k.DenyAll
		case *orgpolicypb.PolicySpec_PolicyRule_AllowAll:
			rule.AllowAll = k.AllowAll
		case *orgpolicypb.PolicySpec_PolicyRule_Values:
			rule.AllowedValues, rule.DeniedValues = k.Values.GetAllowedValues(), k.Values.GetDeniedValues()
		}
		if c := r.GetCondition(); c != nil {
			rule.Condition = &Condition{Title: c.GetTitle(), Description: c.GetDescription(), Expression: c.GetExpression()}
		}
		if params := r.GetParameters(); params != nil {
			if data, err := params.MarshalJSON(); err == nil {
				rule.Parameters = string(data)
			}
		}
		out.Rules = append(out.Rules, rule)
	}

	return out
}
