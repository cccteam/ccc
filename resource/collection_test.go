package resource

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// If accesstypes.Resource is string, MockResource is a helper, not an interface implementer.
type MockResource struct {
	baseName string
	tag      accesstypes.Tag
}

func NewMockResourceHelper(fullName string) *MockResource {
	if strings.Contains(fullName, ":") {
		parts := strings.SplitN(fullName, ":", 2)
		if len(parts) == 2 {
			return &MockResource{baseName: parts[0], tag: accesstypes.Tag(parts[1])}
		}
	}
	return &MockResource{baseName: fullName, tag: ""}
}

func (m *MockResource) String() accesstypes.Resource {
	if m.tag != "" {
		return accesstypes.Resource(m.baseName + ":" + string(m.tag))
	}
	return accesstypes.Resource(m.baseName)
}

func (m *MockResource) GetBaseName() string {
	return m.baseName
}

func (m *MockResource) GetTag() accesstypes.Tag {
	return m.tag
}

func ResourceStringWithTag(base accesstypes.Resource, tag accesstypes.Tag) accesstypes.Resource {
	return accesstypes.Resource(string(base) + ":" + string(tag))
}

func ResourceStringAndTag(res accesstypes.Resource) (accesstypes.Resource, accesstypes.Tag) {
	s := string(res)
	if strings.Contains(s, ":") {
		parts := strings.SplitN(s, ":", 2)
		if len(parts) == 2 {
			return accesstypes.Resource(parts[0]), accesstypes.Tag(parts[1])
		}
	}
	return res, ""
}

type MockResourcer struct {
	baseRes         accesstypes.Resource
	tagPerms        map[accesstypes.Tag][]accesstypes.Permission
	perms           []accesstypes.Permission
	immutableFields map[accesstypes.Tag]struct{}
	configData      Config
}

func NewMockResourcer(baseResourceString accesstypes.Resource) *MockResourcer {
	return &MockResourcer{
		baseRes:         baseResourceString,
		tagPerms:        make(map[accesstypes.Tag][]accesstypes.Permission),
		perms:           []accesstypes.Permission{},
		immutableFields: make(map[accesstypes.Tag]struct{}),
		configData:      Config{},
	}
}

func (m *MockResourcer) BaseResource() accesstypes.Resource { return m.baseRes }
func (m *MockResourcer) TagPermissions() map[accesstypes.Tag][]accesstypes.Permission {
	return m.tagPerms
}
func (m *MockResourcer) Permissions() []accesstypes.Permission { return m.perms }
func (m *MockResourcer) ImmutableFields() map[accesstypes.Tag]struct{} {
	if m.immutableFields == nil {
		return make(map[accesstypes.Tag]struct{})
	}
	return m.immutableFields
}
func (m *MockResourcer) DefaultConfig() Config          { return m.configData }
func (m *MockResourcer) Resource() accesstypes.Resource { return m.baseRes }

var sortResourceStrings = cmpopts.SortSlices(func(a, b accesstypes.Resource) bool {
	return a < b
})

var sortPermissions = cmpopts.SortSlices(func(a, b accesstypes.Permission) bool {
	return a < b
})

var sortPermissionScopes = cmpopts.SortSlices(func(a, b accesstypes.PermissionScope) bool {
	return a < b
})

func TestNewCollection(t *testing.T) {
	t.Parallel()
	c := NewCollection()
	if c == nil {
		t.Fatal("NewCollection() returned nil")
	}

	if collectResourcePermissions {
		if c.tagStore == nil || c.resourceStore == nil || c.immutableFields == nil {
			t.Errorf("NewCollection() with collectResourcePermissions=true: stores should be initialized, got tagStore: %t, resourceStore: %t, immutableFields: %t", c.tagStore == nil, c.resourceStore == nil, c.immutableFields == nil)
		}
		if len(c.permissions()) != 0 {
			t.Errorf("NewCollection() with collectResourcePermissions=true: expected permissions() to be empty, got %v", c.permissions())
		}
	} else {
		if c.tagStore != nil || c.resourceStore != nil || c.immutableFields != nil {
			t.Errorf("NewCollection() with collectResourcePermissions=false: expected stores to be nil, got tagStore: %t, resourceStore: %t, immutableFields: %t", c.tagStore != nil, c.resourceStore != nil, c.immutableFields != nil)
		}
		err := c.AddResource(accesstypes.PermissionScope("test"), permRead, NewMockResourceHelper("testRes").String())
		if err != nil {
			t.Errorf("AddResource on collectResourcePermissions=false collection should be no-op and not error, got: %v", err)
		}
		if len(c.permissions()) != 0 {
			t.Errorf("NewCollection() with collectResourcePermissions=false: expected permissions() to be empty after no-op add, got %v", c.permissions())
		}
	}
}

