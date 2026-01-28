package tracer

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestStartTrace(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		callingFn  func(context.Context) (context.Context, trace.Span)
		tracerName string
		spanName   string
	}{
		{
			name:       "simple private function",
			callingFn:  wrapperFunction,
			tracerName: "github.com/cccteam/ccc/tracer/trace",
			spanName:   "wrapperFunction()",
		},
		{
			name:       "in closure",
			callingFn:  func(ctx context.Context) (context.Context, trace.Span) { return Start(ctx) },
			tracerName: "github.com/cccteam/ccc/tracer/trace",
			spanName:   "TestStartTrace.func1()",
		},
		{
			name:       "nested in closure",
			callingFn:  func(ctx context.Context) (context.Context, trace.Span) { return wrapperFunction(ctx) },
			tracerName: "github.com/cccteam/ccc/tracer/trace",
			spanName:   "wrapperFunction()",
		},
		{
			name:       "simple public function",
			callingFn:  WrapperFunction,
			tracerName: "github.com/cccteam/ccc/tracer/trace",
			spanName:   "WrapperFunction()",
		},
		{
			name:       "ptr method of private struct",
			callingFn:  (&wrapperStruct{}).PtrMethod,
			tracerName: "github.com/cccteam/ccc/tracer/trace",
			spanName:   "wrapperStruct.PtrMethod()",
		},
		{
			name:       "method of private struct",
			callingFn:  (wrapperStruct{}).Method,
			tracerName: "github.com/cccteam/ccc/tracer/trace",
			spanName:   "wrapperStruct.Method()",
		},
		{
			name:       "ptr method of public struct",
			callingFn:  (&WrapperStruct{}).PtrMethod,
			tracerName: "github.com/cccteam/ccc/tracer/trace",
			spanName:   "WrapperStruct.PtrMethod()",
		},
		{
			name:       "method of public struct",
			callingFn:  (WrapperStruct{}).Method,
			tracerName: "github.com/cccteam/ccc/tracer/trace",
			spanName:   "WrapperStruct.Method()",
		},
		{
			name:       "method of public struct with closure",
			callingFn:  (WrapperStruct{}).MethodWithClosure,
			tracerName: "github.com/cccteam/ccc/tracer/trace",
			spanName:   "WrapperStruct.MethodWithClosure.func1()",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
			otel.SetTracerProvider(tp)

			ctx, span := tc.callingFn(context.Background())
			span.End()

			if err := tp.ForceFlush(ctx); err != nil {
				t.Fatal(err)
			}

			spans := exporter.GetSpans()
			if len(spans) != 1 {
				t.Fatalf("expected 1 span, got %d", len(spans))
			}

			if spans[0].Name != tc.spanName {
				t.Errorf("expected span name %q, got %q", tc.spanName, spans[0].Name)
			}

			if spans[0].InstrumentationScope.Name != tc.tracerName {
				t.Errorf("expected tracer name %q, got %q", tc.tracerName, spans[0].InstrumentationScope.Name)
			}
		})
	}
}

func wrapperFunction(ctx context.Context) (context.Context, trace.Span) {
	return Start(ctx)
}

func WrapperFunction(ctx context.Context) (context.Context, trace.Span) {
	return Start(ctx)
}

type wrapperStruct struct{}

func (w *wrapperStruct) PtrMethod(ctx context.Context) (context.Context, trace.Span) {
	return Start(ctx)
}

func (w wrapperStruct) Method(ctx context.Context) (context.Context, trace.Span) {
	return Start(ctx)
}

type WrapperStruct struct{}

func (w *WrapperStruct) PtrMethod(ctx context.Context) (context.Context, trace.Span) {
	return Start(ctx)
}

func (w WrapperStruct) Method(ctx context.Context) (context.Context, trace.Span) {
	return Start(ctx)
}

func (w WrapperStruct) MethodWithClosure(ctx context.Context) (context.Context, trace.Span) {
	return func() (context.Context, trace.Span) {
		return Start(ctx)
	}()
}
