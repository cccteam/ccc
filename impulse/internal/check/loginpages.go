package check

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// loginPages verifies that every auth bound to a browser outlet returns a refused or
// waiting sign-in to that outlet's login page, and sends a command-line client to that
// outlet's device page: the LoginURL the data level names in the auth's settings
// construction (<auth>.Settings{...}, outside tests) is the outlet's mount path joined
// with /login, the path MailLoginURL quotes is the same page, so every mailed link opens
// it, and, when a machine outlet carries the auth's tokens, the DevicePage it names is
// the mount path joined with /device. The session library sends a refused directory
// sign-in (?code=) and a sign-in waiting for its confirmation (?pending=) to LoginURL,
// and to its default, /login, when the construction names none; the password method
// answers its own post and sends nobody anywhere, so a password-only auth's construction
// may name none. A device authorization tells the client the page a signed-in person
// approves it on (verification_uri): DevicePage, or the library's default, /device, when
// the construction names none. A console moved from / to /console/ that kept /login or
// /device sends them to a page nothing serves, which no build or generation sees. The
// transitions brief the line (add method writes it, move console names it); this holds
// it.
type loginPages struct{}

func (loginPages) Name() string { return "login-pages" }

func (loginPages) Describe() string {
	return "every auth bound to a browser outlet names the outlet's login page (<mount>/login) as the LoginURL of its settings construction (the library's default, /login, when a directory method's construction names none), as the path its MailLoginURL quotes, and, when a machine outlet carries the auth's tokens, the outlet's device page (<mount>/device) as its DevicePage (the library's default, /device, when the construction names none)"
}

// The settings type the pages are read from, its fields naming them (MailLoginURL, or
// the LoginURL of a Mail literal, the shape an application composes the sender and the
// page in one value by; DevicePage, the page a command-line client is sent to), the
// segments a login page and a device page end in under the mount path, and the pages
// the session library sends a refused or waiting sign-in and a command-line client to
// when the construction names none.
const (
	settingsType      = "Settings"
	loginURLField     = "LoginURL"
	mailLoginField    = "MailLoginURL"
	mailField         = "Mail"
	devicePageField   = "DevicePage"
	loginSegment      = "login"
	deviceSegment     = "device"
	defaultLoginPage  = "/login"
	defaultDevicePage = "/device"
)

// The verbs the remedies open with: the page LoginURL or DevicePage is set to, and the
// page a mailed links' field ends in.
const (
	setLoginURL   = "set LoginURL to"
	setDevicePage = "set DevicePage to"
	endField      = "end %s in"
)

// loginBinding is what the browser outlets declaring an auth bind it to: the login and
// device pages under their mount paths, one target per mount path, whether any declares
// a directory method, whose refused or waiting sign-ins the library sends back to
// LoginURL, and whether a machine outlet carries the auth's tokens, whose device
// authorizations send a command-line client to DevicePage.
type loginBinding struct {
	targets   []loginTarget
	directory bool
	issues    bool
}

// loginTarget is one mount path an auth is bound under: its login page and its device
// page, the mount path they are under, and the outlets (as --outlet names them) whose
// browser applications are bound to the auth there.
type loginTarget struct {
	page    string
	device  string
	mount   string
	outlets []string
}

// loginSettings is one construction of an auth's settings on the data level: where the
// literal is, and the pages its fields name.
type loginSettings struct {
	file string
	line int
	// login is the LoginURL field; unset when the construction names none.
	login pageField
	// mail is the field the mailed links' page is read from: MailLoginURL, or the
	// LoginURL of a Mail literal; unset when the construction names neither.
	mail pageField
	// device is the DevicePage field; unset when the construction names none.
	device pageField
}

// pageField is one settings field naming a page: whether the construction has it, its
// line, the field as the finding names it, and the path it quotes, when it is a string
// literal or an expression ending in one.
type pageField struct {
	set  bool
	line int
	name string
	// page is the quoted path; empty when the value holds no string literal to read.
	page string
}

