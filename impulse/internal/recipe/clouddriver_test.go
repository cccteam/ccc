package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/cccteam/ccc/impulse/app"
)

// configSource is a config package in one of the old forms: the imports the form needs,
// the region, and the core struct declaring the logging project itself.
func configSource(region, field string, wired bool) string {
	imports := "\t\"context\"\n\t\"log\"\n"
	if wired {
		imports += "\t\"time\"\n"
	}
	imports += "\n\t\"cloud.google.com/go/logging\"\n"
	if wired {
		imports += "\t\"github.com/cccteam/ccc/tracer\"\n"
	}
	imports += "\t\"github.com/cccteam/logger\"\n\t\"github.com/go-playground/errors/v5\"\n\t\"github.com/sethvargo/go-envconfig\"\n"
	src := "// Package config loads the configuration.\npackage config\n\nimport (\n" + imports + ")\n\n"
	if wired {
		src += traceFlushTimeoutConst
	}
	src += region + "\n// coreConfig holds the environment every process reads.\ntype coreConfig struct {\n\t// ServiceName names the process in logs.\n\tServiceName string `env:\"APP_SERVICE_NAME,required\"`\n\n" + field + "}\n"

	return src
}

const appSource = `// Package app is the application.
package app

import (
	"net/http"

	"github.com/cccteam/logger"
)

// App is the application.
type App struct {
	logExporter logger.Exporter
}

` + loggerMiddlewareMethod + `
// SecurityHeaders sets the headers.
func (a *App) SecurityHeaders(next http.Handler) http.Handler {
	return next
}
`

const mainSource = `// main serves the application.
package main

import (
	"context"
	"log"
	"net/http"

	"example.com/acme/beacon/app"
	"example.com/acme/beacon/pkg/config"
	"example.com/acme/beacon/pkg/router"
	"github.com/cccteam/ccc/resource/server"
	"github.com/cccteam/ccc/tracer"
	"github.com/go-playground/errors/v5"
)

func main() {
	if err := Main(); err != nil {
		log.Fatal(err)
	}
}

func Main() error {
	ctx := context.Background()
	conf, err := config.NewSiteConfiguration(ctx)
	if err != nil {
		return errors.Wrap(err, "config.NewSiteConfiguration()")
	}
	defer conf.Close()

	a := app.New(conf)
` + tracingHookStart + `
		return errors.Wrap(err, "server exited unexpectedly")
	}

	return nil
}
`

// at names the line of a marker in a source, as Detect reports it.
func at(rel, src, marker string) string {
	i := strings.Index(src, marker)
	if i < 0 {
		panic("marker not in source: " + marker)
	}

	return fmt.Sprintf("%s:%d: ", rel, 1+strings.Count(src[:i], "\n"))
}

// writeApp lays out an application with the files given and discovers it.
func writeApp(t *testing.T, files map[string]string) *app.App {
	t.Helper()

	root := t.TempDir()
	files["go.mod"] = "module example.com/acme/beacon\n\ngo 1.26.6\n"
	for rel, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := app.Discover(root)
	if err != nil {
		t.Fatalf("app.Discover() error = %v", err)
	}

	return a
}

