// Package roottest is a stand-in for the root engine's Google seams: secrets, zones,
// Cloud Build, IAM and the lock, answered from memory and recorded, so a test can read
// what an apply made and start from an organization that stands in part.
package roottest

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-playground/errors/v5"

	"google.golang.org/grpc/codes"

	"google.golang.org/grpc/status"

	"github.com/cccteam/ccc/bedrock/internal/budgets"
	"github.com/cccteam/ccc/bedrock/internal/create"
	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/root"
)

// Fake is the stand-in for every seam at once. Calls records every call, in order.
type Fake struct {
	mu    sync.Mutex
	Calls []string
	// Refuse names a call (its method name) to answer with the error.
	Refuse map[string]error
	// RefuseOnce refuses the next call of the name with the error, then forgets it: an API
	// that is not serving yet, a write that fails once.
	RefuseOnce map[string]error

	// Secrets by resource name (projects/P/secrets/N); Versions by resource name, oldest
	// first; Payloads by version name; SecretPolicies by resource name.
	Secrets        map[string]*root.Secret
	Versions       map[string][]root.SecretVersion
	Payloads       map[string][]byte
	SecretPolicies map[string][]org.Binding

	// ZoneMap holds the zones by project then name; Records by "project/zone".
	ZoneMap map[string]map[string]root.Zone
	// Deletions records each zone and project deletion in order ("zone project/name",
	// "project id"), so a test sees the zones go ahead of the project.
	Deletions []string
	// QuotaProjects records the quota project each opening of the creation seam named
	// (empty for none), in order.
	QuotaProjects []string
	Records       map[string][]root.RecordSet

	// Connections by resource name; Links by resource name to the remote URI; Triggers
	// by "project/location/name"; ConnectionPolicies by resource name.
	Connections        map[string]root.Connection
	Links              map[string]string
	Triggers           map[string]*root.Trigger
	ConnectionPolicies map[string][]org.Binding

	// Roles by "project/id"; Policies by resource (projects/P, folders/F,
	// organizations/O, billingAccounts/B, serviceAccounts/E); BucketPolicies by bucket;
	// Enabled by project; BillingOf by project.
	Roles          map[string]root.Role
	Policies       map[string][]org.Binding
	BucketPolicies map[string][]root.ConditionedBinding
	Enabled        map[string][]string
	BillingOf      map[string]string

	// Locked by "bucket/object" to the lock's data.
	// OrgPolicies by resource name (folders/F/policies/C).
	OrgPolicies map[string]*root.Policy

	Locked map[string][]byte
	// Objects are the store's objects (the markers), by bucket/object.
	Objects map[string][]byte

	// The creation seam's memory (create.Google): Folders by "parent|name" to id,
	// Projects, Numbers, Accounts (by email) and Buckets by name.
	Folders  map[string]string
	Projects map[string]bool
	// ProjectFolders is each project's folder (numeric id), as CreateProject was told.
	ProjectFolders map[string]string
	Numbers        map[string]string
	Accounts       map[string]bool
	Buckets        map[string]bool
	// SpendBudgets are the billing accounts' budgets (budgets.Client, which the creation
	// seam shares), by billing account id; BudgetIdentities the identities the Budgets
	// API was opened as.
	SpendBudgets     map[string][]org.Budget
	BudgetIdentities []string
	// Datasets are BigQuery's datasets by "project/id" to location; DatasetTables the
	// tables by "project/dataset", and Landed the tables (root.ExportTables) Tables
	// answers, written now, for a dataset that stands and that DatasetTables names none
	// for: the billing export as a person pointed it at the dataset the apply made.
	// Standing sets Landed; a test of the export check clears it, or names stale tables.
	Datasets      map[string]string
	DatasetTables map[string][]root.TableInfo
	Landed        []string
	next          int64

	// The verification seam's memory (root.Verifier), keyed "identity|domain": Unowned
	// says the identity is not an owner; Tokens is its token (made up when absent);
	// Unseen says Google does not see the token, so Verify answers false; Verified is
	// what Verify made. Verifiers lists the identities opened, DNSIdentities the ones
	// Cloud DNS was opened as.
	Unowned       map[string]bool
	Tokens        map[string]string
	Unseen        map[string]bool
	Verified      map[string]bool
	Verifiers     []string
	DNSIdentities []string
}

// New makes an empty stand-in.
func New() *Fake {
	return &Fake{
		Refuse: map[string]error{}, RefuseOnce: map[string]error{},
		Secrets: map[string]*root.Secret{}, Versions: map[string][]root.SecretVersion{}, Payloads: map[string][]byte{}, SecretPolicies: map[string][]org.Binding{},
		ZoneMap: map[string]map[string]root.Zone{}, Records: map[string][]root.RecordSet{},
		Folders: map[string]string{}, Projects: map[string]bool{}, Numbers: map[string]string{}, Accounts: map[string]bool{}, Buckets: map[string]bool{}, SpendBudgets: map[string][]org.Budget{},
		Datasets: map[string]string{}, DatasetTables: map[string][]root.TableInfo{},
		Connections: map[string]root.Connection{}, Links: map[string]string{}, Triggers: map[string]*root.Trigger{}, ConnectionPolicies: map[string][]org.Binding{},
		Roles: map[string]root.Role{}, Policies: map[string][]org.Binding{}, BucketPolicies: map[string][]root.ConditionedBinding{}, Enabled: map[string][]string{}, BillingOf: map[string]string{},
		OrgPolicies: map[string]*root.Policy{},
		Locked:      map[string][]byte{}, Objects: map[string][]byte{},
		Unowned: map[string]bool{}, Tokens: map[string]string{}, Unseen: map[string]bool{}, Verified: map[string]bool{},
	}
}

