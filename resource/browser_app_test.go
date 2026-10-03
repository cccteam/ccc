package resource

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// browserAppBundle is the synthetic bundle the served browser app's tests read: the
// shapes an Angular production build emits, a few bytes each.
const browserAppBundle = "testdata/browserapp"

func TestBrowserApp_Assets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mount      string
		url        string
		wantStatus int
		wantCache  string
		wantType   string
		wantFile   string
	}{
		{name: "a hashed script is immutable", mount: "/", url: "/main-ZPJWNJT4.js", wantStatus: http.StatusOK, wantCache: browserAppImmutable, wantFile: "main-ZPJWNJT4.js"},
		{name: "a hashed chunk is immutable", mount: "/", url: "/chunk-3ANMYK5A.js", wantStatus: http.StatusOK, wantCache: browserAppImmutable, wantFile: "chunk-3ANMYK5A.js"},
		{name: "a hashed stylesheet is immutable", mount: "/", url: "/styles-A4ABYBXD.css", wantStatus: http.StatusOK, wantCache: browserAppImmutable, wantType: "text/css; charset=utf-8", wantFile: "styles-A4ABYBXD.css"},
		{name: "a hashed font under media is immutable", mount: "/", url: "/media/font-ABCDEFGH.woff2", wantStatus: http.StatusOK, wantCache: browserAppImmutable, wantFile: "media/font-ABCDEFGH.woff2"},
		{name: "the root is the entry document, revalidated", mount: "/", url: "/", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantType: "text/html; charset=utf-8", wantFile: "index.html"},
		{name: "the entry document by name, revalidated and not redirected", mount: "/", url: "/index.html", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantType: "text/html; charset=utf-8", wantFile: "index.html"},
		{name: "the worker manifest is revalidated", mount: "/", url: "/ngsw.json", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantType: "application/json", wantFile: "ngsw.json"},
		{name: "the worker script is revalidated", mount: "/", url: "/ngsw-worker.js", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantFile: "ngsw-worker.js"},
		{name: "the web manifest is revalidated with its own media type", mount: "/", url: "/manifest.webmanifest", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantType: webManifestType, wantFile: "manifest.webmanifest"},
		{name: "the favicon is revalidated", mount: "/", url: "/favicon.svg", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantType: "image/svg+xml", wantFile: "favicon.svg"},
		{name: "an icon is revalidated", mount: "/", url: "/icons/icon-192.png", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantType: "image/png", wantFile: "icons/icon-192.png"},
		{name: "a missing file with an extension is 404", mount: "/", url: "/missing-ZPJWNJT4.js", wantStatus: http.StatusNotFound},
		{name: "a directory with a trailing slash is 404, not a listing", mount: "/", url: "/media/", wantStatus: http.StatusNotFound},
		{name: "a directory without a trailing slash is 404, not a listing", mount: "/", url: "/icons", wantStatus: http.StatusNotFound},
		{name: "a path climbing above the directory is confined to it", mount: "/", url: "/../../main-ZPJWNJT4.js", wantStatus: http.StatusOK, wantCache: browserAppImmutable, wantFile: "main-ZPJWNJT4.js"},
		{name: "a hashed script under the mount has the prefix stripped", mount: "/console", url: "/console/main-ZPJWNJT4.js", wantStatus: http.StatusOK, wantCache: browserAppImmutable, wantFile: "main-ZPJWNJT4.js"},
		{name: "the mount with a trailing slash is the entry document", mount: "/console", url: "/console/", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantType: "text/html; charset=utf-8", wantFile: "index.html"},
		{name: "the mount itself is the entry document", mount: "/console", url: "/console", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantFile: "index.html"},
		{name: "the entry document by name under the mount", mount: "/console", url: "/console/index.html", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantFile: "index.html"},
		{name: "the web manifest under the mount", mount: "/console", url: "/console/manifest.webmanifest", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantType: webManifestType, wantFile: "manifest.webmanifest"},
		{name: "an icon under the mount", mount: "/console", url: "/console/icons/icon-192.png", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantType: "image/png", wantFile: "icons/icon-192.png"},
		{name: "a file outside the mount is 404", mount: "/console", url: "/main-ZPJWNJT4.js", wantStatus: http.StatusNotFound},
		{name: "a path sharing the mount's prefix is outside it", mount: "/console", url: "/consoles/main-ZPJWNJT4.js", wantStatus: http.StatusNotFound},
		{name: "a missing file under the mount is 404", mount: "/console", url: "/console/missing.js", wantStatus: http.StatusNotFound},
		{name: "a directory under the mount is 404, not a listing", mount: "/console", url: "/console/media/", wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			NewBrowserApp(browserAppBundle, tt.mount).Assets().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.url, http.NoBody))

			if rec.Code != tt.wantStatus {
				t.Errorf("Assets() status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("Assets() Cache-Control = %q, want %q", got, tt.wantCache)
			}
			if tt.wantType != "" {
				if got := rec.Header().Get("Content-Type"); got != tt.wantType {
					t.Errorf("Assets() Content-Type = %q, want %q", got, tt.wantType)
				}
			}
			if tt.wantFile != "" {
				want, err := os.ReadFile(filepath.Join(browserAppBundle, tt.wantFile))
				if err != nil {
					t.Fatalf("os.ReadFile() error = %v", err)
				}
				if got := rec.Body.String(); got != string(want) {
					t.Errorf("Assets() body = %q, want the bytes of %s", got, tt.wantFile)
				}
			}
			if body := rec.Body.String(); strings.Contains(body, "<a href") || strings.Contains(body, "font-ABCDEFGH") && tt.wantFile == "" {
				t.Errorf("Assets() served a directory listing:\n%s", body)
			}
		})
	}
}

