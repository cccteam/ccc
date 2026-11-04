// Package foundtest is a stand-in for the founding's Google seam: what the founding
// asks Google Cloud, answered from memory and recorded, so a test can read what a
// founding made and granted, and start from an organization half made.
package foundtest

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/found"
)

// FakeGoogle is the stand-in. Folders are by "parent|name"; Projects, Accounts and
// Buckets by name; Grants are the bindings made, "resource role member", in order;
// Calls every call, in order; Identities the identities the seam was opened as.
type FakeGoogle struct {
	mu         sync.Mutex
	Folders    map[string]string
	Projects   map[string]bool
	Numbers    map[string]string
	Accounts   map[string]bool
	Buckets    map[string]bool
	Enabled    map[string][]string
	Billing    map[string]string
	Grants     []string
	Revoked    []string
	Calls      []string
	Identities []string
	// Refuse names a call (its method name) to answer with the error, for a founding
	// that stops halfway.
	Refuse map[string]error
	next   int
}

// New makes an empty stand-in: no folder, no project.
func New() *FakeGoogle {
	return &FakeGoogle{
		Folders: map[string]string{}, Projects: map[string]bool{}, Numbers: map[string]string{}, Accounts: map[string]bool{},
		Buckets: map[string]bool{}, Enabled: map[string][]string{}, Billing: map[string]string{}, Refuse: map[string]error{},
	}
}

// Open is the seam's opener: it records the identity and answers the stand-in.
func (g *FakeGoogle) Open(_ context.Context, identity string) (found.Google, error) {
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
	for name := range g.Buckets {
		made = append(made, "bucket "+name)
	}
	sort.Strings(made)

	return made
}
