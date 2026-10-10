package generation

import (
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
)

// This file holds the data payloads passed to the generation templates.
// Field names must match the {{ .Field }} references in templates.go.
//
// Payloads whose rendered output references parsed types implement typeImporter,
// returning the imports for exactly the types they render, so import resolution
// is scoped to the file being generated.

type resourceInterfacesData struct {
	Source                   string
	Package                  string
	ResourcesPackage         string
	ComputedResourcesPackage string
	Types                    []*resourceInfo
	ComputedResourceTypes    []*computedResource
}

// typeImports covers the struct types only: the interface file renders
// qualified type names, never field types.
func (d *resourceInterfacesData) typeImports() []fixerImport {
	var imports []fixerImport
	for _, res := range d.Types {
		imports = appendTypeImports(imports, res.Imports())
	}
	for _, res := range d.ComputedResourceTypes {
		imports = appendTypeImports(imports, res.Imports())
	}

	return imports
}

type resourceFileData struct {
	Source   string
	Package  string
	Resource *resourceInfo
}

func (d *resourceFileData) typeImports() []fixerImport {
	return resourceTypeImports(nil, d.Resource)
}

type resourceEnumsData struct {
	Source     string
	Package    string
	NamedTypes []*parser.NamedType
	EnumMap    map[string][]*enumData
}

// storageFileData feeds the storage-methods file of one package: the types declared
// there that a JSON column holds and that implement no Spanner methods of their own.
type storageFileData struct {
	Source  string
	Package string
	// Types are the unqualified type names, sorted.
	Types []string
}

// jsonFileData renders a package's zz_gen_json.go: the JSON pair of every defined type
// the package declares over a type with JSON methods.
type jsonFileData struct {
	Source  string
	Package string
	// Types are the pairs, sorted by type name.
	Types []jsonPair
}

// typeImports names the packages the right-hand sides come from, so the import fixer
// resolves their qualifiers.
func (d *jsonFileData) typeImports() []fixerImport {
	var imports []fixerImport
	for _, pair := range d.Types {
		imports = append(imports, pair.imports...)
	}

	return imports
}

type handlersFileData struct {
	Source              string
	LocalPackageImports string
	Handlers            string
	Package             string

	// resource is the resource the pre-rendered Handlers content was built from;
	// it scopes import resolution and is not referenced by the template.
	resource *resourceInfo
}

func (d *handlersFileData) typeImports() []fixerImport {
	return resourceTypeImports(nil, d.resource)
}

type consolidatedPatchData struct {
	Source              string
	LocalPackageImports string
	Resources           []*resourceInfo
	// GlobalCases and DomainCases split Resources by permission scope: global cases
	// dispatch on the operation path's first segment, domain cases dispatch under the
	// domain route segment's descent case with the domain bound from the path.
	// SegmentCase is the tenant record (@tenant), whose route name is the domain route
	// segment: it shares the descent case and branches on path depth (set only when
	// DomainCases exist; otherwise it is an ordinary global case).
	GlobalCases []consolidatedCaseData
	DomainCases []consolidatedCaseData
	SegmentCase *consolidatedCaseData
	// HasTenant says the tenant record is among the cases: the dispatcher then collects
	// the tenants its transaction creates and deletes, hands them to the roster after
	// the commit, and signals the tenants kind.
	HasTenant           bool
	DomainRouteSegment  string
	DomainPatternPrefix string // "/stations/{stationID}" — the chi pattern the descent case prefix-matches
	Package             string
	ResourcePackage     string
	ApplicationName     string
	ReceiverName        string
	// HandlerName is the dispatcher method's name: PatchResources on the default
	// outlet, Patch<Suffix>Resources on an extra outlet's dispatcher.
	HandlerName string
	// ConcealedDomains adds the caller's foothold (HasGrants) to the descent's
	// question after the roster (WithConcealedDomains).
	ConcealedDomains bool
}

// consolidatedCaseData is one resource case of the consolidated dispatch, carrying the
// file-level values the shared case template needs alongside the resource.
type consolidatedCaseData struct {
	*resourceInfo
	DomainPatternPrefix string // "" for global cases
	ResourcePackage     string
	ReceiverName        string
}

func (d *consolidatedPatchData) typeImports() []fixerImport {
	var imports []fixerImport
	for _, res := range d.Resources {
		imports = resourceTypeImports(imports, res)
	}

	return imports
}

