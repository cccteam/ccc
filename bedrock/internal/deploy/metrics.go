// metrics.go is the Cloud Monitoring seam: a maintenance step reads how many instances of
// the revision that served before maintenance are still handling a request, so it can
// let them finish before the database is replaced or migrated.

package deploy

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-playground/errors/v5"
	"golang.org/x/oauth2/google"
)

// monitoringAPI is where the time series are read.
const monitoringAPI = "https://monitoring.googleapis.com"

// Metrics reads the instances of a revision that are handling a request: Cloud Run's
// run.googleapis.com/container/instance_count with state active, or a fake in tests.
type Metrics interface {
	// ActiveInstances is the newest count of the revision's active instances in the
	// window ending now; known is false when the window holds no point (a revision
	// with no traffic reports none).
	ActiveInstances(ctx context.Context, project, revision string, window time.Duration) (count int, known bool, err error)
}

// MetricsFunc opens Metrics.
type MetricsFunc func(ctx context.Context) (Metrics, error)

// NewCloudMonitoring opens the Cloud Monitoring v3 REST API with the process's default
// credentials.
func NewCloudMonitoring(ctx context.Context) (Metrics, error) {
	client, err := google.DefaultClient(ctx, cloudPlatformScope)
	if err != nil {
		return nil, errors.Wrap(err, "google.DefaultClient()")
	}

	return &cloudMonitoring{cloudRun: &cloudRun{http: client, base: monitoringAPI, poll: time.Second, service: "Cloud Monitoring"}, now: time.Now}, nil
}

// cloudMonitoring is Metrics over the v3 API.
type cloudMonitoring struct {
	*cloudRun
	now func() time.Time
}

func (c *cloudMonitoring) ActiveInstances(ctx context.Context, project, revision string, window time.Duration) (count int, known bool, err error) {
	end := c.now().UTC()
	query := url.Values{}
	query.Set("filter", fmt.Sprintf(`metric.type = "run.googleapis.com/container/instance_count" AND resource.labels.revision_name = %q AND metric.labels.state = "active"`, revision))
	query.Set("interval.startTime", end.Add(-window).Format(time.RFC3339))
	query.Set("interval.endTime", end.Format(time.RFC3339))
	doc, err := c.call(ctx, http.MethodGet, "/v3/projects/"+project+"/timeSeries?"+query.Encode(), nil)
	if err != nil {
		return 0, false, err
	}
	series, _ := doc["timeSeries"].([]any)
	for _, entry := range series {
		s, _ := entry.(map[string]any)
		points, _ := s["points"].([]any)
		if len(points) == 0 {
			continue
		}
		// The newest point comes first.
		point, _ := points[0].(map[string]any)
		value, _ := point["value"].(map[string]any)
		known = true
		switch v := value["int64Value"].(type) {
		case string:
			n, err := strconv.Atoi(v)
			if err != nil {
				return 0, false, errors.Wrapf(err, "reading the point %q", v)
			}
			count += n
		case float64:
			count += int(v)
		}
	}

	return count, known, nil
}
