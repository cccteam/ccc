package generation

import (
	"encoding/json"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cccteam/ccc/resource"
	"github.com/go-playground/errors/v5"
)

// runReleaseFileGeneration writes the release file (resource.ReleaseFileName) beside the
// served router: every outlet by name, a machine outlet by its kind and a session outlet
// by its oldest answered release as the generator program declared it, so a deploy reads
// from the checkout whether the release needs a maintenance window, and the scheduled
// routes with their schedules, which the application's stack creates a Cloud Scheduler
// job for each of. It is written only with the router, and the stale sweep removes it
// when the router goes.
func (r *resourceGenerator) runReleaseFileGeneration(outlets []routerOutlet) error {
	begin := time.Now()
	data, err := renderReleaseFile(releaseFileOf(outlets, scheduledRoutesOf(r.scheduledMethods)))
	if err != nil {
		return err
	}

	destination := filepath.Join(r.router.Dir(), resource.ReleaseFileName)
	if err := r.output.writeGeneratedFile(destination, data); err != nil {
		return errors.Wrap(err, "generatedOutput.writeGeneratedFile()")
	}
	log.Printf("Generated release file in %s: %s\n", time.Since(begin), destination)

	return nil
}

// releaseFileOf builds the release file from the validated outlet declarations and the
// scheduled routes, the routes in path order.
func releaseFileOf(outlets []routerOutlet, scheduledRoutes []*scheduledRoute) resource.ReleaseFile {
	file := resource.ReleaseFile{Outlets: make(map[string]resource.ReleaseOutlet, len(outlets))}
	for _, o := range outlets {
		if o.apiKey {
			file.Outlets[o.name] = resource.ReleaseOutlet{APIKey: true}

			continue
		}
		file.Outlets[o.name] = resource.ReleaseOutlet{OldestAnswered: o.oldestAnswered}
	}
	for _, route := range scheduledRoutes {
		file.Scheduled = append(file.Scheduled, resource.ScheduledRoute{Path: route.Path, Schedule: route.Cron, TimeZone: route.Zone})
	}
	slices.SortFunc(file.Scheduled, func(a, b resource.ScheduledRoute) int {
		return strings.Compare(a.Path, b.Path)
	})

	return file
}

// renderReleaseFile renders the release file indented, the outlets in name order, with
// a trailing newline: the same declarations render the same bytes, so regenerating
// moves nothing, and a change to one outlet is a one-line diff.
func renderReleaseFile(file resource.ReleaseFile) ([]byte, error) {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, errors.Wrap(err, "json.MarshalIndent()")
	}

	return append(data, '\n'), nil
}
