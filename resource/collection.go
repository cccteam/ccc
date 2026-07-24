package resource

import (
	"log"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type (
	tagStore          map[accesstypes.Resource]map[accesstypes.Tag][]accesstypes.Permission
	resourceStore     map[accesstypes.Resource][]accesstypes.Permission
	permissionMap     map[accesstypes.Resource]map[accesstypes.Permission]bool
	immutableFieldMap map[accesstypes.Resource]map[accesstypes.Tag]struct{}
)

// AddResources adds all the resources and permissions from a ResourceSet to the collection.
// It is a no-op if collectResourcePermissions is false.
//
// Deprecated: runtime permission registration is replaced by the generated collection,
// which the Resource Generator emits next to the generated routes. Consume the generated
// Collection() (a resource.GeneratedCollection) instead.
func AddResources[Resource Resourcer](c *Collection, scope accesstypes.PermissionScope, rSet *Set[Resource]) error {
	if !collectResourcePermissions {
		return nil
	}

	logCollectionDeprecationOnce()

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.addResourceSet(scope, rSet.BaseResource(), rSet.Permissions(), rSet.TagPermissions(), rSet.ImmutableFields())
}

// Collection stores information about resources, their permissions, and tags.
// It is used during code generation to create TypeScript definitions and Go handlers.
//
// Its constructor and mutation methods are deprecated in favor of the generated
// collection; it survives as the shared internal store behind GeneratedCollection and
// CollectionBuilder and is deleted when the deprecated API is removed.
type Collection struct {
	mu              sync.RWMutex
	tagStore        map[accesstypes.PermissionScope]tagStore
	resourceStore   map[accesstypes.PermissionScope]resourceStore
	immutableFields map[accesstypes.PermissionScope]immutableFieldMap
	// manualRegistrations records which entries arrived via the deprecated AddResource
	// (hand-written registrations) rather than generated handlers, so migration tooling
	// can name the declarations an app still needs.
	manualRegistrations map[ManualRegistration]struct{}
}

// logCollectionDeprecationOnce emits a single informational message per process
// directing migration off runtime permission registration. It fires only in
// collect_resource_permissions builds (terminal and CI contexts), where multiline
// output is safe.
var logCollectionDeprecationOnce = sync.OnceFunc(func() {
	lines := make([]string, 0, 8)
	lines = append(lines, "DEPRECATED: RUNTIME PERMISSION REGISTRATION", "")
	lines = append(lines, wrapBannerText("resource.Collection registration is replaced by the generated collection, which the Resource Generator emits next to the generated routes:", "")...)
	lines = append(lines, wrapBannerText("  1. Consume the generated Collection() instead of the runtime resource.Collection in deployment/bootstrap tooling.", "     ")...)
	lines = append(lines, wrapBannerText("  2. Remove the collect_resource_permissions build tag.", "     ")...)
	log.Println("INFO:" + deprecationBanner(lines...))
})

// bannerTextWidth is the wrap width for prose inside the deprecation banner; the box
// border sizes itself to the longest line.
const bannerTextWidth = 128

// wrapBannerText wraps text at word boundaries to bannerTextWidth, preserving the
// text's leading indentation on the first line and prefixing continuation lines with
// indent. Words longer than the width are emitted unbroken.
func wrapBannerText(text, indent string) []string {
	leading := text[:len(text)-len(strings.TrimLeft(text, " "))]
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	lines := make([]string, 0, len(words)/8+1)
	prefix := leading
	line := words[0]
	for _, word := range words[1:] {
		if utf8.RuneCountInString(prefix)+utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) > bannerTextWidth {
			lines = append(lines, prefix+line)
			prefix = indent
			line = word

			continue
		}
		line += " " + word
	}

	return append(lines, prefix+line)
}

