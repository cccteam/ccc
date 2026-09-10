package resource

import (
	"context"
	"fmt"

	"github.com/cccteam/session/sessioninfo"
)

// UserEvent generates a standard event source string for an action performed by a user.
// It extracts user information from the context: "username (session id)" for an
// ordinary session. For an impersonated session the actor comes first, so every data
// change carries evidence of the real person: "actor impersonating username (session
// id)" for an impersonated user, "actor as role name (session id)" for a role.
func UserEvent(ctx context.Context) string {
	user := sessioninfo.FromCtx(ctx)

	imp, ok := sessioninfo.ImpersonationFromCtx(ctx)
	if !ok {
		return fmt.Sprintf("%s (%s)", user.Username, user.ID)
	}
	if role, isRole := imp.Principal.Role(); isRole {
		return fmt.Sprintf("%s as role %s (%s)", imp.Actor, role, user.ID)
	}

	return fmt.Sprintf("%s impersonating %s (%s)", imp.Actor, user.Username, user.ID)
}

// ProcessEvent generates a standard event source string for a system process.
func ProcessEvent(processName string) string {
	return fmt.Sprintf("Process %s", processName)
}

// UserProcessEvent generates a standard event source string for an action performed
// by a user within a specific system process.
func UserProcessEvent(ctx context.Context, processName string) string {
	return fmt.Sprintf("%s: %s", UserEvent(ctx), ProcessEvent(processName))
}
