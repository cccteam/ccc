package accesstypes

import (
	"fmt"
	"strings"
)

const rolePrefix = "role:"

type Role string

func UnmarshalRole(role string) Role {
	return Role(strings.TrimPrefix(role, rolePrefix))
}

func (r Role) Marshal() string {
	if !r.IsValid() {
		panic(fmt.Sprintf("invalid role %q, type can not contain prefix", string(r)))
	}

	return rolePrefix + string(r)
}

func (r Role) IsValid() bool {
	return !strings.HasPrefix(string(r), rolePrefix)
}
