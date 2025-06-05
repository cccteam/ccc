package resource

import (
	"fmt"
	"iter"
	"maps"
	"reflect"
	"strconv"
	"strings"

	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type FilterKey string

func (f FilterKey) String() string {
	return string(f)
}

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
		statement, err := f.parseToIndexFilter()
		if err != nil {
			return Statement{}, err
		}

		statement.Sql = fmt.Sprintf("WHERE %s", statement.Sql)

		return statement, nil
	case SubString:
		searchStatement, err := f.parseToSearchSubstring()
		if err != nil {
			return Statement{}, err
		}

		scoreStatement, err := f.parseToNgramScore()
		if err != nil {
			return Statement{}, err
		}

		sql := fmt.Sprintf("WHERE %s\nORDER BY %s", searchStatement.Sql, scoreStatement.Sql)
		params := make(map[string]any, len(searchStatement.Params)+len(scoreStatement.Params))

		maps.Insert(params, maps.All(searchStatement.Params))
		maps.Insert(params, maps.All(scoreStatement.Params))

		return Statement{Sql: sql, Params: params}, nil
	case FullText, Ngram:
		return Statement{}, errors.Newf("%s filter is not yet implemented", f.typ)
	default:
		return Statement{}, errors.Newf("%s filter type not supported", f.typ)
	}
}

func (f Filter) parseToIndexFilter() (Statement, error) {
	fragments := make([]string, 0, len(f.values))
	params := make(map[string]any)

	for column, query := range f.values {
		terms := strings.Split(query, "|")

		exprs := make([]string, 0, len(terms))
		for i, term := range terms {
			if term == "_ISNULL_" {
				exprs = append(exprs, fmt.Sprintf("(%s IS NULL)", column))
				continue
			} else if term == "_ISNOTNULL_" {
				exprs = append(exprs, fmt.Sprintf("(%s IS NOT NULL)", column))
				continue
			}

			param := fmt.Sprintf("indexfilterterm%s%d", column, i)
			switch k := f.kinds[column]; k {
			case reflect.Int, reflect.Int16, reflect.Int32, reflect.Int64:
				typed, err := strconv.Atoi(term)
				if err != nil {
					return Statement{}, httpio.NewBadRequestMessageWithErrorf(errors.Wrap(err, "strconv.Atoi()"), "unable to convert %s to an int kind", term)
				}
				params[param] = typed

			case reflect.String, reflect.Struct:
				params[param] = term

			case reflect.Bool:
				typed, err := strconv.ParseBool(term)
				if err != nil {
					return Statement{}, httpio.NewBadRequestMessageWithErrorf(errors.Wrap(err, "strconv.ParseBool()"), "unable to convert %s to a bool kind", term)
				}
				params[param] = typed

			default:
				return Statement{}, errors.Newf("unsupported kind, %s", k.String())
			}

			exprs = append(exprs, fmt.Sprintf("(%s = @%s)", column, param))
		}
		fragment := strings.Join(exprs, " OR ")
		if len(terms) > 1 {
			fragment = fmt.Sprintf("(%s)", fragment)
		}
		fragments = append(fragments, fragment)
	}

	sql := strings.Join(fragments, " AND ")

	return Statement{
		Sql:    sql,
		Params: params,
	}, nil
}

func (f Filter) parseToSearchSubstring() (Statement, error) {
	next, stop := iter.Pull(maps.Keys(f.values))
	tokenlist, foundOne := next()
	if _, foundTwo := next(); !foundOne || foundTwo {
		stop()

		return Statement{}, errors.Newf("expected a single key value pair, got %d", len(f.values))
	}
	terms := strings.Split(f.values[tokenlist], " ")

	exprs := make([]string, 0, len(terms))
	params := make(map[string]any, len(terms))
	for i, term := range terms {
		param := fmt.Sprintf("searchsubstringterm%d", i)

		params[param] = term

		exprs = append(exprs, fmt.Sprintf("SEARCH_SUBSTRING(%s, @%s)", tokenlist, param))
	}
	sql := strings.Join(exprs, " OR ")

	return Statement{
		Sql:    sql,
		Params: params,
	}, nil
}

func (f Filter) parseToNgramScore() (Statement, error) {
	next, stop := iter.Pull(maps.Keys(f.values))
	tokenlist, foundOne := next()
	if _, foundTwo := next(); !foundOne || foundTwo {
		stop()

		return Statement{}, errors.Newf("expected a single key value pair, got %d", len(f.values))
	}
	terms := strings.Split(f.values[tokenlist], " ")

	exprs := make([]string, 0, len(terms))
	params := make(map[string]any, len(terms))
	for i, term := range terms {
		param := fmt.Sprintf("ngramscoreterm%d", i)
		params[param] = term

		exprs = append(exprs, fmt.Sprintf("SCORE_NGRAMS(%s, @%s)", tokenlist, param))
	}
	sql := strings.Join(exprs, " + ")

	return Statement{
		Sql:    sql,
		Params: params,
	}, nil
}