// Clients wires the stand-in into every seam of the engine's clients it stands in for.
func (f *Fake) Clients(c *root.Clients) {
	c.Google = func(_ context.Context, quotaProject string) (create.Google, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.QuotaProjects = append(f.QuotaProjects, quotaProject)

		return f, nil
	}
	c.OrgPolicies = func(context.Context, string) (root.OrgPolicies, error) { return f, nil }
	c.Secrets = func(context.Context) (root.Secrets, error) { return f, nil }
	c.DNS = func(context.Context) (root.DNS, error) { return f, nil }
	c.DNSAs = func(_ context.Context, identity string) (root.DNS, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.DNSIdentities = append(f.DNSIdentities, identity)

		return f, nil
	}
	c.Verifier = func(_ context.Context, identity string) (root.Verifier, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.Verifiers = append(f.Verifiers, identity)

		return &verifier{f: f, identity: identity}, nil
	}
	c.Builds = func(context.Context) (root.Builds, error) { return f, nil }
	c.IAM = func(context.Context) (root.IAM, error) { return f, nil }
	c.Locks = func(context.Context) (root.Locks, error) { return f, nil }
	c.Budgets = func(context.Context, string) (budgets.Client, error) { return f, nil }
	c.BudgetsAs = func(_ context.Context, identity string) (budgets.Client, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.BudgetIdentities = append(f.BudgetIdentities, identity)

		return f, nil
	}
	c.Datasets = func(context.Context) (root.Datasets, error) { return f, nil }
	c.Store = func(context.Context) (deploy.Store, error) { return f, nil }
}

// --- Budgets ---

// Budgets answers the billing account's budgets from memory.
func (f *Fake) Budgets(_ context.Context, billingAccount string) ([]org.Budget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("Budgets", billingAccount); err != nil {
		return nil, err
	}
	out := make([]org.Budget, 0, len(f.SpendBudgets[billingAccount]))
	for _, b := range f.SpendBudgets[billingAccount] {
		c := b
		c.Ancestors, c.Thresholds = slices.Clone(b.Ancestors), slices.Clone(b.Thresholds)
		out = append(out, c)
	}

	return out, nil
}

// CreateBudget remembers the budget on the billing account and names it.
func (f *Fake) CreateBudget(_ context.Context, billingAccount string, b *org.Budget) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateBudget", billingAccount, b.DisplayName); err != nil {
		return err
	}
	b.Name = fmt.Sprintf("billingAccounts/%s/budgets/%d", billingAccount, len(f.SpendBudgets[billingAccount])+1)
	b.CurrencyCode = "USD"
	f.SpendBudgets[billingAccount] = append(f.SpendBudgets[billingAccount], *b)

	return nil
}

// UpdateBudget replaces the budget by its name.
func (f *Fake) UpdateBudget(_ context.Context, b *org.Budget) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("UpdateBudget", b.Name); err != nil {
		return err
	}
	for account, budgets := range f.SpendBudgets {
		for i := range budgets {
			if budgets[i].Name == b.Name {
				c := *b
				c.CurrencyCode = budgets[i].CurrencyCode
				f.SpendBudgets[account][i] = c

				return nil
			}
		}
	}

	return errors.Wrap(status.Error(codes.NotFound, b.Name+" does not exist"), "billingbudgets.BillingAccounts.Budgets.Patch()")
}

// --- BigQuery ---

// Dataset is whether the dataset is there, and its location.
func (f *Fake) Dataset(_ context.Context, project, id string) (location string, exists bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("Dataset", project, id); err != nil {
		return "", false, err
	}
	location, exists = f.Datasets[project+"/"+id]

	return location, exists, nil
}

// CreateDataset remembers the dataset in its location.
func (f *Fake) CreateDataset(_ context.Context, project, id, location string, _ map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateDataset", project, id, location); err != nil {
		return err
	}
	if _, ok := f.Datasets[project+"/"+id]; ok {
		return errors.Wrap(status.Error(codes.AlreadyExists, project+"/"+id+" exists"), "bigquery.DatasetsService.Insert()")
	}
	f.Datasets[project+"/"+id] = location

	return nil
}

// Tables answers the dataset's tables from memory.
func (f *Fake) Tables(_ context.Context, project, dataset string) ([]root.TableInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("Tables", project, dataset); err != nil {
		return nil, err
	}
	key := project + "/" + dataset
	if tables, ok := f.DatasetTables[key]; ok {
		return slices.Clone(tables), nil
	}
	if _, exists := f.Datasets[key]; !exists {
		return nil, nil
	}
	tables := make([]root.TableInfo, 0, len(f.Landed))
	for _, id := range f.Landed {
		tables = append(tables, root.TableInfo{ID: id, LastModified: time.Now()})
	}

	return tables, nil
}

