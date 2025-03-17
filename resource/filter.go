package resource

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type FilterType string

const (
	Index     FilterType = "index"
	SubString FilterType = "substring"
	FullText  FilterType = "fulltext"
	Ngram     FilterType = "ngram"
)

type Filter struct {
	typ    FilterType
	values map[FilterKey]string
	kinds  map[FilterKey]reflect.Kind
}

func NewFilter(typ FilterType, values map[FilterKey]string, kinds map[FilterKey]reflect.Kind) *Filter {
	return &Filter{
		typ:    typ,
		values: values,
		kinds:  kinds,
	}
}

func (f Filter) SpannerStmt() (Statement, error) {
	switch f.typ {
	case Index:
		return f.parseToIndexFilter()
	default:
		return Statement{}, errors.Newf("unsupported filter type %s", f.typ)
	}
}

func (f Filter) parseToIndexFilter() (Statement, error) {
	fragments := make([]string, 0, len(f.values))
	params := make(map[string]any)

	for column, query := range f.values {
		terms := strings.Split(query, "|")

		exprs := make([]string, 0, len(terms))
		for i, term := range terms {
			param := fmt.Sprintf("indexfilterterm%s%d", column, i)

			switch k := f.kinds[column]; k {
			case reflect.Int, reflect.Int16, reflect.Int32, reflect.Int64:
				typed, err := strconv.Atoi(term)
				if err != nil {
					return Statement{}, httpio.NewBadRequestMessageWithErrorf(errors.Wrap(err, "strconv.Atoi()"), "unable to convert %s to an int kind", term)
				}
				params[param] = typed

			case reflect.String:
				params[param] = term

			default:
				return Statement{}, errors.Newf("unsupported kind, %s", k.String())
			}

			exprs = append(exprs, fmt.Sprintf("(%s = @%s)", column, param))
		}
		fragment := strings.Join(exprs, " OR ")
		fragments = append(fragments, fragment)
	}

	sql := strings.Join(fragments, " AND ")

	return Statement{
		Sql:    sql,
		Params: params,
	}, nil
}

// func (s Filter) parseToSearchSubstring(tokenlist FilterKey) *Statement {
// 	terms := strings.Split(s.query, " ")

// 	exprs := make([]string, 0, len(terms))
// 	params := make(map[string]any, len(terms))
// 	for i, term := range terms {
// 		param := fmt.Sprintf("searchsubstringterm%d", i)

// 		params[param] = term

// 		exprs = append(exprs, fmt.Sprintf("SEARCH_SUBSTRING(%s, @%s)", tokenlist, param))
// 	}
// 	sql := strings.Join(exprs, " OR ")

// 	return &Statement{
// 		Sql:    sql,
// 		Params: params,
// 	}
// }

// func (s Filter) parseToNgramScore(tokenlist FilterKey) *Statement {
// 	terms := strings.Split(s.query, " ")

// 	exprs := make([]string, 0, len(terms))
// 	params := make(map[string]any, len(terms))
// 	for i, term := range terms {
// 		param := fmt.Sprintf("ngramscoreterm%d", i)
// 		params[param] = term

// 		exprs = append(exprs, fmt.Sprintf("SCORE_NGRAMS(%s, @%s)", tokenlist, param))
// 	}
// 	sql := strings.Join(exprs, " + ")

// 	return &Statement{
// 		Sql:    sql,
// 		Params: params,
// 	}
// }

type FilterKey string

func (f FilterKey) String() string {
	return string(f)
}
