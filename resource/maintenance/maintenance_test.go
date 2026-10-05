package maintenance

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRequested(t *testing.T) {
	tests := []struct {
		name  string
		value string
		set   bool
		want  bool
	}{
		{name: "unset is not maintenance", set: false, want: false},
		{name: "set to the empty string is not maintenance", set: true, value: "", want: false},
		{name: "set to 1 is maintenance", set: true, value: "1", want: true},
		{name: "set to any other word is maintenance", set: true, value: "yes", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(Variable, tt.value)
			if !tt.set {
				if err := os.Unsetenv(Variable); err != nil {
					t.Fatal(err)
				}
			}
			if got := Requested(); got != tt.want {
				t.Errorf("Requested() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		method  string
		headers map[string]string
		// wantPage says the body is the maintenance page; otherwise the body is empty.
		wantPage bool
	}{
		{name: "a browser navigation gets the page", method: http.MethodGet, headers: map[string]string{"Sec-Fetch-Dest": "document", "Sec-Fetch-Mode": "navigate", "Accept": "text/html,*/*"}, wantPage: true},
		{name: "a request for a document by Accept alone gets the page", method: http.MethodGet, headers: map[string]string{"Accept": "text/html"}, wantPage: true},
		{name: "a HEAD navigation gets the page's headers and no body", method: http.MethodHead, headers: map[string]string{"Sec-Fetch-Mode": "navigate"}, wantPage: false},
		{name: "a fetch from a page gets no body", method: http.MethodGet, headers: map[string]string{"Sec-Fetch-Dest": "empty", "Sec-Fetch-Mode": "cors", "Accept": "application/json"}, wantPage: false},
		{name: "a fetch whose Accept names html is still not a navigation when Sec-Fetch says so", method: http.MethodGet, headers: map[string]string{"Sec-Fetch-Dest": "empty", "Accept": "text/html"}, wantPage: false},
		{name: "a POST gets no body", method: http.MethodPost, headers: map[string]string{"Accept": "text/html"}, wantPage: false},
		{name: "a request with no Accept gets no body", method: http.MethodGet, wantPage: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), tt.method, "/some/path?x=1", http.NoBody)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			Handler().ServeHTTP(rec, req)
			res := rec.Result()
			defer res.Body.Close()
			if res.StatusCode != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want %d", res.StatusCode, http.StatusServiceUnavailable)
			}
			for header, want := range map[string]string{Header: HeaderValue, "Retry-After": "30", "Cache-Control": "no-store"} {
				if got := res.Header.Get(header); got != want {
					t.Errorf("%s = %q, want %q", header, got, want)
				}
			}
			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tt.wantPage && !strings.Contains(string(body), "Down for maintenance"):
				t.Errorf("body lacks the page: %q", body)
			case tt.wantPage && res.Header.Get("Content-Type") != "text/html; charset=utf-8":
				t.Errorf("Content-Type = %q", res.Header.Get("Content-Type"))
			case !tt.wantPage && len(body) != 0:
				t.Errorf("body = %q, want none", body)
			}
		})
	}
}

func TestServe(t *testing.T) {
	t.Parallel()

	h2c := new(http.Protocols)
	h2c.SetUnencryptedHTTP2(true)
	tests := []struct {
		name string
		path string
		// client is what the request is made with: HTTP/1.1, or HTTP/2 with prior
		// knowledge as Cloud Run speaks to a service whose port is named h2c.
		client *http.Client
		// wantProto is the protocol the server answered in, and wantStatus what a GET of
		// the path answers while the server runs.
		wantProto  string
		wantStatus int
	}{
		{name: "the server answers 503 with the marker over HTTP/1.1 and stops when the context ends", path: "/", client: http.DefaultClient, wantProto: "HTTP/1.1", wantStatus: http.StatusServiceUnavailable},
		{name: "the server answers the same over unencrypted HTTP/2, as Cloud Run speaks to an h2c port", path: "/", client: &http.Client{Transport: &http.Transport{Protocols: h2c}}, wantProto: "HTTP/2.0", wantStatus: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var config net.ListenConfig
			listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() {
				done <- serve(ctx, listener)
			}()
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+listener.Addr().String()+tt.path, http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			res, err := tt.client.Do(req)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer res.Body.Close()
			if res.StatusCode != tt.wantStatus || res.Header.Get(Header) != HeaderValue {
				t.Errorf("status = %d, %s = %q; want %d and %q", res.StatusCode, Header, res.Header.Get(Header), tt.wantStatus, HeaderValue)
			}
			if res.Proto != tt.wantProto {
				t.Errorf("answered in %s, want %s", res.Proto, tt.wantProto)
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("serve() = %v, want nil after the context ended", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("serve() did not return after the context ended")
			}
		})
	}
}
