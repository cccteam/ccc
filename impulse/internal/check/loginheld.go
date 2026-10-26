package check

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// loginHeld verifies that every login page rendering the library's login component
// holds its card back while the component's arrival reads are in flight: the page gives
// the component a template reference (<ccc-login-methods #methods>), reads its arriving
// signal (methods.arriving()), and, where the read binds a class on the card
// ([class.arriving]), a stylesheet of the page hides that class (.login-card.arriving
// { visibility: hidden }). The component renders nothing while it is arriving (the
// methods the server lists, the session), so a page that does not hold its card shows
// an empty card and then fills it, where a page shows nothing until it knows what it
// renders and then renders whole, the way a page loads. The skeletons' login pages carry
// the shape; an application's own login page takes it by hand, which no build or
// generation sees, and this check holds it.
type loginHeld struct{}

func (loginHeld) Name() string { return "login-held" }

func (loginHeld) Describe() string {
	return "every login page rendering the library's login component gives it a template reference and reads its arriving signal, hiding the card while it is on, so the page shows nothing until the component knows what it renders"
}

// The library's login component, as a page's template names it, and the stylesheet
// kinds a component's decorator may name.
const loginComponent = "ccc-login-methods"

var (
	// loginComponentRE matches the login component's opening tag, through its end.
	loginComponentRE = regexp.MustCompile(`(?s)<` + loginComponent + `\b[^>]*>`)
	// templateRefRE finds a template reference (#name) in a tag.
	templateRefRE = regexp.MustCompile(`#([A-Za-z_$][A-Za-z0-9_$]*)`)
	// styleURLsRE finds the stylesheets a component's decorator names (styleUrl: '...'
	// or styleUrls: ['...', ...]).
	styleURLsRE = regexp.MustCompile(`styleUrls?\s*:\s*\[?\s*((?:'[^']*'|"[^"]*")(?:\s*,\s*(?:'[^']*'|"[^"]*"))*)`)
	// quotedRE finds one quoted string.
	quotedRE = regexp.MustCompile(`'([^']*)'|"([^"]*)"`)
	// inlineStylesRE finds a component's inline styles (styles: `...` or styles: [`...`]).
	inlineStylesRE = regexp.MustCompile("(?s)styles\\s*:\\s*\\[?\\s*`([^`]*)`")
)

// loginTemplate is one template rendering the login component: where, the template
// reference the component carries, where the page reads the reference's arriving
// signal, and the class the read binds on the card, when it binds one.
type loginTemplate struct {
	file string
	// line is the line of the component's tag.
	line int
	// ref is the component's template reference; empty when it carries none.
	ref string
	// read is the line the page reads <ref>.arriving() on; 0 when it never does.
	read int
	// class is the class the page binds to the signal ([class.<class>]); empty when
	// the read is not a class binding.
	class string
}

// heldTally is what the check gathers over the browser projects bound to session
// outlets: how many it read, how many of their pages hold their card and how many
// show it, the findings, the projects rendering the component nowhere, and the lines
// of the held pages.
type heldTally struct {
	bound, held, shown       int
	details, warnings, lines []string
}

func (c loginHeld) Run(_ context.Context, env *Env) Result {
	a := env.App
	p := a.Profile()
	if len(p.Sites) == 0 {
		return skip(c.Name(), "no site generator")
	}

	var tally heldTally
	seen := map[string]bool{}
	for i := range p.Sites {
		site := &p.Sites[i]
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
				source := path.Join(project.WebDir, project.SourceRoot)
				if !ok || seen[source] {
					continue // outlet-wired reports a target outside a browser project
				}
				seen[source] = true
				if err := c.project(a, source, &tally); err != nil {
					return fail(c.Name(), err.Error())
				}
			}
		}
	}

	switch {
	case tally.bound == 0:
		return skip(c.Name(), "no browser project is bound to a session outlet")
	case tally.shown > 0:
		return fail(c.Name(), fmt.Sprintf("%d login page(s) show their card before the login component's arrival reads answer", tally.shown), append(tally.details, tally.warnings...)...)
	case len(tally.warnings) > 0:
		return warn(c.Name(), fmt.Sprintf("%d browser application(s) render the library's login component nowhere", len(tally.warnings)), append(tally.warnings, tally.lines...)...)
	default:
		return passWithDetails(c.Name(), fmt.Sprintf("%d login page(s) hold their card back while the login component's arrival reads are in flight", tally.held), tally.lines...)
	}
}

