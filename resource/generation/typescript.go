package generation

import (
	"context"
	"fmt"
	"go/token"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/cache"
	cccpkg "github.com/cccteam/ccc/pkg"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

// permissionCollection is the read surface TypeScript generation needs; it is satisfied
// by both the deprecated runtime-populated *resource.Collection and the statically built
// *resource.GeneratedCollection.
type permissionCollection interface {
	Resources() []accesstypes.Resource
	ResourceExists(accesstypes.Resource) bool
	TypescriptData() *resource.TypescriptData
}

type typescriptGenerator struct {
	*client
	genPermission          bool
	genMetadata            bool
	genEnums               bool
	typescriptDestination  string
	typescriptOverrides    map[string]string
	rc                     permissionCollection
	routerResources        []accesstypes.Resource
	spannerEmulatorVersion string

	// parityRuntime and parityData drive the legacy parity check: the deprecated
	// standalone generator verifies the runtime-registered collection against the
	// Resource Generator's cached static collection during Generate, where parsing
	// supplies the struct declaration locations the guidance points at. Both are unset
	// on the unified path.
	parityRuntime *resource.Collection
	parityData    resource.CollectionData
}

// noopGenerator is returned by the deprecated NewTypescriptGenerator when the Resource
// Generator already emitted TypeScript in this pipeline run.
type noopGenerator struct{}

func (noopGenerator) Generate() error { return nil }
func (noopGenerator) Close() error    { return nil }

// NewTypescriptGenerator constructs a new Generator for generating Typescript for a resource-driven Angular app.
//
// While the Resource Generator does not emit TypeScript, the legacy path runs after
// verifying that the runtime-registered collection matches the statically computed one.
// Once the Resource Generator emits TypeScript, this constructor verifies both
// generators are configured identically and returns a no-op Generator.
//
// The options parameter accepts both TypeScript-specific and generator options: this
// constructor builds its own generation client and cannot inherit them.
//
// Deprecated: enable the Resource Generator's GenerateTypescript option and follow the
// staged instructions this constructor logs; TypeScript is then generated in the same
// run without the collect_resource_permissions build tag.
func NewTypescriptGenerator(ctx context.Context, resourceSourcePath string, migrationSourceURL []string, targetDir string, rc *resource.Collection, options ...option) (Generator, error) {
	if rc == nil {
		return nil, errors.New("resource collection cannot be nil")
	}

	marker, collectionData, _, haveData, err := loadTypescriptHandoff(resourceSourcePath)
	if err != nil {
		return nil, err
	}

	// The handoff is per target directory: destinations the Resource Generator already
	// emits to become verified no-ops, while the rest keep the legacy path, so a
	// multi-destination program migrates one destination at a time.
	if config := marker.configFor(targetDir); config != nil {
		return verifiedNoopGenerator(config, targetDir, options, rc, collectionData, haveData)
	}

	if !haveData {
		return nil, errors.New("no statically computed permission collection found in the generation cache: run the Resource Generator first (a Resource Generator run with this library version is required before the deprecated TypeScript generator can run)")
	}

	// Parity between the runtime-registered and statically computed collections is
	// verified in Generate, after parsing: the parsed structs supply the declaration
	// locations the mismatch guidance points at.
	t := &typescriptGenerator{
		rc:                    rc,
		routerResources:       rc.Resources(),
		typescriptDestination: targetDir,
		parityRuntime:         rc,
		parityData:            collectionData,
	}

	c, err := newClient(ctx, typeScriptGeneratorType, resourceSourcePath, migrationSourceURL, nil, options)
	if err != nil {
		return nil, err
	}

	t.client = c

	if err := resolveOptions(t, options); err != nil {
		return nil, err
	}

	return t, nil
}

// verifiedNoopGenerator returns the no-op Generator for a pipeline whose Resource
// Generator already emitted TypeScript to this generator's target directory, after
// verifying that this generator's configuration matches the Resource Generator's and
// that every registration made through the deprecated Collection.AddResource is declared
// to the generator.
func verifiedNoopGenerator(config *typescriptRunConfig, targetDir string, options []option, rc *resource.Collection, collectionData resource.CollectionData, haveData bool) (Generator, error) {
	ownConfig, err := deprecatedTypescriptRunConfig(targetDir, options)
	if err != nil {
		return nil, err
	}

	if diffs := config.diff(&ownConfig); len(diffs) > 0 {
		lines := []string{
			"TYPESCRIPT CONFIGURATION MISMATCH",
			"",
		}
		lines = append(lines, wrapText("The Resource Generator's GenerateTypescript configuration does not match this deprecated TypeScript generator's configuration:", "")...)
		for _, diff := range diffs {
			lines = append(lines, "")
			lines = append(lines, wrapText("• "+diff, "  ")...)
		}
		lines = append(lines, "")
		lines = append(lines, wrapText("Reconcile the NewResourceGenerator options with this program's options and rerun go generate; only delete this program once this check passes.", "")...)
		log.Println("ERROR:" + banner(lines...))

		return nil, errors.New("the Resource Generator's GenerateTypescript configuration does not match this deprecated TypeScript generator's configuration (see the details above)")
	}

	declaredManualCount, missing, err := verifyManualRegistrations(rc, collectionData, haveData)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		lines := []string{
			"UNDECLARED MANUAL REGISTRATIONS",
			"",
		}
		lines = append(lines, wrapText("Runtime code registers permissions through the deprecated Collection.AddResource that are not declared to the generator:", "")...)
		for _, registration := range missing {
			lines = append(lines, "", fmt.Sprintf("• permission %q on resource %q (scope %q)", registration.Permission, registration.Resource, registration.Scope))
		}
		lines = append(lines, "")
		lines = append(lines, wrapText("Declare each with an `// @manualAddResource(<permission>[, <scope>])` annotation on its resource constant, or with generation.WithManualResources(), then rerun go generate.", "")...)
		log.Println("ERROR:" + banner(lines...))

		return nil, errors.New("runtime code registers permissions that are not declared to the generator (see the details above)")
	}

	lines := []string{
		"TYPESCRIPT ALREADY GENERATED BY THE RESOURCE GENERATOR",
		"",
	}
	lines = append(lines, wrapText(fmt.Sprintf("This program's configuration for %q matches the Resource Generator's GenerateTypescript configuration; this call is a no-op.", targetDir), "")...)
	lines = append(lines, "", "NEXT STEP:")
	lines = append(lines, wrapText("  Remove this NewTypescriptGenerator call; once every call in this program is a no-op, delete the program and its //go:generate line.", "  ")...)
	if declaredManualCount > 0 {
		lines = append(lines, "", "ALSO:")
		lines = append(lines, wrapText(fmt.Sprintf("  All %d registration(s) made through the deprecated Collection.AddResource are declared to the generator; you can remove those AddResource calls (keep the permission enforcement they guard).", declaredManualCount), "  ")...)
	}
	log.Println("INFO:" + banner(lines...))

	return noopGenerator{}, nil
}

