package generation

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"time"

	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

// domainTestValue is the domain route parameter value used in generated router tests.
const domainTestValue = "testDomain"

func (r *resourceGenerator) runRouteGeneration() error {
	begin := time.Now()
	if err := removeGeneratedFiles(r.router.Dir(), prefix); err != nil {
		return err
	}

	if err := r.validateDomainSegmentResources(); err != nil {
		return err
	}

	outlets := r.allOutlets()
	outletRoutes := make([]*outletRouteData, len(outlets))
	for i, outlet := range outlets {
		outletRoutes[i] = &outletRouteData{
			Name:                    outlet.name,
			Suffix:                  outlet.suffix(),
			RoutesMap:               make(map[string][]*generatedRoute),
			ConsolidatedHandlerFunc: fmt.Sprintf("Patch%sResources", outlet.suffix()),
			ConsolidatedPath:        fmt.Sprintf("/%s/%s", outlet.prefix, r.ConsolidatedRoute),
		}
	}

	constResources, routerTestRoutes, err := r.accumulateResourceRoutes(outlets, outletRoutes)
	if err != nil {
		return err
	}

	constComputedResources, computedTestRoutes, err := r.accumulateComputedRoutes(outlets, outletRoutes)
	if err != nil {
		return err
	}
	routerTestRoutes = append(routerTestRoutes, computedTestRoutes...)

	r.accumulateRPCRoutes(outlets, outletRoutes)

	stubDomainGuard := false
	for _, outlet := range outletRoutes {
		for _, routes := range outlet.RoutesMap {
			for _, route := range routes {
				if route.DomainScoped {
					outlet.HasDomainScopedRoutes = true
					stubDomainGuard = true
				}
			}
		}
	}

	defaultOutlet := outletRoutes[0]
	extraOutlets := outletRoutes[1:]

	negativeTests, err := r.negativeRouterTests(outlets)
	if err != nil {
		return err
	}

	data := routerFileData{
		Source:                 r.resource.Dir(),
		Package:                r.router.Package(),
		LocalPackageImports:    r.localPackageImports(),
		RoutesMap:              defaultOutlet.RoutesMap,
		ConstResources:         constResources,
		ConstComputedResources: constComputedResources,
		RouterTestRoutes:       routerTestRoutes,
		HasConsolidatedHandler: defaultOutlet.HasConsolidatedHandler,
		HasDomainScoped:        r.hasDomainScoped(),
		HasDomainScopedRoutes:  defaultOutlet.HasDomainScopedRoutes,
		StubDomainGuard:        stubDomainGuard,
		DomainRouteParam:       r.domainRouteParam,
		RoutePrefix:            r.routePrefix,
		ConsolidatedRoute:      r.ConsolidatedRoute,
		ExtraOutlets:           extraOutlets,
		ExtraStubHandlerFuncs:  extraStubHandlerFuncs(defaultOutlet, extraOutlets),
		NegativeRouterTests:    negativeTests,
	}

	routesDestination := filepath.Join(r.router.Dir(), generatedGoFileName(routesOutputName))
	if err := r.writeFormattedGoFile(routesDestination, "routesTemplate", routesTemplate, data); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated routes file in %s: %s\n", time.Since(begin), routesDestination)

	routerTestsDestination := filepath.Join(r.router.Dir(), generatedGoFileName(routerTestOutputName))
	begin = time.Now()
	if err := r.writeFormattedGoFile(routerTestsDestination, "routerTestTemplate", routerTestTemplate, data); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated router tests file in %s: %s\n", time.Since(begin), routerTestsDestination)

	return nil
}

// accumulateResourceRoutes builds every routed resource's routes into each member
// outlet's RoutesMap, returning the read-handler resources (for the param consts) and
// the dispatch-test routes.
func (r *resourceGenerator) accumulateResourceRoutes(outlets []routerOutlet, outletRoutes []*outletRouteData) (constResources []*resourceInfo, routerTestRoutes []*generatedRoute, err error) {
	constResources = make([]*resourceInfo, 0, len(r.resources))
	routerTestRoutes = make([]*generatedRoute, 0, len(r.resources))
	for _, res := range r.resources {
		handlerTypes := resourceEndpoints(res)

		if slices.Contains(handlerTypes, ReadHandler) {
			constResources = append(constResources, res)
		}

		if res.RoutingDisabled() {
			continue
		}

		for i, outlet := range outlets {
			if !res.OnOutlet(outlet.name) {
				continue
			}

			if hasConsolidatedHandler(res) {
				outletRoutes[i].HasConsolidatedHandler = true
			}

			for _, ht := range handlerTypes {
				route, err := r.resourceRoute(res, ht, outlet.prefix)
				if err != nil {
					return nil, nil, err
				}

				outletRoutes[i].RoutesMap[res.Name()] = append(outletRoutes[i].RoutesMap[res.Name()], route)
				routerTestRoutes = append(routerTestRoutes, route)
			}
		}
	}

	return constResources, routerTestRoutes, nil
}

