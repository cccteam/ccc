// Package recipe holds the ledger's recipes: the code changes a step needs of an
// application at the step before it (ledger.Recipe). A recipe detects the old form and
// rewrites only what it recognizes, so it is safe to run twice; what it does not
// recognize it reports, for the agent.
package recipe

import (
	"bytes"
	"context"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/transition"
)

// The files the cloud-driver recipe rewrites.
const (
	configFile = "pkg/config/config.go"
	appFile    = "app/app.go"
	mainFile   = "main.go"
)

// The imports the rewrite adds and drops.
const (
	gcpImport     = "github.com/cccteam/ccc/cloud/gcp"
	loggerImport  = "github.com/cccteam/logger"
	loggingImport = "cloud.google.com/go/logging"
	tracerImport  = "github.com/cccteam/ccc/tracer"
	timeImport    = "time"
	httpImport    = "net/http"
)

// skippedDirs are the directories the walk for main files leaves alone.
var skippedDirs = map[string]bool{".git": true, "node_modules": true, "dist": true, "web": true}

// CloudDriver moves an application's logging and tracing construction from its config
// package into the Google Cloud driver (cloud/gcp): the core configuration embeds
// gcp.Settings and calls gcp.Open, the App provides LogExporter in place of
// LoggerMiddleware (the generated router builds the request logger and installs tracing
// itself), and main no longer passes the tracing handler through the outermost hook.
type CloudDriver struct{}

// Name is the recipe's short name.
func (CloudDriver) Name() string { return "cloud-driver" }

// Meaning says what the change means in this framework.
func (CloudDriver) Meaning() string {
	return "Logs and traces are built by the cloud driver (cloud/gcp): the configuration embeds gcp.Settings and calls gcp.Open, and the generated router installs tracing and the request logger from the App's LogExporter. Nothing in the application names a logging client or a trace provider any more; moving to another cloud swaps the driver import and the embedded settings."
}

// marker is one sign of the old form in a file.
type marker struct {
	text string
	what string
}

// configMarkers are the signs of the old form in the config package.
var configMarkers = []marker{
	{text: "logging.NewClient(", what: "the core configuration opens the Cloud Logging client itself"},
	{text: "tracer.NewGoogleCloudTracerProvider(", what: "the core configuration builds the trace provider itself"},
	{text: "LoggingProjectID string `env:\"GOOGLE_CLOUD_LOGGING_PROJECT\"`", what: "the core configuration declares the logging project itself"},
}

// appMarker is the sign of the old form in the App.
var appMarker = marker{text: "func (a *App) LoggerMiddleware()", what: "the App builds the request logger; the generated router does now"}

// mainMarker is the sign of the old form in a main.
var mainMarker = marker{text: "tracer.NewGoogleCloudHandler()", what: "main passes the tracing handler through the outermost hook; the generated router installs it"}

// stackObligation is what the configuration change leaves to the agent: impulse does not
// render the application's stack, and the embedded settings add a variable to it.
const stackObligation = "infrastructure: the configuration's variables changed (gcp.Settings adds APP_TRACE_SAMPLING), and impulse does not render the stack; run the application's bedrock render and check, and commit the stack it writes"

// The driver's variables in the development environment template: the template that
// documents the logging project and not the sampling is in the old form.
const (
	loggingProjectVariable = "GOOGLE_CLOUD_LOGGING_PROJECT"
	traceSamplingVariable  = "APP_TRACE_SAMPLING"
	envTemplateWhat        = "the development template does not document APP_TRACE_SAMPLING, the driver's trace sampling"
)

// Detect names what in the application is in the old form.
func (r CloudDriver) Detect(_ context.Context, a *app.App) ([]string, error) {
	var found []string
	add := func(rel string, data []byte, markers ...marker) {
		for _, m := range markers {
			if i := bytes.Index(data, []byte(m.text)); i >= 0 {
				found = append(found, fmt.Sprintf("%s:%d: %s", rel, 1+strings.Count(string(data[:i]), "\n"), m.what))
			}
		}
	}
	for _, f := range []struct {
		rel     string
		markers []marker
	}{{configFile, configMarkers}, {appFile, []marker{appMarker}}} {
		data, err := os.ReadFile(a.Abs(f.rel))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}

			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		add(f.rel, data, f.markers...)
	}
	if a.EnvTemplate != "" {
		data, err := os.ReadFile(a.Abs(a.EnvTemplate))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		if !bytes.Contains(data, []byte(traceSamplingVariable)) {
			line := 1
			if i := bytes.Index(data, []byte(loggingProjectVariable)); i >= 0 {
				line += bytes.Count(data[:i], []byte("\n"))
			}
			found = append(found, fmt.Sprintf("%s:%d: %s", a.EnvTemplate, line, envTemplateWhat))
		}
	}
	mains, err := r.mains(a)
	if err != nil {
		return nil, err
	}
	for _, rel := range mains {
		data, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		add(rel, data, mainMarker)
	}

	return found, nil
}

