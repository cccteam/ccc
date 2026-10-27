package transition

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

// The tenant declaration: who may enter a tenant, kept on the tenant record as the
// directories the tenant admits its people through (a child record, one row per
// directory) and whether they are the only way in (a column on the record). The
// sign-in rule reads it in the sign-in's transaction through the generated reader, and
// every instance's roster keeps a copy that filters a session's tenant list.
const (
	declarationsAccessor = "TenantDeclarations"
	// directoriesAccessor is the Settings field the directory reader travels under.
	directoriesAccessor = "TenantDirectories"
	signinImportPath    = "github.com/cccteam/session/signin"
	spannerImportName   = "cloudspanner"
)

// DirectoryRecord is the directories record's struct name: the tenant record's with
// Directory, so Tenant's is TenantDirectory.
func (tn Tenancy) DirectoryRecord() string { return tn.Record() + "Directory" }

// DirectoryTable is the directories table: the directories record's plural.
func (tn Tenancy) DirectoryTable() string { return tn.Record() + "Directories" }

// DirectorySegment is the directories record's route under the tenant segment.
func (tn Tenancy) DirectorySegment() string { return strcase.ToKebab(tn.DirectoryTable()) }

// DeclarationReader is the generated reader's constructor in the handlers package:
// New<Record>DeclarationReader.
func (tn Tenancy) DeclarationReader() string { return "New" + tn.Record() + "DeclarationReader" }

// DirectoryReader is the generated directory reader's constructor in the handlers
// package: New<Record>DirectoryReader, the reader of every tenant's declaration of one
// directory with its default seat, which the auth's directory admission decides with.
func (tn Tenancy) DirectoryReader() string { return "New" + tn.Record() + "DirectoryReader" }

// writeDirectoryRecord adds the directories record beside the tenant record.
func (tn Tenancy) writeDirectoryRecord(a *app.App, resourceDir string, ch *Change) error {
	rel := path.Join(resourceDir, strcase.ToSnake(tn.DirectoryTable())+".go")
	if _, err := os.Stat(a.Abs(rel)); err == nil {
		ch.skipf("%s already exists, so the %s resource struct was not written; make sure it is a @resource annotated @permissionScope(domain) and @tenantDirectory, with a bare @domain column, a @directoryMethod column, a @directoryConnection column and a nullable @directoryDefaultSeat column", rel, tn.DirectoryRecord())

		return nil
	}
	pkg := packageNameOf(a, resourceDir)
	src := fmt.Sprintf(`package %[1]s

import (
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// %[2]s is a directory a tenant admits its people through (@tenantDirectory),
	// one row per declaration: the sign-in method the directory is reached through
	// (google, azure or workos, as the session library spells them) and the connection
	// the method's identities carry, which is what the session's first auth event
	// records: a Google Workspace by its domain or by the name the registration gives
	// it, a Microsoft Entra tenant by its tenant id. One domain, one directory by
	// default; a second directory is an explicit second row. A tenant's own
	// administrator declares its directories under the record's Create and Delete
	// grants, and the change events are the audit.
	//
	// DefaultSeat is what a stranger the directory vouches for gets at their first
	// sign-in: the tenant's role they are seated with, or NULL, and the tenant admits
	// them by invitation only (they are refused with no account, and an invitation is
	// the way in). The tenant's administrator sets it on the Sign-in page, a picker
	// over the tenant's roles.
	//
	// The tenancy column is on the row itself (the bare @domain binding), as the
	// directories record requires: a directory is the tenant's own declaration, stamped
	// from the request's tenant on create, never written by the wire.
	//
	// @resource
	// @permissionScope(domain)
	// @tenantDirectory
	// @order(Method asc, Connection asc)
	%[2]s struct {
		ID ccc.UUID `+"`spanner:\"Id\"`"+`
		// @domain
		%[3]sID string `+"`spanner:\"%[3]sId\"`"+`
		// @directoryMethod
		Method string `+"`spanner:\"Method\"`"+`
		// @directoryConnection
		Connection string `+"`spanner:\"Connection\"`"+`
		// @directoryDefaultSeat
		DefaultSeat *string `+"`spanner:\"DefaultSeat\"`"+`
	}
)

// Config enables change tracking: %[2]s mutations write DataChangeEvents rows in
// the same transaction, the audit of every directory declared or withdrawn.
func (%[2]s) Config() resource.Config {
	return %[4]s
}
`, pkg, tn.DirectoryRecord(), tn.Record(), trackingConfig(a, resourceDir))
	if err := writeNew(a, rel, src); err != nil {
		return err
	}
	ch.didf("%s: the %s resource struct, the tenant's declared directories, tenant-scoped under /%s/{%sID}/%s", rel, tn.DirectoryRecord(), tn.Segment(), strcase.ToCamel(tn.Record()), tn.DirectorySegment())

	return nil
}