// verifyManualRegistrations checks every runtime registration made through the
// deprecated Collection.AddResource against the statically computed collection,
// returning the undeclared ones and, when all are declared, how many — so the caller
// can hint that the AddResource calls can be removed.
func verifyManualRegistrations(rc *resource.Collection, collectionData resource.CollectionData, haveData bool) (declared int, missing []resource.ManualRegistration, err error) {
	manualRegistrations := rc.ManualRegistrations()
	if !haveData || len(manualRegistrations) == 0 {
		return 0, nil, nil
	}

	gc, err := resource.NewGeneratedCollection(collectionData)
	if err != nil {
		return 0, nil, errors.Wrap(err, "resource.NewGeneratedCollection()")
	}

	for _, registration := range manualRegistrations {
		if !gc.HasPermission(registration.Scope, registration.Permission, registration.Resource) {
			missing = append(missing, registration)
		}
	}

	if len(missing) > 0 {
		return 0, missing, nil
	}

	return len(manualRegistrations), nil, nil
}

// deprecatedTypescriptRunConfig resolves the deprecated TypeScript generator's options
// into the comparable configuration, mirroring how the Resource Generator records its
// GenerateTypescript configuration in the marker.
func deprecatedTypescriptRunConfig(targetDir string, options []option) (typescriptRunConfig, error) {
	c := &client{}
	if err := resolveOptions(c, options); err != nil {
		return typescriptRunConfig{}, err
	}

	t := &typescriptGenerator{}
	if err := resolveOptions(t, options); err != nil {
		return typescriptRunConfig{}, err
	}

	return typescriptRunConfigFrom(t, c, targetDir), nil
}

