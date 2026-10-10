// Package cloudrun is the Cloud Run job driver: from the variable it declares, it names
// the job process of this build and starts its executions through the Cloud Run Admin
// API. An application embeds Settings in its served site's configuration and calls Open
// with the version the image bakes in, and names no job platform in its own code; moving
// to another platform swaps this import and the embedded settings for that platform's
// driver. The starter Open builds is what the application hands its scheduled methods
// (jobs.Starter).
//
// The rule it serves: a Cloud Run job is started by the running service and never by a
// schedule or a hand; Cloud Scheduler calls a scheduled route on the service
// (resource/scheduled), and the method behind it starts the job. So a traffic rollback
// rolls the job back too, since each revision names the job built with it. The stack
// owns a template job it never runs and sets its resource name on the service
// (Settings.Template); the pipeline copies the template per build into a job named
// after it with the build's version key, on that build's image, and bakes the version
// into the image. Open names the job of this build from the two (JobOf) and runs it
// through the Cloud Run Admin API as the service's own identity, which holds
// roles/run.jobsExecutorWithOverrides on it (a start passes the command's arguments as
// container overrides); with no template (development, a pull-request stack that sets
// none, an application without a job process), every start is refused with a message
// saying so, which the start logs.
package cloudrun

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cccteam/ccc/resource/jobs"
	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/option"
	htransport "google.golang.org/api/transport/http"
)

const (
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

// Settings are the variables the driver reads, declared on the served site's
// configuration by embedding, so the stack and the development environment render them
// from the declaration. The declaration the tools read is declaration.Settings, in the
// package beside this one, which holds nothing of the driver.
type Settings struct {
	// Template is the application's template job as the Cloud Run API names it
	// (projects/<project>/locations/<location>/jobs/<job>): the job the stack owns and
	// never runs, which the pipeline copies per build into a job named after it with the
	// build's version. Empty, as in development and in a pull-request stack that sets
	// none, no job process is configured, and every start is refused saying so.
	Template string `env:"APP_JOBS_TEMPLATE"`
}

// Driver is what Open built: the starter of this build's job, or of no job at all.
type Driver struct {
	job  string
	http *http.Client
	base string
}

var _ jobs.Starter = (*Driver)(nil)

// Open names the job of this build, JobOf(template, version), and opens the Cloud Run
// Admin API with the process's default credentials, the service's own identity, to
// start it. With the template empty the driver starts nothing: every start is refused
// saying no job process is configured, and this logs so. A template with no version
// beside it is refused: the pipeline bakes the version into every image it deploys.
func Open(ctx context.Context, s Settings, version string) (*Driver, error) {
	template := strings.TrimSpace(s.Template)
	if template == "" {
		logger.FromCtx(ctx).Infof("cloudrun: APP_JOBS_TEMPLATE is empty; no job process is configured, so a scheduled method that starts one answers that it cannot")

		return &Driver{}, nil
	}
	version = strings.TrimSpace(version)
	if VersionKey(version) == "" {
		return nil, errors.Newf("APP_JOBS_TEMPLATE=%q names the template job, but the version %q names no key to pick this build's copy by; the pipeline bakes the version into the image", template, version)
	}
	job := JobOf(template, version)
	if !strings.HasPrefix(job, jobNamePrefix) || strings.Count(job, "/") != 5 || !strings.Contains(job, "/jobs/") {
		return nil, errors.Newf("%q is not a Cloud Run job's resource name (projects/<project>/locations/<location>/jobs/<job>)", job)
	}
	client, _, err := htransport.NewClient(ctx, option.WithScopes(cloudPlatformScope))
	if err != nil {
		return nil, errors.Wrap(err, "transport/http.NewClient()")
	}
	logger.FromCtx(ctx).Infof("cloudrun: the job process is %s (the template %s, this build's version %s)", job, template, version)

	return &Driver{job: job, http: client, base: cloudRunAPI}, nil
}

// JobOf is the job of one build: the template job's resource name, a hyphen and
// VersionKey(version) (projects/p/locations/l/jobs/harbor-jobs-v0-1-15). The pipeline
// makes each build's job under this name, from the template on the build's image.
func JobOf(template, version string) string {
	return template + "-" + VersionKey(version)
}

// VersionKey is a version as a name: lowercase, every run of characters outside a-z and
// 0-9 one hyphen, none at either end (v0.1.15 is v0-1-15, pr39@abc1234 is pr39-abc1234).
// The pipeline names a build's job by the same key.
func VersionKey(version string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(version) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			hyphen = false
		case !hyphen && b.Len() > 0:
			b.WriteByte('-')
			hyphen = true
		}
	}

	return strings.TrimSuffix(b.String(), "-")
}

// Job is the job's resource name, empty when no job process is configured.
func (d *Driver) Job() string {
	return d.job
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
// execution's resource name; with no job configured it refuses, saying so.
func (d *Driver) Start(ctx context.Context, args ...string) (string, error) {
	if d.job == "" {
		return "", errors.New("no job process is configured (APP_JOBS_TEMPLATE is empty), so the job cannot be started")
	}
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.base+"/v2/"+d.job+":run", bytes.NewReader(encoded))
	if err != nil {
		return "", errors.Wrap(err, "http.NewRequestWithContext()")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "http.Client.Do()")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return "", errors.Wrap(err, "io.ReadAll()")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		return "", errors.Newf("Cloud Run answered HTTP %d to the start of %s: %s", resp.StatusCode, d.job, apiMessage(data))
	}
	var op runOperation
	if err := json.Unmarshal(data, &op); err != nil {
		return "", errors.Wrap(err, "json.Unmarshal(): the run operation")
	}
	if op.Error != nil {
		return "", errors.Newf("Cloud Run refused the start of %s: %s", d.job, op.Error.Message)
	}
	if op.Metadata.Name == "" {
		return "", errors.Newf("Cloud Run answered the start of %s with no execution name (operation %q)", d.job, op.Name)
	}

	return op.Metadata.Name, nil
}

// Close releases the connections the API client keeps open; a driver with no job holds
// none.
func (d *Driver) Close() {
	if d.http != nil {
		d.http.CloseIdleConnections()
	}
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
