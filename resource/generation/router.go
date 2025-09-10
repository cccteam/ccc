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
	if err := removeGeneratedFiles(r.routerDestination, prefix); err != nil {
		return err
	}

	generatedRoutesMap := make(map[string][]generatedRoute)
	for i := range r.resources {
		handlerTypes := resourceEndpoints(&r.resources[i])

		for _, ht := range handlerTypes {
			path := fmt.Sprintf("/%s/%s", r.routePrefix, strcase.ToKebab(r.pluralize(r.resources[i].Name())))
			if ht == ReadHandler {
				if r.resources[i].HasCompoundPrimaryKey() {
					for _, field := range r.resources[i].PrimaryKeys() {
						path += fmt.Sprintf("/{%s}", strcase.ToGoCamel(r.resources[i].Name()+field.Name()))
					}
				} else {
					path += fmt.Sprintf("/{%s}", strcase.ToGoCamel(r.resources[i].Name()+"ID"))
				}
			}

			generatedRoutesMap[r.resources[i].Name()] = append(generatedRoutesMap[r.resources[i].Name()], generatedRoute{
				Method:      ht.Method(),
				Path:        path,
				HandlerFunc: r.handlerName(r.resources[i].Name(), ht),
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
		routesDestination := filepath.Join(r.routerDestination, generatedGoFileName(routesOutputName))
		if err := r.writeGeneratedRouterFile(routesDestination, routesTemplate, r.resources, generatedRoutesMap); err != nil {
			return errors.Wrap(err, "c.writeRoutes()")
		}
		log.Printf("Generated routes file in %s: %s\n", time.Since(begin), routesDestination)

		routerTestsDestination := filepath.Join(r.routerDestination, generatedGoFileName(routerTestOutputName))
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
		"HasConsolidatedHandler": r.ConsolidatedRoute != "",
		"RoutePrefix":            r.routePrefix,
		"ConsolidatedRoute":      r.ConsolidatedRoute,
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