func (c loginPages) Run(_ context.Context, env *Env) Result {
	a := env.App
	p := a.Profile()
	if len(p.Sites) == 0 {
		return skip(c.Name(), "no site generator")
	}
	bindings, served := loginBindings(p)
	switch {
	case served == 0:
		return skip(c.Name(), "no outlet serves a browser application")
	case len(bindings) == 0:
		return skip(c.Name(), "no browser outlet declares its auth (a hand-written router), so no login page is bound")
	case len(a.AuthPackages) == 0:
		return skip(c.Name(), "no auth package (auths-wired reports the binding)")
	}

	var details, warnings, lines []string
	held := 0
	for i := range a.AuthPackages {
		pkg := &a.AuthPackages[i]
		bound, ok := bindings[pkg.Path]
		if !ok {
			lines = append(lines, fmt.Sprintf("%s: bound to no browser outlet, so its login page is not held", pkg.Name))

			continue
		}
		constructions, err := settingsConstructions(a, pkg)
		if err != nil {
			return fail(c.Name(), err.Error())
		}
		if len(constructions) == 0 {
			warnings = append(warnings, fmt.Sprintf("%s: no file outside tests builds %s.Settings, so the page a refused or waiting sign-in returns to is not read; build the settings on the data level naming %s", pkg.Name, pkg.Name, targetList(bound.targets)))

			continue
		}
		found := 0
		for j := range constructions {
			findings := loginFindings(pkg, &constructions[j], &bound)
			if len(findings) == 0 {
				lines = append(lines, heldLine(pkg, &constructions[j], &bound))
			}
			details = append(details, findings...)
			found += len(findings)
		}
		if found == 0 {
			held++
		}
	}

	switch {
	case len(details) > 0:
		return fail(c.Name(), fmt.Sprintf("%d login page problem(s)", len(details)), append(details, warnings...)...)
	case len(warnings) > 0:
		return warn(c.Name(), fmt.Sprintf("%d auth(s) whose settings no file the check reads builds", len(warnings)), append(warnings, lines...)...)
	case held == 0:
		return skip(c.Name(), "no auth package is bound to a browser outlet")
	default:
		return passWithDetails(c.Name(), fmt.Sprintf("%d auth(s) return a refused or waiting sign-in to their outlet's login page", held), lines...)
	}
}

// loginBindings maps each auth's import path to what the browser outlets declaring it
// bind it to, and counts the outlets serving a browser application at all. A login page
// is the outlet's mount path joined with /login, a device page the mount path joined
// with /device; an auth issues when a machine outlet declares Tokens of it.
func loginBindings(p app.Profile) (bindings map[string]loginBinding, served int) {
	byPath := map[string]map[string]*loginTarget{}
	directory := map[string]bool{}
	issues := map[string]bool{}
	for i := range p.Sites {
		site := &p.Sites[i]
		surfaces := outletSurfaces(site)
		for j := range surfaces {
			o := &surfaces[j]
			if o.Tokens != "" {
				issues[o.Tokens] = true
			}
			if o.WebApp == "" {
				continue
			}
			served++
			if o.Auth == nil {
				continue
			}
			pages := byPath[o.Auth.ImportPath]
			if pages == nil {
				pages = map[string]*loginTarget{}
				byPath[o.Auth.ImportPath] = pages
			}
			page := loginPage(o.WebApp)
			t := pages[page]
			if t == nil {
				t = &loginTarget{page: page, device: devicePage(o.WebApp), mount: o.WebApp}
				pages[page] = t
			}
			t.outlets = append(t.outlets, outletKey(p, site, o))
			directory[o.Auth.ImportPath] = directory[o.Auth.ImportPath] || o.Auth.Directory()
		}
	}
	bindings = make(map[string]loginBinding, len(byPath))
	for importPath, pages := range byPath {
		b := loginBinding{directory: directory[importPath], issues: issues[importPath]}
		for _, t := range pages {
			b.targets = append(b.targets, *t)
		}
		sort.Slice(b.targets, func(i, j int) bool { return b.targets[i].page < b.targets[j].page })
		bindings[importPath] = b
	}

	return bindings, served
}

// loginPage is the login page of a browser application at the mount path: /login at the
// root, <mount>/login elsewhere.
func loginPage(mount string) string {
	return path.Join("/", mount, loginSegment)
}

// devicePage is the device page of a browser application at the mount path, where a
// signed-in person approves a command-line client's request: /device at the root,
// <mount>/device elsewhere.
func devicePage(mount string) string {
	return path.Join("/", mount, deviceSegment)
}

// settingsConstructions reads every construction of the auth's settings outside tests:
// each <auth>.Settings{...} literal of a file taking the type from the package, with the
// pages its fields name, in file order.
func settingsConstructions(a *app.App, pkg *app.AuthPackage) ([]loginSettings, error) {
	var constructions []loginSettings
	for _, rel := range pkg.References(settingsType, isTestFile) {
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, errors.Wrap(err, "parser.ParseFile()")
		}
		local := importedAs(f, pkg.Path)
		if local == "" {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if ok && isSelector(lit.Type, local, settingsType) {
				constructions = append(constructions, readSettings(rel, fset, lit))
			}

			return true
		})
	}

	return constructions, nil
}

