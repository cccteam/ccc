package check

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// installable verifies that every browser application bound to a session outlet installs
// as a progressive web app, and that the server serves it through the resource package's
// served browser app. On the browser side: the service worker package is a dependency at
// the workspace's Angular line; the project's production configuration names a worker
// config that exists, whose navigation patterns exclude the outlet's API under the mount,
// so the login, callback and stored-file navigations reach the server instead of the cached
// entry document; the app config provides the worker and the library's update provider;
// the entry document links the web app manifest, which identifies the application by its
// mount path and starts it there; every icon the manifest declares is there at the size
// it declares, read from the PNG header; the build defines APP_VERSION and the
// workspace's build script redefines it from VERSION, so a release build stamps its
// release and any other dev; and the app config provides API_VERSION from it and
// registers the version interceptor, so every request carries the release the server
// checks. On the server side: the application's asset
// handlers are built from resource.NewBrowserApp with the outlet's mount path, which holds
// the deep-link rewrite and the two cache classes a worker needs, and the module the type
// replaces, github.com/jtwatson/spaassets, is imported nowhere and gone from go.mod. A
// project with none of the browser side warns as not installable, since an application
// built before the worker still works; a project with part of it fails, naming the first
// missing piece, since a half-installed application misbehaves in ways that are hard to
// read (a worker without the API exclusion answers a login with the entry document).
type installable struct{}

func (installable) Name() string { return "installable" }

func (installable) Describe() string {
	return "every browser application bound to a session outlet installs as a progressive web app (worker dependency and config excluding its API, manifest and icons, update provider, the release stamped and sent with every request) and is served through the resource package's served browser app"
}

func (installable) Meaning() string {
	return "An installable application is one the browser can install, with a service worker that keeps the files of the build an open tab loaded, so a release never breaks that tab, and a web app manifest that names and identifies the application. The skeletons carry the whole shape: `@angular/service-worker` beside the other Angular packages in `package.json`; `ngsw-config.json` at the workspace root, named by every project's production configuration (`\"serviceWorker\": \"ngsw-config.json\"`), whose `navigationUrls` carry `!/api/**` so the login, callback and stored-file navigations under the mount reach the server; `provideServiceWorker('ngsw-worker.js', { enabled: !isDevMode(), registrationStrategy: 'registerWhenStable:30000' })` and `provideAppUpdate()` (from `@cccteam/resource-angular/ui-app-update`) in `app.config.ts`; `public/manifest.webmanifest` with `id` the mount path with a trailing slash (`/` or `/console/`), `scope` and `start_url` `./`, and three PNG icons under `public/icons/` whose dimensions match their `sizes`; and `<link rel=\"manifest\">` in `index.html`. The release reaches the browser the same way in every skeleton: the project's build options define `APP_VERSION` as `'dev'`, the workspace's `build` script redefines it from the `VERSION` environment variable (`ng build console --define \"APP_VERSION='${VERSION:-dev}'\"`, which the image's browser stage sets), and `app.config.ts` provides it as `API_VERSION` (`{ provide: API_VERSION, useValue: APP_VERSION }`, the token from `@cccteam/resource-angular/types`) and registers `apiVersionInterceptor` through `provideHttpClient(withInterceptors([apiVersionInterceptor]))`, so the client and the application's own HttpClient calls send `X-Api-Version` and the server can refuse a build it no longer answers; a `dev` build sends no header. On the server the App holds a `*resource.BrowserApp` per bundle, built with `resource.NewBrowserApp(dir, mount)`, and its `DeepLink` and `Assets` delegate to it one line each, which retires `github.com/jtwatson/spaassets` and the application's own cache header code. Each line under the check names the first missing piece of one project, by file."
}

// The names the check reads: the Angular package that ships the worker and the package
// whose version line it must share, the module the served browser app replaces, the
// constructor of the served browser app, and the manifest's file name.
const (
	serviceWorkerPackage  = "@angular/service-worker"
	angularCore           = "@angular/core"
	spaassetsModule       = "github.com/jtwatson/spaassets"
	browserAppConstructor = "NewBrowserApp"
	manifestFile          = "manifest.webmanifest"
)

