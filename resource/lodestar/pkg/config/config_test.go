package config

import (
	"strings"
	"testing"

	"github.com/cccteam/logger"
)

// TestCoreConfiguration opens the core level as development does, with no logging
// project, and with a trace sampling word the driver does not know: the exporter the
// level hands out is the driver's console exporter, and the refusal is the driver's,
// since the level reads the sampling through the settings it embeds and nothing else.
//
// Demonstrates: cloud.driver.
func TestCoreConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		sampling string
		wantErr  string
	}{
		{name: "no logging project: the console exporter", sampling: "edge"},
		{name: "a sampling word the driver does not know is refused", sampling: "sometimes", wantErr: "tracer.ParseSampling()"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_SERVICE_NAME", "lodestar")
			t.Setenv("GOOGLE_CLOUD_LOGGING_PROJECT", "")
			t.Setenv("APP_TRACE_SAMPLING", tt.sampling)

			core, err := newCoreConfiguration(t.Context())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("newCoreConfiguration() error = %v, want one naming %s", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("newCoreConfiguration() error = %v", err)
			}
			defer core.Close()

			if _, ok := core.LogExporter().(*logger.ConsoleExporter); !ok {
				t.Errorf("LogExporter() = %T, want *logger.ConsoleExporter", core.LogExporter())
			}
			if got := core.ServiceName(); got != "lodestar" {
				t.Errorf("ServiceName() = %q, want lodestar", got)
			}
		})
	}
}
