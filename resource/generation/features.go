package generation

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

// resourceFeatureType is the fully qualified type of the constants that declare feature
// flags: resource.Feature.
const resourceFeatureType = "github.com/cccteam/ccc/resource.Feature"

// featureGate is a resolved @feature(<Constant>): the constant in the resources package
// and the flag it names.
type featureGate struct {
	Constant string
	Name     resource.Feature
}

// registerFeatures reads the feature flag declarations off the resources package's
// resource.Feature constants: the constant is the flag's identifier in @feature, its
// value the flag's name, its doc comment the description. A malformed name or a name
// declared twice is refused naming the constants. It runs before any struct is
// extracted, so every @feature resolves against the complete set.
func (c *client) registerFeatures(constants []*parser.Constant) error {
	c.features = make(map[string]resource.FeatureDeclaration)
	c.featureDeclarations = nil
	for _, constant := range constants {
		if constant.TypeName() != resourceFeatureType {
			continue
		}
		declaration := resource.FeatureDeclaration{
			Name:        resource.Feature(constant.Value()),
			Description: strings.Join(strings.Fields(constant.Comments()), " "),
			Constant:    constant.Name(),
		}
		c.features[constant.Name()] = declaration
		c.featureDeclarations = append(c.featureDeclarations, declaration)
	}
	slices.SortFunc(c.featureDeclarations, func(a, b resource.FeatureDeclaration) int {
		return strings.Compare(a.Constant, b.Constant)
	})

	if err := resource.ValidateFeatureDeclarations(c.featureDeclarations); err != nil {
		return errors.Wrap(err, "resource.ValidateFeatureDeclarations()")
	}

	return nil
}

// featureConstants lists the declared constants, for a refusal.
func (c *client) featureConstants() []string {
	names := make([]string, 0, len(c.featureDeclarations))
	for _, d := range c.featureDeclarations {
		names = append(names, d.Constant)
	}

	return names
}

// resolveFeatureArg resolves one @feature argument against the declarations: the
// argument is the constant's identifier, nothing else.
func (c *client) resolveFeatureArg(arg genlang.Arg, where string) (*featureGate, error) {
	ident := strings.TrimSpace(string(arg))
	if ident == "" || strings.ContainsAny(ident, " \t,\"'.") {
		return nil, errors.Newf("%s: @%s(%s) names a flag by its constant's identifier, as declared in the resources package; got %q", where, featureKeyword, arg, ident)
	}
	declaration, ok := c.features[ident]
	if !ok {
		return nil, errors.Newf("%s: @%s(%s) names no resource.Feature constant in the resources package; declared: %v", where, featureKeyword, ident, c.featureConstants())
	}

	return &featureGate{Constant: declaration.Constant, Name: declaration.Name}, nil
}

// resolveStructFeature resolves a struct-scope @feature: the gate on the whole
// resource or method.
func (c *client) resolveStructFeature(pStruct *parser.Struct, annotations genlang.StructAnnotations) (*featureGate, error) {
	if !annotations.Struct.Has(featureKeyword) {
		return nil, nil
	}

	return c.resolveFeatureArg(annotations.Struct.Get(featureKeyword), "struct "+pStruct.Name())
}

// resolveResourceFeatures resolves a table or view resource's @feature declarations:
// the struct's, and each field's. A field that cannot be hidden is refused: a primary
// key (the row's identity and its route), the tenant key (the partition), the state
// column (the workflow), a @file key (read by the file route for itself), and a column
// a create must supply (NOT NULL, no default), since a creator with the flag off could
// never supply it.
func (c *client) resolveResourceFeatures(res *resourceInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	gate, err := c.resolveStructFeature(pStruct, annotations)
	if err != nil {
		return err
	}
	res.Feature = gate

	var errs []error
	for i, field := range res.Fields {
		fieldAnnotations := annotationsOfField(pStruct, annotations, field.Name())
		if !fieldAnnotations.Has(featureKeyword) {
			continue
		}
		where := fmt.Sprintf("struct %s field %s", pStruct.Name(), field.Name())
		gate, err := c.resolveFeatureArg(fieldAnnotations.Get(featureKeyword), where)
		if err != nil {
			errs = append(errs, err)

			continue
		}
		if refusal, refused := fieldGateRefusal(res, field); refused {
			errs = append(errs, errors.Newf("%s: @%s cannot hide %s", where, featureKeyword, refusal))

			continue
		}
		res.Fields[i].Feature = gate
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "feature annotation error")
	}

	return nil
}

