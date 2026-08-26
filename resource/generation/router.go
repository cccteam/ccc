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

	var hasConsolidatedHandlers bool
	constResources := make([]*resourceInfo, 0, len(r.resources))
	routerTestRoutes := make([]*generatedRoute, 0, len(r.resources)+len(r.computedResources))
	generatedRoutesMap := make(map[string][]*generatedRoute)
	for _, res := range r.resources {
		handlerTypes := resourceEndpoints(res)

		if slices.Contains(handlerTypes, ReadHandler) {
			constResources = append(constResources, res)
		}

		if res.RoutingDisabled() {
			continue
		}

		if hasConsolidatedHandler(res) {
			hasConsolidatedHandlers = true
		}

		for _, ht := range handlerTypes {
			route, err := r.resourceRoute(res, ht)
			if err != nil {
				return err
			}

			generatedRoutesMap[res.Name()] = append(generatedRoutesMap[res.Name()], route)
			routerTestRoutes = append(routerTestRoutes, route)
		}
	}

	constComputedResources := make([]*computedResource, 0, len(r.computedResources))
	for _, res := range r.computedResources {
		if !res.SuppressReadHandler {
			constComputedResources = append(constComputedResources, res)
		}

		if res.RoutingDisabled() {
			continue
		}

		routes, err := r.computedResourceRoutes(res)
		if err != nil {
			return err
		}
		generatedRoutesMap[res.Name()] = append(generatedRoutesMap[res.Name()], routes...)
		routerTestRoutes = append(routerTestRoutes, routes...)
	}

	if r.genRPCMethods {
		for _, rpcStruct := range r.rpcMethods {
			if rpcStruct.SuppressHandler {
				continue
			}

			path := fmt.Sprintf("/%s/%s", r.routePrefix, strcase.ToKebab(rpcStruct.Name()))
			if rpcStruct.IsDomainScoped() {
				path = fmt.Sprintf("/%s/%s/{%s}/%s", r.routePrefix, r.domainRouteSegment, r.domainRouteParam, strcase.ToKebab(rpcStruct.Name()))
			}

			generatedRoutesMap[rpcStruct.Name()] = []*generatedRoute{{
				Method:       http.MethodPost,
				Path:         path,
				HandlerFunc:  rpcStruct.Name(),
				DomainScoped: rpcStruct.IsDomainScoped(),
			}}
		}
	}

	hasDomainScopedRoutes := false
	for _, routes := range generatedRoutesMap {
		for _, route := range routes {
			if route.DomainScoped {
				hasDomainScopedRoutes = true
			}
		}
	}

	data := routerFileData{
		Source:                 r.resource.Dir(),
		Package:                r.router.Package(),
		LocalPackageImports:    r.localPackageImports(),
		RoutesMap:              generatedRoutesMap,
		ConstResources:         constResources,
		ConstComputedResources: constComputedResources,
		RouterTestRoutes:       routerTestRoutes,
		HasConsolidatedHandler: hasConsolidatedHandlers,
		HasDomainScoped:        r.hasDomainScoped(),
		HasDomainScopedRoutes:  hasDomainScopedRoutes,
		DomainRouteParam:       r.domainRouteParam,
		RoutePrefix:            r.routePrefix,
		ConsolidatedRoute:      r.ConsolidatedRoute,
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

// resourceRoute builds the route for one handler type of a resource, including read-route
// primary-key params and, for domain-scoped resources, the domain segment pair.
func (r *resourceGenerator) resourceRoute(res *resourceInfo, ht HandlerType) (*generatedRoute, error) {
	basePath, testBasePath := r.routeBasePaths(res.Name(), res.IsDomainScoped())
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

// computedResourceRoutes builds the read and list routes for a computed resource,
// honoring its handler suppressions and, for domain-scoped resources, the domain
// segment pair.
func (r *resourceGenerator) computedResourceRoutes(res *computedResource) ([]*generatedRoute, error) {
	basePath, testBasePath := r.routeBasePaths(res.Name(), res.IsDomainScoped())

	var routes []*generatedRoute
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

	return routes, nil
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

// routeBasePaths returns the route path and its router-test URL for a resource,
// inserting the {domain} segment (and its test value) for domain-scoped resources.
func (r *resourceGenerator) routeBasePaths(resourceName string, domainScoped bool) (basePath, testBasePath string) {
	kebab := strcase.ToKebab(r.pluralize(resourceName))
	if domainScoped {
		return fmt.Sprintf("/%s/%s/{%s}/%s", r.routePrefix, r.domainRouteSegment, r.domainRouteParam, kebab),
			fmt.Sprintf("/%s/%s/%s/%s", r.routePrefix, r.domainRouteSegment, domainTestValue, kebab)
	}

	basePath = fmt.Sprintf("/%s/%s", r.routePrefix, kebab)

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
