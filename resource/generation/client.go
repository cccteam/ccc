// package generation provides the ability to generate resource, handler, and typescript permissions and metadata code from a resource file.
package generation

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/resource"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/spxscan"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
	"github.com/momaek/formattag/align"
	"golang.org/x/tools/imports"
)

type Client struct {
	genHandlers           func() error
	genTypescriptPerm     func() error
	genTypescriptMeta     func() error
	resourceFilePath      string
	resourceTree          *ast.File
	resourceDestination   string
	handlerDestination    string
	typescriptDestination string
	rc                    *resource.Collection
	db                    *cloudspanner.Client
	caser                 *strcase.Caser
	structNames           []string
	tableLookup           map[string]*TableMetadata
	handlerOptions        map[string]map[HandlerType][]OptionType
	pluralOverrides       map[string]string
	metadataTemplate      []byte
	cleanup               func()

	muAlign sync.Mutex
}

func New(ctx context.Context, resourceFilePath, migrationSourceURL string, generatorOptions ...ClientOption) (*Client, error) {
	spannerContainer, err := initiator.NewSpannerContainer(ctx, "latest")
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

	if err := db.MigrateUp(migrationSourceURL); err != nil {
		return nil, errors.Wrap(err, "db.MigrateUp()")
	}

	resourceTree, err := parseResourceFile(resourceFilePath)
	if err != nil {
		return nil, err
	}

	structs := structsFromSource(resourceTree)

	c := &Client{
		db:                  db.Client,
		resourceFilePath:    resourceFilePath,
		resourceTree:        resourceTree,
		resourceDestination: filepath.Dir(resourceFilePath),
		cleanup:             cleanupFunc,
		caser:               strcase.NewCaser(false, nil, nil),
		structNames:         structs,
	}

	for _, optionFunc := range generatorOptions {
		if err := optionFunc(c); err != nil {
			return nil, err
		}
	}

	c.tableLookup, err = c.createTableLookup(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "c.createTableLookup()")
	}

	return c, nil
}

func (c *Client) Close() {
	c.cleanup()
}

func (c *Client) RunGeneration() error {
	if err := c.runResourcesGeneration(); err != nil {
		return errors.Wrap(err, "c.genResources()")
	}
	if c.genHandlers != nil {
		if err := c.genHandlers(); err != nil {
			return errors.Wrap(err, "c.genHandlers()")
		}
	}
	if c.genTypescriptMeta != nil {
		if err := c.genTypescriptMeta(); err != nil {
			return errors.Wrap(err, "c.genTypescriptMeta()")
		}
	}
	if c.genTypescriptPerm != nil {
		if err := c.genTypescriptPerm(); err != nil {
			return errors.Wrap(err, "c.genTypescriptPerm()")
		}
	}

	return nil
}

func (c *Client) createTableLookup(ctx context.Context) (map[string]*TableMetadata, error) {
	qry := `SELECT DISTINCT
		c.TABLE_NAME,
		c.COLUMN_NAME,
		kcu.CONSTRAINT_NAME,
		(c.IS_NULLABLE = 'YES') AS IS_NULLABLE,
		c.SPANNER_TYPE,
		tc.CONSTRAINT_TYPE,
		(t.TABLE_NAME IS NULL AND v.TABLE_NAME IS NOT NULL) as IS_VIEW,
		ic.INDEX_NAME IS NOT NULL AS IS_INDEX,
		COALESCE(i.IS_UNIQUE, false) AS IS_UNIQUE_INDEX,
		c.GENERATION_EXPRESSION,
		c.ORDINAL_POSITION
	FROM INFORMATION_SCHEMA.COLUMNS c
		LEFT JOIN INFORMATION_SCHEMA.TABLES t ON c.TABLE_NAME = t.TABLE_NAME
			AND t.TABLE_TYPE = 'BASE TABLE'
		LEFT JOIN INFORMATION_SCHEMA.VIEWS v ON c.TABLE_NAME = v.TABLE_NAME
		LEFT JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu ON c.COLUMN_NAME = kcu.COLUMN_NAME
			AND c.TABLE_NAME = kcu.TABLE_NAME
			AND kcu.POSITION_IN_UNIQUE_CONSTRAINT IS NULL
		LEFT JOIN INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc ON kcu.CONSTRAINT_NAME = tc.CONSTRAINT_NAME
		LEFT JOIN INFORMATION_SCHEMA.INDEX_COLUMNS ic ON c.COLUMN_NAME = ic.COLUMN_NAME
			AND c.TABLE_NAME = ic.TABLE_NAME
		LEFT JOIN INFORMATION_SCHEMA.INDEXES i ON ic.INDEX_NAME = i.INDEX_NAME 
			AND c.TABLE_NAME = i.TABLE_NAME 
	WHERE c.TABLE_SCHEMA != 'INFORMATION_SCHEMA'
	ORDER BY c.TABLE_NAME, c.ORDINAL_POSITION`

	return c.createLookupMapForQuery(ctx, qry)
}