// declLocation names where a declaration lives, so parity guidance can point at the
// file the annotation belongs in.
type declLocation struct {
	Name     string
	Position string // file:line, module-root-relative when resolvable
}

// relativePosition renders a fileset position as file:line, module-root-relative when
// possible: the generator runs from the module root, so relative paths are clickable
// where the generate command ran.
func relativePosition(cwd string, pos token.Position) string {
	file := pos.Filename
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, file); err == nil && !strings.HasPrefix(rel, "..") {
			file = rel
		}
	}

	return fmt.Sprintf("%s:%d", file, pos.Line)
}

// workingDir returns the current directory for position relativization, empty when
// unresolvable (positions then render absolute).
func workingDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	return cwd
}

// structLocations records into dest, keyed by registered (pluralized) resource name,
// where each parsed struct is declared.
func (t *typescriptGenerator) structLocations(dest map[accesstypes.Resource]declLocation, fset *token.FileSet, structs []*parser.Struct) {
	cwd := workingDir()
	for _, s := range structs {
		dest[accesstypes.Resource(t.pluralize(s.Name()))] = declLocation{
			Name:     s.Name(),
			Position: relativePosition(cwd, fset.Position(s.Pos())),
		}
	}
}

// constantLocations records into dest, keyed by constant value (the name manual
// registrations are recorded under), where each accesstypes.Resource constant is
// declared.
func constantLocations(dest map[accesstypes.Resource]declLocation, fset *token.FileSet, constants []*parser.Constant) {
	cwd := workingDir()
	for _, c := range constants {
		if c.TypeName() != accesstypesResourceType {
			continue
		}

		dest[accesstypes.Resource(c.Value())] = declLocation{
			Name:     c.Name(),
			Position: relativePosition(cwd, fset.Position(c.Pos())),
		}
	}
}

// verifyLegacyCollectionParity hard-errors when the runtime-registered collection
// diverges from the statically computed one, logging remediation as a bordered box and
// returning a short error for the caller's panic or log.Fatal. On a match it logs the
// migration readiness banner the deprecated path exists to reach. structLocs and
// constantLocs point the guidance at the declarations the annotations belong on.
func (t *typescriptGenerator) verifyLegacyCollectionParity(structLocs, constantLocs map[accesstypes.Resource]declLocation) error {
	gc, err := resource.NewGeneratedCollection(t.parityData)
	if err != nil {
		return errors.Wrap(err, "resource.NewGeneratedCollection()")
	}

	if diffs := resource.DiffCollections(t.parityRuntime, gc); len(diffs) > 0 {
		bullets, footer := collectionMismatchLines(diffs, structLocs, constantLocs)

		lines := []string{
			"PERMISSION COLLECTION MISMATCH",
			"",
		}
		lines = append(lines, wrapText("The runtime-registered permission collection does not match the collection computed statically by the Resource Generator:", "")...)
		for _, bullet := range bullets {
			lines = append(lines, "")
			lines = append(lines, wrapText("• "+bullet, "  ")...)
		}
		lines = append(lines, "")
		lines = append(lines, wrapText(footer, "")...)
		log.Println("ERROR:" + banner(lines...))

		return errors.New("the runtime-registered permission collection does not match the statically computed collection (see the details above)")
	}

	lines := make([]string, 0, 16)
	lines = append(lines, "READY FOR SINGLE-RUN GENERATION", "")
	lines = append(lines, wrapText("The statically computed permission collection matches the runtime-registered collection.", "")...)
	lines = append(lines, "", "NEXT STEP:", "  1. Add to the NewResourceGenerator options:")
	lines = append(lines, wrapText(fmt.Sprintf("       generation.GenerateTypescript(%q, <only the TypeScript-specific options this program passes: GenerateMetadata, GeneratePermissions, GenerateEnums, WithTypescriptOverrides>)", t.typescriptDestination), "           ")...)
	lines = append(lines, wrapText("     Package locations, the Spanner emulator version, and all other settings are inherited from the Resource Generator.", "     ")...)
	lines = append(lines, "  2. Rerun go generate.", "")
	lines = append(lines, wrapText("Keep this program in place: its next run verifies the two configurations match and reports when it is safe to delete it.", "")...)
	log.Println("INFO:" + banner(lines...))

	return nil
}

