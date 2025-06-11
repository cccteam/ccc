package generation

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/graph"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

const (
	migrationSuffixUp   string = "up.sql"
	migrationSuffixDown string = "down.sql"
)

type schemaGenerator struct {
	resourceDestination  string
	schemaDestination    string
	resourceFilePath     string
	datamigrationPath    string
	migrationIndexOffset int
	packageName          string
	appName              string
	schemaGraph          graph.Graph[*schemaTable]
	fileWriter
}

type schemaIndex struct {
	Name     string
	Type     indexType
	Argument string
}

type indexType string

const (
	normalIndexType indexType = ""
	uniqueIndexType indexType = "UNIQUE"
	searchIndexType indexType = "SEARCH"
)

type foreignKeyConstraint struct {
	sourceExpression  string // the column(s) the constraint is on
	referencedTable   string
	referencedColumns string
}

type checkConstraint struct {
	Name       string
	Expression string
}

type schema struct {
	tables []*schemaTable
	views  []*schemaView
}
type conversionFlag int

const (
	noConversion conversionFlag = 0
	custom       conversionFlag = 1 << iota
	pointer
	fromInt
	fromString
	toInt
	toString
	toBool
	toUUID
)

type tableColumn struct {
	Table            *schemaTable
	Name             string
	SQLType          string
	DefaultValue     *string
	IsNullable       bool
	IsHidden         bool
	conversionMethod conversionFlag
}

func (t tableColumn) GoName() string {
	s, ok := strings.CutSuffix(t.Name, "Id")
	if ok {
		s += "ID"
	}

	return s
}

// template use only
func (t tableColumn) IsPrimaryKey() bool {
	return t.Name == t.Table.PrimaryKey
}

// template use only
func (t tableColumn) HasConversion() bool {
	return t.conversionMethod > 0
}

// template use only
func (t tableColumn) NeedsConversionMethod() bool {
	return t.conversionMethod > custom
}

func (t tableColumn) ConversionReturnType() string {
	switch {
	case t.conversionMethod&toInt != 0:
		return "int64"
	case t.conversionMethod&toString != 0, t.conversionMethod&toUUID != 0:
		if t.conversionMethod&pointer != 0 {
			return "*string"
		}
		return "string"
	case t.conversionMethod&toBool != 0:
		return "bool"
	default:
		panic(fmt.Sprintf("conversionReturnType not implemented for %s", t.Name))
	}
}

func (t tableColumn) conversionRefTable() string {
	for _, fk := range t.Table.ForeignKeys {
		if strings.Contains(fk.sourceExpression, t.Name) {
			return fk.referencedTable
		}
	}

	return ""
}

func (t tableColumn) ConversionMethod() string {
	tmpl := template.Must(template.New("ConversionMethod").Parse(conversionTemplateMap[t.conversionMethod]))

	// TODO(jrowland): find a way to pass all possible template parameters from this tableColumn method
	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, map[string]any{
		"RefTableName": t.conversionRefTable(),
		"Column":       t,
	}); err != nil {
		panic(errors.Wrap(err, "template.Template.Execute()"))
	}

	return buf.String()
}

type schemaTable struct {
	Name             string
	Columns          []*tableColumn
	PrimaryKey       string
	ForeignKeys      []foreignKeyConstraint
	Checks           []checkConstraint
	SearchTokens     []searchExpression
	Indexes          []schemaIndex
	HasConvertMethod bool
	HasFilterMethod  bool
	Query            *string // TODO: remove query and use method on struct instead
}

func (s schemaTable) Constraints() []string {
	constraints := make([]string, 0, len(s.ForeignKeys)+len(s.Checks))

	for _, ck := range s.Checks {
		constraint := fmt.Sprintf("CK_%s_%s CHECK (%s)", s.Name, ck.Name, ck.Expression)

		constraints = append(constraints, constraint)
	}

	for _, fk := range s.ForeignKeys {
		// The source expression is a comma-delimited list of column names
		// We want to convert `Id, Type` to `Id_Type`
		columnNames := strings.ReplaceAll(fk.sourceExpression, " ", "")
		columnNames = strings.ReplaceAll(columnNames, ",", "_")

		constraint := fmt.Sprintf("FK_%s_%s FOREIGN KEY (%s) REFERENCES %s(%s)", s.Name, columnNames, fk.sourceExpression, fk.referencedTable, fk.referencedColumns)

		constraints = append(constraints, constraint)
	}

	return constraints
}