type handlerContentData struct {
	ResourcePackage         string
	Resource                *resourceInfo
	VirtualResourcesPackage string
	ApplicationName         string
	ReceiverName            string
}

// fileHandlerData feeds one @file route's handler on a table or view resource.
type fileHandlerData struct {
	handlerContentData
	File *fileRoute
}

type computedHandlerData struct {
	Source              string
	LocalPackageImports string
	Resource            *computedResource
	Package             string
	ComputedPackage     string
	ApplicationName     string
	ReceiverName        string
}

func (d *computedHandlerData) typeImports() []fixerImport {
	imports := appendTypeImports(nil, d.Resource.Imports())
	for _, field := range d.Resource.Fields {
		imports = appendTypeImports(imports, field.Imports())
	}
	if d.Resource.Shape != nil {
		imports = appendTypeImports(imports, d.Resource.Shape.Imports())
	}

	return imports
}

type collectionFileData struct {
	Source  string
	Package string
	Data    resource.CollectionData
}

type routerFileData struct {
	Source                 string
	Package                string
	LocalPackageImports    string
	RoutesMap              map[string][]*generatedRoute
	ConstResources         []*resourceInfo
	ConstComputedResources []*computedResource
	RouterTestRoutes       []*generatedRoute
	HasConsolidatedHandler bool
	// HasDomainScoped emits the Domain route-parameter const, which generated
	// handlers of domain-scoped resources and RPC methods reference.
	HasDomainScoped bool
	// HasDomainScopedRoutes emits the DomainGuard requirement on GeneratedHandlers and
	// the middleware wrapping in generatedRoutes. Distinct from HasDomainScoped: a
	// domain-scoped resource with routing disabled needs the const but has no route to
	// wrap, and an unused guard variable would not compile.
	HasDomainScopedRoutes bool
	// DomainRouteParam is the Domain const's value: the route parameter name of the
	// domain segment pair, the tenant record's key parameter (resolveTenantRecord;
	// "domain" while nothing is domain-scoped).
	DomainRouteParam  string
	RoutePrefix       string
	ConsolidatedRoute string
	// ServesSessions registers the permission-digest, user-domains and live routes
	// under the default outlet's prefix and their handlers on GeneratedHandlers; false
	// under APIKey, when the outlet's generated routes are the resource routes its key
	// authorizes and nothing else (an extra outlet's is on its outletRouteData).
	ServesSessions bool
	// ExtraOutlets carries the WithRouterOutlet registration surfaces; the fields
	// above describe the default outlet, whose generated identifiers are unsuffixed.
	// With no extra outlets the rendered file is exactly the single-outlet file.
	ExtraOutlets []*outletRouteData
	// StubDomainGuard emits the router-test stub's DomainGuard: any outlet has
	// domain-scoped routes (HasDomainScopedRoutes covers the default outlet only).
	StubDomainGuard bool
	// ExtraStubHandlerFuncs are the handler funcs the router-test stub needs beyond
	// the default outlet's: methods served only under extra outlets, plus each extra
	// outlet's consolidated dispatcher.
	ExtraStubHandlerFuncs []string
	// NegativeRouterTests are the outlet-isolation cases: URLs that must fall through
	// to 404 because the addressed outlet does not carry the resource.
	NegativeRouterTests []negativeRouterTest
	// ScheduledRoutes are the scheduled methods' routes under the scheduled prefix,
	// which GeneratedScheduledHandlers and generatedScheduledRoutes carry; none without
	// a scheduled method, and then the file carries neither.
	ScheduledRoutes []*scheduledRoute
	// HasGatedRoutes emits the FeatureGuard requirement on GeneratedHandlers and the
	// wrapping in generatedRoutes for the default outlet; StubFeatureGuard emits the
	// router-test stub's pass-through when any outlet has a gated route.
	HasGatedRoutes   bool
	StubFeatureGuard bool
	// BodyLimit is the request body limit the routes file declares as the router
	// package's BodyLimit (WithBodyLimit; DefaultBodyLimit when the application sets
	// none); BodyLimitText is it in words, and BodyLimitDefault whether it is the
	// default. HasBoundedRoutes emits the default outlet's bounded group when a route
	// the router wraps exists beyond the session and consolidated routes.
	BodyLimit        int64
	BodyLimitText    string
	BodyLimitDefault bool
	HasBoundedRoutes bool
	// RouteRequestLogs says a route or a scheduled route declares its own request log
	// word, so the routes file imports the logger for logger.WithPolicy;
	// RouteMinSeverities says one of those words carries a floor, so it imports the
	// logging library for the severity.
	RouteRequestLogs   bool
	RouteMinSeverities bool
	// ResourcePackage qualifies the feature constants the gated routes name.
	ResourcePackage string
	// AuthName and AuthParam are the default outlet's auth binding (see
	// outletRouteData). AuthImports are the auth packages the session outlets declare,
	// sorted and once each, whose Name the routes functions bind.
	AuthName    string
	AuthParam   bool
	AuthImports []string
	// TestRouterAuthParams are NewTestRouter's auth parameters, one per session outlet
	// that declares no Auth, in declaration order; TestRouterAuthArgs are the quoted
	// names the generated router test passes for them, each outlet's own name, since its
	// stub handlers never reach the live pages.
	TestRouterAuthParams []string
	TestRouterAuthArgs   []string
}

