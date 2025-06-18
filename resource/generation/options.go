package generation

import (
	"maps"
	"path/filepath"
	"reflect"
	"slices"
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
func GenerateHandlers(targetDir string, ignoreHandlers SuppressHandlerGeneration) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.genHandlers = true
		r.handlerDestination = targetDir

		if ignoreHandlers != nil {
			r.handlerOptions = make(map[string]map[HandlerType][]OptionType)

			for structName, handlerTypes := range ignoreHandlers {
				if slices.Contains(handlerTypes, AllHandlers) {
					handlerTypes = []HandlerType{ListHandler, ReadHandler, PatchHandler}
				}
				for _, handlerType := range handlerTypes {
					if _, ok := r.handlerOptions[structName]; !ok {
						r.handlerOptions[structName] = make(map[HandlerType][]OptionType)
					}
					r.handlerOptions[structName][handlerType] = append(r.handlerOptions[structName][handlerType], NoGenerate)
				}
			}
		}

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

func WithSpannerEmulaterVersion(version string) Option {
	return func(g any) error {
		switch t := g.(type) {
		case *client:
			t.spannerEmulatorVersion = version
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
		}

		return nil
	}
}

func CaserInitialismOverrides(overrides map[string]bool) Option {
	return func(g any) error {
		switch t := g.(type) {
		case *client:
			t.caser = strcase.NewCaser(false, overrides, nil)
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
			t.consolidatedResourceNames = resources
			t.consolidatedRoute = route
			t.consolidateAll = consolidateAll
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
		case *client:
			t.genRPCMethods = true
			t.loadPackages = append(t.loadPackages, rpcPackageDir)
		}

		return nil
	}
}

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
				}
			case tsOption:
				switch g := generator.(type) {
				case *typescriptGenerator:
					if err := fn(g); err != nil {
						return err
					}
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
