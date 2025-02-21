package generation

import (
	"maps"

	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

type (
	ResourceOption func(*ResourceGenerator) error
	TSOption       func(*TypescriptGenerator) error
)

func GenerateHandlers(targetDir string, overrides map[string][]HandlerType) ResourceOption {
	return func(r *ResourceGenerator) error {
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
	return func(r *ResourceGenerator) error {
		r.genRoutes = true
		r.routerDestination = targetDir
		r.routerPackage = targetPackage
		r.routePrefix = routePrefix

		return nil
	}
}

func WithTypescriptOverrides(overrides map[string]string) TSOption {
	return func(t *TypescriptGenerator) error {
		tempMap := maps.Clone(_defaultTypescriptOverrides)
		maps.Copy(tempMap, overrides)
		t.typescriptOverrides = tempMap

		return nil
	}
}

func WithPluralOverrides[G ResourceGenerator | TypescriptGenerator](overrides map[string]string) func(*G) error {
	return func(g *G) error {
		switch g := any(g).(type) {
		case *ResourceGenerator:
			tempMap := maps.Clone(_defaultPluralOverrides)
			maps.Copy(tempMap, overrides)
			g.pluralOverrides = tempMap

		case *TypescriptGenerator:
			tempMap := maps.Clone(_defaultPluralOverrides)
			maps.Copy(tempMap, overrides)
			g.pluralOverrides = tempMap
		}

		return nil
	}
}

func CaserInitialismOverrides[G ResourceGenerator | TypescriptGenerator](overrides map[string]bool) func(*G) error {
	return func(g *G) error {
		switch g := any(g).(type) {
		case *ResourceGenerator:
			g.caser = strcase.NewCaser(false, overrides, nil)
		case *TypescriptGenerator:
			g.caser = strcase.NewCaser(false, overrides, nil)
		}

		return nil
	}
}

func WithConsolidatedHandlers[G ResourceGenerator | TypescriptGenerator](route string, consolidateAll bool, resources ...string) func(*G) error {
	return func(g *G) error {
		if !consolidateAll && len(resources) == 0 {
			return errors.New("at least one resource is required if not consolidating all handlers")
		}

		switch g := any(g).(type) {
		case *ResourceGenerator:
			g.consolidatedResourceNames = resources
			g.consolidatedRoute = route
			g.consolidateAll = consolidateAll
		case *TypescriptGenerator:
			g.consolidatedResourceNames = resources
			g.consolidatedRoute = route
			g.consolidateAll = consolidateAll
		}

		return nil
	}
}

func addToMap[K comparable, V any](destination, src map[K]V) map[K]V {
	for k, v := range src {
		destination[k] = v
	}

	return destination
}
