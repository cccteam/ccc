package resource

import (
	"fmt"
	"strings"
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
}

func NewFilter(typ FilterType, values map[FilterKey]string) *Filter {
	return &Filter{
		typ:    typ,
		values: values,
	}
}

func (f Filter) SpannerStmt() Statement {
	switch f.typ {
	case Index:
		return f.parseToIndexFilter()
	}

	return Statement{}
}

func (f Filter) parseToIndexFilter() Statement {
	fragments := make([]string, 0, len(f.values))
	params := make(map[string]any)

	for column, query := range f.values {
		terms := strings.Split(query, "|")

		exprs := make([]string, 0, len(terms))
		for i, term := range terms {
			param := fmt.Sprintf("indexfilterterm%s%d", column, i)

			params[param] = term

			exprs = append(exprs, fmt.Sprintf("(%s = @%s)", column, param))
		}
		fragment := strings.Join(exprs, " OR ")
		fragments = append(fragments, fragment)
	}

	sql := strings.Join(fragments, " AND ")

	fmt.Println(sql)
	fmt.Println(f.values)

	return Statement{
		Sql:    sql,
		Params: params,
	}
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