// The names the release pieces read: the identifier the build defines, the environment
// variable the build script redefines it from, the token the app config provides it as,
// and the interceptor that adds it to the application's own HttpClient calls.
const (
	versionDefine      = "APP_VERSION"
	versionVariable    = "VERSION"
	versionToken       = "API_VERSION"
	versionInterceptor = "apiVersionInterceptor"
)

// versionFeed is the build script flag that redefines the identifier from the variable,
// as package.json spells it.
const versionFeed = `--define \"APP_VERSION='${VERSION:-dev}'\"`

// installState is how far one browser project is along the shape.
type installState int

const (
	// installedAll is a project with every piece.
	installedAll installState = iota
	// installedNone is a project with no piece of the browser side: not installable.
	installedNone
	// installedPart is a project with some pieces and not others.
	installedPart
)

func (c installable) Run(_ context.Context, env *Env) Result {
	a := env.App
	p := a.Profile()
	if len(p.Sites) == 0 {
		return skip(c.Name(), "no site generator")
	}

	var failures, notes []string
	installed, served := 0, 0
	for i := range p.Sites {
		site := &p.Sites[i]
		g := site.Generator
		surfaces := outletSurfaces(site)
		for i := range surfaces {
			o := &surfaces[i]
			if o.WebApp == "" || !o.ServesSessions {
				continue
			}
			for _, t := range targetsFor(g, o) {
				project, ok, err := c.projectOf(a, t)
				if err != nil {
					return fail(c.Name(), err.Error())
				}
				if !ok {
					continue // outlet-wired reports a target outside a browser project
				}
				served++
				state, finding, err := c.project(a, g.HandlersDir(), &project, o)
				if err != nil {
					return fail(c.Name(), err.Error())
				}
				switch state {
				case installedAll:
					installed++
				case installedNone:
					notes = append(notes, finding)
				case installedPart:
					failures = append(failures, finding)
				}
			}
		}
	}

	switch {
	case served == 0:
		return skip(c.Name(), "no browser project is bound to a session outlet")
	case len(failures) > 0:
		return fail(c.Name(), fmt.Sprintf("%d browser application(s) partly installable; the first missing piece of each is named", len(failures)), append(failures, notes...)...)
	case len(notes) > 0:
		return warn(c.Name(), fmt.Sprintf("%d of %d browser application(s) install as progressive web apps; %d not installable", installed, served, len(notes)), notes...)
	default:
		return pass(c.Name(), fmt.Sprintf("%d browser application(s) install as progressive web apps", installed))
	}
}

// boundProject is one browser project bound to a session outlet, with the workspace it
// lives in.
type boundProject struct {
	app.AngularProject
	// WebDir is the root-relative workspace directory holding angular.json.
	WebDir string
}

// projectOf finds the browser project receiving a generated client: the workspace whose
// directory contains the target's, and the project whose root does.
func (installable) projectOf(a *app.App, t app.TSTarget) (boundProject, bool, error) {
	w, ok := a.WebAppFor(t.Dir)
	if !ok {
		return boundProject{}, false, nil
	}
	projects, err := a.ReadAngular(w.Dir)
	if err != nil {
		return boundProject{}, false, err
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(t.Dir, w.Dir), "/")
	project, ok := app.ProjectFor(projects, rel)
	if !ok {
		return boundProject{}, false, nil
	}

	return boundProject{AngularProject: project, WebDir: w.Dir}, true, nil
}

// piece is one part of the shape as the check read it: whether the project has it, and
// the finding when it is missing or wrong (a piece can be present and wrong, such as the
// dependency at another line).
type piece struct {
	present bool
	finding string
}

