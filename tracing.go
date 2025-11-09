package ccc

import (
	"context"
	"runtime"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// StartTrace uses runtime reflection to determine the fully qualified
// package path and the function/method name of the caller.
//
// The Tracer name is set to the fully qualified package path
// (e.g., "github.com/cccteam/ccc").
// The Span name is set to the short function name (e.g., "Struct.Method()").
func StartTrace(ctx context.Context) (context.Context, trace.Span) {
	pc, _, _, ok := runtime.Caller(1)
	if !ok {
		return otel.Tracer("unknown").Start(ctx, "unknown-func")
	}

	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return otel.Tracer("unknown").Start(ctx, "unknown-func")
	}

	var tracerName, spanName string
	qualifiedName := fn.Name()
	lastSlash := strings.LastIndex(qualifiedName, "/")
	splitIndex := -1
	if lastSlash == -1 {
		splitIndex = strings.Index(qualifiedName, ".")
	} else {
		dotIndexInPart := strings.Index(qualifiedName[lastSlash:], ".")
		if dotIndexInPart != -1 {
			splitIndex = lastSlash + dotIndexInPart
		}
	}

	if splitIndex != -1 {
		tracerName = qualifiedName[:splitIndex]
		spanName = qualifiedName[splitIndex+1:]
	} else {
		tracerName = qualifiedName
		spanName = qualifiedName
	}

	if strings.HasPrefix(spanName, "(*") {
		spanName = strings.Replace(spanName[2:], ")", "", 1)
	}

	return otel.Tracer(tracerName).Start(ctx, spanName+"()")
}
