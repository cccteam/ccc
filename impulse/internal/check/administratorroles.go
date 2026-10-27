package check

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// administratorRoles verifies that every user surface the application builds knows its
// auth's administrator roles: each call of a generated surface constructor
// (New<Auth>UserSurface, in the router package) outside generated files passes
// resource.WithAdministratorRoles, the skeletons' being
// resource.WithAdministratorRoles(<auth>.AdministratorRoles()). The surface judges a
// self-unseat by the last-owner rule, refusing it only for the last holder of a
// partition's administrator role, and a surface built without the option knows no
// administrator role and refuses none. An auth whose directory names its roles (every
// registered method carries RoleSync) defines no AdministratorRoles(), since the
// directory writes every seat, and builds its surface without the option, which the
// check reads as satisfied. The constructor takes the option among its variadic
// options, so nothing compiles against it, and this check holds it.
type administratorRoles struct{}

func (administratorRoles) Name() string {
	return "administrator-roles"
}

func (administratorRoles) Describe() string {
	return "every user surface the application builds (a call of a generated New<Auth>UserSurface constructor outside generated files) passes resource.WithAdministratorRoles(<auth>.AdministratorRoles()), so a self-unseat is refused only for the last holder of a partition's administrator role; an auth whose directory names its roles builds its surface without the option"
}

// The generated user surface constructor's shape (New<Auth>UserSurface), the option
// handing the surface its auth's administrator roles, and the auth package function the
// skeletons hand it.
var surfaceConstructorRE = regexp.MustCompile(`^New([A-Z][A-Za-z0-9]*)UserSurface$`)

const (
	administratorRolesOption = "WithAdministratorRoles"
	administratorRolesFunc   = "AdministratorRoles"
)

// surfaceCall is one call of a generated user surface constructor: where, the
// constructor, the auth package the constructor is named for, whether the call passes
// the administrator roles option, whether the auth's directory names its roles (so the
// surface takes none), and whether the call forwards its caller's options (opts...),
// which hides them.
type surfaceCall struct {
	file        string
	line        int
	constructor string
	auth        string
	roles       bool
	directory   bool
	forwarded   bool
}

func (c administratorRoles) Run(_ context.Context, env *Env) Result {
	a := env.App
	calls, err := surfaceCalls(a)
	if err != nil {
		return fail(c.Name(), err.Error())
	}
	if len(calls) == 0 {
		return skip(c.Name(), "no user surface is built (no file outside generated files calls a New<Auth>UserSurface constructor)")
	}

	var details, notes, lines, files, exemptFiles []string
	taken, exempt := 0, 0
	for i := range calls {
		call := &calls[i]
		pos := fmt.Sprintf("%s:%d", call.file, call.line)
		switch {
		case call.roles:
			taken++
			lines = append(lines, fmt.Sprintf("%s: %s takes resource.%s", pos, call.constructor, administratorRolesOption))
			if !slices.Contains(files, call.file) {
				files = append(files, call.file)
			}
		case call.directory:
			exempt++
			lines = append(lines, fmt.Sprintf("%s: %s names no administrator roles: the %s auth's directory names its roles (every method is registered with RoleSync)", pos, call.constructor, call.auth))
			if !slices.Contains(exemptFiles, call.file) {
				exemptFiles = append(exemptFiles, call.file)
			}
		case call.forwarded:
			notes = append(notes, fmt.Sprintf("%s: %s forwards its caller's options (opts...), so whether resource.%s is among them was not read", pos, call.constructor, administratorRolesOption))
		default:
			details = append(details, fmt.Sprintf("%s: %s is built without resource.%s; the user surface takes resource.%s(%s.%s()) so a self-unseat is refused only for the last holder of a partition's administrator role", pos, call.constructor, administratorRolesOption, administratorRolesOption, call.auth, administratorRolesFunc))
		}
	}
	switch {
	case len(details) > 0:
		return fail(c.Name(), fmt.Sprintf("%d user surface(s) built without resource.%s", len(details), administratorRolesOption), append(details, notes...)...)
	case len(notes) > 0:
		return warn(c.Name(), fmt.Sprintf("%d user surface(s) forward their caller's options, so whether they take resource.%s was not read", len(notes), administratorRolesOption), append(notes, lines...)...)
	default:
		var parts []string
		if taken > 0 {
			parts = append(parts, fmt.Sprintf("%d user surface(s) take resource.%s: %s", taken, administratorRolesOption, strings.Join(files, ", ")))
		}
		if exempt > 0 {
			parts = append(parts, fmt.Sprintf("%d user surface(s) of an auth whose directory names its roles are built without resource.%s: %s", exempt, administratorRolesOption, strings.Join(exemptFiles, ", ")))
		}

		return passWithDetails(c.Name(), strings.Join(parts, "; "), lines...)
	}
}

