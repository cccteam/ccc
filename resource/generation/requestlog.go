package generation

import (
	"math"
	"strconv"
	"strings"

	"cloud.google.com/go/logging"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

// An application declares, per surface, what a request writes to the request log and how
// its spans are sampled. The request log word is one of four, always, on event, sampled
// at a fraction, or never, with an optional minimum severity below which lines are
// dropped before they attach; the trace setting is one of three, follow the front end,
// capped at a rate, or off. The application default is WithRequestLog; an outlet's words
// are OutletRequestLog and OutletTraces on GenerateRoutes or WithRouterOutlet; routes the
// application mounts by hand are declared by path prefix with WithMountedRoutes; and a
// generated route's own words are the log: and trace: arguments of @rpc and @file, log:
// alone on @schedule. The nearest declaration wins: a route's over its outlet's, an
// outlet's over the application default, and a handler may change its own request at run
// time through logger.FromReq(r).SetPolicy. The generated router applies the words as a
// request descends and the root decides at the end (the logger's Policy), the tracer
// applies the trace setting as the span starts (tracer.Surfaces), and the release file
// carries every declared surface, so the application's stack renders the cloud's own
// request-log exclusion from the same declarations. An application that declares nothing
// is unchanged: every request's entry is written and spans follow the front end.

// RequestLog is what a surface writes to the request log: one of four words, with an
// optional minimum severity (MinSeverity). The zero value declares nothing. It renders as
// the logger's Policy in the generated router.
type RequestLog struct {
	word        requestLogWord
	fraction    float64
	minSeverity logging.Severity
}

// requestLogWord is the word a RequestLog was declared with; the zero value is none.
type requestLogWord uint8

const (
	requestLogUndeclared requestLogWord = iota
	requestLogAlways
	requestLogOnEvent
	requestLogSampled
	requestLogNever
)

// The words as an annotation writes them (log: onEvent) and as the release file spells
// them.
const (
	wordAlways  = resource.RequestLogAlways
	wordOnEvent = resource.RequestLogOnEvent
	wordSampled = resource.RequestLogSampled
	wordNever   = resource.RequestLogNever
)

// LogAlways writes the request's entry whatever happened: today's behavior, and what a
// surface that declares nothing does.
func LogAlways() RequestLog {
	return RequestLog{word: requestLogAlways}
}

// LogOnEvent writes the request's entry when a line attached to the request or the
// request failed, answering 400 or above; a quiet, successful request writes nothing.
func LogOnEvent() RequestLog {
	return RequestLog{word: requestLogOnEvent}
}

// LogSampled writes as LogOnEvent does and, for the quiet requests, the declared fraction
// of them, drawn once per request. The fraction must be above 0 and at most 1; the option
// the value is passed to refuses any other.
func LogSampled(fraction float64) RequestLog {
	return RequestLog{word: requestLogSampled, fraction: fraction}
}

// LogNever writes nothing, whatever happened: a health check wants this.
func LogNever() RequestLog {
	return RequestLog{word: requestLogNever}
}

// MinSeverity returns a copy of the word with a floor: lines below the severity are
// dropped before they attach to the request, so they neither count as an event nor
// appear as child entries. The receiver is unchanged.
func (l RequestLog) MinSeverity(severity logging.Severity) RequestLog {
	l.minSeverity = severity

	return l
}

// Declared reports whether a word was declared.
func (l RequestLog) Declared() bool {
	return l.word != requestLogUndeclared
}

// Equal reports whether two words are the same declaration, for comparisons that look
// through the generated routes.
func (l RequestLog) Equal(other RequestLog) bool {
	return l == other
}

// HasMinSeverity reports whether the word carries a floor.
func (l RequestLog) HasMinSeverity() bool {
	return l.minSeverity != logging.Default
}

// The words and the settings as the chain comment reads them, the names the logger's
// Policy and the tracer's Traces give themselves.
const (
	nameAlways         = "always"
	nameOnEvent        = "on event"
	nameNever          = "never"
	nameFollowFrontEnd = "follow the front end"
	nameOff            = "off"
)

// String names the word the way the chain comment reads it, as the logger's Policy
// names itself: "always", "on event", "sampled at 0.1" or "never", followed by the floor
// when one is set, as in "on event, Warning and above". Empty when nothing is declared.
func (l RequestLog) String() string {
	var word string
	switch l.word {
	case requestLogUndeclared:
		return ""
	case requestLogAlways:
		word = nameAlways
	case requestLogOnEvent:
		word = nameOnEvent
	case requestLogSampled:
		word = "sampled at " + formatFraction(l.fraction)
	case requestLogNever:
		word = nameNever
	}
	if !l.HasMinSeverity() {
		return word
	}

	return word + ", " + l.minSeverity.String() + " and above"
}

// Expr is the word as the generated router spells it: the logger's constructor, with
// the floor appended (logger.OnEvent().MinSeverity(logging.Warning)). Empty when nothing
// is declared.
func (l RequestLog) Expr() string {
	var expr string
	switch l.word {
	case requestLogUndeclared:
		return ""
	case requestLogAlways:
		expr = "logger.Always()"
	case requestLogOnEvent:
		expr = "logger.OnEvent()"
	case requestLogSampled:
		expr = "logger.Sampled(" + formatFraction(l.fraction) + ")"
	case requestLogNever:
		expr = "logger.Never()"
	}
	if !l.HasMinSeverity() {
		return expr
	}

	return expr + ".MinSeverity(logging." + l.minSeverity.String() + ")"
}

// Word is the word as the release file spells it (always, onEvent, sampled, never);
// empty when nothing is declared.
func (l RequestLog) Word() string {
	switch l.word {
	case requestLogAlways:
		return wordAlways
	case requestLogOnEvent:
		return wordOnEvent
	case requestLogSampled:
		return wordSampled
	case requestLogNever:
		return wordNever
	default:
		return ""
	}
}

// Fraction is the fraction a sampled word keeps; 0 for every other word.
func (l RequestLog) Fraction() float64 {
	if l.word != requestLogSampled {
		return 0
	}

	return l.fraction
}

// validate refuses a sampled word whose fraction is outside (0, 1], and a floor the
// logging library does not name, which the generated code could not spell.
func (l RequestLog) validate() error {
	if l.word == requestLogSampled && (math.IsNaN(l.fraction) || l.fraction <= 0 || l.fraction > 1) {
		return errors.Newf("LogSampled(%v): the fraction must be above 0 and at most 1", l.fraction)
	}
	if l.HasMinSeverity() && !knownSeverity(l.minSeverity) {
		return errors.Newf("MinSeverity(%d) names no severity of the logging library; the floors are logging.Debug through logging.Emergency", l.minSeverity)
	}

	return nil
}

// knownSeverity reports whether the logging library names the severity, so the generated
// code can spell it as a constant of the library.
func knownSeverity(severity logging.Severity) bool {
	switch severity {
	case logging.Default, logging.Debug, logging.Info, logging.Notice, logging.Warning, logging.Error, logging.Critical, logging.Alert, logging.Emergency:
		return true
	default:
		return false
	}
}

// Traces says how a surface's spans are sampled: follow the front end, capped at a rate,
// or off. The zero value declares nothing. It renders as the tracer's Traces in the
// generated router's surface table.
type Traces struct {
	setting tracesSetting
	rate    float64
}

// tracesSetting is the setting a Traces was declared with; the zero value is none.
type tracesSetting uint8

const (
	tracesUndeclared tracesSetting = iota
	tracesFollowFrontEnd
	tracesCapped
	tracesOff
)

// The settings as an annotation writes them (trace: off) and as the release file spells
// them.
const (
	settingFollowFrontEnd = resource.TracesFollowFrontEnd
	settingCapped         = resource.TracesCapped
	settingOff            = resource.TracesOff
)

// TracesFollowFrontEnd records a surface's spans as the application-wide sampling says:
// when the front end sampled the request, or every request where the driver records all.
// Today's behavior, and what a surface that declares nothing does.
func TracesFollowFrontEnd() Traces {
	return Traces{setting: tracesFollowFrontEnd}
}

// TracesCapped records a surface's span when the application-wide sampling would and a
// draw at the rate falls in, for a chatty surface the front end traces too often. The
// rate must be above 0 and at most 1; the option the value is passed to refuses any
// other.
func TracesCapped(rate float64) Traces {
	return Traces{setting: tracesCapped, rate: rate}
}

// TracesOff records no span for the surface, whatever the caller sent.
func TracesOff() Traces {
	return Traces{setting: tracesOff}
}

// Declared reports whether a setting was declared.
func (t Traces) Declared() bool {
	return t.setting != tracesUndeclared
}

// Equal reports whether two settings are the same declaration, for comparisons that look
// through the generated routes.
func (t Traces) Equal(other Traces) bool {
	return t == other
}

// String names the setting the way the chain comment reads it: "follow the front end",
// "capped at 0.1" or "off". Empty when nothing is declared.
func (t Traces) String() string {
	switch t.setting {
	case tracesFollowFrontEnd:
		return nameFollowFrontEnd
	case tracesCapped:
		return "capped at " + formatFraction(t.rate)
	case tracesOff:
		return nameOff
	default:
		return ""
	}
}

// Expr is the setting as the generated router spells it in the tracer's surface table
// (tracer.TracesCapped(0.1)). Empty when nothing is declared.
func (t Traces) Expr() string {
	switch t.setting {
	case tracesFollowFrontEnd:
		return "tracer.TracesFollowFrontEnd()"
	case tracesCapped:
		return "tracer.TracesCapped(" + formatFraction(t.rate) + ")"
	case tracesOff:
		return "tracer.TracesOff()"
	default:
		return ""
	}
}

// Setting is the setting as the release file spells it (followFrontEnd, capped, off);
// empty when nothing is declared.
func (t Traces) Setting() string {
	switch t.setting {
	case tracesFollowFrontEnd:
		return settingFollowFrontEnd
	case tracesCapped:
		return settingCapped
	case tracesOff:
		return settingOff
	default:
		return ""
	}
}

// Rate is the rate a capped setting keeps; 0 for every other setting.
func (t Traces) Rate() float64 {
	if t.setting != tracesCapped {
		return 0
	}

	return t.rate
}

// validate refuses a capped setting whose rate is outside (0, 1].
func (t Traces) validate() error {
	if t.setting == tracesCapped && (math.IsNaN(t.rate) || t.rate <= 0 || t.rate > 1) {
		return errors.Newf("TracesCapped(%v): the rate must be above 0 and at most 1", t.rate)
	}

	return nil
}

// formatFraction spells a fraction or a rate the shortest way that reads back exactly.
func formatFraction(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// The named arguments a route's annotation declares its words with: log: a request log
// word with fraction: for a sampled one, trace: a trace setting with rate: for a capped
// one.
const (
	logArgKey      = "log"
	fractionArgKey = "fraction"
	traceArgKey    = "trace"
	rateArgKey     = "rate"
)

// requestLogArgKeys are the argument keys a route's annotation takes for its words:
// @rpc and @file take all four, @schedule the request log pair alone.
var (
	requestLogArgKeys = []string{logArgKey, fractionArgKey}
	tracesArgKeys     = []string{traceArgKey, rateArgKey}
)

// parseRequestLogArgs reads a route's request log word off its annotation's named
// arguments: log: one of the four words, and fraction: with sampled alone, required
// then. keyword names the annotation for the refusals.
func parseRequestLogArgs(args genlang.NamedArgs, keyword string) (RequestLog, error) {
	word, declared := args.Named(logArgKey)
	fraction, hasFraction := args.Named(fractionArgKey)
	word, fraction = strings.TrimSpace(word), strings.TrimSpace(fraction)
	if !declared {
		if hasFraction {
			return RequestLog{}, errors.Newf("@%s(%s: %s) without %s: %s; the fraction belongs to the sampled word", keyword, fractionArgKey, fraction, logArgKey, wordSampled)
		}

		return RequestLog{}, nil
	}

	var log RequestLog
	switch word {
	case wordAlways:
		log = LogAlways()
	case wordOnEvent:
		log = LogOnEvent()
	case wordNever:
		log = LogNever()
	case wordSampled:
		if !hasFraction {
			return RequestLog{}, errors.Newf("@%s(%s: %s) names no %s: write %s: 0.01 for the share of the quiet requests the entry is written for", keyword, logArgKey, wordSampled, fractionArgKey, fractionArgKey)
		}
		f, err := strconv.ParseFloat(fraction, 64)
		if err != nil {
			return RequestLog{}, errors.Newf("@%s(%s: %s, %s: %s): the fraction is not a number", keyword, logArgKey, wordSampled, fractionArgKey, fraction)
		}
		log = LogSampled(f)
	default:
		return RequestLog{}, errors.Newf("@%s(%s: %s) names no request log word; the words are %s, %s, %s and %s", keyword, logArgKey, word, wordAlways, wordOnEvent, wordSampled, wordNever)
	}
	if word != wordSampled && hasFraction {
		return RequestLog{}, errors.Newf("@%s(%s: %s, %s: %s): the fraction belongs to the sampled word alone", keyword, logArgKey, word, fractionArgKey, fraction)
	}
	if err := log.validate(); err != nil {
		return RequestLog{}, errors.Wrapf(err, "@%s(%s: %s, %s: %s)", keyword, logArgKey, word, fractionArgKey, fraction)
	}

	return log, nil
}

// parseTracesArgs reads a route's trace setting off its annotation's named arguments:
// trace: one of the three settings, and rate: with capped alone, required then.
func parseTracesArgs(args genlang.NamedArgs, keyword string) (Traces, error) {
	setting, declared := args.Named(traceArgKey)
	rate, hasRate := args.Named(rateArgKey)
	setting, rate = strings.TrimSpace(setting), strings.TrimSpace(rate)
	if !declared {
		if hasRate {
			return Traces{}, errors.Newf("@%s(%s: %s) without %s: %s; the rate belongs to the capped setting", keyword, rateArgKey, rate, traceArgKey, settingCapped)
		}

		return Traces{}, nil
	}

	var traces Traces
	switch setting {
	case settingFollowFrontEnd:
		traces = TracesFollowFrontEnd()
	case settingOff:
		traces = TracesOff()
	case settingCapped:
		if !hasRate {
			return Traces{}, errors.Newf("@%s(%s: %s) names no %s: write %s: 0.1 for the share of the front end's traces the surface keeps", keyword, traceArgKey, settingCapped, rateArgKey, rateArgKey)
		}
		r, err := strconv.ParseFloat(rate, 64)
		if err != nil {
			return Traces{}, errors.Newf("@%s(%s: %s, %s: %s): the rate is not a number", keyword, traceArgKey, settingCapped, rateArgKey, rate)
		}
		traces = TracesCapped(r)
	default:
		return Traces{}, errors.Newf("@%s(%s: %s) names no trace setting; the settings are %s, %s and %s", keyword, traceArgKey, setting, settingFollowFrontEnd, settingCapped, settingOff)
	}
	if setting != settingCapped && hasRate {
		return Traces{}, errors.Newf("@%s(%s: %s, %s: %s): the rate belongs to the capped setting alone", keyword, traceArgKey, setting, rateArgKey, rate)
	}
	if err := traces.validate(); err != nil {
		return Traces{}, errors.Wrapf(err, "@%s(%s: %s, %s: %s)", keyword, traceArgKey, setting, rateArgKey, rate)
	}

	return traces, nil
}

// mountedRoutes is one WithMountedRoutes declaration: a path prefix the application
// mounts routes under by hand, with its words.
type mountedRoutes struct {
	prefix     string
	requestLog RequestLog
	traces     Traces
}

// describeWords says what a surface declares, for the chain comment: "request log on
// event, traces off".
func describeWords(log RequestLog, traces Traces) string {
	var parts []string
	if log.Declared() {
		parts = append(parts, "request log "+log.String())
	}
	if traces.Declared() {
		parts = append(parts, "traces "+traces.String())
	}

	return strings.Join(parts, ", ")
}

// surface is one declared surface as the release file lists it and the chain comment
// names it: the path or prefix as mounted, and the words.
type surface struct {
	Prefix     string
	RequestLog RequestLog
	Traces     Traces
}

// releaseSurface renders the surface for the release file.
func (s surface) releaseSurface() resource.Surface {
	return resource.Surface{
		Prefix:   s.Prefix,
		Log:      s.RequestLog.Word(),
		Fraction: s.RequestLog.Fraction(),
		Traces:   s.Traces.Setting(),
		Rate:     s.Traces.Rate(),
	}
}

// resolveRPCWords reads a method's own words off @rpc: log: (with fraction:) and trace:
// (with rate:), which its route registers with. A scheduled method declares its word on
// @schedule and its spans follow the front end, and a suppressed method has no route for
// a word to apply to, so either is refused naming the struct.
func resolveRPCWords(rpcMethod *rpcMethodInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	args, ok, err := rpcArguments(pStruct, annotations)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	log, err := parseRequestLogArgs(args, rpcKeyword)
	if err != nil {
		return errors.Wrapf(err, "struct %s", pStruct.Name())
	}
	traces, err := parseTracesArgs(args, rpcKeyword)
	if err != nil {
		return errors.Wrapf(err, "struct %s", pStruct.Name())
	}
	if !log.Declared() && !traces.Declared() {
		return nil
	}
	switch {
	case annotations.Struct.Has(scheduleKeyword) && log.Declared():
		return errors.Newf("struct %s: @%s(%s: %s) on a scheduled method; declare the word on @%s(..., %s: %s)", pStruct.Name(), rpcKeyword, logArgKey, log.Word(), scheduleKeyword, logArgKey, log.Word())
	case annotations.Struct.Has(scheduleKeyword):
		return errors.Newf("struct %s: @%s(%s: %s) on a scheduled method, whose spans follow the front end: Cloud Scheduler's call carries no trace to narrow", pStruct.Name(), rpcKeyword, traceArgKey, traces.Setting())
	case rpcMethod.SuppressHandler:
		return errors.Newf("struct %s: @%s declares a word with @%s, which generates no route for it to apply to; drop the word, or the suppression", pStruct.Name(), rpcKeyword, suppressKeyword)
	}
	rpcMethod.RequestLog, rpcMethod.Traces = log, traces

	return nil
}
