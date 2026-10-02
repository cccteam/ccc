// Package generation provides tools for generating resource-driven API boilerplate
// in Go & TypeScript based on Go structures and a Spanner DB schema.
//
// The complete reference for the comment annotations (@resource, @suppress, …) and
// struct tags the generator recognizes lives in the resource module's README.md
// (rendered on pkg.go.dev and GitHub).
package generation

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/cache"
	"github.com/cccteam/ccc/pkg"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/cccteam/ccc/resource/live"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

var caser = strcase.NewCaser(false, nil, nil)

type client struct {
	loadPackages []string
	resource     packageDir
	// types are the application packages WithTypes names: loaded with the run, and
	// written into for the generated method pairs alone (storage.go, jsonmethods.go).
	types             []packageDir
	resources         []*resourceInfo
	computedResources []*computedResource
	rpcMethods        []*rpcMethodInfo
	// imports are the import paths WithImports names beyond what the run derives: the
	// escape hatch, each use of it a generator gap.
	imports []string
	// module is the main module, recorded from the resources package when the run loads
	// it: the output packages' import paths are its path plus their directory.
	module *packages.Module
	// outputs are the packages the run writes generated files into (the handlers and the
	// routes), whose import paths are derived from the module.
	outputs             []packageDir
	rpc                 packageDir
	computed            packageDir
	virtual             packageDir
	migrationSourceURLs []string
	tableMap            map[string]*tableMetadata
	enumValues          map[string][]*enumData
	// enumerateTables maps an enum table's name to the named type whose @enumerate
	// declares it (registerEnumerations). A table named here is an enumeration: its
	// rows are the program's constants, so a struct backing it is read-only and a
	// foreign key into it renders from the generated values, not from a resource.
	enumerateTables map[string]string
	// features are the feature flag declarations the resources package's
	// resource.Feature constants make, by constant (registerFeatures); every
	// @feature resolves against them. featureDeclarations is the same set in constant
	// order, what Features() lists.
	features            map[string]resource.FeatureDeclaration
	featureDeclarations []resource.FeatureDeclaration
	pluralOverrides     map[string]string
	// loadedPackages are the packages the run loaded, by package name, so the
	// @typescript reader finds a type's declaration without loading its package again.
	loadedPackages map[string]*packages.Package
	// leafResolver resolves Go types to TypeScript leaves for every path: the built-in
	// table and the @typescript declarations read through tsDecls. Built on first use.
	leafResolver *leafResolver
	tsDecls      *typescriptDecls
	consolidateConfig
	genRPCMethods          bool
	genComputedResources   bool
	genVirtualResources    bool
	spannerEmulatorVersion string
	FileWriter
	// output records the directories the run writes generated files into and the files
	// it wrote, for the atomic writes and the stale sweep at the end of the run.
	output   generatedOutput
	genCache *cache.Cache
}

func newClient(ctx context.Context, resourcePackageDir string, migrationSourceURL []string, opts []option) (*client, error) {
	pkgInfo, err := pkg.Info()
	if err != nil {
		return nil, errors.Wrap(err, "pkg.Info()")
	}

	if err := os.Chdir(pkgInfo.AbsolutePath); err != nil {
		return nil, errors.Wrap(err, "os.Chdir()")
	}

	gCache, err := cache.New(genCacheDir)
	if err != nil {
		return nil, errors.Wrap(err, "cache.New()")
	}

	c := &client{
		migrationSourceURLs: migrationSourceURL,
		genCache:            gCache,
	}
	if err := resolveOptions(c, opts); err != nil {
		return nil, err
	}

	c.loadPackages = append(c.loadPackages, resourcePackageDir)
	c.resource = packageDir(resourcePackageDir)
	c.migrationSourceURLs = migrationSourceURL

	isSchemaClean, err := c.isSchemaClean()
	if err != nil {
		return nil, err
	}

	switch {
	case isSchemaClean:
		if loaded, err := c.loadAllCachedData(); err != nil {
			return nil, err
		} else if loaded {
			break
		}

		fallthrough
	default:
		for _, migrationSource := range migrationSourceURL {
			hashedMigrationSourceURL, err := hashString(migrationSource)
			if err != nil {
				return nil, err
			}
			migrationCachePath := filepath.Join("migrations", fmt.Sprintf("%x", hashedMigrationSourceURL))

			if err := c.genCache.DeleteSubpath(migrationCachePath); err != nil {
				return nil, errors.Wrap(err, "cache.Cache.DeleteSubpath()")
			}
		}

		if err := c.runSpanner(ctx, c.spannerEmulatorVersion, migrationSourceURL); err != nil {
			return nil, err
		}
	}

	return c, nil
}

