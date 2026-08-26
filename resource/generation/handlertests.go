package generation

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

// runHandlerTestsGeneration emits the generated handler test suite: the emulator
// bootstrap (TestMain + prepareDatabase over the application's schema migrations) and
// the authorization-matrix tests over the generated test router. The target package
// hand-writes exactly one function the generated suite calls — newTestHandler — where
// the application's construction knowledge lives; everything else is derived from the
// generator's own configuration and route table.
func (r *resourceGenerator) runHandlerTestsGeneration() error {
	begin := time.Now()
	if err := removeGeneratedFiles(r.handlerTests.Dir(), prefix); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	migrationSources, err := r.testRelativeMigrationSources()
	if err != nil {
		return err
	}

	mainPath := filepath.Join(r.handlerTests.Dir(), generatedGoFileName("main_test"))
	if err := r.writeFormattedGoFile(mainPath, "handlerTestsMainTemplate", handlerTestsMainTemplate, &handlerTestsMainData{
		Source:           r.resource.Dir(),
		Package:          r.handlerTests.Package(),
		EmulatorVersion:  r.spannerEmulatorVersion,
		MigrationSources: migrationSources,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}

	cases, err := r.authzMatrixCases()
	if err != nil {
		return err
	}

	authzPath := filepath.Join(r.handlerTests.Dir(), generatedGoFileName("authz_test"))
	if err := r.writeFormattedGoFile(authzPath, "authzTestTemplate", authzTestTemplate, &authzTestData{
		Source:  r.resource.Dir(),
		Package: r.handlerTests.Package(),
		Cases:   cases,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}

	log.Printf("Generated handler test files in %s: %s", time.Since(begin), r.handlerTests.Dir())

	return nil
}

// testRelativeMigrationSources rewrites the configured migration source URLs so they
// resolve from inside the handler-tests directory: the generator's file:// sources are
// relative to the application root, where the generator runs.
func (r *resourceGenerator) testRelativeMigrationSources() ([]string, error) {
	rel, err := filepath.Rel(r.handlerTests.Dir(), ".")
	if err != nil {
		return nil, errors.Wrap(err, "filepath.Rel()")
	}

	sources := make([]string, 0, len(r.migrationSourceURLs))
	for _, src := range r.migrationSourceURLs {
		path, ok := strings.CutPrefix(src, "file://")
		if !ok || filepath.IsAbs(path) {
			sources = append(sources, src)

			continue
		}
		sources = append(sources, "file://"+filepath.ToSlash(filepath.Join(rel, path)))
	}

	return sources, nil
}

// authzMatrixCases derives one denied/granted case pair per generated list and read
// route — every route, without exception: an endpoint the matrix cannot cover is a
// generation error, never a silent skip. Domain-scoped routes use the router test's
// domain value, which the application's newTestHandler must recognize in its
// DomainExists (the generated suite documents this contract).
func (r *resourceGenerator) authzMatrixCases() (cases []authzCase, err error) {
	appendRoute := func(route *generatedRoute, pkTypes []pkParamType) error {
		var permission string
		switch route.HandlerType {
		case ListHandler:
			permission = "List"
		case ReadHandler:
			permission = "Read"
		default:
			return nil // mutation endpoints join the matrix with the body contract
		}

		pkParams := route.TestParams
		url := route.Path
		if route.DomainScoped {
			// prependDomainTestParam puts the domain parameter first.
			url = strings.Replace(url, "{"+pkParams[0].Key+"}", domainTestValue, 1)
			pkParams = pkParams[1:]
		}
		if route.HandlerType == ReadHandler {
			if len(pkParams) != len(pkTypes) {
				return errors.Newf("route %s: %d route parameters but %d primary keys", route.Path, len(pkParams), len(pkTypes))
			}
			for i, p := range pkParams {
				value, ok := authzParamValue(pkTypes[i])
				if !ok {
					return errors.Newf("authorization matrix: route %s has no parseable placeholder value for primary-key type %s; add the type to authzParamValue or suppress the route", route.Path, pkTypes[i].declared)
				}
				url = strings.Replace(url, "{"+p.Key+"}", value, 1)
			}
		}

		cases = append(cases, authzCase{
			Name:       route.HandlerFunc,
			Method:     httpMethodConst(route.Method),
			URL:        url,
			Permission: permission,
		})

		return nil
	}

	for _, res := range r.resources {
		if res.RoutingDisabled() {
			continue
		}
		var pkTypes []pkParamType
		for _, f := range res.PrimaryKeys() {
			pkTypes = append(pkTypes, pkParamType{declared: f.Type(), underlying: f.UnderlyingType()})
		}
		for _, ht := range resourceEndpoints(res) {
			route, err := r.resourceRoute(res, ht)
			if err != nil {
				return nil, err
			}
			if err := appendRoute(route, pkTypes); err != nil {
				return nil, err
			}
		}
	}

	if r.genComputedResources {
		for _, res := range r.computedResources {
			var pkTypes []pkParamType
			for _, f := range res.PrimaryKeys() {
				pkTypes = append(pkTypes, pkParamType{declared: f.Type(), underlying: f.UnderlyingType()})
			}
			routes, err := r.computedResourceRoutes(res)
			if err != nil {
				return nil, err
			}
			for _, route := range routes {
				if err := appendRoute(route, pkTypes); err != nil {
					return nil, err
				}
			}
		}
	}

	return cases, nil
}

// pkParamType carries a primary key's declared Go type and its underlying type, so
// named key types (enumerated string IDs and the like) resolve to a parseable value.
type pkParamType struct {
	declared   string
	underlying string
}

// authzParamValue returns a placeholder route-parameter value that parses under the
// primary key's Go type, so a granted request reaches data access (404 on the empty
// schema) instead of dying on parameter decoding.
func authzParamValue(t pkParamType) (string, bool) {
	switch {
	case strings.Contains(t.declared, "UUID"):
		return "00000000-0000-0000-0000-000000000001", true
	case t.declared == stringGoType || t.underlying == stringGoType:
		return "authz-test-key", true
	case strings.HasPrefix(t.declared, "int") || strings.HasPrefix(t.underlying, "int"):
		return "1", true
	case t.declared == boolGoType || t.underlying == boolGoType:
		return "true", true
	case t.declared == "civil.Date":
		return "2000-01-01", true
	default:
		return "", false
	}
}

// httpMethodConst renders a route method as its net/http constant expression, e.g.
// GET -> http.MethodGet.
func httpMethodConst(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		return "http.Method" + caser.ToPascal(method)
	default:
		panic(fmt.Sprintf("unexpected route method: %q", method))
	}
}