// collectionMismatchLines renders parity differences actionable-first: groups with a
// resource-level permission difference each collapse into one bullet naming the exact
// declaration to add (folding in the field tags that same declaration resolves), and
// while any exist, field-level-only groups are summarized rather than listed — fixing
// the declarations and rerunning surfaces whatever genuinely remains. Only when no
// actionable group is left are the field-level differences printed individually.
func collectionMismatchLines(diffs []resource.CollectionDiff, structLocs, constantLocs map[accesstypes.Resource]declLocation) (bullets []string, footer string) {
	var actionable, fieldOnly []*resource.CollectionDiff
	for i := range diffs {
		if diff := &diffs[i]; len(diff.Permissions) > 0 {
			actionable = append(actionable, diff)
		} else {
			fieldOnly = append(fieldOnly, diff)
		}
	}

	if len(actionable) == 0 {
		for _, diff := range fieldOnly {
			bullets = append(bullets, fieldDiffLines(diff)...)
		}

		return bullets, "Field-level differences without a permission difference mean the runtime request struct and the generator's declaration disagree on fields: align the hand-written handler's request struct with the declared shape, or rerun the Resource Generator if generated code is stale. If neither applies, report this as a resource/generation bug."
	}

	for _, diff := range actionable {
		bullets = append(bullets, actionableDiffLines(diff, structLocs, constantLocs)...)
	}

	if len(fieldOnly) > 0 {
		count := 0
		for _, diff := range fieldOnly {
			count += diff.FieldDiffCount()
		}
		bullets = append(bullets, fmt.Sprintf("%d field-level difference(s) on %d other resource(s) are not shown: resolve the items above and rerun go generate to surface anything that remains", count, len(fieldOnly)))
	}

	return bullets, "Each item above names the declaration that resolves it where one is known. If an item does not fit its description, report this as a resource/generation bug."
}

// actionableDiffLines renders one resource's permission-level differences: manual
// AddResource registrations each name their exact @manualAddResource annotation, and
// the remaining permissions collapse into one bullet naming the @manualAddResourceSet
// (or the stale-code remediation) that resolves them together with the group's field
// tags.
func actionableDiffLines(diff *resource.CollectionDiff, structLocs, constantLocs map[accesstypes.Resource]declLocation) []string {
	var lines []string

	setPerms := diff.Permissions
	if diff.RuntimeOnly {
		for _, reg := range diff.ManualRegistrations {
			constant := "the resource constant"
			if loc, ok := constantLocs[reg.Resource]; ok {
				constant = fmt.Sprintf("the %s constant at %s", loc.Name, loc.Position)
			}
			lines = append(lines, fmt.Sprintf("resource %q scope %q permission %q was registered at runtime by the deprecated Collection.AddResource but is not declared to the generator: add a `// %s` annotation to %s, or declare it with generation.WithManualResources()", diff.Resource, diff.Scope, reg.Permission, reg.Annotation(), constant))
		}

		setPerms = nonManualPermissions(diff)
		if len(setPerms) == 0 {
			return lines
		}
	}

	permList := quotePermissions(setPerms)
	fieldNote := ""
	if n := diff.FieldDiffCount(); n > 0 {
		fieldNote = fmt.Sprintf(" (and %d field tag(s) resolved by the same declaration)", n)
	}

	if !diff.RuntimeOnly {
		declaredAt := ""
		if loc, ok := structLocs[diff.Resource]; ok {
			declaredAt = fmt.Sprintf("; the resource is declared by the %s struct at %s", loc.Name, loc.Position)
		} else if loc, ok := constantLocs[diff.Resource]; ok {
			declaredAt = fmt.Sprintf("; the resource is declared by the %s constant at %s", loc.Name, loc.Position)
		}

		return append(lines, fmt.Sprintf("resource %q scope %q permission(s) %s%s are in the generated collection but never register at runtime: this usually means stale generated code (rerun go generate) or an @manualAddResourceSet/@manualAddResource declaration for a registration the runtime does not perform — remove or correct the declaration%s", diff.Resource, diff.Scope, permList, fieldNote, declaredAt))
	}

	args, ok := manualSetHandlerArgs(setPerms)
	if !ok {
		return append(lines, fmt.Sprintf("resource %q scope %q permission(s) %s%s are registered at runtime but missing from the generated collection: this usually comes from a hand-written Collection.AddMethodResource call or stale generated code (rerun the Resource Generator)", diff.Resource, diff.Scope, permList, fieldNote))
	}

	target := "the resource struct"
	if loc, ok := structLocs[diff.Resource]; ok {
		target = fmt.Sprintf("the %s struct at %s", loc.Name, loc.Position)
	}
	scopeNote := ""
	if diff.Scope != accesstypes.GlobalPermissionScope {
		scopeNote = fmt.Sprintf(" and declare the scope with `// @permissionScope(%s)`", diff.Scope)
	}

	return append(lines, fmt.Sprintf("resource %q scope %q permission(s) %s%s are registered at runtime by a hand-written handler but not declared to the generator: add `// @manualAddResourceSet(%s)` to %s%s", diff.Resource, diff.Scope, permList, fieldNote, strings.Join(args, ", "), target, scopeNote))
}

