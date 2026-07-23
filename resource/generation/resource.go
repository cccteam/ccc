package generation

import (
	"context"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/go-playground/errors/v5"
)

type resourceGenerator struct {
	*client
	genHandlers         bool
	genRoutes           bool
	handler             packageDir
	router              packageDir
	routePrefix         string
	applicationName     string
	receiverName        string
	typescriptTargets   []typescriptTarget
	manualRegistrations []ManualRegistration
}

// NewResourceGenerator constructs a new Generator for generating a resource-driven API.
//
// localPackages lists import paths that generated code may reference beyond what the
// templates declare (project packages and third-party field-type packages goimports
// cannot resolve on its own). Standard-library paths are ignored: goimports resolves
// those natively and placing them in the local-package import group would produce
// output that editor format-on-save reorders.
func NewResourceGenerator(ctx context.Context, resourcePackageDir string, migrationSourceURL, localPackages []string, options ...ResourceOption) (Generator, error) {
	r := &resourceGenerator{}

	opts := make([]option, 0, len(options))
	for _, opt := range options {
		opts = append(opts, opt)
	}

	c, err := newClient(ctx, resourcePackageDir, migrationSourceURL, localPackages, opts)
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

	pkg := packageMap[r.resource.Package()]
	if pkg == nil {
		return errors.Newf("no packages found in %q", r.resource.Dir())
	}

	resourcesPkg := parser.ParsePackage(pkg)
	r.resources, err = r.structsToResources(resourcesPkg.Structs, r.validateStructNameMatchesFile(pkg, true))
	if err != nil {
		return err
	}

	annotatedRegistrations, err := manualRegistrationsFromConstants(resourcesPkg.Constants)
	if err != nil {
		return err
	}
	r.manualRegistrations = append(r.manualRegistrations, annotatedRegistrations...)

	if r.genVirtualResources {
		virtualStructs := parser.ParsePackage(packageMap[r.virtual.Package()]).Structs
		virtualResources, err := r.structsToVirtualResources(virtualStructs, r.validateStructNameMatchesFile(pkg, true))
		if err != nil {
			return err
		}

		r.resources = append(r.resources, virtualResources...)
		sortResources(r.resources)
	}

	if err := r.validateManualAddResourceSets(); err != nil {
		return err
	}

	if err := r.validateTypescriptTargets(); err != nil {
		return err
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

	if err := r.populateCache(); err != nil {
		return err
	}

	if err := r.runCollectionGeneration(); err != nil {
		return err
	}

	log.Printf("Finished Resource generation in %s\n", time.Since(begin))

	return nil
}

// runCollectionGeneration computes the permission collection and produces its outputs.
func (r *resourceGenerator) runCollectionGeneration() error {
	collectionData, err := r.computeCollectionData()
	if err != nil {
		return err
	}

	unifiedGenerators := make([]*typescriptGenerator, 0, len(r.typescriptTargets))
	for _, target := range r.typescriptTargets {
		unifiedTS, err := r.buildUnifiedTypescriptGenerator(collectionData, target)
		if err != nil {
			return err
		}
		unifiedGenerators = append(unifiedGenerators, unifiedTS)
	}

	// The collection file is a standard artifact of route generation: whatever the
	// generated routes register is emitted next to them for deployment tooling to
	// consume.
	if r.genRoutes {
		if err := r.generateCollectionFile(collectionData); err != nil {
			return err
		}
	}

	for _, unifiedTS := range unifiedGenerators {
		if err := unifiedTS.Generate(); err != nil {
			return err
		}
	}

	return nil
}

// generateCollectionFile emits the application's permission collection as a generated
// file in the router package, exposing Collection() for deployment tooling (role
// migration, bootstrap) to consume in place of runtime registration.
func (r *resourceGenerator) generateCollectionFile(data resource.CollectionData) error {
	begin := time.Now()
	destinationFilePath := filepath.Join(r.router.Dir(), generatedGoFileName(collectionOutputName))

	if err := r.writeFormattedGoFile(destinationFilePath, "collectionTemplate", collectionTemplate, &collectionFileData{
		Source:  r.resource.Dir(),
		Package: r.router.Package(),
		Data:    data,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}

	log.Printf("Generated collection file in %s: %s\n", time.Since(begin), destinationFilePath)

	return nil
}

// buildUnifiedTypescriptGenerator constructs the in-run TypeScript generator for one
// target directory, fed by the statically computed permission collection instead of a
// runtime-registered one.
func (r *resourceGenerator) buildUnifiedTypescriptGenerator(data resource.CollectionData, target typescriptTarget) (*typescriptGenerator, error) {
	gc, err := resource.NewGeneratedCollection(data)
	if err != nil {
		return nil, errors.Wrap(err, "resource.NewGeneratedCollection()")
	}

	t, err := target.resolve()
	if err != nil {
		return nil, err
	}
	t.client = r.client
	t.rc = gc
	t.routerResources = gc.Resources()

	return t, nil
}

// validateTypescriptTargets rejects TypeScript targets whose requested outputs have no
// permission source: permissions and metadata render from the computed collection, which
// derives from the generated route wiring plus the manual declarations, so a run with
// neither would emit them silently empty. Enum output reads only the schema and carries
// no requirement. It runs after parsing so annotation-declared registrations
// (@manualAddResource, @manualAddResourceSet) count as a permission source.
func (r *resourceGenerator) validateTypescriptTargets() error {
	if r.genRoutes || len(r.typescriptTargets) == 0 {
		return nil
	}

	hasManualDeclarations := len(r.manualRegistrations) > 0
	for _, res := range r.resources {
		if len(res.ManualAddResourceSets) > 0 {
			hasManualDeclarations = true
		}
	}
	if hasManualDeclarations {
		return nil
	}

	for _, target := range r.typescriptTargets {
		t, err := target.resolve()
		if err != nil {
			return err
		}

		var requested []string
		if t.genMetadata {
			requested = append(requested, "GenerateMetadata()")
		}
		if t.genPermission {
			requested = append(requested, "GeneratePermissions()")
		}
		if len(requested) > 0 {
			return errors.Newf("GenerateTypescript(%q) requests %s without a permission source: the collection they render derives from the generated route wiring, so enable GenerateRoutes(), or declare manual registrations (@manualAddResource, @manualAddResourceSet, or WithManualResources())", target.destination, strings.Join(requested, " and "))
		}
	}

	return nil
}

func (r *resourceGenerator) runResourcesGeneration() error {
	if err := removeGeneratedFiles(r.resource.Dir(), prefix); err != nil {
		return err
	}

	if r.genVirtualResources {
		if err := removeGeneratedFiles(r.virtual.Dir(), prefix); err != nil {
			return err
		}
	}

	for _, res := range r.resources {
		if err := r.generateResources(res); err != nil {
			return errors.Wrap(err, "c.generateResources()")
		}
	}

	return nil
}

func (r *resourceGenerator) generateResourceInterfaces() error {
	destinationFile := filepath.Join(r.handler.Dir(), generatedGoFileName(resourceInterfaceOutputName))

	if err := r.writeFormattedGoFile(destinationFile, "resourcesInterfaceTemplate", resourcesInterfaceTemplate, &resourceInterfacesData{
		Source:                   r.resource.Dir(),
		Package:                  r.handler.Package(),
		ResourcesPackage:         r.resource.Package(),
		ComputedResourcesPackage: r.computed.Package(),
		Types:                    r.resources,
		ComputedResourceTypes:    r.computedResources,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
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

	if err := r.writeFormattedGoFile(destinationFilePath, "resourceFileTemplate", resourceFileTemplate, &resourceFileData{
		Source:   r.resource.Dir(),
		Package:  packageName,
		Resource: res,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}

	log.Printf("Generated resource file in %s: %v\n", time.Since(begin), destinationFilePath)

	return nil
}

func (r *resourceGenerator) generateEnums(namedTypes []*parser.NamedType) error {
	enumMap, err := r.retrieveDatabaseEnumValues(namedTypes)
	if err != nil {
		return err
	}

	destinationFile := filepath.Join(r.resource.Dir(), generatedGoFileName(resourceEnumsFileName))

	if err := r.writeFormattedGoFile(destinationFile, "resourceEnumsTemplate", resourceEnumsTemplate, resourceEnumsData{
		Source:     r.resource.Dir(),
		Package:    r.resource.Package(),
		NamedTypes: namedTypes,
		EnumMap:    enumMap,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
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
