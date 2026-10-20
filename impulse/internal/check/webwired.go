package check

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// webWired verifies that every browser application bound to a session outlet is bound to
// the outlet's auth: the resources its hand-written TypeScript takes from the generated
// constants (Resources.<Name>) are ones its generated client exports, and the XSRF cookie
// its HttpClient echoes (withXsrfConfiguration) is the one the outlet's auth issues. A
// site's browser application starts as a copy of the console's, bound to the console's
// auth; when another auth is bound to the site's outlet the generated client exports that
// auth's user surface instead, so every stale resource name fails the browser's own build
// (TS2339), which no Go build or check sees, and a stale cookie has the server refuse
// every mutating request. impulse add auth rewrites both in the sites layout, and this
// check holds the result.
type webWired struct{}

func (webWired) Name() string {
	return "web-wired"
}

func (webWired) Describe() string {
	return "every browser application names resources its generated client exports and the XSRF cookie of the auth bound to its outlet"
}

// The generated constants file a browser project's resource names come from, and the
// block of it that lists the resources.
const (
	constantsFile  = "zz_gen_constants.ts"
	resourcesBlock = "export const Resources = {"
)

var (
	// resourcesImportRE matches an import taking Resources from the generated constants.
	resourcesImportRE = regexp.MustCompile(`(?s)import\s*\{[^}]*\bResources\b[^}]*\}\s*from\s*'[^']*/zz_gen_constants'`)
	// resourceUseRE finds one Resources.<Name> expression.
	resourceUseRE = regexp.MustCompile(`\bResources\.([A-Z][A-Za-z0-9_]*)\b`)
	// resourceEntryRE matches one entry of the Resources block.
	resourceEntryRE = regexp.MustCompile(`^\s*([A-Z][A-Za-z0-9_]*):`)
	// xsrfNameRE finds the cookie withXsrfConfiguration names.
	xsrfNameRE = regexp.MustCompile(`cookieName:\s*'([^']*)'`)
)

// xsrfCall is the HttpClient feature that names the cookie.
const xsrfCall = "withXsrfConfiguration("

// webUse is one place a hand-written file names something: a resource or a cookie.
type webUse struct {
	file string
	line int
	name string
}

func (c webWired) Run(_ context.Context, env *Env) Result {
	a := env.App
	p := a.Profile()
	if len(p.Sites) == 0 {
		return skip(c.Name(), "no site generator")
	}

	var details, lines []string
	bound := 0
	for i := range p.Sites {
		site := &p.Sites[i]
		var parts []string
		surfaces := outletSurfaces(site)
		for j := range surfaces {
			o := &surfaces[j]
			if !o.ServesSessions || o.APIKey {
				continue
			}
			for _, t := range targetsFor(site.Generator, o) {
				project, ok, err := boundProjectOf(a, t)
				if err != nil {
					return fail(c.Name(), err.Error())
				}
				if !ok {
					continue // outlet-wired reports a target outside a browser project
				}
				bound++
				findings, line, err := c.project(a, outletKey(p, site, o), o, t, &project)
				if err != nil {
					return fail(c.Name(), err.Error())
				}
				details = append(details, findings...)
				parts = append(parts, line)
			}
		}
		if len(parts) > 0 {
			lines = append(lines, site.Name+": "+strings.Join(parts, "; "))
		}
	}

	switch {
	case bound == 0:
		return skip(c.Name(), "no browser project is bound to a session outlet")
	case len(details) > 0:
		return fail(c.Name(), fmt.Sprintf("%d browser binding problem(s)", len(details)), details...)
	default:
		return passWithDetails(c.Name(), fmt.Sprintf("%d browser application(s) bound to their outlets' auths", bound), lines...)
	}
}

// outletKey is the outlet as --outlet names it: the outlet's name in the flat layout, the
// site's name for a site's default outlet in the sites layout and <site>/<outlet> for
// another of its outlets.
func outletKey(p app.Profile, site *app.Site, o *outletSurface) string {
	switch {
	case p.Layout != app.LayoutSites:
		return o.Name
	case o.Default:
		return site.Name
	default:
		return site.Name + "/" + o.Name
	}
}

