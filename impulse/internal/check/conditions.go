package check

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// conditionsProven verifies that every conditional grant in a roles file the deployment
// provisions is proven by a test case: one that calls the harness helper provesGrant with
// the grant's coordinates, over seeded rows. The generated authorization matrix runs a
// fake engine with unconditional grants and the deploy-time validation reads the grammar,
// so whether a condition does what its author meant on real rows is proven nowhere else.
// The check reads the call; the helper proves at test time that the call still matches
// the file.
type conditionsProven struct{}

// conditionsProvenName is the check's name.
const conditionsProvenName = "conditions-proven"

func (conditionsProven) Name() string { return conditionsProvenName }

func (conditionsProven) Describe() string {
	return "every conditional grant in a provisioned roles file is named by a test case calling provesGrant(t, <auth>.RolesPath, role, permission, resource, condition), the harness helper that fails when the case no longer matches the file"
}

// Meaning explains the obligation for the handoff brief: how a conditional grant is proven.
func (conditionsProven) Meaning() string {
	return "A conditional grant is proven by one test case over seeded rows, Lodestar's pattern: seed rows on both sides of the condition in `schema/devseed` (the first conditional grant in an application brings that directory with it; `impulse add tenancy` already creates one), a login holding the role in `cmd/bootstrap/users.json`, the request through the served harness in `test/integration`, and the expectation derived from the grant and the seed, never from an observed response: a row the condition admits answers, a row it refuses is refused (a read answers not found, a write forbidden). Write the case table-driven, and name the grant it proves with the harness helper, `provesGrant(t, <auth>.RolesPath, role, permission, resource, condition)`, whose arguments are literals: the helper reads the roles file when the test runs and fails when the grant is gone or its condition reads differently, and the conditions-proven check reads the call to find the grants no case names. Unconditional grants need no case; the generic test in `test/integration/grants_test.go` proves every one of them live. Run `impulse check` until it is clean."
}

// grant is one conditional grant's coordinates inside a roles file.
type grant struct {
	Role       string
	Permission string
	Resource   string
	Condition  string
}

// rolesFile is a roles file the deployment provisions, with the auth package naming it.
type rolesFile struct {
	Auth *app.AuthPackage
	// Path is the root-relative roles file.
	Path string
}

func (c conditionsProven) Run(_ context.Context, env *Env) Result {
	a := env.App
	if len(a.AuthPackages) == 0 {
		return skip(c.Name(), "no auth package")
	}
	files := provisionedRolesFiles(a)
	if len(files) == 0 {
		return skip(c.Name(), "no roles file reaches access.MigrateRoles (see auths-wired)")
	}

	proofs, details := readProofs(a, files)
	var proven, unproven int
	var counts []string
	for _, f := range files {
		grants, err := conditionalGrants(a.Abs(f.Path))
		if err != nil {
			details = append(details, fmt.Sprintf("%s: cannot be read: %v", f.Path, errors.Cause(err)))

			continue
		}
		claimed := proofs[f.Path]
		provenHere := 0
		for _, g := range grants {
			if _, ok := claimed[g]; ok {
				delete(claimed, g)
				provenHere++

				continue
			}
			unproven++
			details = append(details, fmt.Sprintf("%s: %s %s %s under %q is proven by no test case; write one over seeded rows and name the grant in it: provesGrant(t, %s.RolesPath, %q, %q, %q, %q)",
				f.Path, g.Role, g.Permission, g.Resource, g.Condition, f.Auth.Name, g.Role, g.Permission, g.Resource, g.Condition))
		}
		details = append(details, staleProofs(f.Path, claimed)...)
		proven += provenHere
		if len(grants) == 0 {
			counts = append(counts, fmt.Sprintf("%s: 0 conditional grants", f.Path))
		} else {
			counts = append(counts, fmt.Sprintf("%s: %d conditional grant(s) proven", f.Path, provenHere))
		}
	}
	if len(details) > 0 {
		summary := fmt.Sprintf("%d conditional grant(s) proven by no test case", unproven)
		if problems := len(details) - unproven; problems > 0 {
			summary += fmt.Sprintf("; %d provesGrant call(s) or roles file(s) the check cannot read", problems)
		}

		return fail(c.Name(), summary, details...)
	}
	result := pass(c.Name(), fmt.Sprintf("%d conditional grant(s) across %d roles file(s), each proven by a test case", proven, len(files)))
	result.Details = counts

	return result
}