func (f *Fake) call(name string, args ...string) error {
	f.Calls = append(f.Calls, name+"("+strings.Join(args, ", ")+")")
	if err, ok := f.RefuseOnce[name]; ok {
		delete(f.RefuseOnce, name)

		return err
	}
	if err, ok := f.Refuse[name]; ok {
		return err
	}

	return nil
}

// Close releases nothing.
func (*Fake) Close() error {
	return nil
}

// --- Secrets ---

// Secret is the secret, or nil when there is none.
func (f *Fake) Secret(_ context.Context, name string) (*root.Secret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("Secret", name); err != nil {
		return nil, err
	}
	s, ok := f.Secrets[name]
	if !ok {
		return nil, nil
	}
	copied := &root.Secret{Labels: s.Labels, Aliases: map[string]string{}}
	for k, v := range s.Aliases {
		copied.Aliases[k] = v
	}

	return copied, nil
}

// CreateSecret makes the secret, with no versions.
func (f *Fake) CreateSecret(_ context.Context, project, id string, labels map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateSecret", project, id); err != nil {
		return err
	}
	f.Secrets["projects/"+project+"/secrets/"+id] = &root.Secret{Labels: labels, Aliases: map[string]string{}}

	return nil
}

// SetSecretAliases sets the secret's aliases whole.
func (f *Fake) SetSecretAliases(_ context.Context, name string, aliases map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("SetSecretAliases", name, fmt.Sprint(aliases)); err != nil {
		return err
	}
	s, ok := f.Secrets[name]
	if !ok {
		return errors.Newf("no secret %s", name)
	}
	s.Aliases = aliases

	return nil
}

// AddSecretVersion adds the version, numbered after the ones there, enabled.
func (f *Fake) AddSecretVersion(_ context.Context, name string, payload []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("AddSecretVersion", name); err != nil {
		return "", err
	}
	if _, ok := f.Secrets[name]; !ok {
		return "", errors.Newf("no secret %s", name)
	}
	version := strconv.Itoa(len(f.Versions[name]) + 1)
	f.Versions[name] = append(f.Versions[name], root.SecretVersion{Version: version, State: "ENABLED"})
	f.Payloads[name+"/versions/"+version] = payload

	return version, nil
}

// SecretVersions lists the secret's versions, newest first.
func (f *Fake) SecretVersions(_ context.Context, name string) ([]root.SecretVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("SecretVersions", name); err != nil {
		return nil, err
	}
	versions := slices.Clone(f.Versions[name])
	slices.Reverse(versions)

	return versions, nil
}

// AccessSecretVersion answers the payload; an alias names the version the secret's
// aliases point at.
func (f *Fake) AccessSecretVersion(_ context.Context, version string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("AccessSecretVersion", version); err != nil {
		return nil, err
	}
	name, v, _ := strings.Cut(version, "/versions/")
	if s, ok := f.Secrets[name]; ok {
		if number, isAlias := s.Aliases[v]; isAlias {
			v = number
		}
	}
	payload, ok := f.Payloads[name+"/versions/"+v]
	if !ok {
		return nil, errors.Newf("no version %s", version)
	}

	return payload, nil
}

// SecretPolicy is the secret's bindings.
func (f *Fake) SecretPolicy(_ context.Context, name string) ([]org.Binding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("SecretPolicy", name); err != nil {
		return nil, err
	}

	return cloneBindings(f.SecretPolicies[name]), nil
}

// SetSecretPolicy sets the secret's bindings whole.
func (f *Fake) SetSecretPolicy(_ context.Context, name string, bindings []org.Binding) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("SetSecretPolicy", name); err != nil {
		return err
	}
	f.SecretPolicies[name] = cloneBindings(bindings)

	return nil
}

func cloneBindings(bs []org.Binding) []org.Binding {
	out := make([]org.Binding, 0, len(bs))
	for _, b := range bs {
		members := slices.Clone(b.Members)
		sort.Strings(members)
		out = append(out, org.Binding{Role: b.Role, Members: members})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Role < out[j].Role })

	return out
}

// --- DNS ---

// Zones lists the project's zones.
func (f *Fake) Zones(_ context.Context, project string) ([]root.Zone, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("Zones", project); err != nil {
		return nil, err
	}
	var zones []root.Zone
	for _, z := range f.ZoneMap[project] {
		zones = append(zones, z)
	}
	sort.Slice(zones, func(i, j int) bool { return zones[i].Name < zones[j].Name })

	return zones, nil
}

// PublicZone is the creation seam's read of a zone: the name servers of the zone in the
// project when it exists.
func (f *Fake) PublicZone(_ context.Context, project, name string) (nameServers []string, exists bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("PublicZone", project, name); err != nil {
		return nil, false, err
	}
	zone, ok := f.ZoneMap[project][name]

	return zone.NameServers, ok, nil
}

// CreatePublicZone is the creation seam's zone: made as CreateZone makes the root's.
func (f *Fake) CreatePublicZone(ctx context.Context, project, name, dnsName, description string, labels map[string]string) ([]string, error) {
	if err := f.CreateZone(ctx, project, root.Zone{Name: name, DNSName: dnsName, Description: description, Labels: labels, DNSSEC: true}); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.ZoneMap[project][name].NameServers, nil
}