// routeHandlerData feeds the routes template's handler expression for one route: the
// route and the package its gate's constant lives in.
type routeHandlerData struct {
	Route           *generatedRoute
	ResourcePackage string
}

// outletRouteData is one extra router outlet's registration surface: the routes the
// routesTemplate renders into the outlet's Generated<Suffix>Handlers interface and
// generated<Suffix>Routes function.
type outletRouteData struct {
	Name   string
	Suffix string
	// Prefix is the outlet's route prefix, under which the session routes render
	// when the outlet serves sessions (resource routes carry it pre-rendered in
	// their paths).
	Prefix string
	// ServesSessions registers the permission-digest and user-domains routes under
	// the outlet's prefix and adds their handler requirements to the outlet's
	// interface (see the ServesSessions option).
	ServesSessions bool
	// HasBoundedRoutes emits the outlet's bounded group when a route the router wraps
	// exists beyond the session and consolidated routes.
	HasBoundedRoutes bool
	// RoutesMap groups the outlet's routes by source struct name (template map
	// iteration is name-sorted, keeping output deterministic).
	RoutesMap             map[string][]*generatedRoute
	HasDomainScopedRoutes bool
	// HasGatedRoutes emits the FeatureGuard requirement and the wrapping for the
	// outlet's gated routes.
	HasGatedRoutes bool
	// HasConsolidatedHandler emits the outlet's consolidated patch dispatcher
	// (ConsolidatedHandlerFunc) at ConsolidatedPath.
	HasConsolidatedHandler  bool
	ConsolidatedHandlerFunc string
	ConsolidatedPath        string
	// AuthName is the Go expression naming the auth whose sessions a session outlet
	// serves, which its routes function binds for the live pages (live.Subscribing):
	// <pkg>.Name for the auth the outlet declares (Auth), or the routes function's auth
	// parameter where it declares none (AuthParam). Empty for an outlet without sessions
	// and for an API-key outlet, whose routes refuse a subscribing request.
	AuthName string
	// AuthParam marks a session outlet that declares no Auth, as when the application
	// writes its own router: the generator does not know its auth, so the routes
	// function takes the auth's name from the router composing it, and NewTestRouter
	// takes it as TestRouterParam.
	AuthParam       bool
	TestRouterParam string
}

// negativeRouterTest is one outlet-isolation case: Method is the net/http constant
// expression, URL the request path that must 404 with no handler dispatched.
type negativeRouterTest struct {
	Method string
	URL    string
}

// permissionsData feeds the permissions template: the application's
// library-delegating PermissionDigest and UserDomains handlers.
type permissionsData struct {
	Source          string
	Package         string
	ApplicationName string
	ReceiverName    string
	// RoutePrefix names the default outlet's route prefix in the emitted doc
	// comments; the routes themselves are registered by the routes template.
	RoutePrefix string
	// DefaultServesSessions says the default outlet serves the routes the doc
	// comments name; false under APIKey, when only the additional session outlets do.
	DefaultServesSessions bool
	// HasExtraSessionOutlets extends the doc comments when additional outlets
	// serve sessions (ServesSessions), whose routes the same handlers serve.
	HasExtraSessionOutlets bool
	// LocalPackageImports and ResourcePackage let the digest handler name the
	// generated FeatureGates() in the resources package.
	LocalPackageImports string
	ResourcePackage     string
	// RouterPackage is the package whose Collection() carries the former names the
	// digest mirrors.
	RouterPackage string
}

