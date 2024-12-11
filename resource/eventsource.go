package resource

import (
	"context"
	"fmt"

	"github.com/cccteam/session/sessioninfo"
)

func UserEvent(ctx context.Context) string {
	user := sessioninfo.FromCtx(ctx)

	return fmt.Sprintf("%s (%s)", user.Username, user.ID)
}

func ProcessEvent(processName string) string {
	return fmt.Sprintf("Process %s", processName)
}

func UserProcessEvent(ctx context.Context, processName string) string {
	return fmt.Sprintf("%s: %s", UserEvent(ctx), ProcessEvent(processName))
}
