package resource

import (
	"context"
	"fmt"

	"github.com/cccteam/session/sessioninfo"
)

// UserEvent generates a standard event source string for an action performed by a user.
// It extracts user information from the context.
func UserEvent(ctx context.Context) string {
	user := sessioninfo.FromCtx(ctx)

	return fmt.Sprintf("%s (%s)", user.Username, user.ID)
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