// deprecationBanner formats migration guidance as a bordered box so it stands out in
// generate and deploy output. Lines render verbatim (pre-wrapped by the caller); empty
// strings become blank in-box lines. The leading newline pushes the box below the log
// prefix.
func deprecationBanner(lines ...string) string {
	width := 0
	for _, line := range lines {
		width = max(width, utf8.RuneCountInString(line))
	}

	var b strings.Builder
	b.WriteString("\n╭")
	b.WriteString(strings.Repeat("─", width+2))
	b.WriteString("╮\n")
	for _, line := range lines {
		b.WriteString("│ ")
		b.WriteString(line)
		b.WriteString(strings.Repeat(" ", width-utf8.RuneCountInString(line)))
		b.WriteString(" │\n")
	}
	b.WriteString("╰")
	b.WriteString(strings.Repeat("─", width+2))
	b.WriteString("╯")

	return b.String()
}

// NewCollection creates and initializes a new Collection.
//
// Deprecated: runtime permission registration is replaced by the generated collection,
// which the Resource Generator emits next to the generated routes. Consume the generated
// Collection() (a resource.GeneratedCollection) instead.
func NewCollection() *Collection {
	if !collectResourcePermissions {
		return &Collection{}
	}

	logCollectionDeprecationOnce()

	return &Collection{
		tagStore:        make(map[accesstypes.PermissionScope]tagStore, 2),
		resourceStore:   make(map[accesstypes.PermissionScope]resourceStore, 2),
		immutableFields: make(map[accesstypes.PermissionScope]immutableFieldMap, 2),
	}
}

// AddResource adds a resource with a specific permission to the collection.
// It is a no-op if collectResourcePermissions is false.
//
// Deprecated: runtime permission registration is replaced by the generated collection.
// Declare manual registrations with the Resource Generator's WithManualResources option
// and consume the generated resource.GeneratedCollection instead.
func (s *Collection) AddResource(scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) error {
	return s.add(true, scope, permission, res)
}

// AddMethodResource adds a resource associated with a method, allowing duplicate permission registrations.
// It is a no-op if collectResourcePermissions is false.
//
// Deprecated: runtime permission registration is replaced by the generated collection,
// which the Resource Generator emits next to the generated routes. Consume the generated
// Collection() (a resource.GeneratedCollection) instead.
func (s *Collection) AddMethodResource(scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) error {
	return s.add(false, scope, permission, res)
}

func (s *Collection) add(allowDuplicateRegistration bool, scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) error {
	if permission == accesstypes.NullPermission {
		return errors.New("cannot register null permission")
	}

	if !collectResourcePermissions {
		return nil
	}

	logCollectionDeprecationOnce()

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.addResource(allowDuplicateRegistration, scope, permission, res); err != nil {
		return err
	}

	// AddResource (duplicate registrations allowed) is the hand-written registration
	// path; AddMethodResource comes from generated RPC handlers.
	if allowDuplicateRegistration {
		s.recordManualRegistration(scope, permission, res)
	}

	return nil
}

func (s *Collection) addResource(allowDuplicateRegistration bool, scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) error {
	if !allowDuplicateRegistration {
		if ok := slices.Contains(s.resourceStore[scope][res], permission); ok {
			return errors.Newf("found existing entry under resource: %s and permission: %s", res, permission)
		}
	}

	if s.resourceStore[scope] == nil {
		s.resourceStore[scope] = resourceStore{}
	}

	s.resourceStore[scope][res] = append(s.resourceStore[scope][res], permission)

	return nil
}

// addResourceSet is the registration core shared by the deprecated runtime path
// (AddResources) and CollectionBuilder, so both apply identical semantics: duplicate
// detection, null-permission filtering, and immutable-field replacement (the last
// registration for a resource wins).
func (s *Collection) addResourceSet(scope accesstypes.PermissionScope, res accesstypes.Resource, perms []accesstypes.Permission, tags accesstypes.TagPermissions, immutableFields map[accesstypes.Tag]struct{}) error {
	for _, perm := range perms {
		if err := s.addResource(false, scope, perm, res); err != nil {
			return err
		}
	}

	if s.tagStore[scope][res] == nil {
		if s.tagStore[scope] == nil {
			s.tagStore[scope] = make(tagStore)
		}

		s.tagStore[scope][res] = make(map[accesstypes.Tag][]accesstypes.Permission, len(tags))
	}

	for tag, tagPermissions := range tags {
		for _, permission := range tagPermissions {
			permissions := s.tagStore[scope][res][tag]
			if slices.Contains(permissions, permission) {
				return errors.Newf("found existing mapping between tag (%s) and permission (%s) under resource (%s)", tag, permission, res)
			}

			if permission != accesstypes.NullPermission {
				s.tagStore[scope][res][tag] = append(permissions, permission)
			} else {
				s.tagStore[scope][res][tag] = permissions
			}
		}
	}

	if _, ok := s.immutableFields[scope]; !ok {
		s.immutableFields[scope] = make(map[accesstypes.Resource]map[accesstypes.Tag]struct{})
	}

	s.immutableFields[scope][res] = immutableFields

	return nil
}

