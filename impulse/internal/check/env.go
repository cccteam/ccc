package check

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// envTemplate verifies that every env tag without a default appears in the development
// environment template, so a fresh checkout knows which variables it must set.
type envTemplate struct{}

func (envTemplate) Name() string { return "env-template" }

func (envTemplate) Describe() string {
	return "every env tag without a default appears in the development environment template"
}

var envAssignRE = regexp.MustCompile(`(?m)^\s*#?\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)=`)

func (c envTemplate) Run(_ context.Context, env *Env) Result {
	a := env.App
	if len(a.EnvTags) == 0 {
		return skip(c.Name(), "no env tags declared")
	}
	if a.EnvTemplate == "" {
		return skip(c.Name(), "no .envrc.template (or .env.template, .env.example) at the application root")
	}

	data, err := os.ReadFile(a.Abs(a.EnvTemplate))
	if err != nil {
		return fail(c.Name(), errors.Wrap(err, "os.ReadFile()").Error())
	}
	declared := map[string]bool{}
	for _, m := range envAssignRE.FindAllStringSubmatch(string(data), -1) {
		declared[m[1]] = true
	}

	var details, missing []string
	seen := map[string]bool{}
	for _, t := range a.EnvTags {
		if t.HasDefault || declared[t.Name] || seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		missing = append(missing, t.Name)
		details = append(details, fmt.Sprintf("%s:%d: %s%s is not in %s", t.File, t.Line, t.Name, requiredNote(t), a.EnvTemplate))
	}

	if len(missing) == 0 {
		return pass(c.Name(), fmt.Sprintf("%d env tag(s) covered by %s", len(a.EnvTags), a.EnvTemplate))
	}
	if env.Fix {
		slices.Sort(missing)
		lines := make([]string, 0, len(missing))
		for _, n := range missing {
			lines = append(lines, "export "+n+"=")
		}
		if err := appendLines(a.Abs(a.EnvTemplate), "# Added by impulse check --fix: set these for local development.", lines); err != nil {
			return fail(c.Name(), err.Error())
		}

		return passWithDetails(c.Name(), fmt.Sprintf("%d variable(s) added to %s", len(lines), a.EnvTemplate), details...)
	}

	return fail(c.Name(), fmt.Sprintf("%d variable(s) missing from %s (--fix adds them)", len(missing), a.EnvTemplate), details...)
}

func requiredNote(t app.EnvTag) string {
	if t.Required {
		return " (required)"
	}

	return ""
}
