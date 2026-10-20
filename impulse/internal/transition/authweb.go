package transition

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/names"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

// The browser files bound to an auth, relative to a project's source root: the
// application configuration, whose HttpClient names the auth's XSRF cookie, and the users
// page, which composes the library's user-management component over the auth's user
// surface and names its resources, its partition and its routes.
const (
	appConfigFile = "app/app.config.ts"
	usersPageDir  = "app/components/ui/users"
)

// usersPageFiles are the users page's files that name the auth, in the order the did
// line lists them. The stylesheet names nothing.
var usersPageFiles = []string{"users.component.ts", "users.component.html", "users.component.spec.ts", "users.routes.ts"}

// baseSourceRoot is the console project's source root inside the base candidate, where
// the templates of the bound files are read from.
const baseSourceRoot = "web/console/src"

// xsrfCookieRE finds the auth whose XSRF cookie an application configuration names.
var xsrfCookieRE = regexp.MustCompile(`cookieName:\s*'([a-z][a-z0-9]*)-xsrf'`)

// webOutcome is what rebinding one browser file did.
type webOutcome int

const (
	// webAbsent is a file the project does not have.
	webAbsent webOutcome = iota
	// webUntouched is a file naming nothing of the auth it was bound to.
	webUntouched
	// webRendered is a file written again from the skeleton's template for the new auth,
	// since it was the template as rendered for the auth it was bound to.
	webRendered
	// webRewritten is a file edited since the skeleton rendered it, rewritten in place.
	webRewritten
)

// rebindWeb binds the browser application the outlet serves to the new auth, in the
// sites layout: a site's browser application starts as a copy of the console's, bound to
// the console's auth, so its users page names that auth's resources and its HttpClient
// echoes that auth's XSRF cookie, and once the outlet names another auth the generated
// client no longer exports those resources (the browser build fails on them) and the
// cookie is another auth's. A file the skeleton rendered and nobody edited is rendered
// again from the template for the new auth; a file edited since is rewritten in place.
// The flat layout is left as it was: an additional outlet's project there is the agent's,
// and the brief says what to name.
func (au Auth) rebindWeb(a *app.App, choice *OutletChoice, src *authSource, ch *Change) error {
	if a.Profile().Layout != app.LayoutSites {
		return nil
	}
	for _, t := range outletTargets(choice.Site.Generator, &choice.Outlet) {
		source, ok, err := projectSource(a, t)
		if err != nil {
			return err
		}
		if !ok {
			continue // outlet-wired reports a target outside a browser project
		}
		base := au.webBaseAuth(a, choice, source, src)
		if base == au.Name {
			continue
		}
		if err := au.rebindProject(a, source, base, ch); err != nil {
			return err
		}
	}

	return nil
}

// outletTargets lists the GenerateTypescript targets addressed to the outlet: those
// naming it with ForOutlet, or those naming no outlet for the default.
func outletTargets(g *app.Generator, o *app.Outlet) []app.TSTarget {
	var targets []app.TSTarget
	for _, t := range g.TypescriptTargets() {
		if (o.Name == app.DefaultOutletName && t.Outlet == "") || t.Outlet == o.Name {
			targets = append(targets, t)
		}
	}

	return targets
}

// projectSource finds the source root of the browser project receiving a generated
// client, root-relative, or false when no browser project holds the target.
func projectSource(a *app.App, t app.TSTarget) (source string, found bool, err error) {
	w, ok := a.WebAppFor(t.Dir)
	if !ok {
		return "", false, nil
	}
	projects, err := a.ReadAngular(w.Dir)
	if err != nil {
		return "", false, err
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(t.Dir, w.Dir), "/")
	project, ok := app.ProjectFor(projects, rel)
	if !ok {
		return "", false, nil
	}
	source = project.SourceRoot
	if source == "" {
		source = path.Join(project.Root, "src")
	}

	return path.Join(w.Dir, source), true, nil
}

// webBaseAuth is the auth the project's files are bound to before the change: the auth
// the outlet declared, else the one whose XSRF cookie the application configuration
// names (under a hand-written router the declaration carries none), else the source auth.
func (au Auth) webBaseAuth(a *app.App, choice *OutletChoice, source string, src *authSource) string {
	if choice.Outlet.Auth != nil {
		return authName(a, choice.Outlet.Auth.ImportPath)
	}
	if data, _, err := readFile(a, path.Join(source, appConfigFile)); err == nil {
		if m := xsrfCookieRE.FindSubmatch(data); m != nil {
			return string(m[1])
		}
	}

	return src.Name
}

// rebindProject rebinds one project's files from the base auth to the new one: the users
// page's files, whose every name of the auth is the auth's (names.Rename, as the skeleton
// renders them), and the application configuration's XSRF lines alone, since the rest of
// that file is the application's own providers.
func (au Auth) rebindProject(a *app.App, source, base string, ch *Change) error {
	templates, err := skeleton.FS(skeleton.Base)
	if err != nil {
		return err
	}
	outcomes := map[webOutcome][]string{}
	for _, name := range usersPageFiles {
		outcome, err := au.rebindFile(a, templates, path.Join(source, usersPageDir, name), path.Join(usersPageDir, name), base, names.Rename)
		if err != nil {
			return err
		}
		outcomes[outcome] = append(outcomes[outcome], name)
	}
	au.reportUsersPage(path.Join(source, usersPageDir), base, outcomes, ch)

	configRel := path.Join(source, appConfigFile)
	outcome, err := au.rebindFile(a, templates, configRel, appConfigFile, base, xsrfRewrite)
	if err != nil {
		return err
	}
	au.reportAppConfig(configRel, base, outcome, ch)

	return nil
}