// recordManualRegistration records the provenance of a hand-written AddResource call.
// Callers must hold s.mu.
func (s *Collection) recordManualRegistration(scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) {
	if s.manualRegistrations == nil {
		s.manualRegistrations = make(map[ManualRegistration]struct{})
	}

	s.manualRegistrations[ManualRegistration{Scope: scope, Permission: permission, Resource: res}] = struct{}{}
}

// ManualRegistrations returns, sorted, every registration made through the deprecated
// AddResource (hand-written registrations, as opposed to generated handlers). Migration
// tooling uses it to verify each one is declared to the generator.
func (s *Collection) ManualRegistrations() []ManualRegistration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	registrations := make([]ManualRegistration, 0, len(s.manualRegistrations))
	for registration := range s.manualRegistrations {
		registrations = append(registrations, registration)
	}

	slices.SortFunc(registrations, compareManualRegistrations)

	return registrations
}

func compareManualRegistrations(a, b ManualRegistration) int {
	if a.Scope != b.Scope {
		if a.Scope < b.Scope {
			return -1
		}

		return 1
	}
	if a.Resource != b.Resource {
		if a.Resource < b.Resource {
			return -1
		}

		return 1
	}
	if a.Permission != b.Permission {
		if a.Permission < b.Permission {
			return -1
		}

		return 1
	}

	return 0
}

// IsResourceImmutable checks if a resource is marked as immutable within a given scope.
// It always returns false if collectResourcePermissions is false.
func (s *Collection) IsResourceImmutable(scope accesstypes.PermissionScope, res accesstypes.Resource) bool {
	resource, tag := res.ResourceAndTag()
	_, ok := s.immutableFields[scope][resource][tag]

	return ok
}

func (s *Collection) permissions() []accesstypes.Permission {
	s.mu.RLock()
	defer s.mu.RUnlock()

	permissions := []accesstypes.Permission{}
	for _, stores := range s.resourceStore {
		for _, perms := range stores {
			permissions = append(permissions, perms...)
		}
	}
	for _, stores := range s.tagStore {
		for _, tags := range stores {
			for _, perms := range tags {
				permissions = append(permissions, perms...)
			}
		}
	}
	slices.Sort(permissions)

	return slices.Compact(permissions)
}

func (s *Collection) resourcePermissions() []accesstypes.Permission {
	s.mu.RLock()
	defer s.mu.RUnlock()

	permissions := []accesstypes.Permission{}
	for _, stores := range s.resourceStore {
		for _, perms := range stores {
			permissions = append(permissions, perms...)
		}
	}
	for _, stores := range s.tagStore {
		for _, tags := range stores {
			for _, perms := range tags {
				permissions = append(permissions, perms...)
			}
		}
	}
	slices.Sort(permissions)

	filteredPermissions := permissions[:0]
	for _, perm := range permissions {
		if perm != accesstypes.Execute {
			filteredPermissions = append(filteredPermissions, perm)
		}
	}
	clear(permissions[len(filteredPermissions):])

	return slices.Compact(filteredPermissions)
}

// Resources returns a sorted list of all unique base resource names in the collection.
// It returns an empty slice if collectResourcePermissions is false.
func (s *Collection) Resources() []accesstypes.Resource {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resources := []accesstypes.Resource{}
	for _, stores := range s.resourceStore {
		for resource, permissions := range stores {
			if slices.Contains(permissions, accesstypes.Execute) {
				continue
			}

			resources = append(resources, resource)
		}
	}

	slices.Sort(resources)

	return slices.Compact(resources)
}

