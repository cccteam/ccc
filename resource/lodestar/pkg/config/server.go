package config

import (
	"context"
	"log"

	"github.com/cccteam/ccc/resource/lodestar/pkg/store"
	"github.com/go-playground/errors/v5"
	"github.com/go-playground/validator/v10"
	"github.com/sethvargo/go-envconfig"
)

// ServerConfiguration is the third level: the served application.
type ServerConfiguration struct {
	*DataConfiguration
	env          *serverConfig
	validator    *validator.Validate
	droidsAPIKey string
	documents    *store.DirStore
}

// NewServerConfiguration loads every level and constructs the served application's
// dependencies.
func NewServerConfiguration(ctx context.Context) (*ServerConfiguration, error) {
	data, err := NewDataConfiguration(ctx)
	if err != nil {
		return nil, err
	}

	env := &serverConfig{}
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: env, Lookuper: envconfig.OsLookuper()}); err != nil {
		return nil, errors.Wrap(err, "envconfig.ProcessWith()")
	}

	droidsAPIKey := env.DroidsAPIKey
	if droidsAPIKey == "" {
		// An ephemeral key keeps the droids outlet fail-closed: nothing knows it, so
		// nothing authenticates until APP_DROIDS_API_KEY is configured.
		droidsAPIKey, err = ephemeralKey()
		if err != nil {
			return nil, err
		}
	}

	documents, err := store.NewDirStore(env.UploadDir)
	if err != nil {
		return nil, errors.Wrap(err, "store.NewDirStore()")
	}

	return &ServerConfiguration{
		DataConfiguration: data,
		env:               env,
		validator:         validator.New(),
		droidsAPIKey:      droidsAPIKey,
		documents:         documents,
	}, nil
}

// Close releases the level's clients, then the levels below it.
func (c *ServerConfiguration) Close() {
	if err := c.documents.Close(); err != nil {
		log.Print(errors.Wrap(err, "store.DirStore.Close()"))
	}
	c.DataConfiguration.Close()
}

// Documents returns the store the upload frame streams mission documents into and the
// document route reads them back from.
func (c *ServerConfiguration) Documents() *store.DirStore {
	return c.documents
}

// Addr returns the TCP address the server listens on, in the form ":port".
func (c *ServerConfiguration) Addr() string {
	return ":" + c.env.Port
}

// Validator returns the request payload validator.
func (c *ServerConfiguration) Validator() *validator.Validate {
	return c.validator
}

// ConsoleDist returns the directory the console's built Angular bundle is served from.
func (c *ServerConfiguration) ConsoleDist() string {
	return c.env.ConsoleDist
}

// PortalDist returns the directory the portal's built Angular bundle is served from.
func (c *ServerConfiguration) PortalDist() string {
	return c.env.PortalDist
}

// DroidsAPIKey returns the bearer key the droids outlet's API-key middleware validates
// droid clients against.
func (c *ServerConfiguration) DroidsAPIKey() string {
	return c.droidsAPIKey
}

// serverConfig holds the environment only the served application reads.
type serverConfig struct {
	// Port is the TCP port the server listens on.
	Port string `env:"PORT,default=8080"`

	// ConsoleDist is the directory holding the console's built Angular bundle.
	ConsoleDist string `env:"APP_CONSOLE_DIST,default=web/dist/console"`

	// PortalDist is the directory holding the portal's built Angular bundle.
	PortalDist string `env:"APP_PORTAL_DIST,default=web/dist/portal"`

	// DroidsAPIKey is the bearer key droids present on the droids outlet (/droids/...).
	// Unset, an ephemeral key is generated at startup, which keeps the surface
	// fail-closed but unreachable until a key is configured.
	DroidsAPIKey string `env:"APP_DROIDS_API_KEY"`

	// UploadDir is the directory the document store keeps mission documents in; uploads
	// stream into its pending/ subdirectory until their transaction commits.
	UploadDir string `env:"APP_UPLOAD_DIR,default=uploads"`
}
