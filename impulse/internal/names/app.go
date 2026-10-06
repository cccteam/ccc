package names

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/module"
)

// appNameRE is the shape of an application name: what a module path's last segment
// usually is, and what the web package name, the service name, and the development
// database ids accept in common. Lowercase letters, digits, and hyphens, starting with a
// letter and ending with a letter or digit.
var appNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// maxAppName bounds the name so that <name> fits a Spanner database id and <name>-dev a
// Google Cloud project id, both 30 characters at most, as the development environment
// template spells them.
const maxAppName = 26

// AppGuidance is the one-line answer to "what is a good application name".
const AppGuidance = "lowercase letters, digits, and hyphens, starting with a letter and ending with a letter or digit, at most 26 characters: beacon, harbor, field-ops"

// App is the application name a module path implies: its last segment before any major
// version suffix (example.com/acme/beacon/v2 names beacon), lowercased. The result may
// still fail ValidateApp (example.com/acme/beacon.service), in which case the name has to
// be given.
func App(modulePath string) string {
	prefix, _, ok := module.SplitPathVersion(modulePath)
	if !ok {
		prefix = modulePath
	}

	return strings.ToLower(path.Base(prefix))
}

// ValidateApp checks an application name. The name becomes the web workspace's package
// name (<name>-web), the service name, and the development Spanner project (<name>-dev),
// instance, and database, so it takes what those accept in common.
func ValidateApp(name string) error {
	if !appNameRE.MatchString(name) || len(name) > maxAppName {
		return errors.Newf("application name %q: %s", name, AppGuidance)
	}

	return nil
}

// The slots a template carries its candidate's name in as the application's name.
var (
	// workspaceNameRE matches the workspace name field of package.json and bun.lock; the
	// value's first segment is the application name (<app>-web, <app>-<site>-web).
	workspaceNameRE = regexp.MustCompile(`("name":\s*")([^"]*)"`)
	// envValueRE matches an environment template line whose value is the application
	// name or starts with it (APP_SERVICE_NAME=<app>, GOOGLE_CLOUD_SPANNER_PROJECT=<app>-dev).
	envValueRE = regexp.MustCompile(`(?m)^(export\s+\w+=)([a-z0-9-]+)$`)
	// envHeadingRE matches the environment template's opening comment, which names the
	// application.
	envHeadingRE = regexp.MustCompile(`(?m)^(# Development environment for )([a-z0-9-]+)\.`)
	// manifestNameRE matches the name and short_name fields of a web app manifest, whose
	// value is the application's display name or starts with it (Solo, Solo Portal).
	manifestNameRE = regexp.MustCompile(`("(?:name|short_name)":\s*")([^"]*)"`)
)

// webManifest is the name of a browser project's web app manifest, the file the browser
// reads when it installs the application.
const webManifest = "manifest.webmanifest"

// envTemplates are the environment template names an application may carry.
var envTemplates = map[string]bool{".envrc.template": true, ".env.template": true, ".env.example": true}

// browserSource reports a file of a browser application's source tree that may show
// the application's name as a title: the page, and the components.
func browserSource(rel string) bool {
	base := path.Base(rel)
	if !strings.Contains(rel, "/src/") {
		return false
	}

	return base == "index.html" || strings.HasSuffix(base, ".component.ts") || strings.HasSuffix(base, ".component.html")
}

// Display is the application name as a title: each hyphenated word capitalized, the
// hyphens spaces (field-ops shows as Field Ops).
func Display(name string) string {
	words := strings.Split(name, "-")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}

	return strings.Join(words, " ")
}

// RenameApp substitutes the application name where a template carries its candidate's
// name as the application's: the workspace name in package.json and bun.lock
// (<candidate>-web, <candidate>-<site>-web), the environment template's values that name
// the service and the development database (<candidate>, <candidate>-dev) and its
// heading comment, the root README's heading, and, in display form, the browser
// projects' titles and the name and short_name of their web app manifests (Solo, Solo
// Portal). rel is the file's path in the tree and text its content; a file outside those
// slots comes back unchanged. Nowhere else: the candidate names are English words (sites,
// outlets), so a template's prose says "the application" rather than naming it.
func RenameApp(rel, text, from, to string) string {
	base := path.Base(rel)
	switch {
	case base == webManifest:
		// The manifest names the installed application in display form, alone or with the
		// project's suffix (Solo Portal).
		fromTitle, toTitle := Display(from), Display(to)

		return manifestNameRE.ReplaceAllStringFunc(text, func(m string) string {
			sub := manifestNameRE.FindStringSubmatch(m)
			name := sub[2]
			if name != fromTitle && !strings.HasPrefix(name, fromTitle+" ") {
				return m
			}

			return sub[1] + toTitle + strings.TrimPrefix(name, fromTitle) + `"`
		})
	case base == "package.json" || base == "bun.lock":
		return workspaceNameRE.ReplaceAllStringFunc(text, func(m string) string {
			sub := workspaceNameRE.FindStringSubmatch(m)
			name := sub[2]
			if name != from && !strings.HasPrefix(name, from+"-") {
				return m
			}

			return sub[1] + to + strings.TrimPrefix(name, from) + `"`
		})
	case envTemplates[base]:
		text = envValueRE.ReplaceAllStringFunc(text, func(m string) string {
			sub := envValueRE.FindStringSubmatch(m)
			value := sub[2]
			if value != from && !strings.HasPrefix(value, from+"-") {
				return m
			}

			return sub[1] + to + strings.TrimPrefix(value, from)
		})

		return envHeadingRE.ReplaceAllStringFunc(text, func(m string) string {
			sub := envHeadingRE.FindStringSubmatch(m)
			if sub[2] != from {
				return m
			}

			return sub[1] + to + "."
		})
	case rel == "README.md":
		if rest, ok := strings.CutPrefix(text, "# "+from+"\n"); ok {
			return "# " + to + "\n" + rest
		}

		return text
	case browserSource(rel):
		// The browser application shows the application's name as a title: the page
		// title, the component's title field, the login card's heading. The candidate's
		// name stands there in display form (Solo), as a whole HTML text or string.
		fromTitle, toTitle := Display(from), Display(to)
		for _, q := range []string{">%s<", "'%s'", `"%s"`} {
			text = strings.ReplaceAll(text, fmt.Sprintf(q, fromTitle), fmt.Sprintf(q, toTitle))
		}

		return text
	default:
		return text
	}
}

// RenameWorkspace renames a browser workspace where package.json and bun.lock spell its
// name, leaving every other name (a dependency's, a script's) alone.
func RenameWorkspace(text, from, to string) string {
	return workspaceNameRE.ReplaceAllStringFunc(text, func(m string) string {
		sub := workspaceNameRE.FindStringSubmatch(m)
		if sub[2] != from {
			return m
		}

		return sub[1] + to + `"`
	})
}