// ResourceExists checks if a resource exists in the collection.
// It always returns false if collectResourcePermissions is false.
func (s *Collection) ResourceExists(r accesstypes.Resource) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, stores := range s.resourceStore {
		for resource, permissions := range stores {
			if slices.Contains(permissions, accesstypes.Execute) {
				continue
			}
			if resource == r {
				return true
			}
		}
	}

	return false
}

func (s *Collection) tags() map[accesstypes.Resource][]accesstypes.Tag {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resourcetags := make(map[accesstypes.Resource][]accesstypes.Tag)

	for _, tagStore := range s.tagStore {
		for resource, tags := range tagStore {
			for tag := range tags {
				resourcetags[resource] = append(resourcetags[resource], tag)
				slices.Sort(resourcetags[resource])
			}
		}
	}

	return resourcetags
}

func (s *Collection) resourcePermissionMap() permissionMap {
	s.mu.RLock()
	defer s.mu.RUnlock()

	permMap := make(map[accesstypes.Resource]map[accesstypes.Permission]bool)
	permSet := make(map[accesstypes.Permission]struct{})
	resources := make(map[accesstypes.Resource]struct{})

	setRequiredPerms := func(res accesstypes.Resource, permissions []accesstypes.Permission) {
		permMap[res] = make(map[accesstypes.Permission]bool)
		for _, perm := range permissions {
			permSet[perm] = struct{}{}
			permMap[res][perm] = true
		}
	}

	for _, store := range s.resourceStore {
		for resource, permissions := range store {
			if slices.Contains(permissions, accesstypes.Execute) {
				continue
			}

			resources[resource] = struct{}{}
			setRequiredPerms(resource, permissions)
		}
	}

	for _, store := range s.tagStore {
		for resource, tagmap := range store {
			for tag, permissions := range tagmap {
				if slices.Contains(permissions, accesstypes.Execute) {
					continue
				}

				resources[resource.ResourceWithTag(tag)] = struct{}{}
				setRequiredPerms(resource.ResourceWithTag(tag), permissions)
			}
		}
	}

	for resource := range resources {
		for perm := range permSet {
			if _, ok := permMap[resource][perm]; !ok {
				permMap[resource][perm] = false
			}
		}
	}

	return permMap
}

func (s *Collection) domains() []accesstypes.PermissionScope {
	domains := make([]accesstypes.PermissionScope, 0, len(s.resourceStore))
	for domain := range s.resourceStore {
		domains = append(domains, domain)
	}

	return domains
}

// List returns a map of permissions to the resources that have them.
// It returns an empty map if collectResourcePermissions is false.
func (s *Collection) List() map[accesstypes.Permission][]accesstypes.Resource {
	s.mu.RLock()
	defer s.mu.RUnlock()

	permissionResources := make(map[accesstypes.Permission][]accesstypes.Resource)
	for _, store := range s.resourceStore {
		for resource, permissions := range store {
			for _, permission := range permissions {
				permissionResources[permission] = append(permissionResources[permission], resource)
			}
		}
	}

	for _, store := range s.tagStore {
		for resource, tags := range store {
			for tag, permissions := range tags {
				for _, permission := range permissions {
					permissionResources[permission] = append(permissionResources[permission], resource.ResourceWithTag(tag))
				}
			}
		}
	}

	return permissionResources
}

// Scope returns the permission scope for a given resource.
// It returns an empty scope if the resource is not found or if collectResourcePermissions is false.
func (s *Collection) Scope(resource accesstypes.Resource) accesstypes.PermissionScope {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for scope, store := range s.resourceStore {
		if _, ok := store[resource]; ok {
			return scope
		}
	}

	for scope, store := range s.tagStore {
		r, t := resource.ResourceAndTag()
		if _, ok := store[r][t]; ok {
			return scope
		}
	}

	return ""
}

// TypescriptData returns a struct containing all the data needed for TypeScript code generation.
func (s *Collection) TypescriptData() *TypescriptData {
	return &TypescriptData{
		Permissions:           s.permissions(),
		ResourcePermissions:   s.resourcePermissions(),
		Resources:             s.Resources(),
		ResourceTags:          s.tags(),
		ResourcePermissionMap: s.resourcePermissionMap(),
		Domains:               s.domains(),
	}
}
