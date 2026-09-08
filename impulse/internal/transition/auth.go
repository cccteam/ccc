package transition

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/names"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

// Auth adds an auth to the application: a population that signs in one way and holds
// roles in its own permission store. An auth is a package, pkg/auth/<name>, copied from
// an auth the application already has with every name substituted, so the new one owns
// its own tables, cookie, store prefix, and roles file from the start; an auth whose
// people sign in through a directory (the OIDC flavors) is copied from the reference
// skeleton's when the application has none. The data level constructs it. Binding a site
// or an outlet to it, the users it starts with, and the tests that prove its people are
// strangers to the other auths are the agent's.
type Auth struct {
	// Name is the auth's lowercase name, the population it serves: partners, learners.
	Name string
	// Flavor is the login flavor: password, preauth, oidc-azure, or oidc-google.
	Flavor string
	// Authority is who owns role membership for an OIDC flavor: directory (the
	// directory's role claims are synchronized at every login, session.RoleSync) or
	// application (roles are assigned in the application, session.DisableRoleSync). It
	// is asked, never defaulted, because the wrong answer deletes hand-assigned roles at
	// the person's next login. Empty for the other flavors, which are the application's.
	Authority string
}

// The flavors add auth lays in.
const (
	FlavorPassword   = app.FlavorPassword
	FlavorPreauth    = app.FlavorPreauth
	FlavorOIDCAzure  = app.FlavorOIDCAzure
	FlavorOIDCGoogle = app.FlavorOIDCGoogle
)

// The authorities for an OIDC auth's role membership.
const (
	AuthorityDirectory   = app.AuthorityDirectory
	AuthorityApplication = app.AuthorityApplication
)

// The reference auth for the OIDC flavors: the outlets skeleton's members auth, an Azure
// OIDC auth with the application as its authority, copied when the application has no
// OIDC auth of its own to copy, and rewritten for Google when that is the flavor asked for.
const (
	oidcReferenceAuth = "members"
	oidcReferenceDir  = "pkg/auth/" + oidcReferenceAuth
	// referenceMigrationsDir is where the reference skeleton keeps its schema.
	referenceMigrationsDir = "schema/migrations"
)

var authNameRE = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// Command is the impulse command line for the transition.
func (au Auth) Command() string {
	cmd := fmt.Sprintf("impulse add auth %s --%s", au.Name, au.Flavor)
	if au.Authority != "" {
		cmd += " --authority " + au.Authority
	}

	return cmd
}

// Pascal is the name's PascalCase form: the table prefix.
func (au Auth) Pascal() string { return names.Pascal(au.Name) }

// oidc reports whether the flavor signs in through a directory.
func (au Auth) oidc() bool { return au.Flavor == FlavorOIDCAzure || au.Flavor == FlavorOIDCGoogle }

// directoryLabel names the directory an OIDC flavor signs in through.
func (au Auth) directoryLabel() string {
	if au.Flavor == FlavorOIDCGoogle {
		return "Google OpenID Connect"
	}

	return "Azure OpenID Connect"
}

// The names the registration and the briefs repeat.
const (
	issuerURLField = "IssuerURL"
	azureName      = "Azure"
)

// registrationVar is one variable of an OIDC auth's directory registration: the Settings
// field it fills, the suffix of its environment variable, APP_<NAME>_OIDC_<suffix>, and
// the field's Go type when it is not a string.
type registrationVar struct{ field, suffix, goType string }

// registration lists the directory registration an OIDC flavor reads from the
// environment: Azure names its issuer, Google (one issuer) the Workspace domain logins
// are restricted to, and a directory-run Google auth the group prefix its role groups
// carry and the Admin SDK service account that reads them.
func (au Auth) registration() []registrationVar {
	if au.Flavor == FlavorOIDCGoogle {
		vars := []registrationVar{{"ClientID", "CLIENT_ID", ""}, {"ClientSecret", "CLIENT_SECRET", ""}, {"RedirectURL", "REDIRECT_URL", ""}, {"HostedDomain", "HOSTED_DOMAIN", ""}}
		if au.Authority == AuthorityDirectory {
			vars = append(vars, registrationVar{"GroupPrefix", "GROUP_PREFIX", ""}, registrationVar{"AdminCredentials", "ADMIN_CREDENTIALS", "[]byte"}, registrationVar{"AdminSubject", "ADMIN_SUBJECT", ""})
		}

		return vars
	}

	return []registrationVar{{issuerURLField, "ISSUER_URL", ""}, {"ClientID", "CLIENT_ID", ""}, {"ClientSecret", "CLIENT_SECRET", ""}, {"RedirectURL", "REDIRECT_URL", ""}}
}

// typeOf is the registration variable's Go type in the environment struct.
func (v registrationVar) typeOf() string {
	if v.goType == "" {
		return "string"
	}

	return v.goType
}

// registrationVars renders the registration's variables for a message: the first in
// full, the rest by suffix.
func (au Auth) registrationVars() string {
	upper := strings.ToUpper(au.Name)
	vars := au.registration()
	parts := make([]string, len(vars))
	for i, v := range vars {
		parts[i] = "_" + v.suffix
	}
	parts[0] = "APP_" + upper + "_OIDC" + parts[0]

	return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
}

// simulatedReads names what the simulated directory (the session library's skipAuth build
// tag) still reads from the registration.
func (au Auth) simulatedReads() string {
	switch {
	case au.Flavor == FlavorOIDCGoogle && au.Authority == AuthorityDirectory:
		return "the redirect URL, the hosted domain, and the group prefix"
	case au.Flavor == FlavorOIDCGoogle:
		return "the redirect URL and the hosted domain"
	default:
		return "the redirect URL"
	}
}

// authSource is the auth package the new one is copied from.
type authSource struct {
	// Name and Pascal are the source auth's name and table prefix.
	Name, Pascal string
	// Dir is the package's directory, relative to FS's root when FS is set, else to the
	// application root.
	Dir string
	// Flavor is the source's login flavor.
	Flavor string
	// FS is the embedded skeleton the source is read from; nil reads the application.
	FS fs.FS
	// MigrationsDir is where the source's table migrations are, relative like Dir.
	MigrationsDir string
	// AuthDir is the application's auth directory, where the new package goes.
	AuthDir string
	// Sibling is the auth the application already constructs, which the new
	// construction is written beside; the source itself when it is in the application.
	Sibling string
}

// Validate checks the transition against the application before anything is changed.
func (au Auth) Validate(a *app.App) error {
	if !authNameRE.MatchString(au.Name) {
		return errors.Newf("auth name %q: name the population in lowercase letters and digits, such as partners", au.Name)
	}
	if err := validateFlavor(au.Flavor, au.Authority); err != nil {
		return err
	}
	if _, err := au.source(a); err != nil {
		return err
	}

	return nil
}

