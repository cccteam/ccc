package resource

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
)

func TestBodyLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		limit        int64
		body         string
		wantRead     string
		wantTooLarge bool
	}{
		{name: "a body under the limit reads whole", limit: 16, body: "twelve bytes", wantRead: "twelve bytes"},
		{name: "a body at the limit reads whole", limit: 12, body: "twelve bytes", wantRead: "twelve bytes"},
		{name: "a body over the limit fails the read naming the limit", limit: 11, body: "twelve bytes", wantTooLarge: true},
		{name: "an empty body is under every limit", limit: 1, body: "", wantRead: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotRead string
			var gotErr error
			handler := BodyLimit(tt.limit)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				read, err := io.ReadAll(r.Body)
				gotRead, gotErr = string(read), err
			}))

			r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/test", strings.NewReader(tt.body))
			handler.ServeHTTP(httptest.NewRecorder(), r)

			var tooLarge *http.MaxBytesError
			if got := errors.As(gotErr, &tooLarge); got != tt.wantTooLarge {
				t.Fatalf("read error = %v, want too large %v", gotErr, tt.wantTooLarge)
			}
			if tt.wantTooLarge {
				if tooLarge.Limit != tt.limit {
					t.Errorf("MaxBytesError.Limit = %d, want %d", tooLarge.Limit, tt.limit)
				}

				return
			}
			if gotRead != tt.wantRead {
				t.Errorf("read = %q, want %q", gotRead, tt.wantRead)
			}
		})
	}
}
