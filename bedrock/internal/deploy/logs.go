// logs.go is the Cloud Logging seam: the migrate step reads the lines its job wrote, by the
// execution's name, through the log view the environment's stack defines over the migrate
// job's own log bucket, and prints them into the build log, so a failed migration's
// message, or the database's migration version, reaches the person who started the run
// without a console. The deploy identity holds the read on that view and nothing wider.

package deploy

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"golang.org/x/oauth2/google"
)

// loggingAPI is where the entries are read.
const loggingAPI = "https://logging.googleapis.com"

// Logs reads log entries: Cloud Logging's v2 API, or a fake in tests.
type Logs interface {
	// Lines reads the text of a job execution's entries through the view (a log view's
	// resource name, projects/<p>/locations/<l>/buckets/<b>/views/<v>), oldest first.
	Lines(ctx context.Context, view, execution string) ([]string, error)
}

// LogsFunc opens Logs.
type LogsFunc func(ctx context.Context) (Logs, error)

// NewCloudLogging opens the Cloud Logging v2 REST API with the process's default
// credentials.
func NewCloudLogging(ctx context.Context) (Logs, error) {
	client, err := google.DefaultClient(ctx, cloudPlatformScope)
	if err != nil {
		return nil, errors.Wrap(err, "google.DefaultClient()")
	}

	return &cloudLogging{cloudRun: &cloudRun{http: client, base: loggingAPI, poll: time.Second, service: "Cloud Logging"}}, nil
}

// cloudLogging is Logs over the v2 API.
type cloudLogging struct {
	*cloudRun
}

// loggingQuery is the filter that finds a job execution's entries, as the console's
// query and the API's filter alike.
func loggingQuery(execution string) string {
	return fmt.Sprintf(`resource.type="cloud_run_job" AND labels."run.googleapis.com/execution_name"=%q`, execution)
}

func (c *cloudLogging) Lines(ctx context.Context, view, execution string) ([]string, error) {
	var lines []string
	token := ""
	for {
		body := map[string]any{"resourceNames": []string{view}, "filter": loggingQuery(execution), "orderBy": "timestamp asc", "pageSize": 1000}
		if token != "" {
			body["pageToken"] = token
		}
		answer, err := c.call(ctx, http.MethodPost, "/v2/entries:list", body)
		if err != nil {
			return nil, err
		}
		entries, _ := answer["entries"].([]any)
		for _, entry := range entries {
			doc, _ := entry.(map[string]any)
			if line := entryText(doc); line != "" {
				lines = append(lines, line)
			}
		}
		if token, _ = answer["nextPageToken"].(string); token == "" {
			return lines, nil
		}
	}
}

// entryText is an entry's message: its text payload, or its JSON payload's message, without
// the newline a logger ends a line with.
func entryText(doc map[string]any) string {
	if s := text(doc, "textPayload"); s != "" {
		return strings.TrimRight(s, "\n")
	}

	return strings.TrimRight(text(doc, "jsonPayload.message"), "\n")
}