// validateFlavor checks a flavor and the authority asked for it: an OIDC flavor needs one,
// the others have none to choose.
func validateFlavor(flavor, authority string) error {
	switch flavor {
	case FlavorPassword, FlavorPreauth:
		if authority == AuthorityDirectory {
			return errors.Newf("flavor %q: only an auth that signs in through a directory can hand it role membership; a %s auth's roles are the application's", flavor, flavor)
		}
	case FlavorOIDCAzure, FlavorOIDCGoogle:
		if authority != AuthorityDirectory && authority != AuthorityApplication {
			return errors.New("an OIDC auth needs --authority: directory (the directory's role claims are the authority: every login reconciles the person's roles to them and removes what they do not name) or application (roles are assigned in the application: the bootstrap now, an administration surface later). It is asked because the wrong answer deletes hand-assigned roles at the next login")
		}
	default:
		return errors.Newf("flavor %q: add auth lays in password, preauth, oidc-azure, or oidc-google", flavor)
	}

	return nil
}

// source picks the auth to copy: one of the same flavor when the application has it,
// else the reference skeleton's for an OIDC flavor, else the first auth package found.
func (au Auth) source(a *app.App) (*authSource, error) {
	var sources []authSource
	seen := map[string]bool{}
	for i := range a.Auths {
		existing := &a.Auths[i]
		name := app.AuthPackageName(existing.File)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if name == au.Name {
			return nil, errors.Newf("%s: the %s auth already exists", path.Dir(existing.File), au.Name)
		}
		dir := path.Dir(existing.File)
		sources = append(sources, authSource{
			Name: name, Pascal: strings.ToUpper(name[:1]) + name[1:], Dir: dir, Flavor: existing.Flavor,
			MigrationsDir: appMigrationsDir(a), AuthDir: path.Dir(dir), Sibling: name,
		})
	}
	if len(sources) == 0 {
		return nil, errors.New("no auth package to copy: an auth is pkg/auth/<name> constructing its session manager and store, and the application has none (impulse new lays the first one in)")
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	for i := range sources {
		if sources[i].Flavor == au.Flavor {
			return &sources[i], nil
		}
	}
	if au.oidc() {
		reference, err := skeleton.FS(ReferenceCandidate)
		if err != nil {
			return nil, err
		}

		return &authSource{
			Name: oidcReferenceAuth, Pascal: strings.ToUpper(oidcReferenceAuth[:1]) + oidcReferenceAuth[1:], Dir: oidcReferenceDir, Flavor: FlavorOIDCAzure,
			FS: reference, MigrationsDir: referenceMigrationsDir, AuthDir: sources[0].AuthDir, Sibling: sources[0].Name,
		}, nil
	}

	return &sources[0], nil
}

// appMigrationsDir is the application's first file:// migration source, or empty.
func appMigrationsDir(a *app.App) string {
	p := a.Profile()
	if len(p.Sites) == 0 || len(p.Sites[0].Generator.MigrationSources) == 0 || !strings.HasPrefix(p.Sites[0].Generator.MigrationSources[0], fileScheme) {
		return ""
	}

	return strings.TrimPrefix(p.Sites[0].Generator.MigrationSources[0], fileScheme)
}

// readDir lists a source directory, in the embedded skeleton or the application.
func (src *authSource) readDir(a *app.App, dir string) ([]fs.DirEntry, error) {
	if src.FS != nil {
		entries, err := fs.ReadDir(src.FS, dir)
		if err != nil {
			return nil, errors.Wrap(err, "fs.ReadDir()")
		}

		return entries, nil
	}
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadDir()")
	}

	return entries, nil
}

// readFile reads a source file, in the embedded skeleton or the application.
func (src *authSource) readFile(a *app.App, rel string) ([]byte, error) {
	if src.FS != nil {
		data, err := fs.ReadFile(src.FS, rel)
		if err != nil {
			return nil, errors.Wrap(err, "fs.ReadFile()")
		}

		return data, nil
	}
	data, err := os.ReadFile(a.Abs(rel))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}

	return data, nil
}

// Apply makes the deterministic half: the package, its migrations, its roles file, and
// its construction on the data level.
func (au Auth) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := au.Validate(a); err != nil {
		return nil, err
	}
	src, err := au.source(a)
	if err != nil {
		return nil, err
	}
	ch := &Change{Command: au.Command()}
	if err := au.copyPackage(a, src, ch); err != nil {
		return nil, err
	}
	if err := au.copyMigrations(a, src, ch); err != nil {
		return nil, err
	}
	if err := au.writeRoles(a, src, ch); err != nil {
		return nil, err
	}
	if err := au.editConfig(a, src, ch); err != nil {
		return nil, err
	}
	if au.oidc() {
		if err := au.tagProcfile(a, ch); err != nil {
			return nil, err
		}
	}
	// Nothing here changes what the generator reads, but a regeneration proves the
	// tree still generates.
	if out, err := exec.Run(ctx, a.Root, nil, "go", "generate", "./..."); err != nil {
		ch.skipf("go generate ./... failed; fix the cause and run it:\n%s", strings.TrimSpace(string(out)))
	} else {
		ch.didf("ran go generate ./...")
	}

	return ch, nil
}

// copyPackage copies the source auth package with the names substituted and, when the
// flavor differs, the constructor swapped; for an OIDC auth, the role-membership authority
// is set to the one asked for.
func (au Auth) copyPackage(a *app.App, src *authSource, ch *Change) error {
	dst := path.Join(src.AuthDir, au.Name)
	if _, err := os.Stat(a.Abs(dst)); err == nil {
		return errors.Newf("%s already exists", dst)
	}
	entries, err := src.readDir(a, src.Dir)
	if err != nil {
		return err
	}
	authoritySet := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := src.readFile(a, path.Join(src.Dir, name))
		if err != nil {
			return err
		}
		text := dropReferenceContext(au.rename(string(data), src))
		if src.Flavor != au.Flavor {
			text = swapFlavor(text, src.Flavor, au.Flavor)
		}
		if au.oidc() {
			if swapped, ok := swapAuthority(text, au.Flavor, au.Authority); ok {
				text, authoritySet = swapped, true
			}
		}
		out := strings.Replace(name, src.Name, au.Name, 1)
		if err := writeNew(a, path.Join(dst, out), text); err != nil {
			return err
		}
	}
	switch {
	case au.oidc():
		from := "the " + src.Name + " auth"
		if src.FS != nil {
			from = "the reference skeleton's " + src.Name + " auth"
		}
		rewrite := " with its names substituted"
		if src.Flavor != au.Flavor {
			rewrite = " with its names substituted and the constructor rewritten for Google (session.NewOIDCGoogle: a hosted domain in place of an issuer, a subject-keyed user anchor, no front-channel logout); read it over, since the rewrite is textual"
		}
		ch.didf("%s: the %s auth package, a copy of %s (%s)%s, role membership the %s's (%s) (tables %sSessions and %sOIDCUsers, cookie %s, store prefix %s)", dst, au.Name, from, Auth{Flavor: src.Flavor}.directoryLabel(), rewrite, au.Authority, roleSyncSlot(au.Flavor, au.Authority), au.Pascal(), au.Pascal(), au.Name, au.Pascal())
		if !authoritySet {
			ch.skipf("%s: the role-synchronization slot was not found where the reference keeps it, so the authority may not be %s; set the constructor's slot to %s", dst, au.Authority, roleSyncSlot(au.Flavor, au.Authority))
		}
	case src.Flavor == au.Flavor:
		ch.didf("%s: the %s auth package, a copy of %s with its names substituted (tables %sSessions and %sSessionUsers, cookie %s, store prefix %s)", dst, au.Name, src.Name, au.Pascal(), au.Pascal(), au.Name, au.Pascal())
	default:
		ch.didf("%s: the %s auth package, a copy of %s with its names substituted and the constructor swapped from %s to %s (tables %sSessions, cookie %s, store prefix %s); read it over, since the swap is textual", dst, au.Name, src.Name, src.Flavor, au.Flavor, au.Pascal(), au.Name, au.Pascal())
	}

	return nil
}