// featureDeclarationsData feeds the resources package's zz_gen_features.go.
type featureDeclarationsData struct {
	Source       string
	Package      string
	Declarations []resource.FeatureDeclaration
	Gates        []featureGateEntry
}

// fileHoldersData feeds the resources package's zz_gen_file_holders.go: the table
// resources with a stored @file by struct name, the computed ones with their key
// columns and store expressions, and the packages those expressions import.
type fileHoldersData struct {
	Source   string
	Package  string
	Imports  []string
	Tables   []string
	Computed []computedHolder
}

// computedHolder is one computed resource's holder: its resource name and key columns.
type computedHolder struct {
	Resource string
	Keys     []computedHolderKey
}

// computedHolderKey is one key column with its store's name expression.
type computedHolderKey struct {
	Field     string
	StoreExpr string
}

// featuresData feeds the handler package's zz_gen_features.go: the feature flag
// handlers as delegations to the library's.
type featuresData struct {
	Source              string
	Package             string
	LocalPackageImports string
	ApplicationName     string
	ReceiverName        string
	// RouterPackage qualifies the generated collection the flags' decoders check
	// grants against.
	RouterPackage string
	RoutePrefix   string
	// HasExtraSessionOutlets extends the doc comments when additional outlets serve
	// sessions, whose routes the same handlers serve.
	HasExtraSessionOutlets bool
	// HasRoutes emits the FeatureFlags and SetFeature handlers, which read the
	// generated collection: only a run that generates routes has one.
	HasRoutes bool
	// HasGates emits the FeatureGuard, which the gated routes are wrapped in.
	HasGates bool
}

type domainGuardData struct {
	Source              string
	Package             string
	LocalPackageImports string
	ApplicationName     string
	ReceiverName        string
	// ConcealedDomains adds the caller's foothold (HasGrants) to the guard's question
	// after the roster, collapsing "unauthorized" into "nonexistent"
	// (WithConcealedDomains).
	ConcealedDomains bool
}

type decodersFileData struct {
	Source              string
	Package             string
	LocalPackageImports string
	ApplicationName     string
	ReceiverName        string
	// RPCPackage qualifies the generated Method union constraining NewRPCDecoder.
	RPCPackage string
	// RouterPackage qualifies the generated collection the query decoders render
	// conditional grants against.
	RouterPackage string
	// The Has* fields emit each constructor only when a generated handler calls it,
	// so an application carries no constructor its code does not use.
	HasQueryDecoder         bool
	HasComputedQueryDecoder bool
	HasPatchDecoder         bool
	HasRPCDecoder           bool
	HasFileDecoder          bool
	HasComputedFileDecoder  bool
	// HasCollection marks an application that generates the permission collection,
	// which the RPC decoder wires in so armed writes render conditional grants.
	HasCollection         bool
	HasTargetedRPCDecoder bool
}

type appContractData struct {
	Source              string
	Package             string
	LocalPackageImports string
	ApplicationName     string
	// The Has* fields emit each contract block only while its feature generates a
	// caller, so an application is never asserted to carry methods nothing generated
	// draws on. The resource block (UserPermissions, ResourceClient) is unconditional.
	HasValidator    bool
	HasDomainScoped bool
	HasRPC          bool
	HasComputed     bool
	// ConcealedDomains says the domain-scoped surface asks the caller's foothold after
	// the roster (WithConcealedDomains); the contract's methods are the same either way,
	// and the comment says which question the guard asks.
	ConcealedDomains bool
}

type handlerTestsMainData struct {
	Source          string
	Package         string
	EmulatorVersion string
	// MigrationSources are the application's schema migration source URLs, rewritten
	// relative to the handler-tests directory.
	MigrationSources []string
}

// authzCase is one endpoint's entry in the generated authorization matrix. Query
// endpoints expand to a denied case (no permission -> 403) and a granted case (exactly
// Permission -> 200, or 404 on the empty schema). Mutation endpoints (DeniedOnly)
// expand to the denied case alone: proving the arm fails closed is the security
// property, while the success path needs generator-synthesized valid request bodies
// and is deferred to manual testing.
type authzCase struct {
	Name string
	// Method is the net/http method constant expression, e.g. "http.MethodGet".
	Method string
	// URL is the route path with parseable placeholder primary-key values substituted.
	URL string
	// Permission is the accesstypes constant name the granted case carries; unused
	// when DeniedOnly.
	Permission string
	// Body is the request body ("" for query endpoints). Mutation bodies are minimal:
	// just enough to reach the operation's enforcement gate, never a valid payload.
	Body string
	// DeniedOnly suppresses the granted case (mutation endpoints).
	DeniedOnly bool
	// DeniedStatus overrides the denied case's expected status (default 403).
	// Concealed domains (WithConcealedDomains) answer a caller with no grants
	// as if the domain did not exist: 404 from the route guard, 400 from the
	// consolidated dispatcher's operation-path descent.
	DeniedStatus string
	// Headers are request headers the case sends (the dry-run header).
	Headers []authzHeader
	// Open marks a route anyone signed in may call: the case carries no grant and
	// expects 200 alone (the features route).
	Open bool
}

