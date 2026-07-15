package generation

import (
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
)

// This file holds the data payloads passed to the generation templates.
// Field names must match the {{ .Field }} references in templates.go.

type resourceInterfacesData struct {
	Source                   string
	Package                  string
	ResourcesPackage         string
	ComputedResourcesPackage string
	Types                    []*resourceInfo
	ComputedResourceTypes    []*computedResource
}

type resourceFileData struct {
	Source   string
	Package  string
	Resource *resourceInfo
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

type routerFileData struct {
	Source                 string
	Package                string
	LocalPackageImports    string
	RoutesMap              map[string][]generatedRoute
	ConstResources         []*resourceInfo
	Resources              []*resourceInfo
	ComputedResources      []*computedResource
	ConstComputedResources []*computedResource
	HasConsolidatedHandler bool
	RoutePrefix            string
	ConsolidatedRoute      string
}

type rpcFileData struct {
	Source    string
	Package   string
	RPCMethod *rpcMethodInfo
}

type rpcHandlerData struct {
	Source              string
	LocalPackageImports string
	RPCMethod           *rpcMethodInfo
	Package             string
	ApplicationName     string
	ReceiverName        string
}

type rpcInterfacesData struct {
	Source  string
	Package string
	Types   []*rpcMethodInfo
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
