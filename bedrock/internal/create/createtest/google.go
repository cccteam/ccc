// Package createtest is a stand-in for the creation's Google seam: what the creation
// asks Google Cloud, answered from memory and recorded, so a test can read what a
// creation made and granted, and start from an organization half made.
package createtest

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/create"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// FakeGoogle is the stand-in. Folders are by "parent|name"; Projects, Accounts and
// Buckets by name; Grants are the bindings made, "resource role member", in order;
// Calls every call, in order; Identities the identities the seam was opened as.
type FakeGoogle struct {
	mu       sync.Mutex
	Folders  map[string]string
	Projects map[string]bool
	// ProjectFolders is each project's folder (numeric id), as CreateProject was told.
	ProjectFolders map[string]string
	Numbers        map[string]string
	Accounts       map[string]bool
	Buckets        map[string]bool
	// Zones are the public zones by project|name, each with its name servers.
	Zones   map[string][]string
	Enabled map[string][]string
	Billing map[string]string
	// SpendBudgets are the budgets by billing account.
	SpendBudgets map[string][]org.Budget
	Grants       []string
	Revoked      []string
	Calls        []string
	Identities   []string
	// Refuse names a call (its method name) to answer with the error, for a creation
	// that stops halfway.
	Refuse map[string]error
	next   int
}

// New makes an empty stand-in: no folder, no project.
func New() *FakeGoogle {
	return &FakeGoogle{
		Folders: map[string]string{}, Projects: map[string]bool{}, Numbers: map[string]string{}, Accounts: map[string]bool{},
		Buckets: map[string]bool{}, Zones: map[string][]string{}, Enabled: map[string][]string{}, Billing: map[string]string{}, SpendBudgets: map[string][]org.Budget{}, Refuse: map[string]error{},
	}
}

// Budgets answers the billing account's budgets from memory.
func (g *FakeGoogle) Budgets(_ context.Context, billingAccount string) ([]org.Budget, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("Budgets", billingAccount); err != nil {
		return nil, err
	}

	return slices.Clone(g.SpendBudgets[billingAccount]), nil
}

// CreateBudget remembers the budget on the billing account, named after its place.
func (g *FakeGoogle) CreateBudget(_ context.Context, billingAccount string, b *org.Budget) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("CreateBudget", billingAccount, b.DisplayName); err != nil {
		return err
	}
	b.Name = fmt.Sprintf("billingAccounts/%s/budgets/%d", billingAccount, len(g.SpendBudgets[billingAccount])+1)
	b.CurrencyCode = "USD"
	g.SpendBudgets[billingAccount] = append(g.SpendBudgets[billingAccount], *b)

	return nil
}

// ZoneNameServers are the name servers the stand-in assigns every zone it makes.
var ZoneNameServers = []string{"ns-cloud-z1.googledomains.com.", "ns-cloud-z2.googledomains.com.", "ns-cloud-z3.googledomains.com.", "ns-cloud-z4.googledomains.com."}

// PublicZone answers the zone's name servers when it exists.
func (g *FakeGoogle) PublicZone(_ context.Context, project, name string) (nameServers []string, exists bool, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("PublicZone", project, name); err != nil {
		return nil, false, err
	}
	servers, ok := g.Zones[project+"|"+name]

	return servers, ok, nil
}

// CreatePublicZone records the zone and assigns it the stand-in's name servers.
func (g *FakeGoogle) CreatePublicZone(_ context.Context, project, name, dnsName, _ string, _ map[string]string) ([]string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("CreatePublicZone", project, name, dnsName); err != nil {
		return nil, err
	}
	g.Zones[project+"|"+name] = ZoneNameServers

	return ZoneNameServers, nil
}

// Open is the seam's opener: it records the identity and answers the stand-in.
func (g *FakeGoogle) Open(_ context.Context, identity string) (create.Google, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Identities = append(g.Identities, identity)

	return g, nil
}

func (g *FakeGoogle) call(name string, args ...string) error {
	g.Calls = append(g.Calls, name+"("+strings.Join(args, ", ")+")")
	if err, ok := g.Refuse[name]; ok {
		return err
	}

	return nil
}

// FindFolder answers the folder's id from memory, empty when none was made.
func (g *FakeGoogle) FindFolder(_ context.Context, parent, displayName string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("FindFolder", parent, displayName); err != nil {
		return "", err
	}

	return g.Folders[parent+"|"+displayName], nil
}

// CreateFolder makes the folder with the next numeric id.
func (g *FakeGoogle) CreateFolder(_ context.Context, parent, displayName string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("CreateFolder", parent, displayName); err != nil {
		return "", err
	}
	g.next++
	id := strconv.Itoa(900000000000 + g.next)
	g.Folders[parent+"|"+displayName] = id

	return id, nil
}

// GrantFolder records the binding.
func (g *FakeGoogle) GrantFolder(_ context.Context, folder, role, member string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("GrantFolder", folder, role, member); err != nil {
		return err
	}
	g.Grants = append(g.Grants, "folders/"+folder+" "+role+" "+member)

	return nil
}

// RevokeFolder records the removal.
func (g *FakeGoogle) RevokeFolder(_ context.Context, folder, member string, roles []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("RevokeFolder", folder, member, strings.Join(roles, "+")); err != nil {
		return err
	}
	g.Revoked = append(g.Revoked, "folders/"+folder+" "+member+" "+strings.Join(roles, "+"))

	return nil
}

// ProjectExists answers from memory.
func (g *FakeGoogle) ProjectExists(_ context.Context, id string) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("ProjectExists", id); err != nil {
		return false, err
	}

	return g.Projects[id], nil
}

