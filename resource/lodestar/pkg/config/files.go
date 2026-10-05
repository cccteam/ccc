package config

import (
	"context"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/filestore"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/go-playground/errors/v5"
	"github.com/sethvargo/go-envconfig"
)

// FileStoreSettings names the application's file stores, one URL each: the default
// store, the refit photos', and the Documents store, the mission documents'. Each is a
// directory in development (file://uploads, file://uploads-documents) and a bucket on
// Cloud Run (gs://<bucket>). An unset variable leaves its store unopened.
type FileStoreSettings struct {
	Default   string `env:"APP_FILE_STORE"`
	Documents string `env:"APP_FILE_STORE_DOCUMENTS"`
}

// LoadFileStoreSettings reads the stores' URLs from the environment without opening
// anything, for the bootstrap, which empties the stores on a reset.
func LoadFileStoreSettings(ctx context.Context) (FileStoreSettings, error) {
	var settings FileStoreSettings
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: &settings, Lookuper: envconfig.OsLookuper()}); err != nil {
		return FileStoreSettings{}, errors.Wrap(err, "envconfig.ProcessWith()")
	}

	return settings, nil
}

// openFileStores opens the stores whose URLs are set: the default store and the
// Documents store, each through filestore.Open, which checks it and refuses a bad URL
// or a missing bucket.
func openFileStores(ctx context.Context, settings FileStoreSettings) (files, documents filestore.Store, err error) {
	if settings.Default != "" {
		files, err = filestore.Open(ctx, settings.Default)
		if err != nil {
			return nil, nil, errors.Wrap(err, "filestore.Open(APP_FILE_STORE)")
		}
	}
	if settings.Documents != "" {
		documents, err = filestore.Open(ctx, settings.Documents)
		if err != nil {
			if files != nil {
				_ = files.Close()
			}

			return nil, nil, errors.Wrap(err, "filestore.Open(APP_FILE_STORE_DOCUMENTS)")
		}
	}

	return files, documents, nil
}

// fileStoreOptions wires the opened stores on the resource client: the default store
// as the default, the Documents store under the resources.Documents type.
func fileStoreOptions(files, documents filestore.Store) []resource.ClientOption {
	var opts []resource.ClientOption
	if files != nil {
		opts = append(opts, resource.WithFileStore(files))
	}
	if documents != nil {
		opts = append(opts, resource.WithNamedFileStore[resources.Documents](documents))
	}

	return opts
}
