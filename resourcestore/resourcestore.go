// resourcestore package provides a store to store permission resource mappings
package resourcestore

import (
	"fmt"
	"slices"
	"sync"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type (
	tagStore      map[accesstypes.Resource]map[accesstypes.Tag][]accesstypes.Permission
	resourceStore map[accesstypes.Resource][]accesstypes.Permission
)

type Store struct {
	mu            sync.RWMutex
	tagStore      map[accesstypes.PermissionScope]tagStore
	resourceStore map[accesstypes.PermissionScope]resourceStore
}

func New() *Store {
	if !generate {
		return &Store{}
	}

	return &Store{
		tagStore:      make(map[accesstypes.PermissionScope]tagStore, 2),
		resourceStore: make(map[accesstypes.PermissionScope]resourceStore, 2),
	}
}

func (s *Store) AddResourceTags(scope accesstypes.PermissionScope, res accesstypes.Resource, tags accesstypes.TagPermission) error {
	if !generate {
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

	for tag, permission := range tags {
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

	return nil
}

func (s *Store) AddResource(scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) error {
	if !generate {
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

func (s *Store) resources() map[string]accesstypes.Resource {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resources := make(map[string]accesstypes.Resource)
	for _, stores := range s.resourceStore {
		for resource := range stores {
			resources[string(resource)] = resource
		}
	}

	for _, stores := range s.tagStore {
		for resource, tags := range stores {
			for tag := range tags {
				resources[fmt.Sprintf("%s_%s", resource, tag)] = resource.ResourceWithTag(tag)
			}
		}
	}

	return resources
}

func (s *Store) permissionResources() map[string]map[accesstypes.Permission]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mapping := map[string]map[accesstypes.Permission]bool{}
	perms := make(map[accesstypes.Permission]struct{})
	enums := make(map[string]struct{})
	for _, store := range s.resourceStore {
		for resource, permissions := range store {
			enum := string(resource)
			enums[enum] = struct{}{}
			for _, perm := range permissions {
				perms[perm] = struct{}{}
				if mapping[enum] == nil {
					mapping[enum] = make(map[accesstypes.Permission]bool)
				}
				mapping[enum][perm] = true
			}
		}
	}
	for _, store := range s.tagStore {
		for resource, tagmap := range store {
			for tag, permissions := range tagmap {
				enum := fmt.Sprintf("%s_%s", resource, tag)
				enums[enum] = struct{}{}
				for _, perm := range permissions {
					perms[perm] = struct{}{}
					if mapping[enum] == nil {
						mapping[enum] = make(map[accesstypes.Permission]bool)
					}
					mapping[enum][perm] = true
				}
			}
		}
	}
	for enum := range enums {
		for perm := range perms {
			if _, ok := mapping[enum][perm]; !ok {
				if mapping[enum] == nil {
					mapping[enum] = make(map[accesstypes.Permission]bool)
				}
				mapping[enum][perm] = false
			}
		}
	}

	return mapping
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