func (c *client) Close() error {
	if err := c.genCache.Close(); err != nil {
		return errors.Wrap(err, "cache.Cache.Close()")
	}

	return nil
}

func (c *client) HasNullBoolean() bool {
	for _, res := range c.resources {
		if res.HasNullBool() {
			return true
		}
	}

	return false
}

// notePackages records the packages a run loaded, for the @typescript reader and the
// import resolution, and the main module from the resources package among them.
func (c *client) notePackages(packageMap map[string]*packages.Package) {
	c.loadedPackages = packageMap
	c.tsDecls = nil
	c.leafResolver = nil
	if resources := packageMap[c.resource.Package()]; resources != nil && resources.Module != nil {
		c.module = resources.Module
	}
}

// leaves is the run's leaf resolver: the built-in table and the @typescript
// declarations, read from the loaded packages and, for a type declared elsewhere, from
// its package on first sight.
func (c *client) leaves() *leafResolver {
	if c.leafResolver == nil {
		if c.tsDecls == nil {
			c.tsDecls = newTypescriptDecls(c.loadedPackages)
		}
		c.leafResolver = newLeafResolver(c.tsDecls.declFor, c.tsDecls.rhsFor)
	}

	return c.leafResolver
}

// ResourceTypeImports lists the imports the resources file needs: the @typescript
// declarations behind every table, view, and computed field on this outlet, grouped
// per module.
func (c *client) ResourceTypeImports() []tsImportGroup {
	var imports []*tsImport
	for _, res := range c.resources {
		for _, field := range res.Fields {
			imports = append(imports, field.tsImport)
		}
		for _, shape := range res.ColumnShapes {
			imports = append(imports, shape.TypescriptImports()...)
		}
	}
	for _, res := range c.computedResources {
		imports = append(imports, res.Shape.TypescriptImports()...)
	}

	return groupImports(imports)
}

// MethodTypeImports lists the imports the methods file needs: the @typescript
// declarations behind every request and result field on this outlet, grouped per
// module.
func (c *client) MethodTypeImports() []tsImportGroup {
	var imports []*tsImport
	for _, method := range c.rpcMethods {
		imports = append(imports, method.Request.TypescriptImports()...)
		imports = append(imports, method.Result.TypescriptImports()...)
	}

	return groupImports(imports)
}

func (c *client) hasRPCMethodWithEnumeratedResource() bool {
	for _, rpcMethod := range c.rpcMethods {
		if rpcMethod.hasEnumeratedResource() {
			return true
		}
	}

	return false
}

func (c *client) hasRPCMethods() bool {
	return len(c.rpcMethods) > 0
}

func (c *client) hasRPCMethodWithTransition() bool {
	for _, rpcMethod := range c.rpcMethods {
		if rpcMethod.Transition != nil {
			return true
		}
	}

	return false
}

// localPackageImports renders the import block the templates share, one quoted path per
// line: every import path the run derives (importPaths). The import fixer then keeps the
// ones each file references.
func (c *client) localPackageImports() string {
	paths := c.importPaths()
	if len(paths) == 0 {
		return ""
	}

	return `"` + strings.Join(paths, "\"\n\t\"") + `"`
}

