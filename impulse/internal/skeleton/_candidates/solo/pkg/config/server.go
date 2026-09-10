package config

import (
	"context"

	"github.com/go-playground/errors/v5"
	"github.com/go-playground/validator/v10"
	"github.com/sethvargo/go-envconfig"
)

// ServerConfiguration is the third level: the served application.
type ServerConfiguration struct {
	*DataConfiguration
	env       *serverConfig
	validator *validator.Validate
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

	return &ServerConfiguration{
		DataConfiguration: data,
		env:               env,
		validator:         validator.New(),
	}, nil
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

// serverConfig holds the environment only the served application reads.
type serverConfig struct {
	// Port is the TCP port the server listens on.
	Port string `env:"PORT,default=8080"`

	// ConsoleDist is the directory holding the console's built Angular bundle.
	ConsoleDist string `env:"APP_CONSOLE_DIST,default=web/dist/console"`
}
