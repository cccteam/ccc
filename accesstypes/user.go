package accesstypes

import (
	"fmt"
	"strings"
)

// NoopUser is the user assigned to an empty role in casbin to ensure the role can be enumerated if no one else is assigned
const NoopUser = "noop"

const userPrefix = "user:"

// User represents a user in the authorization system
type User string

func UnmarshalUser(user string) User {
	u := User(strings.TrimPrefix(user, userPrefix))
	if !u.isValid() {
		panic(fmt.Sprintf("invalid user %q", user))
	}

	return u
}

func (u User) Marshal() string {
	if !u.isValid() {
		panic(fmt.Sprintf("invalid user %q, type can not contain prefix", string(u)))
	}

	return userPrefix + string(u)
}

func (u User) isValid() bool {
	return !strings.HasPrefix(string(u), userPrefix)
}