// annotationsOfField returns a field's scanned annotations by name: the scanner
// indexes them by the struct's field order, and a resource's fields may be a subset.
func annotationsOfField(pStruct *parser.Struct, annotations genlang.StructAnnotations, name string) genlang.ArgMap {
	for i, field := range pStruct.Fields() {
		if field.Name() == name && i < len(annotations.Fields) {
			return annotations.Fields[i]
		}
	}

	return genlang.ArgMap{}
}

// fieldGateRefusal says why a field of a table or view resource cannot be gated, and
// whether it is refused.
func fieldGateRefusal(res *resourceInfo, field *resourceField) (string, bool) {
	switch {
	case field.IsPrimaryKey:
		return "the primary key: it is the row's identity and its route", true
	case field.IsTenantKey:
		return "the tenant key: it is the partition every request binds", true
	case field.IsState:
		return "the state column: the workflow turns on it", true
	case field.IsFileKey:
		return "a @file key: the file route reads it for itself", true
	case !res.IsVirtual && !res.CreateHandlerDisabled() && field.IsRequired() && !field.IsOutputOnly():
		return "a column a create must supply (NOT NULL with no default): a creator with the flag off could never supply it; make the column nullable, give it a default, or gate the resource", true
	default:
		return "", false
	}
}

// resolveComputedFeatures resolves a computed resource's @feature declarations: the
// struct's and each field's, a key refused as on a table.
func (c *client) resolveComputedFeatures(res *computedResource, annotations genlang.StructAnnotations) error {
	gate, err := c.resolveStructFeature(res.Struct, annotations)
	if err != nil {
		return err
	}
	res.Feature = gate

	var errs []error
	for i, field := range res.Fields {
		if i >= len(annotations.Fields) || !annotations.Fields[i].Has(featureKeyword) {
			continue
		}
		where := fmt.Sprintf("struct %s field %s", res.Name(), field.Name())
		gate, err := c.resolveFeatureArg(annotations.Fields[i].Get(featureKeyword), where)
		if err != nil {
			errs = append(errs, err)

			continue
		}
		if field.IsPrimaryKey {
			errs = append(errs, errors.Newf("%s: @%s cannot hide the primary key: it is the row's identity and its route", where, featureKeyword))

			continue
		}
		if field.IsFileKey {
			errs = append(errs, errors.Newf("%s: @%s cannot hide a @file key: the file route reads it for itself", where, featureKeyword))

			continue
		}
		res.Fields[i].Feature = gate
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "feature annotation error")
	}

	return nil
}

// resolveRPCFeature resolves an RPC method's @feature: the struct's alone. A method's
// request fields are not gated one by one; the method is.
func (c *client) resolveRPCFeature(method *rpcMethodInfo, annotations genlang.StructAnnotations) error {
	for i, field := range method.Struct.Fields() {
		if i < len(annotations.Fields) && annotations.Fields[i].Has(featureKeyword) {
			return errors.Newf("struct %s field %s: @%s gates a method whole, not field by field; put it on the struct", method.Name(), field.Name(), featureKeyword)
		}
	}
	gate, err := c.resolveStructFeature(method.Struct, annotations)
	if err != nil {
		return err
	}
	method.Feature = gate

	return nil
}

// rejectReservedResourceName refuses a struct whose resource would be the library's
// FeatureFlags (the flags are served by the library's handlers on every application),
// and one whose plural file stem is a file the generator writes for itself
// (rejectReservedStem).
func (c *client) rejectReservedResourceName(pStruct *parser.Struct, kind string) error {
	plural := c.pluralize(pStruct.Name())
	if plural == string(resource.FeatureFlagsResource) {
		return errors.Newf("struct %s: %s is the feature flags' resource, which the library serves on every application; a %s cannot take the name", pStruct.Name(), resource.FeatureFlagsResource, kind)
	}

	return rejectReservedStem(pStruct.Name(), fileStem(plural))
}