// importPaths are the import paths generated code may reference, sorted: every package
// the run loaded (its path from the type checker: the resources package and the RPC,
// virtual, computed and WithTypes packages), the output packages (the module path plus
// their directory), and the paths WithImports names. Standard-library paths WithImports
// names are left out: goimports resolves them natively into the standard-library group,
// and rendering them here would put them in the local group, where format-on-save
// reorders them (generated output must be a fixed point of format-on-save).
func (c *client) importPaths() []string {
	set := map[string]bool{}
	for _, pkg := range c.loadedPackages {
		if pkg != nil && pkg.PkgPath != "" {
			set[pkg.PkgPath] = true
		}
	}
	for _, dir := range c.outputs {
		if p, ok := c.outputPath(dir); ok {
			set[p] = true
		}
	}
	for _, p := range c.imports {
		if isStandardLibrary(p) {
			continue
		}
		set[p] = true
	}
	if len(set) == 0 {
		return nil
	}
	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	return paths
}

// outputPath is an output package's import path: the module path plus the package
// directory, known once the run has loaded the resources package. The run works from
// the module root (newClient changes into it), so a relative directory is relative to
// the module; an absolute one is taken relative to the module directory. A directory
// outside the module has no path here.
func (c *client) outputPath(dir packageDir) (string, bool) {
	if c.module == nil || c.module.Path == "" || dir == "" {
		return "", false
	}
	rel := filepath.Clean(dir.Dir())
	if filepath.IsAbs(rel) {
		var err error
		if rel, err = filepath.Rel(c.module.Dir, rel); err != nil {
			return "", false
		}
	}
	if rel == "." {
		return c.module.Path, true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}

	return c.module.Path + "/" + filepath.ToSlash(rel), true
}

// isStandardLibrary reports whether an import path is the standard library's: its first
// element carries no dot.
func isStandardLibrary(importPath string) bool {
	root, _, _ := strings.Cut(importPath, "/")

	return !strings.Contains(root, ".")
}

// fixerImports seeds the import fixer: every loaded package under its type-checked name
// (authoritative), and every output package under its directory's base name, which is
// its package name by construction (the templates write package {{ .Package }} from it).
func (c *client) fixerImports() []fixerImport {
	var imports []fixerImport
	for name, pkg := range c.loadedPackages {
		if pkg != nil && pkg.PkgPath != "" {
			pkgName := pkg.Name
			if pkgName == "" {
				pkgName = name
			}
			imports = append(imports, fixerImport{name: pkgName, path: pkg.PkgPath})
		}
	}
	for _, dir := range c.outputs {
		if p, ok := c.outputPath(dir); ok {
			imports = append(imports, fixerImport{name: dir.Package(), path: p})
		}
	}
	sort.Slice(imports, func(i, j int) bool {
		return imports[i].path < imports[j].path
	})

	return imports
}

func (t *tableMetadata) addSchemaResult(result *informationSchemaResult) {
	column, ok := t.Columns[result.ColumnName]
	if !ok {
		column = columnMeta{
			OrdinalPosition:    result.OrdinalPosition - 1, // SQL is 1-indexed. For consistency with JavaScript & Go we translate to 0-indexed
			KeyOrdinalPosition: result.KeyOrdinalPosition - 1,
		}
	}

	if result.IsPrimaryKey {
		t.PkCount++
		column.IsPrimaryKey = true
	}

	if result.IsForeignKey {
		column.IsForeignKey = true

		if result.ReferencedTable != nil {
			column.ReferencedTable = *result.ReferencedTable
		}

		if result.ReferencedColumn != nil {
			column.ReferencedColumn = *result.ReferencedColumn
		}

		column.DeleteRule = valueOrEmpty(result.DeleteRule)
	}

	column.IsNullable = result.IsNullable
	column.HasDefault = result.HasDefault
	column.SpannerType = result.SpannerType

	t.Columns[result.ColumnName] = column
}

// addIndexResult folds one row of the index query into the table's index list: rows
// arrive grouped by index, key columns in ordinal order, and a row with no ordinal
// position is a stored column.
func (t *tableMetadata) addIndexResult(result *indexSchemaResult) {
	if len(t.Indexes) == 0 || t.Indexes[len(t.Indexes)-1].Name != result.IndexName {
		t.Indexes = append(t.Indexes, indexMeta{
			Name:         result.IndexName,
			PrimaryKey:   result.IndexType == primaryKeyIndexType,
			Unique:       result.IsUnique,
			NullFiltered: result.IsNullFiltered,
			Managed:      result.IsManaged,
		})
	}

	index := &t.Indexes[len(t.Indexes)-1]
	if result.OrdinalPosition == nil {
		index.Storing = append(index.Storing, result.ColumnName)

		return
	}

	index.Key = append(index.Key, indexColumn{
		Column:     result.ColumnName,
		Descending: result.ColumnOrdering != nil && *result.ColumnOrdering == descendingOrdering,
	})
}

