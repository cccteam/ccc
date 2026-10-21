package check

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cccteam/ccc/impulse/app"
)

// sessionTables verifies that every session Auth the application constructs has its
// tables in the schema (sessions, users, identities, pending sign-ins, sign-in attempts,
// and the auth events, impersonation and custom data tables where the storage attaches
// them), that
// two Auths never share a table, that every Auth has at least one sign-in method
// registered on it and every registration an Auth it reaches, and reports the auths
// that result with their methods. A construction of a session type v0.13.0 retired (the
// single-method types) fails the check, with what to move to. The compiler holds the
// handler wiring (the router's session interfaces name the methods declared); this check
// holds the schema the Auth reads, which nothing compiles against.
type sessionTables struct{}

func (sessionTables) Name() string { return "session-tables" }

func (sessionTables) Describe() string {
	return "every session Auth's tables exist in the schema, no two Auths share a table, every Auth registers a sign-in method, and the auths are reported with their methods"
}

func (c sessionTables) Run(_ context.Context, env *Env) Result {
	a := env.App
	p := a.Profile()
	if len(p.Sites) == 0 {
		return skip(c.Name(), "no site generator")
	}
	if len(a.Auths) == 0 {
		return fail(c.Name(), "no session Auth is constructed outside tests (session.NewAuth)")
	}
	var details []string

	tables, unread, err := migrationTables(a, p)
	if err != nil {
		return fail(c.Name(), err.Error())
	}

	holders := map[string]string{} // table -> the auth that reads it
	for i := range a.Auths {
		auth := &a.Auths[i]
		for _, t := range authTables(auth) {
			if _, ok := tables[t]; !ok {
				details = append(details, fmt.Sprintf("%s:%d: the %s reads table %s, which no migration creates (the session library's schema is under %s)", auth.File, auth.Line, authLabel(auth), t, schemaSets))
			}
			if other, ok := holders[t]; ok && other != authLabel(auth) {
				details = append(details, fmt.Sprintf("%s:%d: the %s shares table %s with the %s; each Auth needs its own tables (the With*TableName options and the storage's table constructors)", auth.File, auth.Line, authLabel(auth), t, other))

				continue
			}
			holders[t] = authLabel(auth)
		}
		if len(auth.Methods) == 0 {
			details = append(details, fmt.Sprintf("%s:%d: the %s registers no sign-in method, so nobody can sign in to it; register session.PasswordSignIn, azure.SignIn, or google.SignIn on it", auth.File, auth.Line, authLabel(auth)))
		}
		seen := map[string]bool{}
		for j := range auth.Methods {
			m := &auth.Methods[j]
			switch {
			case m.Name == "":
				details = append(details, fmt.Sprintf("%s:%d: the %s method's registration name is not a literal or a constant of the file, so the routes it serves cannot be read", m.File, m.Line, m.Kind))
			case seen[m.Name]:
				details = append(details, fmt.Sprintf("%s:%d: the %s registers %s twice; a registration name is unique on an Auth", m.File, m.Line, authLabel(auth), m.Name))
			}
			seen[m.Name] = true
		}
	}
	for i := range a.UnattachedMethods {
		m := &a.UnattachedMethods[i]
		details = append(details, fmt.Sprintf("%s:%d: the %s method (%s) is registered on %q, which names no session.NewAuth construction the scan can see; register methods in the package that constructs the Auth, on the identifier its construction is assigned to", m.File, m.Line, m.Kind, m.Name, m.AuthVar))
	}

	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d auth wiring problem(s)", len(details)), append(details, unread...)...)
	}

	auths, notes := authSummary(a.Auths)

	return passWithDetails(c.Name(), fmt.Sprintf("%d auth(s): %s", len(auths), strings.Join(auths, "; ")), append(notes, unread...)...)
}

// schemaSets names the session library's schema sets an Auth's tables come from.
const schemaSets = "schema/spanner/migrations and schema/spanner/auth/migrations (impersonation/migrations for the impersonation table)"

// authTables lists the tables one construction reads.
func authTables(auth *app.Auth) []string {
	tables := []string{auth.SessionTable, auth.UserTable, auth.IdentitiesTable, auth.PendingTable, auth.AttemptsTable}
	if auth.AuthEvents {
		table := auth.AuthEventsTable
		if table == "" {
			table = app.DefaultAuthEventsTable
		}
		tables = append(tables, table)
	}
	if auth.Impersonation {
		table := auth.ImpersonationTable
		if table == "" {
			table = app.DefaultImpersonationTable
		}
		tables = append(tables, table)
	}
	tables = append(tables, auth.ExtraTables...)

	return tables
}

// authLabel names an Auth in a report: its package's auth when it lives in one
// (pkg/auth/<name>), else the Auth at its file and line.
func authLabel(auth *app.Auth) string {
	if name := app.AuthPackageName(auth.File); name != "" {
		return name + " auth"
	}

	return fmt.Sprintf("Auth at %s:%d", auth.File, auth.Line)
}

// methodSummary renders an Auth's methods: the registration name with the method behind
// it when the two differ, and the authority for a directory method
// (password, directory (azure, application authority)).
func methodSummary(auth *app.Auth) string {
	if len(auth.Methods) == 0 {
		return "no methods"
	}
	parts := make([]string, 0, len(auth.Methods))
	for i := range auth.Methods {
		m := &auth.Methods[i]
		desc := m.Name
		var notes []string
		if m.Name != m.Kind {
			notes = append(notes, m.Kind)
		}
		if m.Authority != "" {
			notes = append(notes, m.Authority+" authority")
		}
		if len(notes) > 0 {
			desc += " (" + strings.Join(notes, ", ") + ")"
		}
		parts = append(parts, desc)
	}

	return "methods " + strings.Join(parts, ", ")
}

// authSummary renders each construction, named by its package when it lives in one
// (pkg/auth/<name>), with notes on what the constructions leave to their callers.
func authSummary(auths []app.Auth) (summaries, notes []string) {
	for i := range auths {
		auth := &auths[i]
		desc := fmt.Sprintf("%s; tables %s", methodSummary(auth), strings.Join(authTables(auth), ", "))
		if auth.CookieName != "" {
			desc += ", cookie " + auth.CookieName
		}
		if auth.XSRFCookieName != "" {
			desc += ", xsrf cookie " + auth.XSRFCookieName
		}
		if name := app.AuthPackageName(auth.File); name != "" {
			desc = name + ": " + desc
		}
		summaries = append(summaries, desc)
		if auth.OptionsForwarded {
			notes = append(notes, fmt.Sprintf("%s:%d: options are forwarded from the caller (opts...); tables and cookies beyond the defaults are not visible here", auth.File, auth.Line))
		}
		if auth.Impersonation && auth.ImpersonationTable == "" {
			notes = append(notes, fmt.Sprintf("%s:%d: the impersonation table name is not a literal; %s assumed", auth.File, auth.Line, app.DefaultImpersonationTable))
		}
		if auth.AuthEvents && auth.AuthEventsTable == "" {
			notes = append(notes, fmt.Sprintf("%s:%d: the auth events table name is not a literal; %s assumed", auth.File, auth.Line, app.DefaultAuthEventsTable))
		}
	}
	sort.Strings(summaries)

	return summaries, notes
}