// CreateZone makes the zone, on four name servers of its own.
func (f *Fake) CreateZone(_ context.Context, project string, zone root.Zone) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateZone", project, zone.Name); err != nil {
		return err
	}
	if f.ZoneMap[project] == nil {
		f.ZoneMap[project] = map[string]root.Zone{}
	}
	f.next++
	zone.NameServers = []string{fmt.Sprintf("ns-cloud-%d1.googledomains.com.", f.next), fmt.Sprintf("ns-cloud-%d2.googledomains.com.", f.next), fmt.Sprintf("ns-cloud-%d3.googledomains.com.", f.next), fmt.Sprintf("ns-cloud-%d4.googledomains.com.", f.next)}
	f.ZoneMap[project][zone.Name] = zone
	f.Records[project+"/"+zone.Name] = []root.RecordSet{
		{Name: zone.DNSName, Type: "NS", TTL: 21600, Data: zone.NameServers},
		{Name: zone.DNSName, Type: "SOA", TTL: 21600, Data: []string{zone.NameServers[0] + " cloud-dns-hostmaster.google.com. 1 21600 3600 259200 300"}},
	}

	return nil
}

// DeleteZone deletes the zone and its record sets.
func (f *Fake) DeleteZone(_ context.Context, project, zone string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("DeleteZone", project, zone); err != nil {
		return err
	}
	delete(f.ZoneMap[project], zone)
	delete(f.Records, project+"/"+zone)
	f.Deletions = append(f.Deletions, "zone "+project+"/"+zone)

	return nil
}

// RecordSets lists the zone's record sets.
func (f *Fake) RecordSets(_ context.Context, project, zone string) ([]root.RecordSet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("RecordSets", project, zone); err != nil {
		return nil, err
	}

	return slices.Clone(f.Records[project+"/"+zone]), nil
}

// ChangeRecordSets adds and deletes record sets in the zone.
func (f *Fake) ChangeRecordSets(_ context.Context, project, zone string, additions, deletions []root.RecordSet) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("ChangeRecordSets", project, zone, strconv.Itoa(len(additions))+" additions", strconv.Itoa(len(deletions))+" deletions"); err != nil {
		return err
	}
	key := project + "/" + zone
	records := f.Records[key]
	for _, d := range deletions {
		records = slices.DeleteFunc(records, func(r root.RecordSet) bool { return r.Name == d.Name && r.Type == d.Type })
	}
	records = append(records, additions...)
	f.Records[key] = records

	return nil
}

// --- Builds ---

func connectionKey(project, location, name string) string {
	return "projects/" + project + "/locations/" + location + "/connections/" + name
}

// Connection is the Cloud Build connection, or nil when there is none.
func (f *Fake) Connection(_ context.Context, project, location, name string) (*root.Connection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("Connection", name); err != nil {
		return nil, err
	}
	c, ok := f.Connections[connectionKey(project, location, name)]
	if !ok {
		return nil, nil
	}

	return &c, nil
}

// CreateConnection makes the connection with the installation and the token version.
func (f *Fake) CreateConnection(_ context.Context, project, location, name string, c root.Connection) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateConnection", name, c.InstallationID, c.TokenSecretVersion); err != nil {
		return err
	}
	f.Connections[connectionKey(project, location, name)] = c

	return nil
}

// UpdateConnection moves the connection's token version.
func (f *Fake) UpdateConnection(_ context.Context, project, location, name string, c root.Connection) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("UpdateConnection", name, c.InstallationID, c.TokenSecretVersion); err != nil {
		return err
	}
	f.Connections[connectionKey(project, location, name)] = c

	return nil
}

// RepositoryLink is the remote URI the link names, or empty when there is none.
func (f *Fake) RepositoryLink(_ context.Context, project, location, connection, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("RepositoryLink", name); err != nil {
		return "", err
	}

	return f.Links[connectionKey(project, location, connection)+"/repositories/"+name], nil
}

// CreateRepositoryLink links the repository under the connection.
func (f *Fake) CreateRepositoryLink(_ context.Context, project, location, connection, name, remoteURI string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateRepositoryLink", name, remoteURI); err != nil {
		return err
	}
	f.Links[connectionKey(project, location, connection)+"/repositories/"+name] = remoteURI

	return nil
}

// Trigger is the trigger, or nil when there is none.
func (f *Fake) Trigger(_ context.Context, project, location, name string) (*root.Trigger, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("Trigger", name); err != nil {
		return nil, err
	}
	t, ok := f.Triggers[project+"/"+location+"/"+name]
	if !ok {
		return nil, nil
	}
	copied := *t

	return &copied, nil
}

// CreateTrigger makes the trigger.
func (f *Fake) CreateTrigger(_ context.Context, project, location string, t *root.Trigger) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateTrigger", t.Name); err != nil {
		return err
	}
	f.next++
	copied := *t
	copied.ID = fmt.Sprintf("trigger-%d", f.next)
	f.Triggers[project+"/"+location+"/"+t.Name] = &copied

	return nil
}