// project reads one browser project against its outlet (key, as --outlet names it): the
// resource names its hand-written TypeScript takes from the generated constants, against
// what the client exports, and the XSRF cookie its HttpClient names, against the outlet's
// auth's. The line is the project's part of the site's PASS line.
func (webWired) project(a *app.App, key string, o *outletSurface, t app.TSTarget, p *boundProject) (findings []string, line string, err error) {
	source := path.Join(p.WebDir, p.SourceRoot)
	constants := path.Join(t.Dir, constantsFile)
	exported, generated, err := exportedResources(a, constants)
	if err != nil {
		return nil, "", err
	}
	uses, cookies, err := webUses(a, source)
	if err != nil {
		return nil, "", err
	}
	issued := outletCookies(a, o)

	if generated {
		fix := resourceFix(key, authResources(o, issued, exported))
		for _, u := range uses {
			if exported[u.name] {
				continue
			}
			findings = append(findings, fmt.Sprintf("%s:%d: Resources.%s is not exported by %s, the %s outlet's generated client, so the browser build fails on it; %s", u.file, u.line, u.name, constants, key, fix))
		}
	}
	if len(issued) > 0 {
		for _, k := range cookies {
			if _, ok := issued[k.name]; ok {
				continue
			}
			findings = append(findings, fmt.Sprintf("%s:%d: withXsrfConfiguration names the cookie %s, which no auth bound to the %s outlet issues (%s), so the server refuses every mutating request; name %s, as impulse add auth --outlet %s writes it", k.file, k.line, k.name, key, issuedList(issued), issuedNames(issued), key))
		}
	}

	return findings, projectLine(source, uses, cookies, issued, generated), nil
}

// resourceFix is the fix a stale resource name takes: one the client exports, when the
// client exports the surface of the outlet's auth (the users page impulse add auth
// writes names it), and regeneration when it exports nothing of that auth, since the
// client then predates the outlet's binding.
func resourceFix(key, surface string) string {
	if surface == "" {
		return fmt.Sprintf("the client exports no resource of the auth bound to the %s outlet, so it predates the binding: run go generate ./...", key)
	}

	return fmt.Sprintf("rename it to a resource the client exports%s, as impulse add auth --outlet %s writes the site's users page", surface, key)
}

// projectLine renders one project's part of the PASS line: what it names, and whose.
func projectLine(source string, uses, cookies []webUse, issued map[string]string, generated bool) string {
	resources := fmt.Sprintf("%d resource(s) its generated client exports", len(uses))
	if !generated {
		resources = fmt.Sprintf("%d resource(s), with the client not generated yet", len(uses))
	}
	cookie := "no XSRF cookie"
	if len(cookies) > 0 {
		var named []string
		for _, k := range cookies {
			if auth, ok := issued[k.name]; ok {
				named = append(named, fmt.Sprintf("the %s auth's XSRF cookie (%s)", auth, k.name))
			} else {
				named = append(named, "the XSRF cookie "+k.name)
			}
		}
		cookie = strings.Join(slices.Compact(named), " and ")
	}

	return fmt.Sprintf("%s names %s and %s", source, resources, cookie)
}