// authzHeader is one request header a generated authorization case sends.
type authzHeader struct {
	Name  string
	Value string
}

type authzTestData struct {
	Source  string
	Package string
	Cases   []authzCase
	// HasGates says the application declares feature flags: the matrix then puts every
	// one of them on in its database before driving the routes, since a gated route
	// answers 404 before the permission gate while its flag is off.
	HasGates            bool
	LocalPackageImports string
	ResourcePackage     string
}

type rpcFileData struct {
	Source    string
	Package   string
	RPCMethod *rpcMethodInfo
}

func (d *rpcFileData) typeImports() []fixerImport {
	return rpcTypeImports(nil, d.RPCMethod)
}

type rpcHandlerData struct {
	Source              string
	LocalPackageImports string
	RPCMethod           *rpcMethodInfo
	Package             string
	ApplicationName     string
	ReceiverName        string
	// ResourcesPackage names the resources package, whose generated query and
	// patch builders the declared-transition frame works through.
	ResourcesPackage string
	// BodyLimitExpr is the limit the handler wraps the body with: the method's declared
	// maximum as a literal, or the router package's BodyLimit when the method declares
	// none (the literal limit when no router is generated). BodyLimitText says which.
	BodyLimitExpr string
	BodyLimitText string
}

func (d *rpcHandlerData) typeImports() []fixerImport {
	return rpcTypeImports(nil, d.RPCMethod)
}

type rpcInterfacesData struct {
	Source  string
	Package string
	Types   []*rpcMethodInfo
}

func (d *rpcInterfacesData) typeImports() []fixerImport {
	var imports []fixerImport
	for _, method := range d.Types {
		imports = rpcTypeImports(imports, method)
	}

	return imports
}

type tsConstantsData struct {
	File       *typescriptGenerator
	Data       *resource.TypescriptData
	RPCMethods []*rpcMethodInfo
	// ManualMethods are the Execute registrations without a parsed RPC struct
	// (@manualAddResource(Execute)); they join the Methods constants after the
	// generated methods.
	ManualMethods []accesstypes.Resource
	PIIMap        map[accesstypes.Resource]map[accesstypes.Tag]bool
}

type tsResourcesData struct {
	File              *typescriptGenerator
	Resources         []*resourceInfo
	ComputedResources []*computedResource
	ConsolidatedRoute string
	GenPrefix         string
	// DomainRoutePrefix is the route pair domain-scoped routes are served under
	// ("stations/{stationID}"), rendered ahead of their route value; frontends
	// interpolate the parameter token.
	DomainRoutePrefix string
	DomainRouteParam  string
	HasDomainScoped   bool
	// Workflows carries each stateful root's assembled graph — the same facts
	// the DOT files draw (minus context references) — so a frontend can render
	// the workflow itself. Empty for applications without workflows, which then
	// emit nothing (byte-identical output).
	Workflows []*workflowGraph
	// FeatureFlags emits the library's FeatureFlags resource: its interface, its
	// metadata and its scope, which every application that generates routes serves.
	FeatureFlags bool
}

type tsMethodsData struct {
	File       *typescriptGenerator
	RPCMethods []*rpcMethodInfo
	GenPrefix  string
	// FeatureFlags emits the library's SetFeature method: its body and result
	// interfaces and its metadata.
	FeatureFlags bool
}

type tsEnumsData struct {
	Source     string
	NamedTypes []*parser.NamedType
	EnumMap    map[string][]*enumData
}

// appendTypeImports converts parser imports to fixer entries.
func appendTypeImports(dst []fixerImport, imps []parser.Import) []fixerImport {
	for _, imp := range imps {
		dst = append(dst, fixerImport{name: imp.Name, path: imp.Path})
	}

	return dst
}

