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

func (d Resource) Marshal() string {
	if !d.IsValid() {
		panic(fmt.Sprintf("invalid resource %q, type can not contain prefix", string(d)))
	}

	return resourcePrefix + string(d)
}

func (d Resource) IsValid() bool {
	return !strings.HasPrefix(string(d), resourcePrefix)
}
