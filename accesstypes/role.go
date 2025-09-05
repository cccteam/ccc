package accesstypes

import (
	"fmt"
	"strings"
)

const rolePrefix = "role:"

// Role represents a role in the authorization system
type Role string

// UnmarshalRole unmarshals a role string into a Role type.
func UnmarshalRole(role string) Role {
	r := Role(strings.TrimPrefix(role, rolePrefix))
	if !r.isValid() {
		panic(fmt.Sprintf("invalid role %q", role))
	}

	return r
}

// Marshal marshals a Role type into a string.
func (r Role) Marshal() string {
	if !r.isValid() {
		panic(fmt.Sprintf("invalid role %q, type can not contain prefix", string(r)))
	}

	return rolePrefix + string(r)
}

func (r Role) isValid() bool {
	return !strings.HasPrefix(string(r), rolePrefix)
}