// authResources names the resources of the outlet's auths among the exported ones, for
// the fix: the user surface a users page is written over.
func authResources(o *outletSurface, issued map[string]string, exported map[string]bool) string {
	var names []string
	for _, auth := range issued {
		prefix := pascal(auth)
		for name := range exported {
			if strings.HasPrefix(name, prefix) && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	auths := make([]string, 0, len(issued))
	for _, auth := range issued {
		auths = append(auths, auth)
	}
	sort.Strings(auths)
	owner := "the " + strings.Join(auths, " and ") + " auth's"
	if o.Auth == nil {
		owner = "the auths'"
	}

	return fmt.Sprintf(" (%s are %s)", owner, strings.Join(names, ", "))
}

// outletCookies lists the XSRF cookies the outlet's auth issues, by auth name: the auth
// the outlet declares, or every auth of the application under a hand-written router,
// whose declaration carries none. An auth's cookie is the one its construction names
// (session.WithXSRFCookieName), the library's default when the construction names none,
// and <auth>-xsrf, as the skeleton's auths declare it, when the construction is not read.
func outletCookies(a *app.App, o *outletSurface) map[string]string {
	cookies := map[string]string{}
	for i := range a.AuthPackages {
		pkg := &a.AuthPackages[i]
		if o.Auth != nil && pkg.Path != o.Auth.ImportPath {
			continue
		}
		cookies[xsrfCookieOf(a, pkg)] = pkg.Name
	}

	return cookies
}

// xsrfCookieOf is the XSRF cookie an auth package issues.
func xsrfCookieOf(a *app.App, pkg *app.AuthPackage) string {
	auth := authOf(a, pkg)
	switch {
	case auth == nil || auth.OptionsForwarded:
		return pkg.Name + "-xsrf"
	case auth.XSRFCookieName == "":
		return defaultXSRFCookie
	default:
		return auth.XSRFCookieName
	}
}

// issuedList renders the issued cookies with their auths, sorted by auth.
func issuedList(issued map[string]string) string {
	parts := make([]string, 0, len(issued))
	for cookie, auth := range issued {
		parts = append(parts, fmt.Sprintf("the %s auth issues %s", auth, cookie))
	}
	sort.Strings(parts)

	return strings.Join(parts, ", ")
}

// issuedNames lists the issued cookies, sorted.
func issuedNames(issued map[string]string) string {
	names := make([]string, 0, len(issued))
	for cookie := range issued {
		names = append(names, cookie)
	}
	sort.Strings(names)

	return strings.Join(names, " or ")
}

// exportedResources reads the names the generated constants' Resources block lists, and
// whether the file exists at all (a client not generated yet has nothing to compare).
func exportedResources(a *app.App, rel string) (exported map[string]bool, generated bool, err error) {
	data, err := os.ReadFile(a.Abs(rel))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.Wrap(err, "os.ReadFile()")
	}
	exported = map[string]bool{}
	_, block, ok := strings.Cut(string(data), resourcesBlock)
	if !ok {
		return exported, true, nil
	}
	block, _, _ = strings.Cut(block, "}")
	for line := range strings.Lines(block) {
		if m := resourceEntryRE.FindStringSubmatch(line); m != nil {
			exported[m[1]] = true
		}
	}

	return exported, true, nil
}

// webUses reads the hand-written TypeScript under a project's source root: every
// Resources.<Name> in a file taking Resources from the generated constants, one use per
// file and name at its first line, and every cookie withXsrfConfiguration names.
func webUses(a *app.App, source string) (uses, cookies []webUse, err error) {
	root, err := os.OpenRoot(a.Abs(source))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.Wrap(walkErr, "fs.WalkDir()")
		}
		if d.IsDir() {
			// Installed packages and build products are not the application's code.
			if skipped[d.Name()] {
				return fs.SkipDir
			}

			return nil
		}
		if !strings.HasSuffix(d.Name(), ".ts") || strings.HasPrefix(d.Name(), "zz_gen_") {
			return nil
		}
		data, err := root.ReadFile(filepath.FromSlash(p))
		if err != nil {
			return errors.Wrap(err, "os.Root.ReadFile()")
		}
		rel := path.Join(source, p)
		uses = append(uses, resourceUses(rel, string(data))...)
		cookies = append(cookies, cookieUses(rel, string(data))...)

		return nil
	})
	if err != nil {
		return nil, nil, errors.Wrap(err, "fs.WalkDir()")
	}
	sortUses(uses)
	sortUses(cookies)

	return uses, cookies, nil
}

// resourceUses finds the Resources.<Name> expressions of one file taking Resources from
// the generated constants, one per name at its first line.
func resourceUses(rel, text string) []webUse {
	if !resourcesImportRE.MatchString(text) {
		return nil
	}
	var uses []webUse
	seen := map[string]bool{}
	for i, line := range strings.Split(text, "\n") {
		for _, m := range resourceUseRE.FindAllStringSubmatch(line, -1) {
			if seen[m[1]] {
				continue
			}
			seen[m[1]] = true
			uses = append(uses, webUse{file: rel, line: i + 1, name: m[1]})
		}
	}

	return uses
}

// cookieUses finds the cookies one file's withXsrfConfiguration calls name.
func cookieUses(rel, text string) []webUse {
	var uses []webUse
	for i, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, xsrfCall) {
			continue
		}
		if m := xsrfNameRE.FindStringSubmatch(line); m != nil {
			uses = append(uses, webUse{file: rel, line: i + 1, name: m[1]})
		}
	}

	return uses
}

// sortUses orders uses by file, then line, so the findings read in tree order.
func sortUses(uses []webUse) {
	sort.Slice(uses, func(i, j int) bool {
		if uses[i].file != uses[j].file {
			return uses[i].file < uses[j].file
		}

		return uses[i].line < uses[j].line
	})
}

// Meaning explains the invariant for the brief.
func (webWired) Meaning() string {
	return "A browser application is bound to the auth of the outlet it is served from: its users page composes the library's user-management component over that auth's user surface (`Resources.<Auth>Users` and `Resources.<Auth>Roles` from the generated constants, `<ccc-user-management auth=\"<Auth>\" />`, and the spec's `<auth>-users` and `<auth>-roles` routes), and its HttpClient names that auth's XSRF cookie (`withXsrfConfiguration({ cookieName: '<auth>-xsrf' })`, the `XSRFCookie` of `pkg/auth/<auth>`), since the browser echoes the cookie in `X-XSRF-TOKEN` and the server verifies the echo. A site's browser application starts as a copy of the console's, bound to the console's auth; once the site's outlet is bound to another auth (`generation.Auth(...)` on its declaration), the generated client exports that auth's resources instead, so each stale name fails the browser's build (TS2339) and the stale cookie has the server refuse every mutating request. `impulse add auth --outlet <site>` rewrites both in the sites layout; each line under the check names a file still bound to the old auth, the name it carries, and the name to write."
}