// project reads one browser project against the shape and reports how far along it is,
// with the first missing piece as the finding (or, for a project with none of the
// browser side, the note saying so). The pieces are read in the order a developer adds
// them: the dependency, the worker config, its API exclusion, the providers, the manifest
// link, the manifest and its icons, the release (the define, the build script that feeds
// it, the token and the interceptor), and the server's handlers.
func (installable) project(a *app.App, handlersDir string, p *boundProject, o *outletSurface) (installState, string, error) {
	manifest, err := readPackageJSON(a, p)
	if err != nil {
		return 0, "", err
	}
	var pieces []piece
	pieces = append(pieces, dependencyPiece(p, manifest))

	config, configRel := workerConfigPiece(a, p)
	pieces = append(pieces, config)
	if configRel != "" {
		finding, err := apiExclusionFinding(a, configRel, o)
		if err != nil {
			return 0, "", err
		}
		pieces = append(pieces, piece{present: finding == "", finding: finding})
	}

	providers, version, err := providerPieces(a, p)
	if err != nil {
		return 0, "", err
	}
	pieces = append(pieces, providers)

	link, href, err := manifestLinkPiece(a, p)
	if err != nil {
		return 0, "", err
	}
	pieces = append(pieces, link)
	if href != "" {
		finding, err := manifestFinding(a, p, href, o)
		if err != nil {
			return 0, "", err
		}
		pieces = append(pieces, piece{present: finding == "", finding: finding})
	}
	pieces = append(pieces, definePiece(p), scriptPiece(p, manifest), version)

	// The browser side is what "none of it" means; the server side is read after it.
	present := 0
	for _, pc := range pieces {
		if pc.present {
			present++
		}
	}
	findings, err := handlerFindings(a, handlersDir, o.WebApp)
	if err != nil {
		return 0, "", err
	}
	var missing []string
	for _, pc := range pieces {
		if pc.finding != "" {
			missing = append(missing, pc.finding)
		}
	}
	missing = append(missing, findings...)

	switch {
	case len(missing) == 0:
		return installedAll, "", nil
	case present == 0:
		return installedNone, fmt.Sprintf("%s: project %s (outlet %s at %s) is not installable: no %s dependency, no worker config, no worker or update provider, no manifest link, and no release stamped (no %s define, no %s provider, no version interceptor)", path.Join(p.WebDir, p.Root), p.Name, o.Name, o.WebApp, serviceWorkerPackage, versionDefine, versionToken), nil
	default:
		return installedPart, missing[0], nil
	}
}

// packageManifest is what the check reads of the workspace's package.json: the
// dependencies and the package scripts.
type packageManifest struct {
	Scripts      map[string]string `json:"scripts"`
	Dependencies map[string]string `json:"dependencies"`
}

// readPackageJSON reads the workspace's package.json; a workspace without one reads as
// empty, and every piece that needs it reports the file.
func readPackageJSON(a *app.App, p *boundProject) (packageManifest, error) {
	packageJSON := path.Join(p.WebDir, "package.json")
	data, err := os.ReadFile(a.Abs(packageJSON))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return packageManifest{}, errors.Wrap(err, "os.ReadFile()")
	}
	var manifest packageManifest
	if len(data) > 0 {
		if err := json.Unmarshal(data, &manifest); err != nil {
			return packageManifest{}, errors.Wrapf(err, "json.Unmarshal(): %s", packageJSON)
		}
	}

	return manifest, nil
}

// dependencyPiece reads the workspace's package.json for the service worker package at
// the Angular line, the spec of @angular/core.
func dependencyPiece(p *boundProject, manifest packageManifest) piece {
	packageJSON := path.Join(p.WebDir, "package.json")
	core, worker := manifest.Dependencies[angularCore], manifest.Dependencies[serviceWorkerPackage]
	switch {
	case worker == "":
		return piece{finding: fmt.Sprintf("%s: %s is not a dependency; add it at the workspace's Angular line (%s, the line of %s)", packageJSON, serviceWorkerPackage, core, angularCore)}
	case worker != core:
		return piece{present: true, finding: fmt.Sprintf("%s: %s is %s, not the workspace's Angular line %s (%s)", packageJSON, serviceWorkerPackage, worker, core, angularCore)}
	default:
		return piece{present: true}
	}
}