// fieldDiffLines renders one resource's field-level differences individually, one line
// per tag, tag permission, or immutable marker.
func fieldDiffLines(diff *resource.CollectionDiff) []string {
	prefix := "registered at runtime but missing from generated collection"
	if !diff.RuntimeOnly {
		prefix = "in generated collection but never registered at runtime"
	}

	var lines []string
	for _, tag := range diff.Tags {
		lines = append(lines, fmt.Sprintf("%s: resource %q scope %q tag %q", prefix, diff.Resource, diff.Scope, tag))
	}
	for _, tag := range slices.Sorted(maps.Keys(diff.TagPermissions)) {
		for _, perm := range diff.TagPermissions[tag] {
			lines = append(lines, fmt.Sprintf("%s: resource %q scope %q tag %q permission %q", prefix, diff.Resource, diff.Scope, tag, perm))
		}
	}
	for _, tag := range diff.ImmutableTags {
		lines = append(lines, fmt.Sprintf("%s: resource %q scope %q immutable tag %q", prefix, diff.Resource, diff.Scope, tag))
	}

	return lines
}

// nonManualPermissions returns the group's permission differences that did not arrive
// through the deprecated Collection.AddResource — the ones a hand-written handler's
// decoder registered as a Set.
func nonManualPermissions(diff *resource.CollectionDiff) []accesstypes.Permission {
	manual := make(map[accesstypes.Permission]struct{}, len(diff.ManualRegistrations))
	for _, reg := range diff.ManualRegistrations {
		manual[reg.Permission] = struct{}{}
	}

	var perms []accesstypes.Permission
	for _, perm := range diff.Permissions {
		if _, ok := manual[perm]; !ok {
			perms = append(perms, perm)
		}
	}

	return perms
}

// manualSetHandlerArgs maps Set-shaped permissions onto the @manualAddResourceSet
// handler-type arguments that register them, in canonical handler order. It reports
// false when a permission (such as Execute) is not registered through a handler Set.
func manualSetHandlerArgs(perms []accesstypes.Permission) ([]string, bool) {
	var list, read, patch bool
	for _, perm := range perms {
		switch perm {
		case accesstypes.List:
			list = true
		case accesstypes.Read:
			read = true
		case accesstypes.Create, accesstypes.Update, accesstypes.Delete:
			patch = true
		default:
			return nil, false
		}
	}

	var args []string
	if list {
		args = append(args, string(ListHandler))
	}
	if read {
		args = append(args, string(ReadHandler))
	}
	if patch {
		args = append(args, string(PatchHandler))
	}

	return args, true
}

// quotePermissions renders permissions as a quoted, comma-separated list.
func quotePermissions(perms []accesstypes.Permission) string {
	quoted := make([]string, 0, len(perms))
	for _, perm := range perms {
		quoted = append(quoted, fmt.Sprintf("%q", perm))
	}

	return strings.Join(quoted, ", ")
}