// rebindFile rewrites one file from the base auth to the new one. The file is rendered
// again from the skeleton's template when it is that template as rendered for the base
// auth; otherwise rewrite edits it in place, and a file it leaves as it was names nothing
// of the base auth.
func (au Auth) rebindFile(a *app.App, templates fs.FS, rel, templateRel, base string, rewrite func(text, from, to string) string) (webOutcome, error) {
	if _, err := os.Stat(a.Abs(rel)); errors.Is(err, os.ErrNotExist) {
		return webAbsent, nil
	}
	data, mode, err := readFile(a, rel)
	if err != nil {
		return webAbsent, err
	}
	template, err := fs.ReadFile(templates, path.Join(baseSourceRoot, templateRel))
	if err != nil {
		return webAbsent, errors.Wrapf(err, "fs.ReadFile(): %s", templateRel)
	}
	text := string(data)
	if text == names.Rename(string(template), skeleton.PlaceholderAuth, base) {
		rendered := names.Rename(string(template), skeleton.PlaceholderAuth, au.Name)
		if err := os.WriteFile(a.Abs(rel), []byte(rendered), mode); err != nil {
			return webAbsent, errors.Wrap(err, "os.WriteFile()")
		}

		return webRendered, nil
	}
	edited := rewrite(text, base, au.Name)
	if edited == text {
		return webUntouched, nil
	}
	if err := os.WriteFile(a.Abs(rel), []byte(edited), mode); err != nil {
		return webAbsent, errors.Wrap(err, "os.WriteFile()")
	}

	return webRewritten, nil
}

// xsrfRewrite renames the auth in an application configuration's XSRF lines alone: the
// cookie name HttpClient echoes and the comment naming the auth and its package.
func xsrfRewrite(text, from, to string) string {
	for _, r := range []rewrite{
		{"'" + from + "-xsrf'", "'" + to + "-xsrf'"},
		{"pkg/auth/" + from, "pkg/auth/" + to},
		{"the " + from + " auth", "the " + to + " auth"},
	} {
		text = strings.ReplaceAll(text, r.old, r.new)
	}

	return text
}

// reportUsersPage records what rebinding the users page did: the files rendered again
// and the files rewritten in place, each group in one line. A page with no file naming
// the base auth is not the auth's, and nothing is said.
func (au Auth) reportUsersPage(dir, base string, outcomes map[webOutcome][]string, ch *Change) {
	basePascal := names.Pascal(base)
	if rendered := outcomes[webRendered]; len(rendered) > 0 {
		ch.didf("%s: the users page (%s) rendered again from the skeleton's for the %s auth, in place of the %s auth's: Resources.%[6]sUsers and Resources.%[6]sRoles, <ccc-user-management auth=%[7]q />, and the spec's %[3]s-users and %[3]s-roles routes", dir, strings.Join(rendered, ", "), au.Name, base, basePascal, au.Pascal(), au.Pascal())
	}
	if rewritten := outcomes[webRewritten]; len(rewritten) > 0 {
		ch.didf("%s: the users page (%s), edited since the skeleton rendered it, rewritten in place: every name of the %s auth is the %s auth's (%sUsers to %sUsers, %s-users to %s-users, %s to %s)", dir, strings.Join(rewritten, ", "), base, au.Name, basePascal, au.Pascal(), base, au.Name, basePascal, au.Pascal())
	}
}

// reportAppConfig records what rebinding the application configuration did, or what is
// left when it names no XSRF cookie of the base auth.
func (au Auth) reportAppConfig(rel, base string, outcome webOutcome, ch *Change) {
	switch outcome {
	case webRendered:
		ch.didf("%s: rendered again from the skeleton's: its HttpClient names the %s auth's XSRF cookie (%s-xsrf) in place of the %s auth's", rel, au.Name, au.Name, base)
	case webRewritten:
		ch.didf("%s: its HttpClient names the %s auth's XSRF cookie (%s-xsrf) in place of the %s auth's; the rest of the file, edited since the skeleton rendered it, is as it was", rel, au.Name, au.Name, base)
	case webUntouched:
		ch.skipf("%s: names no XSRF cookie of the %s auth, so nothing was rewritten; name the %s auth's (%s-xsrf) in withXsrfConfiguration, since the browser echoes that cookie in X-XSRF-TOKEN and the server verifies the echo", rel, base, au.Name, au.Name)
	case webAbsent:
		ch.skipf("%s: no application configuration to name the %s auth's XSRF cookie (%s-xsrf) in; name it in the project's withXsrfConfiguration", rel, au.Name, au.Name)
	}
}

// webPlan is the sentence the brief carries about the browser application, for the
// Meaning: what the transition did in the sites layout, what is the agent's in the flat.
func webPlan(outlet string) string {
	return fmt.Sprintf("In the sites layout the site's browser application is rebound with the auth: its users page names the auth's resources and partition, and its HttpClient the auth's XSRF cookie, each file rendered again from the skeleton's when nobody edited it and rewritten in place otherwise (the did lines say which); the web-wired check holds the result. In the flat layout the %s outlet's project is yours to rebind the same way.", outlet)
}
