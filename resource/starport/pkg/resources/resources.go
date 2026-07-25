// Package resources provides the resource types for the starport.
package resources

import (
	"github.com/cccteam/ccc/resource"
)

func defaultConfig() resource.Config {
	return resource.Config{
		TrackChanges: false,
	}
}
