// package generation provides the ability to generate resource, handler, and typescript permissions and metadata code from a resource file.
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
	"unicode/utf8"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/pkg"
	"github.com/cccteam/ccc/resource"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/spxscan"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

type client struct {
	loadPackages              []string
	resourceFilePath          string
	resources                 []resourceInfo
	rpcMethods                []rpcMethodInfo
	localPackages             []string
	db                        *cloudspanner.Client
	caser                     *strcase.Caser
	tableMap                  map[string]*tableMetadata
	handlerOptions            map[string]map[HandlerType][]OptionType
	pluralOverrides           map[string]string
	consolidatedResourceNames []string
	consolidateAll            bool
	consolidatedRoute         string
	genRPCMethods             bool
	cleanup                   func()
	spannerEmulatorVersion    string
	FileWriter
}

func newClient(ctx context.Context, resourceFilePath, migrationSourceURL string, localPackages []string, opts []option) (*client, error) {
	pkgInfo, err := pkg.Info()
	if err != nil {
		return nil, errors.Wrap(err, "pkg.Info()")
	}

	if err := os.Chdir(pkgInfo.AbsolutePath); err != nil {
		return nil, errors.Wrap(err, "os.Chdir()")
	}

	c := &client{}
	if err := resolveOptions(c, opts); err != nil {
		return nil, err
	}

	log.Println("Starting Spanner Container...")
	spannerContainer, err := initiator.NewSpannerContainer(ctx, c.spannerEmulatorVersion)
	if err != nil {
		return nil, errors.Wrap(err, "initiator.NewSpannerContainer()")
	}

	db, err := spannerContainer.CreateDatabase(ctx, "resourcegeneration")
	if err != nil {
		return nil, errors.Wrap(err, "container.CreateDatabase()")
	}

	cleanupFunc := func() {
		if err := db.DropDatabase(ctx); err != nil {
			panic(err)
		}

		if err := db.Close(); err != nil {
			panic(err)
		}
	}

	log.Println("Starting Spanner Migration...")
	if err := db.MigrateUp(migrationSourceURL); err != nil {
		return nil, errors.Wrap(err, "db.MigrateUp()")
	}

	c.loadPackages = append(c.loadPackages, resourceFilePath)
	c.resourceFilePath = resourceFilePath
	c.db = db.Client
	c.localPackages = localPackages
	c.cleanup = cleanupFunc
	c.caser = strcase.NewCaser(false, nil, nil)
	c.tableMap, err = c.newTableMap(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "c.newTableMap()")
	}

	return c, nil
}

func (c *client) Close() {
	c.cleanup()
}

func (c *client) localPackageImports() string {
	if len(c.localPackages) == 0 {
		return ""
	}

	return `"` + strings.Join(c.localPackages, "\"\n\t\"") + `"`
}