// roleSyncSlot is the session library's role-synchronization slot for a flavor and an
// authority.
func roleSyncSlot(flavor, authority string) string {
	switch {
	case authority == AuthorityDirectory && flavor == FlavorOIDCGoogle:
		return "session.GoogleRoleSync"
	case authority == AuthorityDirectory:
		return "session.RoleSync"
	default:
		return "session.DisableRoleSync"
	}
}

// The role-synchronization slot and its explanation as the reference auth package writes
// them, one form per authority. swapAuthority rewrites one into the other; the package
// documentation's paragraph on the authority goes with it.
const (
	applicationSlot = `		// The application is the authority for role membership: a login neither reads
		// nor changes the person's roles.
		session.DisableRoleSync(),
`
	directorySlot = `		// The directory is the authority for role membership: every login reconciles the
		// person's roles to the directory's role claims, removing any it does not name,
		// and a login naming no known role is refused. Hand-assigned roles do not survive
		// it, so nothing in the application assigns roles in this store.
		session.RoleSync(accessClient.UserManager(), settings.Domains),
`
	directoryField = `	// Directory identifies the application to the directory that verifies its logins.
	Directory Directory
`
	domainsField = `	// Domains lists the tenants role synchronization sweeps besides the global scope:
	// the application's tenant roster, or nil for an application without tenants.
	Domains session.DomainsProvider
`
	applicationDoc = `// The directory proves who someone is; the application decides what they may do. Role
// membership is the application's (session.DisableRoleSync): the bootstrap assigns the
// development identities their roles, and a deployed application assigns them through its
// administration surface. The directory-run alternative, session.RoleSync, makes the
// directory's role claims the authority and removes every hand-assigned role at the next
// login, so one auth is never both.
`
	directoryDoc = `// The directory proves who someone is and says what they may do. Role membership is the
// directory's (session.RoleSync): every login reconciles the person's roles to the
// directory's role claims and removes any it does not name, so nothing in the application
// assigns roles in this store and the bootstrap seeds none. The application-run
// alternative, session.DisableRoleSync, leaves role membership to the application, so one
// auth is never both.
`
)

// The Google directory authority's forms: the slot over the directory's groups, the
// groups adapter constructed before the session manager, the registration fields the
// lookup needs, and the package documentation. swapGoogleAuthority lays them in or takes
// them out.
const (
	googleDirectorySlot = `		// The directory is the authority for role membership: every login reconciles the
		// person's roles to the Google Groups the directory places them in (those named
		// by the group prefix), removing any it does not name, and a login naming no known
		// role is refused. Hand-assigned roles do not survive it, so nothing in the
		// application assigns roles in this store.
		session.GoogleRoleSync(accessClient.UserManager(), settings.Domains, settings.Directory.GroupPrefix, groups),
`
	googleGroupsConstruction = `	// The directory's groups are the source of role membership, read through the Admin
	// SDK. Under the session library's skipAuth build tag the lookup is simulated: every
	// login is in the groups APP_ROLES names.
	groups, err := googlegroups.NewDirectory(ctx, settings.Directory.AdminCredentials, settings.Directory.AdminSubject)
	if err != nil {
		return nil, errors.Wrap(err, "googlegroups.NewDirectory()")
	}

`
	googleGroupsFields = `	// GroupPrefix is the local-part prefix of the Google Groups that carry roles: a group
	// <GroupPrefix><role>@<domain> assigns <role>. It is never empty, since it is the only
	// filter between role groups and the rest of the directory.
	GroupPrefix string
	// AdminCredentials is the service-account key (JSON) with domain-wide delegation for
	// the Admin SDK's groups scope, and AdminSubject the account it impersonates, which
	// holds a Groups-read privilege. Neither is read under the skipAuth build tag.
	AdminCredentials []byte
	AdminSubject     string
`
	googleSessionImport = `	"github.com/cccteam/session"
`
	googleGroupsImport = `	"github.com/cccteam/session/googlegroups"
`
	googleConstructor    = `	oidcAuth, err := session.NewOIDCGoogle[`
	googleHostedField    = "\tHostedDomain string\n"
	googleApplicationDoc = `// The directory proves who someone is; the application decides what they may do. Role
// membership is the application's (session.DisableRoleSync): the bootstrap assigns the
// development identities their roles, and a deployed application assigns them through its
// administration surface. The directory-run alternative, session.GoogleRoleSync, makes the
// directory's groups the authority and removes every hand-assigned role at the next
// login, so one auth is never both.
`
	googleDirectoryPkgDoc = `// The directory proves who someone is and says what they may do. Role membership is the
// directory's (session.GoogleRoleSync): every login reconciles the person's roles to the
// Google Groups the directory places them in and removes any it does not name, so nothing
// in the application assigns roles in this store and the bootstrap seeds none. The
// application-run alternative, session.DisableRoleSync, leaves role membership to the
// application, so one auth is never both.
`
)

// The reference package's explanations of its directory registration and its user anchor,
// as the Azure auth writes them and as the Google rewrite says them.
const (
	azureDirectoryDoc = `// Directory is the application's OpenID Connect registration with the directory: the
// issuer, the client credentials, and the callback the directory returns the browser to.
// Under the session library's skipAuth build tag the directory is simulated, every login
// is APP_USERNAME, and only RedirectURL is read: it is where the simulated login returns.
`
	googleDirectoryDoc = `// Directory is the application's OpenID Connect registration with Google: the client
// credentials, the callback the directory returns the browser to, and the Workspace domain
// (hosted domain) logins are restricted to. Under the session library's skipAuth build tag
// the directory is simulated, every login is APP_USERNAME, and only RedirectURL and
// HostedDomain are read: where the simulated login returns, and the domain it presents.
`
	googleDirectoryRunDoc = `// Directory is the application's OpenID Connect registration with Google: the client
// credentials, the callback the directory returns the browser to, the Workspace domain
// (hosted domain) logins are restricted to, and the groups lookup that carries role
// membership. Under the session library's skipAuth build tag the directory is simulated,
// every login is APP_USERNAME in the groups APP_ROLES names, and only RedirectURL,
// HostedDomain, and GroupPrefix are read.
`
	azureAnchorDoc = `	// The auth's tables, which schema/migrations creates: the sessions, and the user
	// anchor keyed by the directory's immutable (tenant, object) identifier pair, so a
	// renamed account stays the same person.
`
	googleAnchorDoc = `	// The auth's tables, which schema/migrations creates: the sessions, and the user
	// anchor keyed by the directory's immutable subject identifier, so a renamed account
	// stays the same person.
`
)

