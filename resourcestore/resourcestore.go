// resourcestore package provides a store to store permission resource mappings
package resourcestore

import (
	"context"
	"slices"
	"sync"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type (
	fieldStore    map[accesstypes.Resource]map[accesstypes.Field][]accesstypes.Permission
	resourceStore map[accesstypes.Resource][]accesstypes.Permission
)

type Store struct {
	mu sync.RWMutex

	enforcer accesstypes.Enforcer

	fieldStore    map[accesstypes.PermissionScope]fieldStore
	resourceStore map[accesstypes.PermissionScope]resourceStore
}

func New(e accesstypes.Enforcer) *Store {
	store := &Store{
		enforcer:      e,
		fieldStore:    make(map[accesstypes.PermissionScope]fieldStore, 2),
		resourceStore: make(map[accesstypes.PermissionScope]resourceStore, 2),
	}

	return store
}

func (s *Store) AddResourceFields(scope accesstypes.PermissionScope, res accesstypes.Resource, fields accesstypes.FieldPermission) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fieldStore[scope][res] == nil {
		if s.fieldStore[scope] == nil {
			s.fieldStore[scope] = make(fieldStore)
		}

		s.fieldStore[scope][res] = make(map[accesstypes.Field][]accesstypes.Permission, len(fields))
	}

	for field, permission := range fields {
		permissions := s.fieldStore[scope][res][field]
		if slices.Contains(permissions, permission) {
			return errors.Newf("found existing mapping between field (%s) and permission (%s) under resource (%s)", field, permission, res)
		}

		if permission != accesstypes.NullPermission {
			s.fieldStore[scope][res][field] = append(permissions, permission)
		} else {
			s.fieldStore[scope][res][field] = permissions
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

	resolvedFieldPermissions := make(accesstypes.ResolvedFieldPermissions, len(domains))
	for _, domain := range domains {
		fieldStore := s.fieldStore[accesstypes.DomainPermissionScope]
		if domain == accesstypes.GlobalDomain {
			fieldStore = s.fieldStore[accesstypes.GlobalPermissionScope]
		}
		permissionFields := map[accesstypes.Permission][]accesstypes.Resource{}
		for res, fields := range fieldStore {
			for field, permissions := range fields {
				for _, permission := range permissions {
					permissionFields[permission] = append(permissionFields[permission], res.ResourceWithField(field))
				}
			}
		}

		for permission, resources := range permissionFields {
			_, missing, err := s.enforcer.RequireResources(ctx, user, domain, permission, resources...)
			if err != nil {
				return nil, errors.Wrap(err, "accesstypes.Enforcer.RequireResources()")
			}

			for _, missingField := range missing {
				res, field := missingField.ResourceAndField()
				if resolvedFieldPermissions[domain][res][field] == nil {
					if resolvedFieldPermissions[domain][res] == nil {
						if resolvedFieldPermissions[domain] == nil {
							resolvedFieldPermissions[domain] = make(map[accesstypes.Resource]map[accesstypes.Field]map[accesstypes.Permission]bool)
						}
						resolvedFieldPermissions[domain][res] = make(map[accesstypes.Field]map[accesstypes.Permission]bool)
					}
					resolvedFieldPermissions[domain][res][field] = make(map[accesstypes.Permission]bool)
				}
				resolvedFieldPermissions[domain][res][field][permission] = false
			}
		}
	}

	resolvedResourcePermissions := make(accesstypes.ResolvedResourcePermissions, 2)
	for _, domain := range domains {
		resourceStore := s.resourceStore[accesstypes.DomainPermissionScope]
		if domain == accesstypes.GlobalDomain {
			resourceStore = s.resourceStore[accesstypes.GlobalPermissionScope]
		}
		permissionFields := map[accesstypes.Permission][]accesstypes.Resource{}
		for res, permissions := range resourceStore {
			for _, permission := range permissions {
				permissionFields[permission] = append(permissionFields[permission], res)
			}
		}
		for permission, resoures := range permissionFields {
			_, missing, err := s.enforcer.RequireResources(ctx, user, domain, permission, resoures...)
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

	return &accesstypes.ResolvedPermissions{
		Resources: resolvedResourcePermissions,
		Fields:    resolvedFieldPermissions,
	}, nil
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

	for _, store := range s.fieldStore {
		for resource, fields := range store {
			for field, permissions := range fields {
				for _, permission := range permissions {
					permissionResources[permission] = append(permissionResources[permission], resource.ResourceWithField(field))
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

	for scope, store := range s.fieldStore {
		r, f := resource.ResourceAndField()
		if _, ok := store[r][f]; ok {
			return scope
		}
	}

	return ""
}

func copyOfPermissionFieldsMap(m map[accesstypes.Permission][]string) map[accesstypes.Permission][]string {
	cpy := map[accesstypes.Permission][]string{}
	for permission, fields := range m {
		cpy[permission] = copyOfFields(fields)
	}

	return cpy
}

func copyOfFields(fields []string) []string {
	cpy := make([]string, len(fields))
	copy(cpy, fields)

	return cpy
}
