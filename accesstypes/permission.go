package accesstypes

// Permission represents a permission in the authorization system
type Permission string

const (
	// NullPermission represents no permission
	NullPermission Permission = ""

	// Create is the create permission in CRUD
	Create Permission = "Create"

	// Read is the read permission in CRUD used to fetch single resource
	Read Permission = "Read"

	// List is the list permission used to fetch multiple resources
	List Permission = "List"

	// Update is the update permission in CRUD
	Update Permission = "Update"

	// Delete is the delete permission in CRUD
	Delete Permission = "Delete"

	// Execute is the execute permission used in RPC
	Execute Permission = "Execute"
)

type (
	// Tag represents the string name of a json tag
	Tag string

	// Field represents the string name of a struct field
	Field string

	// TagPermissions is a map of Tags to a slice of permissions associated with the Tag
	TagPermissions map[Tag][]Permission

	// PermissionScope is the type use to define different scopes
	PermissionScope string
)

const (
	// GlobalPermissionScope is the permission scope for global permissions
	GlobalPermissionScope PermissionScope = "global"

	// DomainPermissionScope is the permission scope for domain permissions
	DomainPermissionScope PermissionScope = "domain"
)
