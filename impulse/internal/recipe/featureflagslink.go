package recipe

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/transition"
)

// FeatureFlagsLink gives every browser application's header the Feature flags link where
// it has none: the 0.6.14 step put the link into the skeleton's header, shown to anyone
// holding List on FeatureFlags and opening the web library's dialog, which an application
// rendered before it never had, so its feature administrators had the routes and the role
// and no way to open the dialog. The header is the application's own file, so the recipe
// edits it where it still has the skeleton's shape (the component class injecting
// AuthService, the session block of the template opening with the username) and names
// the header it cannot edit, with what to add by hand. A header that already opens the
// dialog is left alone, so the recipe is safe to run twice.
type FeatureFlagsLink struct{}

// FeatureFlagsLinkMeaning says what the link means in this framework.
const FeatureFlagsLinkMeaning = "A feature flag is flipped from the web library's dialog, which the application opens from a link of its own: the header shows the Feature flags link to anyone holding List on FeatureFlags (the Feature Administrator role, which also holds the flip), and the digest decides inside the dialog whether the toggles work; a header without the link leaves the role with routes and no door."

// headerDir is the header component's directory under a browser application's project.
const headerDir = "src/app/components/shared/header"

// headerFlagsMarker is what a header that opens the dialog carries.
const headerFlagsMarker = "openFeatureFlags"

// headerFlagsImports are the imports the link needs, in the order the import sorter puts
// them among the skeleton's.
var headerFlagsImports = []string{
	"import { MatDialog } from '@angular/material/dialog';",
	"import { Permissions, Resources } from '@app/service/zz_gen_constants';",
	"import { PermissionScope } from '@cccteam/resource';",
	"import { HasPermissionDirective } from '@cccteam/resource-angular/auth-has-permission';",
	"import { openFeatureFlagsDialog } from '@cccteam/resource-angular/ccc-feature-flags';",
}

// headerFlagsMembers are the class members the link needs, after the component's
// injections.
const headerFlagsMembers = `  private dialog = inject(MatDialog);

  /**
   * The grant that shows the Feature flags link: List on the flags, which is what the
   * library's dialog reads. Whether the dialog may flip one is the digest's Execute on
   * SetFeature, which the dialog asks for itself; with List alone it is read-only.
   */
  readonly flagsScope: PermissionScope = { resource: Resources.FeatureFlags, permission: Permissions.List };

  /** Opens the library's feature flags dialog over the application. */
  openFeatureFlags(): void {
    openFeatureFlagsDialog(this.dialog);
  }
`

// headerFlagsButton is the link as the template carries it, before the username.
const headerFlagsButton = `    <!--
      The feature flags dialog is the library's; the link shows under List on FeatureFlags,
      the dialog's read, and the digest decides inside the dialog whether the toggles work.
    -->
    <button mat-stroked-button class="flags" *cccHasPermission="flagsScope" (click)="openFeatureFlags()">
      Feature flags
    </button>
`

// headerUsernameAnchor opens the session block's username, which the link goes before.
const headerUsernameAnchor = `    <span class="username">`

var (
	// importSpecRE reads an import line's module specifier.
	importSpecRE = regexp.MustCompile(`^import .* from '([^']+)';$`)
	// componentImportsRE is the component's imports array, which gains the directive.
	componentImportsRE = regexp.MustCompile(`(?m)^( {2}imports: \[)`)
	// injectionRE is one injection of the component class, after the last of which the
	// members go.
	injectionRE = regexp.MustCompile(`(?m)^ {2}(?:private |readonly )?\w+ = inject\(\w+\);\n`)
)

// header is one browser application's header component, by its files' root-relative
// paths.
type header struct {
	ts, html string
}

// Name is the recipe's short name.
func (FeatureFlagsLink) Name() string { return "feature-flags-link" }

// Meaning says what the change means in this framework.
func (FeatureFlagsLink) Meaning() string { return FeatureFlagsLinkMeaning }

// Detect names each header that does not open the dialog.
func (FeatureFlagsLink) Detect(_ context.Context, a *app.App) ([]string, error) {
	headers, err := headersWithoutLink(a)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, h := range headers {
		found = append(found, h.html+": the header has no Feature flags link, so a feature administrator has no way to open the dialog")
	}

	return found, nil
}

// Apply writes the link into every header the detector names that has the skeleton's
// shape, and names the header it cannot edit, with what to add by hand.
func (FeatureFlagsLink) Apply(_ context.Context, a *app.App, _ check.Execer) (*transition.Change, error) {
	headers, err := headersWithoutLink(a)
	if err != nil {
		return nil, err
	}
	ch := &transition.Change{}
	for _, h := range headers {
		if err := addFeatureFlagsLink(a, h, ch); err != nil {
			return nil, err
		}
	}

	return ch, nil
}