// Apply rewrites the files whose old form it recognizes and reports the rest.
func (r CloudDriver) Apply(_ context.Context, a *app.App, _ check.Execer) (*transition.Change, error) {
	ch := &transition.Change{}
	if err := r.rewriteConfig(a, ch); err != nil {
		return nil, err
	}
	if err := r.rewriteApp(a, ch); err != nil {
		return nil, err
	}
	if err := r.rewriteEnvTemplate(a, ch); err != nil {
		return nil, err
	}
	mains, err := r.mains(a)
	if err != nil {
		return nil, err
	}
	for _, rel := range mains {
		if err := r.rewriteMain(a, rel, ch); err != nil {
			return nil, err
		}
	}

	return ch, nil
}

// rewriteConfig moves the construction into the driver: the region from the struct to
// LogExporter, the declared logging project, the imports.
func (CloudDriver) rewriteConfig(a *app.App, ch *transition.Change) error {
	src, mode, ok, err := read(a, configFile)
	if err != nil || !ok {
		return err
	}
	if !strings.Contains(src, "logging.NewClient(") && !strings.Contains(src, loggingProjectField) {
		return nil
	}
	edited, found := src, false
	for _, old := range configRegions {
		if strings.Contains(edited, old) {
			edited = strings.Replace(edited, old, configRegionNew, 1)
			found = true

			break
		}
	}
	if !found {
		ch.Skipped = append(ch.Skipped, configFile+": the core configuration is not in a form the recipe knows, so it was not rewritten; embed gcp.Settings in coreConfig, build the level with gcp.Open (its LogExporter and Close), and drop the logging client and the trace provider")

		return nil
	}
	edited = strings.Replace(edited, traceFlushTimeoutConst, "", 1)
	if strings.Contains(edited, loggingProjectField) {
		edited = strings.Replace(edited, loggingProjectField, cloudSettingsField, 1)
	} else {
		ch.Skipped = append(ch.Skipped, configFile+": the logging project is not declared as the skeleton declares it, so coreConfig was not changed; replace the GOOGLE_CLOUD_LOGGING_PROJECT field with an embedded gcp.Settings")
	}
	edited = addImport(edited, gcpImport, loggerImport)
	edited = dropUnusedImports(edited, loggingImport, tracerImport, timeImport)
	if err := write(a, configFile, edited, mode); err != nil {
		return err
	}
	ch.Did = append(ch.Did, configFile+": the core configuration embeds gcp.Settings and opens the cloud driver, which builds the log exporter and the trace provider and closes them")
	ch.Skipped = append(ch.Skipped, stackObligation)

	return nil
}

// rewriteApp replaces LoggerMiddleware with LogExporter.
func (CloudDriver) rewriteApp(a *app.App, ch *transition.Change) error {
	src, mode, ok, err := read(a, appFile)
	if err != nil || !ok {
		return err
	}
	if !strings.Contains(src, appMarker.text) {
		return nil
	}
	if !strings.Contains(src, loggerMiddlewareMethod) {
		ch.Skipped = append(ch.Skipped, appFile+": LoggerMiddleware is not the skeleton's, so it was not replaced; replace it with LogExporter() logger.Exporter returning the exporter")

		return nil
	}
	edited := strings.Replace(src, loggerMiddlewareMethod, logExporterMethod, 1)
	edited = dropUnusedImports(edited, httpImport)
	if err := write(a, appFile, edited, mode); err != nil {
		return err
	}
	ch.Did = append(ch.Did, appFile+": LogExporter replaces LoggerMiddleware; the generated router builds the request logger from it")

	return nil
}

