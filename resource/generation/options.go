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
	Option interface {
		ResourceOption | TSOption
	}
	ResourceOption = func(*resourceGenerator) error
	TSOption       = func(*typescriptGenerator) error
)

func GenerateHandlers(targetDir string, overrides map[string][]HandlerType) ResourceOption {
	return func(r *resourceGenerator) error {
		r.genHandlers = true
		r.handlerDestination = targetDir

		if overrides != nil {
			r.handlerOptions = make(map[string]map[HandlerType][]OptionType)

			for structName, handlerTypes := range overrides {
				for _, handlerType := range handlerTypes {
					if _, ok := r.handlerOptions[structName]; !ok {
						r.handlerOptions[structName] = make(map[HandlerType][]OptionType)
					}
					r.handlerOptions[structName][handlerType] = append(r.handlerOptions[structName][handlerType], NoGenerate)
				}
			}
		}

		return nil
	}
}

func GenerateRoutes(targetDir, targetPackage, routePrefix string) ResourceOption {
	return func(r *resourceGenerator) error {
		r.genRoutes = true
		r.routerDestination = targetDir
		r.routerPackage = targetPackage
		r.routePrefix = routePrefix

		return nil
	}
}

func WithTypescriptOverrides(overrides map[string]string) TSOption {
	return func(t *typescriptGenerator) error {
		tempMap := defaultTypescriptOverrides()
		maps.Copy(tempMap, overrides)
		t.typescriptOverrides = tempMap

		return nil
	}
}

func WithPluralOverrides[Opt Option](overrides map[string]string) Opt {
	tempMap := defaultPluralOverrides()
	maps.Copy(tempMap, overrides)

	var opt Opt

	switch t := any(&opt).(type) {
	case *ResourceOption:
		*t = func(r *resourceGenerator) error {
			r.pluralOverrides = tempMap

			return nil
		}
	case *TSOption:
		*t = func(t *typescriptGenerator) error {
			t.pluralOverrides = tempMap

			return nil
		}
	}

	return opt
}

func CaserInitialismOverrides[Opt Option](overrides map[string]bool) Opt {
	var opt Opt

	switch t := any(&opt).(type) {
	case *ResourceOption:
		*t = func(r *resourceGenerator) error {
			r.caser = strcase.NewCaser(false, overrides, nil)

			return nil
		}
	case *TSOption:
		*t = func(t *typescriptGenerator) error {
			t.caser = strcase.NewCaser(false, overrides, nil)

			return nil
		}
	}

	return opt
}

func WithConsolidatedHandlers[Opt Option](route string, consolidateAll bool, resources ...string) Opt {
	var opt Opt

	switch t := any(&opt).(type) {
	case *ResourceOption:
		*t = func(r *resourceGenerator) error {
			if !consolidateAll && len(resources) == 0 {
				return errors.New("at least one resource is required if not consolidating all handlers")
			}
			r.consolidatedResourceNames = resources
			r.consolidatedRoute = route
			r.consolidateAll = consolidateAll

			return nil
		}
	case *TSOption:
		*t = func(t *typescriptGenerator) error {
			if !consolidateAll && len(resources) == 0 {
				return errors.New("at least one resource is required if not consolidating all handlers")
			}
			t.consolidatedResourceNames = resources
			t.consolidatedRoute = route
			t.consolidateAll = consolidateAll

			return nil
		}
	}

	return opt
}

func WithRPC[Opt Option](rpcPackageDir string) Opt {
	var opt Opt

	switch t := any(&opt).(type) {
	case *ResourceOption:
		*t = func(r *resourceGenerator) error {
			r.genRPCMethods = true
			r.loadPackages = append(r.loadPackages, rpcPackageDir)

			return nil
		}
	case *TSOption:
		*t = func(t *typescriptGenerator) error {
			t.genRPCMethods = true
			t.loadPackages = append(t.loadPackages, rpcPackageDir)

			return nil
		}
	}

	return opt
}

func resolveOptions[G Generator, Opt ~func(G) error](generator G, options []Opt) error {
	for _, optionFunc := range options {
		if optionFunc != nil {
			if err := optionFunc(generator); err != nil {
				return err
			}
		}
	}

	switch g := any(generator).(type) {
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