// definePiece reads the project's build for the define of the release identifier: the
// build options (or the production configuration) define APP_VERSION, as 'dev', and the
// build script redefines it for a release build. Without the define the identifier is
// undefined at run time, and the app config cannot provide it.
func definePiece(p *boundProject) piece {
	angularJSON := path.Join(p.WebDir, "angular.json")
	if _, ok := p.Defines[versionDefine]; ok {
		return piece{present: true}
	}

	return piece{finding: fmt.Sprintf("%s: project %s's build defines no %s; add \"define\": { %q: \"'dev'\" } to its build options, which the workspace's build script redefines from %s for a release build", angularJSON, p.Name, versionDefine, versionDefine, versionVariable)}
}

// scriptPiece reads the workspace's build script for the project's segment (ng build
// <project>) redefining the release identifier from the VERSION environment variable, so
// the image's browser stage, which sets it, stamps the release into the bundle and every
// other build stamps dev. A segment that defines the identifier from anything else counts
// as present and is reported.
func scriptPiece(p *boundProject, manifest packageManifest) piece {
	packageJSON := path.Join(p.WebDir, "package.json")
	segment, ok := buildSegment(manifest.Scripts["build"], p.Name)
	switch {
	case manifest.Scripts["build"] == "":
		return piece{finding: fmt.Sprintf("%s: no build script; add \"build\": \"ng build %s %s\" so a release build stamps its release and any other dev", packageJSON, p.Name, versionFeed)}
	case !ok:
		return piece{finding: fmt.Sprintf("%s: the build script has no ng build %s segment; add one with %s so a release build stamps its release and any other dev", packageJSON, p.Name, versionFeed)}
	case !strings.Contains(segment, "--define") || !strings.Contains(segment, versionDefine):
		return piece{finding: fmt.Sprintf("%s: the build script's ng build %s passes no --define of %s, so every build stamps dev; add %s", packageJSON, p.Name, versionDefine, versionFeed)}
	case !versionVariableRE.MatchString(segment):
		return piece{present: true, finding: fmt.Sprintf("%s: the build script's ng build %s defines %s from something other than the %s environment variable, which the image's browser stage sets; pass %s", packageJSON, p.Name, versionDefine, versionVariable, versionFeed)}
	default:
		return piece{present: true}
	}
}

// versionVariableRE matches a shell reference to the VERSION environment variable.
var versionVariableRE = regexp.MustCompile(`\$\{?` + versionVariable + `\b`)

// buildSegment finds the project's segment of a workspace build script: the && separated
// command that runs ng build <project>.
func buildSegment(script, project string) (string, bool) {
	segmentRE := regexp.MustCompile(`\bng build ` + regexp.QuoteMeta(project) + `(\s|$)`)
	for _, segment := range strings.Split(script, "&&") {
		segment = strings.TrimSpace(segment)
		if segmentRE.MatchString(segment) {
			return segment, true
		}
	}

	return "", false
}

// workerConfigPiece reads the project's production configuration for the worker config it
// names, and returns the config's root-relative path when the file is there.
func workerConfigPiece(a *app.App, p *boundProject) (pc piece, configRel string) {
	angularJSON := path.Join(p.WebDir, "angular.json")
	switch rel := path.Join(p.WebDir, p.ServiceWorker); {
	case p.ServiceWorker == "":
		return piece{finding: fmt.Sprintf("%s: project %s's production configuration names no serviceWorker config; add \"serviceWorker\": \"ngsw-config.json\" and the file at the workspace root", angularJSON, p.Name)}, ""
	case !exists(a.Abs(rel)):
		return piece{present: true, finding: fmt.Sprintf("%s: project %s's production configuration names %s, which does not exist", angularJSON, p.Name, rel)}, ""
	default:
		return piece{present: true}, rel
	}
}

