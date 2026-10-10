package generation

import (
	"strings"
	"testing"

	"cloud.google.com/go/logging"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/google/go-cmp/cmp"
)

// TestRequestLogWords pins each word's names: the chain comment's, the generated
// router's expression, the release file's word and fraction, and the floor.
func TestRequestLogWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		log          RequestLog
		wantString   string
		wantExpr     string
		wantWord     string
		wantFraction float64
		wantDeclared bool
	}{
		{name: "nothing declared", log: RequestLog{}},
		{name: "always", log: LogAlways(), wantString: nameAlways, wantExpr: "logger.Always()", wantWord: resource.RequestLogAlways, wantDeclared: true},
		{name: "on event", log: LogOnEvent(), wantString: nameOnEvent, wantExpr: "logger.OnEvent()", wantWord: resource.RequestLogOnEvent, wantDeclared: true},
		{name: "sampled", log: LogSampled(0.01), wantString: "sampled at 0.01", wantExpr: "logger.Sampled(0.01)", wantWord: resource.RequestLogSampled, wantFraction: 0.01, wantDeclared: true},
		{name: "never, which writes nothing", log: LogNever(), wantString: nameNever, wantExpr: "logger.Never()", wantWord: resource.RequestLogNever, wantDeclared: true},
		{name: "on event with a floor", log: LogOnEvent().MinSeverity(logging.Warning), wantString: "on event, Warning and above", wantExpr: "logger.OnEvent().MinSeverity(logging.Warning)", wantWord: resource.RequestLogOnEvent, wantDeclared: true},
		{name: "a floor on a sampled word", log: LogSampled(0.5).MinSeverity(logging.Error), wantString: "sampled at 0.5, Error and above", wantExpr: "logger.Sampled(0.5).MinSeverity(logging.Error)", wantWord: resource.RequestLogSampled, wantFraction: 0.5, wantDeclared: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.log.String(); got != tt.wantString {
				t.Errorf("String() = %q, want %q", got, tt.wantString)
			}
			if got := tt.log.Expr(); got != tt.wantExpr {
				t.Errorf("Expr() = %q, want %q", got, tt.wantExpr)
			}
			if got := tt.log.Word(); got != tt.wantWord {
				t.Errorf("Word() = %q, want %q", got, tt.wantWord)
			}
			if got := tt.log.Fraction(); got != tt.wantFraction {
				t.Errorf("Fraction() = %v, want %v", got, tt.wantFraction)
			}
			if got := tt.log.Declared(); got != tt.wantDeclared {
				t.Errorf("Declared() = %v, want %v", got, tt.wantDeclared)
			}
			if err := tt.log.validate(); err != nil {
				t.Errorf("validate() error = %v", err)
			}
		})
	}
}

