package generation

import (
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/commentlang"
	"github.com/go-playground/errors/v5"
)

type schemaGenerator struct {
	resourceDestination string
	schemaDestination   string
	loadPackages        []string
	structs             []*parser.Struct
	schemaResources     map[string]*schemaResource
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
	sourceExpression    string // the column(s) the constraint is on
	referenceExpression string // the table & column(s) the constraint references
}

type checkConstraint struct {
	Name       string
	Expression string
}

type schemaColumn struct {
	Name         string
	SQLType      string
	DefaultValue *string
	IsNullable   bool
	IsHidden     bool
	SourceTable  *string // only non-nil when parent schemaResource is a View
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

func (s *schemaResource) addStructComments(pStruct *parser.Struct) error {
	structComments, err := commentlang.ScanStruct(pStruct.Comments())
	if err != nil {
		return errors.Wrapf(err, "%s commentlang.ScanStruct()", pStruct.Error())
	}

	if _, ok := structComments[commentlang.View]; ok {
		s.IsView = true
	}

	for keyword, args := range structComments {
		if s.IsView { // View-only keywords go in their own switch so we can error by default on anything else
			switch keyword {
			case commentlang.Query:
				s.Query = &args[0].Arg1

			case commentlang.View:
				continue

			default:
				return errors.Newf("%s keyword not supported because resource %q is a view", keyword.String(), s.Name)
			}

			continue
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

		case commentlang.UniqueIndex:
			for _, arg := range args {
				uniqueIndexArg := arg.Arg1

				s.Indexes = append(s.Indexes, schemaIndex{Name: uniqueIndexArg, indexType: uniqueIndexType})
			}

		default:
			return errors.Newf("struct keyword %s not yet implemented in generator", keyword.String())
		}
	}

	return nil
}

func (s *schemaResource) addFieldComments(pStruct *parser.Struct) error {
	for i, field := range pStruct.Fields() {
		fieldComments, err := commentlang.ScanField(field.Comments())
		if err != nil {
			return errors.Wrap(err, "commentlang.ScanField()")
		}

		for keyword, args := range fieldComments {
			if s.IsView { // View-only keywords go in their own switch so we can error by default on anything else
				switch keyword {
				case commentlang.Using:
					usingName := args[0].Arg1
					s.Columns[i].Name = usingName

				default:
					return errors.Newf("%s keyword not supported because resource %q is a view", keyword.String(), s.Name)
				}

				continue
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
				return errors.Newf("field keyword %s not yet implemented in generator", keyword.String())
			}
		}
	}

	return nil
}