// deriveIndexFlags sets the per-column index flags from the index composition. A
// column that leads some index, the PRIMARY_KEY index and Spanner's managed
// foreign-key indexes included, is indexed: a filter on it alone has a seek path, a
// read of the index from a known key prefix. A trailing key column and a stored column
// are not: a predicate on either alone scans the index, so the tag they would carry
// would promise a path the schema does not give. Whether a trailing column has one
// with the tenant bound before it is a resource-level fact (deriveTenantIndexFlags).
// A column that is the whole key of a unique index, the primary key included,
// identifies a row on its own. Null filtering does not disqualify: such an index still
// enforces one row per non-null value, and the subject subquery compares by equality,
// which never matches NULL. The index list is the fact the schema read records and the
// cache keeps; the flags are derived from it on every read and every cache load, never
// trusted from storage, so a cache an earlier generator wrote cannot carry a flag this
// one no longer derives.
func (t *tableMetadata) deriveIndexFlags() {
	for name, column := range t.Columns {
		column.IsIndex, column.IsUniqueIndex = false, false
		t.Columns[name] = column
	}

	for _, index := range t.Indexes {
		if len(index.Key) == 0 {
			continue
		}
		leading := index.Key[0].Column
		column, ok := t.Columns[leading]
		if !ok {
			continue
		}
		column.IsIndex = true
		if index.Unique && len(index.Key) == 1 {
			column.IsUniqueIndex = true
		}
		t.Columns[leading] = column
	}
}

func (c *client) tableMetadataFor(resourceName string) (*tableMetadata, error) {
	table, ok := c.tableMap[c.pluralize(resourceName)]
	if !ok {
		return nil, errors.Newf("table %q not found in database", c.pluralize(resourceName))
	}

	return table, nil
}

func (c *client) templateFuncs() map[string]any {
	templateFuncs := map[string]any{
		"Pluralize": c.pluralize,
		"GoCamel":   strcase.ToGoCamel,
		"GoCamelConcat": func(parts ...string) string {
			return strcase.ToGoCamel(strings.Join(parts, ""))
		},
		"Camel":                        strcase.ToCamel,
		"Pascal":                       strcase.ToPascal,
		"Kebab":                        strcase.ToKebab,
		"DisplayType":                  renderDisplayType,
		"Add":                          func(a, b int) int { return a + b },
		"EnumerationLiteral":           enumerationLiteral,
		"FormatResourceInterfaceTypes": c.formatResourceInterfaceTypes,
		"FormatRPCInterfaceTypes":      formatRPCInterfaceTypes,
		"PrivateType": func(s string) string {
			r, runeWidth := utf8.DecodeRuneInString(s)
			lowerFirst := strings.ToLower(string(r))

			return lowerFirst + s[runeWidth:]
		},
		"SanitizeIdentifier":      sanitizeEnumIdentifier,
		"TypescriptMethodImports": typescriptMethodImports,
		"TypescriptNamespace":     typescriptNamespace,
		"TypescriptNamespaceOf":   typescriptNamespaceOf,
		"TypescriptConstImports":  typescriptConsImports,
		"PermissionConstant":      permissionConstant,
		"ScopeConstant":           scopeConstant,
		"MaskingConstant":         maskingConstant,
		"BindingHops":             bindingHopsLiteral,
		// The feature flag routes under an outlet's prefix and the read route's
		// parameter, from the resource package's constants, so the routes, the router
		// tests and the TypeScript descriptor spell them once.
		"FeaturesRoute": func() string {
			return resource.FeaturesRoute
		},
		"FeatureFlagsRoute": func() string {
			return resource.FeatureFlagsRoute
		},
		"SetFeatureRoute": func() string {
			return resource.SetFeatureRoute
		},
		"FeatureFlagNameParam": func() string {
			return string(resource.FeatureFlagNameParam)
		},
		// RouteHandler pairs a route with the file's resource package for the routes
		// template's handler expression, which names the gate's constant.
		"RouteHandler": func(resourcePackage string, route *generatedRoute) routeHandlerData {
			return routeHandlerData{Route: route, ResourcePackage: resourcePackage}
		},
		// The live routes under an outlet's prefix, from the live package's constants,
		// so the routes, the router tests and the TypeScript descriptor spell them once.
		"LiveRenewRoute": func() string {
			return live.RenewRoute
		},
		"LiveUnsubscribeRoute": func() string {
			return live.UnsubscribeRoute
		},
		"LiveTokenRoute": func() string {
			return live.TokenRoute
		},
	}

	return templateFuncs
}