// headersWithoutLink finds the header components of the application's browser
// applications that do not open the dialog, in path order: the skeleton's header
// directory under each web root's source (apps/console/web/src, the sites layout) or
// under each project of the web root (web/console/src, the others).
func headersWithoutLink(a *app.App) ([]header, error) {
	var headers []header
	for _, w := range a.WebApps {
		var matches []string
		for _, pattern := range []string{
			filepath.Join(a.Abs(w.Dir), filepath.FromSlash(headerDir), "header.component.ts"),
			filepath.Join(a.Abs(w.Dir), "*", filepath.FromSlash(headerDir), "header.component.ts"),
		} {
			found, err := filepath.Glob(pattern)
			if err != nil {
				return nil, errors.Wrap(err, "filepath.Glob()")
			}
			matches = append(matches, found...)
		}
		for _, ts := range matches {
			data, err := os.ReadFile(ts)
			if err != nil {
				return nil, errors.Wrap(err, "os.ReadFile()")
			}
			if strings.Contains(string(data), headerFlagsMarker) {
				continue
			}
			rel, err := filepath.Rel(a.Root, ts)
			if err != nil {
				return nil, errors.Wrap(err, "filepath.Rel()")
			}
			rel = filepath.ToSlash(rel)
			headers = append(headers, header{ts: rel, html: strings.TrimSuffix(rel, ".ts") + ".html"})
		}
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].ts < headers[j].ts })

	return headers, nil
}

// addFeatureFlagsLink edits one header's class and template, or records why it could not.
func addFeatureFlagsLink(a *app.App, h header, ch *transition.Change) error {
	tsData, err := os.ReadFile(a.Abs(h.ts))
	if err != nil {
		return errors.Wrap(err, "os.ReadFile()")
	}
	htmlData, err := os.ReadFile(a.Abs(h.html))
	if err != nil {
		if os.IsNotExist(err) {
			ch.Skipped = append(ch.Skipped, h.ts+": the header has no template beside it ("+path.Base(h.html)+"); add the Feature flags link to its template by hand, a button under *cccHasPermission with List on FeatureFlags calling openFeatureFlagsDialog")

			return nil
		}

		return errors.Wrap(err, "os.ReadFile()")
	}
	ts, reason := linkIntoClass(string(tsData))
	if reason != "" {
		ch.Skipped = append(ch.Skipped, h.ts+": "+reason+"; inject MatDialog, declare flagsScope (List on FeatureFlags) and openFeatureFlags() calling openFeatureFlagsDialog, and put the button under *cccHasPermission in the template by hand")

		return nil
	}
	html, reason := linkIntoTemplate(string(htmlData))
	if reason != "" {
		ch.Skipped = append(ch.Skipped, h.html+": "+reason+"; put the Feature flags button under *cccHasPermission=\"flagsScope\" where the session controls are, and give the class flagsScope and openFeatureFlags() by hand")

		return nil
	}
	if err := writeKeepingMode(a.Abs(h.ts), ts); err != nil {
		return err
	}
	if err := writeKeepingMode(a.Abs(h.html), html); err != nil {
		return err
	}
	ch.Did = append(ch.Did, h.ts+": the header injects MatDialog, declares flagsScope (List on FeatureFlags) and openFeatureFlags(), and imports the directive and the dialog", h.html+": the Feature flags button under *cccHasPermission=\"flagsScope\" before the username")

	return nil
}

// linkIntoClass gives the component class the link's imports and members, or says why
// the class has not the skeleton's shape.
func linkIntoClass(src string) (edited, reason string) {
	if !componentImportsRE.MatchString(src) {
		return "", "the component declares no imports array on a line of its own"
	}
	injections := injectionRE.FindAllStringIndex(src, -1)
	if len(injections) == 0 {
		return "", "the component class injects nothing on a line of its own, where the dialog's injection goes"
	}
	lines := strings.Split(src, "\n")
	for _, imp := range headerFlagsImports {
		spec := importSpecRE.FindStringSubmatch(imp)[1]
		if strings.Contains(src, "from '"+spec+"'") {
			continue
		}
		lines = insertImportLine(lines, imp, spec)
	}
	src = strings.Join(lines, "\n")
	src = componentImportsRE.ReplaceAllString(src, "${1}HasPermissionDirective, ")
	injections = injectionRE.FindAllStringIndex(src, -1)
	last := injections[len(injections)-1][1]

	return src[:last] + headerFlagsMembers + src[last:], ""
}

// insertImportLine puts an import among the file's imports where the sorter keeps them:
// before the first import whose specifier sorts after it, relative imports last, else
// after the last import.
func insertImportLine(lines []string, imp, spec string) []string {
	lastImport := -1
	for i, l := range lines {
		m := importSpecRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		lastImport = i
		if strings.HasPrefix(m[1], ".") || m[1] > spec {
			return append(lines[:i], append([]string{imp}, lines[i:]...)...)
		}
	}
	if lastImport < 0 {
		return append([]string{imp}, lines...)
	}

	return append(lines[:lastImport+1], append([]string{imp}, lines[lastImport+1:]...)...)
}

// linkIntoTemplate puts the button before the session block's username, or says why the
// template has not the skeleton's shape.
func linkIntoTemplate(src string) (edited, reason string) {
	switch strings.Count(src, headerUsernameAnchor) {
	case 1:
		return strings.Replace(src, headerUsernameAnchor, headerFlagsButton+headerUsernameAnchor, 1), ""
	case 0:
		return "", "the template has no username span the link goes before"
	default:
		return "", "the template has more than one username span"
	}
}

// writeKeepingMode writes a file back with the mode it had.
func writeKeepingMode(name, content string) error {
	info, err := os.Stat(name)
	if err != nil {
		return errors.Wrap(err, "os.Stat()")
	}
	if err := os.WriteFile(name, []byte(content), info.Mode()); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}

	return nil
}
