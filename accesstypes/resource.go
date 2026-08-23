package accesstypes

import (
	"fmt"
	"strings"
)

// GlobalResource is the resource used when a permission is applied to the entire application, (i.e. Global level)
// instead of to a specific resource.
//
// The value carries the reserved marker character ':' so it can never collide
// with a caller-authored resource name: the access policy stores reject any
// other ':'-bearing resource at their write boundary, and a resource literally
// named "global" is ordinary data, distinct from this marker.
const GlobalResource = Resource("access:global")

// Resource represents a resource in the authorization system
type Resource string

// ResourceWithTag returns the fully qualified resource name for the resource field with tag
func (r Resource) ResourceWithTag(tag Tag) Resource {
	if strings.Contains(string(tag), ".") {
		panic("invalid tag name, must not contain '.'")
	}

	return Resource(fmt.Sprintf("%s.%s", r, tag))
}

// ResourceAndTag splits the Resource name from the Tag name for a fully qualified field resource name
func (r Resource) ResourceAndTag() (Resource, Tag) {
	parts := strings.Split(string(r), ".")
	if len(parts) > 2 {
		panic("invalid resource name contains more than one '.'")
	}

	if len(parts) == 2 {
		return Resource(parts[0]), Tag(parts[1])
	}

	return Resource(parts[0]), ""
}