// bindingHopsLiteral renders a binding path as its Path field literal, or
// nothing for a column binding — shared by every binding kind the collection
// template emits.
func bindingHopsLiteral(path []resource.BindingHop) string {
	if len(path) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(", Path: []resource.BindingHop{")
	for i, hop := range path {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "{Table: %q, JoinColumn: %q, Column: %q}", hop.Table, hop.JoinColumn, hop.Column)
	}
	b.WriteString("}")

	return b.String()
}

// permissionConstant renders a permission as its accesstypes constant when one exists,
// falling back to a typed conversion for permissions outside the standard set.
func permissionConstant(p accesstypes.Permission) string {
	switch p {
	case accesstypes.Create:
		return "accesstypes.Create"
	case accesstypes.Read:
		return "accesstypes.Read"
	case accesstypes.List:
		return "accesstypes.List"
	case accesstypes.Update:
		return "accesstypes.Update"
	case accesstypes.Delete:
		return "accesstypes.Delete"
	case accesstypes.Execute:
		return "accesstypes.Execute"
	case accesstypes.NullPermission:
		return "accesstypes.NullPermission"
	default:
		return fmt.Sprintf("accesstypes.Permission(%q)", string(p))
	}
}

// scopeConstant renders a permission scope as its accesstypes constant when one exists.
// maskingConstant renders a masking behavior as the resource package constant
// that names it.
func maskingConstant(m resource.Masking) string {
	switch m {
	case resource.MaskingPositional:
		return "resource.MaskingPositional"
	case resource.MaskingConcealing:
		return "resource.MaskingConcealing"
	default:
		return fmt.Sprintf("resource.Masking(%q)", string(m))
	}
}

func scopeConstant(s accesstypes.PermissionScope) string {
	switch s {
	case accesstypes.GlobalPermissionScope:
		return "accesstypes.GlobalPermissionScope"
	case accesstypes.DomainPermissionScope:
		return "accesstypes.DomainPermissionScope"
	default:
		return fmt.Sprintf("accesstypes.PermissionScope(%q)", string(s))
	}
}