// importedAs is the name the file refers to the import path by, its alias or the path's
// last element, or empty when the file does not import it.
func importedAs(f *ast.File, importPath string) string {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != importPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}

		return path.Base(p)
	}

	return ""
}

// isSelector reports an expression that is <local>.<name>.
func isSelector(expr ast.Expr, local, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)

	return ok && id.Name == local
}

// readSettings reads the pages one settings literal names: LoginURL, the mailed links'
// page from MailLoginURL or from the LoginURL of a Mail literal, and DevicePage.
func readSettings(rel string, fset *token.FileSet, lit *ast.CompositeLit) loginSettings {
	s := loginSettings{file: rel, line: fset.Position(lit.Pos()).Line}
	for _, elt := range lit.Elts {
		kv, key := keyedElement(elt)
		switch key {
		case loginURLField:
			s.login = readPage(fset, kv, loginURLField)
		case mailLoginField:
			s.mail = readPage(fset, kv, mailLoginField)
		case devicePageField:
			s.device = readPage(fset, kv, devicePageField)
		case mailField:
			inner, ok := kv.Value.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, e := range inner.Elts {
				if kv, key := keyedElement(e); key == loginURLField {
					s.mail = readPage(fset, kv, mailField+"."+loginURLField)
				}
			}
		}
	}

	return s
}

// keyedElement is a literal element keyed by an identifier, with the key's name; the
// name is empty for any other element.
func keyedElement(elt ast.Expr) (kv *ast.KeyValueExpr, key string) {
	kv, ok := elt.(*ast.KeyValueExpr)
	if !ok {
		return nil, ""
	}
	id, ok := kv.Key.(*ast.Ident)
	if !ok {
		return nil, ""
	}

	return kv, id.Name
}

// readPage reads the page one keyed element names.
func readPage(fset *token.FileSet, kv *ast.KeyValueExpr, name string) pageField {
	return pageField{set: true, line: fset.Position(kv.Pos()).Line, name: name, page: quotedPath(kv.Value)}
}

// quotedPath is the path a settings value quotes: the string literal itself, or the last
// string literal of a concatenation (env.PublicURL + "/console/login"), cleaned; the
// path of an absolute address. Empty when the value ends in no string literal.
func quotedPath(expr ast.Expr) string {
	for {
		switch v := expr.(type) {
		case *ast.ParenExpr:
			expr = v.X
		case *ast.BinaryExpr:
			if v.Op != token.ADD {
				return ""
			}
			expr = v.Y
		case *ast.BasicLit:
			if v.Kind != token.STRING {
				return ""
			}
			text, err := strconv.Unquote(v.Value)
			if err != nil {
				return ""
			}

			return pathOf(text)
		default:
			return ""
		}
	}
}

// pathOf is the path part of a quoted value: the value itself when it is a path, the
// path of an absolute address otherwise, each cleaned of a trailing slash.
func pathOf(text string) string {
	if u, err := url.Parse(text); err == nil && u.Scheme != "" {
		text = u.Path
	}
	if !strings.HasPrefix(text, "/") {
		return text
	}

	return path.Clean(text)
}