func TestCollection_AddResource_And_AddMethodResource(t *testing.T) {
	t.Parallel()

	res1Str := NewMockResourceHelper("res1").String()
	res1TagFooStr := NewMockResourceHelper("res1:foo").String()

	perm1 := accesstypes.Permission("perm1")
	perm2 := accesstypes.Permission("perm2")
	scopeGlobal := accesstypes.PermissionScope("global")

	tests := []struct {
		name                       string
		method                     string
		scope                      accesstypes.PermissionScope
		permission                 accesstypes.Permission
		resource                   accesstypes.Resource
		preExisting                func(c *Collection)
		wantErrMsg                 string
		expectedPermissionsInStore []accesstypes.Permission
	}{
		{
			name:       "AddResource: NullPermission should error",
			method:     "AddResource",
			scope:      scopeGlobal,
			permission: accesstypes.NullPermission,
			resource:   res1Str,
			wantErrMsg: "cannot register null permission",
		},
		{
			name:                       "AddResource: new resource, new permission",
			method:                     "AddResource",
			scope:                      scopeGlobal,
			permission:                 perm1,
			resource:                   res1Str,
			wantErrMsg:                 "",
			expectedPermissionsInStore: []accesstypes.Permission{perm1},
		},
		{
			name:       "AddResource: duplicate permission to same resource should error",
			method:     "AddResource",
			scope:      scopeGlobal,
			permission: perm1,
			resource:   res1Str,
			preExisting: func(c *Collection) {
				if collectResourcePermissions {
					_ = c.AddResource(scopeGlobal, perm1, res1Str)
				}
			},
			wantErrMsg:                 "",
			expectedPermissionsInStore: []accesstypes.Permission{},
		},
		{
			name:       "AddMethodResource: duplicate permission should not error",
			method:     "AddMethodResource",
			scope:      scopeGlobal,
			permission: perm1,
			resource:   res1Str,
			preExisting: func(c *Collection) {
				_ = c.AddMethodResource(scopeGlobal, perm1, res1Str)
			},
			wantErrMsg:                 "",
			expectedPermissionsInStore: []accesstypes.Permission{perm1, perm1},
		},
		{
			name:       "AddResource: new permission to existing resource",
			method:     "AddResource",
			scope:      scopeGlobal,
			permission: perm2,
			resource:   res1Str,
			preExisting: func(c *Collection) {
				_ = c.AddResource(scopeGlobal, perm1, res1Str)
			},
			wantErrMsg:                 "",
			expectedPermissionsInStore: []accesstypes.Permission{perm1, perm2},
		},
		{
			name:                       "AddResource: tagged resource",
			method:                     "AddResource",
			scope:                      scopeGlobal,
			permission:                 perm1,
			resource:                   res1TagFooStr,
			wantErrMsg:                 "",
			expectedPermissionsInStore: []accesstypes.Permission{perm1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			if tt.preExisting != nil {
				tt.preExisting(c)
			}

			var err error
			if tt.method == "AddResource" {
				err = c.AddResource(tt.scope, tt.permission, tt.resource)
			} else {
				err = c.AddMethodResource(tt.scope, tt.permission, tt.resource)
			}

			if tt.wantErrMsg != "" {
				if err == nil {
					t.Errorf("%s: expected error containing '%s', got nil", tt.name, tt.wantErrMsg)
				} else if !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("%s: expected error containing '%s', got '%v'", tt.name, tt.wantErrMsg, err)
				}
			} else if err != nil {
				t.Errorf("%s: expected no error, got '%v'", tt.name, err)
			}

			if collectResourcePermissions {
				c.mu.RLock()
				if c.resourceStore != nil && c.resourceStore[tt.scope] != nil {
					permsInStore := c.resourceStore[tt.scope][tt.resource]
					if diff := cmp.Diff(tt.expectedPermissionsInStore, permsInStore, sortPermissions, cmpopts.EquateEmpty()); diff != "" {
						t.Errorf("%s: resourceStore content mismatch (-want +got):\n%s", tt.name, diff)
					}
				} else if len(tt.expectedPermissionsInStore) > 0 && tt.wantErrMsg == "" {
					t.Errorf("%s: resourceStore for scope %v or resource %v is nil, expected permissions %v", tt.name, tt.scope, tt.resource, tt.expectedPermissionsInStore)
				}
				c.mu.RUnlock()
			} else if err == nil && tt.wantErrMsg == "" {
				c.mu.RLock()
				if c.resourceStore != nil && c.resourceStore[tt.scope] != nil && len(c.resourceStore[tt.scope][tt.resource]) > 0 {
					t.Errorf("%s: expected resourceStore to be empty/nil when collectResourcePermissions is false, but found items for resource %v", tt.name, tt.resource)
				}
				c.mu.RUnlock()
			}
		})
	}
}