// accumulateComputedRoutes builds every routed computed resource's routes into each
// member outlet's RoutesMap, returning the read-handler resources (for the param
// consts) and the dispatch-test routes.
func (r *resourceGenerator) accumulateComputedRoutes(outlets []routerOutlet, outletRoutes []*outletRouteData) (constComputedResources []*computedResource, routerTestRoutes []*generatedRoute, err error) {
	constComputedResources = make([]*computedResource, 0, len(r.computedResources))
	routerTestRoutes = make([]*generatedRoute, 0, len(r.computedResources))
	for _, res := range r.computedResources {
		if !res.SuppressReadHandler {
			constComputedResources = append(constComputedResources, res)
		}

		if res.RoutingDisabled() {
			continue
		}

		for i, outlet := range outlets {
			if !res.OnOutlet(outlet.name) {
				continue
			}

			routes, err := r.computedResourceRoutes(res, outlet.prefix)
			if err != nil {
				return nil, nil, err
			}
			outletRoutes[i].RoutesMap[res.Name()] = append(outletRoutes[i].RoutesMap[res.Name()], routes...)
			routerTestRoutes = append(routerTestRoutes, routes...)
		}
	}

	return constComputedResources, routerTestRoutes, nil
}

// accumulateRPCRoutes builds every unsuppressed RPC method's route into each member
// outlet's RoutesMap. RPC routes carry no dispatch-test entries (the dispatch test
// covers resource and computed routes).
func (r *resourceGenerator) accumulateRPCRoutes(outlets []routerOutlet, outletRoutes []*outletRouteData) {
	if !r.genRPCMethods {
		return
	}

	for _, rpcStruct := range r.rpcMethods {
		if rpcStruct.SuppressHandler {
			continue
		}

		for i, outlet := range outlets {
			if !rpcStruct.OnOutlet(outlet.name) {
				continue
			}

			outletRoutes[i].RoutesMap[rpcStruct.Name()] = []*generatedRoute{r.rpcRoute(rpcStruct, outlet.prefix)}
		}
	}
}

// rpcRoute builds the route for an RPC method under the outlet route prefix: POST at
// the kebab-cased method name, under the domain segment pair for domain-scoped methods.
func (r *resourceGenerator) rpcRoute(rpcStruct *rpcMethodInfo, routePrefix string) *generatedRoute {
	path := fmt.Sprintf("/%s/%s", routePrefix, strcase.ToKebab(rpcStruct.Name()))
	testPath := path
	if rpcStruct.IsDomainScoped() {
		path = fmt.Sprintf("/%s/%s/{%s}/%s", routePrefix, r.domainRouteSegment, r.domainRouteParam, strcase.ToKebab(rpcStruct.Name()))
		testPath = fmt.Sprintf("/%s/%s/%s/%s", routePrefix, r.domainRouteSegment, domainTestValue, strcase.ToKebab(rpcStruct.Name()))
	}

	return &generatedRoute{
		Method:       http.MethodPost,
		Path:         path,
		HandlerFunc:  rpcStruct.Name(),
		DomainScoped: rpcStruct.IsDomainScoped(),
		TestURL:      testPath,
	}
}

// resourceRoute builds the route for one handler type of a resource under the outlet
// route prefix, including read-route primary-key params and, for domain-scoped
// resources, the domain segment pair.
func (r *resourceGenerator) resourceRoute(res *resourceInfo, ht HandlerType, routePrefix string) (*generatedRoute, error) {
	basePath, testBasePath := r.routeBasePaths(res.Name(), res.IsDomainScoped(), routePrefix)
	route := &generatedRoute{
		Method:       ht.method(),
		Path:         basePath,
		HandlerFunc:  r.handlerName(res.Name(), ht),
		HandlerType:  ht,
		DomainScoped: res.IsDomainScoped(),
		TestURL:      testBasePath,
	}
	if ht == ReadHandler {
		if res.HasCompoundPrimaryKey() {
			var pkNames []string
			for _, field := range res.PrimaryKeys() {
				pkNames = append(pkNames, field.Name())
			}
			route.TestParams = readRouteTestParams(res.Name(), pkNames)
		} else {
			route.TestParams = []routeTestParam{{
				Key:   strcase.ToGoCamel(res.Name() + "ID"),
				Value: strcase.ToGoCamel(fmt.Sprintf("test%sID", caser.ToPascal(res.Name()))),
			}}
		}
		route.appendParamsToPaths()
	}
	if res.IsDomainScoped() {
		if err := r.validateDomainParamCollision(route.TestParams, res.Name()); err != nil {
			return nil, err
		}
	}
	route.prependDomainTestParam(r.domainRouteParam)

	return route, nil
}

