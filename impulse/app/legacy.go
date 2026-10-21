package app

// LegacyAuth is an Auth option of a generator program whose method argument is not a
// sign-in method declaration: the identifier the single-method session types took
// (generation.Password, generation.OIDCGoogle and their kin), which the library keeps
// under session/legacy. No recipe moves an application off those types; it is re-created
// from the skeleton.
type LegacyAuth struct {
	// Pos is the Auth call's position as file:line.
	Pos string
	// Package is the auth package the option names.
	Package string
	// Text is the argument's source text (generation.Password).
	Text string
}

// LegacyAuths lists the generator program's Auth options whose method arguments are not
// sign-in method declarations, nested options included (Auth sits inside GenerateRoutes).
func (g *Generator) LegacyAuths() []LegacyAuth {
	var found []LegacyAuth
	for i := range g.Options {
		found = appendLegacyAuths(found, &g.Options[i])
	}

	return found
}

// appendLegacyAuths walks one option call and the calls nested in its arguments.
func appendLegacyAuths(found []LegacyAuth, c *Call) []LegacyAuth {
	if c.Name == optAuth {
		for _, a := range c.Args[min(1, len(c.Args)):] {
			if a.Kind == ArgOther {
				found = append(found, LegacyAuth{Pos: c.Pos, Package: c.Args[0].Str, Text: a.Text})
			}
		}
	}
	for i := range c.Args {
		if c.Args[i].Kind == ArgCall && c.Args[i].Call != nil {
			found = appendLegacyAuths(found, c.Args[i].Call)
		}
	}

	return found
}
