package check

import (
	"context"
	"fmt"
)

// generatorProgram verifies that every generator program is literal configuration this
// release can read: known options only, literal arguments, right arities. A program the
// tool cannot read completely is a program it cannot later edit or migrate.
type generatorProgram struct{}

func (generatorProgram) Name() string { return "generator-program" }

func (generatorProgram) Describe() string {
	return "every generator program uses known options with literal arguments"
}

func (c generatorProgram) Run(_ context.Context, env *Env) Result {
	if len(env.App.Generators) == 0 {
		return fail(c.Name(), "no generator program found (no file calls generation.NewResourceGenerator)")
	}

	var details []string
	for _, g := range env.App.Generators {
		for _, p := range g.Problems {
			details = append(details, p.String())
		}
	}
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d generator program problem(s)", len(details)), details...)
	}

	return pass(c.Name(), fmt.Sprintf("%d generator program(s) read completely", len(env.App.Generators)))
}