// TestRequestLogValidate pins the refusals: a fraction outside (0, 1] and a floor the
// logging library does not name.
func TestRequestLogValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		log     RequestLog
		wantErr string
	}{
		{name: "a fraction of zero", log: LogSampled(0), wantErr: "LogSampled(0): the fraction must be above 0 and at most 1"},
		{name: "a fraction over one", log: LogSampled(1.5), wantErr: "LogSampled(1.5): the fraction must be above 0 and at most 1"},
		{name: "a negative fraction", log: LogSampled(-0.1), wantErr: "the fraction must be above 0 and at most 1"},
		{name: "an unknown floor", log: LogOnEvent().MinSeverity(logging.Severity(150)), wantErr: "MinSeverity(150) names no severity of the logging library"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.log.validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestTracesSettings pins each setting's names and the refused rates.
func TestTracesSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		traces       Traces
		wantString   string
		wantExpr     string
		wantSetting  string
		wantRate     float64
		wantDeclared bool
		wantErr      string
	}{
		{name: "nothing declared", traces: Traces{}},
		{name: "follow the front end", traces: TracesFollowFrontEnd(), wantString: nameFollowFrontEnd, wantExpr: "tracer.TracesFollowFrontEnd()", wantSetting: resource.TracesFollowFrontEnd, wantDeclared: true},
		{name: "capped", traces: TracesCapped(0.1), wantString: "capped at 0.1", wantExpr: "tracer.TracesCapped(0.1)", wantSetting: resource.TracesCapped, wantRate: 0.1, wantDeclared: true},
		{name: "off", traces: TracesOff(), wantString: nameOff, wantExpr: "tracer.TracesOff()", wantSetting: resource.TracesOff, wantDeclared: true},
		{name: "capped at zero", traces: TracesCapped(0), wantString: "capped at 0", wantExpr: "tracer.TracesCapped(0)", wantSetting: resource.TracesCapped, wantDeclared: true, wantErr: "TracesCapped(0): the rate must be above 0 and at most 1"},
		{name: "capped over one", traces: TracesCapped(2), wantString: "capped at 2", wantExpr: "tracer.TracesCapped(2)", wantSetting: resource.TracesCapped, wantRate: 2, wantDeclared: true, wantErr: "TracesCapped(2): the rate must be above 0 and at most 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.traces.String(); got != tt.wantString {
				t.Errorf("String() = %q, want %q", got, tt.wantString)
			}
			if got := tt.traces.Expr(); got != tt.wantExpr {
				t.Errorf("Expr() = %q, want %q", got, tt.wantExpr)
			}
			if got := tt.traces.Setting(); got != tt.wantSetting {
				t.Errorf("Setting() = %q, want %q", got, tt.wantSetting)
			}
			if got := tt.traces.Rate(); got != tt.wantRate {
				t.Errorf("Rate() = %v, want %v", got, tt.wantRate)
			}
			if got := tt.traces.Declared(); got != tt.wantDeclared {
				t.Errorf("Declared() = %v, want %v", got, tt.wantDeclared)
			}
			err := tt.traces.validate()
			if tt.wantErr == "" && err != nil {
				t.Errorf("validate() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Errorf("validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestWithRequestLog pins the application default's option: a word is recorded once, and
// an undeclared or invalid one is refused.
func TestWithRequestLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options []ResourceOption
		want    RequestLog
		wantErr string
	}{
		{name: "on event", options: []ResourceOption{WithRequestLog(LogOnEvent())}, want: LogOnEvent()},
		{name: "sampled with a floor", options: []ResourceOption{WithRequestLog(LogSampled(0.1).MinSeverity(logging.Info))}, want: LogSampled(0.1).MinSeverity(logging.Info)},
		{name: "nothing declared", options: []ResourceOption{WithRequestLog(RequestLog{})}, wantErr: "WithRequestLog(RequestLog{}) declares no word"},
		{name: "a bad fraction", options: []ResourceOption{WithRequestLog(LogSampled(3))}, wantErr: "LogSampled(3): the fraction must be above 0 and at most 1"},
		{name: "declared twice", options: []ResourceOption{WithRequestLog(LogOnEvent()), WithRequestLog(LogNever())}, wantErr: "WithRequestLog(never) redeclares the application's request log word (on event)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rg := &resourceGenerator{}
			err := applyResourceOptions(t, rg, tt.options)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if rg.requestLog != tt.want {
				t.Errorf("requestLog = %+v, want %+v", rg.requestLog, tt.want)
			}
		})
	}
}

// applyResourceOptions applies resource options to a generator in order, stopping at the
// first refusal.
func applyResourceOptions(t *testing.T, rg *resourceGenerator, options []ResourceOption) error {
	t.Helper()

	for _, option := range options {
		opt, ok := option.(resourceOption)
		if !ok {
			t.Fatalf("%T is not a resourceOption", option)
		}
		if err := opt(rg); err != nil {
			return err
		}
	}

	return nil
}

// TestWithMountedRoutes pins the hand-mounted prefix option: a prefix with both words is
// recorded, and a malformed prefix, a missing word, an invalid one, the root and a
// repeated prefix are refused.
func TestWithMountedRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options []ResourceOption
		want    []mountedRoutes
		wantErr string
	}{
		{
			name:    "two prefixes",
			options: []ResourceOption{WithMountedRoutes("/beacons/", LogOnEvent(), TracesOff()), WithMountedRoutes("/healthz", LogNever(), TracesOff())},
			want:    []mountedRoutes{{prefix: "/beacons/", requestLog: LogOnEvent(), traces: TracesOff()}, {prefix: "/healthz", requestLog: LogNever(), traces: TracesOff()}},
		},
		{name: "the root", options: []ResourceOption{WithMountedRoutes("/", LogOnEvent(), TracesOff())}, wantErr: `WithMountedRoutes("/") declares the whole application; WithRequestLog declares the application default`},
		{name: "a relative prefix", options: []ResourceOption{WithMountedRoutes("beacons/", LogOnEvent(), TracesOff())}, wantErr: `WithMountedRoutes("beacons/") requires a path prefix starting with '/'`},
		{name: "a pattern", options: []ResourceOption{WithMountedRoutes("/beacons/{id}", LogOnEvent(), TracesOff())}, wantErr: "requires a path prefix starting with '/'"},
		{name: "no request log word", options: []ResourceOption{WithMountedRoutes("/beacons/", RequestLog{}, TracesOff())}, wantErr: `WithMountedRoutes("/beacons/") declares no request log word`},
		{name: "no trace setting", options: []ResourceOption{WithMountedRoutes("/beacons/", LogOnEvent(), Traces{})}, wantErr: `WithMountedRoutes("/beacons/") declares no trace setting`},
		{name: "a bad fraction", options: []ResourceOption{WithMountedRoutes("/beacons/", LogSampled(0), TracesOff())}, wantErr: "LogSampled(0): the fraction must be above 0 and at most 1"},
		{name: "a bad rate", options: []ResourceOption{WithMountedRoutes("/beacons/", LogOnEvent(), TracesCapped(5))}, wantErr: "TracesCapped(5): the rate must be above 0 and at most 1"},
		{name: "a prefix declared twice", options: []ResourceOption{WithMountedRoutes("/beacons/", LogOnEvent(), TracesOff()), WithMountedRoutes("/beacons/", LogNever(), TracesOff())}, wantErr: `WithMountedRoutes("/beacons/") is declared twice`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rg := &resourceGenerator{}
			err := applyResourceOptions(t, rg, tt.options)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if diff := cmp.Diff(tt.want, rg.mountedRoutes, cmp.AllowUnexported(mountedRoutes{}, RequestLog{}, Traces{})); diff != "" {
				t.Errorf("mountedRoutes mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestOutletWords pins the outlet options: a word and a setting are recorded once each,
// and an undeclared or invalid one is refused.
func TestOutletWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		options    []OutletOption
		wantLog    RequestLog
		wantTraces Traces
		wantErr    string
	}{
		{name: "a word and a setting", options: []OutletOption{OutletRequestLog(LogOnEvent()), OutletTraces(TracesCapped(0.25))}, wantLog: LogOnEvent(), wantTraces: TracesCapped(0.25)},
		{name: "a setting alone", options: []OutletOption{OutletTraces(TracesOff())}, wantTraces: TracesOff()},
		{name: "no word", options: []OutletOption{OutletRequestLog(RequestLog{})}, wantErr: "OutletRequestLog(RequestLog{}) declares no word"},
		{name: "no setting", options: []OutletOption{OutletTraces(Traces{})}, wantErr: "OutletTraces(Traces{}) declares no setting"},
		{name: "a bad fraction", options: []OutletOption{OutletRequestLog(LogSampled(2))}, wantErr: "LogSampled(2): the fraction must be above 0 and at most 1"},
		{name: "a bad rate", options: []OutletOption{OutletTraces(TracesCapped(0))}, wantErr: "TracesCapped(0): the rate must be above 0 and at most 1"},
		{name: "two words", options: []OutletOption{OutletRequestLog(LogOnEvent()), OutletRequestLog(LogAlways())}, wantErr: "OutletRequestLog(always) redeclares the outlet's request log word (on event)"},
		{name: "two settings", options: []OutletOption{OutletTraces(TracesOff()), OutletTraces(TracesFollowFrontEnd())}, wantErr: "OutletTraces(follow the front end) redeclares the outlet's trace setting (off)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got routerOutlet
			var err error
			for _, opt := range tt.options {
				if err = opt.applyToOutlet(&got); err != nil {
					break
				}
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("applyToOutlet() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("applyToOutlet() error = %v", err)
			}
			if got.requestLog != tt.wantLog || got.traces != tt.wantTraces {
				t.Errorf("outlet words = %v, %v; want %v, %v", got.requestLog, got.traces, tt.wantLog, tt.wantTraces)
			}
		})
	}
}

// TestResolveRPCWords pins a method's own words off @rpc, beside its body limit, and the
// refusals: a word that is not one, a fraction without its word, a word on a scheduled
// method, a setting on a scheduled method, and a word on a suppressed method.
func TestResolveRPCWords(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "wordsfixture"))

	tests := []struct {
		name       string
		structName string
		wantLog    RequestLog
		wantTraces Traces
		wantMax    int64
		wantErr    string
	}{
		{name: "on event", structName: "OnEventHail", wantLog: LogOnEvent()},
		{name: "sampled and capped", structName: "SampledHail", wantLog: LogSampled(0.01), wantTraces: TracesCapped(0.1)},
		{name: "a setting alone", structName: "QuietHail", wantTraces: TracesOff()},
		{name: "a word beside the body limit", structName: "BoundedHail", wantLog: LogNever(), wantMax: 64 << 10},
		{name: "a word that is not one", structName: "BadWord", wantErr: "@rpc(log: sometimes) names no request log word; the words are always, onEvent, sampled and never"},
		{name: "a fraction without its word", structName: "FractionAlone", wantErr: "@rpc(fraction: 0.5) without log: sampled; the fraction belongs to the sampled word"},
		{name: "a word on a scheduled method", structName: "ScheduledWord", wantErr: "@rpc(log: onEvent) on a scheduled method; declare the word on @schedule(..., log: onEvent)"},
		{name: "a setting on a scheduled method", structName: "ScheduledTrace", wantErr: "@rpc(trace: off) on a scheduled method, whose spans follow the front end"},
		{name: "a word on a suppressed method", structName: "SuppressedWord", wantErr: "@rpc declares a word with @suppress, which generates no route for it to apply to"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs[tt.structName]
			if s == nil {
				t.Fatalf("struct %q not found in fixture package", tt.structName)
			}
			methods, err := (&client{}).structsToRPCMethods([]*parser.Struct{s})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("structsToRPCMethods(%s) error = %v, want it to contain %q", tt.structName, err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.structName) {
					t.Errorf("structsToRPCMethods(%s) error does not name the struct: %v", tt.structName, err)
				}

				return
			}
			if err != nil {
				t.Fatalf("structsToRPCMethods(%s) error = %v", tt.structName, err)
			}
			if len(methods) != 1 {
				t.Fatalf("structsToRPCMethods(%s) = %v, want one method", tt.structName, methods)
			}
			if got := methods[0]; got.RequestLog != tt.wantLog || got.Traces != tt.wantTraces || got.MaxBytes != tt.wantMax {
				t.Errorf("words = %v, %v, max %d; want %v, %v, max %d", got.RequestLog, got.Traces, got.MaxBytes, tt.wantLog, tt.wantTraces, tt.wantMax)
			}
		})
	}
}

