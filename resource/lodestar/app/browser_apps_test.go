package app

import (
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource"
	"github.com/go-chi/chi/v5"
)

// browserAppBundle is the synthetic bundle the served-surface test reads: the file shapes
// a production build of either app emits, a few bytes each, since the Go suites build no
// bundle. Both apps serve it in the test, each under its own mount.
const browserAppBundle = "../testdata/browserapp"

const (
	// cacheImmutable is the cache class of a file whose name carries the build hash.
	cacheImmutable = "public, max-age=31536000, immutable"
	// cacheRevalidate is the cache class of every other file.
	cacheRevalidate = "no-cache"
)

// servedBrowserApps composes the two browser applications the way the generated router
// does (pkg/router/zz_gen_router.go): each under its mount, the deep-link rewrite around
// the assets handler, both over the synthetic bundle.
func servedBrowserApps() http.Handler {
	a := &App{
		consoleApp: resource.NewBrowserApp(browserAppBundle, "/console"),
		portalApp:  resource.NewBrowserApp(browserAppBundle, "/portal"),
	}
	r := chi.NewRouter()
	r.Route("/console", func(r chi.Router) {
		r.Use(a.DeepLink)

		r.Get("/*", a.Assets())
	})
	r.Route("/portal", func(r chi.Router) {
		r.Use(a.PortalDeepLink)

		r.Get("/*", a.PortalAssets())
	})

	return r
}

// TestBrowserAppsServedSurface is the served surface of both installed apps over the
// synthetic bundle: under each mount the worker files, the web manifest, the entry
// document (the mount itself, a deep app route, a route with matrix parameters) and a
// hashed file answer the right status, cache class and content type, and a missing file
// 404. The cache classes are the resource package's; this test pins that both apps'
// handlers hand every request to it, under every mount the router declares.
//
// Demonstrates: webapp.cache-rule.
func TestBrowserAppsServedSurface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string // under the mount
		wantStatus int
		wantCache  string
		wantType   string
		wantFile   string // the bundle file whose bytes the body must be
	}{
		{name: "the worker manifest is revalidated", path: "/ngsw.json", wantStatus: http.StatusOK, wantCache: cacheRevalidate, wantType: "application/json", wantFile: "ngsw.json"},
		{name: "the worker script is revalidated", path: "/ngsw-worker.js", wantStatus: http.StatusOK, wantCache: cacheRevalidate, wantType: "text/javascript; charset=utf-8", wantFile: "ngsw-worker.js"},
		{name: "the web manifest is revalidated with its own media type", path: "/manifest.webmanifest", wantStatus: http.StatusOK, wantCache: cacheRevalidate, wantType: "application/manifest+json", wantFile: "manifest.webmanifest"},
		{name: "the mount with a trailing slash is the entry document", path: "/", wantStatus: http.StatusOK, wantCache: cacheRevalidate, wantType: "text/html; charset=utf-8", wantFile: "index.html"},
		{name: "the mount itself is the entry document", path: "", wantStatus: http.StatusOK, wantCache: cacheRevalidate, wantType: "text/html; charset=utf-8", wantFile: "index.html"},
		{name: "a deep app route is the entry document", path: "/sector/squadrons/0f2a", wantStatus: http.StatusOK, wantCache: cacheRevalidate, wantType: "text/html; charset=utf-8", wantFile: "index.html"},
		{name: "an app route with matrix parameters is the entry document", path: "/sector/squadrons;tab=crew/0f2a", wantStatus: http.StatusOK, wantCache: cacheRevalidate, wantType: "text/html; charset=utf-8", wantFile: "index.html"},
		{name: "a hashed script is immutable", path: "/main-ZPJWNJT4.js", wantStatus: http.StatusOK, wantCache: cacheImmutable, wantType: "text/javascript; charset=utf-8", wantFile: "main-ZPJWNJT4.js"},
		{name: "a hashed stylesheet is immutable", path: "/styles-A4ABYBXD.css", wantStatus: http.StatusOK, wantCache: cacheImmutable, wantType: "text/css; charset=utf-8", wantFile: "styles-A4ABYBXD.css"},
		{name: "an icon is revalidated", path: "/icons/icon-192.png", wantStatus: http.StatusOK, wantCache: cacheRevalidate, wantType: "image/png", wantFile: "icons/icon-192.png"},
		{name: "the favicon is revalidated", path: "/favicon.svg", wantStatus: http.StatusOK, wantCache: cacheRevalidate, wantType: "image/svg+xml", wantFile: "favicon.svg"},
		{name: "a missing file is 404", path: "/missing-ZPJWNJT4.js", wantStatus: http.StatusNotFound},
	}
	for _, mount := range []string{"/console", "/portal"} {
		for _, tt := range tests {
			t.Run(mount+": "+tt.name, func(t *testing.T) {
				t.Parallel()

				rec := httptest.NewRecorder()
				servedBrowserApps().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, mount+tt.path, http.NoBody))

				if rec.Code != tt.wantStatus {
					t.Errorf("GET %s%s status = %d, want %d", mount, tt.path, rec.Code, tt.wantStatus)
				}
				if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
					t.Errorf("GET %s%s Cache-Control = %q, want %q", mount, tt.path, got, tt.wantCache)
				}
				if tt.wantType != "" {
					if got := rec.Header().Get("Content-Type"); got != tt.wantType {
						t.Errorf("GET %s%s Content-Type = %q, want %q", mount, tt.path, got, tt.wantType)
					}
				}
				if tt.wantFile != "" {
					want, err := os.ReadFile(filepath.Join(browserAppBundle, tt.wantFile))
					if err != nil {
						t.Fatalf("os.ReadFile() error = %v", err)
					}
					if got := rec.Body.String(); got != string(want) {
						t.Errorf("GET %s%s body = %q, want the bytes of %s", mount, tt.path, got, tt.wantFile)
					}
				}
			})
		}
	}
}

