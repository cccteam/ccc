package resource

import (
	"fmt"
	"strings"
)

type spannerQueryParser struct {
	query string
}

func parseSpannerQuery(query string) *spannerQueryParser {
	return &spannerQueryParser{query: query}
}

func (s spannerQueryParser) parseToIndexFilter(column FilterKey) *Statement {
	terms := strings.Split(s.query, " ")

	exprs := make([]string, 0, len(terms))
	params := make(map[string]any, len(terms))
	for i, term := range terms {
		param := fmt.Sprintf("indexfilterterm%d", i)

		params[param] = term

		exprs = append(exprs, fmt.Sprintf("(%s = @%s)", column, param))
	}
	sql := strings.Join(exprs, " AND ")

	return &Statement{
		Sql:    sql,
		Params: params,
	}
}

func (s spannerQueryParser) parseToSearchSubstring(tokenlist FilterKey) *Statement {
	terms := strings.Split(s.query, " ")

	exprs := make([]string, 0, len(terms))
	params := make(map[string]any, len(terms))
	for i, term := range terms {
		param := fmt.Sprintf("searchsubstringterm%d", i)

		params[param] = term

		exprs = append(exprs, fmt.Sprintf("SEARCH_SUBSTRING(%s, @%s)", tokenlist, param))
	}
	sql := strings.Join(exprs, " OR ")

	return &Statement{
		Sql:    sql,
		Params: params,
	}
}

func (s spannerQueryParser) parseToNgramScore(tokenlist FilterKey) *Statement {
	terms := strings.Split(s.query, " ")

	exprs := make([]string, 0, len(terms))
	params := make(map[string]any, len(terms))
	for i, term := range terms {
		param := fmt.Sprintf("ngramscoreterm%d", i)
		params[param] = term

		exprs = append(exprs, fmt.Sprintf("SCORE_NGRAMS(%s, @%s)", tokenlist, param))
	}
	sql := strings.Join(exprs, " + ")

	return &Statement{
		Sql:    sql,
		Params: params,
	}
}
