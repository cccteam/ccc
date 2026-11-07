package create_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cccteam/ccc/bedrock/internal/create"
	"github.com/cccteam/ccc/bedrock/internal/github"
)

// TestAwaitBranch: a new repository's branch answers 404 while GitHub writes the initial
// commit; the wait reads it again until it is there, sleeping between the reads, and says
// once that it is waiting.
func TestAwaitBranch(t *testing.T) {
	t.Parallel()

	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/git/ref/heads/master") {
			http.Error(w, `{"message":"unexpected"}`, http.StatusInternalServerError)

			return
		}
		if reads.Add(1) < 3 {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)

			return
		}
		_, _ = w.Write([]byte(`{"ref":"refs/heads/master","object":{"sha":"0123456789abcdef0123456789abcdef01234567","type":"commit"}}`))
	}))
	t.Cleanup(server.Close)
	var slept int
	sleep := func(context.Context, time.Duration) error { slept++; return nil }
	var out bytes.Buffer
	if err := create.AwaitBranch(context.Background(), github.New(server.URL, "test-token"), "imp-example", "imr-root-infrastructure", "master", sleep, &out); err != nil {
		t.Fatalf("AwaitBranch() error = %v", err)
	}
	if reads.Load() != 3 || slept != 2 {
		t.Errorf("reads %d, sleeps %d; want 3 and 2", reads.Load(), slept)
	}
	if want := "Waiting for GitHub to write the initial commit of imp-example/imr-root-infrastructure.\n"; out.String() != want {
		t.Errorf("output %q, want %q", out.String(), want)
	}
}
