// Package server starts the HTTP server an application runs behind, speaking HTTP/1.1
// and unencrypted HTTP/2 (h2c) on one listener.
//
// An application's deployment ends every TLS connection at its load balancer and talks
// to the application in the clear, so the application's own server never sees TLS; what
// it can speak on that clear connection decides what a request or a response may carry.
// Cloud Run bounds an HTTP/1 request body to 32 MiB, and an HTTP/1 response to 32 MiB
// unless it is chunked, and lifts both bounds when the service's port is named h2c and
// the container speaks HTTP/2 without TLS. This server speaks it beside HTTP/1.1, by
// the standard library's own switch (http.Server.Protocols), so a file larger than that
// travels end to end, and a client that speaks HTTP/1.1 alone is served as before.
//
// The infrastructure reads the application's use of this package as the declaration
// that its image speaks h2c and names the service's port accordingly; an application on
// another server keeps an HTTP/1 port.
package server

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-playground/errors/v5"
)

const (
	// readHeaderTimeout bounds how long a client may take to send a request's headers.
	readHeaderTimeout = 60 * time.Second
	// shutdownTimeout bounds how long the server waits for requests in flight once asked
	// to stop, after which the process ends with them.
	shutdownTimeout = 5 * time.Second
)

// Server is the HTTP server an application runs behind.
type Server struct {
	srv *http.Server
}

// New configures a server listening on addr (host:port, or :port) that speaks HTTP/1.1
// and unencrypted HTTP/2 on it.
func New(addr string) *Server {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	return &Server{
		srv: &http.Server{
			Addr:              addr,
			ReadHeaderTimeout: readHeaderTimeout,
			Protocols:         protocols,
		},
	}
}

// Addr is the address the server listens on.
func (s *Server) Addr() string {
	return s.srv.Addr
}

// Start serves handler until ctx ends or the process is interrupted (SIGINT, SIGTERM),
// then shuts the server down, waiting shutdownTimeout for the requests in flight. It
// returns the listener's error when the server stops on its own, and the shutdown's
// error when the requests in flight outlast the wait.
func (s *Server) Start(ctx context.Context, handler http.Handler) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("Starting Server at http://%s (HTTP/1.1 and h2c)", s.srv.Addr)
	defer log.Print("Server Exited")

	s.srv.Handler = handler
	errChan := make(chan error, 1)
	go func() {
		defer close(errChan)
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- errors.Wrap(err, "http.Server.ListenAndServe()")
		}
	}()

	select {
	case err := <-errChan:
		if err != nil {
			return err
		}
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := s.srv.Shutdown(shutdownCtx); err != nil {
		return errors.Wrap(err, "http.Server.Shutdown()")
	}

	return nil
}
