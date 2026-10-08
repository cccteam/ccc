// Code generated from the skeleton's forms by the recipe's author; the forms are the
// exact texts the skeleton carried, so the recipe rewrites only what it recognises.

package recipe

// The forms of the core configuration region (from the coreConfiguration struct's
// comment through the end of LogExporter) the recipe rewrites: the skeleton before
// tracing was wired, and the skeleton with tracing wired by hand (impulse 0.1.2).
var configRegions = []string{
	"// coreConfiguration is the first level: what every process of the application shares.\ntype coreConfiguration struct {\n\tenv           *coreConfig\n\tloggingClient *logging.Client\n}\n\nfunc newCoreConfiguration(ctx context.Context) (*coreConfiguration, error) {\n\tenv := &coreConfig{}\n\tif err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: env, Lookuper: envconfig.OsLookuper()}); err != nil {\n\t\treturn nil, errors.Wrap(err, \"envconfig.ProcessWith()\")\n\t}\n\n\tconf := &coreConfiguration{env: env}\n\tif env.LoggingProjectID != \"\" {\n\t\tclient, err := logging.NewClient(ctx, env.LoggingProjectID)\n\t\tif err != nil {\n\t\t\treturn nil, errors.Wrap(err, \"logging.NewClient()\")\n\t\t}\n\t\tconf.loggingClient = client\n\t}\n\n\treturn conf, nil\n}\n\n// Close releases the level's clients.\nfunc (c *coreConfiguration) Close() {\n\tif c.loggingClient != nil {\n\t\tif err := c.loggingClient.Close(); err != nil {\n\t\t\tlog.Print(errors.Wrap(err, \"logging.Client.Close()\"))\n\t\t}\n\t}\n}\n\n// LogExporter returns where request logs go: Cloud Logging when a logging project is\n// configured, the console otherwise.\nfunc (c *coreConfiguration) LogExporter() logger.Exporter {\n\tif c.loggingClient != nil {\n\t\treturn logger.NewGoogleCloudExporter(c.loggingClient, c.env.LoggingProjectID)\n\t}\n\n\treturn logger.NewConsoleExporter()\n}\n",
	"// coreConfiguration is the first level: what every process of the application shares.\ntype coreConfiguration struct {\n\tenv           *coreConfig\n\tloggingClient *logging.Client\n\t// traceProvider exports the process's spans; nil without a logging project.\n\ttraceProvider *tracer.Provider\n}\n\nfunc newCoreConfiguration(ctx context.Context) (*coreConfiguration, error) {\n\tenv := &coreConfig{}\n\tif err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: env, Lookuper: envconfig.OsLookuper()}); err != nil {\n\t\treturn nil, errors.Wrap(err, \"envconfig.ProcessWith()\")\n\t}\n\n\tconf := &coreConfiguration{env: env}\n\tif env.LoggingProjectID != \"\" {\n\t\tclient, err := logging.NewClient(ctx, env.LoggingProjectID)\n\t\tif err != nil {\n\t\t\treturn nil, errors.Wrap(err, \"logging.NewClient()\")\n\t\t}\n\t\tconf.loggingClient = client\n\t\t// The traces go to the logs' project under the process's service name. The\n\t\t// provider is the global one, so every span the libraries and the generated\n\t\t// code start (tracer.Start) is recorded through it.\n\t\tprovider, err := tracer.NewGoogleCloudTracerProvider(env.LoggingProjectID, env.ServiceName)\n\t\tif err != nil {\n\t\t\treturn nil, errors.Wrap(err, \"tracer.NewGoogleCloudTracerProvider()\")\n\t\t}\n\t\tconf.traceProvider = provider\n\t}\n\n\treturn conf, nil\n}\n\n// Close releases the level's clients: the trace provider first, so the spans its\n// batcher still holds are sent before the logging client goes.\nfunc (c *coreConfiguration) Close() {\n\tif c.traceProvider != nil {\n\t\tctx, cancel := context.WithTimeout(context.Background(), traceFlushTimeout)\n\t\tdefer cancel()\n\t\tif err := c.traceProvider.Shutdown(ctx); err != nil {\n\t\t\tlog.Print(errors.Wrap(err, \"tracer.Provider.Shutdown()\"))\n\t\t}\n\t}\n\tif c.loggingClient != nil {\n\t\tif err := c.loggingClient.Close(); err != nil {\n\t\t\tlog.Print(errors.Wrap(err, \"logging.Client.Close()\"))\n\t\t}\n\t}\n}\n\n// LogExporter returns where request logs go: Cloud Logging when a logging project is\n// configured, the console otherwise.\nfunc (c *coreConfiguration) LogExporter() logger.Exporter {\n\tif c.loggingClient != nil {\n\t\treturn logger.NewGoogleCloudExporter(c.loggingClient, c.env.LoggingProjectID)\n\t}\n\n\treturn logger.NewConsoleExporter()\n}\n",
}