// computedResourceRoutes builds the read and list routes for a computed resource under
// the outlet route prefix, honoring its handler suppressions and, for domain-scoped
// resources, the domain segment pair.
func (r *resourceGenerator) computedResourceRoutes(res *computedResource, routePrefix string) ([]*generatedRoute, error) {
	basePath, testBasePath := r.routeBasePaths(res.Name(), res.IsDomainScoped(), routePrefix)

	var routes []*generatedRoute
	if !res.SuppressListHandler {
		route := &generatedRoute{
			Method:       ListHandler.method(),
			Path:         basePath,
			HandlerFunc:  r.handlerName(res.Name(), ListHandler),
			HandlerType:  ListHandler,
			DomainScoped: res.IsDomainScoped(),
			TestURL:      testBasePath,
		}
		route.prependDomainTestParam(r.domainRouteParam)

		routes = append(routes, route)
	}

	if !res.SuppressReadHandler {
		pkNames := make([]string, 0, len(res.PrimaryKeys()))
		for _, field := range res.PrimaryKeys() {
			pkNames = append(pkNames, field.Name())
		}

		route := &generatedRoute{
			Method:       ReadHandler.method(),
			Path:         basePath,
			HandlerFunc:  r.handlerName(res.Name(), ReadHandler),
			HandlerType:  ReadHandler,
			DomainScoped: res.IsDomainScoped(),
			TestURL:      testBasePath,
			TestParams:   readRouteTestParams(res.Name(), pkNames),
		}
		route.appendParamsToPaths()
		if res.IsDomainScoped() {
			if err := r.validateDomainParamCollision(route.TestParams, res.Name()); err != nil {
				return nil, err
			}
		}
		route.prependDomainTestParam(r.domainRouteParam)

		routes = append(routes, route)
	}

	return routes, nil
}

// extraStubHandlerFuncs returns the handler funcs the generated router-test stub needs
// beyond the default outlet's: handler methods served only under extra outlets, plus
// each extra outlet's consolidated dispatcher. Sorted for deterministic output.
func extraStubHandlerFuncs(defaultOutlet *outletRouteData, extraOutlets []*outletRouteData) []string {
	seen := make(map[string]bool)
	for _, routes := range defaultOutlet.RoutesMap {
		for _, route := range routes {
			seen[route.HandlerFunc] = true
		}
	}

	var funcs []string
	add := func(handlerFunc string) {
		if !seen[handlerFunc] {
			seen[handlerFunc] = true
			funcs = append(funcs, handlerFunc)
		}
	}
	for _, outlet := range extraOutlets {
		for _, routes := range outlet.RoutesMap {
			for _, route := range routes {
				add(route.HandlerFunc)
			}
		}
		if outlet.HasConsolidatedHandler {
			add(outlet.ConsolidatedHandlerFunc)
		}
	}
	slices.Sort(funcs)

	return funcs
}

// negativeRouterTests builds the outlet-isolation cases: for every routed resource,
// computed resource, RPC method, and consolidated dispatcher, the URL it would occupy
// under each outlet it is NOT attached to, which the generated test requires to fall
// through to 404 with no handler dispatched. Empty without extra outlets — one outlet
// has nothing to be isolated from.
func (r *resourceGenerator) negativeRouterTests(outlets []routerOutlet) ([]negativeRouterTest, error) {
	if len(outlets) < 2 {
		return nil, nil
	}

	var tests []negativeRouterTest
	for _, outlet := range outlets {
		outletTests, err := r.negativeTestsForOutlet(outlet)
		if err != nil {
			return nil, err
		}
		tests = append(tests, outletTests...)
	}

	return tests, nil
}

// negativeTestsForOutlet builds one outlet's isolation cases: the URLs of everything
// routed that is NOT attached to the outlet, addressed under the outlet's prefix.
func (r *resourceGenerator) negativeTestsForOutlet(outlet routerOutlet) ([]negativeRouterTest, error) {
	var tests []negativeRouterTest
	addRoute := func(route *generatedRoute) {
		for _, method := range route.TestMethods() {
			tests = append(tests, negativeRouterTest{Method: method, URL: route.TestURL})
		}
	}

	anyConsolidated, outletHasConsolidated := false, false
	for _, res := range r.resources {
		if res.RoutingDisabled() {
			continue
		}
		if hasConsolidatedHandler(res) {
			anyConsolidated = true
			if res.OnOutlet(outlet.name) {
				outletHasConsolidated = true
			}
		}
		if res.OnOutlet(outlet.name) {
			continue
		}
		for _, ht := range resourceEndpoints(res) {
			route, err := r.resourceRoute(res, ht, outlet.prefix)
			if err != nil {
				return nil, err
			}
			addRoute(route)
		}
	}

	for _, res := range r.computedResources {
		if res.RoutingDisabled() || res.OnOutlet(outlet.name) {
			continue
		}
		routes, err := r.computedResourceRoutes(res, outlet.prefix)
		if err != nil {
			return nil, err
		}
		for _, route := range routes {
			addRoute(route)
		}
	}

	if r.genRPCMethods {
		for _, rpcStruct := range r.rpcMethods {
			if rpcStruct.SuppressHandler || rpcStruct.OnOutlet(outlet.name) {
				continue
			}
			addRoute(r.rpcRoute(rpcStruct, outlet.prefix))
		}
	}

	if anyConsolidated && !outletHasConsolidated {
		tests = append(tests, negativeRouterTest{
			Method: httpMethodConstant(http.MethodPatch),
			URL:    fmt.Sprintf("/%s/%s", outlet.prefix, r.ConsolidatedRoute),
		})
	}

	return tests, nil
}