func TestBrowserApp_Assets_conditional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		url        string
		header     string
		fromFirst  string
		value      string
		wantStatus int
	}{
		{name: "the entry document by its validator", url: "/console/index.html", header: "If-None-Match", fromFirst: "ETag", wantStatus: http.StatusNotModified},
		{name: "the entry document by its time", url: "/console/index.html", header: "If-Modified-Since", fromFirst: "Last-Modified", wantStatus: http.StatusNotModified},
		{name: "the worker manifest by its validator", url: "/console/ngsw.json", header: "If-None-Match", fromFirst: "ETag", wantStatus: http.StatusNotModified},
		{name: "the worker script by its validator", url: "/console/ngsw-worker.js", header: "If-None-Match", fromFirst: "ETag", wantStatus: http.StatusNotModified},
		{name: "the web manifest by its validator", url: "/console/manifest.webmanifest", header: "If-None-Match", fromFirst: "ETag", wantStatus: http.StatusNotModified},
		{name: "a validator of other content is answered in full", url: "/console/index.html", header: "If-None-Match", value: `"other"`, wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assets := NewBrowserApp(browserAppBundle, "/console").Assets()
			value := tt.value
			if tt.fromFirst != "" {
				first := httptest.NewRecorder()
				assets.ServeHTTP(first, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.url, http.NoBody))
				if first.Code != http.StatusOK {
					t.Fatalf("Assets() first status = %d, want %d", first.Code, http.StatusOK)
				}
				if value = first.Header().Get(tt.fromFirst); value == "" {
					t.Fatalf("Assets() first response carries no %s", tt.fromFirst)
				}
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.url, http.NoBody)
			req.Header.Set(tt.header, value)
			rec := httptest.NewRecorder()
			assets.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Assets() conditional status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Cache-Control"); got != browserAppRevalidate {
				t.Errorf("Assets() conditional Cache-Control = %q, want %q", got, browserAppRevalidate)
			}
			if rec.Code == http.StatusNotModified && rec.Body.Len() != 0 {
				t.Errorf("Assets() 304 carries a body of %d bytes", rec.Body.Len())
			}
		})
	}
}