// trackingConfig is the expression of a resource config with change tracking on: the
// package's own defaultConfig with tracking set when the package declares one (the
// skeletons do), a literal otherwise.
func trackingConfig(a *app.App, resourceDir string) string {
	entries, err := os.ReadDir(a.Abs(resourceDir))
	if err != nil {
		return "resource.Config{TrackChanges: true}"
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(a.Abs(path.Join(resourceDir, e.Name())))
		if err != nil {
			continue
		}
		if strings.Contains(string(src), "func defaultConfig() resource.Config") {
			return "defaultConfig().SetTrackChanges(true)"
		}
	}

	return "resource.Config{TrackChanges: true}"
}

// editAuths sets the sign-in rule on every auth and hands each the generated reader:
// the auth's Settings carry the reader, New refuses a nil one and sets the library's
// tenant policy over it on the session, and the data level's settings literal passes
// the reader in.
func (tn Tenancy) editAuths(a *app.App, g *app.Generator, ch *Change) error {
	if len(a.AuthPackages) == 0 {
		ch.skipf("no auth package constructs a session.Auth, so no sign-in rule was set; where one is, set signin.TenantPolicy(resource.SignInRoles(<engine>.UserManager()), <handlers>.%s()) on the session with SetSignInPolicy before the first request, so a sign-in is admitted by what the person's tenants declare", tn.DeclarationReader())

		return nil
	}
	for i := range a.AuthPackages {
		pkg := &a.AuthPackages[i]
		var auth *app.Auth
		for j := range a.Auths {
			if path.Dir(a.Auths[j].File) == pkg.Dir {
				auth = &a.Auths[j]

				break
			}
		}
		if auth == nil {
			ch.skipf("%s: the %s auth constructs no session.NewAuth the scan reads, so no sign-in rule was set on it; set signin.TenantPolicy over the generated reader on its session with SetSignInPolicy", pkg.Dir, pkg.Name)

			continue
		}
		if err := tn.editAuth(a, auth, pkg, packageNameOf(a, g.HandlersDir())+"."+tn.DeclarationReader(), packageNameOf(a, g.HandlersDir())+"."+tn.DirectoryReader(), ch); err != nil {
			return err
		}
		if err := tn.editSettings(a, g, auth, pkg, ch); err != nil {
			return err
		}
	}

	return nil
}

// directoryAuthority reports an auth with a registration whose directory writes the
// roles: the directory is the authority for everyone the auth holds, so the auth sets
// no directory admission (its strangers are provisioned with the roles the directory
// names).
func directoryAuthority(auth *app.Auth) bool {
	for i := range auth.Methods {
		if auth.Methods[i].Authority == app.AuthorityDirectory {
			return true
		}
	}

	return false
}