// apiExclusionFinding reads the worker config's navigation patterns for the exclusion of
// the outlet's API prefix relative to its mount: an outlet whose prefix sits under the
// mount (console/api under /console, api under /) is inside the worker's scope, and
// without the exclusion the worker answers the login, callback and stored-file
// navigations from the cached entry document. A prefix outside the mount is outside the
// scope and needs none.
func apiExclusionFinding(a *app.App, configRel string, o *outletSurface) (string, error) {
	rel, under := apiUnderMount(o.Prefix, o.WebApp)
	if !under {
		return "", nil
	}
	data, err := os.ReadFile(a.Abs(configRel))
	if err != nil {
		return "", errors.Wrap(err, "os.ReadFile()")
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Sprintf("%s: %v", configRel, err), nil
	}
	var patterns []string
	if raw, ok := config["navigationUrls"]; ok {
		if err := json.Unmarshal(raw, &patterns); err != nil {
			return fmt.Sprintf("%s: navigationUrls: %v", configRel, err), nil
		}
	}
	for _, pattern := range patterns {
		excluded, ok := strings.CutPrefix(pattern, "!")
		if !ok {
			continue
		}
		if excluded == "/"+rel || strings.HasPrefix(excluded, "/"+rel+"/") {
			return "", nil
		}
	}

	return fmt.Sprintf("%s: navigationUrls excludes nothing under /%s, the %s outlet's API under its mount %s, so the worker would answer the login, callback and stored-file navigations from the cached entry document; add \"!/%s/**\"", configRel, rel, o.Name, o.WebApp, rel), nil
}

// apiUnderMount returns the outlet's API prefix relative to its mount path, and whether
// the prefix sits under the mount at all: api under / is api; console/api under /console
// is api; api under /console is outside.
func apiUnderMount(prefix, mount string) (string, bool) {
	base := strings.Trim(mount, "/")
	if base == "" {
		return prefix, true
	}
	rel, under := strings.CutPrefix(prefix, base+"/")
	if !under {
		return "", false
	}

	return rel, true
}

// providerPieces reads the hand-written TypeScript under the project's source root for
// the provider calls: the worker's and the update service's as one piece, and the
// release's, the API_VERSION provider and the version interceptor, as another; specs and
// generated files are not read.
func providerPieces(a *app.App, p *boundProject) (providers, version piece, err error) {
	source := path.Join(p.WebDir, p.SourceRoot)
	calls, err := providerCalls(a.Abs(source))
	if err != nil {
		return piece{}, piece{}, err
	}
	appConfig := path.Join(source, "app", "app.config.ts")
	const workerCall = "provideServiceWorker('ngsw-worker.js', { enabled: !isDevMode(), registrationStrategy: 'registerWhenStable:30000' })"
	const updateCall = "provideAppUpdate() from @cccteam/resource-angular/ui-app-update"
	switch {
	case !calls.worker && !calls.update:
		providers = piece{finding: fmt.Sprintf("%s: nothing under %s provides the service worker (%s) or the update provider (%s)", appConfig, source, workerCall, updateCall)}
	case !calls.worker:
		providers = piece{present: true, finding: fmt.Sprintf("%s: nothing under %s provides the service worker (%s)", appConfig, source, workerCall)}
	case !calls.update:
		providers = piece{present: true, finding: fmt.Sprintf("%s: nothing under %s provides the update provider (%s), so a new build is never announced and a refused build never picked up", appConfig, source, updateCall)}
	default:
		providers = piece{present: true}
	}
	const versionProvision = "{ provide: API_VERSION, useValue: APP_VERSION }, the token from @cccteam/resource-angular/types"
	const interceptorCall = "provideHttpClient(withInterceptors([apiVersionInterceptor])), from @cccteam/resource-angular/resource-client"
	switch {
	case !calls.version && !calls.interceptor:
		version = piece{finding: fmt.Sprintf("%s: nothing under %s provides %s (%s) or registers the version interceptor (%s), so no request carries the release and the server never refuses a build it stopped answering", appConfig, source, versionToken, versionProvision, interceptorCall)}
	case !calls.version:
		version = piece{present: true, finding: fmt.Sprintf("%s: nothing under %s provides %s (%s), so the client sends no release and the server never refuses a build it stopped answering", appConfig, source, versionToken, versionProvision)}
	case !calls.interceptor:
		version = piece{present: true, finding: fmt.Sprintf("%s: nothing under %s registers the version interceptor (%s), so the application's own HttpClient calls carry no release", appConfig, source, interceptorCall)}
	default:
		version = piece{present: true}
	}

	return providers, version, nil
}

