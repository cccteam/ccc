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
	env       *siteConfig
	validator *validator.Validate
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

	return &SiteConfiguration{
		DataConfiguration: data,
		env:               env,
		validator:         validator.New(),
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

// siteConfig holds the environment only the served site reads.
type siteConfig struct {
	// Port is the TCP port the server listens on.
	Port string `env:"PORT,default=8080"`

	// ConsoleDist is the directory holding the console's built Angular bundle.
	ConsoleDist string `env:"APP_CONSOLE_DIST,default=web/dist/console"`
}