func (c *client) newTableMap(ctx context.Context) (map[string]*tableMetadata, error) {
	qry := `WITH DEPENDENCIES AS (
		SELECT DISTINCT
			kcu1.TABLE_NAME, 
			kcu1.COLUMN_NAME, 
			(SUM(CASE tc.CONSTRAINT_TYPE WHEN 'PRIMARY KEY' THEN 1 ELSE 0 END)) AS IS_PRIMARY_KEY,
			(SUM(CASE tc.CONSTRAINT_TYPE WHEN 'FOREIGN KEY' THEN 1 ELSE 0 END)) AS IS_FOREIGN_KEY,
			kcu1.ORDINAL_POSITION AS KEY_ORDINAL_POSITION,
			(CASE MIN(CASE 
					WHEN kcu4.TABLE_NAME IS NOT NULL THEN 1
					WHEN kcu2.TABLE_NAME IS NOT NULL THEN 2
					ELSE 3
					END)
			WHEN 1 THEN MAX(kcu4.TABLE_NAME)
			WHEN 2 THEN MAX(kcu2.TABLE_NAME)
			ELSE NULL
			END) AS REFERENCED_TABLE,
			(CASE MIN(CASE 
					WHEN kcu4.COLUMN_NAME IS NOT NULL THEN 1
					WHEN kcu2.COLUMN_NAME IS NOT NULL THEN 2
					ELSE 3
					END)
			WHEN 1 THEN MAX(kcu4.COLUMN_NAME)
			WHEN 2 THEN MAX(kcu2.COLUMN_NAME)
			ELSE NULL
			END) AS REFERENCED_COLUMN
		FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu1 -- All columns that are Primary Key or Foreign Key
		JOIN INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc ON tc.CONSTRAINT_NAME = kcu1.CONSTRAINT_NAME -- Identify whether column is Primary Key or Foreign Key
		-- All unique constraints (e.g. PK_Persons) referenced by foreign key constraints (e.g. FK_PersonPhones_PersonId)
		LEFT JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc ON rc.CONSTRAINT_NAME = kcu1.CONSTRAINT_NAME 
		LEFT JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu2 ON kcu2.CONSTRAINT_NAME = rc.UNIQUE_CONSTRAINT_NAME -- Table & Column belonging to referenced unique constraint (e.g. Persons, Id)
			AND kcu2.ORDINAL_POSITION = kcu1.ORDINAL_POSITION
		LEFT JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu3 ON kcu3.TABLE_NAME = kcu2.TABLE_NAME AND kcu3.COLUMN_NAME = kcu2.COLUMN_NAME
		LEFT JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc2 ON rc2.CONSTRAINT_NAME = kcu3.CONSTRAINT_NAME
		LEFT JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu4 ON kcu4.CONSTRAINT_NAME = rc2.UNIQUE_CONSTRAINT_NAME -- Table & Column belonging to 1-jump referenced unique constraint (e.g. DoeInstitutions, Id)
			AND kcu4.ORDINAL_POSITION = kcu1.ORDINAL_POSITION
		WHERE
			kcu1.CONSTRAINT_SCHEMA != 'INFORMATION_SCHEMA'
			AND tc.CONSTRAINT_TYPE IN ('PRIMARY KEY', 'FOREIGN KEY')
		GROUP BY kcu1.TABLE_NAME, kcu1.COLUMN_NAME, KEY_ORDINAL_POSITION
	)
	SELECT DISTINCT
		c.TABLE_NAME,
		c.COLUMN_NAME,
		(c.IS_NULLABLE = 'YES') AS IS_NULLABLE,
		c.SPANNER_TYPE,
		(d.IS_PRIMARY_KEY > 0 and d.IS_PRIMARY_KEY IS NOT NULL) as IS_PRIMARY_KEY,
		(d.IS_FOREIGN_KEY > 0 and d.IS_FOREIGN_KEY IS NOT NULL) as IS_FOREIGN_KEY,
		d.REFERENCED_TABLE,
		d.REFERENCED_COLUMN,
		(t.TABLE_NAME IS NULL AND v.TABLE_NAME IS NOT NULL) as IS_VIEW,
		v.VIEW_DEFINITION,
		ic.INDEX_NAME IS NOT NULL AS IS_INDEX,
		MAX(COALESCE(i.IS_UNIQUE, false)) AS IS_UNIQUE_INDEX,
		c.GENERATION_EXPRESSION,
		c.ORDINAL_POSITION,
		COALESCE(d.KEY_ORDINAL_POSITION, 1) AS KEY_ORDINAL_POSITION,
		c.COLUMN_DEFAULT IS NOT NULL AS HAS_DEFAULT,
	FROM INFORMATION_SCHEMA.COLUMNS c
		LEFT JOIN INFORMATION_SCHEMA.TABLES t ON c.TABLE_NAME = t.TABLE_NAME
			AND t.TABLE_TYPE = 'BASE TABLE'
		LEFT JOIN INFORMATION_SCHEMA.VIEWS v ON c.TABLE_NAME = v.TABLE_NAME
		LEFT JOIN DEPENDENCIES d ON c.TABLE_NAME = d.TABLE_NAME
			AND c.COLUMN_NAME = d.COLUMN_NAME
		LEFT JOIN INFORMATION_SCHEMA.INDEX_COLUMNS ic ON c.COLUMN_NAME = ic.COLUMN_NAME
			AND c.TABLE_NAME = ic.TABLE_NAME
		LEFT JOIN INFORMATION_SCHEMA.INDEXES i ON ic.INDEX_NAME = i.INDEX_NAME 
	WHERE 
		c.TABLE_SCHEMA != 'INFORMATION_SCHEMA'
		AND c.COLUMN_NAME NOT LIKE '%_HIDDEN'
	GROUP BY c.TABLE_NAME, c.COLUMN_NAME, IS_NULLABLE, c.SPANNER_TYPE,
	d.IS_PRIMARY_KEY, d.IS_FOREIGN_KEY, d.REFERENCED_COLUMN, d.REFERENCED_TABLE,
	IS_VIEW, v.VIEW_DEFINITION, IS_INDEX, c.GENERATION_EXPRESSION, c.ORDINAL_POSITION, d.KEY_ORDINAL_POSITION, c.COLUMN_DEFAULT
	ORDER BY c.TABLE_NAME, c.ORDINAL_POSITION`

	return c.createTableMapUsingQuery(ctx, qry)
}