func (c *client) generateTemplateOutput(templateName, fileTemplate string, data any) ([]byte, error) {
	tmpl, err := template.New(templateName).Funcs(c.templateFuncs()).Parse(fileTemplate)
	if err != nil {
		return nil, errors.Wrap(err, "template.Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, data); err != nil {
		return nil, errors.Wrap(err, "tmpl.Execute()")
	}

	return buf.Bytes(), nil
}

// writeFormattedGoFile renders a Go file template and formats the result fully in memory,
// only writing destinationPath once the content is known good, so a template or format
// error cannot leave behind an empty or partial generated file.
func (c *client) writeFormattedGoFile(destinationPath, templateName, fileTemplate string, data any) error {
	output, err := c.generateTemplateOutput(templateName, fileTemplate, data)
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	formattedOutput, err := c.formatGoBytes(destinationPath, templateName, output, data)
	if err != nil {
		return err
	}

	if err := c.output.writeGeneratedFile(destinationPath, formattedOutput); err != nil {
		return errors.Wrap(err, "generatedOutput.writeGeneratedFile()")
	}

	return nil
}

// formatGoBytes formats rendered Go source. The file's import block is resolved
// locally, never through goimports' import resolution (which shells out to the go
// command and can scan the module cache, costing upwards of a second per file): the
// fixer knows the derived packages (loaded and written), the WithImports paths and the
// type imports the template payload declares when it implements typeImporter, which
// scopes resolution to exactly the parsed types the file renders — two resources may
// use same-named packages from different paths without affecting each other's files.
// A referenced qualifier none of those resolve is a generation error naming the file,
// the template and the qualifier: the gap is reproduced in a resource/generation test
// and closed, and WithImports names the path until it is.
func (c *client) formatGoBytes(destinationPath, templateName string, output []byte, data any) ([]byte, error) {
	var typeImports []fixerImport
	if importer, ok := data.(typeImporter); ok {
		typeImports = importer.typeImports()
	}

	fixer := newImportFixer(append(c.fixerImports(), typeImports...), c.imports)
	fixed, unknown, err := fixer.fix(destinationPath, output)
	if err != nil {
		return nil, errors.Wrapf(err, "import resolution for %s (template %s)", destinationPath, templateName)
	}
	if len(unknown) > 0 {
		return nil, errors.Newf("import resolution for %s (template %s) cannot resolve qualifier(s) %v: the generator derives every import path from the packages it loads and writes; a path beyond those is a generator gap to file, and WithImports names it until it is fixed", destinationPath, templateName, unknown)
	}

	return c.formatBytes(destinationPath, fixed)
}

// retrieveDatabaseEnumValues resolves every @enumerate named type against the schema's
// enum values, returning the values keyed by type name alongside each type's table
// name (the TypeScript generator's outlet filter matches tables to resources).
func (c *client) retrieveDatabaseEnumValues(namedTypes []*parser.NamedType) (values map[string][]*enumData, tables map[string]string, err error) {
	enumMap := make(map[string][]*enumData)
	enumTables := make(map[string]string)
	for _, namedType := range namedTypes {
		scanner := genlang.NewScanner(resourceKeywords())
		annotations, err := scanner.ScanNamedType(namedType)
		if err != nil {
			return nil, nil, errors.Wrap(err, "scanner.ScanNamedType()")
		}

		var tableName string
		if annotations.Named.Has(enumerateKeyword) {
			tableName = string(annotations.Named.Get(enumerateKeyword))
		} else {
			continue
		}

		if t := namedType.TypeName(); t != stringGoType {
			return nil, nil, errors.Newf("cannot enumerate type %q, underlying type must be %q, found %q", namedType.Name(), stringGoType, t)
		}

		data, ok := c.enumValues[tableName]
		if !ok {
			return nil, nil, errors.Newf("cannot enumerate type %q, tableName %q has no values or does not exist", namedType.Name(), tableName)
		}

		enumMap[namedType.Name()] = data
		enumTables[namedType.Name()] = tableName
	}

	return enumMap, enumTables, nil
}

// registerEnumerations records which tables the package's @enumerate types name, so
// resource extraction and TypeScript metadata can treat a foreign key into one, or a
// struct backing one, as an enumeration. It reads the same declarations enum
// generation does and must run before structsToResources.
func (c *client) registerEnumerations(namedTypes []*parser.NamedType) error {
	_, tables, err := c.retrieveDatabaseEnumValues(namedTypes)
	if err != nil {
		return errors.Wrap(err, "retrieveDatabaseEnumValues()")
	}
	c.enumerateTables = make(map[string]string, len(tables))
	for typeName, tableName := range tables {
		c.enumerateTables[tableName] = typeName
	}

	return nil
}

// enumerationOf returns the @enumerate type behind a table, and whether there is one.
func (c *client) enumerationOf(tableName string) (string, bool) {
	typeName, ok := c.enumerateTables[tableName]

	return typeName, ok
}

// pluralize returns the plural form of value: an explicit override if one is
// configured (see WithPluralOverrides), otherwise standard English suffix rules —
// consonant+y becomes "ies", vowel+z doubles the z ("Quizzes"), a sibilant ending
// (s, x, z, ch, sh) takes "es", and everything else takes "s". Names these rules
// get wrong belong in the project's WithPluralOverrides. It is read-only and safe
// to call from concurrent generation phases.
func (c *client) pluralize(value string) string {
	if plural, ok := c.pluralOverrides[value]; ok {
		return plural
	}

	toLower := strings.ToLower(value)
	switch {
	case strings.HasSuffix(toLower, "y") && len(toLower) > 1 && !isVowel(toLower[len(toLower)-2]):
		return value[:len(value)-1] + "ies"
	case strings.HasSuffix(toLower, "z") && len(toLower) > 1 && isVowel(toLower[len(toLower)-2]):
		return value + "zes"
	case strings.HasSuffix(toLower, "s"), strings.HasSuffix(toLower, "x"), strings.HasSuffix(toLower, "z"),
		strings.HasSuffix(toLower, "ch"), strings.HasSuffix(toLower, "sh"):
		return value + "es"
	default:
		return value + "s"
	}
}

func isVowel(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	default:
		return false
	}
}

