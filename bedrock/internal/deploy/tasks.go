// tasks.go is the Cloud Tasks seam: a maintenance step pauses the application's queue
// while the database is replaced or migrated, purges it on a restore, and resumes it
// after traffic moves.

package deploy

import (
	"context"
	"net/http"
	"time"

	"github.com/go-playground/errors/v5"
	"golang.org/x/oauth2/google"
)

// cloudTasksAPI is where a queue is paused, purged and resumed.
const cloudTasksAPI = "https://cloudtasks.googleapis.com"

// Tasks pauses, purges and resumes a Cloud Tasks queue, named in full
// (projects/<p>/locations/<r>/queues/<q>): the v2 API, or a fake in tests. Each call
// answers the queue's state as the API reports it.
type Tasks interface {
	// Pause stops the queue from dispatching; tasks keep queuing up.
	Pause(ctx context.Context, queue string) (string, error)
	// Purge deletes every task in the queue.
	Purge(ctx context.Context, queue string) (string, error)
	// Resume lets the queue dispatch again.
	Resume(ctx context.Context, queue string) (string, error)
	// State is the queue's state (RUNNING, PAUSED, DISABLED).
	State(ctx context.Context, queue string) (string, error)
}

// TasksFunc opens Tasks.
type TasksFunc func(ctx context.Context) (Tasks, error)

// NewCloudTasks opens the Cloud Tasks v2 REST API with the process's default credentials.
func NewCloudTasks(ctx context.Context) (Tasks, error) {
	client, err := google.DefaultClient(ctx, cloudPlatformScope)
	if err != nil {
		return nil, errors.Wrap(err, "google.DefaultClient()")
	}

	return &cloudTasks{cloudRun: &cloudRun{http: client, base: cloudTasksAPI, poll: time.Second, service: "Cloud Tasks"}}, nil
}

// cloudTasks is Tasks over the v2 API; it reuses the Cloud Run client's calling, since
// both are plain JSON over REST with the same credentials.
type cloudTasks struct {
	*cloudRun
}

func (c *cloudTasks) Pause(ctx context.Context, queue string) (string, error) {
	return c.verb(ctx, queue, "pause")
}

func (c *cloudTasks) Purge(ctx context.Context, queue string) (string, error) {
	return c.verb(ctx, queue, "purge")
}

func (c *cloudTasks) Resume(ctx context.Context, queue string) (string, error) {
	return c.verb(ctx, queue, "resume")
}

// State reads the queue and answers its state.
func (c *cloudTasks) State(ctx context.Context, queue string) (string, error) {
	doc, err := c.call(ctx, http.MethodGet, "/v2/"+queue, nil)
	if err != nil {
		return "", err
	}

	return text(doc, "state"), nil
}

// verb posts one of the queue's verbs and answers the queue's state.
func (c *cloudTasks) verb(ctx context.Context, queue, verb string) (string, error) {
	doc, err := c.call(ctx, http.MethodPost, "/v2/"+queue+":"+verb, map[string]any{})
	if err != nil {
		return "", err
	}

	return text(doc, "state"), nil
}