func (c *client) createTableMapUsingQuery(ctx context.Context, qry string) (map[string]*tableMetadata, error) {
	log.Println("Creating spanner table lookup...")

	stmt := cloudspanner.Statement{SQL: qry}

	var result []InformationSchemaResult
	if err := spxscan.Select(ctx, c.db.Single(), &result, stmt); err != nil {
		return nil, errors.Wrap(err, "spxscan.Select()")
	}

	m := make(map[string]*tableMetadata)
	for _, r := range result {
		table, ok := m[r.TableName]
		if !ok {
			table = &tableMetadata{
				Columns:       make(map[string]columnMeta),
				SearchIndexes: make(map[string][]*searchExpression),
				IsView:        r.IsView,
			}
		}

		if r.SpannerType == "TOKENLIST" {
			continue
		}

		column, ok := table.Columns[r.ColumnName]
		if !ok {
			column = columnMeta{
				ColumnName:         r.ColumnName,
				SpannerType:        r.SpannerType,
				OrdinalPosition:    r.OrdinalPosition - 1, // SQL is 1-indexed. For consistency with JavaScript & Go we translate to 0-indexed
				KeyOrdinalPosition: r.KeyOrdinalPosition - 1,
			}
		}

		if r.IsPrimaryKey {
			table.PkCount++
			column.IsPrimaryKey = true
		}

		if r.IsForeignKey {
			column.IsForeignKey = true

			if r.ReferencedTable != nil {
				column.ReferencedTable = *r.ReferencedTable
			}

			if r.ReferencedColumn != nil {
				column.ReferencedColumn = *r.ReferencedColumn
			}
		}

		if r.IsNullable {
			column.IsNullable = true
		}

		if r.IsIndex {
			column.IsIndex = true
		}

		if r.IsUniqueIndex {
			column.IsUniqueIndex = true
		}

		if r.HasDefault {
			column.HasDefault = true
		}

		table.Columns[r.ColumnName] = column
		m[r.TableName] = table
	}

	for _, r := range result {
		if r.SpannerType != "TOKENLIST" {
			continue
		}

		table := m[r.TableName]

		var generationExpr string
		switch {
		// If the TokenList column is in a View, we don't have direct access to
		// the generation expression. We need to grab the source table's name from
		// the view definition then find it in the information schema results.
		case r.IsView:
			sourceTableName, err := originTableName(*r.ViewDefinition, r.ColumnName)
			if err != nil {
				return nil, err
			}

			sourceTableIndex := slices.IndexFunc(result, func(e InformationSchemaResult) bool {
				return e.TableName == sourceTableName && e.ColumnName == r.ColumnName
			})
			if sourceTableIndex < 0 {
				return nil, errors.Newf("could not find source table %q for TOKENLIST column %q in %q", sourceTableName, r.ColumnName, r.TableName)
			}

			generationExpr = *result[sourceTableIndex].GenerationExpression

		case r.GenerationExpression == nil:
			return nil, errors.Newf("generation expression not found for tokenlist column=`%s` table=`%s`", r.ColumnName, r.TableName)

		default:
			generationExpr = *r.GenerationExpression
		}

		expressionFields, err := searchExpressionFields(generationExpr, table.Columns)
		if err != nil {
			return nil, errors.Wrapf(err, "searchExpressionFields table=`%s`", r.TableName)
		}

		table.SearchIndexes[r.ColumnName] = append(table.SearchIndexes[r.ColumnName], expressionFields...)
	}

	return m, nil
}

