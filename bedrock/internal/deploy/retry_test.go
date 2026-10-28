package deploy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/go-playground/errors/v5"
)

// The answers a call's attempts may give in the retrier's tests.
const (
	operationPath = "/v2/projects/p/locations/l/operations/op-1"
	unavailable   = "The service is currently unavailable."
)

// answered is a call's attempt answered with the status and message, as the client
// wraps it.
func answered(status int, message string) error {
	return errors.Wrap(&apiError{status: status, method: http.MethodGet, path: operationPath, message: message}, "cloudRun.call()")
}

// unanswered is a call's attempt that got no answer for the reason, as the HTTP client
// reports one and the client wraps it.
func unanswered(reason error) error {
	return errors.Wrap(&transportError{service: "Cloud Run", method: http.MethodGet, path: operationPath, err: &url.Error{Op: "Get", URL: "https://run.googleapis.com" + operationPath, Err: reason}}, "http.Client.Do()")
}

func TestRetrierDo(t *testing.T) {
	t.Parallel()

	const unavailableLine = "Cloud Run answered HTTP 503 to GET " + operationPath + ": The service is currently unavailable"
	const resetLine = "Cloud Run did not answer GET " + operationPath + ": connection reset by peer"
	canceled := context.Canceled
	tests := []struct {
		name string
		// answers are what each attempt answers, in order: nil is success. A call made
		// beyond them fails the test.
		answers []func() error
		// cancel cancels the context before the first attempt; cutShort makes the wait
		// between attempts fail, as a canceled build's does.
		cancel, cutShort bool
		wantCalls        int
		// wantErr is what the final failure must say (its cause, as the command prints
		// it); empty for success.
		wantErr   string
		wantOut   string
		wantSlept []time.Duration
	}{
		{
			name:      "a call that answers is made once",
			answers:   []func() error{nil},
			wantCalls: 1,
		},
		{
			name:      "a 503 is sent again after the first wait",
			answers:   []func() error{func() error { return answered(http.StatusServiceUnavailable, unavailable) }, nil},
			wantCalls: 2,
			wantOut:   unavailableLine + "; attempt 2 of 5 in 5s.\n",
			wantSlept: []time.Duration{5 * time.Second},
		},
		{
			name: "the waits double, and the fifth failure is given up saying so",
			answers: []func() error{
				func() error { return answered(http.StatusServiceUnavailable, unavailable) },
				func() error { return answered(http.StatusServiceUnavailable, unavailable) },
				func() error { return answered(http.StatusServiceUnavailable, unavailable) },
				func() error { return answered(http.StatusServiceUnavailable, unavailable) },
				func() error { return answered(http.StatusServiceUnavailable, unavailable) },
			},
			wantCalls: 5,
			wantErr:   unavailableLine + "; given up after 5 attempts",
			wantOut: unavailableLine + "; attempt 2 of 5 in 5s.\n" +
				unavailableLine + "; attempt 3 of 5 in 10s.\n" +
				unavailableLine + "; attempt 4 of 5 in 20s.\n" +
				unavailableLine + "; attempt 5 of 5 in 40s.\n",
			wantSlept: []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second},
		},
		{
			name:      "a 500 is sent again",
			answers:   []func() error{func() error { return answered(http.StatusInternalServerError, "Timed out read retrieving data") }, nil},
			wantCalls: 2,
			wantOut:   "Cloud Run answered HTTP 500 to GET " + operationPath + ": Timed out read retrieving data; attempt 2 of 5 in 5s.\n",
			wantSlept: []time.Duration{5 * time.Second},
		},
		{
			name:      "a refusal is the answer",
			answers:   []func() error{func() error { return answered(http.StatusNotFound, "Requested entity was not found.") }},
			wantCalls: 1,
			wantErr:   "Cloud Run answered HTTP 404 to GET " + operationPath + ": Requested entity was not found.",
		},
		{
			name: "a refusal after a transient answer is the answer, with no count",
			answers: []func() error{
				func() error { return answered(http.StatusServiceUnavailable, unavailable) },
				func() error { return answered(http.StatusForbidden, "Permission denied on resource") },
			},
			wantCalls: 2,
			wantErr:   "Cloud Run answered HTTP 403 to GET " + operationPath + ": Permission denied on resource",
			wantOut:   unavailableLine + "; attempt 2 of 5 in 5s.\n",
			wantSlept: []time.Duration{5 * time.Second},
		},
		{
			name:      "a reset connection is sent again",
			answers:   []func() error{func() error { return unanswered(syscall.ECONNRESET) }, nil},
			wantCalls: 2,
			wantOut:   resetLine + "; attempt 2 of 5 in 5s.\n",
			wantSlept: []time.Duration{5 * time.Second},
		},
		{
			name:      "a refused connection is sent again",
			answers:   []func() error{func() error { return unanswered(syscall.ECONNREFUSED) }, nil},
			wantCalls: 2,
			wantOut:   "Cloud Run did not answer GET " + operationPath + ": connection refused; attempt 2 of 5 in 5s.\n",
			wantSlept: []time.Duration{5 * time.Second},
		},
		{
			name:      "a timed-out request is sent again",
			answers:   []func() error{func() error { return unanswered(os.ErrDeadlineExceeded) }, nil},
			wantCalls: 2,
			wantOut:   "Cloud Run did not answer GET " + operationPath + ": i/o timeout; attempt 2 of 5 in 5s.\n",
			wantSlept: []time.Duration{5 * time.Second},
		},
		{
			name:      "a connection closed before the answer is sent again",
			answers:   []func() error{func() error { return unanswered(io.ErrUnexpectedEOF) }, func() error { return unanswered(io.EOF) }, nil},
			wantCalls: 3,
			wantOut: "Cloud Run did not answer GET " + operationPath + ": unexpected EOF; attempt 2 of 5 in 5s.\n" +
				"Cloud Run did not answer GET " + operationPath + ": EOF; attempt 3 of 5 in 10s.\n",
			wantSlept: []time.Duration{5 * time.Second, 10 * time.Second},
		},
		{
			name: "a connection failing five times is given up saying so",
			answers: []func() error{
				func() error { return unanswered(syscall.ECONNRESET) },
				func() error { return unanswered(syscall.ECONNRESET) },
				func() error { return unanswered(syscall.ECONNRESET) },
				func() error { return unanswered(syscall.ECONNRESET) },
				func() error { return unanswered(syscall.ECONNRESET) },
			},
			wantCalls: 5,
			wantErr:   resetLine + "; given up after 5 attempts",
			wantOut: resetLine + "; attempt 2 of 5 in 5s.\n" +
				resetLine + "; attempt 3 of 5 in 10s.\n" +
				resetLine + "; attempt 4 of 5 in 20s.\n" +
				resetLine + "; attempt 5 of 5 in 40s.\n",
			wantSlept: []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second},
		},
		{
			name:      "a request the build canceled is not sent again",
			answers:   []func() error{func() error { return unanswered(canceled) }},
			wantCalls: 1,
			wantErr:   "Cloud Run did not answer GET " + operationPath + ": context canceled",
		},
		{
			name:      "nothing is sent again once the build is canceled",
			answers:   []func() error{func() error { return answered(http.StatusServiceUnavailable, unavailable) }},
			cancel:    true,
			wantCalls: 1,
			wantErr:   unavailableLine + ".",
		},
		{
			name:      "a wait cut short ends the attempts with the wait's error",
			answers:   []func() error{func() error { return answered(http.StatusServiceUnavailable, unavailable) }},
			cutShort:  true,
			wantCalls: 1,
			wantErr:   "context canceled",
			wantOut:   unavailableLine + "; attempt 2 of 5 in 5s.\n",
		},
		{
			name:      "a failure that is not the API's doing is not sent again",
			answers:   []func() error{func() error { return errors.New("json.Unmarshal(): unexpected end of JSON input") }},
			wantCalls: 1,
			wantErr:   "json.Unmarshal(): unexpected end of JSON input",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			clock := &fakeClock{}
			sleep := clock.Sleep
			if tt.cutShort {
				sleep = func(context.Context, time.Duration) error {
					return errors.Wrap(context.Canceled, "waiting")
				}
			}
			var out strings.Builder
			calls := 0
			err := retrier{sleep: sleep, out: &out}.do(ctx, func() error {
				calls++
				if calls > len(tt.answers) {
					t.Fatalf("attempt %d, want at most %d", calls, len(tt.answers))
				}
				if answer := tt.answers[calls-1]; answer != nil {
					return answer()
				}

				return nil
			})
			if calls != tt.wantCalls {
				t.Errorf("made %d attempts, want %d", calls, tt.wantCalls)
			}
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("do() error = %v, want none", err)
			case tt.wantErr != "" && (err == nil || errors.Cause(err).Error() != tt.wantErr):
				t.Errorf("do() error = %v, want %q", err, tt.wantErr)
			}
			if out.String() != tt.wantOut {
				t.Errorf("noted:\n%s\nwant:\n%s", out.String(), tt.wantOut)
			}
			if !slices.Equal(clock.slept, tt.wantSlept) {
				t.Errorf("slept %v, want %v", clock.slept, tt.wantSlept)
			}
		})
	}
}

