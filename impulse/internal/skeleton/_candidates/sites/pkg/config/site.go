package config

import (
	"context"

	"github.com/go-playground/errors/v5"
	"github.com/go-playground/validator/v10"
	"github.com/sethvargo/go-envconfig"
)

// SiteConfiguration is the third level: one served site. Every site's process reads
// the same variables — its port and its built bundle — so the level is declared once
// and each site's deployment supplies its own values.
type SiteConfiguration struct {
	*DataConfiguration
	env       *siteConfig
	validator *validator.Validate
}

// NewSiteConfiguration loads every level and constructs a served site's dependencies.
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

// Dist returns the directory the site's built Angular bundle is served from.
func (c *SiteConfiguration) Dist() string {
	return c.env.Dist
}

// siteConfig holds the environment only a served site reads. The values differ per
// site, so they come from each site's process environment (the Procfile in
// development, the site's deployment in production), never from the shared template.
type siteConfig struct {
	// Port is the TCP port the site listens on.
	Port string `env:"PORT,default=8080"`

	// Dist is the directory holding the site's built Angular bundle.
	Dist string `env:"APP_DIST,required"`
}