// rejectReservedMethodName refuses an RPC struct named SetFeature: the flip is the
// library's method on every application.
func rejectReservedMethodName(pStruct *parser.Struct) error {
	if pStruct.Name() == string(resource.SetFeatureMethod) {
		return errors.Newf("struct %s: %s is the feature flags' method, which the library serves on every application; an RPC method cannot take the name", pStruct.Name(), resource.SetFeatureMethod)
	}

	return nil
}

// rejectReservedManualRegistrations refuses a manual registration naming the flags'
// resource or method.
func rejectReservedManualRegistrations(registrations []ManualRegistration) error {
	for _, reg := range registrations {
		if reg.Resource == resource.FeatureFlagsResource || reg.Resource == resource.SetFeatureMethod {
			return errors.Newf("manual registration of %s: the feature flags' resource and method are registered by the generator on every application; remove the declaration", reg.Resource)
		}
	}

	return nil
}

// featureGates assembles the generated FeatureGates declaration: every gated resource,
// computed resource and RPC method by its collection name, every gated field by
// "<Resource>.<wire name>", in a stable order.
func (c *client) featureGates() []featureGateEntry {
	var entries []featureGateEntry
	for _, res := range c.resources {
		plural := c.pluralize(res.Name())
		if res.Feature != nil {
			entries = append(entries, featureGateEntry{Key: plural, Constant: res.Feature.Constant})
		}
		for _, field := range res.Fields {
			if field.Feature != nil && field.WireName() != "" {
				entries = append(entries, featureGateEntry{Key: plural + "." + field.WireName(), Constant: field.Feature.Constant})
			}
		}
	}
	for _, res := range c.computedResources {
		plural := c.pluralize(res.Name())
		if res.Feature != nil {
			entries = append(entries, featureGateEntry{Key: plural, Constant: res.Feature.Constant})
		}
		for _, field := range res.Fields {
			if field.Feature != nil && !field.IsInputOnly() {
				entries = append(entries, featureGateEntry{Key: plural + "." + caser.ToCamel(field.Name()), Constant: field.Feature.Constant})
			}
		}
	}
	for _, method := range c.rpcMethods {
		if method.Feature != nil {
			entries = append(entries, featureGateEntry{Key: method.Name(), Constant: method.Feature.Constant})
		}
	}
	slices.SortFunc(entries, func(a, b featureGateEntry) int {
		return strings.Compare(a.Key, b.Key)
	})

	return entries
}

// featureGateEntry is one line of the generated FeatureGates: the digest key and the
// constant that gates it.
type featureGateEntry struct {
	Key      string
	Constant string
}

// hasGates reports whether anything is gated.
func (c *client) hasGates() bool {
	return len(c.featureGates()) > 0
}

