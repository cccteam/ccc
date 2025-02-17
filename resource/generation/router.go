package generation

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"text/template"

	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

func (c *Client) runRouteGeneration() error {
	if err := removeGeneratedFiles(c.routerDestination, Prefix); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	generatedRoutesMap := make(map[string][]generatedRoute)
	for _, s := range c.structNames {
		opts := make(map[HandlerType]map[OptionType]any)
		for handlerType, options := range c.handlerOptions[s] {
			opts[handlerType] = make(map[OptionType]any)
			for _, option := range options {
				opts[handlerType][option] = struct{}{}
			}
		}

		handlerTypes := []HandlerType{List}
		if md, ok := c.tableLookup[c.pluralize(s)]; ok && !md.IsView {
			handlerTypes = append(handlerTypes, Read, Patch)
		}

		for _, h := range handlerTypes {
			if _, skipGeneration := opts[h][NoGenerate]; !skipGeneration {
				path := fmt.Sprintf("/%s/%s", c.routePrefix, strcase.ToKebab(c.pluralize(s)))
				if h == Read {
					path += fmt.Sprintf("/{%s}", strcase.ToGoCamel(s+"ID"))
				}

				generatedRoutesMap[s] = append(generatedRoutesMap[s], generatedRoute{
					Method:      h.Method(),
					Path:        path,
					HandlerFunc: c.handlerName(s, h),
				})
			}
		}
	}

	if len(generatedRoutesMap) > 0 {
		routesDestination := filepath.Join(c.routerDestination, generatedFileName(routesName))
		log.Printf("Generating routes file: %s\n", routesDestination)
		if err := c.writeGeneratedRouterFile(routesDestination, routesTemplate, generatedRoutesMap); err != nil {
			return errors.Wrap(err, "c.writeRoutes()")
		}

		routerTestsDestination := filepath.Join(c.routerDestination, generatedFileName(routerTestName))
		log.Printf("Generating router tests file: %s\n", routerTestsDestination)
		if err := c.writeGeneratedRouterFile(routerTestsDestination, routerTestTemplate, generatedRoutesMap); err != nil {
			return errors.Wrap(err, "c.writeRouterTests()")
		}
	}

	return nil
}

func (c *Client) writeGeneratedRouterFile(destinationFile, templateContent string, generatedRoutes map[string][]generatedRoute) error {
	file, err := os.Create(destinationFile)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	tmpl, err := template.New(filepath.Base(destinationFile)).Funcs(c.templateFuncs()).Parse(templateContent)
	if err != nil {
		return errors.Wrap(err, "template.New().Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, map[string]any{
		"Source":    c.resourceFilePath,
		"Package":   c.routerPackage,
		"RoutesMap": generatedRoutes,
	}); err != nil {
		return errors.Wrap(err, "tmpl.Execute()")
	}

	if err := c.writeBytesToFile(destinationFile, file, buf.Bytes(), true); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	return nil
}