// refusal is a Google API's answer outside 2xx.
func refusal(code int, message string) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "message": message}}
}

// countingServer is a stand-in for an API that answers each request by how many have
// come before it (from 1), through answer; it keeps the requests' methods and paths.
type countingServer struct {
	mu       sync.Mutex
	requests []string
	answer   func(call int, r *http.Request) (int, map[string]any)
}

func (s *countingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.RequestURI())
	status, body := s.answer(len(s.requests), r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// failingFirst is a transport whose first failures requests fail with err before the
// answer, as a connection that was reset or timed out does; the rest go to next.
type failingFirst struct {
	mu       sync.Mutex
	failures int
	err      error
	next     http.RoundTripper
	calls    int
}

func (f *failingFirst) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.calls++
	failing := f.calls <= f.failures
	f.mu.Unlock()
	if failing {
		return nil, f.err
	}
	resp, err := f.next.RoundTrip(r)
	if err != nil {
		return nil, errors.Wrap(err, "http.RoundTripper.RoundTrip()")
	}

	return resp, nil
}

func TestCloudRunRetries(t *testing.T) {
	t.Parallel()

	const (
		service     = "projects/p/locations/l/services/s"
		operation   = "projects/p/locations/l/operations/op-1"
		unavailable = "The service is currently unavailable."
	)
	done := map[string]any{"name": operation, "done": true, "response": map[string]any{"name": service}}
	tests := []struct {
		name string
		// answer is what the stand-in says to each request, by its count.
		answer func(call int, r *http.Request) (int, map[string]any)
		// failures is how many requests fail before an answer with failure.
		failures int
		failure  error
		call     func(ctx context.Context, run Run) (map[string]any, error)
		wantName string
		wantErr  string
		// wantRequests are the requests the stand-in saw, in order.
		wantRequests []string
		wantOut      string
		wantSlept    []time.Duration
	}{
		{
			name: "an operation read answered 503 twice is read a third time",
			answer: func(call int, _ *http.Request) (int, map[string]any) {
				if call <= 2 {
					return http.StatusServiceUnavailable, refusal(503, unavailable)
				}

				return http.StatusOK, done
			},
			call: func(ctx context.Context, run Run) (map[string]any, error) {
				return run.Get(ctx, operation)
			},
			wantName:     operation,
			wantRequests: []string{"GET /v2/" + operation, "GET /v2/" + operation, "GET /v2/" + operation},
			wantOut: "Cloud Run answered HTTP 503 to GET /v2/" + operation + ": The service is currently unavailable; attempt 2 of 5 in 5s.\n" +
				"Cloud Run answered HTTP 503 to GET /v2/" + operation + ": The service is currently unavailable; attempt 3 of 5 in 10s.\n",
			wantSlept: []time.Duration{5 * time.Second, 10 * time.Second},
		},
		{
			name: "a service patch answered 503 five times fails saying so",
			answer: func(int, *http.Request) (int, map[string]any) {
				return http.StatusServiceUnavailable, refusal(503, unavailable)
			},
			call: func(ctx context.Context, run Run) (map[string]any, error) {
				return run.Patch(ctx, service, map[string]any{keyName: service}, keyLabels)
			},
			wantErr:      "Cloud Run answered HTTP 503 to PATCH /v2/" + service + "?updateMask=labels: The service is currently unavailable; given up after 5 attempts",
			wantRequests: slices.Repeat([]string{"PATCH /v2/" + service + "?updateMask=labels"}, 5),
			wantOut: "Cloud Run answered HTTP 503 to PATCH /v2/" + service + "?updateMask=labels: The service is currently unavailable; attempt 2 of 5 in 5s.\n" +
				"Cloud Run answered HTTP 503 to PATCH /v2/" + service + "?updateMask=labels: The service is currently unavailable; attempt 3 of 5 in 10s.\n" +
				"Cloud Run answered HTTP 503 to PATCH /v2/" + service + "?updateMask=labels: The service is currently unavailable; attempt 4 of 5 in 20s.\n" +
				"Cloud Run answered HTTP 503 to PATCH /v2/" + service + "?updateMask=labels: The service is currently unavailable; attempt 5 of 5 in 40s.\n",
			wantSlept: []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second},
		},
		{
			name: "a 500 while polling the patch's operation is read again",
			answer: func(call int, r *http.Request) (int, map[string]any) {
				switch {
				case r.Method == http.MethodPatch:
					return http.StatusOK, map[string]any{"name": operation, "done": false}
				case call == 2:
					return http.StatusInternalServerError, refusal(500, "Timed out read retrieving data")
				default:
					return http.StatusOK, done
				}
			},
			call: func(ctx context.Context, run Run) (map[string]any, error) {
				return run.Patch(ctx, service, map[string]any{keyName: service})
			},
			wantName:     service,
			wantRequests: []string{"PATCH /v2/" + service, "GET /v2/" + operation, "GET /v2/" + operation},
			wantOut:      "Cloud Run answered HTTP 500 to GET /v2/" + operation + ": Timed out read retrieving data; attempt 2 of 5 in 5s.\n",
			wantSlept:    []time.Duration{5 * time.Second},
		},
		{
			name: "a 404 is the answer",
			answer: func(int, *http.Request) (int, map[string]any) {
				return http.StatusNotFound, refusal(404, "Requested entity was not found.")
			},
			call: func(ctx context.Context, run Run) (map[string]any, error) {
				return run.Get(ctx, service)
			},
			wantErr:      "Cloud Run answered HTTP 404 to GET /v2/" + service + ": Requested entity was not found.",
			wantRequests: []string{"GET /v2/" + service},
		},
		{
			name: "a reset connection is sent again",
			answer: func(int, *http.Request) (int, map[string]any) {
				return http.StatusOK, done
			},
			failures: 1,
			failure:  syscall.ECONNRESET,
			call: func(ctx context.Context, run Run) (map[string]any, error) {
				return run.Get(ctx, operation)
			},
			wantName:     operation,
			wantRequests: []string{"GET /v2/" + operation},
			wantOut:      "Cloud Run did not answer GET /v2/" + operation + ": connection reset by peer; attempt 2 of 5 in 5s.\n",
			wantSlept:    []time.Duration{5 * time.Second},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stand := &countingServer{answer: tt.answer}
			srv := httptest.NewServer(stand)
			t.Cleanup(srv.Close)
			client := srv.Client()
			if tt.failures > 0 {
				client = &http.Client{Transport: &failingFirst{failures: tt.failures, err: tt.failure, next: srv.Client().Transport}}
			}
			clock := &fakeClock{}
			var out strings.Builder
			run := &cloudRun{http: client, base: srv.URL, poll: time.Millisecond, retry: retrier{sleep: clock.Sleep, out: &out}}
			doc, err := tt.call(t.Context(), run)
			switch {
			case tt.wantErr == "" && (err != nil || text(doc, keyName) != tt.wantName):
				t.Errorf("call = %v, %v, want %s", doc, err, tt.wantName)
			case tt.wantErr != "" && (err == nil || errors.Cause(err).Error() != tt.wantErr):
				t.Errorf("call error = %v, want %q", err, tt.wantErr)
			}
			if !slices.Equal(stand.requests, tt.wantRequests) {
				t.Errorf("requests %q, want %q", stand.requests, tt.wantRequests)
			}
			if out.String() != tt.wantOut {
				t.Errorf("noted:\n%s\nwant:\n%s", out.String(), tt.wantOut)
			}
			if !slices.Equal(clock.slept, tt.wantSlept) {
				t.Errorf("slept %v, want %v", clock.slept, tt.wantSlept)
			}
		})
	}
}