// rewriteEnvTemplate documents the trace sampling beside the logging project in the
// development environment template.
func (CloudDriver) rewriteEnvTemplate(a *app.App, ch *transition.Change) error {
	if a.EnvTemplate == "" {
		return nil
	}
	src, mode, ok, err := read(a, a.EnvTemplate)
	if err != nil || !ok || strings.Contains(src, traceSamplingVariable) {
		return err
	}
	if !strings.Contains(src, loggingProjectLines) {
		ch.Skipped = append(ch.Skipped, a.EnvTemplate+": the logging project is not documented as the skeleton documents it, so APP_TRACE_SAMPLING was not added; document it beside GOOGLE_CLOUD_LOGGING_PROJECT (all exports every request's spans, edge the ones the caller sampled)")

		return nil
	}
	edited := strings.Replace(src, loggingProjectLines, loggingProjectLines+traceSamplingLines, 1)
	if err := os.WriteFile(a.Abs(a.EnvTemplate), []byte(edited), mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.Did = append(ch.Did, a.EnvTemplate+": APP_TRACE_SAMPLING, the driver's trace sampling, is documented beside the logging project")

	return nil
}

// rewriteMain drops the hand-wired tracing hook.
func (CloudDriver) rewriteMain(a *app.App, rel string, ch *transition.Change) error {
	src, mode, ok, err := read(a, rel)
	if err != nil || !ok {
		return err
	}
	if !strings.Contains(src, mainMarker.text) {
		return nil
	}
	if !strings.Contains(src, tracingHookStart) {
		ch.Skipped = append(ch.Skipped, rel+": the tracing handler is passed in a way the recipe does not know, so main was not changed; drop it from the router's hooks, since the generated router installs tracing itself")

		return nil
	}
	edited := strings.Replace(src, tracingHookStart, routerStart, 1)
	edited = dropUnusedImports(edited, httpImport, tracerImport)
	if err := write(a, rel, edited, mode); err != nil {
		return err
	}
	ch.Did = append(ch.Did, rel+": the tracing handler leaves the router's hooks; the generated router installs it")

	return nil
}

// mains lists the application's main.go files, outside the directories the walk skips.
func (CloudDriver) mains(a *app.App) ([]string, error) {
	var mains []string
	err := filepath.WalkDir(a.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrap(err, "filepath.WalkDir()")
		}
		if d.IsDir() {
			if p != a.Root && skippedDirs[d.Name()] {
				return fs.SkipDir
			}

			return nil
		}
		if d.Name() == mainFile {
			rel, err := filepath.Rel(a.Root, p)
			if err != nil {
				return errors.Wrap(err, "filepath.Rel()")
			}
			mains = append(mains, filepath.ToSlash(rel))
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "filepath.WalkDir()")
	}

	return mains, nil
}

// read answers a file's text and mode; ok is false when the file does not exist.
func read(a *app.App, rel string) (src string, mode fs.FileMode, ok bool, err error) {
	info, err := os.Stat(a.Abs(rel))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", 0, false, nil
		}

		return "", 0, false, errors.Wrap(err, "os.Stat()")
	}
	data, err := os.ReadFile(a.Abs(rel))
	if err != nil {
		return "", 0, false, errors.Wrap(err, "os.ReadFile()")
	}

	return string(data), info.Mode(), true, nil
}

// write formats and writes a rewritten Go file.
func write(a *app.App, rel, src string, mode fs.FileMode) error {
	formatted, err := format.Source([]byte(src))
	if err != nil {
		return errors.Wrapf(err, "format.Source(): %s", rel)
	}
	if err := os.WriteFile(a.Abs(rel), formatted, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}

	return nil
}

// addImport adds an import before another one in the file's import block, unless it is
// there already.
func addImport(src, path, before string) string {
	if strings.Contains(src, "\t\""+path+"\"\n") {
		return src
	}
	anchor := "\t\"" + before + "\"\n"
	if !strings.Contains(src, anchor) {
		return src
	}

	return strings.Replace(src, anchor, "\t\""+path+"\"\n"+anchor, 1)
}

// dropUnusedImports removes each import whose package is no longer named in the file.
func dropUnusedImports(src string, paths ...string) string {
	for _, p := range paths {
		line := "\t\"" + p + "\"\n"
		if !strings.Contains(src, line) {
			continue
		}
		ident := p[strings.LastIndex(p, "/")+1:] + "."
		if strings.Contains(strings.Replace(src, line, "", 1), ident) {
			continue
		}
		src = strings.Replace(src, line, "", 1)
	}

	return src
}
