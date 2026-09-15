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
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/cache"
	"github.com/cccteam/ccc/pkg"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

var caser = strcase.NewCaser(false, nil, nil)

type client struct {
	loadPackages        []string
	resource            packageDir
	resources           []*resourceInfo
	computedResources   []*computedResource
	rpcMethods          []*rpcMethodInfo
	localPackages       []string
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
	pluralOverrides map[string]string
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
	genCache *cache.Cache
}

func newClient(ctx context.Context, resourcePackageDir string, migrationSourceURL, localPackages []string, opts []option) (*client, error) {
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
	c.localPackages = localPackages
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

// notePackages records the packages a run loaded, for the @typescript reader.
func (c *client) notePackages(packageMap map[string]*packages.Package) {
	c.loadedPackages = packageMap
	c.tsDecls = nil
	c.leafResolver = nil
}

// leaves is the run's leaf resolver: the built-in table and the @typescript
// declarations, read from the loaded packages and, for a type declared elsewhere, from
// its package on first sight.
func (c *client) leaves() *leafResolver {
	if c.leafResolver == nil {
		if c.tsDecls == nil {
			c.tsDecls = newTypescriptDecls(c.loadedPackages)
		}
		c.leafResolver = newLeafResolver(c.tsDecls.declFor)
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

func (c *client) localPackageImports() string {
	// Standard-library packages are skipped: goimports resolves them natively into the
	// stdlib import group, whereas rendering them here puts them in the local-package
	// group, where editor format-on-save reorders them (generated output must be a
	// fixed point of format-on-save).
	pkgs := make([]string, 0, len(c.localPackages))
	for _, pkg := range c.localPackages {
		if root, _, _ := strings.Cut(pkg, "/"); !strings.Contains(root, ".") {
			continue
		}
		pkgs = append(pkgs, pkg)
	}

	if len(pkgs) == 0 {
		return ""
	}

	return `"` + strings.Join(pkgs, "\"\n\t\"") + `"`
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

	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o750); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}
	if err := os.WriteFile(destinationPath, formattedOutput, 0o644); err != nil {
		return errors.Wrapf(err, "os.WriteFile(): file: %s", destinationPath)
	}

	return nil
}

// formatGoBytes formats rendered Go source. The fast path resolves the file's import
// block locally, skipping goimports' import resolution (which shells out to the go
// command and can scan the module cache, costing upwards of a second per file). Type
// imports come from the template payload when it implements typeImporter, scoping
// resolution to exactly the parsed types the file renders — two resources may use
// same-named packages from different paths without affecting each other's files. When
// a referenced qualifier cannot be resolved locally it falls back to goimports so
// output stays correct, and logs a warning naming the template and qualifiers so the
// gap can be reproduced in a resource/generation test and closed.
func (c *client) formatGoBytes(destinationPath, templateName string, output []byte, data any) ([]byte, error) {
	var typeImports []fixerImport
	if importer, ok := data.(typeImporter); ok {
		typeImports = importer.typeImports()
	}

	fixer := newImportFixer(typeImports, c.localPackages)
	fixed, unknown, err := fixer.fix(destinationPath, output)
	switch {
	case err != nil:
		log.Printf("WARNING: local import resolution failed for %s (template %s): %v; falling back to goimports resolution (slow)", destinationPath, templateName, err)
	case len(unknown) > 0:
		log.Printf("WARNING: local import resolution for %s (template %s) could not resolve qualifier(s) %v; falling back to goimports resolution (slow). Add a scenario covering this to the resource/generation tests, then cover the qualifier via the template's import block or the payload's typeImports.", destinationPath, templateName, unknown)
	default:
		return c.formatBytes(destinationPath, fixed)
	}

	return c.GoFormatBytes(destinationPath, output)
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

// removeGeneratedFiles sweeps the previous run's output from directory. A directory
// that does not exist yet holds nothing to remove: the first generate into a fresh
// target creates it when the first file is written.
func removeGeneratedFiles(directory string, method generatedFileDeleteMethod) error {
	log.Printf("removing generated files in directory %q...", directory)
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

		switch method {
		case prefix:
			if err := removeGeneratedFileByPrefix(directory, f); err != nil {
				return errors.Wrap(err, "removeGeneratedFileByPrefix()")
			}
		case headerComment:
			if err := removeGeneratedFileByHeaderComment(directory, f); err != nil {
				return errors.Wrap(err, "removeGeneratedFileByHeaderComment()")
			}
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
func resourceEndpoints(res *resourceInfo) []HandlerType {
	handlerTypes := []HandlerType{ListHandler}

	if !res.IsVirtual {
		handlerTypes = append(handlerTypes, ReadHandler)

		if !res.IsConsolidated {
			handlerTypes = append(handlerTypes, PatchHandler)
		}
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
	if t.hasRPCMethods() {
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
	if len(t.rpcMethods) > 0 {
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
