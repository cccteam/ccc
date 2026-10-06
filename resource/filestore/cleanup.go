package filestore

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/go-playground/errors/v5"
)

// The orphaned-file cleanup's bounds.
const (
	// DefaultWindow is the age an object must reach before the cleanup may delete it
	// when the application sets none: two days.
	DefaultWindow = 48 * time.Hour
	// MinimumWindow is the shortest window accepted: a day, longer than any request or
	// job, so an object whose row is still being written is never taken for an orphan.
	MinimumWindow = 24 * time.Hour
	// UnclaimedShare is the share of the aged objects that may be unclaimed before the
	// cleanup refuses: above it the live keys were most likely not all read.
	UnclaimedShare = 0.5
	// UnclaimedFloor is the number of unclaimed objects under which the share rule does
	// not apply, so a small application is not refused forever.
	UnclaimedFloor = 100
)

// Cleanup removes a store's orphaned files: the objects no row holds, older than the
// window. It is the safety net behind the release (a deleted row's object is deleted
// when the transaction commits) and the discard (a failed upload's objects are deleted
// before the response), never the mechanism: it catches the objects a crash between
// the stream and the commit, a row deletion policy or a cascade leaves behind. It runs
// as a command of the application's job process, one store per run.
//
// It lists the store's objects older than the window whose names are UUIDs, the keys
// the upload frame mints, and keeps every key the store's @file columns hold, read
// through the holders the generator writes (FileHolders in each resources package)
// with strong reads, every key column once. A computed resource's keys come from
// application code, so ComputedKeys supplies them or the cleanup refuses. What is left
// is deleted.
//
// It refuses to run when the window is under MinimumWindow, when no row holds any key
// in the store, since the holders handed to it would then be the wrong ones, and when
// the unclaimed share of the aged objects is above UnclaimedShare with at least
// UnclaimedFloor unclaimed, since the live keys were then most likely not all read. It
// touches only UUID-shaped names: an object named any other way belongs to its row and
// is never the cleanup's.
type Cleanup struct {
	// Client is the resource client the store is wired on and the holders read through.
	Client resource.Client
	// Store names the store to clean: resource.DefaultStore or a named store's name.
	Store resource.StoreName
	// Holders are every resource recording stored files' keys, from every resources
	// package the application generates: each package's FileHolders(). Holders of other
	// stores are ignored.
	Holders []resource.FileHolder
	// Window is the age an object must reach before it may be deleted; DefaultWindow
	// when zero, refused under MinimumWindow.
	Window time.Duration
	// ComputedKeys supplies the keys a computed resource's rows hold in the store, for
	// each computed holder; nil refuses when any computed holder names the store.
	ComputedKeys func(ctx context.Context, holder resource.FileHolder) ([]string, error)
	// DryRun lists what would be deleted and deletes nothing.
	DryRun bool
	// Now is the clock, time.Now when nil.
	Now func() time.Time
}

// Report is what one run found and did.
type Report struct {
	Store  resource.StoreName
	Window time.Duration
	DryRun bool
	// Live is the number of distinct keys the rows hold in the store.
	Live int
	// Listed is the number of objects in the store; Aged the UUID-named ones older
	// than the window.
	Listed int
	Aged   int
	// Orphans are the aged objects no row holds: deleted, or listed under DryRun.
	Orphans []string
}

// String renders the report in one line for the job's log.
func (r *Report) String() string {
	verb := "deleted"
	if r.DryRun {
		verb = "would delete"
	}

	return fmt.Sprintf("%s: %d objects listed, %d older than %s with a UUID name, %d keys held by rows, %s %d orphaned objects", r.Store, r.Listed, r.Aged, r.Window, r.Live, verb, len(r.Orphans))
}

// Run performs the cleanup and reports what it did; a refusal is an error and deletes
// nothing.
func (c *Cleanup) Run(ctx context.Context) (*Report, error) {
	window := c.Window
	if window == 0 {
		window = DefaultWindow
	}
	if window < MinimumWindow {
		return nil, errors.Newf("the window %s is under the minimum %s; an object younger than that may belong to a row still being written", window, MinimumWindow)
	}
	if c.Client == nil {
		return nil, errors.New("the cleanup needs the resource client the store is wired on")
	}
	store := c.Client.FileStore(c.Store)
	if store == nil {
		return nil, errors.Newf("no file store is wired for %s on the resource client", c.Store)
	}
	lister, ok := store.(Lister)
	if !ok {
		return nil, errors.Newf("the store wired for %s (%T) cannot list its objects; the cleanup runs over the stores this package opens", c.Store, store)
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	report := &Report{Store: c.Store, Window: window, DryRun: c.DryRun}

	live, err := c.liveKeys(ctx)
	if err != nil {
		return nil, err
	}
	report.Live = len(live)
	if len(live) == 0 {
		return nil, errors.Newf("no row holds any key in %s; the cleanup refuses to empty a store whose holders it was not given", c.Store)
	}

	cutoff := now().Add(-window)
	var orphans []string
	for obj, err := range lister.Objects(ctx) {
		if err != nil {
			return nil, errors.Wrap(err, "listing the store")
		}
		report.Listed++
		if !obj.Created.Before(cutoff) || !isUUID(obj.Key) {
			continue
		}
		report.Aged++
		if _, held := live[obj.Key]; !held {
			orphans = append(orphans, obj.Key)
		}
	}
	slices.Sort(orphans)
	report.Orphans = orphans
	if len(orphans) >= UnclaimedFloor && float64(len(orphans))/float64(report.Aged) > UnclaimedShare {
		return nil, errors.Newf("%d of the %d aged objects in %s are held by no row, above the %.0f%% the cleanup accepts; the live keys were most likely not all read, so nothing is deleted", len(orphans), report.Aged, c.Store, UnclaimedShare*100)
	}
	if c.DryRun || len(orphans) == 0 {
		return report, nil
	}
	if err := store.Delete(ctx, orphans); err != nil {
		return nil, errors.Wrap(err, "resource.FileStore.Delete()")
	}

	return report, nil
}

// liveKeys reads every key the holders of the store hold, each column once, through
// the client for a table resource and through ComputedKeys for a computed one.
func (c *Cleanup) liveKeys(ctx context.Context) (map[string]struct{}, error) {
	live := make(map[string]struct{})
	seen := make(map[accesstypes.Resource]bool)
	for _, holder := range c.Holders {
		if !slices.Contains(holder.Stores(), c.Store) || seen[holder.Resource] {
			continue
		}
		seen[holder.Resource] = true
		var (
			keys []string
			err  error
		)
		switch {
		case !holder.Computed:
			keys, err = holder.HeldKeys(ctx, c.Client, c.Store)
		case c.ComputedKeys == nil:
			return nil, errors.Newf("%s is a computed resource holding keys in %s, and the cleanup was given no ComputedKeys to read them with; supply them, or the cleanup refuses", holder.Resource, c.Store)
		default:
			keys, err = c.ComputedKeys(ctx, holder)
		}
		if err != nil {
			return nil, errors.Wrapf(err, "reading the keys %s holds", holder.Resource)
		}
		for _, key := range keys {
			if key != "" {
				live[key] = struct{}{}
			}
		}
	}

	return live, nil
}

// isUUID reports a UUID-shaped key: the names the upload frame mints.
func isUUID(key string) bool {
	_, err := ccc.UUIDFromString(key)

	return err == nil
}