func (s *schemaTable) resolveStructComments(comments map[genlang.Keyword][]*genlang.Args) error {
	for keyword, args := range comments {
		switch keyword {
		case genlang.PrimaryKey:
			s.PrimaryKey = args[0].Arg1

		case genlang.ForeignKey:
			for _, arg := range args {
				// TODO(jrowland): consider validating column names actually exist in this struct
				// columnNames := strings.Split(strings.ReplaceAll(arg.Arg1, " ", ""), ",")
				sourceExpression := arg.Arg1

				if arg.Arg2 == nil { // TODO(jrowland): move validation into scanner
					return errors.Newf("expected second argument for foreignkey on struct %q", s.Name)
				}

				refTable, refColumns := parseReferenceExpression(*arg.Arg2)

				s.ForeignKeys = append(s.ForeignKeys, foreignKeyConstraint{sourceExpression, refTable, refColumns})
			}

		case genlang.Index:
			for _, arg := range args {
				indexArg := strings.ReplaceAll(arg.Arg1, " ", "")
				indexArg = strings.ReplaceAll(indexArg, ",", "And")

				name := fmt.Sprintf("%sBy%s", s.Name, indexArg)

				s.Indexes = append(s.Indexes, schemaIndex{Name: name, Type: normalIndexType, Argument: arg.Arg1})
			}

		case genlang.UniqueIndex:
			for _, arg := range args {
				uniqueIndexArg := strings.ReplaceAll(arg.Arg1, " ", "")
				uniqueIndexArg = strings.ReplaceAll(uniqueIndexArg, ",", "And")

				name := fmt.Sprintf("%sBy%s", s.Name, uniqueIndexArg)

				s.Indexes = append(s.Indexes, schemaIndex{Name: name, Type: uniqueIndexType, Argument: arg.Arg1})
			}

		default:
			return errors.Newf("keyword %s not yet implemented for schemaTable", keyword.String())
		}
	}

	return nil
}

func (s *schemaTable) resolveFieldComment(column tableColumn, comment map[genlang.Keyword][]*genlang.Args) (tableColumn, error) {
	for keyword, args := range comment {
		switch keyword {
		case genlang.PrimaryKey:
			if s.PrimaryKey != "" { // TODO: do all error checking in Scanner instead of here (redundant)
				return tableColumn{}, errors.New("@primarykey used twice")
			}

			if column.SQLType == "STRING(36)" {
				checkArg := fmt.Sprintf(migrationCheckUUID, column.Name)
				s.Checks = append(s.Checks, checkConstraint{column.Name, checkArg})
			}

			s.PrimaryKey = column.Name

		case genlang.ForeignKey:
			for _, arg := range args {
				sourceExpression := column.Name
				refTable, refColumns := parseReferenceExpression(arg.Arg1)

				s.ForeignKeys = append(s.ForeignKeys, foreignKeyConstraint{sourceExpression, refTable, refColumns})
			}

		case genlang.Default:
			defaultValue := args[0].Arg1
			column.DefaultValue = &defaultValue

		case genlang.Hidden:
			column.IsHidden = true

		case genlang.Check:
			checkArg := args[0].Arg1
			checkArg = strings.ReplaceAll(checkArg, "@self", column.Name)

			s.Checks = append(s.Checks, checkConstraint{column.Name, checkArg})

		case genlang.Substring, genlang.Fulltext, genlang.Ngram:
			for _, arg := range args {
				argument := strings.ReplaceAll(arg.Arg1, "@self", column.Name)
				s.SearchTokens = append(s.SearchTokens, searchExpression{resource.FilterType(keyword.String()), argument})
			}

		case genlang.Index:
			name := fmt.Sprintf("%sBy%s", s.Name, column.Name)

			s.Indexes = append(s.Indexes, schemaIndex{Name: name, Type: normalIndexType, Argument: column.Name})

		case genlang.UniqueIndex:
			name := fmt.Sprintf("%sBy%s", s.Name, column.Name)

			s.Indexes = append(s.Indexes, schemaIndex{Name: name, Type: uniqueIndexType, Argument: column.Name})

		default:
			return tableColumn{}, errors.Newf("field keyword %s not yet implemented for schemaColumn", keyword.String())
		}
	}

	return column, nil
}

type viewColumn struct {
	Name         string
	SourceColumn string
	SourceTable  string
}

type schemaView struct {
	Name    string
	Columns []*viewColumn
	Query   string
}

func (s *schemaView) resolveStructComments(comments map[genlang.Keyword][]*genlang.Args) error {
	for keyword, args := range comments {
		switch keyword {
		case genlang.Query:
			s.Query = args[0].Arg1

		case genlang.View:
			continue

		default:
			return errors.Newf("%s keyword not yet implemented for schemaView", keyword.String())
		}
	}

	return nil
}

func newViewColumn(field *parser.Field) (viewColumn, error) {
	comment, err := genlang.ScanField(field.Comments())
	if err != nil {
		return viewColumn{}, errors.Wrap(err, "commentlang.ScanField()")
	}

	column := viewColumn{
		Name:        field.Name(),
		SourceTable: field.TypeArgs(),
	}

	for keyword, args := range comment {
		switch keyword {
		case genlang.Using:
			usingName := args[0].Arg1
			column.SourceColumn = usingName

		default:
			return viewColumn{}, errors.Newf("%s keyword not yet implemented for schemaView", keyword.String())
		}
	}

	return column, nil
}

// Takes a string of the form `TableName(column1, column2)` and returns
// the identifiers as `TableName“ and `column1, column2`
func parseReferenceExpression(arg string) (table string, columns string) {
	var i int
tableNameLoop:
	for i < len(arg) {
		switch arg[i] {
		case ' ':
			i += 1

		case '(':
			i += 1
			break tableNameLoop

		default:
			table += string(arg[i])
			i += 1
		}
	}

columnsLoop:
	for i < len(arg) {
		switch arg[i] {
		case ')':
			break columnsLoop
		default:
			columns += string(arg[i])
			i += 1
		}
	}

	columns = strings.ReplaceAll(columns, " ", "")
	columns = strings.ReplaceAll(columns, ",", ", ")

	return table, columns
}