// resourceTypeImports appends the packages of a resource's type and all of its
// field types.
func resourceTypeImports(dst []fixerImport, res *resourceInfo) []fixerImport {
	dst = appendTypeImports(dst, res.Imports())
	for _, field := range res.Fields {
		dst = appendTypeImports(dst, field.Imports())
	}

	return dst
}

// rpcTypeImports appends the packages of an RPC method's type and all of its
// field types.
func rpcTypeImports(dst []fixerImport, method *rpcMethodInfo) []fixerImport {
	dst = appendTypeImports(dst, method.Imports())
	if method.Request != nil {
		dst = appendTypeImports(dst, method.Request.Imports())
	}
	if method.Result != nil {
		// The response mirror declares the result's leaf types too.
		dst = appendTypeImports(dst, method.Result.Imports())
	}
	for _, field := range method.Fields {
		dst = appendTypeImports(dst, field.Imports())
	}

	return dst
}

// tsAPIData feeds the typed API client template (zz_gen_api.ts): the descriptor the
// @cccteam/resource runtime interprets plus the per-resource write shapes and key
// tuples only the generator can derive. Only default-outlet resources and methods
// appear — the browser client addresses the default outlet.
type tsAPIData struct {
	File               *typescriptGenerator
	GenPrefix          string
	Resources          []*tsAPIResource
	Methods            []*tsAPIMethod
	DomainRouteSegment string
	DomainRouteParam   string
	ConsolidatedRoute  string
	HasDomainScoped    bool
	HasGlobal          bool
	HasDomain          bool
	// Live says the outlet serves the live routes (resource/live), so the descriptor
	// names them; every outlet a client is generated for serves sessions, and every
	// session outlet serves them.
	Live bool
	// Features are the declared flags in constant order: the Feature union and the
	// Feature constants the client file declares. Empty, neither is declared.
	Features []resource.FeatureDeclaration
}

// HasUpload reports whether any method on this outlet is an @upload, so the client
// file imports the upload handle type.
func (d *tsAPIData) HasUpload() bool {
	for _, method := range d.Methods {
		if method.UploadMaxBytes > 0 {
			return true
		}
	}

	return false
}

// HasNullBoolean reports whether a key or write shape on this outlet is typed
// NullBoolean, so the client file imports the type it names.
func (d *tsAPIData) HasNullBoolean() bool {
	return d.hasFieldType(func(fieldType string) bool { return fieldType == nullBooleanTSType })
}

// TypeImports lists the imports the client file needs: the @typescript declarations
// behind every key, create, and patch field it renders, grouped per module.
func (d *tsAPIData) TypeImports() []tsImportGroup {
	var imports []*tsImport
	for _, res := range d.Resources {
		for _, fields := range [][]*tsAPIField{res.Keys, res.CreateFields, res.PatchFields} {
			for _, field := range fields {
				imports = append(imports, field.Import)
			}
		}
	}

	return groupImports(imports)
}

// hasFieldType reports whether any key, create, or patch field the client file
// renders has a type the predicate accepts.
func (d *tsAPIData) hasFieldType(accept func(fieldType string) bool) bool {
	for _, res := range d.Resources {
		for _, fields := range [][]*tsAPIField{res.Keys, res.CreateFields, res.PatchFields} {
			for _, field := range fields {
				if accept(field.Type) {
					return true
				}
			}
		}
	}

	return false
}

// ResourceImports lists the row types the client file imports from the resources file.
func (d *tsAPIData) ResourceImports() string {
	names := make([]string, 0, len(d.Resources))
	for _, res := range d.Resources {
		names = append(names, res.Name)
	}

	return strings.Join(names, ", ")
}

// MethodImports lists the body and result types the client file imports from the
// methods file.
func (d *tsAPIData) MethodImports() string {
	names := make([]string, 0, len(d.Methods)*2)
	for _, method := range d.Methods {
		names = append(names, method.Name)
		if method.Answers {
			names = append(names, method.ResolvesWith())
		}
	}

	return strings.Join(names, ", ")
}

