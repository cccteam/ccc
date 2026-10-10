package integration

import (
	"bytes"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-playground/errors/v5"
)

// requestLogOutput collects what the console exporter writes through the standard
// logger while the request log test runs.
type requestLogOutput struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *requestLogOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	n, err := o.buf.Write(p)
	if err != nil {
		return n, errors.Wrap(err, "bytes.Buffer.Write()")
	}

	return n, nil
}

func (o *requestLogOutput) reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.buf.Reset()
}

func (o *requestLogOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.buf.String()
}

// TestRequestLogWords proves the request log words land where the generator program and
// the annotations put them, on the served stack with the console exporter deciding each
// request's entry as the Cloud Logging exporter would. The beacon prefix, mounted by hand
// and declared on event, writes no entry for a pulse answered 200 and writes one for a
// pulse answered 404; IngestDroidReports, declared on event on its @rpc, writes no entry
// for a reading that lands and writes one for a reading it refuses; and a droid's list,
// which declares nothing, writes its entry always, as every request did before. The
// steps run in order, not as parallel subtests, and the test itself runs on its own: the
// console exporter writes through the standard logger, which the test redirects to read
// the decisions, so nothing else may write through it meanwhile.
//
// Demonstrates: generation.request-log, @rpc.log.
func TestRequestLogWords(t *testing.T) {
	ctx := t.Context()
	s := newServed(ctx, t)

	const unknownSector = "c0000000-0000-4000-8000-00000000beac"
	ingest := "/droids/sectors/" + anvil + "/ingest-droid-reports"

	var output requestLogOutput
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	steps := []struct {
		name       string
		method     string
		path       string
		key        string
		body       string
		wantStatus int
		// wantEntry says whether the request's own entry is written.
		wantEntry bool
	}{
		{
			name:       "a pulse for a charted sector writes no entry: the prefix is declared on event",
			method:     http.MethodGet,
			path:       "/beacons/" + anvil,
			wantStatus: http.StatusOK,
		},
		{
			name:       "a pulse for a sector that is not charted is an event",
			method:     http.MethodGet,
			path:       "/beacons/" + unknownSector,
			wantStatus: http.StatusNotFound,
			wantEntry:  true,
		},
		{
			name:       "a reading that lands writes no entry: the method is declared on event",
			method:     http.MethodPost,
			path:       ingest,
			key:        droidsAPIKey,
			body:       `{"shipId":"` + shipLanternID + `","subsystem":"reactor","reading":0.41,"recordedAt":"2026-11-30T12:00:00Z"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "a reading the method refuses is an event",
			method:     http.MethodPost,
			path:       ingest,
			key:        droidsAPIKey,
			body:       `{"shipId":"` + shipLanternID + `","reading":0.41}`,
			wantStatus: http.StatusBadRequest,
			wantEntry:  true,
		},
		{
			name:       "a droid's list writes its entry always: nothing is declared for it",
			method:     http.MethodGet,
			path:       "/droids/sectors/" + anvil + "/droid-reports",
			key:        droidsAPIKey,
			wantStatus: http.StatusOK,
			wantEntry:  true,
		},
	}
	for _, step := range steps {
		output.reset()
		status, body := droid(ctx, t, s, step.key, step.method, step.path, []byte(step.body))
		if status != step.wantStatus {
			t.Fatalf("%s: %s %s: status %d, want %d: %s", step.name, step.method, step.path, status, step.wantStatus, body)
		}
		entry := step.method + " " + step.path + " " + strconv.Itoa(status)
		written := strings.Contains(output.String(), entry)
		switch {
		case step.wantEntry && !written:
			t.Errorf("%s: the request's entry %q was not written; the console wrote:\n%s", step.name, entry, output.String())
		case !step.wantEntry && written:
			t.Errorf("%s: the request's entry %q was written; the console wrote:\n%s", step.name, entry, output.String())
		}
	}
}