// swapAuthority sets an OIDC auth package's role-membership authority: the constructor's
// role-synchronization slot, the Settings fields the directory-run form needs, the groups
// adapter a directory-run Google auth reads through, and the package documentation. It
// reports whether the slot was found in either form.
func swapAuthority(text, flavor, authority string) (string, bool) {
	if flavor == FlavorOIDCGoogle {
		return swapGoogleAuthority(text, authority)
	}
	switch {
	case authority == AuthorityDirectory && strings.Contains(text, applicationSlot):
		text = strings.Replace(text, applicationSlot, directorySlot, 1)
		text = strings.Replace(text, directoryField, directoryField+domainsField, 1)
		text = strings.Replace(text, applicationDoc, directoryDoc, 1)

		return text, true
	case authority == AuthorityApplication && strings.Contains(text, directorySlot):
		text = strings.Replace(text, directorySlot, applicationSlot, 1)
		text = strings.Replace(text, domainsField, "", 1)
		text = strings.Replace(text, directoryDoc, applicationDoc, 1)

		return text, true
	case authority == AuthorityDirectory && strings.Contains(text, directorySlot),
		authority == AuthorityApplication && strings.Contains(text, applicationSlot):
		return text, true
	default:
		return text, false
	}
}

// swapGoogleAuthority is swapAuthority for a package already in the Google flavor: the
// directory-run form reads role membership from the directory's groups, so it constructs
// the groups adapter, imports its package, and registers the group prefix and the Admin
// SDK account beside the rest of the registration.
func swapGoogleAuthority(text, authority string) (string, bool) {
	switch {
	case authority == AuthorityDirectory && strings.Contains(text, applicationSlot):
		text = strings.Replace(text, applicationSlot, googleDirectorySlot, 1)
		text = strings.Replace(text, googleConstructor, googleGroupsConstruction+googleConstructor, 1)
		text = strings.Replace(text, googleSessionImport, googleSessionImport+googleGroupsImport, 1)
		text = strings.Replace(text, directoryField, directoryField+domainsField, 1)
		text = strings.Replace(text, googleHostedField, googleHostedField+googleGroupsFields, 1)
		text = strings.Replace(text, googleDirectoryDoc, googleDirectoryRunDoc, 1)
		text = strings.Replace(text, googleApplicationDoc, googleDirectoryPkgDoc, 1)

		return text, true
	case authority == AuthorityApplication && strings.Contains(text, googleDirectorySlot):
		text = strings.Replace(text, googleDirectorySlot, applicationSlot, 1)
		text = strings.Replace(text, googleGroupsConstruction, "", 1)
		text = strings.Replace(text, googleGroupsImport, "", 1)
		text = strings.Replace(text, domainsField, "", 1)
		text = strings.Replace(text, googleGroupsFields, "", 1)
		text = strings.Replace(text, googleDirectoryRunDoc, googleDirectoryDoc, 1)
		text = strings.Replace(text, googleDirectoryPkgDoc, googleApplicationDoc, 1)

		return text, true
	case authority == AuthorityDirectory && strings.Contains(text, googleDirectorySlot),
		authority == AuthorityApplication && strings.Contains(text, applicationSlot):
		return text, true
	default:
		return text, false
	}
}

// rename substitutes the source auth's name for the new one, in both cases, where the
// name stands on its own or starts an identifier and not inside an English word that
// happens to begin with it (names.Rename).
func (au Auth) rename(text string, src *authSource) string {
	return names.Rename(text, src.Name, au.Name)
}

// swapFlavor rewrites a password auth package into a preauth one or back (the session
// constructor, the storage constructor, the type, and the user table, which preauth has
// none of), or an Azure OIDC package into a Google one (the constructor and storage, the
// hosted domain in place of the issuer, and the subject-keyed user anchor).
func swapFlavor(text, from, to string) string {
	switch {
	case from == FlavorOIDCAzure && to == FlavorOIDCGoogle:
		text = strings.ReplaceAll(text, "session.NewOIDCAzure[", "session.NewOIDCGoogle[")
		text = strings.ReplaceAll(text, "*session.OIDCAzure[", "*session.OIDCGoogle[")
		text = strings.ReplaceAll(text, "sessionstorage.NewSpannerOIDC(", "sessionstorage.NewSpannerGoogleOIDC(")
		text = strings.ReplaceAll(text, "sessionstorage.NewPostgresOIDC(", "sessionstorage.NewPostgresGoogleOIDC(")
		text = strings.ReplaceAll(text, `"session.NewOIDCAzure()"`, `"session.NewOIDCGoogle()"`)
		text = strings.ReplaceAll(text, "(OpenID Connect against Azure)", "(OpenID Connect against Google)")
		text = regexp.MustCompile(`(?m)^\s*settings\.Directory\.IssuerURL,\n`).ReplaceAllString(text, "")
		text = regexp.MustCompile(`(?m)^(\s*)settings\.Directory\.RedirectURL,\n`).ReplaceAllString(text, "${1}settings.Directory.RedirectURL,\n${1}settings.Directory.HostedDomain,\n")
		text = regexp.MustCompile(`(?m)^\s*IssuerURL\s+string\n`).ReplaceAllString(text, "")
		text = regexp.MustCompile(`(?m)^(\s*)RedirectURL(\s+)string\n`).ReplaceAllString(text, "${1}RedirectURL${2}string\n${1}HostedDomain string\n")
		text = strings.Replace(text, azureDirectoryDoc, googleDirectoryDoc, 1)
		text = strings.Replace(text, azureAnchorDoc, googleAnchorDoc, 1)
		text = strings.Replace(text, applicationDoc, googleApplicationDoc, 1)
	case from == FlavorPassword && to == FlavorPreauth:
		text = strings.ReplaceAll(text, "session.NewPasswordAuth[session.NoCustomData, session.NoCustomData]", "session.NewPreauth[session.NoCustomData]")
		text = strings.ReplaceAll(text, "*session.PasswordAuth[session.NoCustomData, session.NoCustomData]", "*session.Preauth[session.NoCustomData]")
		text = strings.ReplaceAll(text, "sessionstorage.NewSpannerPasswordAuth(", "sessionstorage.NewSpannerPreauth(")
		text = strings.ReplaceAll(text, "sessionstorage.NewPostgresPassword(", "sessionstorage.NewPostgresPreauth(")
		text = regexp.MustCompile(`(?m)^\s*session\.WithUserTableName\([^)]*\),\n`).ReplaceAllString(text, "")
		text = regexp.MustCompile(`(?m)^\s*usersTable\s*=.*\n`).ReplaceAllString(text, "")
		text = strings.ReplaceAll(text, `"session.NewPasswordAuth()"`, `"session.NewPreauth()"`)
		text = strings.ReplaceAll(text, "with a password", "by the application's own proof of identity (preauth)")
	case from == FlavorPreauth && to == FlavorPassword:
		text = strings.ReplaceAll(text, "session.NewPreauth[session.NoCustomData]", "session.NewPasswordAuth[session.NoCustomData, session.NoCustomData]")
		text = strings.ReplaceAll(text, "*session.Preauth[session.NoCustomData]", "*session.PasswordAuth[session.NoCustomData, session.NoCustomData]")
		text = strings.ReplaceAll(text, "sessionstorage.NewSpannerPreauth(", "sessionstorage.NewSpannerPasswordAuth(")
		text = strings.ReplaceAll(text, "sessionstorage.NewPostgresPreauth(", "sessionstorage.NewPostgresPassword(")
		text = strings.ReplaceAll(text, `"session.NewPreauth()"`, `"session.NewPasswordAuth()"`)
	}

	return text
}

