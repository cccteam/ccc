package generation

import (
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
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
	Option         func(any) error

	option interface {
		isOption()
	}
	ResourceOption interface {
		option
		isResourceOption()
	}
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

// ignoredHandlers maps the name of a resource and to handler types (list, read, patch)
// that you do not want generated for that resource
func GenerateHandlers(targetDir string) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.genHandlers = true
		r.handlerDestination = targetDir

		return nil
	})
}

func GenerateRoutes(targetDir, targetPackage, routePrefix string) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.genRoutes = true
		r.routerDestination = targetDir
		r.routerPackage = targetPackage
		r.routePrefix = routePrefix

		return nil
	})
}

func WithTypescriptOverrides(overrides map[string]string) TSOption {
	return tsOption(func(t *typescriptGenerator) error {
		tempMap := defaultTypescriptOverrides()
		maps.Copy(tempMap, overrides)
		t.typescriptOverrides = tempMap

		return nil
	})
}

func GeneratePermissions() TSOption {
	return tsOption(func(t *typescriptGenerator) error {
		t.genPermission = true

		return nil
	})
}

func GenerateMetadata() TSOption {
	return tsOption(func(t *typescriptGenerator) error {
		t.genMetadata = true

		return nil
	})
}

func GenerateEnums() TSOption {
	return tsOption(func(t *typescriptGenerator) error {
		t.genEnums = true

		return nil
	})
}

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

func CaserInitialismOverrides(overrides map[string]bool) Option {
	return func(g any) error {
		switch t := g.(type) {
		case *client:
			t.caser = strcase.NewCaser(false, overrides, nil)
		case *resourceGenerator, *typescriptGenerator: // no-op
		default:
			panic(fmt.Sprintf("unexpected generator type in CaserInitialismOverrides(): %T", t))
		}

		return nil
	}
}

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

func WithRPC(rpcPackageDir string, businessPackageDir string) Option {
	rpcPackageDir = "./" + filepath.Clean(rpcPackageDir)

	return func(g any) error {
		switch t := g.(type) {
		case *resourceGenerator:
			t.rpcPackageDir = rpcPackageDir
			t.businessLayerPackageDir = businessPackageDir
		case *typescriptGenerator: // no-op
		case *client:
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
		"bool":                                         "boolean",
		"string":                                       "string",
		"int":                                          "number",
		"int8":                                         "number",
		"int16":                                        "number",
		"int32":                                        "number",
		"int64":                                        "number",
		"uint":                                         "number",
		"uint8":                                        "number",
		"uint16":                                       "number",
		"uint32":                                       "number",
		"uint64":                                       "number",
		"uintptr":                                      "number",
		"float32":                                      "number",
		"float64":                                      "number",
		"complex64":                                    "number",
		"complex128":                                   "number",
	}
}
