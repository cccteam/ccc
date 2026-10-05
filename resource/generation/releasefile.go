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
// from the checkout whether the release needs a maintenance window; the scheduled
// routes with their schedules, which the application's stack creates a Cloud Scheduler
// job for each of; and the file routes, each @upload method's and each stored file's
// (fileRoutes, by outlet name), which the stack puts ahead of its web application
// firewall's rules. It is written only with the router, and the stale sweep removes it
// when the router goes.
func (r *resourceGenerator) runReleaseFileGeneration(outlets []routerOutlet, fileRoutes map[string][]*generatedRoute) error {
	begin := time.Now()
	data, err := renderReleaseFile(releaseFileOf(outlets, scheduledRoutesOf(r.scheduledMethods), r.fileRoutesOf(outlets, fileRoutes)))
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

// fileRoutesOf lists the routes that carry a file rather than JSON, in path order: each
// served @upload method's route on every outlet it is on, and each outlet's stored-file
// routes (fileRoutes, by outlet name), each naming its declaration.
func (r *resourceGenerator) fileRoutesOf(outlets []routerOutlet, fileRoutes map[string][]*generatedRoute) []resource.FileRoute {
	var routes []resource.FileRoute
	for _, o := range outlets {
		for _, method := range r.rpcMethods {
			if method.Upload == nil || method.SuppressHandler || !method.OnOutlet(o.name) {
				continue
			}
			route := r.rpcRoute(method, o.prefix)
			routes = append(routes, resource.FileRoute{Kind: resource.FileRouteUpload, Method: route.Method, Path: route.Path, Source: method.Name()})
		}
		for _, route := range fileRoutes[o.name] {
			routes = append(routes, resource.FileRoute{Kind: resource.FileRouteStored, Method: route.Method, Path: route.Path, Source: route.Source})
		}
	}
	slices.SortFunc(routes, func(a, b resource.FileRoute) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}

		return strings.Compare(a.Method, b.Method)
	})

	return routes
}

// releaseFileOf builds the release file from the validated outlet declarations, the
// scheduled routes and the file routes, the routes in path order.
func releaseFileOf(outlets []routerOutlet, scheduledRoutes []*scheduledRoute, fileRoutes []resource.FileRoute) resource.ReleaseFile {
	file := resource.ReleaseFile{Outlets: make(map[string]resource.ReleaseOutlet, len(outlets)), FileRoutes: fileRoutes}
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
