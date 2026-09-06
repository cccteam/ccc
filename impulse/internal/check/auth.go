package check

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// authWired verifies that every session authenticator the application constructs has its
// tables in the schema (sessions, users where the flavor keeps them, impersonation and
// custom data tables where the storage attaches them), that two login flavors never share
// a sessions table, and reports the auths that result: one per distinct flavor and
// table set. The compiler holds
// the handler wiring (the router's Handlers interface embeds the flavor's handler set);
// this check holds the schema the flavor reads, which nothing compiles against.
type authWired struct{}

func (authWired) Name() string { return "auth-wired" }

func (authWired) Describe() string {
	return "every session authenticator's tables exist in the schema, flavors do not share a sessions table, and the auths are reported"
}

func (c authWired) Run(_ context.Context, env *Env) Result {
	a := env.App
	p := a.Profile()
	if len(p.Sites) == 0 {
		return skip(c.Name(), "no site generator")
	}
	if len(a.Auths) == 0 {
		return fail(c.Name(), "no session authenticator is constructed outside tests (session.NewPasswordAuth, NewOIDCAzure, NewOIDCGoogle, or NewPreauth)")
	}

	tables, unread, err := migrationTables(a, p)
	if err != nil {
		return fail(c.Name(), err.Error())
	}

	var details []string
	bySessionTable := map[string]string{} // sessions table -> flavor
	for i := range a.Auths {
		auth := &a.Auths[i]
		for _, t := range authTables(auth) {
			if _, ok := tables[t]; !ok {
				details = append(details, fmt.Sprintf("%s:%d: %s auth reads table %s, which no migration creates (the session library's schema is under %s)", auth.File, auth.Line, auth.Flavor, t, schemaDir(auth.Flavor)))
			}
		}
		if flavor, ok := bySessionTable[auth.SessionTable]; ok && flavor != auth.Flavor {
			details = append(details, fmt.Sprintf("%s:%d: %s auth shares sessions table %s with %s auth; each flavor needs its own (WithSessionTableName)", auth.File, auth.Line, auth.Flavor, auth.SessionTable, flavor))

			continue
		}
		bySessionTable[auth.SessionTable] = auth.Flavor
	}

	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d auth wiring problem(s)", len(details)), append(details, unread...)...)
	}

	auths, notes := authSummary(a.Auths)

	return passWithDetails(c.Name(), fmt.Sprintf("%d auth(s): %s", len(auths), strings.Join(auths, "; ")), append(notes, unread...)...)
}

// authTables lists the tables one construction reads.
func authTables(auth *app.Auth) []string {
	tables := []string{auth.SessionTable}
	if auth.UserTable != "" {
		tables = append(tables, auth.UserTable)
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

// schemaDir is the session library's schema directory for a flavor.
func schemaDir(flavor string) string {
	switch flavor {
	case app.FlavorOIDCAzure:
		return "schema/spanner/oidc/migrations"
	case app.FlavorOIDCGoogle:
		return "schema/spanner/oidc-google/migrations"
	default:
		return "schema/spanner/migrations"
	}
}

// authSummary groups the constructions into auths (one per flavor and table set) and
// renders each, named by its package when it lives in one (pkg/auth/<name>), with notes
// on what the constructions leave to their callers.
func authSummary(auths []app.Auth) (summaries, notes []string) {
	seen := map[string]bool{}
	for i := range auths {
		auth := &auths[i]
		key := auth.Flavor + " " + strings.Join(authTables(auth), ",")
		if seen[key] {
			continue
		}
		seen[key] = true
		desc := fmt.Sprintf("%s (%s", auth.Flavor, strings.Join(authTables(auth), ", "))
		if auth.CookieName != "" {
			desc += ", cookie " + auth.CookieName
		}
		if auth.Authority != "" {
			desc += ", " + auth.Authority + " authority"
		}
		desc += ")"
		if name := app.AuthPackageName(auth.File); name != "" {
			desc = name + ": " + desc
		}
		summaries = append(summaries, desc)
		if auth.OptionsForwarded {
			notes = append(notes, fmt.Sprintf("%s:%d: options are forwarded from the caller (opts...); tables and cookie beyond the defaults are not visible here", auth.File, auth.Line))
		}
		if auth.Impersonation && auth.ImpersonationTable == "" {
			notes = append(notes, fmt.Sprintf("%s:%d: the impersonation table name is not a literal; %s assumed", auth.File, auth.Line, app.DefaultImpersonationTable))
		}
	}
	sort.Strings(summaries)

	return summaries, notes
}
