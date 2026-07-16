// Package generation provides tools for generating resource-driven API boilerplate
// in Go & TypeScript based on Go structures and a Spanner DB schema.
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

	"github.com/cccteam/ccc/cache"
	"github.com/cccteam/ccc/pkg"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

const customTypesPrefix = "CustomTypes."

type generatorType string

const (
	resourceGeneratorType   generatorType = "resource"
	typeScriptGeneratorType generatorType = "typescript"
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
	pluralOverrides     map[string]string
	consolidateConfig
	genRPCMethods          bool
	genComputedResources   bool
	genVirtualResources    bool
	spannerEmulatorVersion string
	FileWriter
	genCache *cache.Cache
}

func newClient(ctx context.Context, genType generatorType, resourcePackageDir string, migrationSourceURL, localPackages []string, opts []option) (*client, error) {
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
		if loaded, err := c.loadAllCachedData(genType); err != nil {
			return nil, err
		} else if loaded {
			break
		}

		fallthrough
	default:
		if genType == typeScriptGeneratorType {
			return nil, errors.New("schema cache is out of date, please run Resource Generator first")
		}

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

// HasCustomTypesInResources checks if CustomTypes are used in any resource (including computed resources)
func (c *client) HasCustomTypesInResources() bool {
	for _, resource := range c.resources {
		for _, field := range resource.Fields {
			if strings.HasPrefix(field.typescriptType, customTypesPrefix) {
				return true
			}
		}
	}

	for _, resource := range c.computedResources {
		for _, field := range resource.Fields {
			if strings.HasPrefix(field.typescriptType, customTypesPrefix) {
				return true
			}
		}
	}

	return false
}

// HasCustomTypesInMethods checks if CustomTypes are used in any RPC method
func (c *client) HasCustomTypesInMethods() bool {
	for _, method := range c.rpcMethods {
		for _, field := range method.Fields {
			if strings.HasPrefix(field.typescriptType, customTypesPrefix) {
				return true
			}
		}
	}

	return false
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
	column.IsIndex = result.IsIndex
	column.IsUniqueIndex = result.IsUniqueIndex
	column.HasDefault = result.HasDefault

	t.Columns[result.ColumnName] = column
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
		"Lower":                        strings.ToLower,
		"Add":                          func(a, b int) int { return a + b },
		"FormatResourceInterfaceTypes": c.formatResourceInterfaceTypes,
		"FormatRPCInterfaceTypes":      formatRPCInterfaceTypes,
		"PrivateType": func(s string) string {
			r, runeWidth := utf8.DecodeRuneInString(s)
			lowerFirst := strings.ToLower(string(r))

			return lowerFirst + s[runeWidth:]
		},
		"SanitizeIdentifier":      sanitizeEnumIdentifier,
		"TypescriptMethodImports": typescriptMethodImports,
		"TypescriptConstImports":  typescriptConsImports,
	}

	return templateFuncs
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

	formattedOutput, err := c.GoFormatBytes(destinationPath, output)
	if err != nil {
		return err
	}

	if err := os.WriteFile(destinationPath, formattedOutput, 0o644); err != nil {
		return errors.Wrapf(err, "os.WriteFile(): file: %s", destinationPath)
	}

	return nil
}

func (c *client) retrieveDatabaseEnumValues(namedTypes []*parser.NamedType) (map[string][]*enumData, error) {
	enumMap := make(map[string][]*enumData)
	for _, namedType := range namedTypes {
		scanner := genlang.NewScanner(resourceKeywords())
		annotations, err := scanner.ScanNamedType(namedType)
		if err != nil {
			return nil, errors.Wrap(err, "scanner.ScanNamedType()")
		}

		var tableName string
		if annotations.Named.Has(enumerateKeyword) {
			tableName = string(annotations.Named.Get(enumerateKeyword))
		} else {
			continue
		}

		if t := namedType.TypeName(); t != stringGoType {
			return nil, errors.Newf("cannot enumerate type %q, underlying type must be %q, found %q", namedType.Name(), stringGoType, t)
		}

		data, ok := c.enumValues[tableName]
		if !ok {
			return nil, errors.Newf("cannot enumerate type %q, tableName %q has no values or does not exist", namedType.Name(), tableName)
		}

		enumMap[namedType.Name()] = data
	}

	return enumMap, nil
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

func removeGeneratedFiles(directory string, method generatedFileDeleteMethod) error {
	log.Printf("removing generated files in directory %q...", directory)
	dir, err := os.Open(directory)
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
		if !strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, ".ts") {
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
	var typeNames [][]string
	var typeNamesLen int
	for i, t := range types {
		typeNamesLen += len(t)
		if i == 0 || typeNamesLen > 80 {
			typeNamesLen = len(t)
			typeNames = append(typeNames, []string{})
		}

		typeNames[len(typeNames)-1] = append(typeNames[len(typeNames)-1], t)
	}

	var sb strings.Builder
	for _, row := range typeNames {
		sb.WriteString("\n\t")
		for _, cell := range row {
			line := fmt.Sprintf("%s | ", cell)
			sb.WriteString(line)
		}
	}

	return strings.TrimSuffix(strings.TrimPrefix(sb.String(), "\n"), " | ")
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
	if t.hasRPCMethodWithEnumeratedResource() {
		pkgs = append(pkgs, "Resources")
	}

	return strings.Join(pkgs, ", ")
}

func typescriptConsImports(t *typescriptGenerator, d *resource.TypescriptData) string {
	pkgs := make([]string, 0, 2)
	if len(d.Domains) > 0 {
		pkgs = append(pkgs, "Domain")
	}
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

func (c *client) doesResourceExist(resourceName string) bool {
	for _, res := range c.resources {
		if c.pluralize(res.Name()) == resourceName {
			return true
		}
	}

	return false
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
