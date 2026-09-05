// Package resources provides the resource types for outlets. Each struct annotated with
// @resource describes one table of schema/migrations; the generator derives the
// handlers, routes, permission collection, and TypeScript client from them.
package resources

import "github.com/cccteam/ccc/resource"

func defaultConfig() resource.Config {
	return resource.Config{
		TrackChanges: false,
	}
}
