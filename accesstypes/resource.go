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
	return Resource(strings.TrimPrefix(resource, resourcePrefix))
}

func (r Resource) Marshal() string {
	if !r.IsValid() {
		panic(fmt.Sprintf("invalid resource %q, type can not contain prefix", string(r)))
	}

	return resourcePrefix + string(r)
}

func (r Resource) IsValid() bool {
	return !strings.HasPrefix(string(r), resourcePrefix)
}

func (r Resource) Resource(fieldName string) Resource {
	return Resource(fmt.Sprintf("%s.%s", r, fieldName))
}
