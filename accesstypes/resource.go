package accesstypes

import (
	"fmt"
	"strings"
)

// GlobalResource is the resource used when a permission is applied to the entire application, (i.e. Global level)
// instead of to a specific resource.
const GlobalResource = Resource("global")

const resourcePrefix = "resource:"

type Resource string

func UnmarshalResource(resource string) Resource {
	r := Resource(strings.TrimPrefix(resource, resourcePrefix))
	if !r.isValid() {
		panic(fmt.Sprintf("invalid resource %q", resource))
	}

	return r
}

func (r Resource) Marshal() string {
	if !r.isValid() {
		panic(fmt.Sprintf("invalid resource %q, type can not contain prefix", string(r)))
	}

	return resourcePrefix + string(r)
}

func (r Resource) isValid() bool {
	return !strings.HasPrefix(string(r), resourcePrefix)
}

func (r Resource) ResourceWithTag(tag Tag) Resource {
	if strings.Contains(string(tag), ".") {
		panic("invalid tag name, must not contain '.'")
	}

	return Resource(fmt.Sprintf("%s.%s", r, tag))
}

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
