// Package jobs holds the work the application's job process runs on a schedule, apart
// from the served site: today the orphaned-file cleanup over the application's file
// stores. cmd/jobs is the command around it.
package jobs

import (
	"context"
	"slices"
	"time"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/filestore"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/go-playground/errors/v5"
)

// CleanupOptions says how the cleanup runs.
type CleanupOptions struct {
	// Stores names the stores to clean; empty cleans every store the application's
	// holders name, the default store and the Documents store.
	Stores []resource.StoreName
	// Window is the age an object must reach before it may be deleted; zero is the
	// cleanup's default of two days, and under a day is refused.
	Window time.Duration
	// DryRun lists what would be deleted and deletes nothing.
	DryRun bool
}

// CleanupFiles runs the orphaned-file cleanup over the application's stores, one store
// at a time, through the holders the generator wrote (resources.FileHolders): every
// resource recording stored files' keys, with each key column's store. The resource
// client carries the stores, so the cleanup refuses a store that is not wired on it.
// It returns one report per store, and stops at the first store that refuses.
//
// Demonstrates: filestore.cleanup.
func CleanupFiles(ctx context.Context, client resource.Client, opts CleanupOptions) ([]*filestore.Report, error) {
	holders := resources.FileHolders()
	stores := opts.Stores
	if len(stores) == 0 {
		stores = storesOf(holders)
	}
	reports := make([]*filestore.Report, 0, len(stores))
	for _, store := range stores {
		cleanup := filestore.Cleanup{
			Client:  client,
			Store:   store,
			Holders: holders,
			Window:  opts.Window,
			DryRun:  opts.DryRun,
		}
		report, err := cleanup.Run(ctx)
		if err != nil {
			return reports, errors.Wrapf(err, "filestore.Cleanup.Run(%s)", store)
		}
		reports = append(reports, report)
	}

	return reports, nil
}

// storesOf lists the distinct stores the holders name, in the order they first appear.
func storesOf(holders []resource.FileHolder) []resource.StoreName {
	var stores []resource.StoreName
	for _, holder := range holders {
		for _, store := range holder.Stores() {
			if !slices.Contains(stores, store) {
				stores = append(stores, store)
			}
		}
	}

	return stores
}