// loginFindings holds one construction to the pages the auth is bound at: LoginURL when
// the construction names it, the library's default when it names none and the auth has
// a directory method, the path the mailed links' field quotes when the construction
// names one, and the device page when the construction names DevicePage or a machine
// outlet carries the auth's tokens.
func loginFindings(pkg *app.AuthPackage, s *loginSettings, bound *loginBinding) []string {
	pages := make(map[string]bool, len(bound.targets))
	for _, t := range bound.targets {
		pages[t.page] = true
	}
	loginRemedy := remedy(setLoginURL, s.file, loginPagesOf(bound.targets))
	var findings []string
	switch {
	case !s.login.set:
		if bound.directory && !pages[defaultLoginPage] {
			findings = append(findings, fmt.Sprintf("%s:%d: the %s auth's settings name no LoginURL, so a refused or waiting sign-in returns to the library's default, %s, but %s; %s", s.file, s.line, pkg.Name, defaultLoginPage, targetPhrase(bound.targets), loginRemedy))
		}
	case s.login.page == "":
		findings = append(findings, fmt.Sprintf("%s:%d: the %s auth's LoginURL is not a string literal, so the page a refused or waiting sign-in returns to cannot be read; %s", s.file, s.login.line, pkg.Name, loginRemedy))
	case !pages[s.login.page]:
		findings = append(findings, fmt.Sprintf("%s:%d: the %s auth's LoginURL is %q, but %s, so a refused or waiting sign-in returns to a page nothing serves; %s", s.file, s.login.line, pkg.Name, s.login.page, targetPhrase(bound.targets), loginRemedy))
	}
	if s.mail.set {
		mailRemedy := remedy(fmt.Sprintf(endField, s.mail.name), s.file, loginPagesOf(bound.targets))
		switch {
		case s.mail.page == "":
			findings = append(findings, fmt.Sprintf("%s:%d: the %s auth's %s quotes no path, so the page its mailed links open cannot be read; %s", s.file, s.mail.line, pkg.Name, s.mail.name, mailRemedy))
		case !pages[s.mail.page]:
			findings = append(findings, fmt.Sprintf("%s:%d: the %s auth's %s ends in %q, but %s, so every mailed link opens a page nothing serves; %s", s.file, s.mail.line, pkg.Name, s.mail.name, s.mail.page, targetPhrase(bound.targets), mailRemedy))
		}
	}

	return append(findings, deviceFindings(pkg, s, bound)...)
}

// deviceFindings holds one construction to the device pages the auth is bound at:
// DevicePage when the construction names it, whether or not a machine outlet carries
// the auth's tokens, since a page nothing serves is wrong either way; the library's
// default when it names none and a machine outlet does.
func deviceFindings(pkg *app.AuthPackage, s *loginSettings, bound *loginBinding) []string {
	pages := make(map[string]bool, len(bound.targets))
	for _, t := range bound.targets {
		pages[t.device] = true
	}
	deviceRemedy := remedy(setDevicePage, s.file, devicePagesOf(bound.targets))
	switch {
	case !s.device.set:
		if bound.issues && !pages[defaultDevicePage] {
			return []string{fmt.Sprintf("%s:%d: the %s auth's settings name no DevicePage, so a command-line client is sent to the library's default, %s, but %s; %s", s.file, s.line, pkg.Name, defaultDevicePage, devicePhrase(bound.targets), deviceRemedy)}
		}
	case s.device.page == "":
		return []string{fmt.Sprintf("%s:%d: the %s auth's DevicePage is not a string literal, so the page a command-line client is sent to cannot be read; %s", s.file, s.device.line, pkg.Name, deviceRemedy)}
	case !pages[s.device.page]:
		return []string{fmt.Sprintf("%s:%d: the %s auth's DevicePage is %q, but %s, so a command-line client is sent to a page nothing serves; %s", s.file, s.device.line, pkg.Name, s.device.page, devicePhrase(bound.targets), deviceRemedy)}
	}

	return nil
}

// remedy is the sentence that fixes a finding: the page to write when the auth is bound
// at one page, one of them when it is bound at several.
func remedy(verb, file string, pages []string) string {
	if len(pages) == 1 {
		return fmt.Sprintf("%s %q in %s", verb, pages[0], file)
	}

	return fmt.Sprintf("%s one of them in %s", verb, file)
}

// loginPagesOf lists the login pages of the targets, devicePagesOf their device pages.
func loginPagesOf(targets []loginTarget) []string {
	pages := make([]string, 0, len(targets))
	for _, t := range targets {
		pages = append(pages, t.page)
	}

	return pages
}

func devicePagesOf(targets []loginTarget) []string {
	pages := make([]string, 0, len(targets))
	for _, t := range targets {
		pages = append(pages, t.device)
	}

	return pages
}

// targetPhrase says what the login page of the outlets bound to the auth is: the page
// under one mount path, or every page when the auth is bound under several; devicePhrase
// says the same of the device page.
func targetPhrase(targets []loginTarget) string {
	if len(targets) == 1 {
		return fmt.Sprintf("the login page under %s is %s", mountPhrase(targets[0]), targets[0].page)
	}

	return "the login pages under the mount paths it is bound at are " + targetList(targets)
}

func devicePhrase(targets []loginTarget) string {
	if len(targets) == 1 {
		return fmt.Sprintf("the device page under %s is %s", mountPhrase(targets[0]), targets[0].device)
	}

	return "the device pages under the mount paths it is bound at are " + pageList(devicePagesOf(targets), targets)
}

// targetList lists the login pages the auth is bound at, each with its outlets.
func targetList(targets []loginTarget) string {
	return pageList(loginPagesOf(targets), targets)
}

