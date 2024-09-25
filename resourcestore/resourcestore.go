// resourcestore package provides a store to store permission resource mappings
package resourcestore

import (
	"sync"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type Store struct {
	mu sync.RWMutex

	// store is used to store permission resource mappings
	//
	//	store
	//	├── Resource1
	//	│   ├── Read
	//	│   │   └── [field1, field3, ...]
	//	│   ├── Write
	//	│   │   └── [field2, field3, ...]
	//	│   └── ...
	//	├── Resource2
	//	└── Resource...
	store map[accesstypes.Resource]map[accesstypes.Permission][]string
}

func New() *Store {
	store := &Store{
		store: map[accesstypes.Resource]map[accesstypes.Permission][]string{},
	}

	return store
}

func (s *Store) Add(res accesstypes.Resource, permission accesstypes.Permission, fields []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.store[res][permission]; ok {
		return errors.Newf("found existing entry under parent: %s and permission: %s", res, permission)
	}

	if s.store[res] == nil {
		s.store[res] = map[accesstypes.Permission][]string{}
	}
	s.store[res][permission] = copyOfFields(fields)

	return nil
}

func (s *Store) Fields(parent accesstypes.Resource, permission accesstypes.Permission) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fields, ok := s.store[parent][permission]
	if !ok {
		return nil
	}

	return copyOfFields(fields)
}

func (s *Store) PermissionsWithFields(parent accesstypes.Resource) map[accesstypes.Permission][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	permissionsWithFields, ok := s.store[parent]
	if !ok {
		return nil
	}

	return copyOfPermissionFieldsMap(permissionsWithFields)
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