// configCalls is what the hand-written TypeScript under a source root calls of the
// shape: the worker and update providers, the API_VERSION provision, and the version
// interceptor's registration.
type configCalls struct {
	worker, update, version, interceptor bool
}

// all reports whether every call was seen, so the walk can stop.
func (c configCalls) all() bool {
	return c.worker && c.update && c.version && c.interceptor
}

// versionProvisionRE matches the provision of the API_VERSION token.
var versionProvisionRE = regexp.MustCompile(`\bprovide:\s*` + versionToken + `\b`)

// providerCalls reports which of the shape's calls the hand-written TypeScript under a
// source root makes. The tree is opened as a root, so the walk stays inside it; a source
// root that is not there makes none.
func providerCalls(abs string) (configCalls, error) {
	var calls configCalls
	root, err := os.OpenRoot(abs)
	if errors.Is(err, os.ErrNotExist) {
		return calls, nil
	}
	if err != nil {
		return calls, errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()

	fsys := root.FS()
	err = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrap(err, "fs.WalkDir()")
		}
		if d.IsDir() {
			if p != "." && skippedDirs[d.Name()] {
				return fs.SkipDir
			}

			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".spec.ts") || strings.HasPrefix(name, "zz_gen_") {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return errors.Wrap(err, "fs.ReadFile()")
		}
		text := string(data)
		calls.worker = calls.worker || strings.Contains(text, "provideServiceWorker(")
		calls.update = calls.update || strings.Contains(text, "provideAppUpdate(")
		calls.version = calls.version || versionProvisionRE.MatchString(text)
		calls.interceptor = calls.interceptor || (strings.Contains(text, "withInterceptors(") && strings.Contains(text, versionInterceptor))
		if calls.all() {
			return fs.SkipAll
		}

		return nil
	})
	if err != nil {
		return calls, errors.Wrap(err, "fs.WalkDir()")
	}

	return calls, nil
}

// linkTagRE matches a link element; relAttrRE and hrefAttrRE read its rel and href.
var (
	linkTagRE  = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	relAttrRE  = regexp.MustCompile(`(?i)\brel\s*=\s*["']([^"']*)["']`)
	hrefAttrRE = regexp.MustCompile(`(?i)\bhref\s*=\s*["']([^"']*)["']`)
)

// manifestLinkPiece reads the project's entry document for the manifest link and returns
// the href it names.
func manifestLinkPiece(a *app.App, p *boundProject) (piece, string, error) {
	indexRel := p.Index
	if indexRel == "" {
		indexRel = path.Join(p.SourceRoot, "index.html")
	}
	indexRel = path.Join(p.WebDir, indexRel)
	data, err := os.ReadFile(a.Abs(indexRel))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return piece{}, "", errors.Wrap(err, "os.ReadFile()")
	}
	for _, tag := range linkTagRE.FindAllString(string(data), -1) {
		rel := relAttrRE.FindStringSubmatch(tag)
		if len(rel) < 2 || !strings.EqualFold(strings.TrimSpace(rel[1]), "manifest") {
			continue
		}
		if href := hrefAttrRE.FindStringSubmatch(tag); len(href) >= 2 && href[1] != "" {
			return piece{present: true}, href[1], nil
		}
	}

	return piece{finding: fmt.Sprintf("%s: no <link rel=\"manifest\"> names the web app manifest; add <link rel=\"manifest\" href=%q />", indexRel, manifestFile)}, "", nil
}

// manifestIcon is one icon a manifest declares.
type manifestIcon struct {
	Src   string `json:"src"`
	Sizes string `json:"sizes"`
}

