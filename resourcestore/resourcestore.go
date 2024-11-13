// resourcestore package provides a store to store permission resource mappings
package resourcestore

import (
	"slices"
	"sync"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type (
	tagStore      map[accesstypes.Resource]map[accesstypes.Tag][]accesstypes.Permission
	resourceStore map[accesstypes.Resource][]accesstypes.Permission
	permissionMap map[accesstypes.Resource]map[accesstypes.Permission]bool
)

type Store struct {
	mu            sync.RWMutex
	tagStore      map[accesstypes.PermissionScope]tagStore
	resourceStore map[accesstypes.PermissionScope]resourceStore
}

func New() *Store {
	if !collectResourcePermissions {
		return &Store{}
	}

	return &Store{
		tagStore:      make(map[accesstypes.PermissionScope]tagStore, 2),
		resourceStore: make(map[accesstypes.PermissionScope]resourceStore, 2),
	}
}

func (s *Store) AddResourceTags(scope accesstypes.PermissionScope, res accesstypes.Resource, tags accesstypes.TagPermissions) error {
	if !collectResourcePermissions {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

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

	return nil
}

func (s *Store) AddResource(scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) error {
	if permission == accesstypes.NullPermission {
		return errors.New("cannot register null permission")
	}

	if !collectResourcePermissions {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if ok := slices.Contains(s.resourceStore[scope][res], permission); ok {
		return errors.Newf("found existing entry under resource: %s and permission: %s", res, permission)
	}

	if s.resourceStore[scope] == nil {
		s.resourceStore[scope] = resourceStore{}
	}

	s.resourceStore[scope][res] = append(s.resourceStore[scope][res], permission)

	return nil
}

func (s *Store) permissions() []accesstypes.Permission {
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

func (s *Store) resources() []accesstypes.Resource {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resources := []accesstypes.Resource{}
	for _, stores := range s.resourceStore {
		for resource := range stores {
			resources = append(resources, resource)
		}
	}

	slices.Sort(resources)

	return slices.Compact(resources)
}

func (s *Store) tags() map[accesstypes.Resource][]accesstypes.Tag {
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

func (s *Store) resourcePermissions() permissionMap {
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
			resources[resource] = struct{}{}
			setRequiredPerms(resource, permissions)
		}
	}

	for _, store := range s.tagStore {
		for resource, tagmap := range store {
			for tag, permissions := range tagmap {
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

func (s *Store) List() map[accesstypes.Permission][]accesstypes.Resource {
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

func (s *Store) Scope(resource accesstypes.Resource) accesstypes.PermissionScope {
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