// UpdateTrigger replaces the trigger.
func (f *Fake) UpdateTrigger(_ context.Context, project, location string, t *root.Trigger) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("UpdateTrigger", t.Name); err != nil {
		return err
	}
	copied := *t
	f.Triggers[project+"/"+location+"/"+t.Name] = &copied

	return nil
}

// ConnectionPolicy is the connection's bindings.
func (f *Fake) ConnectionPolicy(_ context.Context, project, location, name string) ([]org.Binding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("ConnectionPolicy", name); err != nil {
		return nil, err
	}

	return cloneBindings(f.ConnectionPolicies[connectionKey(project, location, name)]), nil
}

// GrantConnection adds the member to the role on the connection.
func (f *Fake) GrantConnection(_ context.Context, project, location, name, role, member string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("GrantConnection", name, role, member); err != nil {
		return err
	}
	key := connectionKey(project, location, name)
	f.ConnectionPolicies[key] = grant(f.ConnectionPolicies[key], role, member)

	return nil
}

// grant adds the member to the role in the bindings.
func grant(bindings []org.Binding, role, member string) []org.Binding {
	for i := range bindings {
		if bindings[i].Role == role {
			if !slices.Contains(bindings[i].Members, member) {
				bindings[i].Members = append(bindings[i].Members, member)
			}

			return bindings
		}
	}

	return append(bindings, org.Binding{Role: role, Members: []string{member}})
}

// revoke takes the member out of the roles.
func revoke(bindings []org.Binding, member string, roles []string) []org.Binding {
	var kept []org.Binding
	for _, b := range bindings {
		if slices.Contains(roles, b.Role) {
			b.Members = slices.DeleteFunc(slices.Clone(b.Members), func(m string) bool { return m == member })
		}
		if len(b.Members) > 0 {
			kept = append(kept, b)
		}
	}

	return kept
}

// --- IAM ---

// Role is the custom role, or nil when there is none.
func (f *Fake) Role(_ context.Context, project, id string) (*root.Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("Role", project, id); err != nil {
		return nil, err
	}
	r, ok := f.Roles[project+"/"+id]
	if !ok {
		return nil, nil
	}

	return &r, nil
}

// CreateRole makes the custom role.
func (f *Fake) CreateRole(_ context.Context, project, id string, role root.Role) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateRole", project, id); err != nil {
		return err
	}
	f.Roles[project+"/"+id] = role

	return nil
}

// UpdateRole replaces the custom role's permissions.
func (f *Fake) UpdateRole(_ context.Context, project, id string, role root.Role) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("UpdateRole", project, id); err != nil {
		return err
	}
	f.Roles[project+"/"+id] = role

	return nil
}

func (f *Fake) policy(name, resource string) ([]org.Binding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call(name, resource); err != nil {
		return nil, err
	}

	return cloneBindings(f.Policies[resource]), nil
}

// ProjectPolicy is the project's bindings.
func (f *Fake) ProjectPolicy(_ context.Context, project string) ([]org.Binding, error) {
	return f.policy("ProjectPolicy", "projects/"+project)
}

// FolderPolicy is the folder's bindings.
func (f *Fake) FolderPolicy(_ context.Context, folder string) ([]org.Binding, error) {
	return f.policy("FolderPolicy", "folders/"+folder)
}

// OrganizationPolicy is the organization node's bindings.
func (f *Fake) OrganizationPolicy(_ context.Context, organizationID string) ([]org.Binding, error) {
	return f.policy("OrganizationPolicy", "organizations/"+organizationID)
}

// BillingPolicy is the billing account's bindings.
func (f *Fake) BillingPolicy(_ context.Context, account string) ([]org.Binding, error) {
	return f.policy("BillingPolicy", "billingAccounts/"+account)
}

// ServiceAccountPolicy is the identity's own bindings.
func (f *Fake) ServiceAccountPolicy(_ context.Context, email string) ([]org.Binding, error) {
	return f.policy("ServiceAccountPolicy", "serviceAccounts/"+email)
}

// SeedGrant adds the member to the role on the resource, as the create seam's grants do;
// a test seeds a policy with it.
func (f *Fake) SeedGrant(resource, role, member string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Policies[resource] = grant(f.Policies[resource], role, member)
}

func (f *Fake) grantCall(name, resource, role, member string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call(name, resource, role, member); err != nil {
		return err
	}
	f.Policies[resource] = grant(f.Policies[resource], role, member)

	return nil
}

func (f *Fake) revokeCall(name, resource, member string, roles []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call(name, resource, member, strings.Join(roles, "+")); err != nil {
		return err
	}
	f.Policies[resource] = revoke(f.Policies[resource], member, roles)

	return nil
}

// GrantOrganization adds the member to the roles at the organization node.
func (f *Fake) GrantOrganization(_ context.Context, organizationID, role, member string) error {
	return f.grantCall("GrantOrganization", "organizations/"+organizationID, role, member)
}

// RevokeOrganization takes the member out of the roles at the organization node.
func (f *Fake) RevokeOrganization(_ context.Context, organizationID, member string, roles []string) error {
	return f.revokeCall("RevokeOrganization", "organizations/"+organizationID, member, roles)
}

// GrantBilling adds the member to the roles on the billing account.
func (f *Fake) GrantBilling(_ context.Context, account, role, member string) error {
	return f.grantCall("GrantBilling", "billingAccounts/"+account, role, member)
}

