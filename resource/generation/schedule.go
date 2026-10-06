package generation

import (
	"strconv"
	"strings"
	"time"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/cccteam/ccc/resource/scheduled"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

// A scheduled method is an @rpc struct with @schedule("<cron>"[, zone: "<zone>"]): Cloud
// Scheduler calls it on the schedule, in the zone (UTC when none is named). It takes no
// input, since the scheduler sends no body and names no row or tenant, so it declares no
// field and no path parameter. The generated router mounts it under the scheduled prefix
// alone (scheduled.Prefix, /_scheduled/<method in kebab case>), behind the scheduler's
// token check and nothing else: no outlet serves it, no permission names it, and no
// browser client calls it. The release file lists every scheduled route with its
// schedule, which the application's stack reads to create one Cloud Scheduler job each.

// scheduleZoneArgKey is @schedule's one named argument, the time zone the schedule is
// read in; defaultScheduleZone is the zone a schedule without one is read in.
const (
	scheduleZoneArgKey  = "zone"
	defaultScheduleZone = "UTC"
	// localZone is the name time.LoadLocation reads as the machine's own zone, which
	// says nothing about where the scheduler runs.
	localZone = "Local"
)

// rpcSchedule is a method's validated @schedule declaration.
type rpcSchedule struct {
	// Cron is the cron expression as declared: five fields, minute, hour, day of the
	// month, month and day of the week, separated by single spaces.
	Cron string
	// Zone is the time zone the expression is read in, an IANA zone name as declared,
	// or UTC when the declaration names none.
	Zone string
}

// scheduledRoute is one scheduled method as the router mounts it and the release file
// lists it: the path under the scheduled prefix, the handler, and when it is called.
type scheduledRoute struct {
	Path        string
	HandlerFunc string
	Cron        string
	Zone        string
}

// scheduledPath is the path a scheduled method is mounted at: the scheduled prefix and
// the method's name in kebab case.
func scheduledPath(methodName string) string {
	return scheduled.Prefix + "/" + strcase.ToKebab(methodName)
}

// scheduledRoutesOf lists the scheduled methods' routes in the methods' order.
func scheduledRoutesOf(methods []*rpcMethodInfo) []*scheduledRoute {
	routes := make([]*scheduledRoute, 0, len(methods))
	for _, method := range methods {
		routes = append(routes, &scheduledRoute{
			Path:        scheduledPath(method.Name()),
			HandlerFunc: method.Name(),
			Cron:        method.Schedule.Cron,
			Zone:        method.Schedule.Zone,
		})
	}

	return routes
}

// splitScheduled separates the scheduled methods from the methods a person calls,
// keeping each list in the order given.
func splitScheduled(methods []*rpcMethodInfo) (served, scheduledMethods []*rpcMethodInfo) {
	for _, method := range methods {
		if method.Schedule != nil {
			scheduledMethods = append(scheduledMethods, method)

			continue
		}
		served = append(served, method)
	}

	return served, scheduledMethods
}

// resolveSchedule reads and validates the struct's @schedule: the cron expression and
// the zone, and that the method takes no input and declares nothing an outlet, a
// permission or a browser reads. It runs after the method's other declarations are
// resolved, so it reads them off the method.
func resolveSchedule(rpcMethod *rpcMethodInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	if !annotations.Struct.Has(scheduleKeyword) {
		return nil
	}
	invocations, err := annotations.Struct.Get(scheduleKeyword).ParseInvocations(&genlang.ArgSpec{
		Positional: 1,
		Keys:       []string{scheduleZoneArgKey},
	})
	if err != nil {
		return errors.Wrapf(err, "@%s on %s", scheduleKeyword, pStruct.Name())
	}
	schedule := &rpcSchedule{Cron: invocations[0].Positional[0], Zone: defaultScheduleZone}
	if zone, ok := invocations[0].Named(scheduleZoneArgKey); ok {
		schedule.Zone = zone
	}

	var errs []error
	if err := validateCron(schedule.Cron); err != nil {
		errs = append(errs, errors.Wrapf(err, "struct %s: @%s(%q) is not a cron expression", pStruct.Name(), scheduleKeyword, schedule.Cron))
	}
	if err := validateScheduleZone(schedule.Zone); err != nil {
		errs = append(errs, errors.Wrapf(err, "struct %s: @%s names zone %q", pStruct.Name(), scheduleKeyword, schedule.Zone))
	}
	errs = append(errs, scheduleInputErrors(rpcMethod, pStruct, annotations)...)
	if len(errs) > 0 {
		return errors.Wrapf(errors.Join(errs...), "@%s on %s", scheduleKeyword, pStruct.Name())
	}
	rpcMethod.Schedule = schedule

	return nil
}

// scheduleInputErrors refuses what a scheduled method cannot carry. Cloud Scheduler
// sends no body and names no row or tenant, so the method takes no input: no field, no
// upload, no tenant segment. It is served under the scheduled prefix alone, behind the
// scheduler's token, so nothing that places it on an outlet, gates it, names its
// permission or renames its route applies.
func scheduleInputErrors(rpcMethod *rpcMethodInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) []error {
	name := pStruct.Name()
	var errs []error
	if fields := pStruct.Fields(); len(fields) > 0 {
		names := make([]string, 0, len(fields))
		for _, field := range fields {
			names = append(names, field.Name())
		}
		errs = append(errs, errors.Newf("struct %s: a scheduled method takes no input, since Cloud Scheduler sends no request body, and %s declares %s; read what it needs inside Execute", name, name, strings.Join(names, ", ")))
	}
	if rpcMethod.Upload != nil {
		errs = append(errs, errors.Newf("struct %s: @%s with @%s: a scheduled call carries no body, so it uploads nothing", name, scheduleKeyword, uploadKeyword))
	}
	if annotations.Struct.Has(permissionScopeKeyword) {
		if rpcMethod.IsDomainScoped() {
			errs = append(errs, errors.Newf("struct %s: @%s with @%s(domain): a domain-scoped method takes the tenant as a path parameter, and a scheduled call names none; run over every tenant inside Execute", name, scheduleKeyword, permissionScopeKeyword))
		} else {
			errs = append(errs, errors.Newf("struct %s: @%s with @%s: a scheduled method checks no permission, the scheduler's token is its gate; drop the annotation", name, scheduleKeyword, permissionScopeKeyword))
		}
	}
	if annotations.Struct.Has(outletKeyword) {
		errs = append(errs, errors.Newf("struct %s: @%s with @%s: a scheduled method is served under %s alone, never on an outlet", name, scheduleKeyword, outletKeyword, scheduled.Prefix))
	}
	if annotations.Struct.Has(transitionKeyword) {
		errs = append(errs, errors.Newf("struct %s: @%s with @%s: a transition moves the row a caller names, and a scheduled call names none", name, scheduleKeyword, transitionKeyword))
	}
	if rpcMethod.Feature != nil {
		errs = append(errs, errors.Newf("struct %s: @%s with @%s: a scheduled route is not gated behind a flag; read the flag inside Execute", name, scheduleKeyword, featureKeyword))
	}
	if rpcMethod.Formerly != "" {
		errs = append(errs, errors.Newf("struct %s: @%s with @%s: the stack calls a scheduled route at its current name, so no former name is answered", name, scheduleKeyword, formerlyKeyword))
	}
	if rpcMethod.SuppressHandler {
		errs = append(errs, errors.Newf("struct %s: @%s with @%s: the generated router mounts a scheduled method's generated handler, so it is never suppressed", name, scheduleKeyword, suppressKeyword))
	}

	return errs
}

// validateScheduleZone refuses a zone the time zone database does not know, and the
// machine's own zone (Local), which says nothing about where the scheduler runs.
func validateScheduleZone(zone string) error {
	if zone == localZone {
		return errors.New("the zone Local is the generating machine's own; name an IANA zone such as America/Denver, or drop zone: for UTC")
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return errors.Wrap(err, "time.LoadLocation()")
	}

	return nil
}

// cronField is one field of a cron expression: its name and the values it admits, with
// the names it admits for them (JAN for 1, SUN for 0).
type cronField struct {
	name     string
	min, max int
	names    []string
}

// cronFields are the five fields of a cron expression as Cloud Scheduler reads it (the
// unix-cron format), in order.
var cronFields = []cronField{
	{name: "minute", min: 0, max: 59},
	{name: "hour", min: 0, max: 23},
	{name: "day of the month", min: 1, max: 31},
	{name: "month", min: 1, max: 12, names: []string{"JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"}},
	{name: "day of the week", min: 0, max: 6, names: []string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"}},
}

// validateCron refuses an expression that is not five fields separated by single spaces,
// each a comma list of *, a value, or a range a-b, the * and the range optionally
// stepped (*/15, 1-5/2), every value inside its field's bounds.
func validateCron(expr string) error {
	parts := strings.Split(expr, " ")
	if len(parts) != len(cronFields) {
		return errors.Newf("a cron expression is %d fields separated by single spaces (minute, hour, day of the month, month, day of the week), and this one has %d", len(cronFields), len(parts))
	}
	for i, part := range parts {
		if err := cronFields[i].validate(part); err != nil {
			return err
		}
	}

	return nil
}

// validate refuses a field that is not a comma list of valid items, naming the field and
// the item.
func (f *cronField) validate(text string) error {
	if text == "" {
		return errors.Newf("the %s field is empty", f.name)
	}
	for item := range strings.SplitSeq(text, ",") {
		if reason := f.itemProblem(item); reason != "" {
			return errors.Newf("the %s field %q: %s", f.name, text, reason)
		}
	}

	return nil
}

// itemProblem says what is wrong with an item that is not *, a value, or a range, each
// optionally stepped when it is * or a range; empty for a valid item.
func (f *cronField) itemProblem(item string) string {
	base, step, stepped := strings.Cut(item, "/")
	if stepped {
		if n, err := strconv.Atoi(step); err != nil || n < 1 {
			return strconv.Quote(item) + ": a step is a whole number of at least 1"
		}
	}
	if base == "*" {
		return ""
	}
	low, high, isRange := strings.Cut(base, "-")
	if !isRange {
		if stepped {
			return strconv.Quote(item) + ": a step follows * or a range, not a single value"
		}
		_, reason := f.value(base)

		return reason
	}
	from, reason := f.value(low)
	if reason != "" {
		return reason
	}
	to, reason := f.value(high)
	if reason != "" {
		return reason
	}
	if from > to {
		return strconv.Quote(item) + ": the range runs backwards"
	}

	return ""
}

// value reads one value of the field, a number inside its bounds or one of its names,
// or says why it is neither.
func (f *cronField) value(text string) (n int, problem string) {
	for i, name := range f.names {
		if strings.EqualFold(text, name) {
			return f.min + i, ""
		}
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return 0, strconv.Quote(text) + " is not a number"
	}
	if n < f.min || n > f.max {
		return 0, strconv.Itoa(n) + " is outside " + strconv.Itoa(f.min) + "-" + strconv.Itoa(f.max)
	}

	return n, ""
}

// requireRouterForSchedules refuses a scheduled method where no router is generated: the
// generated router mounts a scheduled method behind the scheduler's token check, and the
// release file it writes beside itself is where the application's stack reads the
// schedule, so without GenerateRouter nothing would serve the method or call it.
func (r *resourceGenerator) requireRouterForSchedules() error {
	if len(r.scheduledMethods) == 0 || r.genRouter {
		return nil
	}
	names := make([]string, 0, len(r.scheduledMethods))
	for _, method := range r.scheduledMethods {
		names = append(names, method.Name())
	}

	return errors.Newf("%s declare @%s without GenerateRouter: the generated router mounts a scheduled method behind the scheduler's token check, and its release file is where the stack reads the schedule; declare GenerateRouter", strings.Join(names, ", "), scheduleKeyword)
}

// scheduledRoutes are the scheduled methods' routes as the router mounts them; none when
// the RPC methods are not generated, since then no handler is either.
func (r *resourceGenerator) scheduledRoutes() []*scheduledRoute {
	if !r.genRPCMethods {
		return nil
	}

	return scheduledRoutesOf(r.scheduledMethods)
}
