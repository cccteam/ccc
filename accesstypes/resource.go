package accesstypes

import (
	"fmt"
	"strings"
)

// GlobalResource is the resource used when a permission is applied to the entire application, (i.e. Global level)
// instead of to a specific resource.
const GlobalResource = Resource("global")

const resourcePrefix = "resource:"

// Resource represents a resource in the authorization system
type Resource string

// UnmarshalResource unmarshals a resource string into a Resource type.
func UnmarshalResource(resource string) Resource {
	r := Resource(strings.TrimPrefix(resource, resourcePrefix))
	if !r.isValid() {
		panic(fmt.Sprintf("invalid resource %q", resource))
	}

	return r
}

// Marshal marshals a Resource type into a string.
func (r Resource) Marshal() string {
	if !r.isValid() {
		panic(fmt.Sprintf("invalid resource %q, type can not contain prefix", string(r)))
	}

	return resourcePrefix + string(r)
}

func (r Resource) isValid() bool {
	return !strings.HasPrefix(string(r), resourcePrefix)
}

// ResourceWithTag returns the fully qualified resource name for the resource feild with tag
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
