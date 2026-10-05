package generation

import (
	goparser "go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
)

// TestResolveSchedule extracts each @schedule shape through the RPC extraction: a valid
// schedule is recorded with its zone (UTC when none is named) in either Execute form, and
// every expression, zone and declaration a scheduled method cannot carry is refused
// naming the struct.
func TestResolveSchedule(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "schedulefixture"))

	tests := []struct {
		name       string
		structName string
		wantCron   string
		wantZone   string
		wantErr    string
	}{
		{name: "a schedule in a zone", structName: "PruneLogs", wantCron: "30 3 * * *", wantZone: "America/Denver"},
		{name: "a schedule without a zone is read in UTC", structName: "SweepEveryQuarterHour", wantCron: "*/15 * * * *", wantZone: "UTC"},
		{name: "lists, ranges and names", structName: "SendDigest", wantCron: "0 6,18 1-15 JAN-MAR MON-FRI", wantZone: "UTC"},
		{name: "a day of the week by name", structName: "TallyWeek", wantCron: "0 4 * * SUN", wantZone: "UTC"},
		{name: "four fields are refused", structName: "FourFields", wantErr: "is 5 fields separated by single spaces (minute, hour, day of the month, month, day of the week), and this one has 4"},
		{name: "an hour outside 0-23 is refused", structName: "HourTooLarge", wantErr: `the hour field "24": 24 is outside 0-23`},
		{name: "a stepped single value is refused", structName: "SteppedValue", wantErr: `"5/15": a step follows * or a range, not a single value`},
		{name: "a zero step is refused", structName: "ZeroStep", wantErr: `"*/0": a step is a whole number of at least 1`},
		{name: "an unknown month name is refused", structName: "UnknownMonth", wantErr: `the month field "FOO": "FOO" is not a number`},
		{name: "a backwards range is refused", structName: "BackwardsRange", wantErr: `"5-3": the range runs backwards`},
		{name: "two spaces between fields are refused", structName: "DoubleSpace", wantErr: "this one has 6"},
		{name: "an unknown zone is refused", structName: "UnknownZone", wantErr: `@schedule names zone "Mars/Olympus_Mons"`},
		{name: "the machine's own zone is refused", structName: "LocalZone", wantErr: "the zone Local is the generating machine's own"},
		{name: "an argument other than zone is refused", structName: "UnknownArgument", wantErr: `unknown argument "tz"`},
		{name: "a method that takes input is refused", structName: "TakesInput", wantErr: "a scheduled method takes no input, since Cloud Scheduler sends no request body, and TakesInput declares Note"},
		{name: "a domain-scoped method is refused: a path parameter", structName: "DomainScoped", wantErr: "takes the tenant as a path parameter, and a scheduled call names none"},
		{name: "a permission scope is refused", structName: "GlobalScoped", wantErr: "a scheduled method checks no permission"},
		{name: "an outlet is refused", structName: "OnOutlet", wantErr: "is served under /_scheduled alone, never on an outlet"},
		{name: "a former name is refused", structName: "Renamed", wantErr: "the stack calls a scheduled route at its current name"},
		{name: "a suppressed handler is refused", structName: "Suppressed", wantErr: "a scheduled method's generated handler, so it is never suppressed"},
		{name: "an upload is refused", structName: "Uploads", wantErr: "a scheduled call carries no body, so it uploads nothing"},
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
			if len(methods) != 1 || methods[0].Schedule == nil {
				t.Fatalf("structsToRPCMethods(%s) = %v, want one scheduled method", tt.structName, methods)
			}
			if got := methods[0].Schedule; got.Cron != tt.wantCron || got.Zone != tt.wantZone {
				t.Errorf("Schedule = %+v, want cron %q in %q", got, tt.wantCron, tt.wantZone)
			}
		})
	}
}

// TestScheduleInputErrors refuses a scheduled method gated behind a feature flag, a gate
// the extraction resolves against the resources package's flags.
func TestScheduleInputErrors(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "schedulefixture"))

	tests := []struct {
		name    string
		feature *featureGate
		wantErr string
	}{
		{name: "an ungated method carries nothing it cannot"},
		{name: "a gated method is refused", feature: &featureGate{Constant: "Digests"}, wantErr: "struct Gated: @schedule with @feature: a scheduled route is not gated behind a flag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs["Gated"]
			annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(s)
			if err != nil {
				t.Fatalf("ScanStruct(Gated) error = %v", err)
			}
			errs := scheduleInputErrors(&rpcMethodInfo{Struct: s, Feature: tt.feature}, s, annotations)
			if tt.wantErr == "" {
				if len(errs) != 0 {
					t.Errorf("scheduleInputErrors() = %v, want none", errs)
				}

				return
			}
			if len(errs) != 1 || !strings.Contains(errs[0].Error(), tt.wantErr) {
				t.Errorf("scheduleInputErrors() = %v, want one containing %q", errs, tt.wantErr)
			}
		})
	}
}

// TestScheduleOnAnotherKind refuses @schedule on a struct that is not an @rpc method: a
// resource declares no method for Cloud Scheduler to call.
func TestScheduleOnAnotherKind(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "schedulefixture"))

	tests := []struct {
		name       string
		structName string
		wantErr    string
	}{
		{name: "a resource", structName: "Report", wantErr: "struct Report: @schedule is only valid on @rpc structs; a resource declares no method for Cloud Scheduler to call"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs[tt.structName]
			annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(s)
			if err != nil {
				t.Fatalf("ScanStruct(%s) error = %v", tt.structName, err)
			}
			if err := rejectRPCOnlyAnnotations(s, annotations, "resource"); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("rejectRPCOnlyAnnotations(%s) = %v, want it to contain %q", tt.structName, err, tt.wantErr)
			}
		})
	}
}