// manifestFinding reads the manifest the entry document links, served from the
// project's public directory, for the identity of the installed application and its
// icons: id is the mount path with a trailing slash, scope and start_url are ./ (the
// mount, relative to the manifest), and every icon is there at the size it declares.
func manifestFinding(a *app.App, p *boundProject, href string, o *outletSurface) (string, error) {
	rel, ok := publicFile(a, p, href)
	if !ok {
		return fmt.Sprintf("%s links %s, which is not under the project's public directory (%s)", path.Join(p.WebDir, p.SourceRoot, "index.html"), href, strings.Join(publicDirs(p), ", ")), nil
	}
	data, err := os.ReadFile(a.Abs(rel))
	if err != nil {
		return "", errors.Wrap(err, "os.ReadFile()")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Sprintf("%s: %v", rel, err), nil
	}
	text := func(key string) string {
		var s string
		_ = json.Unmarshal(fields[key], &s)

		return s
	}
	var icons []manifestIcon
	if raw, ok := fields["icons"]; ok {
		if err := json.Unmarshal(raw, &icons); err != nil {
			return fmt.Sprintf("%s: icons: %v", rel, err), nil
		}
	}
	wantID := strings.TrimSuffix(o.WebApp, "/") + "/"
	switch id, scope, start := text("id"), text("scope"), text("start_url"); {
	case id != wantID:
		return fmt.Sprintf("%s: id is %q, want %q, the mount path with a trailing slash, which identifies the installed application", rel, id, wantID), nil
	case scope != "./":
		return fmt.Sprintf("%s: scope is %q, want \"./\", the mount relative to the manifest", rel, scope), nil
	case start != "./":
		return fmt.Sprintf("%s: start_url is %q, want \"./\", the mount relative to the manifest", rel, start), nil
	case len(icons) == 0:
		return fmt.Sprintf("%s: declares no icons; the install needs icons/icon-192.png and icons/icon-512.png (purpose any) and icons/icon-512-maskable.png (purpose maskable)", rel), nil
	}
	for _, icon := range icons {
		if finding := iconFinding(a, rel, icon); finding != "" {
			return finding, nil
		}
	}

	return "", nil
}

// iconFinding reads one declared icon: it is there, relative to the manifest, as a PNG
// whose dimensions are the sizes it declares.
func iconFinding(a *app.App, manifestRel string, icon manifestIcon) string {
	iconRel := path.Join(path.Dir(manifestRel), icon.Src)
	wantW, wantH, ok := parseSizes(icon.Sizes)
	if !ok {
		return fmt.Sprintf("%s: icon %s declares sizes %q; one PNG has one size, <width>x<height>", manifestRel, icon.Src, icon.Sizes)
	}
	if !exists(a.Abs(iconRel)) {
		return fmt.Sprintf("%s: icon %s is not there (%s)", manifestRel, icon.Src, iconRel)
	}
	w, h, err := pngDimensions(a.Abs(iconRel))
	switch {
	case err != nil:
		return fmt.Sprintf("%s: icon %s: %v", manifestRel, icon.Src, err)
	case w != wantW || h != wantH:
		return fmt.Sprintf("%s: icon %s is %dx%d, not the %s its sizes declares", manifestRel, icon.Src, w, h, icon.Sizes)
	default:
		return ""
	}
}

// publicDirs lists the directories the build copies into the bundle root as the
// workspace-relative inputs of the build's assets option, falling back to the project's
// public directory.
func publicDirs(p *boundProject) []string {
	dirs := make([]string, 0, len(p.AssetDirs)+1)
	for _, d := range p.AssetDirs {
		dirs = append(dirs, path.Join(p.WebDir, d))
	}
	if len(dirs) == 0 {
		dirs = append(dirs, path.Join(p.WebDir, p.Root, "public"))
	}

	return dirs
}

// publicFile finds a bundle-root file (a manifest href) under the project's public
// directories, as a root-relative path.
func publicFile(a *app.App, p *boundProject, href string) (string, bool) {
	for _, dir := range publicDirs(p) {
		rel := path.Join(dir, href)
		if exists(a.Abs(rel)) {
			return rel, true
		}
	}

	return "", false
}