// webRoot is the web workspace, found from this file so the test runs from any directory.
func webRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return filepath.Join(filepath.Dir(thisFile), "..", "web")
}

// readText reads a committed source file.
func readText(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	return string(data)
}

// stringAt reads a string member of a parsed JSON object; a missing or non-string member is "".
func stringAt(object map[string]any, key string) string {
	value, _ := object[key].(string)

	return value
}

// TestBrowserAppsInstallable pins, in the committed sources, what makes each app
// installable, since the Go jobs build no bundle: the web manifest with the display
// name, the id the mount path with a trailing slash, scope and start_url relative,
// standalone display, the app's own colors and three icons whose files exist with the
// declared PNG dimensions; the entry document linking the manifest, the theme color and
// the touch icon under the app's base href; and the app config providing the service
// worker (off in dev mode, registering when stable or after thirty seconds) and the
// library's update notice beside it. Needs no emulator.
//
// Demonstrates: webapp.installable, webapp.update-notice.
func TestBrowserAppsInstallable(t *testing.T) {
	t.Parallel()

	type icon struct {
		sizes   string
		purpose string
	}
	wantIcons := map[string]icon{
		"icons/icon-192.png":          {sizes: "192x192", purpose: "any"},
		"icons/icon-512.png":          {sizes: "512x512", purpose: "any"},
		"icons/icon-512-maskable.png": {sizes: "512x512", purpose: "maskable"},
	}

	tests := []struct {
		name            string
		project         string
		mount           string
		displayName     string
		themeColor      string
		backgroundColor string
	}{
		{name: "the console", project: "console", mount: "/console/", displayName: "Lodestar", themeColor: "#15161d", backgroundColor: "#f4f5f8"},
		{name: "the portal", project: "portal", mount: "/portal/", displayName: "Lodestar Portal", themeColor: "#1f2331", backgroundColor: "#f7f6f2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			project := filepath.Join(webRoot(t), tt.project)

			var manifest map[string]any
			if err := json.Unmarshal([]byte(readText(t, filepath.Join(project, "public", "manifest.webmanifest"))), &manifest); err != nil {
				t.Fatalf("the manifest does not parse: %v", err)
			}
			for key, want := range map[string]string{
				"name": tt.displayName, "short_name": tt.displayName, "id": tt.mount, "scope": "./", "start_url": "./",
				"display": "standalone", "theme_color": tt.themeColor, "background_color": tt.backgroundColor,
			} {
				if got := stringAt(manifest, key); got != want {
					t.Errorf("manifest %s = %q, want %q", key, got, want)
				}
			}
			icons, _ := manifest["icons"].([]any)
			if len(icons) != len(wantIcons) {
				t.Errorf("manifest declares %d icons, want %d", len(icons), len(wantIcons))
			}
			for _, raw := range icons {
				declared, _ := raw.(map[string]any)
				src := stringAt(declared, "src")
				want, ok := wantIcons[src]
				if !ok {
					t.Errorf("manifest declares an icon %q the design does not name", src)

					continue
				}
				if got := stringAt(declared, "sizes"); got != want.sizes {
					t.Errorf("manifest icon %s sizes = %q, want %q", src, got, want.sizes)
				}
				if got := stringAt(declared, "purpose"); got != want.purpose {
					t.Errorf("manifest icon %s purpose = %q, want %q", src, got, want.purpose)
				}
				if got := stringAt(declared, "type"); got != "image/png" {
					t.Errorf("manifest icon %s type = %q, want image/png", src, got)
				}
				f, err := os.Open(filepath.Join(project, "public", filepath.FromSlash(src)))
				if err != nil {
					t.Errorf("manifest icon %s: %v", src, err)

					continue
				}
				config, err := png.DecodeConfig(f)
				_ = f.Close()
				if err != nil {
					t.Errorf("manifest icon %s is not a PNG: %v", src, err)

					continue
				}
				if got := fmt.Sprintf("%dx%d", config.Width, config.Height); got != want.sizes {
					t.Errorf("manifest icon %s is %s, its declared sizes %s", src, got, want.sizes)
				}
			}

			index := readText(t, filepath.Join(project, "src", "index.html"))
			for _, want := range []string{
				`<base href="` + tt.mount + `" />`,
				`<meta name="theme-color" content="` + tt.themeColor + `" />`,
				`<link rel="manifest" href="manifest.webmanifest" />`,
				`<link rel="apple-touch-icon" href="icons/icon-192.png" />`,
			} {
				if !strings.Contains(index, want) {
					t.Errorf("index.html: missing %q", want)
				}
			}

			config := readText(t, filepath.Join(project, "src", "app", "app.config.ts"))
			for _, want := range []string{
				"import { provideServiceWorker } from '@angular/service-worker';",
				"import { provideAppUpdate } from '@cccteam/resource-angular/ui-app-update';",
				"provideServiceWorker('ngsw-worker.js', { enabled: !isDevMode(), registrationStrategy: 'registerWhenStable:30000' }),",
				"provideAppUpdate(),",
			} {
				if !strings.Contains(config, want) {
					t.Errorf("app.config.ts: missing %q", want)
				}
			}
		})
	}
}