// removeGeneratedFiles removes the previous run's stale output from directory: the
// generated files there, by method, that this run did not write (written holds the
// paths it did). It runs after the run has written everything, so a package compiling
// against the tree during the run never misses a file. A directory that does not exist
// holds nothing to remove.
func removeGeneratedFiles(directory string, method generatedFileDeleteMethod, written map[string]struct{}) error {
	log.Printf("removing stale generated files in directory %q...", directory)
	dir, err := os.Open(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.Wrap(err, "os.Open()")
	}
	defer dir.Close()

	files, err := dir.Readdirnames(0)
	if err != nil {
		return errors.Wrap(err, "dir.Readdirnames()")
	}

	if err := dir.Close(); err != nil {
		return errors.Wrap(err, "dir.Close()")
	}

	for _, f := range files {
		if !strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, ".ts") && !strings.HasSuffix(f, ".dot") {
			continue
		}
		if _, ok := written[filepath.Join(directory, f)]; ok {
			continue
		}

		switch method {
		case prefix:
			if err := removeGeneratedFileByPrefix(directory, f); err != nil {
				return errors.Wrap(err, "removeGeneratedFileByPrefix()")
			}
		case headerComment:
			if err := removeGeneratedFileByHeaderComment(directory, f); err != nil {
				return errors.Wrap(err, "removeGeneratedFileByHeaderComment()")
			}
		case methodFiles:
			if err := removeGeneratedFileByName(directory, f, methodFileNames); err != nil {
				return errors.Wrap(err, "removeGeneratedFileByName()")
			}
		}
	}

	return nil
}

// removeGeneratedFileByName removes the file when its name is one of names.
func removeGeneratedFileByName(directory, file string, names []string) error {
	if slices.Contains(names, file) {
		if err := os.Remove(filepath.Join(directory, file)); err != nil {
			return errors.Wrap(err, "os.Remove()")
		}
	}

	return nil
}

func removeGeneratedFileByPrefix(directory, file string) error {
	if strings.HasPrefix(file, genPrefix) {
		fp := filepath.Join(directory, file)
		if err := os.Remove(fp); err != nil {
			return errors.Wrap(err, "os.Remove()")
		}
	}

	return nil
}

func removeGeneratedFileByHeaderComment(directory, file string) error {
	fp := filepath.Join(directory, file)
	content, err := os.ReadFile(fp)
	if err != nil {
		return errors.Wrap(err, "os.ReadFile()")
	}

	if bytes.HasPrefix(content, []byte(generationHeader)) {
		if err := os.Remove(fp); err != nil {
			return errors.Wrap(err, "os.Remove()")
		}
	}

	return nil
}

func formatInterfaceTypes(types []string) string {
	var rows []string
	var rowLen int
	for i, t := range types {
		rowLen += len(t)
		if i == 0 || rowLen > 80 {
			rowLen = len(t)
			rows = append(rows, t)
		} else {
			rows[len(rows)-1] += " | " + t
		}
	}

	if len(rows) == 0 {
		return ""
	}

	return "\t" + strings.Join(rows, " |\n\t")
}

