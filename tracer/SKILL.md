---
name: tracer
description: Use the github.com/cccteam/ccc/tracer package for all OpenTelemetry tracing in CCC services — auto-named per-function spans via tracer.Start, Google Cloud Trace export, and HTTP middleware. Reach for it whenever code needs spans, trace setup, Cloud Trace integration, sampling configuration, or when debugging missing traces; it replaces the deprecated ccc.StartTrace and shields projects from importing OTel SDK packages directly.
---

# tracer

Convenience layer over OpenTelemetry for CCC services. It gives you three things:
`Start` (a span helper that names the tracer and span automatically from the
calling function), a Google Cloud Trace `TracerProvider`, and HTTP middleware. A
stated design goal is **dependency containment**: it wraps `*sdktrace.TracerProvider`
so downstream projects don't import OTel SDK/semconv directly (version
coordination between those is painful). Prefer this package over raw OTel in this
stack, and over the deprecated `ccc.StartTrace`.

## Per-function spans (the dominant pattern)

```go
func (s *Server) DoWork(ctx context.Context) error {
    ctx, span := tracer.Start(ctx)
    defer span.End()
    // tracer name: "github.com/you/yourpkg"; span name: "Server.DoWork()"
    ...
}
```

`Start` derives names from the caller via `runtime.Caller` (memoized per program
counter). Pointer and value receivers produce identical span names. Because the
stack depth is fixed, **never wrap `Start` in your own helper** — every span would
be named after the helper. `tracer.Span` is a type alias for `trace.Span`, so it
interoperates freely.

## Provider setup

```go
tp, err := tracer.NewGoogleCloudTracerProvider("my-gcp-project", "my-service")
if err != nil { ... }
defer tp.Shutdown(ctx)
```

`loggingProjectID` is the GCP project (auth via Application Default Credentials);
`serviceName` becomes the `service.name` resource attribute. Both constructors set
the **global** OTel tracer provider.

⚠️ **The default sampler is `ParentBased(NeverSample())`** — root spans are never
sampled, so a service that doesn't receive an already-sampled parent context
exports *nothing*. This is the number-one cause of "my traces are missing".
Override it (caller options are appended after the defaults, so yours wins):

```go
tp, err := tracer.NewGoogleCloudTracerProviderWithOptions(
    "my-gcp-project", "my-service",
    tracer.WithClientOptions(option.WithCredentialsFile("/secrets/sa.json")),
    tracer.WithTracerProviderOptions(
        sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))),
    ),
)
```

Use the `WithOptions` form whenever you need `WithClientOptions`; the plain form
only forwards `sdktrace.TracerProviderOption`s.

## Dev builds

Building with `-tags dev` swaps in a no-export provider (`provider_dev.go`):
identical signatures, all arguments ignored, no GCP dependency at runtime — the
sanctioned way to disable tracing locally. Call sites compile unchanged.

## HTTP middleware

```go
router.Use(tracer.NewGoogleCloudHandler())
```

Presets: Cloud Trace propagation (`X-Cloud-Trace-Context`), read/write message
events, and span names set to `r.URL.Path`. Pass `otelhttp.Option`s to extend or
override.

## Gotchas

- Missing traces? Check the sampler first (see above), then confirm a provider
  was constructed *without* `-tags dev`, then check ADC credentials.
- Constructors mutate global state (`otel.SetTracerProvider`) — call once at
  startup.
- When bumping `go.opentelemetry.io/otel/sdk`, the pinned `semconv` import in
  `provider.go` must move in lockstep; `provider_test.go` fails the build's tests
  with the exact version to move to.
- `Start` with no provider configured yields no-op spans (safe, silent).
