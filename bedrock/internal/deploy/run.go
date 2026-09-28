package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"golang.org/x/oauth2/google"
)

// cloudRunAPI is where the services and jobs are read, changed and run.
const cloudRunAPI = "https://run.googleapis.com"

// The keys of a resource's document the steps write: a traffic target's type, revision,
// percent and tag, and the resource's name.
const (
	keyName     = "name"
	keyTraffic  = "traffic"
	keyType     = "type"
	keyRevision = "revision"
	keyPercent  = "percent"
	keyTag      = "tag"
)

// Run reads and changes Cloud Run services and jobs and follows what it started: the
// v2 API, or a fake in tests. A resource is its JSON document, as the API answers it,
// so a change edits the document and sends it back whole.
type Run interface {
	// Get reads the resource (projects/<p>/locations/<r>/jobs/<j> or .../services/<s>).
	Get(ctx context.Context, name string) (map[string]any, error)
	// Patch sends the resource back and waits for the change to settle; with fields, only
	// those are updated. It answers the resource as it settled.
	Patch(ctx context.Context, name string, resource map[string]any, fields ...string) (map[string]any, error)
	// RunJob starts an execution of the job, with the arguments overriding the container's
	// when given, and waits for it to end. It answers the execution.
	RunJob(ctx context.Context, name string, args []string) (map[string]any, error)
	// Services lists the services of the project in the region, every page.
	Services(ctx context.Context, project, region string) ([]map[string]any, error)
}

// RunFunc opens Run.
type RunFunc func(ctx context.Context) (Run, error)

// NewCloudRun opens the Cloud Run v2 REST API with the process's default credentials.
func NewCloudRun(ctx context.Context) (Run, error) {
	client, err := google.DefaultClient(ctx, cloudPlatformScope)
	if err != nil {
		return nil, errors.Wrap(err, "google.DefaultClient()")
	}

	return &cloudRun{http: client, base: cloudRunAPI, poll: 5 * time.Second}, nil
}

// cloudRun is Run over the v2 API. Every change is a long-running operation, polled
// until done.
type cloudRun struct {
	http *http.Client
	base string
	poll time.Duration
}

func (c *cloudRun) Get(ctx context.Context, name string) (map[string]any, error) {
	return c.call(ctx, http.MethodGet, "/v2/"+name, nil)
}

func (c *cloudRun) Services(ctx context.Context, project, region string) ([]map[string]any, error) {
	var all []map[string]any
	token := ""
	for {
		path := "/v2/projects/" + project + "/locations/" + region + "/services?pageSize=100"
		if token != "" {
			path += "&pageToken=" + url.QueryEscape(token)
		}
		page, err := c.call(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		list, _ := page["services"].([]any)
		for _, item := range list {
			if doc, ok := item.(map[string]any); ok {
				all = append(all, doc)
			}
		}
		if token, _ = page["nextPageToken"].(string); token == "" {
			return all, nil
		}
	}
}

func (c *cloudRun) Patch(ctx context.Context, name string, resource map[string]any, fields ...string) (map[string]any, error) {
	path := "/v2/" + name
	if len(fields) > 0 {
		path += "?updateMask=" + strings.Join(fields, ",")
	}
	op, err := c.call(ctx, http.MethodPatch, path, resource)
	if err != nil {
		return nil, err
	}

	return c.wait(ctx, op)
}

func (c *cloudRun) RunJob(ctx context.Context, name string, args []string) (map[string]any, error) {
	body := map[string]any{}
	if len(args) > 0 {
		body["overrides"] = map[string]any{"containerOverrides": []map[string]any{{"args": args}}}
	}
	op, err := c.call(ctx, http.MethodPost, "/v2/"+name+":run", body)
	if err != nil {
		return nil, err
	}

	return c.wait(ctx, op)
}

// wait polls the operation until it is done and answers its response; an operation
// that ended in error is that error.
func (c *cloudRun) wait(ctx context.Context, op map[string]any) (map[string]any, error) {
	name, _ := op[keyName].(string)
	if name == "" {
		return nil, errors.New("Cloud Run answered no operation name")
	}
	for {
		if done, _ := op["done"].(bool); done {
			if apiErr, ok := op["error"].(map[string]any); ok {
				msg, _ := apiErr["message"].(string)

				return nil, errors.Newf("Cloud Run operation %s failed: %s", name, msg)
			}
			response, _ := op["response"].(map[string]any)

			return response, nil
		}
		select {
		case <-ctx.Done():
			return nil, errors.Wrapf(ctx.Err(), "waiting for Cloud Run operation %s", name)
		case <-time.After(c.poll):
		}
		var err error
		if op, err = c.call(ctx, http.MethodGet, "/v2/"+name, nil); err != nil {
			return nil, err
		}
	}
}

// call sends one request and decodes the answer; a status outside 2xx is an error
// carrying the API's message.
func (c *cloudRun) call(ctx context.Context, method, path string, body map[string]any) (map[string]any, error) {
	var payload io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, errors.Wrap(err, "json.Marshal()")
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, payload)
	if err != nil {
		return nil, errors.Wrap(err, "http.NewRequestWithContext()")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "http.Client.Do()")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return nil, errors.Wrap(err, "io.ReadAll()")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		return nil, errors.Newf("Cloud Run answered HTTP %d to %s %s: %s", resp.StatusCode, method, path, apiMessage(data))
	}
	answer := map[string]any{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &answer); err != nil {
			return nil, errors.Wrap(err, "json.Unmarshal()")
		}
	}

	return answer, nil
}

// Helpers over a resource's document.

// field reads a nested value by path (a.b.c), or nil.
func field(doc map[string]any, path string) any {
	var cur any = doc
	for _, key := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[key]
	}

	return cur
}

// text is the string a nested value holds, or empty.
func text(doc map[string]any, path string) string {
	s, _ := field(doc, path).(string)

	return s
}

// firstContainer is the first container of a template (a service's template, a job's
// task template), which is the application's.
func firstContainer(template map[string]any) (map[string]any, error) {
	containers, _ := template["containers"].([]any)
	if len(containers) == 0 {
		return nil, errors.New("the template has no container")
	}
	container, ok := containers[0].(map[string]any)
	if !ok {
		return nil, errors.New("the template's first container is not an object")
	}

	return container, nil
}

// setLabels puts the pipeline's labels on the document's labels, keeping every other
// label; a label with an empty value is removed.
func setLabels(doc map[string]any, labels map[string]string) {
	existing, _ := doc["labels"].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
	}
	for name, value := range labels {
		if value == "" {
			delete(existing, name)

			continue
		}
		existing[name] = value
	}
	doc["labels"] = existing
}

// shortName is the last element of a resource name (a revision's, an execution's).
func shortName(name string) string {
	return name[strings.LastIndex(name, "/")+1:]
}