func (c *client) formatResourceInterfaceTypes(resources []*resourceInfo, computedResources []*computedResource) string {
	names := make([]string, 0, len(resources)+len(computedResources))
	for _, res := range resources {
		if res.IsVirtual {
			names = append(names, fmt.Sprintf("%s.%s", c.virtual.Package(), res.Name()))
		} else {
			names = append(names, fmt.Sprintf("%s.%s", c.resource.Package(), res.Name()))
		}
	}

	for _, res := range computedResources {
		names = append(names, fmt.Sprintf("%s.%s", c.computed.Package(), res.Name()))
	}

	return formatInterfaceTypes(names)
}

func formatRPCInterfaceTypes(rpcMethods []*rpcMethodInfo) string {
	names := make([]string, 0, len(rpcMethods))
	for _, rpcMethod := range rpcMethods {
		names = append(names, rpcMethod.Name())
	}

	return formatInterfaceTypes(names)
}

// Returns slice of applicable handler types for a given resource.
// Every resource starts with a List handler.
// Views do not have Read handlers.
// Consolidated resources do not have Patch handlers.
// Ignored handler types are filtered out.
// resourceEndpoints is the handler set a resource generates before suppression: a
// list for every resource; a keyed read for a table and for a view that declares
// its @primarykey (a view with no key has no read identity); a patch for a table
// outside the consolidated handler.
func resourceEndpoints(res *resourceInfo) []HandlerType {
	handlerTypes := []HandlerType{ListHandler}

	if res.HasPrimaryKey() {
		handlerTypes = append(handlerTypes, ReadHandler)
	}
	if !res.IsVirtual && !res.IsConsolidated {
		handlerTypes = append(handlerTypes, PatchHandler)
	}

	handlerTypes = slices.DeleteFunc(handlerTypes, func(ht HandlerType) bool {
		return slices.Contains(res.SuppressedHandlers, ht)
	})

	return handlerTypes
}

func hasConsolidatedHandler(res *resourceInfo) bool {
	if res.IsConsolidated {
		return !slices.Contains(res.SuppressedHandlers, PatchHandler)
	}

	return false
}

func sanitizeEnumIdentifier(name string) string {
	var result []byte
	for _, b := range []byte(name) {
		switch {
		case startStandaloneNumber(result, b):
			result = append(result, 'N', b)
		case alphaFollowingNumber(result, b):
			result = append(result, '_', b)
		case isAlphaNumeric(b):
			result = append(result, b)
		case b == '`' || b == '\'':
		default:
			result = append(result, '_')
		}
	}

	return caser.ToPascal(string(result))
}

func typescriptMethodImports(t *typescriptGenerator) string {
	pkgs := make([]string, 0, 2)
	if t.hasRPCMethods() || t.servesFeatureFlags() {
		pkgs = append(pkgs, "Methods")
	}
	if t.hasRPCMethodWithEnumeratedResource() || t.hasRPCMethodWithTransition() {
		pkgs = append(pkgs, "Resources")
	}

	return strings.Join(pkgs, ", ")
}

func typescriptConsImports(t *typescriptGenerator, d *resource.TypescriptData) string {
	pkgs := make([]string, 0, 2)
	if len(d.ResourceTags) > 0 || len(t.rpcMethods) > 0 {
		pkgs = append(pkgs, "FieldName")
	}
	if len(t.rpcMethods) > 0 || t.servesFeatureFlags() {
		pkgs = append(pkgs, "Method")
	}
	if len(d.Permissions) > 0 {
		pkgs = append(pkgs, "Permission")
	}
	if len(d.Resources) > 0 {
		pkgs = append(pkgs, "Resource")
	}

	return strings.Join(pkgs, ", ")
}

func startStandaloneNumber(result []byte, b byte) bool {
	if len(result) == 0 && ('0' <= b && b <= '9') {
		return true
	}

	if len(result) < 2 {
		return false
	}

	return bytes.HasSuffix(result, []byte("_")) && ('0' <= b && b <= '9') && ('0' <= result[len(result)-2] && result[len(result)-2] <= '9')
}

func alphaFollowingNumber(result []byte, b byte) bool {
	if len(result) == 0 {
		return false
	}

	prev := result[len(result)-1]

	return ('0' <= prev && prev <= '9') && (('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z'))
}

func isAlphaNumeric(b byte) bool {
	return ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z') || ('0' <= b && b <= '9')
}