// copyMigrations copies the migrations creating the source auth's tables, renamed, as
// the next migrations. A preauth auth gets no users table.
func (au Auth) copyMigrations(a *app.App, src *authSource, ch *Change) error {
	dir := appMigrationsDir(a)
	if dir == "" {
		ch.skipf("no file:// migration source names the schema, so the %s auth's tables were not written; copy the %s auth's session, user, and access table migrations under the %s prefix", au.Name, src.Name, au.Pascal())

		return nil
	}
	entries, err := src.readDir(a, src.MigrationsDir)
	if err != nil {
		return err
	}
	next, err := nextMigration(a.Abs(dir))
	if err != nil {
		return err
	}
	var copied []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		data, err := src.readFile(a, path.Join(src.MigrationsDir, name))
		if err != nil {
			return err
		}
		if !createsPrefixed(data, src.Pascal) {
			continue
		}
		if au.Flavor == FlavorPreauth && strings.Contains(string(data), src.Pascal+"SessionUsers") {
			continue
		}
		_, stem, _ := strings.Cut(strings.TrimSuffix(name, ".up.sql"), "_")
		base := fmt.Sprintf("%06d_%s", next, strings.Replace(stem, src.Pascal, au.Pascal(), 1))
		next++
		if err := writeNew(a, path.Join(dir, base+".up.sql"), swapMigrationFlavor(au.rename(string(data), src), src.Flavor, au.Flavor)); err != nil {
			return err
		}
		down, err := src.readFile(a, path.Join(src.MigrationsDir, strings.TrimSuffix(name, ".up.sql")+".down.sql"))
		if err == nil {
			if err := writeNew(a, path.Join(dir, base+".down.sql"), swapMigrationFlavor(au.rename(string(down), src), src.Flavor, au.Flavor)); err != nil {
				return err
			}
		}
		copied = append(copied, base)
	}
	if len(copied) == 0 {
		ch.skipf("%s: no migration creates a table prefixed %s, so the %s auth's tables were not written; add its session, user, and access tables under the %s prefix", dir, src.Pascal, au.Name, au.Pascal())

		return nil
	}
	ch.didf("%s: %s, the %s auth's tables copied from the %s auth's under the %s prefix", dir, strings.Join(copied, ", "), au.Name, src.Name, au.Pascal())

	return nil
}

// swapMigrationFlavor rewrites the Azure reference's session DDL for Google, whose
// sessions carry no directory session identifier (Google has no front-channel logout to
// look one up for) and whose user anchor is keyed by the subject claim and its hosted
// domain in place of Azure's tenant and object identifiers. Other flavor pairs share
// their DDL.
func swapMigrationFlavor(text, from, to string) string {
	if from != FlavorOIDCAzure || to != FlavorOIDCGoogle {
		return text
	}
	text = oidcSidColumnRE.ReplaceAllString(text, "")
	text = oidcSidIndexRE.ReplaceAllString(text, "")
	text = oidcSidDropRE.ReplaceAllString(text, "")
	text = tidOidColumnsRE.ReplaceAllString(text, "${1}Sub${2}STRING(MAX) NOT NULL,\n${1}Hd${3} STRING(MAX) NOT NULL,\n")
	text = tidOidIndexRE.ReplaceAllString(text, "BySub ON ${1} (Sub)")
	text = strings.ReplaceAll(text, "ByTidOid;", "BySub;")

	return text
}

// The Azure-only parts of the reference's session DDL.
var (
	oidcSidColumnRE = regexp.MustCompile(`(?m)^\s*OidcSid\s+STRING\(MAX\) NOT NULL,\n`)
	oidcSidIndexRE  = regexp.MustCompile(`CREATE INDEX \w+SessionsByOidcSid ON \w+ \([^)]*\);\n\n?`)
	oidcSidDropRE   = regexp.MustCompile(`DROP INDEX \w+SessionsByOidcSid;\n\n?`)
	tidOidColumnsRE = regexp.MustCompile(`(?m)^(\s*)Tid(\s+)STRING\(36\) NOT NULL,\n\s*Oid(\s+)STRING\(36\) NOT NULL,\n`)
	tidOidIndexRE   = regexp.MustCompile(`ByTidOid ON (\w+) \(Tid, Oid\)`)
)

// fileScheme prefixes a migration source the transition can read.
const fileScheme = "file://"

// createsPrefixed reports whether a migration creates a table carrying the prefix.
func createsPrefixed(data []byte, prefix string) bool {
	for _, m := range createTableRE.FindAllSubmatch(data, -1) {
		if strings.HasPrefix(string(m[1]), prefix) {
			return true
		}
	}

	return false
}

var createTableRE = regexp.MustCompile("(?i)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?`?([A-Za-z_][A-Za-z0-9_]*)`?")

// writeRoles writes the auth's roles file, empty, beside the source auth's.
func (au Auth) writeRoles(a *app.App, src *authSource, ch *Change) error {
	rolesDir := "schema/roles"
	if entries, err := fs.Glob(os.DirFS(a.Root), "schema/roles/"+src.Name+".json"); err == nil && len(entries) > 0 {
		rolesDir = path.Dir(entries[0])
	}
	rel := path.Join(rolesDir, au.Name+".json")
	if _, err := os.Stat(a.Abs(rel)); err == nil {
		ch.skipf("%s already exists and was left alone", rel)

		return nil
	}
	if err := writeNew(a, rel, "{\n  \"roles\": {\n    \"global\": [],\n    \"domain\": []\n  }\n}\n"); err != nil {
		return err
	}
	ch.didf("%s: the %s auth's role configuration, empty (the Administrator role at each scope is implicit)", rel, au.Name)

	return nil
}

