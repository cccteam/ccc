package generation

import (
	"maps"
	"reflect"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

type (
	resourceOption func(*resourceGenerator) error
	tsOption       func(*typescriptGenerator) error
	option         func(Generator) error

	Option interface {
		isOption()
	}
	ResourceOption interface {
		Option
		isResourceOption()
	}
	TSOption interface {
		Option
		isTypescriptOption()
	}
)

func (resourceOption) isOption()         {}
func (resourceOption) isResourceOption() {}

func (tsOption) isOption()           {}
func (tsOption) isTypescriptOption() {}

func (option) isOption()           {}
func (option) isResourceOption()   {}
func (option) isTypescriptOption() {}

// ignoredHandlers maps the name of a resource and to handler types (list, read, patch)
// that you do not want generated for that resource
func GenerateHandlers(targetDir string, ignoredHandlers map[string][]HandlerType) ResourceOption {
	return resourceOption(func(r *resourceGenerator) error {
		r.genHandlers = true
		r.handlerDestination = targetDir

		if ignoredHandlers != nil {
			r.handlerOptions = make(map[string]map[HandlerType][]OptionType)

			for structName, handlerTypes := range ignoredHandlers {
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

func WithPluralOverrides(overrides map[string]string) option {
	tempMap := defaultPluralOverrides()
	maps.Copy(tempMap, overrides)

	return func(g Generator) error {
		switch t := g.(type) {
		case *resourceGenerator:
			t.pluralOverrides = tempMap
		case *typescriptGenerator:
			t.pluralOverrides = tempMap
		}

		return nil
	}
}

func CaserInitialismOverrides(overrides map[string]bool) option {
	return func(g Generator) error {
		switch t := g.(type) {
		case *resourceGenerator:
			t.caser = strcase.NewCaser(false, overrides, nil)
		case *typescriptGenerator:
			t.caser = strcase.NewCaser(false, overrides, nil)
		}

		return nil
	}
}

func WithConsolidatedHandlers(route string, consolidateAll bool, resources ...string) option {
	return func(g Generator) error {
		if !consolidateAll && len(resources) == 0 {
			return errors.New("at least one resource is required if not consolidating all handlers")
		}

		switch t := g.(type) {
		case *resourceGenerator:
			t.consolidatedResourceNames = resources
			t.consolidatedRoute = route
			t.consolidateAll = consolidateAll
		case *typescriptGenerator:
			t.consolidatedResourceNames = resources
			t.consolidatedRoute = route
			t.consolidateAll = consolidateAll
		}

		return nil
	}
}

func WithRPC(rpcPackageDir string) option {
	return func(g Generator) error {
		switch t := g.(type) {
		case *resourceGenerator:
			t.genRPCMethods = true
			t.loadPackages = append(t.loadPackages, rpcPackageDir)
		case *typescriptGenerator:
			t.genRPCMethods = true
			t.loadPackages = append(t.loadPackages, rpcPackageDir)
		}

		return nil
	}
}

func resolveOptions(generator Generator, options []Option) error {
	for _, optionFunc := range options {
		if optionFunc != nil {
			switch fn := optionFunc.(type) {
			case resourceOption:
				if err := fn(generator.(*resourceGenerator)); err != nil {
					return err
				}
			case tsOption:
				if err := fn(generator.(*typescriptGenerator)); err != nil {
					return err
				}
			case option:
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

	case *typescriptGenerator:
		if g.pluralOverrides == nil {
			g.pluralOverrides = defaultPluralOverrides()
		}
		if g.typescriptOverrides == nil {
			g.typescriptOverrides = defaultTypescriptOverrides()
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
	}
}
