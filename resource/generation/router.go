package generation

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"text/template"

	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

func (r *resourceGenerator) runRouteGeneration() error {
	if err := removeGeneratedFiles(r.routerDestination, Prefix); err != nil {
		return err
	}

	hasConsolidatedHandler := false

	generatedRoutesMap := make(map[string][]generatedRoute)
	for _, resource := range r.resources {
		opts := make(map[HandlerType]map[OptionType]any)
		for handlerType, options := range r.handlerOptions[resource.Name] {
			opts[handlerType] = make(map[OptionType]any)
			for _, option := range options {
				opts[handlerType][option] = struct{}{}
			}
		}

		handlerTypes := []HandlerType{List}
		if !resource.IsView {
			handlerTypes = append(handlerTypes, Read)

			if slices.Contains(r.consolidatedResourceNames, resource.Name) == r.consolidateAll {
				handlerTypes = append(handlerTypes, Patch)
			}
		}

		for _, h := range handlerTypes {
			if _, skipGeneration := opts[h][NoGenerate]; !skipGeneration {
				path := fmt.Sprintf("/%s/%s", r.routePrefix, strcase.ToKebab(r.pluralize(resource.Name)))
				if h == Read {
					path += fmt.Sprintf("/{%s}", strcase.ToGoCamel(resource.Name+"ID"))
				}

				generatedRoutesMap[resource.Name] = append(generatedRoutesMap[resource.Name], generatedRoute{
					Method:      h.Method(),
					Path:        path,
					HandlerFunc: r.handlerName(resource.Name, h),
				})
			}
		}

		if !resource.IsView && slices.Contains(r.consolidatedResourceNames, resource.Name) != r.consolidateAll {
			hasConsolidatedHandler = true
		}
	}

	if len(generatedRoutesMap) > 0 {
		routesDestination := filepath.Join(r.routerDestination, generatedFileName(routesOutputName))
		log.Printf("Generating routes file: %s\n", routesDestination)
		if err := r.writeGeneratedRouterFile(routesDestination, routesTemplate, generatedRoutesMap, hasConsolidatedHandler); err != nil {
			return errors.Wrap(err, "c.writeRoutes()")
		}

		routerTestsDestination := filepath.Join(r.routerDestination, generatedFileName(routerTestOutputName))
		log.Printf("Generating router tests file: %s\n", routerTestsDestination)
		if err := r.writeGeneratedRouterFile(routerTestsDestination, routerTestTemplate, generatedRoutesMap, hasConsolidatedHandler); err != nil {
			return errors.Wrap(err, "c.writeRouterTests()")
		}
	}

	return nil
}

func (r *resourceGenerator) writeGeneratedRouterFile(destinationFile, templateContent string, generatedRoutes map[string][]generatedRoute, hasConsolidatedHandler bool) error {
	file, err := os.Create(destinationFile)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	tmpl, err := template.New(filepath.Base(destinationFile)).Funcs(r.templateFuncs()).Parse(templateContent)
	if err != nil {
		return errors.Wrap(err, "template.New().Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, map[string]any{
		"Source":                 r.resourceFilePath,
		"Package":                r.routerPackage,
		"RoutesMap":              generatedRoutes,
		"HasConsolidatedHandler": hasConsolidatedHandler,
		"RoutePrefix":            r.routePrefix,
		"ConsolidatedRoute":      r.consolidatedRoute,
	}); err != nil {
		return errors.Wrap(err, "tmpl.Execute()")
	}

	if err := r.writeBytesToFile(destinationFile, file, buf.Bytes(), true); err != nil {
		return errors.Wrap(err, "c.writeBytesToFile()")
	}

	return nil
}
