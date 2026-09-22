package check

import (
	"context"
	"fmt"
)

// generatorProgram verifies that every generator program is literal configuration this
// release can read: known options only, literal arguments, right arities. A program the
// tool cannot read completely is a program it cannot later edit or migrate.
type generatorProgram struct{}

// generatorProgramName is the check's name.
const generatorProgramName = "generator-program"

func (generatorProgram) Name() string { return generatorProgramName }

func (generatorProgram) Describe() string {
	return "every generator program uses known options with literal arguments, and reads Warnings() after it generates (WARN)"
}

func (c generatorProgram) Run(_ context.Context, env *Env) Result {
	if len(env.App.Generators) == 0 {
		return fail(c.Name(), "no generator program found (no file calls generation.NewResourceGenerator)")
	}

	var details, unread []string
	for _, g := range env.App.Generators {
		for _, p := range g.Problems {
			details = append(details, p.String())
		}
		if !g.ReadsWarnings {
			unread = append(unread, fmt.Sprintf("%s: the program never reads Warnings(): the schema warnings a generation raises go unseen; print them after Generate() as the skeletons' runner does, and pin the accepted set in its warnings test", g.File))
		}
	}
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d generator program problem(s)", len(details)), append(details, unread...)...)
	}
	read := fmt.Sprintf("%d generator program(s) read completely", len(env.App.Generators))
	if len(unread) > 0 {
		return warn(c.Name(), fmt.Sprintf("%s; %d never read Warnings()", read, len(unread)), unread...)
	}

	return pass(c.Name(), read)
}
