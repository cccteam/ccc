package accesstypes

import (
	"fmt"
	"strings"
)

const rolePrefix = "role:"

type Role string

func UnmarshalRole(role string) Role {
	r := Role(strings.TrimPrefix(role, rolePrefix))
	if !r.isValid() {
		panic(fmt.Sprintf("invalid role %q", role))
	}

	return r
}

func (r Role) Marshal() string {
	if !r.isValid() {
		panic(fmt.Sprintf("invalid role %q, type can not contain prefix", string(r)))
	}

	return rolePrefix + string(r)
}

func (r Role) isValid() bool {
	return !strings.HasPrefix(string(r), rolePrefix)
}
