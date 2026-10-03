package check

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// pngOf encodes a PNG of the given dimensions, as an icon fixture.
func pngOf(t *testing.T, w, h int) string {
	t.Helper()

	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}

	return b.String()
}

// TestInstallable runs the check over browser projects written for the case: the console
// bound to the default outlet under /console with its API at /console/api, carrying the
// whole shape or missing one piece of it.
func TestInstallable(t *testing.T) {
	t.Parallel()

	const staffAuth = `generation.Auth("example.com/harbor/pkg/auth/staff", generation.Password)`
	consoleProgram := program("pkg/resources",
		`generation.GenerateHandlers("app"),`,
		`generation.GenerateRouter(),`,
		`generation.GenerateRoutes("pkg/router", "console/api", `+staffAuth+`, generation.WebApp("/console")),`,
		`generation.GenerateTypescript("web/console/src/app/core/service", generation.GenerateMetadata()),`,
	)
	// The API prefix beside the mount path, outside the worker's scope: no exclusion needed.
	prefixOutsideProgram := program("pkg/resources",
		`generation.GenerateHandlers("app"),`,
		`generation.GenerateRouter(),`,
		`generation.GenerateRoutes("pkg/router", "api", `+staffAuth+`, generation.WebApp("/console")),`,
		`generation.GenerateTypescript("web/console/src/app/core/service", generation.GenerateMetadata()),`,
	)

	const angularJSON = `{
  "version": 1,
  "projects": {
    "console": {
      "projectType": "application",
      "root": "console",
      "sourceRoot": "console/src",
      "architect": {
        "build": {
          "builder": "@angular/build:application",
          "options": {
            "index": "console/src/index.html",
            "assets": [{ "glob": "**/*", "input": "console/public" }],
            "baseHref": "/console/"
          },
          "configurations": { "production": { "serviceWorker": "ngsw-config.json" } }
        }
      }
    }
  }
}
`
	const angularJSONNoWorker = `{
  "version": 1,
  "projects": {
    "console": {
      "projectType": "application",
      "root": "console",
      "sourceRoot": "console/src",
      "architect": {
        "build": {
          "builder": "@angular/build:application",
          "options": {
            "index": "console/src/index.html",
            "assets": [{ "glob": "**/*", "input": "console/public" }],
            "baseHref": "/console/"
          },
          "configurations": { "production": { "outputHashing": "all" } }
        }
      }
    }
  }
}
`
	const packageJSON = `{ "dependencies": { "@angular/core": "^21.1.1", "@angular/router": "^21.1.1", "@angular/service-worker": "^21.1.1" } }`
	const packageJSONNoWorker = `{ "dependencies": { "@angular/core": "^21.1.1", "@angular/router": "^21.1.1" } }`
	const packageJSONOtherLine = `{ "dependencies": { "@angular/core": "^21.1.1", "@angular/router": "^21.1.1", "@angular/service-worker": "^20.0.0" } }`
	const ngswConfig = `{
  "index": "/index.html",
  "navigationUrls": ["/**", "!/**/*.*", "!/**/*__*", "!/**/*__*/**", "!/api/**"],
  "assetGroups": []
}
`
	const ngswConfigNoExclusion = `{
  "index": "/index.html",
  "navigationUrls": ["/**", "!/**/*.*", "!/**/*__*", "!/**/*__*/**"],
  "assetGroups": []
}
`
	const appConfig = `import { ApplicationConfig, isDevMode } from '@angular/core';
import { provideServiceWorker } from '@angular/service-worker';
import { provideAppUpdate } from '@cccteam/resource-angular/ui-app-update';

export const appConfig: ApplicationConfig = {
  providers: [
    provideServiceWorker('ngsw-worker.js', { enabled: !isDevMode(), registrationStrategy: 'registerWhenStable:30000' }),
    provideAppUpdate(),
  ],
};
`
	const appConfigNoUpdate = `import { ApplicationConfig, isDevMode } from '@angular/core';
import { provideServiceWorker } from '@angular/service-worker';

export const appConfig: ApplicationConfig = {
  providers: [provideServiceWorker('ngsw-worker.js', { enabled: !isDevMode(), registrationStrategy: 'registerWhenStable:30000' })],
};
`
	const appConfigPlain = `import { ApplicationConfig } from '@angular/core';

export const appConfig: ApplicationConfig = { providers: [] };
`
	const indexHTML = `<!doctype html>
<html lang="en">
  <head>
    <title>Harbor</title>
    <base href="/console/" />
    <meta name="theme-color" content="#005cbb" />
    <link rel="icon" type="image/svg+xml" href="favicon.svg" />
    <link rel="manifest" href="manifest.webmanifest" />
    <link rel="apple-touch-icon" href="icons/icon-192.png" />
  </head>
  <body></body>
</html>
`
	const indexHTMLNoManifest = `<!doctype html>
<html lang="en">
  <head>
    <title>Harbor</title>
    <base href="/console/" />
    <link rel="icon" type="image/svg+xml" href="favicon.svg" />
  </head>
  <body></body>
</html>
`
	const manifest = `{
  "name": "Harbor",
  "short_name": "Harbor",
  "id": "/console/",
  "scope": "./",
  "start_url": "./",
  "display": "standalone",
  "icons": [
    { "src": "icons/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any" },
    { "src": "icons/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any" },
    { "src": "icons/icon-512-maskable.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable" }
  ]
}
`
	const manifestRootID = `{
  "name": "Harbor",
  "short_name": "Harbor",
  "id": "/",
  "scope": "./",
  "start_url": "./",
  "icons": [{ "src": "icons/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any" }]
}
`
	const handlers = `package app

import (
	"net/http"

	"github.com/cccteam/ccc/resource"
)

type App struct{ console *resource.BrowserApp }

func New(dist string) *App { return &App{console: resource.NewBrowserApp(dist, "/console")} }

func (a *App) DeepLink(next http.Handler) http.Handler { return a.console.DeepLink(next) }

func (a *App) Assets() http.HandlerFunc { return a.console.Assets() }
`
	const handlersSpaassets = `package app

import (
	"net/http"

	"github.com/jtwatson/spaassets"
)

type App struct{ dist string }

func (a *App) DeepLink(next http.Handler) http.Handler { return spaassets.DeepLink(next, "/console/") }

func (a *App) Assets() http.HandlerFunc {
	return http.StripPrefix("/console", http.FileServer(http.Dir(a.dist))).ServeHTTP
}
`
	const goMod = "module example.com/harbor\n\ngo 1.26.6\n"
	const goModSpaassets = "module example.com/harbor\n\ngo 1.26.6\n\nrequire github.com/jtwatson/spaassets v0.0.0-20160917192555-583a733b0a63\n"

	icon192, icon512, icon180 := pngOf(t, 192, 192), pngOf(t, 512, 512), pngOf(t, 180, 180)
	installed := func() map[string]string {
		return map[string]string{
			"go.mod":                                         goMod,
			"cmd/generate/main.go":                           consoleProgram,
			"app/app.go":                                     handlers,
			"web/angular.json":                               angularJSON,
			"web/package.json":                               packageJSON,
			"web/ngsw-config.json":                           ngswConfig,
			"web/console/src/index.html":                     indexHTML,
			"web/console/src/app/app.config.ts":              appConfig,
			"web/console/public/manifest.webmanifest":        manifest,
			"web/console/public/icons/icon-192.png":          icon192,
			"web/console/public/icons/icon-512.png":          icon512,
			"web/console/public/icons/icon-512-maskable.png": icon512,
		}
	}
	with := func(changes map[string]string) map[string]string {
		files := installed()
		for rel, content := range changes {
			if content == "" {
				delete(files, rel)

				continue
			}
			files[rel] = content
		}

		return files
	}

	const name = "installable"
	tests := []struct {
		name  string
		files map[string]string
		want  Result
	}{
		{
			name:  "an installable project passes",
			files: installed(),
			want:  Result{Name: name, Status: Pass, Summary: "1 browser application(s) install as progressive web apps"},
		},
		{
			name:  "a missing API exclusion fails",
			files: with(map[string]string{"web/ngsw-config.json": ngswConfigNoExclusion}),
			want: Result{Name: name, Status: Fail, Summary: "1 browser application(s) partly installable; the first missing piece of each is named", Details: []string{
				`web/ngsw-config.json: navigationUrls excludes nothing under /api, the default outlet's API under its mount /console, so the worker would answer the login, callback and stored-file navigations from the cached entry document; add "!/api/**"`,
			}},
		},
		{
			name:  "an icon whose size disagrees fails",
			files: with(map[string]string{"web/console/public/icons/icon-192.png": icon180}),
			want: Result{Name: name, Status: Fail, Summary: "1 browser application(s) partly installable; the first missing piece of each is named", Details: []string{
				"web/console/public/manifest.webmanifest: icon icons/icon-192.png is 180x180, not the 192x192 its sizes declares",
			}},
		},
		{
			name: "a project with none of it warns",
			files: with(map[string]string{
				"web/angular.json":                        angularJSONNoWorker,
				"web/package.json":                        packageJSONNoWorker,
				"web/ngsw-config.json":                    "",
				"web/console/src/index.html":              indexHTMLNoManifest,
				"web/console/src/app/app.config.ts":       appConfigPlain,
				"web/console/public/manifest.webmanifest": "",
				"app/app.go":                              handlersSpaassets,
			}),
			want: Result{Name: name, Status: Warn, Summary: "0 of 1 browser application(s) install as progressive web apps; 1 not installable", Details: []string{
				"web/console: project console (outlet default at /console) is not installable: no @angular/service-worker dependency, no worker config, no worker or update provider, and no manifest link",
			}},
		},
		{
			name:  "an API prefix outside the mount needs no exclusion",
			files: with(map[string]string{"cmd/generate/main.go": prefixOutsideProgram, "web/ngsw-config.json": ngswConfigNoExclusion}),
			want:  Result{Name: name, Status: Pass, Summary: "1 browser application(s) install as progressive web apps"},
		},
		{
			name:  "the dependency at another line fails first",
			files: with(map[string]string{"web/package.json": packageJSONOtherLine, "web/ngsw-config.json": ngswConfigNoExclusion}),
			want: Result{Name: name, Status: Fail, Summary: "1 browser application(s) partly installable; the first missing piece of each is named", Details: []string{
				"web/package.json: @angular/service-worker is ^20.0.0, not the workspace's Angular line ^21.1.1 (@angular/core)",
			}},
		},
		{
			name:  "a worker without the update provider fails",
			files: with(map[string]string{"web/console/src/app/app.config.ts": appConfigNoUpdate}),
			want: Result{Name: name, Status: Fail, Summary: "1 browser application(s) partly installable; the first missing piece of each is named", Details: []string{
				"web/console/src/app/app.config.ts: nothing under web/console/src provides the update provider (provideAppUpdate() from @cccteam/resource-angular/ui-app-update), so a new build is never announced and a refused build never picked up",
			}},
		},
		{
			name:  "a manifest identified by the root fails",
			files: with(map[string]string{"web/console/public/manifest.webmanifest": manifestRootID}),
			want: Result{Name: name, Status: Fail, Summary: "1 browser application(s) partly installable; the first missing piece of each is named", Details: []string{
				`web/console/public/manifest.webmanifest: id is "/", want "/console/", the mount path with a trailing slash, which identifies the installed application`,
			}},
		},
		{
			name:  "a missing icon fails",
			files: with(map[string]string{"web/console/public/icons/icon-512-maskable.png": ""}),
			want: Result{Name: name, Status: Fail, Summary: "1 browser application(s) partly installable; the first missing piece of each is named", Details: []string{
				"web/console/public/manifest.webmanifest: icon icons/icon-512-maskable.png is not there (web/console/public/icons/icon-512-maskable.png)",
			}},
		},
		{
			name:  "handlers still on spaassets fail",
			files: with(map[string]string{"app/app.go": handlersSpaassets, "go.mod": goModSpaassets}),
			want: Result{Name: name, Status: Fail, Summary: "1 browser application(s) partly installable; the first missing piece of each is named", Details: []string{
				"go.mod: requires github.com/jtwatson/spaassets, which the resource package's served browser app replaces; remove the import and run go mod tidy",
			}},
		},
		{
			name:  "handlers built for another mount path fail",
			files: with(map[string]string{"app/app.go": strings.ReplaceAll(handlers, `"/console"`, `"/"`)}),
			want: Result{Name: name, Status: Fail, Summary: "1 browser application(s) partly installable; the first missing piece of each is named", Details: []string{
				`app: no hand-written file builds the application's asset handlers from the resource package's served browser app (resource.NewBrowserApp(dir, "/console")) so that DeepLink and Assets delegate to it; without it the bundle is served with cache headers the service worker cannot rely on`,
			}},
		},
		{
			name: "a hand-written router declares no browser application",
			files: map[string]string{
				"go.mod": goMod,
				"cmd/generate/main.go": program("pkg/resources",
					`generation.GenerateHandlers("app"),`,
					`generation.GenerateRoutes("pkg/router", "api"),`,
				),
			},
			want: Result{Name: name, Status: Skip, Summary: "no browser project is bound to a session outlet"},
		},
		{
			name:  "no site generator",
			files: map[string]string{"go.mod": goMod, "cmd/generate/main.go": program("pkg/sharedresources", `generation.GenerateTypescript("web/src"),`)},
			want:  Result{Name: name, Status: Skip, Summary: "no site generator"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			for rel, content := range tt.files {
				abs := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := installable{}.Run(context.Background(), &Env{App: a})
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestAPIUnderMount pins the prefix an exclusion is written for: the API relative to the
// mount when it sits under it, and nothing when it sits beside it.
func TestAPIUnderMount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prefix    string
		mount     string
		wantRel   string
		wantUnder bool
	}{
		{name: "the root application", prefix: "api", mount: "/", wantRel: "api", wantUnder: true},
		{name: "an application under a path", prefix: "console/api", mount: "/console", wantRel: "api", wantUnder: true},
		{name: "a deeper prefix under the mount", prefix: "portal/v2/api", mount: "/portal", wantRel: "v2/api", wantUnder: true},
		{name: "a prefix beside the mount", prefix: "api", mount: "/console", wantUnder: false},
		{name: "a prefix that only starts with the mount's segment", prefix: "consoles/api", mount: "/console", wantUnder: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rel, under := apiUnderMount(tt.prefix, tt.mount)
			if rel != tt.wantRel || under != tt.wantUnder {
				t.Errorf("apiUnderMount(%q, %q) = %q, %v; want %q, %v", tt.prefix, tt.mount, rel, under, tt.wantRel, tt.wantUnder)
			}
		})
	}
}
