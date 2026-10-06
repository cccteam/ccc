// Package config is the lighthouse configuration (test fixture).
package config

import "time"

// Core is read by every process.
type Core struct {
	ProjectID string `env:"LIGHTHOUSE_PROJECT_ID,required"`
	Data      Data   `env:",prefix=LIGHTHOUSE_"`
}

// Data is read by every process that opens the database.
type Data struct {
	InstanceID   string `env:"LIGHTHOUSE_SPANNER_INSTANCE_ID,default=lighthouse"`
	DatabaseName string `env:"LIGHTHOUSE_SPANNER_DATABASE,required"`
}

// Server is read by the web server only.
type Server struct {
	Port           string        `env:"LIGHTHOUSE_PORT,default=8080"`
	CookieKey      string        `env:"LIGHTHOUSE_COOKIE_KEY"`
	SessionTimeout time.Duration `env:"LIGHTHOUSE_SESSION_TIMEOUT,default=1h"`
	BeaconAPIKey   string        `env:"LIGHTHOUSE_BEACON_API_KEY,required"`
}