// RevokeBilling takes the member out of the roles on the billing account.
func (f *Fake) RevokeBilling(_ context.Context, account, member string, roles []string) error {
	return f.revokeCall("RevokeBilling", "billingAccounts/"+account, member, roles)
}

// GrantServiceAccount adds the member to the role on the identity's own policy.
func (f *Fake) GrantServiceAccount(_ context.Context, email, role, member string) error {
	return f.grantCall("GrantServiceAccount", "serviceAccounts/"+email, role, member)
}

// RevokeServiceAccount takes the member off the role on the service account.
func (f *Fake) RevokeServiceAccount(_ context.Context, email, role, member string) error {
	return f.revokeCall("RevokeServiceAccount", "serviceAccounts/"+email, member, []string{role})
}

// GrantProject and RevokeProject, GrantFolder and RevokeFolder are the create seam's
// grants over the same policies, so a test wires one stand-in for both.
func (f *Fake) GrantProject(_ context.Context, id, role, member string) error {
	return f.grantCall("GrantProject", "projects/"+id, role, member)
}

// RevokeProject takes the member out of the roles on the project.
func (f *Fake) RevokeProject(_ context.Context, id, member string, roles []string) error {
	return f.revokeCall("RevokeProject", "projects/"+id, member, roles)
}

// GrantFolder adds the member to the role on the folder.
func (f *Fake) GrantFolder(_ context.Context, folder, role, member string) error {
	return f.grantCall("GrantFolder", "folders/"+folder, role, member)
}

// RevokeFolder takes the member out of the roles on the folder.
func (f *Fake) RevokeFolder(_ context.Context, folder, member string, roles []string) error {
	return f.revokeCall("RevokeFolder", "folders/"+folder, member, roles)
}

// BucketBindings is the bucket's bindings, with their conditions.
func (f *Fake) BucketBindings(_ context.Context, bucket string) ([]root.ConditionedBinding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("BucketBindings", bucket); err != nil {
		return nil, err
	}

	return slices.Clone(f.BucketPolicies[bucket]), nil
}

// GrantBucket adds the member to the role on the bucket, under the condition.
func (f *Fake) GrantBucket(_ context.Context, bucket, role, member string, condition *root.Condition) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	title := ""
	if condition != nil {
		title = condition.Title
	}
	if err := f.call("GrantBucket", bucket, role, member, title); err != nil {
		return err
	}
	for i, b := range f.BucketPolicies[bucket] {
		if b.Role == role && ((b.Condition == nil && condition == nil) || (b.Condition != nil && condition != nil && b.Condition.Title == condition.Title)) {
			if !slices.Contains(b.Members, member) {
				f.BucketPolicies[bucket][i].Members = append(f.BucketPolicies[bucket][i].Members, member)
			}

			return nil
		}
	}
	f.BucketPolicies[bucket] = append(f.BucketPolicies[bucket], root.ConditionedBinding{Role: role, Members: []string{member}, Condition: condition})

	return nil
}

// EnabledServices lists the project's enabled APIs.
func (f *Fake) EnabledServices(_ context.Context, project string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("EnabledServices", project); err != nil {
		return nil, err
	}

	return slices.Clone(f.Enabled[project]), nil
}

// EnableServices records the APIs as enabled, as the create seam's does; a test
// wires the stand-in for both.
func (f *Fake) EnableServices(_ context.Context, project string, services []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("EnableServices", project, strconv.Itoa(len(services))+" services"); err != nil {
		return err
	}
	for _, s := range services {
		if !slices.Contains(f.Enabled[project], s) {
			f.Enabled[project] = append(f.Enabled[project], s)
		}
	}
	sort.Strings(f.Enabled[project])

	return nil
}

// ProjectBillingAccount is the billing account the project is linked to.
func (f *Fake) ProjectBillingAccount(_ context.Context, project string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("ProjectBillingAccount", project); err != nil {
		return "", err
	}

	return f.BillingOf[project], nil
}

// DeleteProject deletes the project; a test reads what went off Projects.
func (f *Fake) DeleteProject(_ context.Context, project string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("DeleteProject", project); err != nil {
		return err
	}
	delete(f.Projects, project)
	f.Deletions = append(f.Deletions, "project "+project)

	return nil
}

// DeleteFolder deletes the folder.
func (f *Fake) DeleteFolder(_ context.Context, folder string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("DeleteFolder", folder); err != nil {
		return err
	}
	for key, id := range f.Folders {
		if id == folder {
			delete(f.Folders, key)
		}
	}

	return nil
}

// --- Locks ---

// Lock makes the lock object, or answers ErrLocked with the one there.
func (f *Fake) Lock(_ context.Context, bucket, object string, data []byte) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("Lock", bucket, object); err != nil {
		return nil, err
	}
	key := bucket + "/" + object
	if held, ok := f.Locked[key]; ok {
		return held, root.ErrLocked
	}
	f.Locked[key] = data

	return nil, nil
}

// Unlock removes the lock object.
func (f *Fake) Unlock(_ context.Context, bucket, object string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("Unlock", bucket, object); err != nil {
		return err
	}
	delete(f.Locked, bucket+"/"+object)

	return nil
}