func (c *Client) createLookupMapForQuery(ctx context.Context, qry string) (map[string]*TableMetadata, error) {
	log.Println("Creating spanner table lookup...")

	stmt := cloudspanner.Statement{SQL: qry}

	var result []InformationSchemaResult
	if err := spxscan.Select(ctx, c.db.Single(), &result, stmt); err != nil {
		return nil, errors.Wrap(err, "spxscan.Select()")
	}

	m := make(map[string]*TableMetadata)
	for _, r := range result {
		tableName := r.TableName

		table, ok := m[tableName]
		if !ok || table.Columns == nil {
			table = &TableMetadata{
				Columns:       make(map[string]FieldMetadata),
				SearchIndexes: make(map[string][]*expressionField),
				IsView:        r.IsView,
			}
		}

		if r.SpannerType == "TOKENLIST" {
			if r.GenerationExpression != nil {
				for _, f := range searchExpressionFields(*r.GenerationExpression, table.Columns) {
					table.SearchIndexes[r.ColumnName] = append(table.SearchIndexes[r.ColumnName], f)
				}

				continue
			}
		}

		var ct ConstraintType
		if r.ConstraintType != nil {
			ct = ConstraintType(*r.ConstraintType)
		}

		if _, ok := table.Columns[r.ColumnName]; !ok {
			table.Columns[r.ColumnName] = FieldMetadata{
				ColumnName:     r.ColumnName,
				SpannerType:    r.SpannerType,
				IsNullable:     r.IsNullable,
				ConstraintType: ct,
				IsIndex:        r.IsIndex,
				IsUniqueIndex:  r.IsUniqueIndex,
			}
		}

		m[tableName] = table
	}

	return m, nil
}

func (c *Client) writeBytesToFile(destination string, file *os.File, data []byte, goFormat bool) error {
	if goFormat {
		var err error
		data, err = format.Source(data)
		if err != nil {
			return errors.Wrap(err, "format.Source()")
		}

		data, err = imports.Process(destination, data, nil)
		if err != nil {
			return errors.Wrap(err, "imports.Process()")
		}

		// align package is not concurrent safe
		c.muAlign.Lock()
		defer c.muAlign.Unlock()

		align.Init(bytes.NewReader(data))
		data, err = align.Do()
		if err != nil {
			return errors.Wrap(err, "align.Do()")
		}
	}

	if err := file.Truncate(0); err != nil {
		return errors.Wrap(err, "file.Truncate()")
	}
	if _, err := file.Seek(0, 0); err != nil {
		return errors.Wrap(err, "file.Seek()")
	}
	if _, err := file.Write(data); err != nil {
		return errors.Wrap(err, "file.Write()")
	}

	return nil
}

func structsFromSource(resourceTree *ast.File) []string {
	structs := make([]string, 0)
	for k, v := range resourceTree.Scope.Objects {
		spec, ok := v.Decl.(*ast.TypeSpec)
		if !ok {
			continue
		}
		if _, ok := spec.Type.(*ast.StructType); ok {
			structs = append(structs, k)
		}
	}

	sort.Slice(structs, func(i, j int) bool {
		return structs[i] < structs[j]
	})

	return structs
}

func (c *Client) templateFuncs() map[string]any {
	templateFuncs := map[string]any{
		"Pluralize": c.pluralize,
		"GoCamel":   strcase.ToGoCamel,
		"Camel":     c.caser.ToCamel,
		"Kebab":     c.caser.ToKebab,
		"Lower":     strings.ToLower,
		"PrimaryKeyTypeIsUUID": func(fields []*typeField) bool {
			for _, f := range fields {
				if f.IsPrimaryKey {
					return f.Type == "ccc.UUID"
				}
			}

			return false
		},
		"FormatPerm": func(s string) string {
			if s == "" {
				return ""
			}

			return ` perm:"` + s + `"`
		},
		"PrimaryKeyType": func(fields []*typeField) string {
			for _, f := range fields {
				if f.IsPrimaryKey {
					return f.Type
				}
			}

			return ""
		},
		"FormatQueryTag": func(query string) string {
			if query != "" {
				return " " + query
			}

			return ""
		},
		"DetermineJSONTag": func(field *typeField, isPatch bool) string {
			if isPatch {
				if field.IsPrimaryKey {
					return "-"
				}

				for _, c := range field.Conditions {
					if c == "immutable" {
						return "-"
					}
				}
			}

			val := c.caser.ToCamel(field.Name)
			if !field.IsPrimaryKey && !isPatch {
				val += ",omitempty"
			}

			return val
		},
		"FormatResourceInterfaceTypes": formatResourceInterfaceTypes,
		"FormatTokenTag":               c.formatTokenTags,
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
	}

	return templateFuncs
}