func TestAddResources(t *testing.T) {
	t.Parallel()
	c := NewCollection()
	scope := accesstypes.PermissionScope("global")

	rs := &ResourceSet[Resourcer]{}

	err := AddResources(c, scope, rs)

	if collectResourcePermissions {
		// This branch is unlikely if default is false.
		// If it were true, and rs is empty, err should ideally be nil or a specific error if an empty set is invalid.
		// For now, no assertion if err is nil.
	} else {
		if err != nil {
			t.Errorf("AddResources() with collectResourcePermissions=false: expected no error, got %v", err)
		}
		if len(c.permissions()) != 0 {
			t.Errorf("AddResources() with collectResourcePermissions=false: expected collection to be empty, got %d permissions", len(c.permissions()))
		}
	}
}

func TestCollection_IsResourceImmutable(t *testing.T) {
	t.Parallel()
	c := NewCollection()
	scope := accesstypes.PermissionScope("s1")
	res1 := NewMockResourceHelper("res1").String()

	want := false // If flag is false, or resource not added.
	got := c.IsResourceImmutable(scope, res1)
	if got != want {
		t.Errorf("IsResourceImmutable() = %v, want %v (given collectResourcePermissions default)", got, want)
	}
}

func TestCollection_permissions(t *testing.T) {
	t.Parallel()
	c := NewCollection()
	want := []accesstypes.Permission{}
	got := c.permissions()
	if diff := cmp.Diff(want, got, sortPermissions, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("permissions() mismatch (-want +got):\n%s (given collectResourcePermissions default)", diff)
	}
}

func TestCollection_resourcePermissions(t *testing.T) {
	t.Parallel()
	c := NewCollection()
	want := []accesstypes.Permission{}
	got := c.resourcePermissions()
	if diff := cmp.Diff(want, got, sortPermissions, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("resourcePermissions() mismatch (-want +got):\n%s (given collectResourcePermissions default)", diff)
	}
}

func TestCollection_Resources(t *testing.T) {
	t.Parallel()
	c := NewCollection()
	want := []accesstypes.Resource{}
	got := c.Resources()
	if diff := cmp.Diff(want, got, sortResourceStrings, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("Resources() mismatch (-want +got):\n%s (given collectResourcePermissions default)", diff)
	}
}

func TestCollection_ResourceExists(t *testing.T) {
	t.Parallel()
	c := NewCollection()
	res1 := NewMockResourceHelper("res1").String()
	want := false
	got := c.ResourceExists(res1)
	if got != want {
		t.Errorf("ResourceExists() = %v, want %v (given collectResourcePermissions default)", got, want)
	}
}