// TestCloudDriver detects both old forms, rewrites them onto the driver, is idle on the
// result, and reports a form it does not know instead of touching it.
func TestCloudDriver(t *testing.T) {
	t.Parallel()

	before := configSource(configRegions[0], loggingProjectField, false)
	wired := configSource(configRegions[1], loggingProjectField, true)
	unknown := strings.Replace(before, "conf.loggingClient = client", "conf.loggingClient = client // kept by hand", 1)
	moved := configSource(configRegionNew, cloudSettingsField, false)
	const (
		opensClient    = "the core configuration opens the Cloud Logging client itself"
		buildsProvider = "the core configuration builds the trace provider itself"
		declaresProj   = "the core configuration declares the logging project itself"
		buildsLogger   = "the App builds the request logger; the generated router does now"
		passesTracing  = "main passes the tracing handler through the outermost hook; the generated router installs it"
		configDid      = configFile + ": the core configuration embeds gcp.Settings and opens the cloud driver, which builds the log exporter and the trace provider and closes them"
		appDid         = appFile + ": LogExporter replaces LoggerMiddleware; the generated router builds the request logger from it"
		mainDid        = mainFile + ": the tracing handler leaves the router's hooks; the generated router installs it"
	)
	tests := []struct {
		name       string
		files      map[string]string
		wantDetect []string
		wantDid    []string
		wantSkip   []string
		// wantIn and wantOut are texts the rewritten files hold and lack, by file.
		wantIn  map[string][]string
		wantOut map[string][]string
	}{
		{
			name:  "the skeleton before tracing was wired",
			files: map[string]string{configFile: before, appFile: appSource},
			wantDetect: []string{
				at(configFile, before, "logging.NewClient(") + opensClient,
				at(configFile, before, "LoggingProjectID string") + declaresProj,
				at(appFile, appSource, "func (a *App) LoggerMiddleware()") + buildsLogger,
			},
			wantDid: []string{configDid, appDid},
			wantIn: map[string][]string{
				configFile: {"\t\"github.com/cccteam/ccc/cloud/gcp\"\n", "gcp.Open(ctx, env.Settings, env.ServiceName)", "\tgcp.Settings\n", "return c.cloud.LogExporter"},
				appFile:    {"func (a *App) LogExporter() logger.Exporter", "\t\"net/http\"\n"},
			},
			wantOut: map[string][]string{
				configFile: {"logging.", "cloud.google.com/go/logging", "LoggingProjectID"},
				appFile:    {"LoggerMiddleware"},
			},
		},
		{
			name:  "the skeleton with tracing wired by hand",
			files: map[string]string{configFile: wired, appFile: appSource, mainFile: mainSource},
			wantDetect: []string{
				at(configFile, wired, "logging.NewClient(") + opensClient,
				at(configFile, wired, "tracer.NewGoogleCloudTracerProvider(") + buildsProvider,
				at(configFile, wired, "LoggingProjectID string") + declaresProj,
				at(appFile, appSource, "func (a *App) LoggerMiddleware()") + buildsLogger,
				at(mainFile, mainSource, "tracer.NewGoogleCloudHandler()") + passesTracing,
			},
			wantDid: []string{configDid, appDid, mainDid},
			wantIn: map[string][]string{
				configFile: {"gcp.Open(ctx, env.Settings, env.ServiceName)", "\tgcp.Settings\n"},
				mainFile:   {"router.New(a, router.Hooks{})"},
			},
			wantOut: map[string][]string{
				configFile: {"logging.", "tracer.", "time.", "traceFlushTimeout"},
				mainFile:   {"tracer", "net/http", "hooks :="},
			},
		},
		{
			name:  "a configuration the recipe does not know is reported, not touched",
			files: map[string]string{configFile: unknown},
			wantDetect: []string{
				at(configFile, unknown, "logging.NewClient(") + opensClient,
				at(configFile, unknown, "LoggingProjectID string") + declaresProj,
			},
			wantSkip: []string{
				configFile + ": the core configuration is not in a form the recipe knows, so it was not rewritten; embed gcp.Settings in coreConfig, build the level with gcp.Open (its LogExporter and Close), and drop the logging client and the trace provider",
			},
			wantIn: map[string][]string{configFile: {"conf.loggingClient = client // kept by hand", "LoggingProjectID"}},
		},
		{
			name:  "an application already on the driver is left alone",
			files: map[string]string{configFile: moved},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := writeApp(t, tt.files)
			r := CloudDriver{}
			found, err := r.Detect(t.Context(), a)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDetect, found, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Detect() mismatch (-want +got):\n%s", diff)
			}
			ch, err := r.Apply(t.Context(), a, nil)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDid, ch.Did, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Did mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantSkip, ch.Skipped, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Skipped mismatch (-want +got):\n%s", diff)
			}
			for rel, texts := range tt.wantIn {
				data, err := os.ReadFile(a.Abs(rel))
				if err != nil {
					t.Fatal(err)
				}
				for _, text := range texts {
					if !strings.Contains(string(data), text) {
						t.Errorf("%s lacks %q after Apply:\n%s", rel, text, data)
					}
				}
			}
			for rel, texts := range tt.wantOut {
				data, err := os.ReadFile(a.Abs(rel))
				if err != nil {
					t.Fatal(err)
				}
				for _, text := range texts {
					if strings.Contains(string(data), text) {
						t.Errorf("%s still holds %q after Apply:\n%s", rel, text, data)
					}
				}
			}
			if len(tt.wantSkip) > 0 {
				return
			}
			// Running again finds nothing: the recipe is safe to run twice.
			again, err := r.Detect(t.Context(), a)
			if err != nil {
				t.Fatalf("Detect() after Apply error = %v", err)
			}
			if len(again) > 0 {
				t.Errorf("Detect() after Apply = %v, want none", again)
			}
		})
	}
}
