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

func (r *resourceGenerator) runRouteGeneration() error {
	begin := time.Now()
	if err := removeGeneratedFiles(r.router.Dir(), prefix); err != nil {
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
			basePath := fmt.Sprintf("/%s/%s", r.routePrefix, strcase.ToKebab(r.pluralize(res.Name())))
			route := &generatedRoute{
				Method:      ht.method(),
				Path:        basePath,
				HandlerFunc: r.handlerName(res.Name(), ht),
				HandlerType: ht,
				TestURL:     basePath,
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

		basePath := fmt.Sprintf("/%s/%s", r.routePrefix, strcase.ToKebab(r.pluralize(res.Name())))
		if !res.SuppressReadHandler {
			var pkNames []string
			for _, field := range res.PrimaryKeys() {
				pkNames = append(pkNames, field.Name())
			}

			route := &generatedRoute{
				Method:      ReadHandler.method(),
				Path:        basePath,
				HandlerFunc: r.handlerName(res.Name(), ReadHandler),
				HandlerType: ReadHandler,
				TestURL:     basePath,
				TestParams:  readRouteTestParams(res.Name(), pkNames),
			}
			route.appendParamsToPaths()

			generatedRoutesMap[res.Name()] = append(generatedRoutesMap[res.Name()], route)
			routerTestRoutes = append(routerTestRoutes, route)
		}

		if !res.SuppressListHandler {
			route := &generatedRoute{
				Method:      ListHandler.method(),
				Path:        basePath,
				HandlerFunc: r.handlerName(res.Name(), ListHandler),
				HandlerType: ListHandler,
				TestURL:     basePath,
			}

			generatedRoutesMap[res.Name()] = append(generatedRoutesMap[res.Name()], route)
			routerTestRoutes = append(routerTestRoutes, route)
		}
	}

	if r.genRPCMethods {
		for _, rpcStruct := range r.rpcMethods {
			if rpcStruct.SuppressHandler {
				continue
			}

			generatedRoutesMap[rpcStruct.Name()] = []*generatedRoute{{
				Method:      http.MethodPost,
				Path:        fmt.Sprintf("/%s/%s", r.routePrefix, strcase.ToKebab(rpcStruct.Name())),
				HandlerFunc: rpcStruct.Name(),
			}}
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
