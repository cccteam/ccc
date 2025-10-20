package generation

import (
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

type (
	resourceOption func(*resourceGenerator) error
	tsOption       func(*typescriptGenerator) error
	// Option is a functional option for configuring a Generator
	Option func(any) error

	option interface {
		isOption()
	}

	// ResourceOption is a functional option for configuring a ResourceGenerator
	ResourceOption interface {
		option
		isResourceOption()
	}

	// TSOption is a functional option for configuring a TypescriptGenerator
	TSOption interface {
		option
		isTypescriptOption()
	}
)

func (resourceOption) isOption()         {}
func (resourceOption) isResourceOption() {}

func (tsOption) isOption()           {}
func (tsOption) isTypescriptOption() {}

func (Option) isOption()           {}
func (Option) isResourceOption()   {}
func (Option) isTypescriptOption() {}

// GenerateHandlers enables generating a handler file for each resource.
// To generate resource handlers in a single file use WithConsolidatedHandlers.
func GenerateHandlers(targetDir string) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.genHandlers = true
		r.handlerDestination = targetDir

		return nil
	})
}

// ApplicationName sets the name of the application struct.
// The default is "App".
func ApplicationName(name string) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.applicationName = name

		return nil
	})
}

// GenerateRoutes enables generating a router file containing routes for all handlers and RPC methods.
func GenerateRoutes(targetDir, targetPackage, routePrefix string) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.genRoutes = true
		r.routerDestination = targetDir
		r.routerPackage = targetPackage
		r.routePrefix = routePrefix

		return nil
	})
}

// WithTypescriptOverrides sets the Typescript type for a given Go type.
func WithTypescriptOverrides(overrides map[string]string) TSOption {
	return tsOption(func(t *typescriptGenerator) error {
		tempMap := defaultTypescriptOverrides()
		maps.Copy(tempMap, overrides)
		t.typescriptOverrides = tempMap

		return nil
	})
}

// GeneratePermissions enables generating resource and resource-field level permission mappings,
// based on the routes registered in the app router. Requires `collect_resource_permissions` build tag.
func GeneratePermissions() TSOption {
	return tsOption(func(t *typescriptGenerator) error {
		t.genPermission = true

		return nil
	})
}

// GenerateMetadata enables generating information necessary for Typescript configuration of resources.
func GenerateMetadata() TSOption {
	return tsOption(func(t *typescriptGenerator) error {
		t.genMetadata = true

		return nil
	})
}

// GenerateEnums enables generating constants for resources that have been tagged with `@enumerate`
// and have Id and Description values in the schema migrations directory.
func GenerateEnums() TSOption {
	return tsOption(func(t *typescriptGenerator) error {
		t.genEnums = true

		return nil
	})
}

// WithSpannerEmulatorVersion sets the version of the Spanner image pulled from gcr.io
func WithSpannerEmulatorVersion(version string) Option {
	return func(g any) error {
		switch t := g.(type) {
		case *client:
			t.spannerEmulatorVersion = version
		case *resourceGenerator, *typescriptGenerator: // no-op
		default:
			panic(fmt.Sprintf("unexpected generator type in WithSpannerEmulatorVersion(): %T", t))
		}

		return nil
	}
}

// WithPluralOverrides sets the pluralization for any resources that are not covered by the default pluralizations.
func WithPluralOverrides(overrides map[string]string) Option {
	tempMap := defaultPluralOverrides()
	maps.Copy(tempMap, overrides)

	return func(g any) error {
		switch t := g.(type) {
		case *client:
			t.pluralOverrides = tempMap
		case *resourceGenerator, *typescriptGenerator: // no-op
		default:
			panic(fmt.Sprintf("unexpected generator type in WithPluralOverrides(): %T", t))
		}

		return nil
	}
}

// CaserInitialismOverrides sets the initialism for any resources that are not covered by the default initialisms.
func CaserInitialismOverrides(overrides map[string]bool) Option {
	return func(g any) error {
		switch t := g.(type) {
		case *client:
			caser = strcase.NewCaser(false, overrides, nil)
		case *resourceGenerator, *typescriptGenerator: // no-op
		default:
			panic(fmt.Sprintf("unexpected generator type in CaserInitialismOverrides(): %T", t))
		}

		return nil
	}
}

// WithConsolidatedHandlers enables generating a handler file for all or a list of resources.
func WithConsolidatedHandlers(route string, consolidateAll bool, resources ...string) Option {
	return func(g any) error {
		if !consolidateAll && len(resources) == 0 {
			return errors.New("at least one resource is required if not consolidating all handlers")
		}

		switch t := g.(type) {
		case *client:
			t.ConsolidatedRoute = route
			t.ConsolidateAll = consolidateAll
			t.ConsolidatedResourceNames = resources
		case *resourceGenerator, *typescriptGenerator: // no-op
		default:
			panic(fmt.Sprintf("unexpected generator type in WithConsolidatedHandlers(): %T", t))
		}

		return nil
	}
}