// --- Store (deploy.Store): the markers, by bucket/object ---

// Write stores the object.
func (f *Fake) Write(_ context.Context, bucket, object string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("Write", bucket, object); err != nil {
		return err
	}
	f.Objects[bucket+"/"+object] = data

	return nil
}

// List names the objects under the prefix.
func (f *Fake) List(_ context.Context, bucket, prefix string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var names []string
	for key := range f.Objects {
		if object, ok := strings.CutPrefix(key, bucket+"/"); ok && strings.HasPrefix(object, prefix) {
			names = append(names, object)
		}
	}
	sort.Strings(names)

	return names, nil
}

// Read is the object's content, or os.ErrNotExist.
func (f *Fake) Read(_ context.Context, bucket, object string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.Objects[bucket+"/"+object]
	if !ok {
		return nil, os.ErrNotExist
	}

	return data, nil
}

// Delete removes the object, or answers os.ErrNotExist.
func (f *Fake) Delete(_ context.Context, bucket, object string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.Objects[bucket+"/"+object]; !ok {
		return os.ErrNotExist
	}
	delete(f.Objects, bucket+"/"+object)

	return nil
}

// --- the creation seam (create.Google) ---

// GrantBillingAccount adds the member to the role on the billing account, as the
// creation does for a hosting organization's identities.
func (f *Fake) GrantBillingAccount(_ context.Context, account, role, member string) error {
	return f.grantCall("GrantBillingAccount", "billingAccounts/"+account, role, member)
}

// FindFolder is the folder's id by its parent and name, or empty.
func (f *Fake) FindFolder(_ context.Context, parent, displayName string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("FindFolder", parent, displayName); err != nil {
		return "", err
	}

	return f.Folders[parent+"|"+displayName], nil
}

// CreateFolder makes the folder under the parent.
func (f *Fake) CreateFolder(_ context.Context, parent, displayName string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateFolder", parent, displayName); err != nil {
		return "", err
	}
	f.next++
	id := strconv.FormatInt(900000000000+f.next, 10)
	f.Folders[parent+"|"+displayName] = id

	return id, nil
}

// ProjectExists reports whether the project is there.
func (f *Fake) ProjectExists(_ context.Context, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("ProjectExists", id); err != nil {
		return false, err
	}

	return f.Projects[id], nil
}

// FindProject is the one active project under the folder whose id starts with idPrefix, or
// empty; several are an error, as Google's are told apart no better.
func (f *Fake) FindProject(_ context.Context, folder, idPrefix string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("FindProject", folder, idPrefix); err != nil {
		return "", err
	}
	var found []string
	for id, in := range f.ProjectFolders {
		if in == folder && strings.HasPrefix(id, idPrefix) && f.Projects[id] {
			found = append(found, id)
		}
	}
	sort.Strings(found)
	switch len(found) {
	case 0:
		return "", nil
	case 1:
		return found[0], nil
	default:
		return "", errors.Newf("folder %s holds %d projects named %s<suffix>: %s", folder, len(found), idPrefix, strings.Join(found, ", "))
	}
}

// CreateProject makes the project under the folder.
func (f *Fake) CreateProject(_ context.Context, id, displayName, folder string, _ map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateProject", id, displayName, "folders/"+folder); err != nil {
		return err
	}
	f.Projects[id] = true
	f.Numbers[id] = fmt.Sprintf("1000000%05d", len(f.Projects))
	if f.ProjectFolders == nil {
		f.ProjectFolders = map[string]string{}
	}
	f.ProjectFolders[id] = folder

	return nil
}

// ProjectNumber is the project's number.
func (f *Fake) ProjectNumber(_ context.Context, id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("ProjectNumber", id); err != nil {
		return "", err
	}
	number, ok := f.Numbers[id]
	if !ok {
		return "", errors.Newf("no project %s", id)
	}

	return number, nil
}

// LinkBilling links the project to the billing account.
func (f *Fake) LinkBilling(_ context.Context, id, billingAccount string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("LinkBilling", id, billingAccount); err != nil {
		return err
	}
	f.BillingOf[id] = billingAccount

	return nil
}

// ServiceAccountExists reports whether the identity is there.
func (f *Fake) ServiceAccountExists(_ context.Context, email string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("ServiceAccountExists", email); err != nil {
		return false, err
	}

	return f.Accounts[email], nil
}

// CreateServiceAccount makes the identity in the project.
func (f *Fake) CreateServiceAccount(_ context.Context, project, accountID, displayName, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateServiceAccount", project, accountID, displayName); err != nil {
		return err
	}
	f.Accounts[accountID+"@"+project+".iam.gserviceaccount.com"] = true

	return nil
}

// BucketExists reports whether the bucket is there.
func (f *Fake) BucketExists(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("BucketExists", name); err != nil {
		return false, err
	}

	return f.Buckets[name], nil
}

// CreateBucket makes the bucket in the project.
func (f *Fake) CreateBucket(_ context.Context, project, name, location string, _ map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateBucket", project, name, location); err != nil {
		return err
	}
	f.Buckets[name] = true

	return nil
}

