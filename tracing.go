package ccc

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const callerStackDepth = 1 // Caller of StartTrace is at depth 1

var traceCache = &sync.Map{}

type traceInfo struct {
	tracerName string
	spanName   string
}

// StartTrace uses runtime reflection to determine the fully qualified
// package path and the function/method name of the caller.
//
// The Tracer name is set to the fully qualified package path
// (e.g., "github.com/cccteam/ccc").
// The Span name is set to the short function name (e.g., "Struct.Method()").
//
// Deprecated: Use github.com/cccteam/ccc/tracer.Start instead
//
//go:noinline
func StartTrace(ctx context.Context) (context.Context, trace.Span) {
	pc, _, _, ok := runtime.Caller(callerStackDepth)
	if !ok {
		return otel.Tracer("unknown").Start(ctx, "unknown-func")
	}

	if i, ok := traceCache.Load(pc); ok {
		switch info := i.(type) {
		case traceInfo:
			return otel.Tracer(info.tracerName).Start(ctx, info.spanName)
		default:
			panic(fmt.Sprintf("unexpected type %T in traceCache for pc %v", i, pc))
		}
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

	spanName += "()"
	traceCache.Store(pc, traceInfo{tracerName: tracerName, spanName: spanName})

	return otel.Tracer(tracerName).Start(ctx, spanName)
}
