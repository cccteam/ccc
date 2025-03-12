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

func (r *resourceGenerator) runRouteGeneration() error {
	if err := removeGeneratedFiles(r.routerDestination, Prefix); err != nil {
		return err
	}

	generatedRoutesMap := make(routeMap)
	for _, resource := range r.resources {
		handlerTypes := r.resourceEndpoints(resource)

		for _, ht := range handlerTypes {
			path := fmt.Sprintf("/%s/%s", r.routePrefix, strcase.ToKebab(r.pluralize(resource.Name())))
			if ht == Read {
				path += fmt.Sprintf("/{%s}", strcase.ToGoCamel(resource.Name()+"ID"))
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
		log.Printf("Generating routes file: %s\n", routesDestination)
		if err := r.writeGeneratedRouterFile(routesDestination, routesTemplate, generatedRoutesMap); err != nil {
			return errors.Wrap(err, "c.writeRoutes()")
		}

		routerTestsDestination := filepath.Join(r.routerDestination, generatedFileName(routerTestOutputName))
		log.Printf("Generating router tests file: %s\n", routerTestsDestination)
		if err := r.writeGeneratedRouterFile(routerTestsDestination, routerTestTemplate, generatedRoutesMap); err != nil {
			return errors.Wrap(err, "c.writeRouterTests()")
		}
	}

	return nil
}

func (r *resourceGenerator) writeGeneratedRouterFile(destinationFile, templateContent string, generatedRoutes routeMap) error {
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
		"PackageName":            r.packageName,
		"RoutesMap":              generatedRoutes,
		"HasConsolidatedHandler": r.consolidatedRoute != "",
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