// project reads one bound browser project's pages rendering the login component into
// the tally: each held page's line, each shown page's findings, or the warning that the
// project renders the component nowhere.
func (loginHeld) project(a *app.App, source string, tally *heldTally) error {
	tally.bound++
	pages, err := loginComponentPages(a, source)
	if err != nil {
		return err
	}
	if len(pages) == 0 {
		tally.warnings = append(tally.warnings, fmt.Sprintf("%s renders the library's login component (<%s>) nowhere, so whether its login page holds its card back is not read", source, loginComponent))

		return nil
	}
	for k := range pages {
		findings, err := pages[k].findings(a)
		if err != nil {
			return err
		}
		if len(findings) > 0 {
			tally.shown++
			tally.details = append(tally.details, findings...)

			continue
		}
		line, err := pages[k].heldLine(a)
		if err != nil {
			return err
		}
		tally.held++
		tally.lines = append(tally.lines, line)
	}

	return nil
}

// loginComponentPages finds the templates under a project's source root that render the
// library's login component: the .html files, and the .ts files holding an inline
// template, the generated files and the specs aside, in path order.
func loginComponentPages(a *app.App, source string) ([]loginTemplate, error) {
	root, err := os.OpenRoot(a.Abs(source))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()
	var pages []loginTemplate
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
		name := d.Name()
		if (!strings.HasSuffix(name, ".html") && !strings.HasSuffix(name, ".ts")) || strings.HasPrefix(name, "zz_gen_") || strings.HasSuffix(name, ".spec.ts") {
			return nil
		}
		data, err := root.ReadFile(filepath.FromSlash(p))
		if err != nil {
			return errors.Wrap(err, "os.Root.ReadFile()")
		}
		text := string(data)
		if !strings.Contains(text, "<"+loginComponent) {
			return nil
		}
		pages = append(pages, readLoginPage(path.Join(source, p), text))

		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "fs.WalkDir()")
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].file < pages[j].file })

	return pages, nil
}

// readLoginPage reads one template rendering the login component: the component's
// template reference, the line the page reads its arriving signal on, and the class the
// read binds on the card when it is a class binding.
func readLoginPage(rel, text string) loginTemplate {
	pg := loginTemplate{file: rel}
	loc := loginComponentRE.FindStringIndex(text)
	if loc == nil {
		pg.line = lineAt(text, strings.Index(text, "<"+loginComponent))

		return pg
	}
	pg.line = lineAt(text, loc[0])
	m := templateRefRE.FindStringSubmatch(text[loc[0]:loc[1]])
	if m == nil {
		return pg
	}
	pg.ref = m[1]
	ref := regexp.QuoteMeta(pg.ref)
	if i := regexp.MustCompile(`\b` + ref + `\.arriving\(\)`).FindStringIndex(text); i != nil {
		pg.read = lineAt(text, i[0])
	}
	if m := regexp.MustCompile(`\[class\.([A-Za-z0-9_-]+)\]\s*=\s*"\s*` + ref + `\.arriving\(\)\s*"`).FindStringSubmatch(text); m != nil {
		pg.class = m[1]
	}

	return pg
}

// lineAt is the 1-based line of a byte offset in text.
func lineAt(text string, offset int) int {
	if offset < 0 {
		return 1
	}

	return 1 + strings.Count(text[:offset], "\n")
}

// findings holds one page to the shape: the component carries a reference, the page
// reads its arriving signal, and the class the read binds, when it binds one, is hidden
// by a stylesheet of the page.
func (pg *loginTemplate) findings(a *app.App) ([]string, error) {
	const shape = `bind the card's arriving class to it ([class.arriving]="%s.arriving()" on the <mat-card class="login-card">), with .login-card.arriving { visibility: hidden } in the page's stylesheet, as the skeletons' login pages do`
	switch {
	case pg.ref == "":
		return []string{fmt.Sprintf("%s:%d: the login component carries no template reference, so the page cannot read its arriving signal and its card shows before the component knows what it renders; write <%s #methods ...> and %s", pg.file, pg.line, loginComponent, fmt.Sprintf(shape, "methods"))}, nil
	case pg.read == 0:
		return []string{fmt.Sprintf("%s:%d: the page never reads %s.arriving(), so its card shows before the login component knows what it renders; %s", pg.file, pg.line, pg.ref, fmt.Sprintf(shape, pg.ref))}, nil
	case pg.class == "":
		return nil, nil
	}
	sheet, read, err := pg.hiddenBy(a)
	if err != nil {
		return nil, err
	}
	if sheet != "" {
		return nil, nil
	}
	looked := "the page names no stylesheet"
	if len(read) > 0 {
		looked = "read " + strings.Join(read, ", ")
	}

	return []string{fmt.Sprintf("%s:%d: the page binds the %s class to %s.arriving(), but no stylesheet of the page hides .%s (%s), so the card shows while the arrival reads are in flight; write .login-card.%s { visibility: hidden } in the page's stylesheet", pg.file, pg.read, pg.class, pg.ref, pg.class, looked, pg.class)}, nil
}

