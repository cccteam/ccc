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
	fieldStore    map[accesstypes.Resource]map[accesstypes.Permission][]string
	resourceStore map[accesstypes.Resource][]accesstypes.Permission
)

type Enforcer interface {
	RequireResources(ctx context.Context, user accesstypes.User, domain accesstypes.Domain, perms accesstypes.Permission, resources ...accesstypes.Resource) (bool, error)
}

type Store struct {
	mu sync.RWMutex

	enforcer Enforcer

	fieldStore    map[accesstypes.PermissionScope]fieldStore
	resourceStore map[accesstypes.PermissionScope]resourceStore
}

func New(e Enforcer) *Store {
	store := &Store{
		enforcer:      e,
		fieldStore:    map[accesstypes.PermissionScope]fieldStore{},
		resourceStore: map[accesstypes.PermissionScope]resourceStore{},
	}

	return store
}

func (s *Store) AddResourceFields(scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource, fields []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.fieldStore[scope][res][permission]; ok {
		return errors.Newf("found existing entry under resource: %s and permission: %s", res, permission)
	}
	if s.fieldStore[scope][res] == nil {
		if s.fieldStore[scope] == nil {
			s.fieldStore[scope] = make(fieldStore, 2)
		}
		s.fieldStore[scope][res] = map[accesstypes.Permission][]string{}
	}
	s.fieldStore[scope][res][permission] = copyOfFields(fields)

	return nil
}

func (s *Store) AddResource(scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ok := slices.Contains(s.resourceStore[scope][res], permission); ok {
		return errors.Newf("found existing entry under resource: %s and permission: %s", res, permission)
	}

	if s.fieldStore[scope][res] == nil {
		if s.fieldStore[scope] == nil {
			s.fieldStore[scope] = make(map[accesstypes.Resource]map[accesstypes.Permission][]string, 2)
		}
		s.fieldStore[scope][res] = make(map[accesstypes.Permission][]string, 2)
	}
	s.resourceStore[scope][res] = append(s.resourceStore[scope][res], permission)

	return nil
}

func (s *Store) ResolvePermissionsOnResource(ctx context.Context, user accesstypes.User) any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	permissions := s.fieldStore[scope][parent]

	resolvedPermissions := make(map[accesstypes.Permission]map[accesstypes.Resource]bool, len(permissions))
	for permission, fields := range permissions {

		usersPermissions := make(map[accesstypes.Resource]bool, len(fields))
		for _, field := range fields {
			fullyQualifiedResource := parent.Resource(field)
			err := s.enforcer.RequireResources(ctx, user, domain, permission, fullyQualifiedResource) // TODO: Do some research here to see if we need to inspect the err

			userPossesses := err == nil
			usersPermissions[fullyQualifiedResource] = userPossesses
		}
		resolvedPermissions[permission] = usersPermissions

	}

	return resolvedPermissions
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
