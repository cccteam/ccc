package config

import (
	"context"

	"github.com/go-playground/errors/v5"
	"github.com/go-playground/validator/v10"
	"github.com/sethvargo/go-envconfig"
)

// SiteConfiguration is the third level: the served site. A flat application is one site,
// so the level is the one the sites layout declares once and every site reads.
type SiteConfiguration struct {
	*DataConfiguration
	env            *siteConfig
	validator      *validator.Validate
	machinesAPIKey string
}

// NewSiteConfiguration loads every level and constructs the served site's
// dependencies.
func NewSiteConfiguration(ctx context.Context) (*SiteConfiguration, error) {
	data, err := NewDataConfiguration(ctx)
	if err != nil {
		return nil, err
	}

	env := &siteConfig{}
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: env, Lookuper: envconfig.OsLookuper()}); err != nil {
		return nil, errors.Wrap(err, "envconfig.ProcessWith()")
	}

	machinesAPIKey := env.MachinesAPIKey
	if machinesAPIKey == "" {
		// An ephemeral key keeps the machines outlet fail-closed: nothing knows it, so
		// nothing authenticates until APP_MACHINES_API_KEY is configured.
		machinesAPIKey, err = ephemeralKey()
		if err != nil {
			return nil, err
		}
	}

	return &SiteConfiguration{
		DataConfiguration: data,
		env:               env,
		validator:         validator.New(),
		machinesAPIKey:    machinesAPIKey,
	}, nil
}

// Addr returns the TCP address the site listens on, in the form ":port".
func (c *SiteConfiguration) Addr() string {
	return ":" + c.env.Port
}

// Validator returns the request payload validator.
func (c *SiteConfiguration) Validator() *validator.Validate {
	return c.validator
}

// ConsoleDist returns the directory the console's built Angular bundle is served from.
func (c *SiteConfiguration) ConsoleDist() string {
	return c.env.ConsoleDist
}

// PortalDist returns the directory the portal's built Angular bundle is served from.
func (c *SiteConfiguration) PortalDist() string {
	return c.env.PortalDist
}

// MachinesAPIKey returns the bearer key the machines outlet's API-key middleware
// validates machine clients against.
func (c *SiteConfiguration) MachinesAPIKey() string {
	return c.machinesAPIKey
}

// siteConfig holds the environment only the served site reads.
type siteConfig struct {
	// Port is the TCP port the server listens on.
	Port string `env:"PORT,default=8080"`

	// ConsoleDist is the directory holding the console's built Angular bundle.
	ConsoleDist string `env:"APP_CONSOLE_DIST,default=web/dist/console"`

	// PortalDist is the directory holding the portal's built Angular bundle.
	PortalDist string `env:"APP_PORTAL_DIST,default=web/dist/portal"`

	// MachinesAPIKey is the bearer key machine clients present on the machines outlet
	// (/machines/...). Unset, an ephemeral key is generated at startup, which keeps
	// the surface fail-closed but unreachable until a key is configured.
	MachinesAPIKey string `env:"APP_MACHINES_API_KEY"`
}
