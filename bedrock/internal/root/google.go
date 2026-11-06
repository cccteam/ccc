// google.go is what the root organization's engine asks Google Cloud beyond the
// creation's seam (create.Google: folders, projects, service accounts, buckets and their
// grants): the root project's secrets with their versions and aliases, its custom role,
// its zones and record sets, its Cloud Build connection, repository link and triggers,
// the IAM policies the engine compares and sets, and the lock in the state bucket. Each
// is an interface the real clients implement and roottest fakes.

package root

import (
	"context"
	"time"

	"github.com/cccteam/ccc/bedrock/internal/create"
	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// Secret is a secret as Secret Manager describes it: its labels and its aliases, each
// alias to the version's number.
type Secret struct {
	Labels  map[string]string
	Aliases map[string]string
}

// SecretVersion is one version of a secret: its number and its state (ENABLED,
// DISABLED, DESTROYED).
type SecretVersion struct {
	Version string
	State   string
}

// Secrets is Secret Manager in the root project.
type Secrets interface {
	// Secret is the secret (projects/P/secrets/N), or nil when there is none.
	Secret(ctx context.Context, name string) (*Secret, error)
	// CreateSecret makes the secret in the project with automatic replication.
	CreateSecret(ctx context.Context, project, id string, labels map[string]string) error
	// SetSecretAliases sets the secret's aliases whole (alias to version number).
	SetSecretAliases(ctx context.Context, name string, aliases map[string]string) error
	// AddSecretVersion adds a version with the payload and answers its number.
	AddSecretVersion(ctx context.Context, name string, payload []byte) (string, error)
	// SecretVersions lists the secret's versions, newest first.
	SecretVersions(ctx context.Context, name string) ([]SecretVersion, error)
	// AccessSecretVersion is the payload of the version (projects/P/secrets/N/versions/V,
	// or an alias in place of the number).
	AccessSecretVersion(ctx context.Context, version string) ([]byte, error)
	// SecretPolicy is the secret's bindings, by role.
	SecretPolicy(ctx context.Context, name string) ([]org.Binding, error)
	// SetSecretPolicy sets the secret's bindings whole.
	SetSecretPolicy(ctx context.Context, name string, bindings []org.Binding) error
	Close() error
}

// Zone is a public zone as Cloud DNS describes it.
type Zone struct {
	Name        string
	DNSName     string
	Description string
	Labels      map[string]string
	DNSSEC      bool
	NameServers []string
}

// RecordSet is one record set of a zone.
type RecordSet struct {
	Name string
	Type string
	TTL  int64
	Data []string
}

// DNS is Cloud DNS in the root project.
type DNS interface {
	// Zones lists the project's zones.
	Zones(ctx context.Context, project string) ([]Zone, error)
	// CreateZone makes the zone, public, with DNSSEC on when the zone says so.
	CreateZone(ctx context.Context, project string, zone Zone) error
	// RecordSets lists the zone's record sets.
	RecordSets(ctx context.Context, project, zone string) ([]RecordSet, error)
	// ChangeRecordSets applies one change: the deletions, then the additions.
	ChangeRecordSets(ctx context.Context, project, zone string, additions, deletions []RecordSet) error
	Close() error
}

// Connection is a Cloud Build GitHub connection: the Cloud Build GitHub App's
// installation and the token secret version the connection authorizes with.
type Connection struct {
	InstallationID     string
	TokenSecretVersion string
}

// Trigger is a Cloud Build trigger as the engine compares it: what the retired
// cloud-build.tf declared, no more.
type Trigger struct {
	Name           string
	Description    string
	ServiceAccount string
	Substitutions  map[string]string
	// Repository is the repository link's resource name; PullRequestBranch or
	// PushBranch the branch pattern the trigger runs on.
	Repository        string
	PullRequestBranch string
	PushBranch        string
	Steps             []org.TriggerStep
	TimeoutSeconds    int
	// ID is the trigger's id, read back; empty on a trigger to make.
	ID string
}

// Builds is Cloud Build in the root project: the connection, the repository link and
// the two triggers.
type Builds interface {
	// Connection is the connection (projects/P/locations/L/connections/N), or nil.
	Connection(ctx context.Context, project, location, name string) (*Connection, error)
	// CreateConnection makes the connection and waits for it.
	CreateConnection(ctx context.Context, project, location, name string, c Connection) error
	// UpdateConnection changes the connection's installation or token.
	UpdateConnection(ctx context.Context, project, location, name string, c Connection) error
	// RepositoryLink is the remote URI of the repository linked under the connection, or
	// empty when there is none.
	RepositoryLink(ctx context.Context, project, location, connection, name string) (string, error)
	// CreateRepositoryLink links the repository under the connection.
	CreateRepositoryLink(ctx context.Context, project, location, connection, name, remoteURI string) error
	// Trigger is the trigger by name in the location, or nil.
	Trigger(ctx context.Context, project, location, name string) (*Trigger, error)
	// CreateTrigger makes the trigger.
	CreateTrigger(ctx context.Context, project, location string, t *Trigger) error
	// UpdateTrigger replaces the trigger (by its id).
	UpdateTrigger(ctx context.Context, project, location string, t *Trigger) error
	// ConnectionPolicy is the connection's bindings, by role.
	ConnectionPolicy(ctx context.Context, project, location, name string) ([]org.Binding, error)
	// GrantConnection adds the member to the role on the connection.
	GrantConnection(ctx context.Context, project, location, name, role, member string) error
	Close() error
}

// Role is a custom role as IAM describes it.
type Role struct {
	Title       string
	Description string
	Permissions []string
}

// Condition is an IAM binding's condition.
type Condition struct {
	Title       string
	Description string
	Expression  string
}

// IAM is the rest of Google Cloud the engine reads and sets: the root project's custom
// role, the policies of the project, the folder, the organization, the billing account,
// a service account and a bucket, the APIs a project has enabled, and a project's
// billing link.
type IAM interface {
	// Role is the project's custom role (projects/P/roles/ID), or nil.
	Role(ctx context.Context, project, id string) (*Role, error)
	CreateRole(ctx context.Context, project, id string, role Role) error
	UpdateRole(ctx context.Context, project, id string, role Role) error
	// ProjectPolicy, FolderPolicy, OrganizationPolicy and BillingPolicy are the
	// bindings of each, by role, conditions left out.
	ProjectPolicy(ctx context.Context, project string) ([]org.Binding, error)
	FolderPolicy(ctx context.Context, folder string) ([]org.Binding, error)
	OrganizationPolicy(ctx context.Context, organizationID string) ([]org.Binding, error)
	BillingPolicy(ctx context.Context, account string) ([]org.Binding, error)
	// GrantOrganization and RevokeOrganization change a binding at the organization
	// node; GrantBilling and RevokeBilling on the billing account.
	GrantOrganization(ctx context.Context, organizationID, role, member string) error
	RevokeOrganization(ctx context.Context, organizationID, member string, roles []string) error
	GrantBilling(ctx context.Context, account, role, member string) error
	RevokeBilling(ctx context.Context, account, member string, roles []string) error
	// ServiceAccountPolicy is the bindings on the service account (by email), and
	// GrantServiceAccount adds one.
	ServiceAccountPolicy(ctx context.Context, email string) ([]org.Binding, error)
	GrantServiceAccount(ctx context.Context, email, role, member string) error
	// BucketBindings are the bucket's bindings with their conditions, and GrantBucket
	// adds one, conditioned when the condition is not nil.
	BucketBindings(ctx context.Context, bucket string) ([]ConditionedBinding, error)
	GrantBucket(ctx context.Context, bucket, role, member string, condition *Condition) error
	// EnabledServices lists the APIs enabled in the project.
	EnabledServices(ctx context.Context, project string) ([]string, error)
	// ProjectBillingAccount is the billing account the project is linked to, empty
	// when none.
	ProjectBillingAccount(ctx context.Context, project string) (string, error)
	// DeleteProject deletes the project (Google keeps it thirty days); DeleteFolder the
	// folder, which must be empty.
	DeleteProject(ctx context.Context, project string) error
	DeleteFolder(ctx context.Context, folder string) error
	Close() error
}

// ConditionedBinding is a bucket's binding: the role, the members and the condition,
// nil for none.
type ConditionedBinding struct {
	Role      string
	Members   []string
	Condition *Condition
}

// Locks is the lock in the state bucket: one object made only when it is not there, so
// that two runs of the apply never overlap.
type Locks interface {
	// Lock makes the object with the data, or answers ErrLocked with the data of the
	// object that is there.
	Lock(ctx context.Context, bucket, object string, data []byte) ([]byte, error)
	// Unlock removes the object.
	Unlock(ctx context.Context, bucket, object string) error
	Close() error
}

// Each opener opens one seam with the run's Application Default Credentials.
type (
	// SecretsFunc opens Secret Manager.
	SecretsFunc func(ctx context.Context) (Secrets, error)
	// DNSFunc opens Cloud DNS.
	DNSFunc func(ctx context.Context) (DNS, error)
	// BuildsFunc opens Cloud Build.
	BuildsFunc func(ctx context.Context) (Builds, error)
	// IAMFunc opens the IAM and resource clients.
	IAMFunc func(ctx context.Context) (IAM, error)
	// LocksFunc opens the state bucket's lock.
	LocksFunc func(ctx context.Context) (Locks, error)
	// GoogleFunc opens the creation seam: folders, projects, identities and buckets.
	GoogleFunc func(ctx context.Context) (create.Google, error)
	// OrgPoliciesFunc opens the Org Policy client (the root project's runners hold
	// Organization Policy Administrator and Viewer at the organization node), billing
	// its calls to the root project: the API wants a quota project when the caller is a
	// person's application default credentials (bedrock org check at a terminal).
	OrgPoliciesFunc func(ctx context.Context, quotaProject string) (OrgPolicies, error)
)

// Clients are the engine's seams: Google Cloud as the run's own credentials (the
// break-glass account at the bootstrap, the runner identity in a trigger's run), GitHub
// with a token, the state bucket, and the creation of hosting organizations as the
// creating identity. Tests pass fakes.
type Clients struct {
	Google      GoogleFunc
	OrgPolicies OrgPoliciesFunc
	Secrets     SecretsFunc
	DNS         DNSFunc
	Builds      BuildsFunc
	IAM         IAMFunc
	Locks       LocksFunc
	GitHub      deploy.GitHubFunc
	Store       deploy.StoreFunc
	// Create are the creation's seams, for the hosting organizations the apply creates;
	// nil when the run creates none (check, plan).
	Create *create.Clients
	// Discovery and Permissions are the bootstrap's: whose the credentials are and what
	// they see, and the permission tests of the preflight.
	Discovery   DiscoveryFunc
	Permissions org.PermissionTesterFunc
	Sleep       deploy.SleepFunc
	// Clone makes the bootstrap's directory a clone of the repository it pushed, at the
	// commit (GitClone runs git); nil leaves the rendered files alone and the bootstrap
	// says how to clone.
	Clone CloneFunc
	// Now is the clock, for the lock's record and the markers.
	Now func() time.Time
}

// OrgPolicies reads and writes organization policies: the baseline the root organization
// sets on each hosting organization's folders.
type OrgPolicies interface {
	// Policy is the policy by resource name (folders/F/policies/CONSTRAINT), and whether
	// there is one.
	Policy(ctx context.Context, name string) (*Policy, bool, error)
	// CreatePolicy makes the policy on its parent (folders/F); UpdatePolicy replaces an
	// existing policy's rules with the policy's.
	CreatePolicy(ctx context.Context, parent string, policy *Policy) error
	UpdatePolicy(ctx context.Context, policy *Policy) error
	// Close releases the connection.
	Close() error
}

// Policy is one organization policy: its resource name and its rules, which replace the
// parent's (inheritFromParent false) so that the policy is the whole of the constraint
// on the subtree.
type Policy struct {
	Name  string
	Rules []PolicyRule
}

// PolicyRule is one rule of a policy, in one of four shapes: a boolean constraint
// enforced or not (Enforce), a list constraint denying every value (DenyAll) or allowing
// every value (AllowAll), or a list constraint with values allowed or denied (Values);
// Condition, when set, limits the rule to the resources its CEL expression matches, and
// Parameters are a managed constraint's parameters as JSON.
type PolicyRule struct {
	Enforce       *bool
	DenyAll       bool
	AllowAll      bool
	AllowedValues []string
	DeniedValues  []string
	Condition     *Condition
	Parameters    string
}