func TestBrowserApp_DeepLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mount    string
		url      string
		wantPath string
	}{
		{name: "an application route at the root", mount: "/", url: "/missions", wantPath: "/index.html"},
		{name: "a route with matrix parameters at the root", mount: "/", url: "/missions;id=5", wantPath: "/index.html"},
		{name: "a nested route with matrix parameters", mount: "/", url: "/missions/5;tab=crew/detail", wantPath: "/index.html"},
		{name: "the root itself", mount: "/", url: "/", wantPath: "/index.html"},
		{name: "a directory path at the root is a route", mount: "/", url: "/media/", wantPath: "/index.html"},
		{name: "a file at the root passes unchanged", mount: "/", url: "/main-ZPJWNJT4.js", wantPath: "/main-ZPJWNJT4.js"},
		{name: "a file with matrix parameters passes as it came", mount: "/", url: "/main-ZPJWNJT4.js;v=1", wantPath: "/main-ZPJWNJT4.js;v=1"},
		{name: "an application route under the mount", mount: "/console", url: "/console/missions", wantPath: "/console/index.html"},
		{name: "a route with matrix parameters under the mount", mount: "/console", url: "/console/missions;id=5", wantPath: "/console/index.html"},
		{name: "the mount itself", mount: "/console", url: "/console", wantPath: "/console/index.html"},
		{name: "the mount with a trailing slash", mount: "/console", url: "/console/", wantPath: "/console/index.html"},
		{name: "a route under a segment with a dot", mount: "/console", url: "/console/v1.2/missions", wantPath: "/console/index.html"},
		{name: "a file under the mount passes unchanged", mount: "/console", url: "/console/styles-A4ABYBXD.css", wantPath: "/console/styles-A4ABYBXD.css"},
		{name: "a file in a directory under the mount passes unchanged", mount: "/console", url: "/console/icons/icon-192.png", wantPath: "/console/icons/icon-192.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got string
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = r.URL.Path
			})
			NewBrowserApp(browserAppBundle, tt.mount).DeepLink(next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.url, http.NoBody))

			if got != tt.wantPath {
				t.Errorf("DeepLink() passed on %q, want %q", got, tt.wantPath)
			}
		})
	}
}

func TestBrowserApp_DeepLinkAssets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mount      string
		url        string
		wantStatus int
		wantCache  string
		wantFile   string
	}{
		{name: "a route at the root serves the entry document", mount: "/", url: "/missions;id=5", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantFile: "index.html"},
		{name: "a route under the mount serves the entry document", mount: "/console", url: "/console/missions/5", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantFile: "index.html"},
		{name: "a directory under the mount is the entry document, never a listing", mount: "/console", url: "/console/media/", wantStatus: http.StatusOK, wantCache: browserAppRevalidate, wantFile: "index.html"},
		{name: "a hashed file under the mount is itself", mount: "/console", url: "/console/chunk-3ANMYK5A.js", wantStatus: http.StatusOK, wantCache: browserAppImmutable, wantFile: "chunk-3ANMYK5A.js"},
		{name: "a missing file under the mount is 404", mount: "/console", url: "/console/missing.js", wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := NewBrowserApp(browserAppBundle, tt.mount)
			rec := httptest.NewRecorder()
			app.DeepLink(app.Assets()).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.url, http.NoBody))

			if rec.Code != tt.wantStatus {
				t.Errorf("DeepLink(Assets()) status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("DeepLink(Assets()) Cache-Control = %q, want %q", got, tt.wantCache)
			}
			if tt.wantFile == "" {
				return
			}
			want, err := os.ReadFile(filepath.Join(browserAppBundle, tt.wantFile))
			if err != nil {
				t.Fatalf("os.ReadFile() error = %v", err)
			}
			if got := rec.Body.String(); got != string(want) {
				t.Errorf("DeepLink(Assets()) body = %q, want the bytes of %s", got, tt.wantFile)
			}
		})
	}
}

func TestNewBrowserApp_mountPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mount     string
		wantPanic bool
	}{
		{name: "the root", mount: "/"},
		{name: "a path", mount: "/console"},
		{name: "a nested path", mount: "/apps/console"},
		{name: "empty", mount: "", wantPanic: true},
		{name: "no leading slash", mount: "console", wantPanic: true},
		{name: "a trailing slash", mount: "/console/", wantPanic: true},
		{name: "a pattern", mount: "/console/*", wantPanic: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if r := recover(); (r != nil) != tt.wantPanic {
					t.Errorf("NewBrowserApp(%q) panic = %v, want a panic %t", tt.mount, r, tt.wantPanic)
				}
			}()
			NewBrowserApp(browserAppBundle, tt.mount)
		})
	}
}