// pageList lists one page per target, each with the target's outlets.
func pageList(pages []string, targets []loginTarget) string {
	parts := make([]string, 0, len(targets))
	for i, t := range targets {
		parts = append(parts, fmt.Sprintf("%s (%s)", pages[i], strings.Join(t.outlets, ", ")))
	}

	return strings.Join(parts, " and ")
}

// mountPhrase names a target's mount path by its outlets.
func mountPhrase(t loginTarget) string {
	if len(t.outlets) == 1 {
		return fmt.Sprintf("the %s outlet's mount path %s", t.outlets[0], t.mount)
	}

	return fmt.Sprintf("the %s outlets' mount path %s", strings.Join(t.outlets, " and "), t.mount)
}

// heldLine renders one construction's part of the PASS lines: the page it returns a
// refused or waiting sign-in to (the library's default when a directory method's
// construction names none), the page its mailed links open when it names one, and whose
// login page that is; a password-only construction naming no LoginURL says that nothing
// is sent back. An auth whose tokens a machine outlet carries adds the page a
// command-line client is sent to, and whose device page that is.
func heldLine(pkg *app.AuthPackage, s *loginSettings, bound *loginBinding) string {
	var parts []string
	page := s.login.page
	switch {
	case s.login.set:
		parts = append(parts, "returns a refused or waiting sign-in to "+page)
	case bound.directory:
		page = defaultLoginPage
		parts = append(parts, "returns a refused or waiting sign-in to "+page+" (the library's default)")
	}
	if s.mail.set {
		parts = append(parts, "mails its links to "+s.mail.page)
		if page == "" {
			page = s.mail.page
		}
	}
	line := fmt.Sprintf("%s: %s:%d ", pkg.Name, s.file, s.line)
	switch {
	case len(parts) == 0:
		line += "names no login page, and its password method sends no sign-in back to one"
	default:
		line += strings.Join(parts, " and ")
		for _, t := range bound.targets {
			if t.page == page {
				line += ", the login page under " + mountPhrase(t)
			}
		}
		if !s.login.set && !bound.directory {
			line += "; its password method sends no sign-in back to a login page"
		}
	}

	return line + deviceHeldClause(s, bound)
}

// deviceHeldClause is the held line's clause for the device page: the page a
// command-line client is sent to (the library's default when a construction of an
// issuing auth names none), and whose device page that is; empty when the construction
// names none and no machine outlet carries the auth's tokens.
func deviceHeldClause(s *loginSettings, bound *loginBinding) string {
	var clause string
	switch {
	case s.device.set:
		clause = "; sends a command-line client to " + s.device.page
	case bound.issues:
		clause = "; sends a command-line client to " + defaultDevicePage + " (the library's default)"
	default:
		return ""
	}
	page := s.device.page
	if !s.device.set {
		page = defaultDevicePage
	}
	for _, t := range bound.targets {
		if t.device == page {
			clause += ", the device page under " + mountPhrase(t)
		}
	}

	return clause
}

// Meaning explains the invariant for the brief.
func (loginPages) Meaning() string {
	return "Every auth bound to a browser outlet returns a refused or waiting sign-in to that outlet's login page, and sends a command-line client to that outlet's device page. The session library sends a refused directory sign-in (`?code=<reason>`) and a sign-in waiting for its confirmation (`?pending=confirmation`) to the auth's `LoginURL`, which the data level names in the auth's settings construction (`<auth>.Settings{LoginURL: \"/console/login\", ...}` in `pkg/config/data.go`); a construction naming none sends them to the library's default, `/login`. The page is the outlet's mount path joined with `/login` (`/login` at the root, `/console/login`, `/portal/login`), and the path `MailLoginURL` quotes (`env.PublicURL + \"/console/login\"`) is the same page, so every mailed link opens it. The password method answers its own post and sends nobody anywhere, so a password-only auth's construction may name no `LoginURL`; its `MailLoginURL` is held all the same. When a machine outlet carries the auth's tokens (`Tokens(\"<module>/pkg/auth/<name>\")`), a device authorization tells the command-line client the page a signed-in person approves it on (`verification_uri`): the auth's `DevicePage` (`DevicePage: \"/console/device\"`), the outlet's mount path joined with `/device`, where the console mounts the library's device approval page; a construction naming none sends the client to the library's default, `/device`. A console moved from `/` to `/console/` that kept `/login` or `/device` sends them to a page nothing serves, which no build or generation sees; each line under the check names the file, the auth, what it holds and the outlet's page, and the value to set."
}
