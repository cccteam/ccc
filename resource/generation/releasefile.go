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
// job for each of; the file routes, each @upload method's and each stored file's
// (fileRoutes, by outlet name), which the stack puts ahead of its web application
// firewall's rules; and the surfaces that declare a request log word or a trace setting
// (the default, the outlets, the hand-mounted prefixes, and the routes in wordedRoutes,
// by outlet name), from which the stack renders the cloud's own request-log exclusion.
// It is written only with the router, and the stale sweep removes it when the router
// goes.
func (r *resourceGenerator) runReleaseFileGeneration(outlets []routerOutlet, fileRoutes, wordedRoutes map[string][]*generatedRoute) error {
	begin := time.Now()
	scheduledRoutes := scheduledRoutesOf(r.scheduledMethods)
	file := releaseFileOf(outlets, scheduledRoutes, r.fileRoutesOf(outlets, fileRoutes), r.surfacesOf(outlets, wordedRoutes, scheduledRoutes))
	data, err := renderReleaseFile(&file)
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

// surfacesOf lists the declared surfaces for the release file, in prefix order: the
// application default at /, each outlet with a word or a setting at its prefix, each
// hand-mounted prefix, each worded route at the path it is mounted at, and each
// scheduled route with a word. Each carries its kind as the router matches it: the
// default, an outlet and a hand-mounted prefix are prefixes, a route is a route.
func (r *resourceGenerator) surfacesOf(outlets []routerOutlet, wordedRoutes map[string][]*generatedRoute, scheduledRoutes []*scheduledRoute) []resource.Surface {
	var surfaces []surface
	if r.requestLog.Declared() {
		surfaces = append(surfaces, surface{Prefix: "/", Kind: resource.SurfacePrefix, RequestLog: r.requestLog})
	}
	for _, o := range outlets {
		if o.requestLog.Declared() || o.traces.Declared() {
			surfaces = append(surfaces, surface{Prefix: "/" + o.prefix + "/", Kind: resource.SurfacePrefix, RequestLog: o.requestLog, Traces: o.traces})
		}
		for _, route := range wordedRoutes[o.name] {
			surfaces = append(surfaces, surface{Prefix: route.Path, Kind: resource.SurfaceRoute, RequestLog: route.RequestLog, Traces: route.Traces})
		}
	}
	for _, mounted := range r.mountedRoutes {
		surfaces = append(surfaces, surface{Prefix: mounted.prefix, Kind: resource.SurfacePrefix, RequestLog: mounted.requestLog, Traces: mounted.traces})
	}
	for _, route := range scheduledRoutes {
		if route.RequestLog.Declared() {
			surfaces = append(surfaces, surface{Prefix: route.Path, Kind: resource.SurfaceRoute, RequestLog: route.RequestLog})
		}
	}
	slices.SortFunc(surfaces, func(a, b surface) int {
		return strings.Compare(a.Prefix, b.Prefix)
	})
	released := make([]resource.Surface, 0, len(surfaces))
	for _, s := range surfaces {
		released = append(released, s.releaseSurface())
	}

	return released
}

// releaseFileOf builds the release file from the validated outlet declarations, the
// scheduled routes, the file routes and the surfaces, the routes in path order.
func releaseFileOf(outlets []routerOutlet, scheduledRoutes []*scheduledRoute, fileRoutes []resource.FileRoute, surfaces []resource.Surface) resource.ReleaseFile {
	file := resource.ReleaseFile{Outlets: make(map[string]resource.ReleaseOutlet, len(outlets)), FileRoutes: fileRoutes, Surfaces: surfaces}
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
func renderReleaseFile(file *resource.ReleaseFile) ([]byte, error) {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, errors.Wrap(err, "json.MarshalIndent()")
	}

	return append(data, '\n'), nil
}