// heldLine renders one held page's PASS line: the reference, where the signal is read,
// and, for a class binding, the stylesheet hiding the class.
func (pg *loginTemplate) heldLine(a *app.App) (string, error) {
	line := fmt.Sprintf("%s: <%s #%s> at line %d, %s.arriving() read at line %d", pg.file, loginComponent, pg.ref, pg.line, pg.ref, pg.read)
	if pg.class == "" {
		return line, nil
	}
	sheet, _, err := pg.hiddenBy(a)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s, the %s class hidden by %s", line, pg.class, sheet), nil
}

// hiddenBy finds the stylesheet of the page that names the class the page binds: the
// stylesheets the component's decorator names (styleUrl, styleUrls) and its inline
// styles, read from the component's TypeScript file (the template's sibling .ts, or the
// file itself for an inline template), or the template's sibling stylesheet when the
// decorator names none. It returns the stylesheet (the file, or the inline styles of
// the component file), empty when none names the class, with the stylesheets read.
func (pg *loginTemplate) hiddenBy(a *app.App) (sheet string, read []string, err error) {
	selector := regexp.MustCompile(`\.` + regexp.QuoteMeta(pg.class) + `(?:[^A-Za-z0-9_-]|$)`)
	component := pg.file
	if strings.HasSuffix(component, ".html") {
		component = strings.TrimSuffix(component, ".html") + ".ts"
	}
	// The stylesheets a component names are read inside the application's root, so a
	// name reaching outside it is refused rather than followed.
	root, err := os.OpenRoot(a.Root)
	if err != nil {
		return "", nil, errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()
	var sheets []string
	inline := ""
	text, err := root.ReadFile(filepath.FromSlash(component))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return "", nil, errors.Wrap(err, "os.Root.ReadFile()")
	default:
		if m := styleURLsRE.FindStringSubmatch(string(text)); m != nil {
			for _, q := range quotedRE.FindAllStringSubmatch(m[1], -1) {
				sheets = append(sheets, path.Join(path.Dir(component), q[1]+q[2]))
			}
		}
		if m := inlineStylesRE.FindStringSubmatch(string(text)); m != nil {
			inline = m[1]
		}
	}
	if len(sheets) == 0 && inline == "" {
		stem := strings.TrimSuffix(strings.TrimSuffix(pg.file, ".html"), ".ts")
		for _, ext := range []string{".scss", ".css", ".sass", ".less"} {
			if _, statErr := root.Stat(filepath.FromSlash(stem + ext)); statErr == nil {
				sheets = append(sheets, stem+ext)
			}
		}
	}
	for _, s := range sheets {
		data, readErr := root.ReadFile(filepath.FromSlash(s))
		if readErr != nil {
			// A stylesheet named but missing, or reaching outside the root, hides nothing.
			continue
		}
		read = append(read, s)
		if selector.MatchString(string(data)) {
			return s, read, nil
		}
	}
	if inline != "" {
		read = append(read, "the inline styles of "+component)
		if selector.MatchString(inline) {
			return "the inline styles of " + component, read, nil
		}
	}

	return "", read, nil
}

// Meaning explains the invariant for the brief.
func (loginHeld) Meaning() string {
	return "A login page shows nothing until the library's login component knows what it renders, and then renders whole, the way a page loads. The component (`<ccc-login-methods>`, from `@cccteam/resource-angular` 0.0.17) renders nothing while its arrival reads are in flight (the methods the server lists at `user/methods`, the session at `user/session`) and says so through a signal, `arriving()`. The page gives the component a template reference (`<ccc-login-methods #methods ...>`) and binds the card's arriving class to the signal (`<mat-card class=\"login-card\" [class.arriving]=\"methods.arriving()\">`), and the page's stylesheet hides the held card (`.login-card.arriving { visibility: hidden }`), so nothing stands in for the card meanwhile and no progress bar runs on every load; a page that reads the signal another way (`@if`, `[hidden]`) holds its card as well. The skeletons' login pages carry the shape (`web/<app>/src/app/components/login/login.component.html` and `.scss`); an application's own login page takes it by hand, in its template and its stylesheet, and this check holds it."
}
