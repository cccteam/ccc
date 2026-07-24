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
	"github.com/cccteam/ccc/accesstypes"
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
		r.handler = packageDir(targetDir)

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
func GenerateRoutes(targetDir, routePrefix string) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.genRoutes = true
		r.router = packageDir(targetDir)
		r.routePrefix = routePrefix

		return nil
	})
}

// GenerateTypescript enables TypeScript generation as part of the resource generator run.
// The permission data is computed statically from the parsed resources, so the run needs
// no compiled application router.
//
// The option may be passed multiple times, once per target directory, each call carrying
// its own TypeScript-specific options — a shared resource package emits its TypeScript
// into every consuming application this way. Target directories must be distinct.
//
// It accepts only TypeScript-specific options: GenerateMetadata, GeneratePermissions,
// GenerateEnums, and WithTypescriptOverrides. Everything else — package locations
// (WithVirtualResources, WithComputedResources, WithRPC), the Spanner emulator version,
// plural overrides, and consolidated handlers — is a ResourceOption inherited from the
// enclosing NewResourceGenerator options, so nesting one here fails to compile.
// GeneratePermissions and GenerateMetadata render from the permission collection, so
// they additionally require GenerateRoutes or manual declarations (@manualAddResource,
// @manualAddResourceSet, WithManualResources); enum output reads only the schema and
// carries no such requirement.
func GenerateTypescript(targetDir string, options ...TSOption) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.typescriptTargets = append(r.typescriptTargets, typescriptTarget{destination: targetDir, options: options})

		return nil
	})
}

// typescriptTarget is one recorded GenerateTypescript call: a target directory and the
// TypeScript-specific options that shape what is emitted there.
type typescriptTarget struct {
	destination string
	options     []TSOption
}

// resolve applies the target's options onto a fresh typescriptGenerator, yielding its
// resolved flag set and destination; the caller attaches the shared client and the
// permission collection.
func (target typescriptTarget) resolve() (*typescriptGenerator, error) {
	t := &typescriptGenerator{typescriptDestination: target.destination}

	opts := make([]option, 0, len(target.options))
	for _, opt := range target.options {
		opts = append(opts, opt)
	}
	if err := resolveOptions(t, opts); err != nil {
		return nil, err
	}

	return t, nil
}

// WithManualResources declares permission registrations the generator cannot derive from
// generated handlers: resources registered by hand-written routes (e.g. a Require()
// middleware calling Collection.AddResource). Each declared registration is included in
// the generated permission collection and the generated TypeScript constants.
func WithManualResources(registrations ...ManualRegistration) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		for _, reg := range registrations {
			if reg.Resource == "" {
				return errors.New("manual registration requires a resource name")
			}
			if reg.Permission == accesstypes.NullPermission {
				return errors.Newf("manual registration for resource %q requires a permission", reg.Resource)
			}
		}

		r.manualRegistrations = append(r.manualRegistrations, registrations...)

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

// GeneratePermissions enables generating resource and resource-field level permission
// mappings, computed statically from the permission collection (see GenerateTypescript).
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
func WithSpannerEmulatorVersion(version string) ResourceOption {
	return Option(func(g any) error {
		switch t := g.(type) {
		case *client:
			t.spannerEmulatorVersion = version
		case *resourceGenerator, *typescriptGenerator: // no-op
		default:
			panic(fmt.Sprintf("unexpected generator type in WithSpannerEmulatorVersion(): %T", t))
		}

		return nil
	})
}

// WithPluralOverrides sets the pluralization for any resource names that are not
// handled correctly by the default pluralization rules.
func WithPluralOverrides(overrides map[string]string) ResourceOption {
	tempMap := maps.Clone(overrides)

	return Option(func(g any) error {
		switch t := g.(type) {
		case *client:
			t.pluralOverrides = tempMap
		case *resourceGenerator, *typescriptGenerator: // no-op
		default:
			panic(fmt.Sprintf("unexpected generator type in WithPluralOverrides(): %T", t))
		}

		return nil
	})
}

// CaserInitialismOverrides sets the initialism for any resources that are not covered by the default initialisms.
func CaserInitialismOverrides(overrides map[string]bool) ResourceOption {
	return Option(func(g any) error {
		switch t := g.(type) {
		case *client:
			caser = strcase.NewCaser(false, overrides, nil)
		case *resourceGenerator, *typescriptGenerator: // no-op
		default:
			panic(fmt.Sprintf("unexpected generator type in CaserInitialismOverrides(): %T", t))
		}

		return nil
	})
}

// WithConsolidatedHandlers enables generating a handler file for all or a list of resources.
func WithConsolidatedHandlers(route string, consolidateAll bool, resources ...string) ResourceOption {
	return Option(func(g any) error {
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
	})
}