// loadTypescriptHandoff reads the state the Resource Generator run left in the
// generation cache: the TypeScript-emission marker and the statically computed
// permission collection.
func loadTypescriptHandoff(resourceSourcePath string) (marker typescriptMarker, data resource.CollectionData, haveMarker, haveData bool, err error) {
	pkgInfo, err := cccpkg.Info()
	if err != nil {
		return typescriptMarker{}, resource.CollectionData{}, false, false, errors.Wrap(err, "pkg.Info()")
	}

	if err := os.Chdir(pkgInfo.AbsolutePath); err != nil {
		return typescriptMarker{}, resource.CollectionData{}, false, false, errors.Wrap(err, "os.Chdir()")
	}

	gCache, err := cache.New(genCacheDir)
	if err != nil {
		return typescriptMarker{}, resource.CollectionData{}, false, false, errors.Wrap(err, "cache.New()")
	}
	defer func() {
		if closeErr := gCache.Close(); closeErr != nil && err == nil {
			err = errors.Wrap(closeErr, "cache.Cache.Close()")
		}
	}()

	hashedPath, err := hashString(filepath.Clean(resourceSourcePath))
	if err != nil {
		return typescriptMarker{}, resource.CollectionData{}, false, false, err
	}
	appCachePath := filepath.Join("app", fmt.Sprintf("%x", hashedPath))

	haveMarker, err = gCache.Load(appCachePath, typescriptMarkerCache, &marker)
	if err != nil {
		return typescriptMarker{}, resource.CollectionData{}, false, false, errors.Wrapf(err, "cache.Cache.Load() for %q", typescriptMarkerCache)
	}

	haveData, err = gCache.Load(appCachePath, collectionDataCache, &data)
	if err != nil {
		return typescriptMarker{}, resource.CollectionData{}, false, false, errors.Wrapf(err, "cache.Cache.Load() for %q", collectionDataCache)
	}

	return marker, data, haveMarker, haveData, nil
}

// parseAndVerifyResources parses the resource and virtual-resource packages, verifies
// the legacy collection parity while struct declaration locations are at hand, and
// returns the parsed resources alongside the resources package, whose named types the
// enum generation consumes.
func (t *typescriptGenerator) parseAndVerifyResources(packageMap map[string]*packages.Package) ([]*resourceInfo, *parser.Package, error) {
	pkg := packageMap[t.resource.Package()]
	if pkg == nil {
		return nil, nil, errors.Newf("no packages found in %q", t.resource.Dir())
	}
	resourcesPkg := parser.ParsePackage(pkg)

	resources, err := t.structsToResources(resourcesPkg.Structs, t.validateStructNameMatchesFile(pkg, true))
	if err != nil {
		return nil, nil, err
	}

	structLocs := make(map[accesstypes.Resource]declLocation)
	t.structLocations(structLocs, pkg.Fset, resourcesPkg.Structs)
	constantLocs := make(map[accesstypes.Resource]declLocation)
	constantLocations(constantLocs, pkg.Fset, resourcesPkg.Constants)

	if t.genVirtualResources {
		pkg := packageMap[t.virtual.Package()]
		virtualStructs := parser.ParsePackage(pkg).Structs
		virtualResources, err := t.structsToVirtualResources(virtualStructs, t.validateStructNameMatchesFile(pkg, true))
		if err != nil {
			return nil, nil, err
		}

		t.structLocations(structLocs, pkg.Fset, virtualStructs)

		resources = append(resources, virtualResources...)
		sortResources(resources)
	}

	if t.parityRuntime != nil {
		if err := t.verifyLegacyCollectionParity(structLocs, constantLocs); err != nil {
			return nil, nil, err
		}
	}

	return resources, resourcesPkg, nil
}