func TestArtifactRegistryRetries(t *testing.T) {
	t.Parallel()

	const (
		image       = "us-central1-docker.pkg.dev/shr/repo/harbor"
		pkg         = "/v1/projects/shr/locations/us-central1/repositories/repo/packages/harbor"
		unavailable = "The service is currently unavailable."
	)
	tagged := map[string]any{"name": pkg[4:] + "/tags/v1.2.3-tst", keyVersion: pkg[4:] + "/versions/sha256:abc"}
	tests := []struct {
		name     string
		answer   func(call int, r *http.Request) (int, map[string]any)
		failures int
		failure  error
		call     func(ctx context.Context, registry Registry) (string, error)
		want     string
		wantErr  string
		// wantRequests are the requests the stand-in saw, in order.
		wantRequests []string
		wantOut      string
		wantSlept    []time.Duration
	}{
		{
			name: "a tag read answered 503 is read again",
			answer: func(call int, _ *http.Request) (int, map[string]any) {
				if call == 1 {
					return http.StatusServiceUnavailable, refusal(503, unavailable)
				}

				return http.StatusOK, tagged
			},
			call: func(ctx context.Context, registry Registry) (string, error) {
				return registry.Digest(ctx, image, "v1.2.3-tst")
			},
			want:         "sha256:abc",
			wantRequests: []string{"GET " + pkg + "/tags/v1.2.3-tst", "GET " + pkg + "/tags/v1.2.3-tst"},
			wantOut:      "Artifact Registry answered HTTP 503 to GET " + pkg + "/tags/v1.2.3-tst: The service is currently unavailable; attempt 2 of 5 in 5s.\n",
			wantSlept:    []time.Duration{5 * time.Second},
		},
		{
			name: "a tag that does not exist is the answer",
			answer: func(int, *http.Request) (int, map[string]any) {
				return http.StatusNotFound, refusal(404, "Requested entity was not found.")
			},
			call: func(ctx context.Context, registry Registry) (string, error) {
				return registry.Digest(ctx, image, "missing")
			},
			wantRequests: []string{"GET " + pkg + "/tags/missing"},
		},
		{
			name: "a tag add answered 503 five times fails saying so",
			answer: func(int, *http.Request) (int, map[string]any) {
				return http.StatusServiceUnavailable, refusal(503, unavailable)
			},
			call: func(ctx context.Context, registry Registry) (string, error) {
				return "", registry.AddTag(ctx, image, "v1.2.4-tst", "sha256:abc")
			},
			wantErr:      "Artifact Registry answered HTTP 503 to POST " + pkg + "/tags?tagId=v1.2.4-tst: The service is currently unavailable; given up after 5 attempts",
			wantRequests: slices.Repeat([]string{"POST " + pkg + "/tags?tagId=v1.2.4-tst"}, 5),
			wantOut: "Artifact Registry answered HTTP 503 to POST " + pkg + "/tags?tagId=v1.2.4-tst: The service is currently unavailable; attempt 2 of 5 in 5s.\n" +
				"Artifact Registry answered HTTP 503 to POST " + pkg + "/tags?tagId=v1.2.4-tst: The service is currently unavailable; attempt 3 of 5 in 10s.\n" +
				"Artifact Registry answered HTTP 503 to POST " + pkg + "/tags?tagId=v1.2.4-tst: The service is currently unavailable; attempt 4 of 5 in 20s.\n" +
				"Artifact Registry answered HTTP 503 to POST " + pkg + "/tags?tagId=v1.2.4-tst: The service is currently unavailable; attempt 5 of 5 in 40s.\n",
			wantSlept: []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second},
		},
		{
			name: "a refusal is the answer",
			answer: func(int, *http.Request) (int, map[string]any) {
				return http.StatusForbidden, refusal(403, "Permission denied on resource")
			},
			call: func(ctx context.Context, registry Registry) (string, error) {
				return registry.Digest(ctx, image, "v1.2.3-tst")
			},
			wantErr:      "Artifact Registry answered HTTP 403 to GET " + pkg + "/tags/v1.2.3-tst: Permission denied on resource",
			wantRequests: []string{"GET " + pkg + "/tags/v1.2.3-tst"},
		},
		{
			name: "a timed-out read is sent again",
			answer: func(int, *http.Request) (int, map[string]any) {
				return http.StatusOK, tagged
			},
			failures: 1,
			failure:  os.ErrDeadlineExceeded,
			call: func(ctx context.Context, registry Registry) (string, error) {
				return registry.Digest(ctx, image, "v1.2.3-tst")
			},
			want:         "sha256:abc",
			wantRequests: []string{"GET " + pkg + "/tags/v1.2.3-tst"},
			wantOut:      "Artifact Registry did not answer GET " + pkg + "/tags/v1.2.3-tst: i/o timeout; attempt 2 of 5 in 5s.\n",
			wantSlept:    []time.Duration{5 * time.Second},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stand := &countingServer{answer: tt.answer}
			srv := httptest.NewServer(stand)
			t.Cleanup(srv.Close)
			client := srv.Client()
			if tt.failures > 0 {
				client = &http.Client{Transport: &failingFirst{failures: tt.failures, err: tt.failure, next: srv.Client().Transport}}
			}
			clock := &fakeClock{}
			var out strings.Builder
			registry := &artifactRegistry{http: client, base: srv.URL, retry: retrier{sleep: clock.Sleep, out: &out}}
			got, err := tt.call(t.Context(), registry)
			switch {
			case tt.wantErr == "" && (err != nil || got != tt.want):
				t.Errorf("call = %q, %v, want %q", got, err, tt.want)
			case tt.wantErr != "" && (err == nil || errors.Cause(err).Error() != tt.wantErr):
				t.Errorf("call error = %v, want %q", err, tt.wantErr)
			}
			if !slices.Equal(stand.requests, tt.wantRequests) {
				t.Errorf("requests %q, want %q", stand.requests, tt.wantRequests)
			}
			if out.String() != tt.wantOut {
				t.Errorf("noted:\n%s\nwant:\n%s", out.String(), tt.wantOut)
			}
			if !slices.Equal(clock.slept, tt.wantSlept) {
				t.Errorf("slept %v, want %v", clock.slept, tt.wantSlept)
			}
		})
	}
}
