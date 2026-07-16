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
	Package             string
	ResourcePackage     string
	ApplicationName     string
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

type routerFileData struct {
	Source                 string
	Package                string
	LocalPackageImports    string
	RoutesMap              map[string][]*generatedRoute
	ConstResources         []*resourceInfo
	ConstComputedResources []*computedResource
	RouterTestRoutes       []*generatedRoute
	HasConsolidatedHandler bool
	RoutePrefix            string
	ConsolidatedRoute      string
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