// FindProject is the one active project under the folder whose id starts with idPrefix, or
// empty; several are an error, as Google's are told apart no better.
func (g *FakeGoogle) FindProject(_ context.Context, folder, idPrefix string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("FindProject", folder, idPrefix); err != nil {
		return "", err
	}
	var found []string
	for id, in := range g.ProjectFolders {
		if in == folder && strings.HasPrefix(id, idPrefix) && g.Projects[id] {
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

// CreateProject makes the project, numbered in order of creation.
func (g *FakeGoogle) CreateProject(_ context.Context, id, displayName, folder string, labels map[string]string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	labeled := make([]string, 0, len(keys))
	for _, k := range keys {
		labeled = append(labeled, k+"="+labels[k])
	}
	if err := g.call("CreateProject", id, displayName, "folders/"+folder, strings.Join(labeled, ",")); err != nil {
		return err
	}
	g.Projects[id] = true
	g.Numbers[id] = fmt.Sprintf("1000000%05d", len(g.Projects))
	if g.ProjectFolders == nil {
		g.ProjectFolders = map[string]string{}
	}
	g.ProjectFolders[id] = folder

	return nil
}

// ProjectNumber answers the project's number, or an error for a project never made.
func (g *FakeGoogle) ProjectNumber(_ context.Context, id string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("ProjectNumber", id); err != nil {
		return "", err
	}
	number, ok := g.Numbers[id]
	if !ok {
		return "", errors.Newf("no project %s", id)
	}

	return number, nil
}

// GrantProject records the binding.
func (g *FakeGoogle) GrantProject(_ context.Context, id, role, member string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("GrantProject", id, role, member); err != nil {
		return err
	}
	g.Grants = append(g.Grants, "projects/"+id+" "+role+" "+member)

	return nil
}

// RevokeProject records the removal.
func (g *FakeGoogle) RevokeProject(_ context.Context, id, member string, roles []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("RevokeProject", id, member, strings.Join(roles, "+")); err != nil {
		return err
	}
	g.Revoked = append(g.Revoked, "projects/"+id+" "+member+" "+strings.Join(roles, "+"))

	return nil
}

// GrantBillingAccount records the grant on the billing account.
func (g *FakeGoogle) GrantBillingAccount(_ context.Context, billingAccount, role, member string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("GrantBillingAccount", billingAccount, role, member); err != nil {
		return err
	}
	g.Grants = append(g.Grants, "billingAccounts/"+billingAccount+" "+role+" "+member)

	return nil
}

// LinkBilling records the link.
func (g *FakeGoogle) LinkBilling(_ context.Context, id, billingAccount string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("LinkBilling", id, billingAccount); err != nil {
		return err
	}
	g.Billing[id] = billingAccount

	return nil
}

// EnableServices records the APIs.
func (g *FakeGoogle) EnableServices(_ context.Context, id string, services []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("EnableServices", id, strconv.Itoa(len(services))+" services"); err != nil {
		return err
	}
	g.Enabled[id] = services

	return nil
}

// ServiceAccountExists answers from memory.
func (g *FakeGoogle) ServiceAccountExists(_ context.Context, email string) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("ServiceAccountExists", email); err != nil {
		return false, err
	}

	return g.Accounts[email], nil
}

// CreateServiceAccount makes the account.
func (g *FakeGoogle) CreateServiceAccount(_ context.Context, project, accountID, displayName, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("CreateServiceAccount", project, accountID, displayName); err != nil {
		return err
	}
	g.Accounts[accountID+"@"+project+".iam.gserviceaccount.com"] = true

	return nil
}

// BucketExists answers from memory.
func (g *FakeGoogle) BucketExists(_ context.Context, name string) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("BucketExists", name); err != nil {
		return false, err
	}

	return g.Buckets[name], nil
}

// CreateBucket makes the bucket.
func (g *FakeGoogle) CreateBucket(_ context.Context, project, name, location string, _ map[string]string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("CreateBucket", project, name, location); err != nil {
		return err
	}
	g.Buckets[name] = true

	return nil
}

// GrantSecret records the binding.
func (g *FakeGoogle) GrantSecret(_ context.Context, secret, role, member string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.call("GrantSecret", secret, role, member); err != nil {
		return err
	}
	g.Grants = append(g.Grants, secret+" "+role+" "+member)

	return nil
}

// Close releases nothing.
func (*FakeGoogle) Close() error {
	return nil
}

// Made lists what the stand-in holds: folders, projects, accounts and buckets, sorted.
func (g *FakeGoogle) Made() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	made := make([]string, 0, len(g.Folders)+len(g.Projects)+len(g.Accounts)+len(g.Buckets))
	for key, id := range g.Folders {
		made = append(made, "folder "+key+"="+id)
	}
	for id := range g.Projects {
		made = append(made, "project "+id+" ("+g.Numbers[id]+", billing "+g.Billing[id]+", "+strconv.Itoa(len(g.Enabled[id]))+" APIs)")
	}
	for email := range g.Accounts {
		made = append(made, "account "+email)
	}
	for key := range g.Zones {
		made = append(made, "zone "+key)
	}
	for name := range g.Buckets {
		made = append(made, "bucket "+name)
	}
	for account, budgets := range g.SpendBudgets {
		for _, b := range budgets {
			made = append(made, fmt.Sprintf("budget %s|%s=%d %s", account, b.DisplayName, b.Units, strings.Join(b.Ancestors, ",")))
		}
	}
	sort.Strings(made)

	return made
}
