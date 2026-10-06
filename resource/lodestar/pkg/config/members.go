package config

import (
	"github.com/cccteam/access"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
)

// Members returns the members auth: its session manager and permission store, for the
// surface that binds to it.
func (c *DataConfiguration) Members() *members.Auth {
	return c.members
}

// MembersAccess returns the members auth's permission engine: what a request the portal's
// session group bound checks against.
func (c *DataConfiguration) MembersAccess() access.Controller {
	return c.members.Access()
}
