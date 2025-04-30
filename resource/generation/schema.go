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
	Columns      []schemaColumn
	PrimaryKey   string
	ForeignKeys  []foreignKeyConstraint
	Checks       []checkConstraint
	SearchTokens []searchExpression
	Indexes      []schemaIndex
	IsView       bool
	Query        *string
}

type schemaColumn struct {
	Name         string
	SQLType      string
	DefaultValue *string
	IsNullable   bool
	IsHidden     bool
	SourceTable  *string // only non-nil when parent schemaResource is a View
}

func (s *schemaResource) addStructComments(pStruct *parser.Struct) error {
	structComments, err := commentlang.Scan(pStruct.Comments(), commentlang.ScanStruct)
	if err != nil {
		return errors.Wrap(err, "commentlang.Scan()")
	}

	if _, ok := structComments[commentlang.View]; ok {
		s.IsView = true
	}

	for keyword, args := range structComments {
		if s.IsView {
			switch keyword {
			case commentlang.Query:
				s.Query = &args[0].Arg1
			case commentlang.View:
				continue
			default:
				return errors.Newf("%s keyword not supported because resource %q is a view", keyword.String(), s.Name)
			}
		}
		switch keyword {
		case commentlang.PrimaryKey:
			s.PrimaryKey = args[0].Arg1

		case commentlang.ForeignKey:
			for _, arg := range args {
				sourceExpression := arg.Arg1
				if arg.Arg2 == nil {
					return errors.Newf("expected second argument for foreignkey on struct %q", s.Name)
				}
				referenceExpression := *arg.Arg2

				s.ForeignKeys = append(s.ForeignKeys, foreignKeyConstraint{sourceExpression, referenceExpression})
			}
		}
	}

	return nil
}

func (s *schemaResource) addFieldComments(pStruct *parser.Struct) error {
	for i, field := range pStruct.Fields() {
		fieldComments, err := commentlang.Scan(field.Comments(), commentlang.ScanField)
		if err != nil {
			return errors.Wrap(err, "commentlang.Scan()")
		}

		for keyword, args := range fieldComments {
			// View-only keywords go in their own switch so we can error out by default
			if s.IsView {
				switch keyword {
				case commentlang.Using:
					usingName := args[0].Arg1
					s.Columns[i].Name = usingName

				default:
					return errors.Newf("%s keyword not supported because resource %q is a view", keyword.String(), s.Name)
				}
			}

			switch keyword {
			case commentlang.PrimaryKey:
				if s.PrimaryKey != "" {
					return errors.Newf("cannot use @primarykey on field %q and struct, or on multiple fields", field.Name())
				}

				s.PrimaryKey = field.Name()

			case commentlang.ForeignKey:
				for _, arg := range args {
					sourceExpression := field.Name()
					referenceExpression := arg.Arg1

					s.ForeignKeys = append(s.ForeignKeys, foreignKeyConstraint{sourceExpression, referenceExpression})
				}

			case commentlang.Default:
				defaultValue := args[0].Arg1
				s.Columns[i].DefaultValue = &defaultValue

			case commentlang.Hidden:
				s.Columns[i].IsHidden = true

			case commentlang.Check:
				checkArg := args[0].Arg1
				s.Checks = append(s.Checks, checkConstraint{field.Name(), checkArg})

			case commentlang.Substring, commentlang.Fulltext, commentlang.Ngram:
				for _, arg := range args {
					argument := arg.Arg1
					s.SearchTokens = append(s.SearchTokens, searchExpression{resource.FilterType(keyword.String()), argument})
				}

			case commentlang.UniqueIndex:
				s.Indexes = append(s.Indexes, schemaIndex{Name: field.Name(), indexType: uniqueIndexType})

			default:
				return errors.Newf("%s keyword not yet implemented in generator", keyword.String())
			}
		}
	}

	return nil
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

	if err := res.addStructComments(pStruct); err != nil {
		return nil, err
	}

	if err := res.addFieldComments(pStruct); err != nil {
		return nil, err
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
