package genlang

import (
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// ArgSpec describes one keyword invocation's argument shape: how many leading
// positional values it takes, and which named (key: value) arguments it
// accepts or requires. It is the shared argument-parsing layer for keywords
// with structured argument lists — one place for arity, unknown-key, and
// duplicate-key errors instead of per-keyword string splitting.
type ArgSpec struct {
	// Positional is the exact number of leading positional values.
	Positional int

	// Keys is the set of accepted named-argument keys.
	Keys []string

	// Required is the subset of Keys that must appear.
	Required []string
}

// NamedArgs is one keyword invocation's parsed argument list: the leading
// positional values in order, then the named arguments.
type NamedArgs struct {
	Positional []string
	named      map[string]string
}

// Named returns the value of a named argument and whether it appeared.
func (n NamedArgs) Named(key string) (string, bool) {
	value, ok := n.named[key]

	return value, ok
}

// ParseInvocations parses every invocation of a keyword against spec — one
// NamedArgs per use of the keyword on the same subject. Positional values
// come first, named arguments (key: value) after; an empty value, an unknown
// or duplicate key, a missing required key, or a wrong positional count is an
// error.
func (a Arg) ParseInvocations(spec ArgSpec) ([]NamedArgs, error) {
	invocations := make([]NamedArgs, 0, a.Count())
	for invocation := range a.Seq() {
		parsed, err := parseInvocation(invocation, spec)
		if err != nil {
			return nil, err
		}
		invocations = append(invocations, parsed)
	}

	return invocations, nil
}

func parseInvocation(invocation string, spec ArgSpec) (NamedArgs, error) {
	args := NamedArgs{named: make(map[string]string)}

	for part := range strings.SplitSeq(invocation, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return NamedArgs{}, errors.Newf("empty argument in %q", invocation)
		}

		key, value, isNamed := strings.Cut(part, ":")
		if !isNamed {
			if len(args.named) > 0 {
				return NamedArgs{}, errors.Newf("positional argument %q after a named argument in %q", part, invocation)
			}
			args.Positional = append(args.Positional, part)

			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch {
		case key == "":
			return NamedArgs{}, errors.Newf("named argument with no key in %q", invocation)
		case value == "":
			return NamedArgs{}, errors.Newf("named argument %q with no value in %q", key, invocation)
		case !slices.Contains(spec.Keys, key):
			return NamedArgs{}, errors.Newf("unknown argument %q in %q (accepted: %s)", key, invocation, strings.Join(spec.Keys, ", "))
		}
		if _, dup := args.named[key]; dup {
			return NamedArgs{}, errors.Newf("argument %q given twice in %q", key, invocation)
		}
		args.named[key] = value
	}

	if len(args.Positional) != spec.Positional {
		return NamedArgs{}, errors.Newf("expected %d positional argument(s), found %d in %q", spec.Positional, len(args.Positional), invocation)
	}
	for _, required := range spec.Required {
		if _, ok := args.named[required]; !ok {
			return NamedArgs{}, errors.Newf("missing required argument %q in %q", required, invocation)
		}
	}

	return args, nil
}
