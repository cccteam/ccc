package generation

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/ettle/strcase"
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

// authzMatrixCases derives the authorization cases for every generated route — every
// route, without exception: an endpoint the matrix cannot cover is a generation error,
// never a silent skip. Domain-scoped routes use the router test's domain value, which
// the application's newTestHandler must recognize in its DomainExists (the generated
// suite documents this contract).
//
// Query routes expand to a denied/granted pair. Mutation routes (patch operations,
// consolidated dispatch arms, RPC methods) expand to denied-only cases: every
// mutation's permission check runs at its enforcement gate before required-field
// validation, defaults, or row reads — buffer time for patch sets, decode time for RPC
// — so a minimal body reaches the gate and the denied case must fail closed with 403.
// Success paths need generator-synthesized valid bodies and are deferred.
func (r *resourceGenerator) authzMatrixCases() ([]authzCase, error) {
	cases, err := r.resourceAuthzCases()
	if err != nil {
		return nil, err
	}

	computed, err := r.computedAuthzCases()
	if err != nil {
		return nil, err
	}
	cases = append(cases, computed...)

	consolidated, err := r.consolidatedAuthzCases()
	if err != nil {
		return nil, err
	}
	cases = append(cases, consolidated...)

	return append(cases, r.rpcAuthzCases()...), nil
}

// queryRouteCase builds the denied/granted case for a list or read route; ok is false
// for every other handler type.
func queryRouteCase(route *generatedRoute, pkTypes []pkParamType) (c authzCase, ok bool, err error) {
	var permission string
	switch route.HandlerType {
	case ListHandler:
		permission = "List"
	case ReadHandler:
		permission = "Read"
	default:
		return authzCase{}, false, nil
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
			return authzCase{}, false, errors.Newf("route %s: %d route parameters but %d primary keys", route.Path, len(pkParams), len(pkTypes))
		}
		for i, p := range pkParams {
			value, ok := authzParamValue(pkTypes[i])
			if !ok {
				return authzCase{}, false, errors.Newf("authorization matrix: route %s has no parseable placeholder value for primary-key type %s; add the type to authzParamValue or suppress the route", route.Path, pkTypes[i].declared)
			}
			url = strings.Replace(url, "{"+p.Key+"}", value, 1)
		}
	}

	return authzCase{
		Name:       route.HandlerFunc,
		Method:     httpMethodConst(route.Method),
		URL:        url,
		Permission: permission,
	}, true, nil
}

// resourceAuthzCases covers each resource's own routes: query pairs for list and read,
// denied-only operation cases for the patch route.
func (r *resourceGenerator) resourceAuthzCases() (cases []authzCase, err error) {
	for _, res := range r.resources {
		if res.RoutingDisabled() {
			continue
		}
		pkTypes := resourcePKTypes(res)
		for _, ht := range resourceEndpoints(res) {
			route, err := r.resourceRoute(res, ht)
			if err != nil {
				return nil, err
			}
			if ht == PatchHandler {
				opCases, err := patchOpCases(route.HandlerFunc, route.TestURL, "", res, pkTypes)
				if err != nil {
					return nil, err
				}
				cases = append(cases, opCases...)

				continue
			}
			c, ok, err := queryRouteCase(route, pkTypes)
			if err != nil {
				return nil, err
			}
			if ok {
				cases = append(cases, c)
			}
		}
	}

	return cases, nil
}

func (r *resourceGenerator) computedAuthzCases() (cases []authzCase, err error) {
	if !r.genComputedResources {
		return nil, nil
	}

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
			c, ok, err := queryRouteCase(route, pkTypes)
			if err != nil {
				return nil, err
			}
			if ok {
				cases = append(cases, c)
			}
		}
	}

	return cases, nil
}

// consolidatedAuthzCases covers the consolidated patch route: one endpoint dispatching
// per-resource, so denied-only operation cases per consolidated resource prove each
// dispatch arm checks its own permission. The operation path carries the resource
// segment (and the domain pair for domain-scoped resources) ahead of the record key.
func (r *resourceGenerator) consolidatedAuthzCases() (cases []authzCase, err error) {
	for _, res := range r.resources {
		if res.RoutingDisabled() || !hasConsolidatedHandler(res) {
			continue
		}

		opPathPrefix := "/" + strcase.ToKebab(r.pluralize(res.Name()))
		if res.IsDomainScoped() {
			opPathPrefix = fmt.Sprintf("/%s/%s%s", r.domainRouteSegment, domainTestValue, opPathPrefix)
		}

		opCases, err := patchOpCases(
			"PatchResources "+res.Name(),
			fmt.Sprintf("/%s/%s", r.routePrefix, r.ConsolidatedRoute),
			opPathPrefix,
			res,
			resourcePKTypes(res),
		)
		if err != nil {
			return nil, err
		}
		cases = append(cases, opCases...)
	}

	return cases, nil
}

// rpcAuthzCases covers the RPC method routes. The RPC decoder checks the method
// permission after parsing the body (the parsed request is what a data-dependent rule
// evaluates against) and before executing anything; an empty object reaches that check
// because RPC request structs carry no parse-time validation.
func (r *resourceGenerator) rpcAuthzCases() (cases []authzCase) {
	if !r.genRPCMethods {
		return nil
	}

	for _, rpcStruct := range r.rpcMethods {
		if rpcStruct.SuppressHandler {
			continue
		}

		route := r.rpcRoute(rpcStruct)
		cases = append(cases, authzCase{
			Name:       route.HandlerFunc,
			Method:     httpMethodConst(route.Method),
			URL:        route.TestURL,
			Body:       "{}",
			DeniedOnly: true,
		})
	}

	return cases
}