// TestResolveScheduleWords pins a scheduled method's word off @schedule, and the
// refusals: a word that is not one, and a trace setting, which @schedule does not take.
func TestResolveScheduleWords(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "wordsfixture"))

	tests := []struct {
		name       string
		structName string
		wantLog    RequestLog
		wantZone   string
		wantErr    string
	}{
		{name: "on event", structName: "ScheduledOnEvent", wantLog: LogOnEvent(), wantZone: "UTC"},
		{name: "sampled in a zone", structName: "ScheduledSampled", wantLog: LogSampled(0.5), wantZone: "America/Denver"},
		{name: "a word that is not one", structName: "ScheduledBadWord", wantErr: "@schedule(log: loud) names no request log word"},
		{name: "a trace setting", structName: "ScheduledTraceArgument", wantErr: `unknown argument "trace"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs[tt.structName]
			if s == nil {
				t.Fatalf("struct %q not found in fixture package", tt.structName)
			}
			methods, err := (&client{}).structsToRPCMethods([]*parser.Struct{s})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("structsToRPCMethods(%s) error = %v, want it to contain %q", tt.structName, err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("structsToRPCMethods(%s) error = %v", tt.structName, err)
			}
			if len(methods) != 1 || methods[0].Schedule == nil {
				t.Fatalf("structsToRPCMethods(%s) = %v, want one scheduled method", tt.structName, methods)
			}
			if got := methods[0].Schedule; got.RequestLog != tt.wantLog || got.Zone != tt.wantZone {
				t.Errorf("Schedule = %+v, want word %v in %q", got, tt.wantLog, tt.wantZone)
			}
			routes := scheduledRoutesOf(methods)
			if routes[0].RequestLog != tt.wantLog {
				t.Errorf("scheduled route's word = %v, want %v", routes[0].RequestLog, tt.wantLog)
			}
		})
	}
}

// Test_requestLogDecision pins what the generated request log test expects of the
// console exporter under each word, on a quiet and on a failed request.
func Test_requestLogDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		log        RequestLog
		wantQuiet  string
		wantFailed string
	}{
		{name: "nothing declared is always", log: RequestLog{}, wantQuiet: decisionWritten, wantFailed: decisionWritten},
		{name: "always", log: LogAlways(), wantQuiet: decisionWritten, wantFailed: decisionWritten},
		{name: "on event", log: LogOnEvent(), wantQuiet: decisionDropped, wantFailed: decisionWritten},
		{name: "sampled leaves a quiet request to the draw", log: LogSampled(0.5), wantQuiet: "", wantFailed: decisionWritten},
		{name: "never drops a failed request too", log: LogNever(), wantQuiet: decisionDropped, wantFailed: decisionDropped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := requestLogDecision(tt.log, false); got != tt.wantQuiet {
				t.Errorf("quiet = %q, want %q", got, tt.wantQuiet)
			}
			if got := requestLogDecision(tt.log, true); got != tt.wantFailed {
				t.Errorf("failed = %q, want %q", got, tt.wantFailed)
			}
		})
	}
}

// Test_routesTemplate_words pins the routes file: a route with a word registers through
// logger.WithPolicy ahead of its handler, on the bounded group or beside it as its kind
// does, with the logger imported and the logging library with a floor; a scheduled
// method's word registers the same way; and a file without words imports neither.
func Test_routesTemplate_words(t *testing.T) {
	t.Parallel()

	routes := map[string][]*generatedRoute{
		"HailShip": {
			{Method: "POST", Path: "/api/hail-ship", HandlerFunc: "HailShip", SelfBounded: true, RequestLog: LogOnEvent()},
		},
		"Ship": {
			{Method: "GET", Path: "/api/ships", HandlerFunc: "Ships", HandlerType: ListHandler},
			{Method: "GET", Path: "/api/ships/{shipID}/content", HandlerFunc: "ShipContent", HandlerType: fileHandler, RequestLog: LogSampled(0.1).MinSeverity(logging.Warning), Traces: TracesOff()},
		},
		"Renamed": {
			{Method: "POST", Path: "/api/renamed", FormerPath: "/api/former", HandlerFunc: "Renamed", SelfBounded: true, RequestLog: LogNever()},
		},
	}
	scheduled := []*scheduledRoute{
		{Path: "/_scheduled/prune-logs", HandlerFunc: "PruneLogs", Cron: "30 3 * * *", Zone: "UTC", RequestLog: LogOnEvent()},
		{Path: "/_scheduled/send-digest", HandlerFunc: "SendDigest", Cron: "0 6 * * *", Zone: "UTC"},
	}

	tests := []struct {
		name            string
		data            routerFileData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "routes and scheduled methods with words",
			data: routerFileData{
				ServesSessions:     true,
				Package:            "router",
				RoutesMap:          routes,
				ScheduledRoutes:    scheduled,
				ResourcePackage:    "resources",
				RouteRequestLogs:   true,
				RouteMinSeverities: true,
			},
			wantContains: []string{
				"\t\"cloud.google.com/go/logging\"\n",
				"\t\"github.com/cccteam/logger\"\n",
				"// HailShip writes its request log on event: the route's own word, set ahead of the handler.\n\tr.With(logger.WithPolicy(logger.OnEvent())).Post(\"/api/hail-ship\", h.HailShip())",
				"// ShipContent writes its request log sampled at 0.1, Warning and above: the route's own word, set ahead of the handler.\n\tbounded.With(logger.WithPolicy(logger.Sampled(0.1).MinSeverity(logging.Warning))).Get(\"/api/ships/{shipID}/content\", h.ShipContent())",
				"\tshipsHandler := h.Ships()\n\tbounded.Get(\"/api/ships\", shipsHandler)",
				"renamedHandler := h.Renamed()\n\tr.With(logger.WithPolicy(logger.Never())).Post(\"/api/renamed\", renamedHandler)\n\tr.With(logger.WithPolicy(logger.Never())).Post(\"/api/former\", renamedHandler)",
				"// PruneLogs writes its request log on event: the method's own word (@schedule), set ahead of the handler.\n\tr.With(logger.WithPolicy(logger.OnEvent())).Post(\"/_scheduled/prune-logs\", h.PruneLogs())",
				"\tr.Post(\"/_scheduled/send-digest\", h.SendDigest())",
			},
		},
		{
			name: "no words import nothing",
			data: routerFileData{
				ServesSessions:  true,
				Package:         "router",
				RoutesMap:       map[string][]*generatedRoute{"Ship": routes["Ship"][:1]},
				ResourcePackage: "resources",
			},
			wantNotContains: []string{"cloud.google.com/go/logging", "github.com/cccteam/logger", "WithPolicy"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, err := (&client{}).generateTemplateOutput("routesTemplate", routesTemplate, &tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			assertContains(t, "routesTemplate", string(out), tt.wantContains, tt.wantNotContains)
		})
	}
}

// TestRequireRouterForTraces pins the refusal of a route's trace setting without the
// generated router, naming the methods and resources that declare one.
func TestRequireRouterForTraces(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "wordsfixture"))
	quiet := &rpcMethodInfo{Struct: structs["QuietHail"], Traces: TracesOff()}
	loud := &rpcMethodInfo{Struct: structs["OnEventHail"], RequestLog: LogOnEvent()}

	tests := []struct {
		name    string
		r       *resourceGenerator
		wantErr string
	}{
		{name: "a router is generated", r: &resourceGenerator{genRouter: true, client: &client{rpcMethods: []*rpcMethodInfo{quiet}}}},
		{name: "no setting is declared", r: &resourceGenerator{client: &client{rpcMethods: []*rpcMethodInfo{loud}}}},
		{name: "a method's setting without a router", r: &resourceGenerator{client: &client{rpcMethods: []*rpcMethodInfo{loud, quiet}}}, wantErr: "QuietHail declare a trace setting (trace:) without GenerateRouter"},
		{name: "a stored file's setting without a router", r: &resourceGenerator{client: &client{resources: []*resourceInfo{{TypeInfo: structs["QuietHail"].TypeInfo, Files: []*fileRoute{{Segment: "content", Traces: TracesOff()}}}}}}, wantErr: "QuietHail declare a trace setting (trace:) without GenerateRouter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.r.requireRouterForTraces()
			if tt.wantErr == "" && err != nil {
				t.Errorf("requireRouterForTraces() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Errorf("requireRouterForTraces() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// Test_surfacesOf pins the release file's surfaces: the default at /, each outlet with
// words at its prefix, each worded route at its path, each hand-mounted prefix, and each
// scheduled method with a word, in prefix order, each carrying only what it declares and
// its kind: the default, an outlet and a hand-mounted prefix are prefixes, a worded route
// and a scheduled route are routes.
func Test_surfacesOf(t *testing.T) {
	t.Parallel()

	r := &resourceGenerator{
		requestLog:    LogOnEvent().MinSeverity(logging.Info),
		mountedRoutes: []mountedRoutes{{prefix: "/beacons/", requestLog: LogOnEvent(), traces: TracesOff()}},
	}
	outlets := []routerOutlet{
		{name: "default", prefix: "api"},
		{name: "droids", prefix: "droids", apiKey: true, requestLog: LogSampled(0.01), traces: TracesCapped(0.1)},
		{name: "portal", prefix: "portal/api", traces: TracesOff()},
	}
	worded := map[string][]*generatedRoute{
		"default": {{Method: "GET", Path: "/api/ships/{shipID}/content", RequestLog: LogNever()}},
		"droids":  {{Method: "POST", Path: "/droids/ingest-droid-reports", Traces: TracesOff()}},
	}
	scheduled := []*scheduledRoute{
		{Path: "/_scheduled/prune-logs", RequestLog: LogOnEvent()},
		{Path: "/_scheduled/send-digest"},
	}
	want := []resource.Surface{
		{Prefix: "/", Kind: resource.SurfacePrefix, Log: resource.RequestLogOnEvent},
		{Prefix: "/_scheduled/prune-logs", Kind: resource.SurfaceRoute, Log: resource.RequestLogOnEvent},
		{Prefix: "/api/ships/{shipID}/content", Kind: resource.SurfaceRoute, Log: resource.RequestLogNever},
		{Prefix: "/beacons/", Kind: resource.SurfacePrefix, Log: resource.RequestLogOnEvent, Traces: resource.TracesOff},
		{Prefix: "/droids/", Kind: resource.SurfacePrefix, Log: resource.RequestLogSampled, Fraction: 0.01, Traces: resource.TracesCapped, Rate: 0.1},
		{Prefix: "/droids/ingest-droid-reports", Kind: resource.SurfaceRoute, Traces: resource.TracesOff},
		{Prefix: "/portal/api/", Kind: resource.SurfacePrefix, Traces: resource.TracesOff},
	}
	if diff := cmp.Diff(want, r.surfacesOf(outlets, worded, scheduled)); diff != "" {
		t.Errorf("surfacesOf() mismatch (-want +got):\n%s", diff)
	}
	if got := (&resourceGenerator{}).surfacesOf([]routerOutlet{{name: "default", prefix: "api"}}, nil, nil); len(got) != 0 {
		t.Errorf("surfacesOf() with nothing declared = %+v, want none", got)
	}
}
