package check

import (
	"context"
)

// goworkOff builds and vets the module with GOWORK=off. A go.work in the tree hides stale
// pins: the workspace builds against local siblings while CI, which has no workspace,
// resolves the pins in go.mod and fails.
type goworkOff struct{}

func (goworkOff) Name() string { return "gowork-off" }

func (goworkOff) Describe() string {
	return "the module builds and vets with GOWORK=off (pins, not the workspace, resolve)"
}

func (c goworkOff) Run(ctx context.Context, env *Env) Result {
	extra := []string{"GOWORK=off"}
	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		env.notef("running GOWORK=off go %s %s", args[0], args[1])
		out, err := env.Exec.Run(ctx, env.App.Root, extra, "go", args...)
		if err != nil {
			return fail(c.Name(), "GOWORK=off go "+args[0]+" ./... failed", outputLines(out, 40)...)
		}
	}

	return pass(c.Name(), "GOWORK=off go build and go vet succeed")
}