// editConfig constructs the auth on the data level beside the source auth: an import, a
// field, the construction before the return, and the literal element. An accessor goes
// in a new file.
func (au Auth) editConfig(a *app.App, src *authSource, ch *Change) error {
	rel, data, mode, err := findDeclaringFile(a, "DataConfiguration")
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no file declares a DataConfiguration struct, so the %s auth is not constructed anywhere; construct it where the %s auth is and hand its session manager and store to the surface that binds to it", au.Name, src.Name)

		return nil
	}
	modulePath := ""
	if a.GoMod != nil && a.GoMod.Module != nil {
		modulePath = a.GoMod.Module.Mod.Path
	}
	pkgPath := modulePath + "/" + path.Join(src.AuthDir, au.Name)
	edited, err := app.AddImport(rel, data, pkgPath)
	if err != nil {
		return err
	}
	edited, err = app.AddStructField(rel, edited, "DataConfiguration", au.Name+" *"+au.Name+".Auth")
	if err != nil {
		return err
	}
	srcVar, ok := constructionVar(string(data), src.Sibling)
	if !ok {
		ch.skipf("%s: NewDataConfiguration constructs no %s.New(...) to copy, so the %s auth is declared on DataConfiguration but not constructed; construct it beside the %s auth and set the field", rel, src.Sibling, au.Name, src.Sibling)
	} else {
		statements := strings.ReplaceAll(strings.ReplaceAll(srcVar.statement, src.Sibling+".", au.Name+"."), srcVar.name, au.Name+"Auth")
		if au.oidc() {
			statements, edited, err = au.oidcConstruction(rel, edited, statements, ch)
			if err != nil {
				return err
			}
		}
		// An anchor miss leaves the file as edited so far: the working copy is only
		// replaced by a result the editor produced.
		constructed, err := app.AddStatementsBeforeConstruction(rel, edited, "NewDataConfiguration", "DataConfiguration", statements)
		switch {
		case errors.Is(err, app.ErrNoAnchor):
			ch.skipf("%s: NewDataConfiguration builds no &DataConfiguration{...} literal, so the %s auth is declared but not constructed; construct it beside the %s auth and set the field", rel, au.Name, src.Sibling)
		case err != nil:
			return err
		default:
			edited, err = app.AddLiteralElement(rel, constructed, "NewDataConfiguration", "DataConfiguration", au.Name+": "+au.Name+"Auth")
			if err != nil {
				return err
			}
		}
	}
	if err := os.WriteFile(a.Abs(rel), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	pkg, err := app.PackageName(rel, data)
	if err != nil {
		return err
	}
	accessor := path.Join(path.Dir(rel), au.Name+".go")
	if err := writeNew(a, accessor, fmt.Sprintf(`package %[1]s

import %[2]q

// %[3]s returns the %[4]s auth: its session manager and permission store, for the
// surface that binds to it.
func (c *DataConfiguration) %[3]s() *%[4]s.Auth {
	return c.%[4]s
}
`, pkg, pkgPath, au.Pascal(), au.Name)); err != nil {
		return err
	}
	ch.didf("%s: the %s auth constructed on DataConfiguration beside the %s auth (field, construction, import); %s: its accessor %s()", rel, au.Name, src.Sibling, accessor, au.Pascal())
	ch.skipf("%s: Close releases the %s auth only; release the %s auth too", rel, src.Sibling, au.Name)

	return nil
}

// settingsLiteralRE matches the copied construction's Settings literal, whose fields are
// the sibling auth's (cookie key, session timeout).
var settingsLiteralRE = regexp.MustCompile(`&?(\w+)\.Settings\{([^{}]*)\}`)

// envVarRE finds the data level's environment struct: the variable envconfig fills.
var envVarRE = regexp.MustCompile(`(?m)^\s*env := &(\w+)\{\}`)

// oidcConstruction turns the copied construction into an OIDC auth's: the Settings
// literal gains the login page and the directory registration, and the environment
// struct gains the registration's four variables, prefixed with the auth's name. Without
// an environment struct to read them from, the registration is left to the agent.
func (au Auth) oidcConstruction(rel string, edited []byte, statements string, ch *Change) (extended string, file []byte, err error) {
	m := settingsLiteralRE.FindStringSubmatch(statements)
	if m == nil {
		ch.skipf("%s: the copied construction has no %s.Settings literal to extend, so the %s auth's directory registration (%s) is not read from the environment; pass it in %s.Settings.Directory", rel, au.Name, au.Name, au.registrationVars(), au.Name)

		return statements, edited, nil
	}
	fields := []string{}
	for _, f := range strings.Split(m[2], ",") {
		if f = strings.TrimSpace(f); f != "" {
			fields = append(fields, f)
		}
	}
	fields = append(fields, `LoginURL: "/login"`)

	upper := strings.ToUpper(au.Name)
	if env := envVarRE.FindSubmatch(edited); env != nil {
		structName := string(env[1])
		comment := fmt.Sprintf("// The %s auth's directory registration (pkg/auth/%s): the OpenID Connect issuer, the\n// application's client credentials, and the callback the directory returns the browser to.\n// Under the session library's skipAuth build tag only the redirect URL is read.\n", au.Name, au.Name)
		if au.Flavor == FlavorOIDCGoogle {
			comment = fmt.Sprintf("// The %s auth's directory registration (pkg/auth/%s): the application's client\n// credentials, the callback Google returns the browser to, and the Workspace domain logins\n// are restricted to. Under the session library's skipAuth build tag only %s are read.\n", au.Name, au.Name, au.simulatedReads())
			if au.Authority == AuthorityDirectory {
				comment = fmt.Sprintf("// The %s auth's directory registration (pkg/auth/%s): the application's client\n// credentials, the callback Google returns the browser to, the Workspace domain logins are\n// restricted to, the prefix of the Google Groups that carry roles, and the Admin SDK service\n// account (a key with domain-wide delegation, and the admin it impersonates) that reads them.\n// Under the session library's skipAuth build tag only %s are read.\n", au.Name, au.Name, au.simulatedReads())
			}
		}
		var directory strings.Builder
		fmt.Fprintf(&directory, "Directory: %s.Directory{\n", au.Name)
		for i, v := range au.registration() {
			field := fmt.Sprintf("%s%s %s `env:\"APP_%s_OIDC_%s\"`", au.Pascal(), v.field, v.typeOf(), upper, v.suffix)
			if i == 0 {
				field = comment + field
			}
			edited, err = app.AddStructField(rel, edited, structName, field)
			if err != nil {
				return "", nil, err
			}
			fmt.Fprintf(&directory, "\t%s: env.%s%s,\n", v.field, au.Pascal(), v.field)
		}
		directory.WriteString("}")
		fields = append(fields, directory.String())
		ch.didf("%s: %s reads the %s auth's directory registration from %s", rel, structName, au.Name, au.registrationVars())
	} else {
		ch.skipf("%s: no environment struct (env := &T{}) to add the %s auth's directory registration to; read %s and pass them in %s.Settings.Directory", rel, au.Name, au.registrationVars(), au.Name)
	}
	literal := "&" + au.Name + ".Settings{\n\t" + strings.Join(fields, ",\n\t") + ",\n}"

	return strings.Replace(statements, m[0], literal, 1), edited, nil
}