// WithComputedResources enables generating routes and handlers for Computed Resources.
// The package's name is expected to be the same as its directory name.
func WithComputedResources(compResourcesPkgDir string) Option {
	compResourcesPkgName := filepath.Base(compResourcesPkgDir)
	compResourcesPkgDir = "./" + filepath.Clean(compResourcesPkgDir)

	return func(g any) error {
		switch t := g.(type) {
		case *resourceGenerator:
		case *typescriptGenerator: // no-op
		case *client:
			t.genComputedResources = true
			t.compPackageDir = compResourcesPkgDir
			t.compPackageName = compResourcesPkgName
			t.loadPackages = append(t.loadPackages, compResourcesPkgDir)
		default:
			panic(fmt.Sprintf("unexpected generator type in WithComputedResources(): %T", t))
		}

		return nil
	}
}

// WithRPC enables generating RPC method handlers.
// The package's name is expected to be the same as its directory name.
func WithRPC(rpcPackageDir string) Option {
	rpcPackageName := filepath.Base(rpcPackageDir)
	rpcPackageDir = "./" + filepath.Clean(rpcPackageDir)

	return func(g any) error {
		switch t := g.(type) {
		case *resourceGenerator:
		case *typescriptGenerator: // no-op
		case *client:
			t.rpcPackageDir = rpcPackageDir
			t.rpcPackageName = rpcPackageName
			t.genRPCMethods = true
			t.loadPackages = append(t.loadPackages, rpcPackageDir)
		default:
			panic(fmt.Sprintf("unexpected generator type in WithRPC(): %T", t))
		}

		return nil
	}
}

// resolveOptions is called twice, once in the client constructor and once in either the resource or typescript generator's constructor.
// That is why no-op cases are included to prevent falling through to the default panic case.
func resolveOptions(generator any, options []option) error {
	for _, optionFunc := range options {
		if optionFunc != nil {
			switch fn := optionFunc.(type) {
			case resourceOption:
				switch g := generator.(type) {
				case *resourceGenerator:
					if err := fn(g); err != nil {
						return err
					}
				case *client: // no-op
				default:
					panic(fmt.Sprintf("unexpected generator type in resourceOption: %T", g))
				}
			case tsOption:
				switch g := generator.(type) {
				case *typescriptGenerator:
					if err := fn(g); err != nil {
						return err
					}
				case *client: // no-op
				default:
					panic(fmt.Sprintf("unexpected generator type in tsOption: %T", g))
				}
			case Option:
				if err := fn(generator); err != nil {
					return err
				}
			}
		}
	}

	switch g := generator.(type) {
	case *resourceGenerator:
		if g.pluralOverrides == nil {
			g.pluralOverrides = defaultPluralOverrides()
		}
		if g.spannerEmulatorVersion == "" {
			g.spannerEmulatorVersion = "latest"
		}
		if g.applicationName == "" {
			g.applicationName = "App"
		}
		g.receiverName = strings.ToLower(string(g.applicationName[0]))

	case *typescriptGenerator:
		if g.pluralOverrides == nil {
			g.pluralOverrides = defaultPluralOverrides()
		}
		if g.typescriptOverrides == nil {
			g.typescriptOverrides = defaultTypescriptOverrides()
		}
		if g.spannerEmulatorVersion == "" {
			g.spannerEmulatorVersion = "latest"
		}
	case *client: // no-op
	default:
		panic(fmt.Sprintf("unexpected generator type: %T", g))
	}

	return nil
}

func defaultPluralOverrides() map[string]string {
	return map[string]string{
		"LenderBranch": "LenderBranches",
	}
}

const (
	stringGoType     = "string"
	boolGoType       = "bool"
	intGoType        = "int"
	int8GoType       = "int8"
	int16GoType      = "int16"
	int32GoType      = "int32"
	int64GoType      = "int64"
	uintGoType       = "uint"
	uint8GoType      = "uint8"
	uint16GoType     = "uint16"
	uint32GoType     = "uint32"
	uint64GoType     = "uint64"
	uintptrGoType    = "uintptr"
	float32GoType    = "float32"
	float64GoType    = "float64"
	complex64GoType  = "complex64"
	complex128GoType = "complex128"
)

func defaultTypescriptOverrides() map[string]string {
	return map[string]string{
		reflect.TypeOf(ccc.UUID{}).String():            "uuid",
		reflect.TypeOf(ccc.NullUUID{}).String():        "uuid",
		reflect.TypeOf(resource.Link{}).String():       "Link",
		reflect.TypeOf(resource.NullLink{}).String():   "Link",
		reflect.TypeOf(decimal.Decimal{}).String():     "number",
		reflect.TypeOf(decimal.NullDecimal{}).String(): "number",
		reflect.TypeOf(time.Time{}).String():           "Date",
		reflect.TypeOf(civil.Date{}).String():          "civilDate",
		boolGoType:                                     "boolean",
		stringGoType:                                   "string",
		intGoType:                                      "number",
		int8GoType:                                     "number",
		int16GoType:                                    "number",
		int32GoType:                                    "number",
		int64GoType:                                    "number",
		uintGoType:                                     "number",
		uint8GoType:                                    "number",
		uint16GoType:                                   "number",
		uint32GoType:                                   "number",
		uint64GoType:                                   "number",
		uintptrGoType:                                  "number",
		float32GoType:                                  "number",
		float64GoType:                                  "number",
		complex64GoType:                                "number",
		complex128GoType:                               "number",
	}
}
