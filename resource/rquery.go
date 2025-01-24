package resource

import (
	"bufio"
	"strings"
)

type token string

var and token = "AND"

func ParseRawQuery(query string) error {

	queryScanner := bufio.NewScanner(strings.NewReader(query))
	queryScanner.Scan()

	return nil
}

type BoolExpr string
type Operator string

const (
	AND Operator = "AND"
	NOT Operator = "NOT"
	OR  Operator = "OR"
)

type WhereClause struct {
	BoolExprs []BoolExpr
	Operators []Operator
}

func NewWhereClause(exp BoolExpr) *WhereClause {
	return &WhereClause{BoolExprs: []BoolExpr{exp}}
}

func (pr *WhereClause) Add(op Operator, exp BoolExpr) *WhereClause {
	pr.Operators = append(pr.Operators, op)
	pr.BoolExprs = append(pr.BoolExprs, exp)

	return pr
}

func (pr *WhereClause) String() string {
	b := strings.Builder{}
	for i, exp := range pr.BoolExprs {
		if i != 0 {
			b.WriteString(string(pr.Operators[i-1]))
		}

		b.WriteString(string(exp))
	}

	return b.String()
}

/*

search=bank

SELECT *
FROM DoeInstitutions
WHERE SEARCH_SUBSTRING(SearchTokens, 'bank')
ORDER BY SCORE_NGRAMS(SearchTokens, 'bank') DESC;

search=bank fargo

SELECT *
FROM DoeInstitutions
WHERE SEARCH_SUBSTRING(SearchTokens, 'well bank')
ORDER BY SCORE_NGRAMS(SearchTokens, 'bank') DESC;
*/