// tagProcfile builds the development processes with the session library's skipAuth tag,
// which simulates the directory an OIDC auth signs in through, so the application can be
// signed in to before it is registered with one. Every `go run` in the Procfile gains the
// tag, and the environment template gains the simulated directory's variables and the
// registration's, for a person to fill in.
func (au Auth) tagProcfile(a *app.App, ch *Change) error {
	data, mode, err := readFile(a, "Procfile")
	switch {
	case err != nil:
		ch.skipf("Procfile: not read (%v); build the development server with -tags skipAuth so the %s auth's directory is simulated", err, au.Name)
	case strings.Contains(string(data), "skipAuth"):
		ch.didf("Procfile: already builds with -tags skipAuth, so the %s auth's directory is simulated in development", au.Name)
	default:
		n := 0
		text := goRunRE.ReplaceAllStringFunc(string(data), func(string) string {
			n++

			return "go run -tags skipAuth "
		})
		if n == 0 {
			ch.skipf("Procfile: no `go run` to tag; build the development server with -tags skipAuth so the %s auth's directory is simulated", au.Name)

			break
		}
		if err := os.WriteFile(a.Abs("Procfile"), []byte(text), mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
		ch.didf("Procfile: %d go run command(s) build with -tags skipAuth, so the %s auth's directory is simulated in development and every %s login is APP_USERNAME", n, au.Name, au.Name)
	}

	return au.writeEnvTemplate(a, ch)
}

var goRunRE = regexp.MustCompile(`\bgo run `)

// writeEnvTemplate adds the simulated directory's variables and the auth's registration to
// the development environment template.
func (au Auth) writeEnvTemplate(a *app.App, ch *Change) error {
	if a.EnvTemplate == "" {
		ch.skipf("no environment template (.envrc.template, .env.template, .env.example) to add APP_USERNAME and the %s auth's APP_%s_OIDC_* variables to", au.Name, strings.ToUpper(au.Name))

		return nil
	}
	data, mode, err := readFile(a, a.EnvTemplate)
	if err != nil {
		return err
	}
	upper := strings.ToUpper(au.Name)
	var b strings.Builder
	fmt.Fprintf(&b, "\n# --- %s auth: the directory its people sign in through (pkg/auth/%s) ---\n", au.Name, au.Name)
	b.WriteString("# The Procfile builds with the session library's skipAuth tag, which simulates the\n")
	if !strings.Contains(string(data), "APP_USERNAME=") {
		fmt.Fprintf(&b, "# directory: every login through it is APP_USERNAME (APP_ROLES are the role claims), and\n# only %s below is read. Register the application in a directory, fill the\n# rest in, and drop the tag to use one.\nexport APP_USERNAME=%s-dev\nexport APP_ROLES=\n", au.simulatedReads(), au.Name)
	} else {
		fmt.Fprintf(&b, "# directory: every login through it is APP_USERNAME (set above), and only %s\n# below is read. Register the application in a directory, fill the rest in, and drop\n# the tag to use one.\n", au.simulatedReads())
	}
	if au.Flavor == FlavorOIDCGoogle {
		fmt.Fprintf(&b, "# export APP_%[1]s_OIDC_CLIENT_ID=\n# export APP_%[1]s_OIDC_CLIENT_SECRET=\n# APP_%[1]s_OIDC_REDIRECT_URL is the browser-facing callback of the surface that binds to\n# the %[2]s auth, such as http://127.0.0.1:4300/api/user/callback through the dev proxy.\nexport APP_%[1]s_OIDC_REDIRECT_URL=\n# APP_%[1]s_OIDC_HOSTED_DOMAIN is the Google Workspace domain logins are restricted to; the\n# simulated directory presents it too, so it is set in development.\nexport APP_%[1]s_OIDC_HOSTED_DOMAIN=example.com\n", upper, au.Name)
		if au.Authority == AuthorityDirectory {
			fmt.Fprintf(&b, "# APP_%[1]s_OIDC_GROUP_PREFIX names the Google Groups that carry roles: <prefix><role>@<domain>\n# assigns <role>. Read under the simulated directory too (APP_ROLES stand in for the groups),\n# so it is set in development.\nexport APP_%[1]s_OIDC_GROUP_PREFIX=%[2]s-\n# The Admin SDK service account that reads the groups: a key with domain-wide delegation for\n# the groups scope, and the admin it impersonates. Unread under the simulated directory.\n# export APP_%[1]s_OIDC_ADMIN_CREDENTIALS=\n# export APP_%[1]s_OIDC_ADMIN_SUBJECT=\n", upper, au.Name)
		}
	} else {
		fmt.Fprintf(&b, "# export APP_%[1]s_OIDC_ISSUER_URL=\n# export APP_%[1]s_OIDC_CLIENT_ID=\n# export APP_%[1]s_OIDC_CLIENT_SECRET=\n# APP_%[1]s_OIDC_REDIRECT_URL is the browser-facing callback of the surface that binds to\n# the %[2]s auth, such as http://127.0.0.1:4300/api/user/callback through the dev proxy.\nexport APP_%[1]s_OIDC_REDIRECT_URL=\n", upper, au.Name)
	}
	text := strings.TrimRight(string(data), "\n") + "\n" + b.String()
	if err := os.WriteFile(a.Abs(a.EnvTemplate), []byte(text), mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: APP_USERNAME and APP_ROLES for the simulated directory, and the %s auth's APP_%s_OIDC_* registration, to fill in", a.EnvTemplate, au.Name, upper)

	return nil
}

// construction is a "<var>, err := <pkg>.New(...)" statement with its error check.
type construction struct {
	name      string
	statement string
}

// constructionVar finds the source auth's construction in the data level's source: the
// assignment calling <src>.New and the error check that follows it.
func constructionVar(text, srcName string) (construction, bool) {
	re := regexp.MustCompile(`(?m)^\t(\w+), err := ` + regexp.QuoteMeta(srcName) + `\.New\((?s:.*?)\n\t\}\n`)
	m := re.FindStringSubmatch(text)
	if m == nil {
		return construction{}, false
	}
	lines := strings.Split(strings.TrimRight(m[0], "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, "\t")
	}

	return construction{name: m[1], statement: strings.Join(lines, "\n")}, true
}

// Meaning explains an auth in this framework and names the wiring left to do.
func (au Auth) Meaning() string {
	if au.oidc() {
		return au.oidcMeaning()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "An auth is a population that signs in one way and holds roles in its own permission store, and it is a package: `pkg/auth/%s` owns the %s session manager (tables `%sSessions`%s, cookie `%s`), the %s permission store (tables prefixed `%s`), and the roles file `schema/roles/%s.json`. A person in two auths is two unrelated principals. The data level now constructs it beside the other auths.\n\n", au.Name, au.Flavor, au.Pascal(), au.userTableNote(), au.Name, au.Name, au.Pascal(), au.Name)
	b.WriteString("Left to wire:\n\n")
	items := []string{
		fmt.Sprintf("Bind a surface to it. Decide which site or outlet serves the %s population and compose its session group around the %s auth's handlers (session start, XSRF, login, and logout for a password auth; the application's own proof-of-identity handler issuing the session for preauth), separate from the other auths' groups, so a %s session opens nothing bound to another auth. The surface's web app names the auth's XSRF cookie, `%s.XSRFCookie` (`%s-xsrf`), in its HttpClient configuration (`withXsrfConfiguration`), since the browser echoes that cookie in the X-XSRF-TOKEN header. If the population has no surface yet, add an outlet for it first.", au.Name, au.Name, au.Name, au.Name, au.Name),
		fmt.Sprintf("Provision its roles. Call the roles migration for the %s auth in the bootstrap and the deployment's migrate step with `%s.RolesPath` and the %s auth's user manager, across the tenants when the application is tenanted. Give the %s auth its development identities (a login and its roles) in the bootstrap identities, kept apart from the other auths' identities.", au.Name, au.Name, au.Name, au.Name),
		fmt.Sprintf("Release it. Close the %s auth where the data level closes the others.", au.Name),
		fmt.Sprintf("Prove the segmentation in the integration tests: a %s login is refused by every other auth's surface, another auth's session is refused by the %s surface, and a username that exists in two auths is two principals with separate roles.", au.Name, au.Name),
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	b.WriteString("\nData consequence: none for existing people, since the new auth starts empty. If people are to move from another auth into this one, their accounts and roles are re-created here by a migration a person reviews, and the old auth keeps or deletes them by decision, never by default.\n")

	return b.String()
}

// oidcMeaning explains an auth whose people sign in through a directory.
func (au Auth) oidcMeaning() string {
	var b strings.Builder
	anchor, directory, handlers, logoutRoute := "tenant and object identifiers", azureName, "session.OIDCAzureHandlers", ", `GET <prefix>/user/logout` (the directory's front-channel logout)"
	if au.Flavor == FlavorOIDCGoogle {
		anchor, directory, handlers, logoutRoute = "subject identifier", "Google, restricted to the Workspace domain the registration names", "session.OIDCGoogleHandlers", " (Google has no directory-initiated logout, so there is no front-channel route: the session's own logout route ends it)"
	}
	fmt.Fprintf(&b, "An auth is a population that signs in one way and holds roles in its own permission store, and it is a package: `pkg/auth/%s` owns the %s session manager (tables `%sSessions` and `%sOIDCUsers`, the user anchor keyed by the directory's immutable %s; cookie `%s`), the %s permission store (tables prefixed `%s`), and the roles file `schema/roles/%s.json`. Its people sign in through the organization's directory over OpenID Connect (%s): the login route sends the browser to the directory, the directory returns it to the callback, and the callback starts the session. A person in two auths is two unrelated principals. The data level now constructs it beside the other auths, reading the directory registration from the environment.\n\n", au.Name, au.Flavor, au.Pascal(), au.Pascal(), anchor, au.Name, au.Name, au.Pascal(), au.Name, directory)
	switch {
	case au.Authority == AuthorityDirectory && au.Flavor == FlavorOIDCGoogle:
		fmt.Fprintf(&b, "Role membership is the directory's (`session.GoogleRoleSync`): every login reconciles the person's roles to the Google Groups the directory places them in, a group named `<prefix><role>@<domain>` assigning `<role>`, and removes any role no group names; a login in no role group is refused. So nothing in the application assigns roles in the %s store, the bootstrap seeds no %s identities, and the roles file only defines the roles and their grants; the directory's groups assign them. The groups are read through the Admin SDK (`googlegroups.NewDirectory`, a service-account key with domain-wide delegation and the admin it impersonates); under the session library's `skipAuth` tag the lookup is simulated and `APP_ROLES` names the groups every login is in. In a tenanted application, pass the tenant roster as `%s.Settings.Domains` so the sweep covers every tenant scope.\n\n", au.Name, au.Name, au.Name)
	case au.Authority == AuthorityDirectory:
		fmt.Fprintf(&b, "Role membership is the directory's (`session.RoleSync`): every login reconciles the person's roles to the directory's role claims and removes any it does not name, and a login naming no known role is refused. So nothing in the application assigns roles in the %s store, the bootstrap seeds no %s identities, and the roles file only defines the roles and their grants; the directory assigns them. In a tenanted application, pass the tenant roster as `%s.Settings.Domains` so the sweep covers every tenant scope.\n\n", au.Name, au.Name, au.Name)
	default:
		fmt.Fprintf(&b, "Role membership is the application's (`session.DisableRoleSync`): the directory proves who someone is and the application decides what they may do. A login neither reads nor changes roles, so the bootstrap assigns the development %s identities their roles in the %s store by username, with no password and no account to create, since the directory presents the name.\n\n", au.Name, au.Name)
	}
	b.WriteString("Left to wire:\n\n")
	items := []string{
		fmt.Sprintf("Bind a surface to it. Decide which site or outlet serves the %s population and compose an OIDC session group around the %s auth's handlers, as the reference's `pkg/router/router.go` does for its members auth (`oidcGroup`): session start and XSRF, `GET <prefix>/user/login` (the redirect to the directory), `GET <prefix>/user/callback` (the directory's return)%s, the session and logout routes, then the API behind session validation and the XSRF guard. Give the App a method returning the %s auth's `%s` for the router. Bind the group's requests to the auth (`pkg/auth.Bind`, the reference's `BindAuth` middleware) so permission checks and tenant visibility answer from the %s store, and set `LoginURL` in the data level's construction to the surface's login page. The surface's web app names the auth's XSRF cookie, `%s.XSRFCookie` (`%s-xsrf`), in its HttpClient configuration (`withXsrfConfiguration`), as the reference's portal does. If the population has no surface yet, add an outlet for it first.", au.Name, au.Name, logoutRoute, au.Name, handlers, au.Name, au.Name, au.Name),
		fmt.Sprintf("Provision its roles. Call the roles migration for the %s auth in the bootstrap and the deployment's migrate step with `%s.RolesPath` and the %s auth's user manager, across the tenants when the application is tenanted.%s", au.Name, au.Name, au.Name, au.identitiesNote()),
		fmt.Sprintf("Register it. The environment template now carries the %s auth's `APP_%s_OIDC_*` variables: set the redirect URL to the browser-facing callback of the surface it binds to (through the dev proxy in development), and fill in %s when the application is registered in a directory. Until then the Procfile builds with the session library's `skipAuth` tag, which simulates the directory: every %s login is `APP_USERNAME`.", au.Name, strings.ToUpper(au.Name), au.registrationToFill(), au.Name),
		"Sign in from the browser. The surface's login page becomes a button that sends the browser to `<prefix>/user/login?returnUrl=<page>`, as the reference's portal login component does; a refused login returns to the login page with the reason in `?message=`.",
		fmt.Sprintf("Release it. Close the %s auth where the data level closes the others.", au.Name),
		fmt.Sprintf("Prove it in the integration tests under `-tags skipAuth`, as the reference's `test/integration/portal_login_skipauth_test.go` does: a login helper that sets `APP_USERNAME` under a mutex and follows the login route to the callback, with a `!skipAuth` twin that skips with the reason, so `go test ./...` without the tag skips the %s login tests visibly and CI runs with it. Prove the segmentation: a %s session is refused by every other auth's surface, another auth's session by the %s surface, and a username that exists in two auths is two principals with separate roles.", au.Name, au.Name, au.Name),
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	b.WriteString("\nData consequence: none for existing people, since the new auth starts empty. If people are to move from another auth into this one, their roles are re-created here by a migration a person reviews, keyed by the username the directory will present, and the old auth keeps or deletes them by decision, never by default.\n")

	return b.String()
}

// registrationToFill names the registration a person fills in once the application is
// registered with its directory.
func (au Auth) registrationToFill() string {
	if au.Flavor == FlavorOIDCGoogle && au.Authority == AuthorityDirectory {
		return "the client and secret, the hosted domain (the Workspace domain logins are restricted to, which the simulated directory presents too, so it is set from the start), the group prefix (set from the start too, since the simulated groups carry it), and the Admin SDK service account that reads the groups"
	}
	if au.Flavor == FlavorOIDCGoogle {
		return "the client, secret, and hosted domain (the Workspace domain logins are restricted to, which the simulated directory presents too, so it is set from the start)"
	}

	return "the issuer, client, and secret"
}

// identitiesNote says what the bootstrap does with development identities for the
// authority.
func (au Auth) identitiesNote() string {
	if au.Authority == AuthorityDirectory && au.Flavor == FlavorOIDCGoogle {
		return " Seed no role assignments for it: the directory's groups assign them, and the roles file defines what each role may do. In development, APP_ROLES names the groups every simulated login is in."
	}
	if au.Authority == AuthorityDirectory {
		return " Seed no role assignments for it: the directory assigns them, and the roles file defines what each role may do."
	}

	return fmt.Sprintf(" Give the %s auth its development identities in the bootstrap identities: role assignments by username, no passwords, kept apart from the other auths' identities.", au.Name)
}

func (au Auth) userTableNote() string {
	if au.Flavor == FlavorPreauth {
		return ""
	}

	return fmt.Sprintf(" and `%sSessionUsers`", au.Pascal())
}
