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
	for _, res := range r.resources {
		handlerTypes := resourceEndpoints(res)

		for _, ht := range handlerTypes {
			path := fmt.Sprintf("/%s/%s", r.routePrefix, strcase.ToKebab(r.pluralize(res.Name())))
			if ht == ReadHandler {
				if res.HasCompoundPrimaryKey() {
					for _, field := range res.PrimaryKeys() {
						path += fmt.Sprintf("/{%s}", strcase.ToGoCamel(res.Name()+field.Name()))
					}
				} else {
					path += fmt.Sprintf("/{%s}", strcase.ToGoCamel(res.Name()+"ID"))
				}
			}

			generatedRoutesMap[res.Name()] = append(generatedRoutesMap[res.Name()], generatedRoute{
				Method:        ht.method(),
				Path:          path,
				HandlerFunc:   r.handlerName(res.Name(), ht),
				SharedHandler: ht == ReadHandler || ht == ListHandler,
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

func (r *resourceGenerator) writeGeneratedRouterFile(destinationFile, templateContent string, resources []*resourceInfo, generatedRoutes map[string][]generatedRoute) error {
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
		"ComputedResources":      r.computedResources,
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