// provisionedRolesFiles lists the roles files the deployment provisions: each auth
// package's RolesPath that a file migrating roles reads, when the constant is readable.
func provisionedRolesFiles(a *app.App) []rolesFile {
	var files []rolesFile
	for i := range a.AuthPackages {
		p := &a.AuthPackages[i]
		provisioned := false
		for _, file := range p.References(authRolesPath, isTestFile) {
			for _, m := range a.RoleMigrations {
				if m.File == file {
					provisioned = true
				}
			}
		}
		if !provisioned {
			continue
		}
		if rel := rolesPathOf(a, p); rel != "" {
			files = append(files, rolesFile{Auth: p, Path: rel})
		}
	}

	return files
}

// readProofs groups the provesGrant calls by the roles file they name, each grant to the
// positions claiming it, and returns the calls the check cannot read as findings.
func readProofs(a *app.App, files []rolesFile) (proofs map[string]map[grant][]string, details []string) {
	byPackage := map[string]string{}
	for _, f := range files {
		byPackage[f.Auth.Path] = f.Path
	}
	proofs = map[string]map[grant][]string{}
	for i := range a.GrantProofs {
		p := &a.GrantProofs[i]
		pos := fmt.Sprintf("%s:%d", p.File, p.Line)
		if p.Problem != "" {
			details = append(details, fmt.Sprintf("%s: provesGrant %s, so the check cannot read which grant the case proves", pos, p.Problem))

			continue
		}
		file := p.RolesPath
		if p.RolesPackage != "" {
			var ok bool
			if file, ok = byPackage[p.RolesPackage]; !ok {
				details = append(details, fmt.Sprintf("%s: provesGrant names the RolesPath of %s, which is not an auth package whose roles file the deployment provisions", pos, p.RolesPackage))

				continue
			}
		}
		if proofs[file] == nil {
			proofs[file] = map[grant][]string{}
		}
		g := grant{Role: p.Role, Permission: p.Permission, Resource: p.Resource, Condition: p.Condition}
		proofs[file][g] = append(proofs[file][g], pos)
	}

	return proofs, details
}

// staleProofs lists the calls left over after matching: each names a grant the file does
// not carry, so its case fails at test time until the call matches the file again.
func staleProofs(file string, leftover map[grant][]string) []string {
	var details []string
	for g, positions := range leftover {
		sort.Strings(positions)
		for _, pos := range positions {
			details = append(details, fmt.Sprintf("%s: provesGrant names a grant %s does not carry (%s %s %s under %q); the case fails at test time until the call matches the file", pos, file, g.Role, g.Permission, g.Resource, g.Condition))
		}
	}
	sort.Strings(details)

	return details
}

// rolesDocument is the generic shape of a roles file: the roles by scope, each role's
// grants by permission, and on each grant the resource and the condition it may carry.
// It is read generically so a roles file of any application is read the same way.
type rolesDocument struct {
	Roles struct {
		Global []rolesRole `json:"global"`
		Domain []rolesRole `json:"domain"`
	} `json:"roles"`
}

type rolesRole struct {
	Name        string `json:"name"`
	Permissions map[string][]struct {
		Resource  string `json:"resource"`
		Condition string `json:"condition"`
	} `json:"permissions"`
}

// conditionalGrants reads the conditional grants of a roles file in file order: the
// global roles, then the domain roles, each role's permissions by name, each permission's
// grants as written.
func conditionalGrants(abs string) ([]grant, error) {
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	var doc rolesDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal()")
	}
	var grants []grant
	for _, role := range slices.Concat(doc.Roles.Global, doc.Roles.Domain) {
		permissions := make([]string, 0, len(role.Permissions))
		for p := range role.Permissions {
			permissions = append(permissions, p)
		}
		sort.Strings(permissions)
		for _, p := range permissions {
			for _, g := range role.Permissions[p] {
				if g.Condition == "" {
					continue
				}
				grants = append(grants, grant{Role: role.Name, Permission: p, Resource: g.Resource, Condition: g.Condition})
			}
		}
	}

	return grants, nil
}
