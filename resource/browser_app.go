package resource

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"
)

// hashedBundleFile matches a file name the build stamped with its content hash: eight
// uppercase letters or digits before the extension, as the Angular build names them
// (main-ZPJWNJT4.js, chunk-3ANMYK5A.js, styles-A4ABYBXD.css, media/font-ABCDEFGH.woff2).
var hashedBundleFile = regexp.MustCompile(`-[A-Z0-9]{8}\.[A-Za-z0-9]+$`)

const (
	// browserAppEntry is the entry document of a built browser application.
	browserAppEntry = "index.html"
	// browserAppImmutable is the cache class of a hashed file: its name changes with its
	// content, so a copy anywhere is good for as long as anything keeps it.
	browserAppImmutable = "public, max-age=31536000, immutable"
	// browserAppRevalidate is the cache class of every other file: a copy may be kept and
	// must be revalidated before each use, and an unchanged file answers 304.
	browserAppRevalidate = "no-cache"
	// webManifestExt and webManifestType are the web app manifest's extension and media
	// type, which Go's type table lacks.
	webManifestExt  = ".webmanifest"
	webManifestType = "application/manifest+json"
)

// BrowserApp serves a built browser application, an Angular bundle, from a directory
// under a mount path: "/" for the application at the root, "/console" for one under a
// path, the value the application's generated router declares with WebApp. An
// application's DeepLink and Assets handlers delegate to it, one line each.
//
// Two cache classes, by file name. A file whose name carries the build hash (eight
// uppercase letters or digits before the extension: main-ZPJWNJT4.js,
// styles-A4ABYBXD.css, media/font-ABCDEFGH.woff2) answers "public, max-age=31536000,
// immutable": the name changes with the content, so the service worker and any cache
// between may keep it for good. Every other file answers "no-cache": the entry document,
// ngsw.json, the worker scripts, the web manifest, the favicon, the icons, the
// prerendered-routes file, and the licenses file may be kept and are revalidated before
// each use; an unchanged file answers 304. Their validator is a strong ETag over the
// content, since the files ride in a container image whose modification times are the
// build's and may repeat, and the standard library's conditional handling answers it.
// A .webmanifest answers application/manifest+json. A directory is never listed: its
// path is an application route through DeepLink and 404 through Assets alone.
type BrowserApp struct {
	files http.FileSystem
	mount string
	entry string
}

// NewBrowserApp returns the server of the bundle built into dir, mounted at mountPath:
// "/" or a path starting with "/" and without a trailing "/", as WebApp takes it. Any
// other mount path is a programming error and panics, naming the rule.
func NewBrowserApp(dir, mountPath string) *BrowserApp {
	if mountPath == "" || !strings.HasPrefix(mountPath, "/") || strings.ContainsAny(mountPath, "{}* \t\n\"") || (mountPath != "/" && strings.HasSuffix(mountPath, "/")) {
		panic(fmt.Sprintf("resource.NewBrowserApp(%q, %q) requires a mount path starting with '/' and without a trailing '/', such as \"/\" or \"/console\"", dir, mountPath))
	}

	return &BrowserApp{
		files: http.Dir(dir),
		mount: mountPath,
		entry: strings.TrimSuffix(mountPath, "/") + "/" + browserAppEntry,
	}
}

// DeepLink rewrites the application's own routes to its entry document, so a bookmarked
// or reloaded route loads the application. A request whose last path segment has no
// extension, Angular matrix parameters (";key=value") removed, is rewritten to
// <mount>/index.html and passed on; a path with an extension passes through unchanged,
// matrix parameters and all, and is a file or a 404.
func (b *BrowserApp) DeepLink(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := r.URL.Path
		if i := strings.IndexByte(requestPath, ';'); i >= 0 {
			requestPath = requestPath[:i]
		}
		if path.Ext(requestPath) == "" {
			r.URL.Path = b.entry
			r.URL.RawPath = ""
		}

		next.ServeHTTP(w, r)
	})
}

// Assets serves the bundle's files under the mount path: the mount prefix removed, the
// file read from the directory with the cache class its name selects, a path outside
// the mount, a missing file, or a directory answered 404. The mount path itself, with
// or without a trailing slash, and <mount>/index.html serve the entry document.
func (b *BrowserApp) Assets() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, ok := b.fileName(r.URL.Path)
		if !ok {
			http.NotFound(w, r)

			return
		}
		f, info, ok := openBundleFile(b.files, name)
		if !ok {
			http.NotFound(w, r)

			return
		}
		defer f.Close()

		header := w.Header()
		if hashedBundleFile.MatchString(name) {
			header.Set("Cache-Control", browserAppImmutable)
		} else {
			tag, err := contentTag(f)
			if err != nil {
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

				return
			}
			header.Set("ETag", tag)
			header.Set("Cache-Control", browserAppRevalidate)
		}
		if strings.HasSuffix(name, webManifestExt) {
			header.Set("Content-Type", webManifestType)
		}

		http.ServeContent(w, r, name, info.ModTime(), f)
	}
}

// fileName maps a request path to the file it names, as a slash path under the
// directory: the mount prefix removed, the result cleaned, and a path naming the mount
// itself mapped to the entry document. A path outside the mount reports false.
func (b *BrowserApp) fileName(requestPath string) (string, bool) {
	rest := requestPath
	if b.mount != "/" {
		var ok bool
		rest, ok = strings.CutPrefix(requestPath, b.mount)
		if !ok || (rest != "" && rest[0] != '/') {
			return "", false
		}
	}
	name := path.Clean("/" + rest)
	if name == "/" {
		name += browserAppEntry
	}

	return name, true
}

// openBundleFile opens the named file and reports false for one that is missing or is a
// directory, which is never listed; the file is open when it reports true.
func openBundleFile(files http.FileSystem, name string) (http.File, fs.FileInfo, bool) {
	f, err := files.Open(name)
	if err != nil {
		return nil, nil, false
	}
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		_ = f.Close()

		return nil, nil, false
	}

	return f, info, true
}

// contentTag returns the strong validator of a revalidated file, the quoted hex SHA-256
// of its content, and leaves the file at its start for serving.
func contentTag(f io.ReadSeeker) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", errors.Wrap(err, "io.Copy()")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", errors.Wrap(err, "io.ReadSeeker.Seek()")
	}

	return `"` + hex.EncodeToString(h.Sum(nil)) + `"`, nil
}
