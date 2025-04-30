package generation

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/commentlang"
	"github.com/go-playground/errors/v5"
)

type schemaGenerator struct {
	resourceDestination string
	schemaDestination   string
	loadPackages        []string
	structs             []parser.Struct
	schemaResources     map[string]*schemaResource
}

func NewSchemaGenerator(resourceFilePath, schemaDestinationPath string) (Generator, error) {
	s := &schemaGenerator{
		resourceDestination: filepath.Dir(resourceFilePath),
		schemaDestination:   schemaDestinationPath,
		loadPackages:        []string{resourceFilePath},
	}

	return s, nil
}

func (s *schemaGenerator) Generate() error {
	packageMap, err := parser.LoadPackages(s.loadPackages...)
	if err != nil {
		return err
	}

	pStructs, err := parser.ParseStructs(packageMap["resources"])
	if err != nil {
		return err
	}

	s.structs = pStructs

	if err := s.structsToSchema(); err != nil {
		return err
	}

	if err := s.generateSchemaMigrations(); err != nil {
		return err
	}

	if err := s.generateMutations(); err != nil {
		return err
	}

	return nil
}

func (s *schemaGenerator) structsToSchema() error {
	s.schemaResources = make(map[string]*schemaResource, len(s.structs))
	for i := range s.structs {
		res, err := structToSchemaResource(&s.structs[i])
		if err != nil {
			return err
		}

		s.schemaResources[s.structs[i].Name()] = res
	}

	return nil
}

func (s *schemaGenerator) generateSchemaMigrations() error {
	// TODO:
	// - write to file
	return nil
}

func (s *schemaGenerator) generateMutations() error {
	return nil
}

func (schemaGenerator) Close() {}

type schemaIndex struct {
	Name      string
	indexType indexType
	Argument  string
}

type indexType string

const (
	uniqueIndexType indexType = "UNIQUE"
	searchIndexType indexType = "SEARCH"
)

type foreignKeyConstraint struct {
	sourceExpression    string // the column(s) the constraint is on
	referenceExpression string // the table & column(s) the constraint references
}

type checkConstraint struct {
	Name       string
	Expression string
}

type schemaResource struct {
	Name         string
	PrimaryKey   string
	ForeignKeys  []foreignKeyConstraint
	Columns      []schemaColumn
	Checks       []checkConstraint
	SearchTokens []searchExpression
	Indexes      []schemaIndex
}

type schemaColumn struct {
	Name         string
	SQLType      string
	DefaultValue *string
	IsNullable   bool
	IsHidden     bool
}

func structToSchemaResource(pStruct *parser.Struct) (*schemaResource, error) {
	res := &schemaResource{
		Columns: make([]schemaColumn, 0, pStruct.NumFields()),
	}

	for _, f := range pStruct.Fields() {
		res.Columns = append(res.Columns, schemaColumn{
			Name:       f.Name(),
			SQLType:    sqlTypeFromField(f),
			IsNullable: isTypeNullable(f),
		})
	}

	structComments, err := commentlang.Scan(pStruct.Comments(), commentlang.ScanStruct)
	if err != nil {
		return nil, errors.Wrap(err, "commentlang.Scan()")
	}
	// TODO: validate struct comments
	_ = structComments

	for i, field := range pStruct.Fields() {
		fieldComments, err := commentlang.Scan(field.Comments(), commentlang.ScanField)
		if err != nil {
			return nil, errors.Wrap(err, "commentlang.Scan()")
		}

		for keyword, args := range fieldComments {
			switch keyword {
			case commentlang.PrimaryKey:
				if res.PrimaryKey != "" {
					return nil, errors.Newf("cannot use @primarykey on multiple fields: %[1]s and %[2]s\nhint: use `@primarykey (%[1]s, %[2]s)` on struct", res.PrimaryKey, field.Name())
				}

				res.PrimaryKey = field.Name()

			case commentlang.ForeignKey:
				for _, arg := range args {
					sourceExpression := field.Name()
					referenceExpression := arg.Arg1

					res.ForeignKeys = append(res.ForeignKeys, foreignKeyConstraint{sourceExpression, referenceExpression})
				}

			case commentlang.Default:
				// TODO: consider something less ugly than "args[0].Arguments()[0]"
				defaultValue := args[0].Arg1
				res.Columns[i].DefaultValue = &defaultValue

			case commentlang.Hidden:
				res.Columns[i].IsHidden = true

			case commentlang.Check:
				checkArg := args[0].Arg1
				res.Checks = append(res.Checks, checkConstraint{field.Name(), checkArg})

			case commentlang.Substring, commentlang.Fulltext, commentlang.Ngram:
				for _, arg := range args {
					argument := arg.Arg1
					res.SearchTokens = append(res.SearchTokens, searchExpression{resource.FilterType(keyword.String()), argument})
				}

			case commentlang.UniqueIndex:
				res.Indexes = append(res.Indexes, schemaIndex{Name: field.Name(), indexType: uniqueIndexType})

			case commentlang.Using:
				// TODO: validate that the struct is a view, because this is a view-only keyword
				usingName := args[0].Arg1
				res.Columns[i].Name = usingName

			default:
				return nil, errors.Newf("%s keyword not yet implemented in generator", keyword.String())
			}
		}
	}

	return res, nil
}