// TestBrowserAppsWorkspace pins the workspace's side of the installed apps: the one worker
// configuration, with the index and the file lists the build joins with each project's
// base href, the API under the mount excluded from navigation, the entry document and the
// web manifest prefetched, and no data groups; both production configurations naming it;
// the service-worker package at the workspace's Angular line; and the application serving
// its bundles through the resource package alone, the former assets module gone from
// go.mod. Needs no emulator.
//
// Demonstrates: webapp.installable.
func TestBrowserAppsWorkspace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		file   string
		want   []string
		absent []string
		counts map[string]int
		check  func(t *testing.T, source string)
	}{
		{
			name: "the worker configuration prefetches the app, excludes the API from navigation and caches no data",
			file: "ngsw-config.json",
			want: []string{
				`"index": "/index.html"`,
				`"navigationUrls": ["/**", "!/**/*.*", "!/**/*__*", "!/**/*__*/**", "!/api/**"]`,
				`"name": "app"`,
				`"installMode": "prefetch"`,
				`"files": ["/favicon.svg", "/index.html", "/manifest.webmanifest", "/*.css", "/*.js"]`,
				`"name": "assets"`,
				`"installMode": "lazy"`,
			},
			absent: []string{"dataGroups"},
		},
		{
			name:   "both production configurations name the worker configuration",
			file:   "angular.json",
			counts: map[string]int{`"serviceWorker": "ngsw-config.json"`: 2, `"baseHref": "/console/"`: 1, `"baseHref": "/portal/"`: 1},
		},
		{
			name: "the service-worker package is at the workspace's Angular line",
			file: "package.json",
			check: func(t *testing.T, source string) {
				t.Helper()

				var pkg struct {
					Dependencies map[string]string `json:"dependencies"`
				}
				if err := json.Unmarshal([]byte(source), &pkg); err != nil {
					t.Fatalf("package.json does not parse: %v", err)
				}
				if got, want := pkg.Dependencies["@angular/service-worker"], pkg.Dependencies["@angular/core"]; got == "" || got != want {
					t.Errorf("@angular/service-worker = %q, want @angular/core's line %q", got, want)
				}
			},
		},
		{
			name:   "the module serves its bundles through the resource package alone",
			file:   filepath.Join("..", "go.mod"),
			absent: []string{"github.com/jtwatson/spaassets"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source := readText(t, filepath.Join(webRoot(t), tt.file))
			for _, want := range tt.want {
				if !strings.Contains(source, want) {
					t.Errorf("%s: missing %q", tt.file, want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(source, absent) {
					t.Errorf("%s: carries %q, want absent", tt.file, absent)
				}
			}
			for want, count := range tt.counts {
				if got := strings.Count(source, want); got != count {
					t.Errorf("%s: %q appears %d times, want %d", tt.file, want, got, count)
				}
			}
			if tt.check != nil {
				tt.check(t, source)
			}
		})
	}
}
