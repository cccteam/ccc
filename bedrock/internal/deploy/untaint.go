// untaint.go repairs the state before an environment's stack is planned: the Cloud Run
// services and jobs a failed apply left tainted are untainted, so that the plan updates
// them in place instead of replacing them, which production's deletion protection
// refuses.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/go-playground/errors/v5"
)

// The Cloud Run resource types in the stack: the services (google_cloud_run_v2_service.app)
// and the jobs job (google_cloud_run_v2_job.jobs).
const (
	cloudRunServiceType = "google_cloud_run_v2_service"
	cloudRunJobType     = "google_cloud_run_v2_job"
)

// untaintCloudRun lifts the taint off the stack's Cloud Run services and jobs before the
// plan. tofu taints a resource whose create failed, and the next plan replaces it:
// destroy, then create. A Cloud Run service or job whose create failed exists, though:
// what failed was its first revision's start (a health check its image did not pass),
// and the repair is a new revision from the corrected template, which an update in
// place creates. Production's services carry deletion protection, which refuses the
// destroy, so a production service left tainted keeps every later apply from going
// through. Every other kind keeps its taint: a half-created bucket or policy is
// replaced, as before.
func (s *stack) untaintCloudRun(ctx context.Context, out io.Writer) error {
	shown, err := s.tofuOutput(ctx, "show", "-json")
	if err != nil {
		return err
	}
	addresses, err := taintedCloudRun(shown)
	if err != nil {
		return err
	}
	for _, address := range addresses {
		if err := s.tofu(ctx, "untaint", noColorFlag, address); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s is tainted (its create failed) but exists: untainted, so the apply creates a new revision in place of the one whose start failed and does not replace it (which production's deletion protection would refuse).\n", address)
	}

	return nil
}

// taintedCloudRun reads, off tofu show -json of the state, the addresses of the Cloud
// Run services and jobs that are tainted, in the state's order. OpenTofu marks a tainted
// instance's entry "tainted": true (the raw state, tofu state pull, carries the same as
// the instance's status); an untainted one carries no such field.
func taintedCloudRun(shown []byte) ([]string, error) {
	resources, err := stateResources(shown)
	if err != nil {
		return nil, err
	}
	var addresses []string
	for _, r := range resources {
		var address, kind string
		var tainted bool
		if err := errors.Join(unmarshalField(r, "address", &address), unmarshalField(r, "type", &kind), unmarshalField(r, "tainted", &tainted)); err != nil {
			return nil, errors.Wrap(err, "json.Unmarshal(): tofu show -json of the state, resources")
		}
		if tainted && (kind == cloudRunServiceType || kind == cloudRunJobType) {
			addresses = append(addresses, address)
		}
	}

	return addresses, nil
}

// stateResources are the state's resources, off tofu show -json of it: the root module's,
// then each child module's. An empty state (before the first apply) has no values.
func stateResources(shown []byte) ([]map[string]json.RawMessage, error) {
	var state struct {
		Values map[string]json.RawMessage `json:"values"`
	}
	if err := json.Unmarshal(shown, &state); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): tofu show -json of the state")
	}
	root, ok := state.Values["root_module"]
	if !ok {
		return nil, nil
	}

	return moduleResources(root)
}

// moduleResources are a module's resources and, after them, its child modules'.
func moduleResources(raw json.RawMessage) ([]map[string]json.RawMessage, error) {
	var module map[string]json.RawMessage
	if err := json.Unmarshal(raw, &module); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): tofu show -json of the state, module")
	}
	var resources []map[string]json.RawMessage
	var children []json.RawMessage
	if err := errors.Join(unmarshalField(module, "resources", &resources), unmarshalField(module, "child_modules", &children)); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): tofu show -json of the state, module")
	}
	for _, child := range children {
		more, err := moduleResources(child)
		if err != nil {
			return nil, err
		}
		resources = append(resources, more...)
	}

	return resources, nil
}
