// Package jobs starts the application's job process: an execution of its Cloud Run job,
// the one deployed with the serving revision. The rule it serves: a Cloud Run job is
// started by the running service and never by a schedule or a hand; Cloud Scheduler
// calls a scheduled route on the service (resource/scheduled), and the method behind
// it starts the job. So a traffic rollback rolls the job back too, since each
// revision's image names the job built with it.
//
// The pipeline bakes the job's resource name into the image as JobVariable
// (APP_JOBS_JOB). FromEnvironment reads it when the application starts: set, the
// starter runs the job through the Cloud Run Admin API as the service's own identity,
// which holds roles/run.invoker on its job; unset (development, a pull-request stack,
// an application without a job process), every start is refused with a message saying
// so, which the start logs.
package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/option"
	htransport "google.golang.org/api/transport/http"
)

const (
	// JobVariable is the environment variable naming the application's Cloud Run job
	// (projects/<p>/locations/<l>/jobs/<j>), baked into the image by the pipeline.
	JobVariable = "APP_JOBS_JOB"
	// cloudRunAPI is where a job is run.
	cloudRunAPI = "https://run.googleapis.com"
	// cloudPlatformScope is the OAuth scope the service's token carries for the API.
	cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"
	// callTimeout bounds the start call; the execution itself runs on without the caller.
	callTimeout = 30 * time.Second
	// maxAnswer bounds the operation the API answers with.
	maxAnswer = 1 << 20
	// jobNamePrefix is how a job's resource name begins.
	jobNamePrefix = "projects/"
)

// Starter starts the job process: the Cloud Run job in production, a fake in tests, or
// None where no job is configured.
type Starter interface {
	// Start starts one execution of the job with args as the container's arguments (the
	// job process's command and its flags, "cleanup-files" for the orphaned-file
	// cleanup) and answers the execution's resource name. The execution runs on; a
	// caller that wants its outcome reads it from Cloud Run.
	Start(ctx context.Context, args ...string) (execution string, err error)
}

// FromEnvironment reads JobVariable: the Cloud Run starter for the job it names, or
// None when it is empty, which is logged.
func FromEnvironment(ctx context.Context) (Starter, error) {
	job := strings.TrimSpace(os.Getenv(JobVariable))
	if job == "" {
		logger.FromCtx(ctx).Infof("jobs: %s is not set; no job process is configured, so a scheduled method that starts one answers that it cannot", JobVariable)

		return None{}, nil
	}
	starter, err := NewCloudRun(ctx, job)
	if err != nil {
		return nil, err
	}
	logger.FromCtx(ctx).Infof("jobs: the job process is %s", job)

	return starter, nil
}

// None is the starter of an application with no job configured: every start is refused,
// saying so.
type None struct{}

// Start refuses: no job process is configured.
func (None) Start(context.Context, ...string) (string, error) {
	return "", errors.Newf("no job process is configured (%s is not set), so the job cannot be started", JobVariable)
}

// CloudRun starts executions of one job through the Cloud Run Admin API.
type CloudRun struct {
	job  string
	http *http.Client
	base string
}

// NewCloudRun opens the Cloud Run Admin API with the process's default credentials, the
// service's own identity, for the job named (projects/<p>/locations/<l>/jobs/<j>).
func NewCloudRun(ctx context.Context, job string) (*CloudRun, error) {
	if !strings.HasPrefix(job, jobNamePrefix) || strings.Count(job, "/") != 5 || !strings.Contains(job, "/jobs/") {
		return nil, errors.Newf("%s=%q is not a Cloud Run job's resource name (projects/<project>/locations/<location>/jobs/<job>)", JobVariable, job)
	}
	client, _, err := htransport.NewClient(ctx, option.WithScopes(cloudPlatformScope))
	if err != nil {
		return nil, errors.Wrap(err, "transport/http.NewClient()")
	}

	return &CloudRun{job: job, http: client, base: cloudRunAPI}, nil
}

// Job is the job's resource name.
func (c *CloudRun) Job() string {
	return c.job
}

// runRequest is the body of a run call: the container's arguments for this execution.
type runRequest struct {
	Overrides struct {
		ContainerOverrides []struct {
			Args []string `json:"args"`
		} `json:"containerOverrides"`
	} `json:"overrides"`
}

// runOperation is the operation the API answers: the execution's name in its metadata.
type runOperation struct {
	Name     string `json:"name"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Start runs the job once with args as the container's arguments and answers the
// execution's resource name.
func (c *CloudRun) Start(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	var body runRequest
	if len(args) > 0 {
		body.Overrides.ContainerOverrides = append(body.Overrides.ContainerOverrides, struct {
			Args []string `json:"args"`
		}{Args: args})
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", errors.Wrap(err, "json.Marshal()")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v2/"+c.job+":run", bytes.NewReader(encoded))
	if err != nil {
		return "", errors.Wrap(err, "http.NewRequestWithContext()")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "http.Client.Do()")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return "", errors.Wrap(err, "io.ReadAll()")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		return "", errors.Newf("Cloud Run answered HTTP %d to the start of %s: %s", resp.StatusCode, c.job, apiMessage(data))
	}
	var op runOperation
	if err := json.Unmarshal(data, &op); err != nil {
		return "", errors.Wrap(err, "json.Unmarshal(): the run operation")
	}
	if op.Error != nil {
		return "", errors.Newf("Cloud Run refused the start of %s: %s", c.job, op.Error.Message)
	}
	if op.Metadata.Name == "" {
		return "", errors.Newf("Cloud Run answered the start of %s with no execution name (operation %q)", c.job, op.Name)
	}

	return op.Metadata.Name, nil
}

// apiMessage is the message of an error the API answers, or the body trimmed.
func apiMessage(data []byte) string {
	var refusal struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &refusal)
	if refusal.Error.Message != "" {
		return refusal.Error.Message
	}

	return strings.TrimSpace(string(data))
}