// WithVirtualResources enables generating resources utilities, routes and handlers for Virtual Resources.
// The package's name is expected to be the same as its directory name.
func WithVirtualResources(virtualResourcesPkgDir string) ResourceOption {
	return Option(func(g any) error {
		switch t := g.(type) {
		case *resourceGenerator:
		case *typescriptGenerator: // no-op
		case *client:
			t.genVirtualResources = true
			t.virtual = packageDir(virtualResourcesPkgDir)
			t.loadPackages = append(t.loadPackages, virtualResourcesPkgDir)
		default:
			panic(fmt.Sprintf("unexpected generator type in WithVirtualResources(): %T", t))
		}

		return nil
	})
}

// WithComputedResources enables generating routes and handlers for Computed Resources.
// The package's name is expected to be the same as its directory name.
func WithComputedResources(compResourcesPkgDir string) ResourceOption {
	return Option(func(g any) error {
		switch t := g.(type) {
		case *resourceGenerator:
		case *typescriptGenerator: // no-op
		case *client:
			t.genComputedResources = true
			t.computed = packageDir(compResourcesPkgDir)
			t.loadPackages = append(t.loadPackages, compResourcesPkgDir)
		default:
			panic(fmt.Sprintf("unexpected generator type in WithComputedResources(): %T", t))
		}

		return nil
	})
}

// WithRPC enables generating RPC method handlers.
// The package's name is expected to be the same as its directory name.
func WithRPC(rpcPackageDir string) ResourceOption {
	return Option(func(g any) error {
		switch t := g.(type) {
		case *resourceGenerator:
		case *typescriptGenerator: // no-op
		case *client:
			t.rpc = packageDir(rpcPackageDir)
			t.genRPCMethods = true
			t.loadPackages = append(t.loadPackages, rpcPackageDir)
		default:
			panic(fmt.Sprintf("unexpected generator type in WithRPC(): %T", t))
		}

		return nil
	})
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
		if err := applyResourceGeneratorDefaults(g); err != nil {
			return err
		}

	case *typescriptGenerator:
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

// applyResourceGeneratorDefaults fills option defaults after all options have been
// applied, so defaults that depend on other options (e.g. the collection directory
// following the routes directory) see the final configuration.
func applyResourceGeneratorDefaults(g *resourceGenerator) error {
	if g.spannerEmulatorVersion == "" {
		g.spannerEmulatorVersion = "latest"
	}
	if g.applicationName == "" {
		g.applicationName = "App"
	}
	g.receiverName = strings.ToLower(string(g.applicationName[0]))

	// Each GenerateTypescript call owns one directory; two calls writing the same files
	// to the same place is always a configuration mistake.
	seen := make(map[string]struct{}, len(g.typescriptTargets))
	for _, target := range g.typescriptTargets {
		dir := filepath.Clean(target.destination)
		if _, ok := seen[dir]; ok {
			return errors.Newf("GenerateTypescript(%q) is declared more than once: each call must name a distinct target directory", target.destination)
		}
		seen[dir] = struct{}{}
	}

	return nil
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

// TypeScript type names emitted by the generator.
const (
	stringTSType    = "string"
	linkTSType      = "Link"
	numberTSType    = "number"
	uuidTSType      = "uuid"
	dateTSType      = "Date"
	civilDateTSType = "civilDate"
)

func defaultTypescriptOverrides() map[string]string {
	return map[string]string{
		reflect.TypeFor[ccc.UUID]().String():            uuidTSType,
		reflect.TypeFor[ccc.NullUUID]().String():        uuidTSType,
		reflect.TypeFor[resource.Link]().String():       linkTSType,
		reflect.TypeFor[resource.NullLink]().String():   linkTSType,
		reflect.TypeFor[decimal.Decimal]().String():     numberTSType,
		reflect.TypeFor[decimal.NullDecimal]().String(): numberTSType,
		reflect.TypeFor[time.Time]().String():           dateTSType,
		reflect.TypeFor[civil.Date]().String():          civilDateTSType,
		boolGoType:                                      booleanStr,
		stringGoType:                                    stringTSType,
		intGoType:                                       numberTSType,
		int8GoType:                                      numberTSType,
		int16GoType:                                     numberTSType,
		int32GoType:                                     numberTSType,
		int64GoType:                                     numberTSType,
		uintGoType:                                      numberTSType,
		uint8GoType:                                     numberTSType,
		uint16GoType:                                    numberTSType,
		uint32GoType:                                    numberTSType,
		uint64GoType:                                    numberTSType,
		uintptrGoType:                                   numberTSType,
		float32GoType:                                   numberTSType,
		float64GoType:                                   numberTSType,
		complex64GoType:                                 numberTSType,
		complex128GoType:                                numberTSType,
	}
}