// editAuth gives the auth's Settings the readers (reader and directoryReader are their
// constructors as the data level qualifies them) and its New the sign-in rule and, on
// an auth whose roles the application assigns, the directory admission, worded as the
// rendered auth packages word them.
func (tn Tenancy) editAuth(a *app.App, auth *app.Auth, pkg *app.AuthPackage, reader, directoryReader string, ch *Change) error {
	rel := auth.File
	src, mode, err := readFile(a, rel)
	if err != nil {
		return err
	}
	spanner, err := app.ImportName(rel, src, spannerImportPath)
	if err != nil {
		return err
	}
	edited := src
	if spanner == "" {
		spanner = spannerImportName
		if edited, err = app.AddNamedImport(rel, edited, spannerImportName, spannerImportPath); err != nil {
			return err
		}
	}
	field := fmt.Sprintf("// %[1]s reads what a tenant declares about who may enter it, in the\n// sign-in's transaction, which the sign-in rule decides with: the generated reader\n// over the %[2]s record and its directories (%[3]s).\n// Required; New refuses nil.\n%[1]s signin.DeclarationReader[*%[4]s.ReadWriteTransaction]", declarationsAccessor, tn.Record(), reader, spanner)
	admission := !directoryAuthority(auth)
	edited, err = app.AddStructField(rel, edited, "Settings", field)
	if errors.Is(err, app.ErrNoAnchor) {
		ch.skipf("%s: no Settings struct to carry the reader; hand %s.%s() to the %s auth where it is built and set signin.TenantPolicy over it on its session", rel, pkg.Name, tn.DeclarationReader(), pkg.Name)

		return nil
	} else if err != nil {
		return err
	}
	if admission {
		directoryField := fmt.Sprintf("// %[1]s reads every tenant's declaration of one directory with the\n// default seat its row carries, in the sign-in's transaction, which the directory\n// admission decides a stranger's first sign-in with: the generated reader\n// (%[2]s). Required; New refuses nil.\n%[1]s signin.DirectoryReader[*%[3]s.ReadWriteTransaction]", directoriesAccessor, directoryReader, spanner)
		if edited, err = app.AddStructField(rel, edited, "Settings", directoryField); err != nil {
			return err
		}
	}
	for _, imp := range []string{signinImportPath, resourceImportPath} {
		if edited, err = app.AddImport(rel, edited, imp); err != nil {
			return err
		}
	}
	checked := ""
	if ok, _ := app.HasImport(rel, edited, errorsImportPath); ok {
		withCheck, err := app.AddStatementsBeforeCall(rel, edited, "", "New", "spannerstore.New", nilReaderChecks(pkg.Name, reader, directoryReader, admission))
		switch {
		case errors.Is(err, app.ErrNoAnchor):
		case err != nil:
			return err
		default:
			edited = withCheck
			checked = ", New refuses a nil one"
		}
	}
	session := auth.Var
	if session == "" {
		session = "authSession"
	}
	policy := signInRule(session, admission)
	loc := returnAuthRE.FindIndex(edited)
	if loc == nil {
		ch.skipf("%s: New does not end in `return &Auth{db: db, access: accessClient, handlers: handlers}, nil` as the skeleton's does, so the sign-in rule was not set; call %s.SetSignInPolicy(signin.TenantPolicy(resource.SignInRoles(accessClient.UserManager()), settings.%s)) before the first request", rel, session, declarationsAccessor)
	} else {
		edited = append(edited[:loc[0]:loc[0]], append([]byte(policy), edited[loc[0]:]...)...)
	}
	if err := os.WriteFile(a.Abs(rel), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: Settings carries %s", rel, authCarries(checked, loc != nil, admission))

	return nil
}

// nilReaderChecks is the refusal New makes of a nil reader in its Settings: the
// declaration reader, and the directory reader where the admission is set.
func nilReaderChecks(pkgName, reader, directoryReader string, admission bool) string {
	check := fmt.Sprintf("\tif settings.%[1]s == nil {\n\t\treturn nil, errors.New(\"%[2]s.Settings.%[1]s is nil: the sign-in rule reads what each tenant declares about who may enter it through the generated reader (%[3]s), which the data level hands in\")\n\t}", declarationsAccessor, pkgName, reader)
	if admission {
		check += fmt.Sprintf("\n\tif settings.%[1]s == nil {\n\t\treturn nil, errors.New(\"%[2]s.Settings.%[1]s is nil: the directory admission reads what the tenants declare about one directory, its default seat included, through the generated reader (%[3]s), which the data level hands in\")\n\t}", directoriesAccessor, pkgName, directoryReader)
	}

	return check
}

// authCarries words what the auth's Settings carries after the edit, for the change's
// did line: the readers, whether New refuses a nil one, and the rules New sets.
func authCarries(checked string, ruleSet, admission bool) string {
	set := ""
	if ruleSet {
		set = ", and New sets the sign-in rule on the session (signin.TenantPolicy over the reader, the memberships read through resource.SignInRoles)"
		if admission {
			set += " and the directory admission (session.Auth.SetDirectoryAdmission over the directory reader, the seats written through resource.SeatDeclared)"
		}
	}
	carried := "the declaration reader (" + declarationsAccessor + ")"
	if admission {
		carried = "the declaration reader (" + declarationsAccessor + ") and the directory reader (" + directoriesAccessor + ")"
	}

	return carried + checked + set
}

// signInRule is the source set before New's return: the tenant policy on the session
// and, on an auth whose roles the application assigns, the directory admission.
func signInRule(session string, admission bool) string {
	policy := fmt.Sprintf("\t// The sign-in rule: the library's tenant policy. A person's tenants are where they\n\t// hold a role (the engine's memberships), and what each tenant admits is its\n\t// declaration, read in the sign-in's transaction: a password is admitted unless\n\t// every one of the person's tenants admits its people through directories only, a\n\t// sign-in through a directory when one of their tenants declares it (its method and\n\t// the connection its identities carry); the application's own staff (a global role,\n\t// or one held in every tenant) are admitted on the library's checks.\n\t// A person who holds no tenant is refused (no_tenant), since the administrator\n\t// creates tenants here; a record created by its own people (@tenant(creation:\n\t// self-service) or both) adds signin.AdmitWithoutTenant().\n\t// Which tenants the session then reaches is decided again, tenant by tenant, where\n\t// its tenant list is computed (UserPermissions). The memberships are read outside\n\t// the sign-in's transaction (resource.SignInRoles): the Spanner client refuses the\n\t// engine's read under the transaction's context as a nested transaction.\n\t%s.SetSignInPolicy(signin.TenantPolicy(resource.SignInRoles(accessClient.UserManager()), settings.%s))\n", session, declarationsAccessor)
	if admission {
		policy += fmt.Sprintf("\n\t// The directory admission: what a stranger gets at their first sign-in through a\n\t// directory a tenant declares, before the registration's account rule is reached.\n\t// The directory rows the tenants declare for the identity's connection decide: a\n\t// row with a default seat creates the person and seats them with it (written\n\t// through the engine once the sign-in's transaction has committed, resource.SeatDeclared,\n\t// before the sign-in rule decides, so the rule finds the membership); rows with\n\t// none refuse them (invitation_only) and no account is created, unless they\n\t// followed an invitation link, which is the way in; a person already seated\n\t// somewhere is the tenant's own and passes. A directory no tenant declares falls to\n\t// the registration's rule. The auth's roles are the application's, so the admission\n\t// is set here; an auth whose directory writes the roles sets none.\n\t%s.SetDirectoryAdmission(settings.%s, resource.SignInRoles(accessClient.UserManager()), resource.SeatDeclared(accessClient.UserManager()))\n", session, directoriesAccessor)
	}

	return policy
}

// editSettings passes the generated readers in the data level's settings literal of
// the auth: the declaration reader, and the directory reader on an auth whose roles
// the application assigns.
func (tn Tenancy) editSettings(a *app.App, g *app.Generator, auth *app.Auth, pkg *app.AuthPackage, ch *Change) error {
	handlersPath := a.GoMod.Module.Mod.Path + "/" + g.HandlersDir()
	handlersPkg := packageNameOf(a, g.HandlersDir())
	reader := handlersPkg + "." + tn.DeclarationReader() + "()"
	directoryReader := handlersPkg + "." + tn.DirectoryReader() + "()"
	rel, src, mode, err := findDeclaringFile(a, "DataConfiguration")
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no file declares a DataConfiguration struct, so the %s auth is not handed the declaration reader; pass %s in its Settings (%s) where it is built", pkg.Name, reader, declarationsAccessor)

		return nil
	}
	structName := ""
	if env := envVarRE.FindSubmatch(src); env != nil {
		structName = string(env[1])
	}
	place := settingsPlace(rel, src, structName, pkg.Name)
	edited, err := place.add(src, declarationsAccessor+": "+reader)
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		ch.skipf("%s: no %sSettings method and no %s.Settings{...} literal in NewDataConfiguration, so the %s auth is not handed the declaration reader; pass %s in its Settings (%s) where it is built", rel, pkg.Name, pkg.Name, pkg.Name, reader, declarationsAccessor)

		return nil
	case err != nil:
		return err
	}
	passed := "the declaration reader (" + reader + ")"
	if !directoryAuthority(auth) {
		if edited, err = place.add(edited, directoriesAccessor+": "+directoryReader); err != nil {
			return err
		}
		passed = "the declaration reader (" + reader + ") and the directory reader (" + directoryReader + ")"
	}
	if edited, err = app.AddImport(rel, edited, handlersPath); err != nil {
		return err
	}
	if err := os.WriteFile(a.Abs(rel), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: %s passes %s to the %s auth", rel, place.name, passed, pkg.Name)

	return nil
}