// deriveDomainRouteParam resolves the domain route parameter after parsing. When a
// resource's route name equals the domain route segment (the tenant-record pattern),
// chi permits one wildcard name per tree position, so the parameter must be that
// resource's read-route parameter — it is derived as ToGoCamel(name+pkName) rather
// than configured. A domain-scoped match lives under the segment pair itself and
// shares no position with it, and without any match the name is a cosmetic pattern
// label; both keep the default "domain". Compound-key matches also keep the default:
// validateDomainSegmentResources rejects them structurally.
func (r *resourceGenerator) deriveDomainRouteParam() {
	derive := func(name string, domainScoped, compoundPK bool, pkName string) {
		if strcase.ToKebab(r.pluralize(name)) != r.domainRouteSegment {
			return
		}
		if domainScoped || compoundPK || pkName == "" {
			return
		}
		r.domainRouteParam = strcase.ToGoCamel(name + pkName)
	}

	for _, res := range r.resources {
		if pk := res.PrimaryKey(); pk != nil {
			derive(res.Name(), res.IsDomainScoped(), res.HasCompoundPrimaryKey(), pk.Name())
		}
	}
	for _, res := range r.computedResources {
		if pk := res.PrimaryKey(); pk != nil {
			derive(res.Name(), res.IsDomainScoped(), res.HasCompoundPrimaryKey(), pk.Name())
		}
	}
}

// validateDomainParamCollision rejects a domain-scoped route whose primary-key route
// parameters collide with the domain route parameter name — chi panics on duplicate
// param names within one pattern, so fail at generate time with a fix instead.
func (r *resourceGenerator) validateDomainParamCollision(params []routeTestParam, resourceName string) error {
	for _, p := range params {
		if p.Key == r.domainRouteParam {
			return errors.Newf("resource %s: primary-key route parameter %q collides with the domain route parameter; rename the primary key or choose a different segment via WithDomainRoute()", resourceName, r.domainRouteParam)
		}
	}

	return nil
}

// hasDomainScoped reports whether any resource, computed resource, or RPC method is
// domain-scoped. Scope is checked regardless of routing/handler suppression so the
// Domain route-parameter const is emitted whenever generated code could reference it.
func (r *resourceGenerator) hasDomainScoped() bool {
	for _, res := range r.resources {
		if res.IsDomainScoped() {
			return true
		}
	}
	for _, res := range r.computedResources {
		if res.IsDomainScoped() {
			return true
		}
	}
	for _, rpcStruct := range r.rpcMethods {
		if rpcStruct.IsDomainScoped() {
			return true
		}
	}

	return false
}

// routeBasePaths returns the route path and its router-test URL for a resource under
// the outlet route prefix, inserting the {domain} segment (and its test value) for
// domain-scoped resources.
func (r *resourceGenerator) routeBasePaths(resourceName string, domainScoped bool, routePrefix string) (basePath, testBasePath string) {
	kebab := strcase.ToKebab(r.pluralize(resourceName))
	if domainScoped {
		return fmt.Sprintf("/%s/%s/{%s}/%s", routePrefix, r.domainRouteSegment, r.domainRouteParam, kebab),
			fmt.Sprintf("/%s/%s/%s/%s", routePrefix, r.domainRouteSegment, domainTestValue, kebab)
	}

	basePath = fmt.Sprintf("/%s/%s", routePrefix, kebab)

	return basePath, basePath
}

// readRouteTestParams returns one route parameter per primary-key field for
// addressing a read route in the generated router tests.
func readRouteTestParams(resourceName string, pkNames []string) []routeTestParam {
	params := make([]routeTestParam, 0, len(pkNames))
	for _, pk := range pkNames {
		params = append(params, routeTestParam{
			Key:   strcase.ToGoCamel(resourceName + pk),
			Value: fmt.Sprintf("test%s%s", caser.ToPascal(resourceName), pk),
		})
	}

	return params
}
