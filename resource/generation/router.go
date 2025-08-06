package generation

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

func (r *resourceGenerator) runRouteGeneration() error {
	begin := time.Now()
	if err := RemoveGeneratedFiles(r.routerDestination, Prefix); err != nil {
		return err
	}

	generatedRoutesMap := make(map[string][]generatedRoute)
	for _, resource := range r.resources {
		handlerTypes := r.resourceEndpoints(resource)

		for _, ht := range handlerTypes {
			path := fmt.Sprintf("/%s/%s", r.routePrefix, strcase.ToKebab(r.pluralize(resource.Name())))
			if ht == ReadHandler {
				if resource.HasCompoundPrimaryKey() {
					for _, field := range resource.PrimaryKeys() {
						path += fmt.Sprintf("/{%s}", strcase.ToGoCamel(resource.Name()+field.Name()))
					}
				} else {
					path += fmt.Sprintf("/{%s}", strcase.ToGoCamel(resource.Name()+"ID"))
				}
			}

			generatedRoutesMap[resource.Name()] = append(generatedRoutesMap[resource.Name()], generatedRoute{
				Method:      ht.Method(),
				Path:        path,
				HandlerFunc: r.handlerName(resource.Name(), ht),
			})
		}
	}

	if r.genRPCMethods {
		for _, rpcStruct := range r.rpcMethods {
			generatedRoutesMap[rpcStruct.Name()] = []generatedRoute{{
				Method:      "POST",
				Path:        fmt.Sprintf("/%s/%s", r.routePrefix, strcase.ToKebab(rpcStruct.Name())),
				HandlerFunc: rpcStruct.Name(),
			}}
		}
	}

	if len(generatedRoutesMap) > 0 {
		routesDestination := filepath.Join(r.routerDestination, generatedFileName(routesOutputName))
		if err := r.writeGeneratedRouterFile(routesDestination, routesTemplate, r.resources, generatedRoutesMap); err != nil {
			return errors.Wrap(err, "c.writeRoutes()")
		}
		log.Printf("Generated routes file in %s: %s\n", time.Since(begin), routesDestination)

		routerTestsDestination := filepath.Join(r.routerDestination, generatedFileName(routerTestOutputName))
		begin = time.Now()
		if err := r.writeGeneratedRouterFile(routerTestsDestination, routerTestTemplate, r.resources, generatedRoutesMap); err != nil {
			return errors.Wrap(err, "c.writeRouterTests()")
		}
		log.Printf("Generated router tests file in %s: %s\n", time.Since(begin), routerTestsDestination)
	}

	return nil
}

func (r *resourceGenerator) writeGeneratedRouterFile(destinationFile, templateContent string, resources []resourceInfo, generatedRoutes map[string][]generatedRoute) error {
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
		"LocalPackageImports":    r.localPackageImports(),
		"RoutesMap":              generatedRoutes,
		"Resources":              resources,
		"HasConsolidatedHandler": r.consolidatedRoute != "",
		"RoutePrefix":            r.routePrefix,
		"ConsolidatedRoute":      r.consolidatedRoute,
	}); err != nil {
		return errors.Wrap(err, "tmpl.Execute()")
	}

	formattedBytes, err := r.GoFormatBytes(file.Name(), buf.Bytes())
	if err != nil {
		return err
	}

	if err := r.WriteBytesToFile(file, formattedBytes); err != nil {
		return err
	}

	return nil
}