// parseSizes reads one <width>x<height> declaration.
func parseSizes(sizes string) (w, h int, ok bool) {
	wText, hText, found := strings.Cut(strings.TrimSpace(sizes), "x")
	if !found {
		return 0, 0, false
	}
	w, errW := strconv.Atoi(wText)
	h, errH := strconv.Atoi(hText)
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}

	return w, h, true
}

// pngSignature opens every PNG, and pngHeaderChunk is the chunk that follows, which
// carries the dimensions.
var (
	pngSignature   = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	pngHeaderChunk = []byte("IHDR")
)

// pngDimensions reads a PNG's width and height from its header: the eight-byte signature,
// the IHDR chunk's length and type, then the two big-endian dimensions.
func pngDimensions(abs string) (w, h int, err error) {
	f, err := os.Open(abs)
	if err != nil {
		return 0, 0, errors.Wrap(err, "os.Open()")
	}
	defer f.Close()

	header := make([]byte, 24)
	if _, err := f.Read(header); err != nil {
		return 0, 0, errors.Wrap(err, "os.File.Read()")
	}
	if !bytes.Equal(header[:8], pngSignature) || !bytes.Equal(header[12:16], pngHeaderChunk) {
		return 0, 0, errors.New("not a PNG")
	}

	return int(binary.BigEndian.Uint32(header[16:20])), int(binary.BigEndian.Uint32(header[20:24])), nil
}

// handlerFindings reads the application's hand-written handlers for the server side of
// the shape: a construction of the served browser app with the outlet's mount path
// (resource.NewBrowserApp(dir, "/console")), no import of the module it replaces, and
// no requirement of that module in go.mod. An application with no handlers directory is
// not read here; the options check reports it.
func handlerFindings(a *app.App, handlersDir, mount string) ([]string, error) {
	var findings []string
	if a.GoMod != nil {
		for _, req := range a.GoMod.Require {
			if req.Mod.Path == spaassetsModule {
				findings = append(findings, fmt.Sprintf("go.mod: requires %s, which the resource package's served browser app replaces; remove the import and run go mod tidy", spaassetsModule))

				break
			}
		}
	}
	if handlersDir == "" {
		return findings, nil
	}
	entries, err := os.ReadDir(a.Abs(handlersDir))
	if errors.Is(err, os.ErrNotExist) {
		return findings, nil // the options check reports the missing directory
	}
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadDir()")
	}
	mounted := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "zz_gen_") {
			continue
		}
		rel := path.Join(handlersDir, name)
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, errors.Wrap(err, "parser.ParseFile()")
		}
		for _, imp := range f.Imports {
			if strings.Trim(imp.Path.Value, `"`) == spaassetsModule {
				findings = append(findings, fmt.Sprintf("%s:%d: imports %s, which the resource package's served browser app replaces: build the asset handlers from resource.NewBrowserApp(dir, %q) and delegate DeepLink and Assets to it", rel, fset.Position(imp.Pos()).Line, spaassetsModule, mount))
			}
		}
		if browserAppMounts(f)[mount] {
			mounted = true
		}
	}
	if !mounted {
		findings = append(findings, fmt.Sprintf("%s: no hand-written file builds the application's asset handlers from the resource package's served browser app (resource.NewBrowserApp(dir, %q)) so that DeepLink and Assets delegate to it; without it the bundle is served with cache headers the service worker cannot rely on", handlersDir, mount))
	}

	return findings, nil
}

// browserAppMounts lists the mount paths a file builds served browser apps with: the
// second argument of every NewBrowserApp call that is a string literal.
func browserAppMounts(f *ast.File) map[string]bool {
	mounts := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != browserAppConstructor {
			return true
		}
		if lit, ok := call.Args[1].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if mount, err := strconv.Unquote(lit.Value); err == nil {
				mounts[mount] = true
			}
		}

		return true
	})

	return mounts
}