// configRegionNew is the region on the cloud driver.
const configRegionNew = "// coreConfiguration is the first level: what every process of the application shares.\ntype coreConfiguration struct {\n\tenv *coreConfig\n\t// cloud is the cloud driver: where the process's logs and spans go.\n\tcloud *gcp.Driver\n}\n\nfunc newCoreConfiguration(ctx context.Context) (*coreConfiguration, error) {\n\tenv := &coreConfig{}\n\tif err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: env, Lookuper: envconfig.OsLookuper()}); err != nil {\n\t\treturn nil, errors.Wrap(err, \"envconfig.ProcessWith()\")\n\t}\n\n\t// The cloud driver builds the log exporter and the trace provider from the settings\n\t// the configuration embeds; without a logging project the logs go to the console and\n\t// no span is exported.\n\tcloud, err := gcp.Open(ctx, env.Settings, env.ServiceName)\n\tif err != nil {\n\t\treturn nil, errors.Wrap(err, \"gcp.Open()\")\n\t}\n\n\treturn &coreConfiguration{env: env, cloud: cloud}, nil\n}\n\n// Close releases the level's clients: the spans still in hand are sent first.\nfunc (c *coreConfiguration) Close() {\n\tif err := c.cloud.Close(); err != nil {\n\t\tlog.Print(err)\n\t}\n}\n\n// LogExporter returns where request logs go: Cloud Logging when a logging project is\n// configured, the console otherwise.\nfunc (c *coreConfiguration) LogExporter() logger.Exporter {\n\treturn c.cloud.LogExporter\n}\n"

// traceFlushTimeoutConst is the constant the hand-wired form declared; it goes.
const traceFlushTimeoutConst = "// traceFlushTimeout bounds the wait for the spans still in hand when the process ends.\nconst traceFlushTimeout = 5 * time.Second\n\n"

// loggingProjectField is the variable the configuration declared itself; the embedded
// settings declare it now.
const loggingProjectField = "\t// LoggingProjectID is the Google Cloud project request logs ship to. Empty logs\n\t// to the console.\n\tLoggingProjectID string `env:\"GOOGLE_CLOUD_LOGGING_PROJECT\"`\n"

// cloudSettingsField is the embedded driver settings.
const cloudSettingsField = "\t// The Google Cloud driver's variables: the logging project and the trace sampling.\n\tgcp.Settings\n"

// loggerMiddlewareMethod is the App's method the generated router used to call.
const loggerMiddlewareMethod = "// LoggerMiddleware returns a middleware that logs requests.\nfunc (a *App) LoggerMiddleware() func(http.Handler) http.Handler {\n\treturn logger.NewRequestLogger(a.logExporter)\n}\n"

// logExporterMethod is what the generated router calls now.
const logExporterMethod = "// LogExporter is where the request log goes; the generated router builds the request\n// logger from it.\nfunc (a *App) LogExporter() logger.Exporter {\n\treturn a.logExporter\n}\n"

// tracingHookStart is main's hand-wired tracing hook, and routerStart the start without it.
const (
	tracingHookStart = "\t// Tracing runs ahead of the logger on every request, so the request log and the spans\n\t// share the trace the caller's headers name.\n\thooks := router.Hooks{Outermost: []func(http.Handler) http.Handler{tracer.NewGoogleCloudHandler()}}\n\tif err := server.New(conf.Addr()).Start(ctx, router.New(a, hooks)); err != nil {"
	routerStart      = "\tif err := server.New(conf.Addr()).Start(ctx, router.New(a, router.Hooks{})); err != nil {"
)

// loggingProjectLines document the logging project in the development environment
// template; the sampling's lines go after them.
const loggingProjectLines = "# GOOGLE_CLOUD_LOGGING_PROJECT ships request logs to Cloud Logging; unset logs to the console.\n# export GOOGLE_CLOUD_LOGGING_PROJECT=\n"

// traceSamplingLines document the trace sampling the driver's settings add.
const traceSamplingLines = "# APP_TRACE_SAMPLING exports every request's spans (all) or the ones the caller sampled (edge,\n# the default); the spans go to the logging project, so none leaves without one.\n# export APP_TRACE_SAMPLING=edge\n"