// GrantSecret adds the member to the role on the secret's policy, as the creation
// grants; the engine's SecretPolicy reads it back.
func (f *Fake) GrantSecret(_ context.Context, secret, role, member string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("GrantSecret", secret, role, member); err != nil {
		return err
	}
	f.SecretPolicies[secret] = grant(f.SecretPolicies[secret], role, member)

	return nil
}

// --- Secret Manager as the deploy sequence reads it (deploy.Secrets), over the same memory ---

// Access is the version's payload, by number or alias.
func (f *Fake) Access(ctx context.Context, version string) ([]byte, error) {
	return f.AccessSecretVersion(ctx, version)
}

// State is the version's state.
func (f *Fake) State(_ context.Context, version string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name, v, _ := strings.Cut(version, "/versions/")
	for _, sv := range f.Versions[name] {
		if sv.Version == v {
			return sv.State, nil
		}
	}

	return "", errors.Newf("no version %s", version)
}

// Container reports whether the secret is there.
func (f *Fake) Container(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.Secrets[name]

	return ok, nil
}

// Create makes the secret, as the deploy sequence does.
func (f *Fake) Create(ctx context.Context, project, id string, labels map[string]string) error {
	return f.CreateSecret(ctx, project, id, labels)
}

// Newest is the number of the secret's newest enabled version, or empty.
func (f *Fake) Newest(_ context.Context, container string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	versions := f.Versions[container]
	for i := len(versions) - 1; i >= 0; i-- {
		if versions[i].State == "ENABLED" {
			return versions[i].Version, nil
		}
	}

	return "", nil
}

// Grant adds the member to the role on the secret's own policy.
func (f *Fake) Grant(_ context.Context, container, role, member string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	bindings := f.SecretPolicies[container]
	for i, b := range bindings {
		if b.Role == role {
			if !slices.Contains(b.Members, member) {
				bindings[i].Members = append(bindings[i].Members, member)
			}
			f.SecretPolicies[container] = bindings

			return nil
		}
	}
	f.SecretPolicies[container] = append(bindings, org.Binding{Role: role, Members: []string{member}})

	return nil
}

// Add adds a version with the payload and answers its number.
func (f *Fake) Add(ctx context.Context, container string, payload []byte) (string, error) {
	return f.AddSecretVersion(ctx, container, payload)
}

// Policy is the organization policy by name, when there is one.
func (f *Fake) Policy(_ context.Context, name string) (*root.Policy, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("Policy", name); err != nil {
		return nil, false, err
	}
	p, ok := f.OrgPolicies[name]
	if !ok {
		return nil, false, nil
	}
	c := *p
	c.Rules = slices.Clone(p.Rules)

	return &c, true, nil
}

// CreatePolicy stores the policy under its name; one there already is an error, as the
// API answers.
func (f *Fake) CreatePolicy(_ context.Context, parent string, policy *root.Policy) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreatePolicy", parent, policy.Name); err != nil {
		return err
	}
	if _, ok := f.OrgPolicies[policy.Name]; ok {
		return errors.Wrap(status.Error(codes.AlreadyExists, policy.Name+" exists"), "orgpolicy.Client.CreatePolicy()")
	}
	c := *policy
	f.OrgPolicies[policy.Name] = &c

	return nil
}

// UpdatePolicy replaces the policy under its name; one not there is an error, as the API
// answers.
func (f *Fake) UpdatePolicy(_ context.Context, policy *root.Policy) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("UpdatePolicy", policy.Name); err != nil {
		return err
	}
	if _, ok := f.OrgPolicies[policy.Name]; !ok {
		return errors.Wrap(status.Error(codes.NotFound, policy.Name+" does not exist"), "orgpolicy.Client.UpdatePolicy()")
	}
	c := *policy
	f.OrgPolicies[policy.Name] = &c

	return nil
}

// --- Verifier ---

// verifier is the stand-in's webmaster verification as one identity.
type verifier struct {
	f        *Fake
	identity string
}

func (v *verifier) key(domain string) string {
	return v.identity + "|" + domain
}

// Owned reports the identity an owner unless Unowned says otherwise and Verify has not
// made it one.
func (v *verifier) Owned(_ context.Context, domain string) (bool, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	if err := v.f.call("Owned", v.identity, domain); err != nil {
		return false, err
	}

	return !v.f.Unowned[v.key(domain)] || v.f.Verified[v.key(domain)], nil
}

// Token is the token Tokens holds, or one made from the identity and the domain.
func (v *verifier) Token(_ context.Context, domain string) (string, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	if err := v.f.call("Token", v.identity, domain); err != nil {
		return "", err
	}
	if token, ok := v.f.Tokens[v.key(domain)]; ok {
		return token, nil
	}

	return "google-site-verification=" + strings.ReplaceAll(strings.SplitN(v.identity, "@", 2)[0]+"-"+domain, ".", "-"), nil
}

// Verify makes the identity an owner, unless Unseen says Google does not see the token.
func (v *verifier) Verify(_ context.Context, domain string) (bool, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	if err := v.f.call("Verify", v.identity, domain); err != nil {
		return false, err
	}
	if v.f.Unseen[v.key(domain)] {
		return false, nil
	}
	v.f.Verified[v.key(domain)] = true

	return true, nil
}

// Close releases nothing.
func (*verifier) Close() error {
	return nil
}
