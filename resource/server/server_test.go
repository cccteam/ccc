package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// freeAddr is a loopback address nothing listens on.
func freeAddr(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() = %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	return addr
}

// listening reports whether something answers on addr within five seconds.
func listening(addr string) bool {
	dialer := &net.Dialer{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := dialer.DialContext(context.Background(), "tcp", addr)
		if err == nil {
			_ = conn.Close()

			return true
		}
		time.Sleep(20 * time.Millisecond)
	}

	return false
}

// started runs a server on a free address with handler until the test ends, and
// returns its address.
func started(t *testing.T, handler http.Handler) (addr string) {
	t.Helper()
	addr = freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_ = New(addr).Start(ctx, handler)
	}()
	if !listening(addr) {
		t.Fatalf("nothing listens on %s after five seconds", addr)
	}

	return addr
}

// h2cClient speaks unencrypted HTTP/2 with prior knowledge and nothing else.
func h2cClient() *http.Client {
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)

	return &http.Client{Transport: &http.Transport{Protocols: protocols}}
}

// echo answers the request's protocol and its body's length.
func echo() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}
		w.Header().Set("X-Proto", r.Proto)
		w.Header().Set("X-Len", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
	})
}

// TestProtocols holds the server to answering HTTP/1.1 and h2c on one listener, each
// with a body larger than the 32 MiB an HTTP/1 request may carry through Cloud Run.
func TestProtocols(t *testing.T) {
	t.Parallel()

	addr := started(t, echo())
	const body = 33 << 20

	tests := []struct {
		name      string
		client    *http.Client
		wantProto string
	}{
		{
			name:      "HTTP/1.1 is served as before",
			client:    &http.Client{Transport: &http.Transport{}},
			wantProto: "HTTP/1.1",
		},
		{
			name:      "h2c with prior knowledge is served on the same listener",
			client:    h2cClient(),
			wantProto: "HTTP/2.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+addr+"/", strings.NewReader(strings.Repeat("b", body)))
			if err != nil {
				t.Fatalf("http.NewRequestWithContext() = %v", err)
			}
			resp, err := tt.client.Do(req)
			if err != nil {
				t.Fatalf("Do() = %v", err)
			}
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Errorf("Close() = %v", err)
				}
			}()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200", resp.StatusCode)
			}
			if got := resp.Header.Get("X-Proto"); got != tt.wantProto {
				t.Errorf("request protocol = %q, want %q", got, tt.wantProto)
			}
			if got := resp.Header.Get("X-Len"); got != strconv.Itoa(body) {
				t.Errorf("body read = %s bytes, want %d", got, body)
			}
		})
	}
}

// TestStart holds Start to ending with the context, returning nil after a graceful
// shutdown, and to returning the listener's error when the address is taken.
func TestStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		run     func(t *testing.T) error
		wantErr string
	}{
		{
			name: "the context's end shuts the server down and Start returns nil",
			run: func(t *testing.T) error {
				t.Helper()
				addr := freeAddr(t)
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan error, 1)
				go func() {
					done <- New(addr).Start(ctx, http.NotFoundHandler())
				}()
				if !listening(addr) {
					t.Fatalf("nothing listens on %s after five seconds", addr)
				}
				cancel()
				select {
				case err := <-done:
					return err
				case <-time.After(10 * time.Second):
					t.Fatal("Start did not return within ten seconds of the context's end")
				}

				return nil
			},
		},
		{
			name: "an address in use is the listener's error",
			run: func(t *testing.T) error {
				t.Helper()
				var lc net.ListenConfig
				l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatalf("Listen() = %v", err)
				}
				t.Cleanup(func() { _ = l.Close() })
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				return New(l.Addr().String()).Start(ctx, http.NotFoundHandler())
			},
			wantErr: "address already in use",
		},
		{
			name: "Addr is the address given",
			run: func(t *testing.T) error {
				t.Helper()
				if got := New("127.0.0.1:8080").Addr(); got != "127.0.0.1:8080" {
					t.Errorf("Addr() = %q, want 127.0.0.1:8080", got)
				}

				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.run(t)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Start() = %v, want nil", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Start() = %v, want an error naming %q", err, tt.wantErr)
			}
		})
	}
}
