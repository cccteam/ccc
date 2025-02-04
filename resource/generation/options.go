package generation

import (
	"github.com/cccteam/ccc/resource"
	"github.com/ettle/strcase"
)

type GenerationClientOption func(*GenerationClient) error

func GenerateHandlers(targetDir string, overrides map[string][]HandlerType) GenerationClientOption {
	return func(c *GenerationClient) error {
		c.genHandlers = func() error {
			return c.runHandlerGeneration()
		}

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

func GenerateTypescriptPermission(rc *resource.Collection, targetDir string) GenerationClientOption {
	return func(c *GenerationClient) error {
		c.genTypescriptPerm = func() error {
			return c.runTypescriptPermissionGeneration()
		}

		c.rc = rc
		c.typescriptDestination = targetDir

		return nil
	}
}

func GenerateTypescriptMetadata(rc *resource.Collection, targetDir string) GenerationClientOption {
	return func(c *GenerationClient) error {
		c.genTypescriptMeta = func() error {
			return c.runTypescriptMetadataGeneration()
		}

		c.rc = rc
		c.typescriptDestination = targetDir

		return nil
	}
}

func WithPluralOverrides(overrides map[string]string) GenerationClientOption {
	return func(c *GenerationClient) error {
		c.pluralOverrides = overrides

		return nil
	}
}

func CaserInitialismOverrides(overrides map[string]bool) GenerationClientOption {
	return func(c *GenerationClient) error {
		c.caser = strcase.NewCaser(false, overrides, nil)

		return nil
	}
}