func (c *client) lookupTable(resourceName string) (*tableMetadata, error) {
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
		"Camel":                        c.caser.ToCamel,
		"Pascal":                       c.caser.ToPascal,
		"Kebab":                        c.caser.ToKebab,
		"Lower":                        strings.ToLower,
		"FormatResourceInterfaceTypes": formatResourceInterfaceTypes,
		"FormatRPCInterfaceTypes":      formatRPCInterfaceTypes,
		"ResourceSearchType": func(searchType string) string {
			switch strings.ToUpper(searchType) {
			case "SUBSTRING":
				return "resource.SubString"
			case "FULLTEXT":
				return "resource.FullText"
			case "NGRAMS":
				return "resource.Ngram"
			default:
				return ""
			}
		},
		"DetermineTestURL": func(structName, routePrefix string, route generatedRoute) string {
			if strings.EqualFold(route.Method, "get") && strings.HasSuffix(route.Path, fmt.Sprintf("{%sID}", strcase.ToGoCamel(structName))) {
				return fmt.Sprintf("/%s/%s/%s",
					routePrefix,
					c.caser.ToKebab(c.pluralize(structName)),
					strcase.ToGoCamel(fmt.Sprintf("test%sID", c.caser.ToPascal(structName))),
				)
			}

			return route.Path
		},
		"DetermineParameters": func(structName string, route generatedRoute) string {
			if strings.EqualFold(route.Method, "get") && strings.HasSuffix(route.Path, fmt.Sprintf("{%sID}", strcase.ToGoCamel(structName))) {
				return fmt.Sprintf(`map[string]string{%q: %q}`, strcase.ToGoCamel(structName+"ID"), strcase.ToGoCamel(fmt.Sprintf("test%sID", c.caser.ToPascal(structName))))
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
		"SanitizeIdentifier": c.sanitizeEnumIdentifier,
	}

	return templateFuncs
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

func RemoveGeneratedFiles(directory string, method GeneratedFileDeleteMethod) error {
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
		case Prefix:
			if err := removeGeneratedFileByPrefix(directory, f); err != nil {
				return errors.Wrap(err, "removeGeneratedFileByPrefix()")
			}
		case HeaderComment:
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

func formatResourceInterfaceTypes(resources []resourceInfo) string {
	names := make([]string, len(resources))
	for i, resource := range resources {
		names[i] = resource.Name()
	}

	return formatInterfaceTypes(names)
}

func formatRPCInterfaceTypes(rpcMethods []rpcMethodInfo) string {
	names := make([]string, len(rpcMethods))
	for i, rpcMethod := range rpcMethods {
		names[i] = rpcMethod.Name()
	}

	return formatInterfaceTypes(names)
}

func searchExpressionFields(expression string, cols map[string]columnMeta) ([]*searchExpression, error) {
	var flds []*searchExpression

	for _, match := range tokenizeRegex.FindAllStringSubmatch(expression, -1) {
		if len(match) != 3 {
			return nil, errors.Newf("expression `%s` has unexpected number of matches: `%d` (expected 3)", expression, len(match))
		}

		var tokenType resource.SearchType
		switch match[1] {
		case "TOKENIZE_SUBSTRING":
			tokenType = resource.SubString
		case "TOKENIZE_FULLTEXT":
			tokenType = resource.FullText
		case "TOKENIZE_NGRAMS":
			tokenType = resource.Ngram
		default:
			continue
		}

		fieldName := match[2]
		if _, ok := cols[fieldName]; !ok { // sanity check that the field name is a real column. just in case the regex leaks something improper
			return nil, errors.Newf("column `%s` from expression `%s` was not found in table (is the tokenizeRegex working?)", fieldName, match[0])
		}

		flds = append(flds, &searchExpression{
			tokenType: tokenType,
			argument:  match[2],
		})
	}

	return flds, nil
}

// Returns slice of applicable handler types for a given resource.
// Every resource starts with a List handler.
// Views do not have Read handlers.
// Consolidated resources do not have Patch handlers.
// Ignored handler types are filtered out.
func (c *client) resourceEndpoints(resource resourceInfo) []HandlerType {
	handlerTypes := []HandlerType{ListHandler}

	if !resource.IsView {
		handlerTypes = append(handlerTypes, ReadHandler)

		if !resource.IsConsolidated && !c.consolidateAll {
			handlerTypes = append(handlerTypes, PatchHandler)
		}
	}

	handlerTypes = slices.DeleteFunc(handlerTypes, func(ht HandlerType) bool {
		return slices.Contains(resource.SuppressedHandlers[:], ht)
	})

	return handlerTypes
}

func (c *client) sanitizeEnumIdentifier(name string) string {
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

	return c.caser.ToPascal(string(result))
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
