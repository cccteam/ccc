package generation

import (
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
}

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
	sourceExpression  string // the column(s) the constraint is on
	referencedTable   string
	referencedColumns []string
}

type checkConstraint struct {
	Name       string
	Expression string
}

type schema struct {
	tables []*schemaTable
	views  []*schemaView
}

type tableColumn struct {
	Name         string
	SQLType      string
	DefaultValue *string
	IsNullable   bool
	IsHidden     bool
}

type schemaTable struct {
	Name         string
	Columns      []*tableColumn
	PrimaryKey   string
	ForeignKeys  []foreignKeyConstraint
	Checks       []checkConstraint
	SearchTokens []searchExpression
	Indexes      []schemaIndex
	Query        *string
}

func (s *schemaTable) resolveStructComments(comments map[commentlang.Keyword][]*commentlang.KeywordArguments) error {
	for keyword, args := range comments {
		switch keyword {
		case commentlang.PrimaryKey:
			s.PrimaryKey = args[0].Arg1

		case commentlang.ForeignKey:
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

		case commentlang.UniqueIndex:
			for _, arg := range args {
				uniqueIndexArg := arg.Arg1

				s.Indexes = append(s.Indexes, schemaIndex{Name: uniqueIndexArg, indexType: uniqueIndexType})
			}

		default:
			return errors.Newf("keyword %s not yet implemented for schemaTable", keyword.String())
		}
	}

	return nil
}

func (s *schemaTable) resolveFieldComment(column tableColumn, comment map[commentlang.Keyword][]*commentlang.KeywordArguments) (tableColumn, error) {
	for keyword, args := range comment {
		switch keyword {
		case commentlang.PrimaryKey:
			if s.PrimaryKey != "" { // TODO: do all error checking in Scanner instead of here (redundant)
				return tableColumn{}, errors.New("@primarykey used twice")
			}

			s.PrimaryKey = column.Name

		case commentlang.ForeignKey:
			for _, arg := range args {
				sourceExpression := column.Name
				refTable, refColumns := parseReferenceExpression(arg.Arg1)

				s.ForeignKeys = append(s.ForeignKeys, foreignKeyConstraint{sourceExpression, refTable, refColumns})
			}

		case commentlang.Default:
			defaultValue := args[0].Arg1
			column.DefaultValue = &defaultValue

		case commentlang.Hidden:
			column.IsHidden = true

		case commentlang.Check:
			checkArg := args[0].Arg1
			s.Checks = append(s.Checks, checkConstraint{column.Name, checkArg})

		case commentlang.Substring, commentlang.Fulltext, commentlang.Ngram:
			for _, arg := range args {
				argument := arg.Arg1
				s.SearchTokens = append(s.SearchTokens, searchExpression{resource.FilterType(keyword.String()), argument})
			}

		case commentlang.UniqueIndex:
			s.Indexes = append(s.Indexes, schemaIndex{Name: column.Name, indexType: uniqueIndexType})

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

func (s *schemaView) resolveStructComments(comments map[commentlang.Keyword][]*commentlang.KeywordArguments) error {
	for keyword, args := range comments {
		switch keyword {
		case commentlang.Query:
			s.Query = args[0].Arg1

		case commentlang.View:
			continue

		default:
			return errors.Newf("%s keyword not yet implemented for schemaView", keyword.String())
		}
	}

	return nil
}

func newViewColumn(field *parser.Field) (viewColumn, error) {
	comment, err := commentlang.ScanField(field.Comments())
	if err != nil {
		return viewColumn{}, errors.Wrap(err, "commentlang.ScanField()")
	}

	column := viewColumn{
		Name:        field.Name(),
		SourceTable: field.TypeArgs(),
	}

	for keyword, args := range comment {
		switch keyword {
		case commentlang.Using:
			usingName := args[0].Arg1
			column.SourceColumn = usingName

		default:
			return viewColumn{}, errors.Newf("%s keyword not yet implemented for schemaView", keyword.String())
		}
	}

	return column, nil
}

// Takes a string of the form `TableName(column1, column2)` and returns
// the identifiers as table and columns
func parseReferenceExpression(arg string) (table string, columns []string) {
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

	var cols string
columnsLoop:
	for i < len(arg) {
		switch arg[i] {
		case ')':
			break columnsLoop
		default:
			cols += string(arg[i])
			i += 1
		}
	}

	cols = strings.ReplaceAll(cols, " ", "")
	columns = strings.Split(cols, ",")

	return table, columns
}