func (t *typescriptGenerator) Generate() error {
	log.Println("Starting TypescriptGenerator Generation")

	begin := time.Now()

	packageMap, err := parser.LoadPackages(t.loadPackages...)
	if err != nil {
		return errors.Wrap(err, "parser.LoadPackages()")
	}

	resources, resourcesPkg, err := t.parseAndVerifyResources(packageMap)
	if err != nil {
		return err
	}

	if t.genComputedResources {
		pkg := packageMap[t.computed.Package()]
		compStructs := parser.ParsePackage(pkg).Structs
		computedResources, err := structsToCompResources(compStructs, t.validateStructNameMatchesFile(pkg, true))
		if err != nil {
			return err
		}

		for _, res := range computedResources {
			res.Fields = t.computedFieldsTypescriptType(res.Fields)
		}

		t.computedResources = computedResources
	}

	t.resources = make([]*resourceInfo, 0, len(resources))
	for _, res := range resources {
		if t.rc.ResourceExists(accesstypes.Resource(t.pluralize(res.Name()))) {
			res.Fields = t.resourceFieldsTypescriptType(res.Fields)
			t.resources = append(t.resources, res)
		}
	}

	if t.genRPCMethods {
		pkg := packageMap[t.rpc.Package()]
		rpcStructs := parser.ParsePackage(pkg).Structs
		t.rpcMethods, err = t.structsToRPCMethods(rpcStructs, t.validateStructNameMatchesFile(pkg, false))
		if err != nil {
			return err
		}

		for _, rpcMethod := range t.rpcMethods {
			rpcMethod.Fields = t.rpcFieldsTypescriptType(rpcMethod.Fields)
		}
	}

	if err := t.runTypescriptMetadataGeneration(); err != nil {
		return err
	}

	if err := t.runTypescriptPermissionGeneration(); err != nil {
		return err
	}

	if err := t.runTypescriptEnumGeneration(resourcesPkg.NamedTypes); err != nil {
		return err
	}

	log.Printf("Finished Typescript generation in %s\n", time.Since(begin))

	return nil
}

func (t *typescriptGenerator) runTypescriptEnumGeneration(namedTypes []*parser.NamedType) error {
	if !t.genEnums {
		return nil
	}

	if !t.genMetadata && !t.genPermission {
		if err := removeGeneratedFiles(t.typescriptDestination, headerComment); err != nil {
			return errors.Wrap(err, "RemoveGeneratedFiles()")
		}
	}

	if err := t.generateEnums(namedTypes); err != nil {
		return errors.Wrap(err, "generateEnums")
	}

	return nil
}

