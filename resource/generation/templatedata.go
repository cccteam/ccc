package generation

import (
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
	// SegmentCase is the tenant-record pattern: a global resource named like the
	// domain route segment, sharing the descent case and branching on path depth
	// (set only when DomainCases exist; otherwise it is an ordinary global case).
	GlobalCases         []consolidatedCaseData
	DomainCases         []consolidatedCaseData
	SegmentCase         *consolidatedCaseData
	DomainRouteSegment  string
	DomainPatternPrefix string // "/stations/{stationID}" — the chi pattern the descent case prefix-matches
	Package             string
	ResourcePackage     string
	ApplicationName     string
	ReceiverName        string
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
	// domain segment pair (default "domain", derived from the tenant-record resource
	// when one matches the segment — see deriveDomainRouteParam).
	DomainRouteParam  string
	RoutePrefix       string
	ConsolidatedRoute string
}

type domainGuardData struct {
	Source              string
	Package             string
	LocalPackageImports string
	ApplicationName     string
	ReceiverName        string
}

type decodersFileData struct {
	Source              string
	Package             string
	LocalPackageImports string
	ApplicationName     string
	ReceiverName        string
	// RPCPackage qualifies the generated Method union constraining NewRPCDecoder.
	RPCPackage string
	// The Has* fields emit each constructor only when a generated handler calls it,
	// so an application carries no constructor its code does not use.
	HasQueryDecoder bool
	HasPatchDecoder bool
	HasRPCDecoder   bool
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
}

type handlerTestsMainData struct {
	Source          string
	Package         string
	EmulatorVersion string
	// MigrationSources are the application's schema migration source URLs, rewritten
	// relative to the handler-tests directory.
	MigrationSources []string
}

// authzCase is one endpoint's entry in the generated authorization matrix: it expands
// to a denied case (no permission -> 403) and a granted case (exactly Permission ->
// 200, or 404 on the empty schema).
type authzCase struct {
	Name string
	// Method is the net/http method constant expression, e.g. "http.MethodGet".
	Method string
	// URL is the route path with parseable placeholder primary-key values substituted.
	URL string
	// Permission is the accesstypes constant name the endpoint's decoder demands.
	Permission string
}

type authzTestData struct {
	Source  string
	Package string
	Cases   []authzCase
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
	PIIMap     map[accesstypes.Resource]map[accesstypes.Tag]bool
}

type tsResourcesData struct {
	File              *typescriptGenerator
	Resources         []*resourceInfo
	ComputedResources []*computedResource
	ConsolidatedRoute string
	GenPrefix         string
	// DomainRoutePrefix is the route pair domain-scoped routes are served under
	// ("stations/{stationID}"), rendered ahead of their route value; frontends
	// interpolate the parameter token. DomainRoutePrefixTS is the same pair as a
	// TypeScript template-literal fragment ("stations/${string}") for operation path
	// types.
	DomainRoutePrefix   string
	DomainRoutePrefixTS string
	DomainRouteParam    string
	HasDomainScoped     bool
	HasConsolidated     bool
}

type tsMethodsData struct {
	File       *typescriptGenerator
	RPCMethods []*rpcMethodInfo
	GenPrefix  string
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
	for _, field := range method.Fields {
		dst = appendTypeImports(dst, field.Imports())
	}

	return dst
}
