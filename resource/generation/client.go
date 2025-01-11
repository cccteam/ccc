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
	"slices"
	"sort"
	"strings"

	cloudspanner "cloud.google.com/go/spanner"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/spxscan"
	"github.com/ettle/strcase"
	"github.com/gertd/go-pluralize"
	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/imports"
)

type GenerationClient struct {
	resourceSource     string
	spannerDestination string
	handlerDestination string
	db                 *cloudspanner.Client
	pluralizer         *pluralize.Client
	tableFieldLookup   map[string]TableMetadata
	handlerOptions     map[string]map[HandlerType][]OptionType
	cleanup            func()
}

func New(ctx context.Context, config *Config) (*GenerationClient, error) {
	spannerContainer, err := initiator.NewSpannerContainer(ctx, "latest")
	if err != nil {
		return nil, errors.Wrap(err, "initiator.NewSpannerContainer()")
	}

	db, err := spannerContainer.CreateDatabase(ctx, "spannergen")
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

	pluralizer := pluralize.NewClient()
	for k, v := range config.PluralRules {
		pluralizer.AddPluralRule(k, v)
	}

	c := &GenerationClient{
		resourceSource:     config.ResourceSource,
		spannerDestination: config.SpannerDestination,
		handlerDestination: config.HandlerDestination,
		handlerOptions:     config.HandlerOptions,
		db:                 db.Client,
		pluralizer:         pluralizer,
		cleanup:            cleanupFunc,
	}

	c.tableFieldLookup, err = c.createTableLookup(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "c.createTableLookup()")
	}

	return c, nil
}

func (c *GenerationClient) Close() {
	c.cleanup()
}

func (c *GenerationClient) createTableLookup(ctx context.Context) (map[string]TableMetadata, error) {
	qry := `SELECT
		c.TABLE_NAME,
		c.COLUMN_NAME,
		kcu.CONSTRAINT_NAME,
		(c.IS_NULLABLE = 'YES') AS IS_NULLABLE,
		c.SPANNER_TYPE,
		tc.CONSTRAINT_TYPE,
		(t.TABLE_NAME IS NULL AND v.TABLE_NAME IS NOT NULL) as IS_VIEW,
		ic.INDEX_NAME IS NOT NULL as IS_INDEX
	FROM INFORMATION_SCHEMA.COLUMNS c
		LEFT JOIN INFORMATION_SCHEMA.TABLES t ON c.TABLE_NAME = t.TABLE_NAME
			AND t.TABLE_TYPE = 'BASE TABLE'
		LEFT JOIN INFORMATION_SCHEMA.VIEWS v ON c.TABLE_NAME = v.TABLE_NAME
		LEFT JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu ON c.COLUMN_NAME = kcu.COLUMN_NAME
			AND c.TABLE_NAME = kcu.TABLE_NAME
		LEFT JOIN INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc ON kcu.CONSTRAINT_NAME = tc.CONSTRAINT_NAME
		LEFT JOIN INFORMATION_SCHEMA.INDEX_COLUMNS ic ON c.COLUMN_NAME = ic.COLUMN_NAME
			AND c.TABLE_NAME = ic.TABLE_NAME
	WHERE c.TABLE_SCHEMA != 'INFORMATION_SCHEMA'
	ORDER BY c.TABLE_NAME, c.ORDINAL_POSITION`

	return c.createLookupMapForQuery(ctx, qry)
}

func (c *GenerationClient) createLookupMapForQuery(ctx context.Context, qry string) (map[string]TableMetadata, error) {
	stmt := cloudspanner.Statement{SQL: qry}

	var result []InformationSchemaResult
	if err := spxscan.Select(ctx, c.db.Single(), &result, stmt); err != nil {
		return nil, errors.Wrap(err, "spxscan.Select()")
	}

	// We need a custom caser to handle the SFTP initialism
	caserIntialisms := map[string]bool{"SFTP": true}
	caser := strcase.NewCaser(true, caserIntialisms, nil)

	m := make(map[string]TableMetadata)
	for _, r := range result {
		// Convert to Go pascal to match struct type names
		tableName := caser.ToPascal(r.TableName)

		table, ok := m[tableName]
		if !ok || table.Columns == nil {
			table = TableMetadata{
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
		if _, ok := spec.Type.(*ast.StructType); !ok {
			continue
		}

		structs = append(structs, k)
	}

	sort.Slice(structs, func(i, j int) bool {
		return structs[i] < structs[j]
	})

	return structs, nil
}

func (c *GenerationClient) templateFuncs() map[string]any {
	templateFuncs := map[string]any{
		"Pluralize": c.pluralizer.Plural,
		"GoCamel":   strcase.ToGoCamel,
		"Camel":     strcase.ToCamel,
		"IsPtr": func(fieldType string) bool {
			return strings.HasPrefix(fieldType, "*")
		},
		"TrimPtr": func(fieldType string) string {
			return strings.TrimPrefix(fieldType, "*")
		},
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

			val := strcase.ToCamel(field.Name)
			if !field.IsPrimaryKey && !isPatch {
				val += ",omitempty"
			}

			return val
		},
	}

	return templateFuncs
}

func fieldType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		switch {
		case slices.Contains(baseTypes, t.Name):
			return t.Name
		default:
			return fmt.Sprintf("resources.%s", t.Name)
		}
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", t.X, t.Sel.Name)
	case *ast.StarExpr:
		return "*" + fieldType(t.X)
	case *ast.ArrayType:
		return "[]" + fieldType(t.Elt)
	default:
		panic(fmt.Sprintf("unknown type: %T", t))
	}
}
