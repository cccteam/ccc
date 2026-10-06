package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/jobs"
	"github.com/cccteam/ccc/resource/lodestar/pkg/rpc"
	"github.com/cccteam/ccc/resource/scheduled"
)

// TestScheduled_cleanUpFiles calls the scheduled cleanup on the served stack the way Cloud
// Scheduler does: POST /_scheduled/clean-up-files with a token Google signed (the fake
// standing in for Google's keys) for the route's URL. The method starts one execution of
// the job process with the cleanup command, through the starter the configuration built,
// and answers the execution; the service starts its job, and the cleanup never runs
// inside the request. A call without a token is refused and starts nothing, and where no
// job is configured the start is refused saying so.
//
// Demonstrates: jobs.start.
func TestScheduled_cleanUpFiles(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	route := scheduled.Prefix + "/clean-up-files"
	audience := "https://" + s.server.Listener.Addr().String() + route

	if status, body := postScheduled(t, s, route, ""); status != http.StatusUnauthorized {
		t.Fatalf("a call without a token: status = %d, want %d: %s", status, http.StatusUnauthorized, body)
	}
	if started := s.jobs.Started(); len(started) != 0 {
		t.Fatalf("a refused call started the job: %v", started)
	}

	status, body := postScheduled(t, s, route, s.scheduler.Mint(audience, schedulerInvoker))
	if status != http.StatusOK {
		t.Fatalf("the scheduler's call: status = %d, want %d: %s", status, http.StatusOK, body)
	}
	var answer struct {
		Execution string `json:"execution"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("the answer is not the cleanup's outcome: %v: %s", err, body)
	}
	if !strings.HasSuffix(answer.Execution, "/executions/j-1") {
		t.Errorf("execution = %q, want the fake's first execution", answer.Execution)
	}
	if started := s.jobs.Started(); len(started) != 1 || strings.Join(started[0], " ") != "cleanup-files" {
		t.Errorf("the job was started with %v, want one start with the cleanup command", started)
	}

	// No job configured (development, a pull-request stack): the start is refused and
	// the method says so.
	client := rpc.NewClient(nil, nil, jobs.None{})
	if _, err := (&rpc.CleanUpFiles{}).Execute(ctx, nil, client); err == nil || !strings.Contains(err.Error(), "no job process is configured") {
		t.Errorf("Execute() with no job error = %v, want the refusal", err)
	}
}
