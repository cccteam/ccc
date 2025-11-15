package generation

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/go-playground/errors/v5"
)

type resourceGenerator struct {
	*client
	genHandlers     bool
	genRoutes       bool
	handler         packageDir
	router          packageDir
	routePrefix     string
	applicationName string
	receiverName    string
}

// NewResourceGenerator constructs a new Generator for generating a resource-driven API.
func NewResourceGenerator(ctx context.Context, resourcePackageDir, migrationSourceURL string, localPackages []string, options ...ResourceOption) (Generator, error) {
	r := &resourceGenerator{}

	opts := make([]option, 0, len(options))
	for _, opt := range options {
		opts = append(opts, opt)
	}

	c, err := newClient(ctx, resourceGeneratorType, resourcePackageDir, migrationSourceURL, localPackages, opts)
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

	skippedErrors, packageMap, err := parser.LoadPackages(true, r.loadPackages...)
	if err != nil {
		return errors.Wrap(err, "parser.LoadPackages()")
	}

	pkg := packageMap[r.resource.Package()]
	if pkg == nil {
		return errors.Newf("no packages found in %q", r.resource.Dir())
	}

	resourcesPkg := parser.ParsePackage(pkg)
	r.resources, err = r.structsToResources(resourcesPkg.Structs, r.validateStructNameMatchesFile(pkg, true))
	if err != nil {
		return err
	}

	if r.genVirtualResources {
		virtualStructs := parser.ParsePackage(packageMap[r.virtual.Package()]).Structs
		virtualResources, err := r.structsToVirtualResources(virtualStructs, r.validateStructNameMatchesFile(pkg, true))
		if err != nil {
			return err
		}

		r.resources = append(r.resources, virtualResources...)
		sortResources(r.resources)
	}

	// needs to run before resource generation so the data can be sneakily snuck into resource generation
	if r.genComputedResources {
		compStructs := parser.ParsePackage(packageMap[r.computed.Package()]).Structs
		computedResources, err := structsToCompResources(compStructs, r.validateStructNameMatchesFile(pkg, true))
		if err != nil {
			return err
		}

		r.computedResources = computedResources
	}

	if err := r.runResourcesGeneration(); err != nil {
		return err
	}

	if err := r.generateEnums(resourcesPkg.NamedTypes); err != nil {
		return err
	}

	if r.genRPCMethods {
		rpcStructs := parser.ParsePackage(packageMap[r.rpc.Package()]).Structs
		if len(rpcStructs) == 0 {
			log.Printf("(RPC Generation) No structs in package %q annotated with @rpc", r.rpc.Dir())
		}

		r.rpcMethods, err = r.structsToRPCMethods(rpcStructs, r.validateStructNameMatchesFile(pkg, false))
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

	if skippedErrors {
		if _, _, err := parser.LoadPackages(false, r.loadPackages...); err != nil {
			return errors.Wrap(err, "parser.LoadPackages()")
		}
	}

	log.Printf("Finished Resource generation in %s\n", time.Since(begin))

	return nil
}

func (r *resourceGenerator) runResourcesGeneration() error {
	if err := removeGeneratedFiles(r.resource.Dir(), prefix); err != nil {
		return err
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
		"Source":                   r.resource.Dir(),
		"Package":                  r.handler.Package(),
		"ResourcesPackage":         r.resource.Package(),
		"ComputedResourcesPackage": r.computed.Package(),
		"Types":                    r.resources,
		"ComputedResourceTypes":    r.computedResources,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFile := filepath.Join(r.handler.Dir(), generatedGoFileName(resourceInterfaceOutputName))

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
	var (
		packageName         string
		destinationFilePath string
	)
	if !res.IsVirtual {
		packageName = r.resource.Package()
		destinationFilePath = filepath.Join(r.resource.Dir(), fileName)
	} else {
		packageName = r.virtual.Package()
		destinationFilePath = filepath.Join(r.virtual.Dir(), fileName)
	}

	output, err := r.generateTemplateOutput("resourceFileTemplate", resourceFileTemplate, map[string]any{
		"Source":   r.resource.Dir(),
		"Package":  packageName,
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
		"Source":     r.resource.Dir(),
		"Package":    r.resource.Package(),
		"NamedTypes": namedTypes,
		"EnumMap":    enumMap,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	file, err := os.Create(filepath.Join(r.resource.Dir(), generatedGoFileName(resourceEnumsFileName)))
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

func sortResources(s []*resourceInfo) {
	slices.SortFunc(s, func(a, b *resourceInfo) int {
		if a.Name() > b.Name() {
			return 1
		} else if a.Name() < b.Name() {
			return -1
		}

		return 0
	})
}
