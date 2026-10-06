package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/cccteam/ccc/resource/filestore"
	"github.com/cccteam/ccc/resource/lodestar/pkg/config"
	"github.com/go-playground/errors/v5"
)

// emptyFileStores removes every object from the file stores that are directories
// (file://), so the seeded database starts with no file that no row holds: the bootstrap
// runs before any row exists, or after the data was emptied, and an object left behind
// would otherwise sit in the development directories until the orphaned-file cleanup's
// window passed. A bucket store is left alone: the bootstrap targets development, and a
// bucket's objects are the cleanup's to judge. Each store opens through filestore.Open,
// as the data level opens it.
func emptyFileStores(ctx context.Context) error {
	settings, err := config.LoadFileStoreSettings(ctx)
	if err != nil {
		return errors.Wrap(err, "config.LoadFileStoreSettings()")
	}
	for _, rawURL := range []string{settings.Default, settings.Documents} {
		if !strings.HasPrefix(rawURL, filestore.SchemeDir+"://") {
			continue
		}
		if err := emptyFileStore(ctx, rawURL); err != nil {
			return err
		}
	}

	return nil
}

// emptyFileStore deletes every object the store at rawURL holds.
func emptyFileStore(ctx context.Context, rawURL string) error {
	store, err := filestore.Open(ctx, rawURL)
	if err != nil {
		return errors.Wrap(err, "filestore.Open()")
	}
	defer store.Close()

	var keys []string
	for obj, err := range store.Objects(ctx) {
		if err != nil {
			return errors.Wrap(err, "filestore.Store.Objects()")
		}
		keys = append(keys, obj.Key)
	}
	if err := store.Delete(ctx, keys); err != nil {
		return errors.Wrap(err, "filestore.Store.Delete()")
	}
	fmt.Printf("Emptied the file store %s (%d objects)\n", rawURL, len(keys))

	return nil
}