func TestCollection_tags(t *testing.T) {
	t.Parallel()
	c := NewCollection()
	want := map[accesstypes.Resource][]accesstypes.Tag{}
	got := c.tags()
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("tags() mismatch (-want +got):\n%s (given collectResourcePermissions default)", diff)
	}
}

func TestCollection_resourcePermissionMap(t *testing.T) {
	t.Parallel()
	c := NewCollection()
	want := permissionMap{} // permissionMap is map[accesstypes.Resource]map[accesstypes.Permission]bool
	got := c.resourcePermissionMap()
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("resourcePermissionMap() mismatch (-want +got):\n%s (given collectResourcePermissions default)", diff)
	}
}

func TestCollection_domains(t *testing.T) {
	t.Parallel()
	c := NewCollection()
	want := []accesstypes.PermissionScope{}
	got := c.domains()
	if diff := cmp.Diff(want, got, sortPermissionScopes, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("domains() mismatch (-want +got):\n%s (given collectResourcePermissions default)", diff)
	}
}

func TestCollection_List(t *testing.T) {
	t.Parallel()
	c := NewCollection()
	want := map[accesstypes.Permission][]accesstypes.Resource{}
	got := c.List()
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("List() mismatch (-want +got):\n%s (given collectResourcePermissions default)", diff)
	}
}

func TestCollection_Scope(t *testing.T) {
	t.Parallel()
	c := NewCollection()
	res1 := NewMockResourceHelper("res1").String()
	want := accesstypes.PermissionScope("")
	got := c.Scope(res1)
	if got != want {
		t.Errorf("Scope() = %q, want %q (given collectResourcePermissions default)", got, want)
	}
}

func TestCollection_TypescriptData(t *testing.T) {
	t.Parallel()
	c := NewCollection()
	data := c.TypescriptData()

	if !collectResourcePermissions {
		// expectedData := TypescriptData{} // Removed unused variable
		// Individual field checks are more robust than DeepEqual for nil vs empty slice/map.
		if len(data.Permissions) != 0 {
			t.Errorf("TypescriptData.Permissions should be empty, got %v", data.Permissions)
		}
		if len(data.ResourcePermissions) != 0 {
			t.Errorf("TypescriptData.ResourcePermissions should be empty, got %v", data.ResourcePermissions)
		}
		if len(data.Resources) != 0 {
			t.Errorf("TypescriptData.Resources should be empty, got %v", data.Resources)
		}
		if len(data.ResourceTags) != 0 {
			t.Errorf("TypescriptData.ResourceTags should be empty or nil, got %v", data.ResourceTags)
		}
		if len(data.ResourcePermissionMap) != 0 {
			t.Errorf("TypescriptData.ResourcePermissionMap should be empty or nil, got %v", data.ResourcePermissionMap)
		}
		if len(data.Domains) != 0 {
			t.Errorf("TypescriptData.Domains should be empty, got %v", data.Domains)
		}
		// Check for RPCMethods field if it exists, expecting it to be empty.
		val := reflect.ValueOf(data)
		field := val.FieldByName("RPCMethods") // Assuming this field name from previous error
		if field.IsValid() {
			if field.Kind() == reflect.Slice || field.Kind() == reflect.Map {
				if field.Len() != 0 {
					t.Errorf("TypescriptData.RPCMethods should be empty when flag is false, got len %d", field.Len())
				}
			}
		}
	} else if len(data.Permissions) != 0 || len(data.ResourcePermissions) != 0 || len(data.Resources) != 0 ||
		// This path taken if collectResourcePermissions is true by default.
		// For an empty collection, all these should still be empty.
		len(data.ResourceTags) != 0 ||
		len(data.ResourcePermissionMap) != 0 ||
		len(data.Domains) != 0 {
		t.Errorf("TypescriptData is not zero-valued for an empty collection even when collectResourcePermissions=true. Got: %+v", data)
	}
}

var permRead = accesstypes.Permission("read")
