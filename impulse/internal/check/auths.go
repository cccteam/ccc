package check

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// authsWired verifies that every auth package is wired through the application: the data
// level constructs it, a roles migration provisions its store from its roles file, and a
// surface binds to it by taking its type, so a population the application declares can
// sign in somewhere and holds roles. auth-wired holds each authenticator to its tables;
// this holds each auth to the application around it.
type authsWired struct{}

// authsWiredName is the check's name.
const authsWiredName = "auths-wired"

func (authsWired) Name() string { return authsWiredName }

func (authsWired) Describe() string {
	return "every auth package is constructed by the data level, provisioned from its roles file, and bound by a surface, and a directory-run auth has no role writers in the application"
}

// The identifiers an auth package exports that the wiring is read from.
const (
	authNew       = "New"
	authRolesPath = "RolesPath"
	authType      = "Auth"
)

func (c authsWired) Run(_ context.Context, env *Env) Result {
	a := env.App
	if len(a.AuthPackages) == 0 {
		if len(a.Auths) == 0 {
			return skip(c.Name(), "no session authenticator is constructed")
		}

		return warn(c.Name(), "no auth package: the authenticators are constructed outside pkg/auth/<name> packages, so the auths cannot be told apart", authFiles(a)...)
	}

	var details, summaries []string
	for i := range a.AuthPackages {
		p := &a.AuthPackages[i]
		details = append(details, c.packageFindings(a, p)...)
		summaries = append(summaries, p.Name)
	}
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d auth wiring problem(s)", len(details)), details...)
	}

	return pass(c.Name(), fmt.Sprintf("%d auth(s) constructed, provisioned, and bound: %s", len(summaries), strings.Join(summaries, ", ")))
}

// packageFindings checks one auth against the application.
func (authsWired) packageFindings(a *app.App, p *app.AuthPackage) []string {
	var details []string
	if len(p.References(authNew, isTestFile)) == 0 {
		details = append(details, fmt.Sprintf("%s: nothing outside tests calls %s.New; the %s auth is never constructed", p.Dir, p.Name, p.Name))
	}

	if refs := p.References(authRolesPath, isTestFile); len(refs) == 0 {
		details = append(details, fmt.Sprintf("%s: nothing outside tests reads %s.RolesPath; the %s auth's roles are never provisioned", p.Dir, p.Name, p.Name))
	} else {
		provisioning := false
		for _, file := range refs {
			for _, m := range a.RoleMigrations {
				if m.File == file {
					provisioning = true
				}
			}
		}
		if !provisioning {
			details = append(details, fmt.Sprintf("%s: %s.RolesPath is read (%s) but not by a file that migrates roles; the %s auth's roles are never provisioned", p.Dir, p.Name, strings.Join(refs, ", "), p.Name))
		}
		if rolesFile := rolesPathOf(a, p); rolesFile != "" {
			if _, err := os.Stat(a.Abs(rolesFile)); err != nil {
				details = append(details, fmt.Sprintf("%s: %s.RolesPath names %s, which does not exist", p.Dir, p.Name, rolesFile))
			}
		}
	}

	bound := p.References(authType, func(file string) bool {
		return isTestFile(file) || isDataLevel(file) || strings.HasPrefix(file, "cmd/")
	})
	if len(bound) == 0 {
		details = append(details, fmt.Sprintf("%s: no surface takes *%s.Auth; nothing binds to the %s auth, so its people can sign in nowhere", p.Dir, p.Name, p.Name))
	}

	if authorityOf(a, p) == app.AuthorityDirectory {
		details = append(details, directoryWriters(a, p)...)
	}

	return details
}

// authorityOf returns the role-membership authority of the auth package's constructor.
func authorityOf(a *app.App, p *app.AuthPackage) string {
	for i := range a.Auths {
		if path.Dir(a.Auths[i].File) == p.Dir {
			return a.Auths[i].Authority
		}
	}

	return ""
}

// directoryWriters finds role assignments in the store of an auth whose membership is the
// directory's: a role assigned in the application is removed at the person's next login,
// so the two cannot coexist. It reads every non-test file for a role-writer call
// (AddUserRoles, AddRoleUsers, DeleteUserRoles, DeleteRoleUsers) whose receiver is the
// auth's user manager, reached through the auth's accessor (<Name>()) or its variable
// (<name>Auth), and for the user manager handed to a function of the same file that
// calls a role writer.
func directoryWriters(a *app.App, p *app.AuthPackage) []string {
	var details []string
	for _, rel := range a.GoFiles() {
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			continue
		}
		for _, line := range app.RoleWriters(rel, src, p.Name) {
			details = append(details, fmt.Sprintf("%s:%d: the %s auth hands role membership to the directory (session.RoleSync), but this assigns roles in its store; the directory removes them at the next login. Assign the roles in the directory, or hand membership to the application (session.DisableRoleSync)", rel, line, p.Name))
		}
	}

	return details
}

// rolesPathOf reads the auth package's RolesPath constant.
func rolesPathOf(a *app.App, p *app.AuthPackage) string {
	entries, err := os.ReadDir(a.Abs(p.Dir))
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(a.Abs(path.Join(p.Dir, e.Name())))
		if err != nil {
			continue
		}
		if value, ok := app.ConstString(e.Name(), src, authRolesPath); ok {
			return value
		}
	}

	return ""
}

func isTestFile(file string) bool { return strings.HasSuffix(file, "_test.go") }

// isDataLevel reports a file of a config package: the level that constructs the auths
// rather than a surface that binds to one.
func isDataLevel(file string) bool { return path.Base(path.Dir(file)) == "config" }

// authFiles lists where authenticators are constructed, for the warning.
func authFiles(a *app.App) []string {
	files := make([]string, 0, len(a.Auths))
	for i := range a.Auths {
		auth := &a.Auths[i]
		files = append(files, fmt.Sprintf("%s:%d: %s", auth.File, auth.Line, auth.Flavor))
	}

	return files
}