type tsAPIResource struct {
	// Name is the plural PascalCase resource name — the Resources constant and the row type.
	Name string
	// Property is the camelCase name the handle hangs off the client under.
	Property     string
	Route        string
	Scope        accesstypes.PermissionScope
	Consolidated bool
	Keys         []*tsAPIField
	Operations   []string
	HasCreate    bool
	CreateFields []*tsAPIField
	HasPatch     bool
	PatchFields  []*tsAPIField
	// PageDefault and PageMax are the resource's page sizes as the client
	// descriptor carries them: the page a limit-less request receives, and
	// the largest page it may ask for (0 = none, which also permits limit=all).
	PageDefault uint64
	PageMax     uint64
	// Order is the declared @order as the descriptor carries it, each entry a JSON
	// field name and direction, so a client knows a request without a sort is already
	// ordered and issues cursors; empty when the resource declares none and lists by
	// primary key.
	Order []*tsAPISort
	// Files are the resource's @file segments, so the handle addresses a row's file
	// (fileUrl); empty when the resource declares none.
	Files []string
	// Feature is the flag the resource is gated behind, as the descriptor carries it;
	// empty when the resource is not gated.
	Feature string
}

// tsAPISort is one entry of a descriptor's declared order.
type tsAPISort struct {
	Field     string
	Direction string
}

// HandleType renders the ResourceHandle instantiation for the resource: the row type,
// the key tuple, the operations the server generated, and the write shapes when the
// resource accepts writes.
func (r *tsAPIResource) HandleType() string {
	ops := make([]string, 0, len(r.Operations))
	for _, op := range r.Operations {
		ops = append(ops, "'"+op+"'")
	}
	args := []string{r.Name, r.Name + "Key", strings.Join(ops, " | ")}
	switch {
	case r.HasPatch:
		args = append(args, r.createTypeName(), r.Name+"Patch")
	case r.HasCreate:
		args = append(args, r.createTypeName())
	}

	return "ResourceHandle<" + strings.Join(args, ", ") + ">"
}

func (r *tsAPIResource) createTypeName() string {
	if r.HasCreate {
		return r.Name + "Create"
	}

	return "never"
}

type tsAPIField struct {
	Name     string
	Type     string
	Required bool
	// Import is the @typescript declaration behind the field's type, nil for a
	// built-in row or a derived interface.
	Import *tsImport
}

type tsAPIMethod struct {
	Name     string
	Property string
	Route    string
	Scope    accesstypes.PermissionScope
	// Answers marks a method whose Execute returns a result: its handle is typed
	// with the generated <Name>Result, or <Name>Answer when it declares statuses.
	Answers bool
	// Statuses is the method's @answers declaration, carried on the descriptor
	// so the client tells the method's own 4xx from the frame's.
	Statuses []int
	// UploadMaxBytes is the method's @upload maximum, 0 for a JSON method; the
	// descriptor carries it so the client refuses an oversized upload locally.
	UploadMaxBytes int64
	// Feature is the flag the method is gated behind, as the descriptor carries it;
	// empty when the method is not gated.
	Feature string
}

// HandleType is the handle the client exposes the method under: an
// UploadMethodHandle, which adds upload(body, files), for an @upload method.
func (m *tsAPIMethod) HandleType() string {
	if m.UploadMaxBytes > 0 {
		return "UploadMethodHandle"
	}

	return "MethodHandle"
}

// ResultName is the generated TypeScript result interface's name.
func (m *tsAPIMethod) ResultName() string {
	return m.Name + "Result"
}

// AnswerName is the generated TypeScript answer interface's name: the status
// the method chose with its typed result.
func (m *tsAPIMethod) AnswerName() string {
	return m.Name + "Answer"
}

// ResolvesWith is the type the method's handle resolves with: the answer when
// the method declares statuses, else the result.
func (m *tsAPIMethod) ResolvesWith() string {
	if len(m.Statuses) > 0 {
		return m.AnswerName()
	}

	return m.ResultName()
}

// StatusArray renders the declared statuses as a TypeScript array literal.
func (m *tsAPIMethod) StatusArray() string {
	return "[" + statusList(m.Statuses, ", ") + "]"
}

// ScopeKind renders the resource's permission scope as the client descriptor spells it.
func (r *tsAPIResource) ScopeKind() string {
	return scopeKind(r.Scope)
}

// ScopeKind renders the method's permission scope as the client descriptor spells it.
func (m *tsAPIMethod) ScopeKind() string {
	return scopeKind(m.Scope)
}

func scopeKind(scope accesstypes.PermissionScope) string {
	if scope == accesstypes.DomainPermissionScope {
		return string(accesstypes.DomainPermissionScope)
	}

	return string(accesstypes.GlobalPermissionScope)
}