func (t *typescriptGenerator) runTypescriptPermissionGeneration() error {
	if !t.genPermission {
		return nil
	}
	begin := time.Now()
	if !t.genMetadata {
		if err := removeGeneratedFiles(t.typescriptDestination, headerComment); err != nil {
			return errors.Wrap(err, "RemoveGeneratedFiles()")
		}
	}

	log.Println("Starting typescript resource permission generation...")

	routerData := t.rc.TypescriptData()

	piiResourceFields := make(map[accesstypes.Resource]map[accesstypes.Tag]bool, len(t.resources)+len(t.computedResources))
	for _, res := range t.resources {
		for _, field := range res.Fields {
			if field.IsPII() {
				if _, ok := piiResourceFields[accesstypes.Resource(t.pluralize(res.Name()))]; !ok {
					piiResourceFields[accesstypes.Resource(t.pluralize(res.Name()))] = make(map[accesstypes.Tag]bool)
				}
				piiResourceFields[accesstypes.Resource(t.pluralize(res.Name()))][accesstypes.Tag(caser.ToCamel(field.Name()))] = true
			}
		}
	}

	for _, res := range t.computedResources {
		for _, field := range res.Fields {
			if field.IsPII() {
				if _, ok := piiResourceFields[accesstypes.Resource(t.pluralize(res.Name()))]; !ok {
					piiResourceFields[accesstypes.Resource(t.pluralize(res.Name()))] = make(map[accesstypes.Tag]bool)
				}
				piiResourceFields[accesstypes.Resource(t.pluralize(res.Name()))][accesstypes.Tag(caser.ToCamel(field.Name()))] = true
			}
		}
	}

	templateData := tsConstantsData{
		File:       t,
		Data:       routerData,
		RPCMethods: t.rpcMethods,
		PIIMap:     piiResourceFields,
	}

	output, err := t.generateTemplateOutput(typescriptConstantsTemplate, typescriptConstantsTemplate, templateData)
	if err != nil {
		return errors.Wrap(err, "c.generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, generatedTypescriptFileName("constants"))
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated Permissions in %s: %s\n", time.Since(begin), file.Name())

	return nil
}

func (t *typescriptGenerator) runTypescriptMetadataGeneration() error {
	if !t.genMetadata {
		return nil
	}

	if err := removeGeneratedFiles(t.typescriptDestination, headerComment); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	if err := t.generateTypescriptMetadata(); err != nil {
		return errors.Wrap(err, "generateTypescriptResources")
	}

	return nil
}

func (t *typescriptGenerator) generateTypescriptMetadata() error {
	begin := time.Now()
	log.Println("Starting typescript metadata generation...")

	if err := t.generateResourceMetadata(); err != nil {
		return errors.Wrap(err, "generateResourceMetadata()")
	}

	if err := t.generateMethodMetadata(); err != nil {
		return errors.Wrap(err, "generateMethodMetadata()")
	}

	log.Printf("Generated typescript metadata in %s\n", time.Since(begin))

	return nil
}

func (t *typescriptGenerator) generateResourceMetadata() error {
	begin := time.Now()
	log.Println("Starting resource metadata generation...")
	output, err := t.generateTemplateOutput(typescriptResourcesTemplate, typescriptResourcesTemplate, tsResourcesData{
		File:              t,
		Resources:         t.resources,
		ComputedResources: t.computedResources,
		ConsolidatedRoute: t.ConsolidatedRoute,
		GenPrefix:         genPrefix,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, generatedTypescriptFileName("resources"))
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated resource metadata in %s: %s\n", time.Since(begin), file.Name())

	return nil
}

func (t *typescriptGenerator) generateMethodMetadata() error {
	begin := time.Now()
	log.Println("Starting method metadata generation...")

	output, err := t.generateTemplateOutput(typescriptMethodsTemplate, typescriptMethodsTemplate, tsMethodsData{
		File:       t,
		RPCMethods: t.rpcMethods,
		GenPrefix:  genPrefix,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, generatedTypescriptFileName("methods"))
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated methods metadata in %s: %s\n", time.Since(begin), file.Name())

	return nil
}

func (t *typescriptGenerator) generateEnums(namedTypes []*parser.NamedType) error {
	begin := time.Now()
	log.Println("Starting enum generation...")

	enumMap, err := t.retrieveDatabaseEnumValues(namedTypes)
	if err != nil {
		return err
	}

	output, err := t.generateTemplateOutput("typescriptEnumsTemplate", typescriptEnumsTemplate, tsEnumsData{
		Source:     t.resource.Dir(),
		NamedTypes: namedTypes,
		EnumMap:    enumMap,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	file, err := os.Create(filepath.Join(t.typescriptDestination, generatedTypescriptFileName("enums")))
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated enums in %s: %s\n", time.Since(begin), file.Name())

	return nil
}

func (t *typescriptGenerator) resourceFieldsTypescriptType(fields []*resourceField) []*resourceField {
	for _, field := range fields {
		if override, ok := t.typescriptOverrides[field.TypeName()]; ok {
			field.typescriptType = override
		} else {
			field.typescriptType = stringGoType
		}

		if field.IsIterable() {
			field.typescriptType = fmt.Sprintf("%s[]", field.typescriptType)
		}

		if field.IsForeignKey && slices.Contains(t.routerResources, accesstypes.Resource(field.ReferencedResource)) {
			field.IsEnumerated = true
		}
	}

	return fields
}

func (t *typescriptGenerator) computedFieldsTypescriptType(fields []*computedField) []*computedField {
	for _, field := range fields {
		if override, ok := t.typescriptOverrides[field.TypeName()]; ok {
			field.typescriptType = override
		} else {
			field.typescriptType = stringGoType
		}

		if field.IsIterable() {
			field.typescriptType = fmt.Sprintf("%s[]", field.typescriptType)
		}
	}

	return fields
}

func (t *typescriptGenerator) rpcFieldsTypescriptType(fields []*rpcField) []*rpcField {
	for _, field := range fields {
		if override, ok := t.typescriptOverrides[field.TypeName()]; ok {
			if override == booleanStr && field.Type() == "*bool" {
				panic("Bool pointer (*bool) not currently supported for rpc methods.")
			}
			field.typescriptType = override
		} else {
			field.typescriptType = stringGoType
		}

		if field.IsIterable() {
			field.typescriptType = fmt.Sprintf("%s[]", field.typescriptType)
		}
	}

	return fields
}
