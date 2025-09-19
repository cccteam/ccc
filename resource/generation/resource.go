package generation

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/go-playground/errors/v5"
)

type resourceGenerator struct {
	*client
	genHandlers             bool
	genRoutes               bool
	resourceDestination     string
	handlerDestination      string
	routerDestination       string
	routerPackage           string
	routePrefix             string
	rpcPackageDir           string
	businessLayerPackageDir string
}

// NewResourceGenerator constructs a new Generator for generating a resource-driven API.
func NewResourceGenerator(ctx context.Context, resourceSourcePath, migrationSourceURL string, localPackages []string, options ...ResourceOption) (Generator, error) {
	r := &resourceGenerator{
		resourceDestination: filepath.Dir(resourceSourcePath),
	}

	opts := make([]option, 0, len(options))
	for _, opt := range options {
		opts = append(opts, opt)
	}

	c, err := newClient(ctx, resourceGeneratorType, resourceSourcePath, migrationSourceURL, localPackages, opts)
	if err != nil {
		return nil, err
	}

	r.client = c

	if err := resolveOptions(r, opts); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *resourceGenerator) Generate() error {
	log.Println("Starting ResourceGenerator Generation")

	begin := time.Now()

	packageMap, err := parser.LoadPackages(r.loadPackages...)
	if err != nil {
		return errors.Wrap(err, "parser.LoadPackages()")
	}

	resourcesPkg := parser.ParsePackage(packageMap["resources"])

	resources, err := r.extractResources(resourcesPkg.Structs)
	if err != nil {
		return err
	}

	r.resources = resources

	if err := r.runResourcesGeneration(); err != nil {
		return err
	}

	if err := r.generateEnums(resourcesPkg.NamedTypes); err != nil {
		return err
	}

	if r.genRPCMethods {
		rpcStructs := parser.ParsePackage(packageMap["rpc"]).Structs

		rpcStructs = parser.FilterStructsByInterface(rpcStructs, rpcInterfaces[:])

		r.rpcMethods, err = r.structsToRPCMethods(rpcStructs)
		if err != nil {
			return err
		}

		if err := r.runRPCGeneration(); err != nil {
			return err
		}
	}

	if r.genRoutes {
		if err := r.runRouteGeneration(); err != nil {
			return err
		}
	}
	if r.genHandlers {
		if err := r.runHandlerGeneration(); err != nil {
			return err
		}
	}

	// We always want to cache the consolidatedRoute data for the typescript gen
	if err := r.genCache.Store("app", consolidatedRouteCache, r.consolidateConfig); err != nil {
		return errors.Wrap(err, "cache.Cache.Store()")
	}

	if err := r.populateCache(); err != nil {
		return err
	}

	log.Printf("Finished Resource generation in %s\n", time.Since(begin))

	return nil
}

func (r *resourceGenerator) runResourcesGeneration() error {
	if err := removeGeneratedFiles(r.resourceDestination, prefix); err != nil {
		return err
	}

	if err := r.generateResourceInterfaces(); err != nil {
		return errors.Wrap(err, "c.generateResourceInterfaces()")
	}

	for _, res := range r.resources {
		if err := r.generateResources(res); err != nil {
			return errors.Wrap(err, "c.generateResources()")
		}
	}

	return nil
}

func (r *resourceGenerator) generateResourceInterfaces() error {
	output, err := r.generateTemplateOutput("resourcesInterfaceTemplate", resourcesInterfaceTemplate, map[string]any{
		"Source": r.resourceFilePath,
		"Types":  r.resources,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFile := filepath.Join(r.resourceDestination, generatedGoFileName(resourceInterfaceOutputName))

	file, err := os.Create(destinationFile)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	output, err = r.GoFormatBytes(file.Name(), output)
	if err != nil {
		return err
	}

	if err := r.WriteBytesToFile(file, output); err != nil {
		return err
	}

	return nil
}

func (r *resourceGenerator) generateResources(res *resourceInfo) error {
	begin := time.Now()
	fileName := generatedGoFileName(strings.ToLower(caser.ToSnake(r.pluralize(res.Name()))))
	destinationFilePath := filepath.Join(r.resourceDestination, fileName)

	output, err := r.generateTemplateOutput("resourceFileTemplate", resourceFileTemplate, map[string]any{
		"Source":   r.resourceFilePath,
		"Resource": res,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	output, err = r.GoFormatBytes(file.Name(), output)
	if err != nil {
		return err
	}

	if err := r.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated resource file in %s: %v\n", time.Since(begin), destinationFilePath)

	return nil
}

func (r *resourceGenerator) generateEnums(namedTypes []*parser.NamedType) error {
	enumMap, err := r.retrieveDatabaseEnumValues(namedTypes)
	if err != nil {
		return err
	}

	output, err := r.generateTemplateOutput("resourceEnumsTemplate", resourceEnumsTemplate, map[string]any{
		"Source":     r.resourceFilePath,
		"NamedTypes": namedTypes,
		"EnumMap":    enumMap,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	file, err := os.Create(filepath.Join(r.resourceDestination, generatedGoFileName(resourceEnumsFileName)))
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	output, err = r.GoFormatBytes(file.Name(), output)
	if err != nil {
		return err
	}

	if err := r.WriteBytesToFile(file, output); err != nil {
		return err
	}

	return nil
}