func resourcePKTypes(res *resourceInfo) []pkParamType {
	var pkTypes []pkParamType
	for _, f := range res.PrimaryKeys() {
		pkTypes = append(pkTypes, pkParamType{declared: f.Type(), underlying: f.UnderlyingType()})
	}

	return pkTypes
}

// patchOpCases builds the denied-only cases for one patch endpoint: one per operation
// type, each carrying the minimal body that reaches the operation's buffer-time
// permission check. Create sends an empty value (the check precedes the required-field
// rejection), update one synthesized field (an empty update is a no-op that skips the
// check), delete no value at all. opPathPrefix is the consolidated dispatch prefix
// ("" for a resource's own patch route).
func patchOpCases(name, url, opPathPrefix string, res *resourceInfo, pkTypes []pkParamType) ([]authzCase, error) {
	keyPath, err := authzKeyPath(res, pkTypes)
	if err != nil {
		return nil, err
	}
	keyPath = opPathPrefix + keyPath

	createPath := opPathPrefix
	if !res.PrimaryKeyIsGeneratedUUID() {
		// The handler requires the client-supplied key on create operations.
		createPath = keyPath
	}
	createOp := `{"op":"add","value":{}}`
	if createPath != "" {
		createOp = fmt.Sprintf(`{"op":"add","path":%q,"value":{}}`, createPath)
	}

	field, value, patchable, err := updateProbeField(res)
	if err != nil {
		return nil, err
	}

	method := httpMethodConst(http.MethodPatch)

	cases := []authzCase{{
		Name:       name + " create",
		Method:     method,
		URL:        url,
		Body:       "[" + createOp + "]",
		DeniedOnly: true,
	}}
	if patchable {
		cases = append(cases, authzCase{
			Name:       name + " update",
			Method:     method,
			URL:        url,
			Body:       fmt.Sprintf(`[{"op":"patch","path":%q,"value":{%q:%s}}]`, keyPath, field, value),
			DeniedOnly: true,
		})
	}

	return append(cases, authzCase{
		Name:       name + " delete",
		Method:     method,
		URL:        url,
		Body:       fmt.Sprintf(`[{"op":"remove","path":%q}]`, keyPath),
		DeniedOnly: true,
	}), nil
}

// authzKeyPath returns the operation path segments addressing one record: one
// parseable placeholder value per primary key.
func authzKeyPath(res *resourceInfo, pkTypes []pkParamType) (string, error) {
	var path strings.Builder
	for _, t := range pkTypes {
		value, ok := authzParamValue(t)
		if !ok {
			return "", errors.Newf("authorization matrix: resource %s has no parseable placeholder value for primary-key type %s; add the type to authzParamValue or suppress the route", res.Name(), t.declared)
		}
		path.WriteString("/" + value)
	}

	return path.String(), nil
}

// updateProbeField picks the field carried by the update operation's denied case: the
// first patchable (json-visible, mutable) field with a synthesizable JSON value. One
// field is required because an empty update is a no-op that never reaches the
// permission check; nothing beyond the check evaluates the value, so validity against
// enums, foreign keys, or row state is irrelevant.
//
// A resource with no patchable field at all (a pure key table) returns patchable false:
// its update arm is vacuous — no client request can carry a field — so the matrix emits
// no update case for it. Patchable fields whose types are all outside authzJSONValue
// are an error: that is a type-coverage gap, not a vacuous arm.
func updateProbeField(res *resourceInfo) (name, value string, patchable bool, err error) {
	jsonCaser := strcase.NewCaser(false, nil, nil)
	for _, f := range res.Fields {
		if f.IsPrimaryKey || f.IsOutputOnly() || f.IsImmutable() {
			continue
		}
		patchable = true
		value, ok := authzJSONValue(pkParamType{declared: f.Type(), underlying: f.UnderlyingType()})
		if !ok {
			continue
		}

		return jsonCaser.ToCamel(f.Name()), value, true, nil
	}

	if patchable {
		return "", "", false, errors.Newf("authorization matrix: resource %s has no patchable field with a synthesizable JSON value; add a type to authzJSONValue or suppress the patch handler", res.Name())
	}

	return "", "", false, nil
}

// authzJSONValue returns a JSON literal that parses into the field's Go type, for the
// update probe's single field. The allowlist stays conservative: a field outside it is
// skipped in favor of the next.
func authzJSONValue(t pkParamType) (string, bool) {
	declared := strings.TrimPrefix(t.declared, "*")
	underlying := strings.TrimPrefix(t.underlying, "*")
	switch {
	case declared == cccUUIDGoType:
		return `"00000000-0000-0000-0000-000000000001"`, true
	case declared == stringGoType || underlying == stringGoType:
		return `"authz-test-value"`, true
	case strings.HasPrefix(declared, "int") || strings.HasPrefix(underlying, "int"),
		strings.HasPrefix(declared, "float") || strings.HasPrefix(underlying, "float"):
		return "1", true
	case declared == boolGoType || underlying == boolGoType:
		return jsonTrueLiteral, true
	case declared == "civil.Date":
		return `"2000-01-01"`, true
	case declared == "time.Time":
		return `"2000-01-01T00:00:00Z"`, true
	case declared == "decimal.Decimal" || declared == "decimal.NullDecimal":
		return "1", true
	default:
		return "", false
	}
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
