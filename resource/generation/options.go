package generation

import (
	"errors"

	"github.com/cccteam/ccc/resource"
	"github.com/ettle/strcase"
)

type ClientOption func(*Client) error

func GenerateHandlers(targetDir string, overrides map[string][]HandlerType) ClientOption {
	return func(c *Client) error {
		c.genHandlers = true
		c.handlerDestination = targetDir

		if overrides != nil {
			c.handlerOptions = make(map[string]map[HandlerType][]OptionType)

			for structName, handlerTypes := range overrides {
				for _, handlerType := range handlerTypes {
					if _, ok := c.handlerOptions[structName]; !ok {
						c.handlerOptions[structName] = make(map[HandlerType][]OptionType)
					}
					c.handlerOptions[structName][handlerType] = append(c.handlerOptions[structName][handlerType], NoGenerate)
				}
			}
		}

		return nil
	}
}

func GenerateTypescriptPermission(rc *resource.Collection, targetDir string) ClientOption {
	return func(c *Client) error {
		if rc == nil {
			return errors.New("resource collection cannot be nil")
		}

		c.genTypescriptPerm = true
		c.rc = rc
		c.routerResources = rc.Resources()
		c.typescriptDestination = targetDir

		return nil
	}
}

func GenerateTypescriptMetadata(rc *resource.Collection, targetDir string) ClientOption {
	return func(c *Client) error {
		if rc == nil {
			return errors.New("resource collection cannot be nil")
		}

		c.genTypescriptMeta = true
		c.rc = rc
		c.routerResources = rc.Resources()
		c.typescriptDestination = targetDir

		return nil
	}
}

func GenerateRoutes(targetDir, targetPackage, routePrefix string) ClientOption {
	return func(c *Client) error {
		c.genRoutes = true
		c.routerDestination = targetDir
		c.routerPackage = targetPackage
		c.routePrefix = routePrefix

		return nil
	}
}

func WithPluralOverrides(overrides map[string]string) ClientOption {
	return func(c *Client) error {
		c.pluralOverrides = overrides

		return nil
	}
}

func CaserInitialismOverrides(overrides map[string]bool) ClientOption {
	return func(c *Client) error {
		c.caser = strcase.NewCaser(false, overrides, nil)

		return nil
	}
}

func WithTypescriptOverrides(overrides map[string]string) ClientOption {
	return func(c *Client) error {
		c.typescriptOverrides = overrides

		return nil
	}
}

func WithConsolidatedHandlers(route string, resources ...string) ClientOption {
	return func(c *Client) error {
		c.consolidatedResourceNames = resources
		c.consolidatedRoute = route
		c.consolidateAll = false

		return nil
	}
}

func WithoutConsolidatedHandlers(route string, resources ...string) ClientOption {
	return func(c *Client) error {
		c.consolidatedResourceNames = resources
		c.consolidatedRoute = route
		c.consolidateAll = true

		return nil
	}
}
