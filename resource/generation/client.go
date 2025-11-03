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

type generatorType string

const (
	resourceGeneratorType   generatorType = "resource"
	typeScriptGeneratorType generatorType = "typescript"
)

var caser = strcase.NewCaser(false, nil, nil)

type client struct {
	loadPackages       []string
	resourceFilePath   string
	resources          []*resourceInfo
	computedResources  []*computedResource
	rpcMethods         []*rpcMethodInfo
	localPackages      []string
	rpcPackageDir      string
	rpcPackageName     string
	compPackageDir     string
	compPackageName    string
	migrationSourceURL string
	tableMap           map[string]*tableMetadata
	enumValues         map[string][]*enumData
	pluralOverrides    map[string]string
	consolidateConfig
	genRPCMethods          bool
	genComputedResources   bool
	spannerEmulatorVersion string
	FileWriter
	genCache *cache.Cache
}

func newClient(ctx context.Context, genType generatorType, resourceFilePath, migrationSourceURL string, localPackages []string, opts []option) (*client, error) {
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
		migrationSourceURL: migrationSourceURL,
		genCache:           gCache,
	}
	if err := resolveOptions(c, opts); err != nil {
		return nil, err
	}

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
		if err := c.genCache.DeleteSubpath("migrations"); err != nil {
			return nil, errors.Wrap(err, "cache.Cache.DeleteSubpath()")
		}
		if err := c.runSpanner(ctx, c.spannerEmulatorVersion, migrationSourceURL); err != nil {
			return nil, err
		}
	}

	c.loadPackages = append(c.loadPackages, resourceFilePath)
	c.resourceFilePath = resourceFilePath
	c.localPackages = localPackages
	c.migrationSourceURL = migrationSourceURL

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
	if len(c.localPackages) == 0 {
		return ""
	}

	return `"` + strings.Join(c.localPackages, "\"\n\t\"") + `"`
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
		return nil, errors.Newf("resource %q pluralized as %q not in tableMetadata", resourceName, c.pluralize(resourceName))
	}

	return table, nil
}

func (c *client) templateFuncs() map[string]any {
	templateFuncs := map[string]any{
		"Pluralize":                    c.pluralize,
		"GoCamel":                      strcase.ToGoCamel,
		"Camel":                        strcase.ToCamel,
		"Pascal":                       strcase.ToPascal,
		"Kebab":                        strcase.ToKebab,
		"Lower":                        strings.ToLower,
		"FormatResourceInterfaceTypes": formatResourceInterfaceTypes,
		"FormatRPCInterfaceTypes":      formatRPCInterfaceTypes,
		"DetermineTestURL": func(resource resourceInfo, routePrefix string, route generatedRoute) string {
			if !resource.IsView &&
				strings.EqualFold(route.Method, "get") &&
				(strings.Contains(route.Path, fmt.Sprintf("{%sID}", strcase.ToGoCamel(resource.Name()))) || strings.Contains(route.Path, resource.PrimaryKey().Name())) {
				if resource.HasCompoundPrimaryKey() {
					url := fmt.Sprintf("/%s/%s", routePrefix, caser.ToKebab(c.pluralize(resource.Name())))

					for _, key := range resource.PrimaryKeys() {
						url += fmt.Sprintf("/test%s%s", caser.ToPascal(resource.Name()), key.Name())
					}

					return url
				}

				return fmt.Sprintf("/%s/%s/%s",
					routePrefix,
					caser.ToKebab(c.pluralize(resource.Name())),
					strcase.ToGoCamel(fmt.Sprintf("test%sID", caser.ToPascal(resource.Name()))),
				)
			}

			return route.Path
		},
		"DetermineParameters": func(resource resourceInfo, route generatedRoute) string {
			if !resource.IsView &&
				strings.EqualFold(route.Method, "get") &&
				(strings.Contains(route.Path, fmt.Sprintf("{%sID}", strcase.ToGoCamel(resource.Name()))) || strings.Contains(route.Path, resource.PrimaryKey().Name())) {
				if resource.HasCompoundPrimaryKey() {
					params := "map[string]string{"
					for _, key := range resource.PrimaryKeys() {
						params += fmt.Sprintf(`"%[1]s%[3]s": "test%[2]s%[3]s", `, strcase.ToGoCamel(resource.Name()), caser.ToPascal(resource.Name()), key.Name())
					}

					params += "}"

					return params
				}

				return fmt.Sprintf(`map[string]string{%q: %q}`, strcase.ToGoCamel(resource.Name()+"ID"), strcase.ToGoCamel(fmt.Sprintf("test%sID", caser.ToPascal(resource.Name()))))
			}

			return "map[string]string{}"
		},
		"MethodToHttpConst": func(method string) (string, error) {
			switch method {
			case "GET":
				return "http.MethodGet", nil
			case "POST":
				return "http.MethodPost", nil
			case "PATCH":
				return "http.MethodPatch", nil
			default:
				return "", errors.Newf("MethodToHttpConst: unknown method: %s", method)
			}
		},
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

func (c *client) generateTemplateOutput(templateName, fileTemplate string, data map[string]any) ([]byte, error) {
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

func (c *client) retrieveDatabaseEnumValues(namedTypes []*parser.NamedType) (map[string][]*enumData, error) {
	enumMap := make(map[string][]*enumData)
	for _, namedType := range namedTypes {
		scanner := genlang.NewScanner(keywords())
		result, err := scanner.ScanNamedType(namedType)
		if err != nil {
			return nil, errors.Wrap(err, "scanner.ScanNamedType()")
		}

		var tableName string
		if result.Named.Has(enumerateKeyword) {
			tableName = result.Named.GetOne(enumerateKeyword).Arg1
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

func (c *client) pluralize(value string) string {
	if plural, ok := c.pluralOverrides[value]; ok {
		return plural
	}

	var pluralValue string
	toLower := strings.ToLower(value)
	switch {
	case strings.HasSuffix(toLower, "y"):
		pluralValue = value[:len(value)-1] + "ies"
	case strings.HasSuffix(toLower, "s"):
		pluralValue = value + "es"
	default:
		pluralValue = value + "s"
	}

	c.pluralOverrides[value] = pluralValue
	// This should prevent any accidental double-pluralizations
	c.pluralOverrides[pluralValue] = pluralValue

	return pluralValue
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

	generationHeader := "// Code generated by resourcegeneration. DO NOT EDIT."
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

func formatResourceInterfaceTypes(resourcesPackage, computedResourcePackage string, resources []*resourceInfo, computedResources []*computedResource) string {
	names := make([]string, 0, len(resources)+len(computedResources))
	for _, res := range resources {
		names = append(names, fmt.Sprintf("%s.%s", resourcesPackage, res.Name()))
	}

	for _, res := range computedResources {
		names = append(names, fmt.Sprintf("%s.%s", computedResourcePackage, res.Name()))
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

	if !res.IsView {
		handlerTypes = append(handlerTypes, ReadHandler)

		if !res.IsConsolidated {
			handlerTypes = append(handlerTypes, PatchHandler)
		}
	}

	handlerTypes = slices.DeleteFunc(handlerTypes, func(ht HandlerType) bool {
		return slices.Contains(res.SuppressedHandlers[:], ht)
	})

	return handlerTypes
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