// surfaceCalls finds the calls of a generated user surface constructor in the
// application's own code: every non-test Go file but the generated ones, in path
// order, each call with what it passes.
func surfaceCalls(a *app.App) ([]surfaceCall, error) {
	var calls []surfaceCall
	for _, rel := range a.GoFiles() {
		if strings.HasPrefix(path.Base(rel), "zz_gen_") {
			continue
		}
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		if !bytes.Contains(src, []byte("UserSurface(")) {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, errors.Wrap(err, "parser.ParseFile()")
		}
		resourcePkg := importName(f, resourceModule)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			m := surfaceConstructorRE.FindStringSubmatch(finalName(call.Fun))
			if m == nil {
				return true
			}
			found := surfaceCall{file: rel, line: fset.Position(call.Pos()).Line, constructor: m[0], auth: authPackageFor(a, m[1]), directory: directoryNamed(a, m[1]), forwarded: call.Ellipsis.IsValid()}
			for _, arg := range call.Args {
				if option, ok := arg.(*ast.CallExpr); ok && resourcePkg != "" && isQualified(option.Fun, resourcePkg, administratorRolesOption) {
					found.roles = true
				}
			}
			calls = append(calls, found)

			return true
		})
	}

	return calls, nil
}

// authPackageFor names the auth package a generated surface constructor is named for
// (NewStaffUserSurface for the staff auth): the application's package of that name,
// or the name as the constructor spells it with its first letter lowered.
func authPackageFor(a *app.App, auth string) string {
	for i := range a.AuthPackages {
		if strings.EqualFold(a.AuthPackages[i].Name, auth) {
			return a.AuthPackages[i].Name
		}
	}

	return strings.ToLower(auth[:1]) + auth[1:]
}

// directoryNamed reports whether the auth a surface constructor is named for has its
// roles named by its directory: the application's auth package of that name constructs
// an Auth whose every registered method carries RoleSync (no method of application
// authority, as session-tables classifies them), so the package defines no
// AdministratorRoles(), the directory writing every seat, and its surface is built
// without the option on purpose. An auth the scan reads no construction of is held like
// any other.
func directoryNamed(a *app.App, auth string) bool {
	for i := range a.AuthPackages {
		p := &a.AuthPackages[i]
		if !strings.EqualFold(p.Name, auth) {
			continue
		}
		constructed := authOf(a, p)
		if constructed == nil || len(constructed.Methods) == 0 {
			return false
		}
		for j := range constructed.Methods {
			if constructed.Methods[j].Authority != app.AuthorityDirectory {
				return false
			}
		}

		return true
	}

	return false
}

// isQualified reports whether a call's function is the package's named identifier
// (resource.WithAdministratorRoles).
func isQualified(fun ast.Expr, pkg, name string) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)

	return ok && id.Name == pkg
}

// Meaning explains the invariant for the brief.
func (administratorRoles) Meaning() string {
	return "A user surface knows its auth's administrator roles, so it judges a self-unseat by the last-owner rule: a person may drop their own administrator role unless they are the last holder of it in the partition, which is refused, in place of refusing every self-unseat. The generated constructor (`New<Auth>UserSurface` in the router package) takes the roles as an option, `resource.WithAdministratorRoles(<auth>.AdministratorRoles())`, the auth package's roles by where they are held (the global roles, and the domain role held in every tenant), and a surface built without it knows no administrator role and refuses no self-unseat. An auth whose directory names its roles (every method registered with `RoleSync`) defines no `AdministratorRoles()`, since the directory writes every seat, and builds its surface without the option, which the check reads as satisfied. The skeletons build each surface in `app/app.go` (`router.NewStaffUserSurface(accounts, seats, ..., resource.WithAdministratorRoles(staff.AdministratorRoles()))`); an application builds its own by hand, which nothing compiles against, and this check holds it."
}
