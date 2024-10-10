// resourcestore package provides a store to store permission resource mappings
package resourcestore

import (
	"context"
	"maps"
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
	mu sync.RWMutex

	enforcer accesstypes.Enforcer

	tagStore      map[accesstypes.PermissionScope]tagStore
	resourceStore map[accesstypes.PermissionScope]resourceStore
}

func New(e accesstypes.Enforcer) *Store {
	store := &Store{
		enforcer:      e,
		tagStore:      make(map[accesstypes.PermissionScope]tagStore, 2),
		resourceStore: make(map[accesstypes.PermissionScope]resourceStore, 2),
	}

	return store
}

func (s *Store) AddResourceTags(scope accesstypes.PermissionScope, res accesstypes.Resource, tags accesstypes.TagPermission) error {
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

func (s *Store) ResolvePermissions(ctx context.Context, user accesstypes.User, domains ...accesstypes.Domain) (*accesstypes.ResolvedPermissions, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resolvedTagPermissions, err := resolveTags(ctx, domains, s, user)
	if err != nil {
		return nil, err
	}

	resolvedResourcePermissions, err := resolveResource(ctx, domains, s, user)
	if err != nil {
		return nil, err
	}

	return &accesstypes.ResolvedPermissions{
		Resources: resolvedResourcePermissions,
		Tags:      resolvedTagPermissions,
	}, nil
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
		slices.AppendSeq(resources, maps.Keys(stores))
	}

	for _, stores := range s.tagStore {
		for resource, tags := range stores {
			for tag := range tags {
				resources = append(resources, resource.ResourceWithTag(tag))
			}
		}
	}
	slices.Sort(resources)

	return slices.Compact(resources)
}

func resolveTags(ctx context.Context, domains []accesstypes.Domain, s *Store, user accesstypes.User) (accesstypes.ResolvedTagPermissions, error) {
	resolvedTagPermissions := make(accesstypes.ResolvedTagPermissions, len(domains))
	for _, domain := range domains {
		tagStore := s.tagStore[accesstypes.DomainPermissionScope]
		if domain == accesstypes.GlobalDomain {
			tagStore = s.tagStore[accesstypes.GlobalPermissionScope]
		}
		permissionResources := map[accesstypes.Permission][]accesstypes.Resource{}
		for res, tags := range tagStore {
			for tag, permissions := range tags {
				for _, permission := range permissions {
					permissionResources[permission] = append(permissionResources[permission], res.ResourceWithTag(tag))
				}
			}
		}

		for permission, resources := range permissionResources {
			_, missing, err := s.enforcer.RequireResources(ctx, user, domain, permission, resources...)
			if err != nil {
				return nil, errors.Wrap(err, "accesstypes.Enforcer.RequireResources()")
			}

			for _, missingResource := range missing {
				res, tag := missingResource.ResourceAndTag()
				if resolvedTagPermissions[domain][res][tag] == nil {
					if resolvedTagPermissions[domain][res] == nil {
						if resolvedTagPermissions[domain] == nil {
							resolvedTagPermissions[domain] = make(map[accesstypes.Resource]map[accesstypes.Tag]map[accesstypes.Permission]bool)
						}
						resolvedTagPermissions[domain][res] = make(map[accesstypes.Tag]map[accesstypes.Permission]bool)
					}
					resolvedTagPermissions[domain][res][tag] = make(map[accesstypes.Permission]bool)
				}
				resolvedTagPermissions[domain][res][tag][permission] = false
			}
		}
	}

	return resolvedTagPermissions, nil
}

func resolveResource(ctx context.Context, domains []accesstypes.Domain, s *Store, user accesstypes.User) (accesstypes.ResolvedResourcePermissions, error) {
	resolvedResourcePermissions := make(accesstypes.ResolvedResourcePermissions, 2)
	for _, domain := range domains {
		resourceStore := s.resourceStore[accesstypes.DomainPermissionScope]
		if domain == accesstypes.GlobalDomain {
			resourceStore = s.resourceStore[accesstypes.GlobalPermissionScope]
		}
		permissionResources := map[accesstypes.Permission][]accesstypes.Resource{}
		for res, permissions := range resourceStore {
			for _, permission := range permissions {
				permissionResources[permission] = append(permissionResources[permission], res)
			}
		}
		for permission, resources := range permissionResources {
			_, missing, err := s.enforcer.RequireResources(ctx, user, domain, permission, resources...)
			if err != nil {
				return nil, errors.Wrap(err, "accesstypes.Enforcer.RequireResources()")
			}

			for _, res := range missing {
				if resolvedResourcePermissions[domain][res] == nil {
					if resolvedResourcePermissions[domain] == nil {
						resolvedResourcePermissions[domain] = make(map[accesstypes.Resource]map[accesstypes.Permission]bool)
					}
					resolvedResourcePermissions[domain][res] = make(map[accesstypes.Permission]bool)
				}
				resolvedResourcePermissions[domain][res][permission] = false
			}
		}
	}

	return resolvedResourcePermissions, nil
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
