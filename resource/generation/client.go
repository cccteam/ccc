package generation

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	cloudspanner "cloud.google.com/go/spanner"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/spxscan"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
	"github.com/momaek/formattag/align"
	"golang.org/x/tools/imports"
)

type GenerationClient struct {
	resourceSource        string
	spannerDestination    string
	handlerDestination    string
	typescriptDestination string
	db                    *cloudspanner.Client
	caser                 *strcase.Caser
	tableLookup           map[string]*TableMetadata
	handlerOptions        map[string]map[HandlerType][]OptionType
	pluralOverrides       map[string]string
	cleanup               func()

	muAlign sync.Mutex
}

func New(ctx context.Context, config *Config) (*GenerationClient, error) {
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

	if config.Migrations != "" {
		if err := db.MigrateUp(config.Migrations); err != nil {
			return nil, errors.Wrap(err, "db.MigrateUp()")
		}
	}

	handlerOpts := make(map[string]map[HandlerType][]OptionType)
	for structName, handlerTypes := range config.IgnoredHandlers {
		for _, handlerType := range handlerTypes {
			if _, ok := handlerOpts[structName]; !ok {
				handlerOpts[structName] = make(map[HandlerType][]OptionType)
			}
			handlerOpts[structName][handlerType] = append(handlerOpts[structName][handlerType], NoGenerate)
		}
	}

	c := &GenerationClient{
		resourceSource:     config.ResourceSource,
		spannerDestination: config.SpannerDestination,
		handlerDestination: config.HandlerDestination,
		handlerOptions:     handlerOpts,
		pluralOverrides:    config.PluralOverrides,
		db:                 db.Client,
		cleanup:            cleanupFunc,
		caser:              strcase.NewCaser(false, config.CaserGoInitialisms, nil),
	}

	c.tableLookup, err = c.createTableLookup(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "c.createTableLookup()")
	}

	return c, nil
}

func (c *GenerationClient) Close() {
	c.cleanup()
}

func (c *GenerationClient) createTableLookup(ctx context.Context) (map[string]*TableMetadata, error) {
	qry := `SELECT DISTINCT
		c.TABLE_NAME,
		c.COLUMN_NAME,
		kcu.CONSTRAINT_NAME,
		(c.IS_NULLABLE = 'YES') AS IS_NULLABLE,
		c.SPANNER_TYPE,
		tc.CONSTRAINT_TYPE,
		(t.TABLE_NAME IS NULL AND v.TABLE_NAME IS NOT NULL) as IS_VIEW,
		ic.INDEX_NAME IS NOT NULL as IS_INDEX,
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
	WHERE c.TABLE_SCHEMA != 'INFORMATION_SCHEMA'
	ORDER BY c.TABLE_NAME, c.ORDINAL_POSITION`

	return c.createLookupMapForQuery(ctx, qry)
}

func (c *GenerationClient) createLookupMapForQuery(ctx context.Context, qry string) (map[string]*TableMetadata, error) {
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
				Columns: make(map[string]FieldMetadata),
				IsView:  r.IsView,
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
			}
		}

		m[tableName] = table
	}

	return m, nil
}

func (c *GenerationClient) writeBytesToFile(destination string, file *os.File, data []byte) error {
	data, err := format.Source(data)
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

func (c *GenerationClient) structsFromSource() ([]string, error) {
	tk := token.NewFileSet()
	parse, err := parser.ParseFile(tk, c.resourceSource, nil, 0)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}

	if parse == nil || parse.Scope == nil {
		return nil, errors.New("unable to parse file")
	}

	structs := make([]string, 0)
	for k, v := range parse.Scope.Objects {
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

	return structs, nil
}

func (c *GenerationClient) templateFuncs() map[string]any {
	templateFuncs := map[string]any{
		"Pluralize": c.pluralize,
		"GoCamel":   strcase.ToGoCamel,
		"Camel":     c.caser.ToCamel,
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
	}

	return templateFuncs
}

func (c *GenerationClient) pluralize(value string) string {
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
		if !strings.HasSuffix(f, ".go") {
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