func isTypeNullable(f parser.Field) bool {
	if f.IsPointer() {
		return true
	}

	return strings.Contains(strings.ToLower(f.UnqualifiedTypeName()), "null")
}

func sqlTypeFromField(f parser.Field) string {
	switch f.UnqualifiedTypeName() {
	case "string":
		return "STRING(MAX)"
	case "bool", "IntToBool", "CharToBool":
		return "BOOL"
	case "IntToUUID":
		return "STRING(36)"
	case "int":
		return "INT64"
	default:
		panic(fmt.Sprintf("unknown fieldtype %q", f.UnqualifiedTypeName()))
	}
}

// TODO: move these elsewhere
const (
	migrationHeaderComment   = `-- GENERATED BY SCHEMA GEN. DO NOT EDIT.`
	tableMigrationUpTemplate = `{{ .MigrationHeaderComment }}
CREATE TABLE {{ .Resource.Name }} (
  {{- range $column := .Resource.Columns }}
  {{ $column.Name }} {{ $column.SQLType }} {{ if not $column.IsNullable }}NOT NULL{{ end }} {{ if $column.DefaultValue }}DEFAULT ({{ $column.DefaultValue }}){{ end }}{{ if $column.IsHidden }}HIDDEN{{ end }},
  {{- end }}

  {{ if .Resource.SearchTokens -}}
  SearchTokens TOKENLIST AS (
    TOKENLIST_CONCAT([
    {{- range $index, $searchToken := .Resource.SearchTokens }}
        {{ if $index }},{{ end }}({{ $searchToken.Name }}({{ $searchToken.Arg }}))
    {{- end }}
    ])
  ) HIDDEN,
  {{- end }}

  {{ range $constraint := .Resource.Constraints -}}
  CONSTRAINT {{ $constraint }},
  {{- end }}
) PRIMARY KEY ({{ .Resource.PrimaryKey }});

{{ range $index := .Resource.Indexes -}}
CREATE {{ $index.Type }} INDEX {{ $index.Name }} ON {{ .Resource.Name }}({{ $index.Argument }});
{{- end }}
`
	tableMigrationDownTemplate = `{{ .MigrationHeaderComment }}
{{ range $index := .Resource.Indexes -}}
DROP INDEX {{ $index.Name }};
{{- end }}
DROP TABLE {{ .Resource.Name }};
`

	viewMigrationUpTemplate = `{{ .MigrationHeaderComment }}
CREATE VIEW {{ .Resource.Name }}
SQL SECURITY INVOKER
AS 
SELECT
  {{- range $column := .Resource.Columns }}
  {{ $column.SourceTable }}.{{ $column.Name }},
  {{- end }}
{{ .Resource.Query }}
`
	viewMigrationDownTemplate = `{{ .MigrationHeaderComment }}
DROP VIEW {{ .View.Name }};
`
)
