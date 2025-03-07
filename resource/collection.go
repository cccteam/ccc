package resource

import (
	"slices"
	"sync"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type (
	tagStore          map[accesstypes.Resource]map[accesstypes.Tag][]accesstypes.Permission
	resourceStore     map[accesstypes.Resource][]accesstypes.Permission
	permissionMap     map[accesstypes.Resource]map[accesstypes.Permission]bool
	immutableFieldMap map[accesstypes.Resource]map[accesstypes.Tag]struct{}
)

func AddResources[Resource Resourcer, Request any](s *Collection, scope accesstypes.PermissionScope, rSet *ResourceSet[Resource, Request]) error {
	if !collectResourcePermissions {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	res := rSet.BaseResource()
	tags := rSet.TagPermissions()

	for _, perm := range rSet.Permissions() {
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

	s.immutableFields[scope][res] = rSet.ImmutableFields()

	return nil
}

type Collection struct {
	mu              sync.RWMutex
	tagStore        map[accesstypes.PermissionScope]tagStore
	resourceStore   map[accesstypes.PermissionScope]resourceStore
	immutableFields map[accesstypes.PermissionScope]immutableFieldMap
}

func NewCollection() *Collection {
	if !collectResourcePermissions {
		return &Collection{}
	}

	return &Collection{
		tagStore:        make(map[accesstypes.PermissionScope]tagStore, 2),
		resourceStore:   make(map[accesstypes.PermissionScope]resourceStore, 2),
		immutableFields: make(map[accesstypes.PermissionScope]immutableFieldMap, 2),
	}
}

func (s *Collection) AddResource(scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) error {
	if permission == accesstypes.NullPermission {
		return errors.New("cannot register null permission")
	}

	if !collectResourcePermissions {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.addResource(true, scope, permission, res)
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

func (c *Collection) TypescriptData() TypescriptData {
	return TypescriptData{
		Permissions:           c.permissions(),
		ResourcePermissions:   c.resourcePermissions(),
		Resources:             c.Resources(),
		ResourceTags:          c.tags(),
		ResourcePermissionMap: c.resourcePermissionMap(),
		Domains:               c.domains(),
	}
}