func (c *Client) pluralize(value string) string {
	if plural, ok := c.pluralOverrides[value]; ok {
		return plural
	}

	toLower := strings.ToLower(value)
	switch {
	case strings.HasSuffix(toLower, "y"):
		return value[:len(value)-1] + "ies"
	case strings.HasSuffix(toLower, "s"):
		return value + "es"
	default:
		return value + "s"
	}
}

func (c *Client) formatTokenTags(tableName, fieldName string) string {
	tokenIndexMap := make(map[string][]string)
	if t, ok := c.tableLookup[tableName]; ok {
		for k, v := range t.SearchIndexes {
			for _, f := range v {
				if f.fieldName == fieldName {
					tokenIndexMap[f.tokenType] = append(tokenIndexMap[f.tokenType], k)
				}
			}
		}
	}

	var tags []string
	for tt, indexes := range tokenIndexMap {
		tags = append(tags, fmt.Sprintf(`%s:"%s"`, tt, strings.Join(indexes, ",")))
	}

	return strings.Join(tags, " ")
}

func fieldType(expr ast.Expr, isHandlerOutput bool) string {
	switch t := expr.(type) {
	case *ast.Ident:
		switch {
		case slices.Contains(baseTypes, t.Name) || !isHandlerOutput:
			return t.Name
		default:
			return fmt.Sprintf("resources.%s", t.Name)
		}
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", t.X, t.Sel.Name)
	case *ast.StarExpr:
		return "*" + fieldType(t.X, isHandlerOutput)
	case *ast.ArrayType:
		return "[]" + fieldType(t.Elt, isHandlerOutput)
	default:
		panic(fmt.Sprintf("unknown type: %T", t))
	}
}

func removeGeneratedFiles(directory string, method GeneratedFileDeleteMethod) error {
	dir, err := os.Open(directory)
	if err != nil {
		return errors.Wrap(err, "os.Open()")
	}
	defer dir.Close()

	files, err := dir.Readdirnames(0)
	if err != nil {
		return errors.Wrap(err, "dir.Readdirnames()")
	}

	for _, f := range files {
		if !strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, ".ts") {
			continue
		}

		switch method {
		case Suffix:
			if err := removeGeneratedFileBySuffix(directory, f); err != nil {
				return errors.Wrap(err, "removeGeneratedFileBySuffix()")
			}
		case HeaderComment:
			if err := removeGeneratedFileByHeaderComment(directory, f); err != nil {
				return errors.Wrap(err, "removeGeneratedFileByHeaderComment()")
			}
		}
	}

	return nil
}

func removeGeneratedFileBySuffix(directory, file string) error {
	if strings.HasSuffix(file, "_generated.go") {
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

func formatResourceInterfaceTypes(types []*generatedType) string {
	var typeNames [][]string
	var typeNamesLen int
	for i, t := range types {
		typeNamesLen += len(t.Name)
		if i == 0 || typeNamesLen > 80 {
			typeNamesLen = len(t.Name)
			typeNames = append(typeNames, []string{})
		}

		typeNames[len(typeNames)-1] = append(typeNames[len(typeNames)-1], t.Name)
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

func parseResourceFile(resourceFilePath string) (*ast.File, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, resourceFilePath, nil, 0)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}
	if file == nil {
		return nil, errors.Newf("unable to parse `%s`", resourceFilePath)
	}

	return file, nil
}

func searchExpressionFields(expression string, cols map[string]FieldMetadata) []*expressionField {
	fieldMap := make(map[string]*expressionField)

	lines := strings.Split(expression, "\n")
	for i := 1; i < len(lines)-1; i++ {
		l := lines[i]
		if matches := tokenizeRegex.FindAllStringSubmatch(l, -1); len(matches) > 0 && len(matches[0]) > 2 {
			searchType := matches[0][1]

			// todo(jkyte): use resource.SearchType when it's available
			var tokenType string
			switch searchType {
			case "SUBSTRING":
				tokenType = "substring"
			case "FULLTEXT":
				tokenType = "fulltext"
			case "NGRAMS":
				tokenType = "ngram"
			}

			fieldName := matches[0][2]
			colKeys := maps.Keys(cols)
			for k := range colKeys {
				if strings.Contains(fieldName, k) {
					fieldName = k
					break
				}
			}

			if _, ok := fieldMap[fieldName]; !ok && tokenType != "" {
				fieldMap[fieldName] = &expressionField{
					tokenType: tokenType,
					fieldName: fieldName,
				}
			}
		}
	}

	return slices.Collect(maps.Values(fieldMap))
}
