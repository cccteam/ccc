package resource

import (
	"fmt"
	"iter"
	"maps"
	"strings"

	"github.com/go-playground/errors/v5"
)

type SearchKey string

func (f SearchKey) String() string {
	return string(f)
}

type SearchType string

const (
	Index     SearchType = "index"
	SubString SearchType = "substring"
	FullText  SearchType = "fulltext"
	Ngram     SearchType = "ngram"
)

type Search struct {
	typ    SearchType
	values map[SearchKey]string
}

func NewSearch(typ SearchType, values map[SearchKey]string) *Search {
	return &Search{
		typ:    typ,
		values: values,
	}
}

func (s Search) spannerStmt() (Statement, error) {
	switch s.typ {
	case SubString:
		searchStatement, err := s.parseToSearchSubstring()
		if err != nil {
			return Statement{}, err
		}

		scoreStatement, err := s.parseToNgramScore()
		if err != nil {
			return Statement{}, err
		}

		sql := fmt.Sprintf("WHERE %s\nORDER BY %s", searchStatement.SQL, scoreStatement.SQL)
		params := make(map[string]any, len(searchStatement.SpannerParams)+len(scoreStatement.SpannerParams))

		maps.Insert(params, maps.All(searchStatement.SpannerParams))
		maps.Insert(params, maps.All(scoreStatement.SpannerParams))

		return Statement{SQL: sql, SpannerParams: params}, nil
	case FullText, Ngram:
		return Statement{}, errors.Newf("%s search is not yet implemented", s.typ)
	default:
		return Statement{}, errors.Newf("%s search type not supported", s.typ)
	}
}

func (s Search) parseToSearchSubstring() (Statement, error) {
	next, stop := iter.Pull(maps.Keys(s.values))
	tokenlist, foundOne := next()
	if _, foundTwo := next(); !foundOne || foundTwo {
		stop()

		return Statement{}, errors.Newf("expected a single key value pair, got %d", len(s.values))
	}
	terms := strings.Split(s.values[tokenlist], " ")

	exprs := make([]string, 0, len(terms))
	params := make(map[string]any, len(terms))
	for i, term := range terms {
		param := fmt.Sprintf("searchsubstringterm%d", i)

		params[param] = term

		exprs = append(exprs, fmt.Sprintf("SEARCH_SUBSTRING(%s, @%s)", tokenlist, param))
	}
	sql := strings.Join(exprs, " OR ")

	return Statement{
		SQL:           sql,
		SpannerParams: params,
	}, nil
}

func (s Search) parseToNgramScore() (Statement, error) {
	next, stop := iter.Pull(maps.Keys(s.values))
	tokenlist, foundOne := next()
	if _, foundTwo := next(); !foundOne || foundTwo {
		stop()

		return Statement{}, errors.Newf("expected a single key value pair, got %d", len(s.values))
	}
	terms := strings.Split(s.values[tokenlist], " ")

	exprs := make([]string, 0, len(terms))
	params := make(map[string]any, len(terms))
	for i, term := range terms {
		param := fmt.Sprintf("ngramscoreterm%d", i)
		params[param] = term

		exprs = append(exprs, fmt.Sprintf("SCORE_NGRAMS(%s, @%s)", tokenlist, param))
	}
	sql := strings.Join(exprs, " + ")

	return Statement{
		SQL:           sql,
		SpannerParams: params,
	}, nil
}