// generateFeatureDeclarations writes the resources package's zz_gen_features.go:
// Features(), the declarations the deploy step migrates the table to, and
// FeatureGates(), what the digest filters by. Both are written on every application, so
// the generated handlers call them unconditionally. It runs once every kind that can
// be gated is extracted, which is also when every manual registration is known, so a
// registration taking the library's names is refused here.
func (r *resourceGenerator) generateFeatureDeclarations() error {
	if err := rejectReservedManualRegistrations(r.manualRegistrations); err != nil {
		return err
	}

	begin := time.Now()
	destinationFilePath := filepath.Join(r.resource.Dir(), generatedGoFileName(featuresOutputName))

	if err := r.writeFormattedGoFile(destinationFilePath, "featureDeclarationsTemplate", featureDeclarationsTemplate, &featureDeclarationsData{
		Source:       r.resource.Dir(),
		Package:      r.resource.Package(),
		Declarations: r.featureDeclarations,
		Gates:        r.featureGates(),
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated feature declarations file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

// generateFeatures writes the handler package's zz_gen_features.go: the features route,
// the FeatureFlags routes and SetFeature as delegations to the library's handlers, and
// the FeatureGuard the gated routes are wrapped in.
func (r *resourceGenerator) generateFeatures() error {
	begin := time.Now()
	destinationFilePath := filepath.Join(r.handler.Dir(), generatedGoFileName(featuresOutputName))

	if err := r.writeFormattedGoFile(destinationFilePath, "featuresTemplate", featuresTemplate, &featuresData{
		Source:                 r.resource.Dir(),
		Package:                r.handler.Package(),
		LocalPackageImports:    r.localPackageImports(),
		ApplicationName:        r.applicationName,
		ReceiverName:           r.receiverName,
		RouterPackage:          r.router.Package(),
		RoutePrefix:            r.routePrefix,
		HasExtraSessionOutlets: slices.ContainsFunc(r.extraOutlets, func(outlet routerOutlet) bool { return outlet.servesSessions }),
		HasRoutes:              r.genRoutes,
		HasGates:               r.hasGates(),
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated features file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

// The feature flag routes' handler names, as the generated handlers interface and the
// library's handlers spell them.
const (
	featuresHandlerFunc     = "Features"
	featureFlagsHandlerFunc = "FeatureFlags"
	featureFlagHandlerFunc  = "FeatureFlag"
	setFeatureHandlerFunc   = "SetFeature"
	// featureFlagRoutesKey groups the FeatureFlags routes in an outlet's route map.
	featureFlagRoutesKey = "FeatureFlag"
)

// featureFlagNameTestValue is the route parameter's value in the generated router test.
const featureFlagNameTestValue = "testFeatureFlagName"

// featuresRoute is the features route under the outlet prefix: on every outlet.
func featuresRoute(routePrefix string) *generatedRoute {
	path := fmt.Sprintf("/%s/%s", routePrefix, resource.FeaturesRoute)

	return &generatedRoute{Method: http.MethodGet, Path: path, HandlerFunc: featuresHandlerFunc, TestURL: path}
}

// featureFlagRoutes are the FeatureFlags resource's routes under the outlet prefix: the
// list and the read, on a session-serving outlet.
func featureFlagRoutes(routePrefix string) []*generatedRoute {
	base := fmt.Sprintf("/%s/%s", routePrefix, resource.FeatureFlagsRoute)
	list := &generatedRoute{Method: http.MethodGet, Path: base, HandlerFunc: featureFlagsHandlerFunc, HandlerType: ListHandler, TestURL: base}
	read := &generatedRoute{
		Method:      http.MethodGet,
		Path:        base,
		HandlerFunc: featureFlagHandlerFunc,
		HandlerType: ReadHandler,
		TestURL:     base,
		TestParams:  []routeTestParam{{Key: string(resource.FeatureFlagNameParam), Value: featureFlagNameTestValue}},
	}
	read.appendParamsToPaths()

	return []*generatedRoute{list, read}
}

// setFeatureRoute is the SetFeature method's route under the outlet prefix, on a
// session-serving outlet.
func setFeatureRoute(routePrefix string) *generatedRoute {
	path := fmt.Sprintf("/%s/%s", routePrefix, resource.SetFeatureRoute)

	return &generatedRoute{Method: http.MethodPost, Path: path, HandlerFunc: setFeatureHandlerFunc, TestURL: path}
}

// accumulateFeatureRoutes adds the feature flag routes to every outlet: the features
// route on each, the FeatureFlags routes and SetFeature on the session-serving ones.
// Every route joins the dispatch test.
func accumulateFeatureRoutes(outlets []routerOutlet, outletRoutes []*outletRouteData) (routerTestRoutes []*generatedRoute) {
	for i, outlet := range outlets {
		features := featuresRoute(outlet.prefix)
		outletRoutes[i].RoutesMap[featuresHandlerFunc] = []*generatedRoute{features}
		routerTestRoutes = append(routerTestRoutes, features)
		if !outlet.servesSessions {
			continue
		}
		flags := featureFlagRoutes(outlet.prefix)
		outletRoutes[i].RoutesMap[featureFlagRoutesKey] = flags
		routerTestRoutes = append(routerTestRoutes, flags...)
		setFeature := setFeatureRoute(outlet.prefix)
		outletRoutes[i].RoutesMap[setFeatureHandlerFunc] = []*generatedRoute{setFeature}
		routerTestRoutes = append(routerTestRoutes, setFeature)
	}

	return routerTestRoutes
}

// featureNegativeTests are the isolation cases of a session-less outlet: the
// FeatureFlags routes and SetFeature under its prefix must fall through to 404.
func featureNegativeTests(outlet *routerOutlet) []negativeRouterTest {
	if outlet.servesSessions {
		return nil
	}
	var tests []negativeRouterTest
	for _, route := range featureFlagRoutes(outlet.prefix) {
		for _, method := range route.TestMethods() {
			tests = append(tests, negativeRouterTest{Method: method, URL: route.TestURL})
		}
	}
	setFeature := setFeatureRoute(outlet.prefix)

	return append(tests, negativeRouterTest{Method: httpMethodConstant(http.MethodPost), URL: setFeature.TestURL})
}

// featureAuthzCases are the matrix's cases for the feature flag routes: on every
// outlet the features route open to anyone signed in; on a session-serving outlet the
// FeatureFlags list and read as query pairs and SetFeature denied-only, with its dry
// run.
func (r *resourceGenerator) featureAuthzCases() []authzCase {
	var cases []authzCase
	for _, outlet := range r.allOutlets() {
		features := featuresRoute(outlet.prefix)
		cases = append(cases, authzCase{
			Name:   authzCaseName(features.HandlerFunc, &outlet),
			Method: httpMethodConst(features.Method),
			URL:    features.TestURL,
			Open:   true,
		})
		if !outlet.servesSessions {
			continue
		}
		flags := featureFlagRoutes(outlet.prefix)
		cases = append(cases,
			authzCase{Name: authzCaseName(flags[0].HandlerFunc, &outlet), Method: httpMethodConst(flags[0].Method), URL: flags[0].TestURL, Permission: string(accesstypes.List)},
			authzCase{Name: authzCaseName(flags[1].HandlerFunc, &outlet), Method: httpMethodConst(flags[1].Method), URL: strings.Replace(flags[1].Path, "{"+string(resource.FeatureFlagNameParam)+"}", "authz-test-key", 1), Permission: string(accesstypes.Read)},
		)
		setFeature := setFeatureRoute(outlet.prefix)
		cases = append(cases,
			authzCase{Name: authzCaseName(setFeature.HandlerFunc, &outlet), Method: httpMethodConst(setFeature.Method), URL: setFeature.TestURL, Body: emptyObjectBody, DeniedOnly: true},
			authzCase{Name: authzCaseName(setFeature.HandlerFunc, &outlet) + " dry run", Method: httpMethodConst(setFeature.Method), URL: setFeature.TestURL, Body: emptyObjectBody, Headers: []authzHeader{{Name: resource.DryRunHeader, Value: jsonTrueLiteral}}, DeniedOnly: true},
		)
	}

	return cases
}

// collectFeatureRegistrations registers the feature flags' resource and method into
// every application's collection: FeatureFlags with List and Read over its five
// fields, ordered by name, and SetFeature with Execute, both in the global scope.
// Registered only when this run generates routes, since the registrations model what
// the generated routes serve.
func (r *resourceGenerator) collectFeatureRegistrations(b *resource.CollectionBuilder) error {
	if !r.genRoutes {
		return nil
	}
	for _, perm := range []accesstypes.Permission{accesstypes.List, accesstypes.Read} {
		set, err := resource.NewSetData(resource.FeatureFlagFieldTags(), perm)
		if err != nil {
			return errors.Wrap(err, "resource.NewSetData()")
		}
		if err := b.AddResourceSet(accesstypes.GlobalPermissionScope, resource.FeatureFlagsResource, set); err != nil {
			return errors.Wrapf(err, "registering %s %s", resource.FeatureFlagsResource, perm)
		}
	}
	order, keys := resource.FeatureFlagsQueryKeys()
	b.SetResourceQueryKeys(accesstypes.GlobalPermissionScope, resource.FeatureFlagsResource, order, keys)
	if err := b.AddMethodResource(accesstypes.GlobalPermissionScope, accesstypes.Execute, resource.SetFeatureMethod); err != nil {
		return errors.Wrapf(err, "registering %s", resource.SetFeatureMethod)
	}

	return nil
}

// featureGateTest is one gated route in the generated feature gate tests, on one
// outlet: how to call it and the grants the on state is called with.
type featureGateTest struct {
	Name   string
	Method string
	URL    string
	Body   string
	// Constant is the gate's constant in the resources package.
	Constant string
	// Permission is the grant the on state carries, for a query route; a mutation
	// route is called without one, so the on state is the gate's own refusal.
	Permission string
}

// featureFieldTest is one gated field in the generated tests: the list route asking
// for the column alone, which the off state refuses as an unknown column.
type featureFieldTest struct {
	Name     string
	URL      string
	Constant string
}

// featureTestsData feeds the generated feature gate tests.
type featureTestsData struct {
	Source              string
	Package             string
	LocalPackageImports string
	ResourcePackage     string
	Routes              []featureGateTest
	Fields              []featureFieldTest
}

// featureTests assembles the generated feature gate tests: every gated resource's,
// computed resource's and method's routes on every outlet serving them, and every
// gated field's list route asking for it.
func (r *resourceGenerator) featureTests() (*featureTestsData, error) {
	data := &featureTestsData{
		Source:              r.resource.Dir(),
		Package:             r.handlerTests.Package(),
		LocalPackageImports: r.localPackageImports(),
		ResourcePackage:     r.resource.Package(),
	}
	for _, res := range r.resources {
		if res.RoutingDisabled() {
			continue
		}
		for _, outlet := range r.memberOutlets(&res.outletMembership) {
			if err := r.resourceFeatureTests(data, res, &outlet); err != nil {
				return nil, err
			}
		}
	}
	for _, res := range r.computedResources {
		if res.RoutingDisabled() {
			continue
		}
		for _, outlet := range r.memberOutlets(&res.outletMembership) {
			if err := r.computedFeatureTests(data, res, &outlet); err != nil {
				return nil, err
			}
		}
	}
	if r.genRPCMethods {
		r.methodFeatureTests(data)
	}

	return data, nil
}

// resourceFeatureTests adds one table or view resource's cases on one outlet: every
// route when the resource is gated, and the list asking for each gated field.
func (r *resourceGenerator) resourceFeatureTests(data *featureTestsData, res *resourceInfo, outlet *routerOutlet) error {
	pkTypes := resourcePKTypes(res)
	var listRoute *generatedRoute
	for _, ht := range resourceEndpoints(res) {
		route, err := r.resourceRoute(res, ht, outlet.prefix)
		if err != nil {
			return err
		}
		if ht == ListHandler {
			listRoute = route
		}
		if res.Feature == nil {
			continue
		}
		test, err := gateTestOf(route, pkTypes, resourceMatrixListQuery(res), res.Feature.Constant, outlet)
		if err != nil {
			return err
		}
		data.Routes = append(data.Routes, test)
	}
	if listRoute == nil {
		return nil
	}
	for _, field := range res.Fields {
		if field.Feature == nil || field.WireName() == "" {
			continue
		}
		data.Fields = append(data.Fields, fieldGateTest(listRoute, res.Name()+"."+field.Name(), field.WireName(), resourceMatrixListQuery(res), field.Feature.Constant, outlet))
	}

	return nil
}

// computedFeatureTests adds one computed resource's cases on one outlet, as
// resourceFeatureTests does for a table.
func (r *resourceGenerator) computedFeatureTests(data *featureTestsData, res *computedResource, outlet *routerOutlet) error {
	keys := res.PrimaryKeys()
	pkTypes := make([]pkParamType, 0, len(keys))
	for _, f := range keys {
		pkTypes = append(pkTypes, pkParamType{declared: f.Type(), underlying: f.UnderlyingType()})
	}
	routes, err := r.computedResourceRoutes(res, outlet.prefix)
	if err != nil {
		return err
	}
	var listRoute *generatedRoute
	for _, route := range routes {
		if route.HandlerType == ListHandler {
			listRoute = route
		}
		if res.Feature == nil {
			continue
		}
		test, err := gateTestOf(route, pkTypes, computedMatrixListQuery(res), res.Feature.Constant, outlet)
		if err != nil {
			return err
		}
		data.Routes = append(data.Routes, test)
	}
	if listRoute == nil {
		return nil
	}
	for _, field := range res.Fields {
		if field.Feature == nil || field.IsInputOnly() {
			continue
		}
		data.Fields = append(data.Fields, fieldGateTest(listRoute, res.Name()+"."+field.Name(), caser.ToCamel(field.Name()), computedMatrixListQuery(res), field.Feature.Constant, outlet))
	}

	return nil
}

// methodFeatureTests adds every gated method's route on every outlet serving it: a
// mutation called without a grant, so the on state is the method's own refusal.
func (r *resourceGenerator) methodFeatureTests(data *featureTestsData) {
	for _, method := range r.rpcMethods {
		if method.SuppressHandler || method.Feature == nil {
			continue
		}
		for _, outlet := range r.memberOutlets(&method.outletMembership) {
			route := r.rpcRoute(method, outlet.prefix)
			data.Routes = append(data.Routes, featureGateTest{
				Name:     authzCaseName(route.HandlerFunc, &outlet),
				Method:   httpMethodConst(route.Method),
				URL:      route.TestURL,
				Body:     emptyObjectBody,
				Constant: method.Feature.Constant,
			})
		}
	}
}

// gateTestOf renders one gated route's test: a query route with the permission that
// reaches data access, a mutation route with none.
func gateTestOf(route *generatedRoute, pkTypes []pkParamType, listQuery, constant string, outlet *routerOutlet) (featureGateTest, error) {
	if c, ok, err := queryRouteCase(route, pkTypes, listQuery); err != nil {
		return featureGateTest{}, err
	} else if ok {
		return featureGateTest{Name: authzCaseName(c.Name, outlet), Method: c.Method, URL: c.URL, Constant: constant, Permission: c.Permission}, nil
	}
	url := route.TestURL
	body := ""
	if route.HandlerType == PatchHandler {
		body = `[{"op":"remove","path":"/authz-test-key"}]`
	}

	return featureGateTest{Name: authzCaseName(route.HandlerFunc, outlet), Method: httpMethodConst(route.Method), URL: url, Body: body, Constant: constant}, nil
}

// fieldGateTest renders one gated field's test: the list route with the column asked
// for, and the matrix's order where the list needs one.
func fieldGateTest(listRoute *generatedRoute, name, wireName, listQuery, constant string, outlet *routerOutlet) featureFieldTest {
	url := listRoute.TestURL + "?" + columnsParam + "=" + wireName
	if listQuery != "" {
		url += "&" + listQuery
	}

	return featureFieldTest{Name: authzCaseName(name, outlet), URL: url, Constant: constant}
}

// The query parameter the field tests spell, as the resource package reads it.
const columnsParam = "columns"

// generateFeatureTests writes the handler tests package's zz_gen_features_test.go when
// anything is gated: every gated route and field driven in both states.
func (r *resourceGenerator) generateFeatureTests() error {
	if !r.hasGates() {
		return nil
	}
	data, err := r.featureTests()
	if err != nil {
		return err
	}

	begin := time.Now()
	destinationFilePath := filepath.Join(r.handlerTests.Dir(), generatedGoFileName(featureTestsOutputName))
	if err := r.writeFormattedGoFile(destinationFilePath, "featureTestsTemplate", featureTestsTemplate, data); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated feature gate tests file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

// featureFlagNameField is the flags' key on the wire, as the descriptor spells it.
const featureFlagNameField = "name"

// featureFlagsAPIResource is the FeatureFlags resource as the client descriptor carries
// it: a read-only global resource keyed by name, listed by name.
func featureFlagsAPIResource() *tsAPIResource {
	return &tsAPIResource{
		Name:        string(resource.FeatureFlagsResource),
		Property:    strcase.ToCamel(string(resource.FeatureFlagsResource)),
		Route:       resource.FeatureFlagsRoute,
		Scope:       accesstypes.GlobalPermissionScope,
		Keys:        []*tsAPIField{{Name: featureFlagNameField, Type: string(displayTypeString)}},
		Operations:  []string{"list", "read"},
		PageDefault: resource.DefaultPageSize,
		Order:       []*tsAPISort{{Field: featureFlagNameField, Direction: "asc"}},
	}
}

// setFeatureAPIMethod is the SetFeature method as the client descriptor carries it.
func setFeatureAPIMethod() *tsAPIMethod {
	return &tsAPIMethod{
		Name:     string(resource.SetFeatureMethod),
		Property: strcase.ToCamel(string(resource.SetFeatureMethod)),
		Route:    resource.SetFeatureRoute,
		Scope:    accesstypes.GlobalPermissionScope,
		Answers:  true,
	}
}
