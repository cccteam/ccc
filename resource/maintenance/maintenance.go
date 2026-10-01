// Package maintenance is what an application serves while it is down for maintenance: a
// revision the deploy pipeline starts with the maintenance variable set, before a release
// that replaces or interrupts the database, so that the people using the application see
// a maintenance page instead of errors while the database is away. The framework owns it;
// no application writes it. The skeletons' mains, and every application's, call Requested
// before the site configuration is built: when the variable is set, the process serves the
// maintenance handler on PORT and returns, so no database client, no session store and no
// secret is opened. The handler answers every request 503 Service Unavailable with a
// Retry-After header and the marker header, X-Maintenance: 1, by which the browser
// library tells maintenance from an ordinary 503: a navigation gets the embedded page,
// which checks back on its own and reloads when the application answers again; anything
// else gets no body. The pipeline trusts none of this at deploy time: it probes the
// revision for the 503 and the marker before any traffic moves.
package maintenance

import (
	"context"
	_ "embed"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

// Variable is the environment variable whose presence (any value but the empty string)
// puts the process in maintenance mode: the deploy pipeline sets it on a maintenance
// revision, and the application's stack declares it empty on the service.
const Variable = "APP_MAINTENANCE"

// Header is the marker every maintenance answer carries, with HeaderValue, by which a
// 503 from maintenance is told from an ordinary 503 (an overloaded or failing revision).
const (
	Header      = "X-Maintenance"
	HeaderValue = "1"
)

// The browser's fetch metadata that names a navigation: a request for a document.
const (
	fetchDest     = "Sec-Fetch-Dest"
	fetchMode     = "Sec-Fetch-Mode"
	destDocument  = "document"
	modeNavigate  = "navigate"
	acceptHeader  = "Accept"
	acceptingHTML = "text/html"
)

// retryAfter is how long an answer tells its caller to wait before trying again; checkBack
// is how often the page itself checks, in seconds, which the page reads from its markup.
const (
	retryAfter   = 30 * time.Second
	portVariable = "PORT"
	defaultPort  = "8080"
	// shutdownGrace is how long Serve lets the requests in flight finish once it is told
	// to stop, the way the application's own server does.
	shutdownGrace = 10 * time.Second
	// readHeaderTimeout bounds a slow client, as any server of ours does.
	readHeaderTimeout = 10 * time.Second
)

//go:embed page.html
var page []byte

// Requested reports whether the process was started in maintenance mode: the variable
// is set to anything but the empty string.
func Requested() bool {
	return os.Getenv(Variable) != ""
}

// Serve serves the maintenance handler on PORT (8080 when unset) until the context ends,
// then lets the requests in flight finish and returns nil. It is what a main calls in place
// of building the site configuration when Requested reports maintenance.
func Serve(ctx context.Context) error {
	port := os.Getenv(portVariable)
	if port == "" {
		port = defaultPort
	}
	var config net.ListenConfig
	listener, err := config.Listen(ctx, "tcp", ":"+port)
	if err != nil {
		return errors.Wrapf(err, "net.Listen(): the maintenance server on port %s", port)
	}

	return serve(ctx, listener)
}

// serve serves the handler on the listener until the context ends.
func serve(ctx context.Context, listener net.Listener) error {
	server := &http.Server{Handler: Handler(), ReadHeaderTimeout: readHeaderTimeout}
	errs := make(chan error, 1)
	go func() {
		errs <- server.Serve(listener)
	}()
	select {
	case err := <-errs:
		return errors.Wrap(err, "http.Server.Serve()")
	case <-ctx.Done():
	}
	grace, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := server.Shutdown(grace); err != nil {
		return errors.Wrap(err, "http.Server.Shutdown()")
	}

	return nil
}

// Handler answers every request 503 Service Unavailable with Retry-After and the marker: a
// navigation (a request for a document, as the browser's Sec-Fetch headers or its Accept
// header say) gets the maintenance page; anything else gets no body. Nothing is cached.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set(Header, HeaderValue)
		h.Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		h.Set("Cache-Control", "no-store")
		if !navigation(r) {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Content-Length", strconv.Itoa(len(page)))
		w.WriteHeader(http.StatusServiceUnavailable)
		if r.Method != http.MethodHead {
			_, _ = w.Write(page)
		}
	})
}

// navigation reports whether the request asks for a document: a GET or HEAD whose
// Sec-Fetch-Dest is document, whose Sec-Fetch-Mode is navigate, or, from a client without
// those headers, whose Accept names text/html.
func navigation(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	switch {
	case r.Header.Get(fetchDest) == destDocument, r.Header.Get(fetchMode) == modeNavigate:
		return true
	case r.Header.Get(fetchDest) != "" || r.Header.Get(fetchMode) != "":
		return false
	}

	return strings.Contains(r.Header.Get(acceptHeader), acceptingHTML)
}
