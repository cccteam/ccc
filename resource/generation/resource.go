package generation

import (
	"context"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

type resourceGenerator struct {
	*client
	genHandlers     bool
	genRoutes       bool
	genHandlerTests bool
	handler         packageDir
	router          packageDir
	handlerTests    packageDir
	routePrefix     string
	applicationName string
	receiverName    string
	// domainRouteSegment/domainRouteParam form the route segment pair domain-scoped
	// resources are served under: /{prefix}/{domainRouteSegment}/{domainRouteParam}/...
	// Defaults: "domains"/"domain"; customized via WithDomainRoute.
	domainRouteSegment string
	domainRouteParam   string
	// extraOutlets are the router outlets declared by WithRouterOutlet, beyond the
	// default outlet GenerateRoutes declares. Resources join them via @outlet.
	extraOutlets        []routerOutlet
	typescriptTargets   []typescriptTarget
	manualRegistrations []ManualRegistration
}

// allOutlets returns every declared router outlet: the default outlet first,
// followed by the WithRouterOutlet declarations in option order.
func (r *resourceGenerator) allOutlets() []routerOutlet {
	outlets := make([]routerOutlet, 0, len(r.extraOutlets)+1)
	outlets = append(outlets, routerOutlet{name: defaultOutletName, prefix: r.routePrefix})

	return append(outlets, r.extraOutlets...)
}

// memberOutlets returns the declared outlets the membership joins, in declaration order.
func (r *resourceGenerator) memberOutlets(m *outletMembership) []routerOutlet {
	var outlets []routerOutlet
	for _, outlet := range r.allOutlets() {
		if m.OnOutlet(outlet.name) {
			outlets = append(outlets, outlet)
		}
	}

	return outlets
}

// validateOutletConfig checks the WithRouterOutlet declarations against each other and
// the default outlet: unique names, and prefixes that neither collide nor nest, so
// every outlet's URL space stays disjoint (the generated outlet-isolation tests rely
// on this).
func (r *resourceGenerator) validateOutletConfig() error {
	if len(r.extraOutlets) == 0 {
		return nil
	}
	if !r.genRoutes {
		return errors.New("WithRouterOutlet requires GenerateRoutes: outlets are registration surfaces of the generated router")
	}

	outlets := r.allOutlets()
	for i, outlet := range outlets {
		for _, prior := range outlets[:i] {
			if strings.EqualFold(prior.name, outlet.name) {
				return errors.Newf("WithRouterOutlet(%q) redeclares outlet %q", outlet.name, prior.name)
			}
			if prefixesNest(prior.prefix, outlet.prefix) {
				return errors.Newf("WithRouterOutlet(%q, %q) route prefix collides with outlet %q's prefix %q: outlet prefixes must not equal or nest within each other", outlet.name, outlet.prefix, prior.name, prior.prefix)
			}
		}
	}

	return nil
}

// prefixesNest reports whether one route prefix equals the other or sits beneath it
// as a path.
func prefixesNest(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// validateAnnotatedOutlets checks every @outlet annotation against the declared
// outlets, so a typo or an undeclared outlet fails generation instead of silently
// dropping routes.
func (r *resourceGenerator) validateAnnotatedOutlets() error {
	declared := make([]string, 0, len(r.extraOutlets)+1)
	for _, outlet := range r.allOutlets() {
		declared = append(declared, outlet.name)
	}

	var errs []error
	check := func(structName string, m *outletMembership) {
		for _, name := range m.OutletNames {
			if !slices.Contains(declared, name) {
				errs = append(errs, errors.Newf("@%s(%s) on %s references an undeclared outlet; declared outlets are %v (see WithRouterOutlet)", outletKeyword, name, structName, declared))
			}
		}
	}

	for _, res := range r.resources {
		check(res.Name(), &res.outletMembership)
	}
	for _, res := range r.computedResources {
		check(res.Name(), &res.outletMembership)
	}
	for _, rpcStruct := range r.rpcMethods {
		check(rpcStruct.Name(), &rpcStruct.outletMembership)
	}

	if len(errs) != 0 {
		return errors.Wrap(errors.Join(errs...), "outlet annotation error")
	}

	return nil
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

	if err := r.validateOutletConfig(); err != nil {
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
	r.resources, err = r.structsToResources(resourcesPkg.Structs, r.validateStructNameMatchesFile(pkg, true), validateNoPermTags)
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
		virtualResources, err := r.structsToVirtualResources(virtualStructs, r.validateStructNameMatchesFile(pkg, true), validateNoPermTags)
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
		computedResources, err := structsToCompResources(compStructs, r.validateStructNameMatchesFile(pkg, true), validateNoPermTags)
		if err != nil {
			return err
		}

		r.computedResources = computedResources
	}

	// The domain route parameter is derived from the parsed resources (tenant-record
	// pattern), so it must resolve before anything renders a domain route.
	r.deriveDomainRouteParam()

	if err := r.runResourcesGeneration(); err != nil {
		return err
	}

	if err := r.generateEnums(resourcesPkg.NamedTypes); err != nil {
		return err
	}

	if err := r.extractAndGenerateRPC(packageMap, pkg); err != nil {
		return err
	}

	// Runs after every annotated struct kind is extracted (rpc methods last).
	if err := r.validateAnnotatedOutlets(); err != nil {
		return err
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
	if r.genHandlerTests {
		if err := r.runHandlerTestsGeneration(); err != nil {
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

// extractAndGenerateRPC parses the RPC package into rpcMethods and runs the RPC
// generation, when enabled.
func (r *resourceGenerator) extractAndGenerateRPC(packageMap map[string]*packages.Package, pkg *packages.Package) error {
	if !r.genRPCMethods {
		return nil
	}

	rpcStructs := parser.ParsePackage(packageMap[r.rpc.Package()]).Structs
	if len(rpcStructs) == 0 {
		log.Printf("(RPC Generation) No structs in package %q annotated with @rpc", r.rpc.Dir())
	}

	var err error
	r.rpcMethods, err = r.structsToRPCMethods(rpcStructs, r.validateStructNameMatchesFile(pkg, false), validateNoPermTags)
	if err != nil {
		return err
	}

	return r.runRPCGeneration()
}

// runCollectionGeneration computes the permission collection and produces its outputs.
func (r *resourceGenerator) runCollectionGeneration() error {
	collectionData, err := r.computeCollectionData()
	if err != nil {
		return err
	}

	unifiedGenerators := make([]*typescriptGenerator, 0, len(r.typescriptTargets))
	if len(r.typescriptTargets) > 0 {
		// Every target shares one built collection: it's immutable and read-only once
		// constructed, so building and deriving from it once and reusing the pointer
		// across targets is safe and avoids redoing the same validation/sort per target.
		gc, err := resource.NewGeneratedCollection(collectionData)
		if err != nil {
			return errors.Wrap(err, "resource.NewGeneratedCollection()")
		}
		routerResources := gc.Resources()

		for _, target := range r.typescriptTargets {
			unifiedTS, err := r.buildUnifiedTypescriptGenerator(gc, routerResources, target)
			if err != nil {
				return err
			}
			unifiedGenerators = append(unifiedGenerators, unifiedTS)
		}
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
// runtime-registered one. gc and routerResources are shared across every target.
func (r *resourceGenerator) buildUnifiedTypescriptGenerator(gc *resource.GeneratedCollection, routerResources []accesstypes.Resource, target typescriptTarget) (*typescriptGenerator, error) {
	t, err := target.resolve()
	if err != nil {
		return nil, err
	}
	t.client = r.client
	t.rc = gc
	t.routerResources = routerResources
	t.domainRouteSegment = r.domainRouteSegment
	t.domainRouteParam = r.domainRouteParam

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