// TestValidateCron reads cron expressions as Cloud Scheduler does (the unix-cron
// format): five fields of lists, ranges, names and steps, each inside its bounds.
func TestValidateCron(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    string
		wantErr string
	}{
		{name: "every minute", expr: "* * * * *"},
		{name: "a stepped range", expr: "0-30/10 * * * *"},
		{name: "the bounds of each field", expr: "59 23 31 12 6"},
		{name: "the lower bounds", expr: "0 0 1 1 0"},
		{name: "names in either case", expr: "0 0 * jan,Dec sun-sat"},
		{name: "an empty expression", expr: "", wantErr: "this one has 1"},
		{name: "six fields", expr: "0 0 0 * * *", wantErr: "this one has 6"},
		{name: "a minute of 60", expr: "60 * * * *", wantErr: `the minute field "60": 60 is outside 0-59`},
		{name: "a day of the month of 0", expr: "0 0 0 * *", wantErr: `the day of the month field "0": 0 is outside 1-31`},
		{name: "a month of 13", expr: "0 0 * 13 *", wantErr: `the month field "13": 13 is outside 1-12`},
		{name: "a day of the week of 7", expr: "0 0 * * 7", wantErr: `the day of the week field "7": 7 is outside 0-6`},
		{name: "an empty list item", expr: "0,,5 * * * *", wantErr: `"" is not a number`},
		{name: "a descriptor", expr: "@daily", wantErr: "this one has 1"},
		{name: "a negative step", expr: "*/-5 * * * *", wantErr: "a step is a whole number of at least 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateCron(tt.expr)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("validateCron(%q) = %v, want nil", tt.expr, err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("validateCron(%q) = %v, want it to contain %q", tt.expr, err, tt.wantErr)
			}
		})
	}
}

// TestRequireRouterForSchedules refuses a scheduled method without GenerateRouter, which
// mounts it and writes the release file its schedule is read from.
func TestRequireRouterForSchedules(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "schedulefixture"))
	scheduledMethod := &rpcMethodInfo{Struct: structs["PruneLogs"], Schedule: &rpcSchedule{Cron: "30 3 * * *", Zone: "America/Denver"}}

	tests := []struct {
		name      string
		genRouter bool
		methods   []*rpcMethodInfo
		wantErr   string
	}{
		{name: "no scheduled method needs no router"},
		{name: "a scheduled method under GenerateRouter", genRouter: true, methods: []*rpcMethodInfo{scheduledMethod}},
		{name: "a scheduled method without GenerateRouter is refused", methods: []*rpcMethodInfo{scheduledMethod}, wantErr: "PruneLogs declare @schedule without GenerateRouter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{client: &client{scheduledMethods: tt.methods}, genRouter: tt.genRouter}
			err := r.requireRouterForSchedules()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("requireRouterForSchedules() = %v, want nil", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("requireRouterForSchedules() = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// Test_scheduledHandlerTemplate renders a scheduled method's handler in each shape the
// fixture declares and parses it as Go: the transaction form answering a result and the
// client form answering nothing. Neither decodes a request, stamps a caller or runs a dry
// run, and both publish the rows they wrote.
func Test_scheduledHandlerTemplate(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "schedulefixture"))

	tests := []struct {
		name       string
		structName string
		want       []string
	}{
		{
			name:       "the transaction form answering a result",
			structName: "PruneLogs",
			want: []string{
				"// PruneLogs serves the scheduled method PruneLogs, which Cloud Scheduler calls\n// on the schedule \"30 3 * * *\" in America/Denver.",
				"p := &schedulefixture.PruneLogs{}",
				"answer, err := p.Execute(ctx, txn, a.RPCClient())",
				"type response struct {",
				`live.Publish(ctx, a.LiveService(), "", touched)`,
			},
		},
		{
			name:       "the client form answering nothing",
			structName: "SweepEveryQuarterHour",
			want: []string{
				"if err := p.Execute(ctx, a.ResourceClient(), a.RPCClient()); err != nil {",
				"return httpio.NewEncoder(w).Ok(nil)",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			methods, err := (&client{}).structsToRPCMethods([]*parser.Struct{structs[tt.structName]})
			if err != nil {
				t.Fatalf("structsToRPCMethods(%s) error = %v", tt.structName, err)
			}
			r := &resourceGenerator{client: &client{}}
			out, err := r.generateTemplateOutput("scheduledHandlerTemplate", scheduledHandlerTemplate, &rpcHandlerData{
				Source:          "pkg/rpc",
				Package:         "app",
				RPCMethod:       methods[0],
				ApplicationName: "App",
				ReceiverName:    "a",
			})
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			if _, err := goparser.ParseFile(token.NewFileSet(), "zz_gen_handler.go", out, goparser.ParseComments); err != nil {
				t.Fatalf("the rendered handler is not Go: %v\n%s", err, out)
			}
			for _, want := range tt.want {
				if !strings.Contains(string(out), want) {
					t.Errorf("rendered handler missing %q:\n%s", want, out)
				}
			}
			for _, absent := range []string{"Decode", "WithCaller", "IsDryRun", "type request struct"} {
				if strings.Contains(string(out), absent) {
					t.Errorf("rendered handler carries %q:\n%s", absent, out)
				}
			}
		})
	}
}
