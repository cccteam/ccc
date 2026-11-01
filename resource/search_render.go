package resource

import (
	"fmt"
	"strings"

	"github.com/go-playground/errors/v5"
)

// The rendering of the three matches and the box (README section 4). On Spanner a
// match is a search function over the field's token column, SEARCH_SUBSTRING with the
// anchor as its relative search type, or STARTS_WITH over the lowercased column for the
// prefix fact; on PostgreSQL it is ILIKE over the role's expression of the column, the
// matching trigram or expression index being the application's DDL as every PostgreSQL
// index is. The box is AND over terms of OR over fields of the field's match: an
// anchored field contributes each declared anchor, a plain field contributes contains.
// Under the caller's grants a denied field drops out of the box and a conditionally
// granted one conjoins its condition inside its match, which keeps the index usable.
//
// A box whose fields come from several search indexes, or whose resource declares
// @searchAlso branches, cannot sit in one Spanner WHERE: the service refuses an OR that
// touches two search indexes in one query block. The statement builder renders the key
// set of renderSearchKeySet instead (queryset.go); this file's inline forms serve a
// table or view whose box is one index's, and PostgreSQL always.

// searchKeyAlias is the reserved column each key set yields its keys under, and the
// prefix of the key sets' aliases; generation keeps resource columns off the prefix.
const searchKeyAlias = "zzSearchKey"

// generateMatchSQL renders a startswith, endswith or contains condition through the
// field's search vocabulary.
func (s *sqlGenerator) generateMatchSQL(cn *ConditionNode) (string, []QueryParam, error) {
	c := &cn.Condition
	if c.match == nil || c.match.spec == nil {
		return "", nil, errors.Wrapf(ErrUnsupportedOperator, "operator %s on %s carries no search vocabulary", c.Operator, c.Field)
	}
	term, _ := c.Value.(string)
	grant := s.searchGrants[c.match.column]
	if grant.dropped {
		return sqlFalse, nil, nil
	}

	var params []QueryParam
	sql, ok := s.matchSQL(c.match, c.Operator, term, &params)
	if !ok {
		return sqlFalse, nil, nil
	}
	if grant.condition != "" && !c.match.positional {
		sql = fmt.Sprintf("(%s AND %s)", grant.condition, sql)
	}

	return sql, params, nil
}

// matchSQL renders one match of a term against a field. The second result is false
// when the term normalizes to nothing for the field's role, so the match can never hold.
func (s *sqlGenerator) matchSQL(field *searchField, operator, term string, params *[]QueryParam) (string, bool) {
	spec := field.spec
	if operator == startswithStr && spec.Lower != "" {
		return s.lowerPrefixSQL(spec.Lower, term, params), true
	}
	normalized, ok := normalizeSearchTerm(term, spec.Role)
	if !ok {
		return "", false
	}
	if s.dialect == Spanner {
		switch operator {
		case startswithStr:
			return s.searchSubstringSQL(spec.Token, normalized, SearchAnchorPrefix, params), true
		case endswithStr:
			return s.searchSubstringSQL(spec.Token, normalized, SearchAnchorSuffix, params), true
		default:
			return s.searchSubstringSQL(spec.Token, normalized, "", params), true
		}
	}

	return s.likeSQL(s.roleExpression(field.column, spec), normalized, operator, params), true
}

// lowerPrefixSQL renders the prefix fact: STARTS_WITH over the lowercased stored column
// on Spanner, LOWER(col) LIKE on PostgreSQL, the term lowercased by the server.
func (s *sqlGenerator) lowerPrefixSQL(lower, term string, params *[]QueryParam) string {
	if s.dialect == Spanner {
		placeholder := s.bindParam(strings.ToLower(term), params)

		return fmt.Sprintf("STARTS_WITH(%s, %s)", s.quoteIdentifier(lower), placeholder)
	}
	placeholder := s.bindParam(escapeLikePattern(strings.ToLower(term))+"%", params)

	return fmt.Sprintf("%s LIKE %s", s.quoteIdentifier(lower), placeholder)
}

// searchSubstringSQL renders SEARCH_SUBSTRING over a token column, with the anchor as
// its relative search type where one is given.
func (s *sqlGenerator) searchSubstringSQL(token, term string, anchor SearchAnchor, params *[]QueryParam) string {
	placeholder := s.bindParam(term, params)
	if anchor == "" {
		return fmt.Sprintf("SEARCH_SUBSTRING(%s, %s)", s.quoteIdentifier(token), placeholder)
	}

	return fmt.Sprintf("SEARCH_SUBSTRING(%s, %s, relative_search_type => '%s')", s.quoteIdentifier(token), placeholder, relativeSearchType(anchor))
}

// roleExpression renders the PostgreSQL expression a role reads a column through: the
// column, its digits, or its date as YYYYMMDD.
func (s *sqlGenerator) roleExpression(column string, spec *SearchSpec) string {
	quoted := s.quoteIdentifier(column)
	switch spec.Role {
	case SearchRoleDigits:
		return fmt.Sprintf(`regexp_replace(%s, '\D', '', 'g')`, quoted)
	case SearchRoleDate:
		if spec.Zone != "" {
			return fmt.Sprintf("to_char(%s AT TIME ZONE '%s', 'YYYYMMDD')", quoted, strings.ReplaceAll(spec.Zone, "'", "''"))
		}

		return fmt.Sprintf("to_char(%s, 'YYYYMMDD')", quoted)
	default:
		return quoted
	}
}

// likeSQL renders the PostgreSQL form of a match: ILIKE over the expression with the
// escaped term as the pattern, anchored by the operator.
func (s *sqlGenerator) likeSQL(expression, term, operator string, params *[]QueryParam) string {
	pattern := escapeLikePattern(term)
	switch operator {
	case startswithStr:
		pattern += "%"
	case endswithStr:
		pattern = "%" + pattern
	default:
		pattern = "%" + pattern + "%"
	}
	placeholder := s.bindParam(pattern, params)

	return fmt.Sprintf("%s ILIKE %s", expression, placeholder)
}

// bindParam allocates the next placeholder for a value and records it.
func (s *sqlGenerator) bindParam(value any, params *[]QueryParam) string {
	placeholder := s.nextPlaceholder()
	*params = append(*params, QueryParam{Name: strings.TrimPrefix(placeholder, "@"), Value: value})

	return placeholder
}

// generateSearchSQL renders the box inline: AND over terms of OR over the fields that
// take part, with a PostgreSQL branch per @searchAlso declaration as a correlated
// EXISTS. On Spanner the inline form serves a box whose fields are one index's and whose
// resource declares no branch; the statement builder routes every other box to the key
// set before this is reached.
func (s *sqlGenerator) generateSearchSQL(cn *ConditionNode) (string, []QueryParam, error) {
	c := cn.Condition.search
	if c == nil {
		return "", nil, errors.Wrapf(ErrUnsupportedOperator, "operator %s carries no search box", cn.Condition.Operator)
	}
	if s.dialect == Spanner && len(c.branches) > 0 {
		return "", nil, errors.New("a search box with @searchAlso branches renders as a key set on Spanner, never inline")
	}

	var params []QueryParam
	termSQLs := make([]string, 0, len(c.terms))
	for _, term := range c.terms {
		var alternatives []string
		for i := range c.fields {
			sql, ok := s.boxFieldSQL(&c.fields[i], term, &params)
			if ok {
				alternatives = append(alternatives, sql)
			}
		}
		for i := range c.branches {
			sql, ok := s.branchExistsSQL(&c.branches[i], c.key, term, &params)
			if ok {
				alternatives = append(alternatives, sql)
			}
		}
		termSQLs = append(termSQLs, disjunction(alternatives))
	}

	return conjunction(termSQLs), params, nil
}

// boxFieldSQL renders one field's contribution to a term: each declared anchor, or
// contains where none is declared, under the field's grant. The second result is false
// when the field contributes nothing: denied, or the term normalizes to nothing for its
// role.
func (s *sqlGenerator) boxFieldSQL(field *searchField, term string, params *[]QueryParam) (string, bool) {
	grant := s.searchGrants[field.column]
	if grant.dropped {
		return "", false
	}
	sql, ok := s.boxMatchSQL(field.column, field.spec, term, params)
	if !ok {
		return "", false
	}
	if grant.condition != "" && !field.positional {
		sql = fmt.Sprintf("(%s AND %s)", grant.condition, sql)
	}

	return sql, true
}

// boxMatchSQL renders a spec's box match for a term over a column.
func (s *sqlGenerator) boxMatchSQL(column string, spec *SearchSpec, term string, params *[]QueryParam) (string, bool) {
	normalized, ok := normalizeSearchTerm(term, spec.Role)
	if !ok {
		return "", false
	}
	if len(spec.Anchors) == 0 {
		if s.dialect == Spanner {
			return s.searchSubstringSQL(spec.Token, normalized, "", params), true
		}

		return s.likeSQL(s.roleExpression(column, spec), normalized, containsStr, params), true
	}
	alternatives := make([]string, 0, len(spec.Anchors))
	for _, anchor := range spec.Anchors {
		if s.dialect == Spanner {
			alternatives = append(alternatives, s.searchSubstringSQL(spec.Token, normalized, anchor, params))

			continue
		}
		operator := startswithStr
		if anchor == SearchAnchorSuffix {
			operator = endswithStr
		}
		alternatives = append(alternatives, s.likeSQL(s.roleExpression(column, spec), normalized, operator, params))
	}

	return disjunction(alternatives), true
}

// branchExistsSQL renders a PostgreSQL branch: the child rows joined to this row by the
// foreign key whose text matches the term, under the child field's grant.
func (s *sqlGenerator) branchExistsSQL(branch *SearchBranch, key, term string, params *[]QueryParam) (string, bool) {
	grant := s.branchGrants[branch.Name]
	if grant.dropped {
		return "", false
	}
	match, ok := s.boxMatchSQL(branch.Column, branch.spec(), term, params)
	if !ok {
		return "", false
	}
	if grant.condition != "" {
		match = fmt.Sprintf("(%s AND %s)", grant.condition, match)
	}
	parentKey := s.quoteIdentifier(key)
	if s.outer != "" {
		parentKey = s.quoteIdentifier(s.outer) + "." + parentKey
	}
	table := s.quoteIdentifier(branch.Table)
	where := []string{fmt.Sprintf("%s.%s = %s", table, s.quoteIdentifier(branch.JoinColumn), parentKey)}
	if branch.TenantColumn != "" {
		where = append(where, fmt.Sprintf("%s.%s = @%s", table, s.quoteIdentifier(branch.TenantColumn), domainParamName))
	}
	where = append(where, match)

	return fmt.Sprintf("EXISTS (SELECT 1 FROM %s WHERE %s)", table, strings.Join(where, " AND ")), true
}

// disjunction joins alternatives with OR, FALSE where there is none.
func disjunction(alternatives []string) string {
	switch len(alternatives) {
	case 0:
		return sqlFalse
	case 1:
		return alternatives[0]
	default:
		return "(" + strings.Join(alternatives, " OR ") + ")"
	}
}

// conjunction joins terms with AND, parenthesized when there are several.
func conjunction(terms []string) string {
	if len(terms) == 1 {
		return terms[0]
	}

	return "(" + strings.Join(terms, " AND ") + ")"
}

// searchKeySet is the FROM item the key-set statement reads a resource through: one key
// set per term, a UNION DISTINCT of the resource filtered by one index's predicates and
// of each branch's child rows, joined on the key, and driving an apply join into the
// resource (README section 4, "the box over fields in several indexes").
type searchKeySet struct {
	// from is the FROM clause, the key sets joined to the resource, "" when empty.
	from string
	// empty says some term found no branch to search, so the statement admits no row.
	empty bool
	// params are the values the key sets bind.
	params []QueryParam
}

// renderSearchKeySet renders the key sets of a box on Spanner. query is the resource's
// FROM item as the plain statement reads it (the table, or the view's subquery under
// its alias), outer the resource's name, and tenancy the tenancy predicate every
// branch over the resource carries, "" in a global request.
func (s *sqlGenerator) renderSearchKeySet(c *searchCondition, query, outer, tenancy string) (*searchKeySet, error) {
	if c.key == "" {
		return nil, errors.New("a search box over several indexes or branches needs the resource's single-column key, which the resource does not declare")
	}
	set := &searchKeySet{}
	resource := s.quoteIdentifier(outer)
	key := s.quoteIdentifier(c.key)

	var from strings.Builder
	for i, term := range c.terms {
		branches := s.termBranches(c, term, query, resource, key, tenancy, &set.params)
		if len(branches) == 0 {
			return &searchKeySet{empty: true}, nil
		}
		alias := fmt.Sprintf("%s%d", searchKeyAlias, i+1)
		if i == 0 {
			fmt.Fprintf(&from, "(%s) AS %s", strings.Join(branches, "\n UNION DISTINCT\n "), alias)

			continue
		}
		fmt.Fprintf(&from, "\nJOIN (%s) AS %s ON %s.%s = %s1.%s", strings.Join(branches, "\n UNION DISTINCT\n "), alias, alias, searchKeyAlias, searchKeyAlias, searchKeyAlias)
	}
	fmt.Fprintf(&from, "\nJOIN@{JOIN_METHOD=APPLY_JOIN} %s ON %s.%s = %s1.%s", query, resource, key, searchKeyAlias, searchKeyAlias)
	set.from = from.String()

	return set, nil
}

// termBranches renders one term's branches: the resource's own fields grouped by the
// index that carries them, then each @searchAlso branch, each a SELECT of the key.
func (s *sqlGenerator) termBranches(c *searchCondition, term, query, resource, key, tenancy string, params *[]QueryParam) []string {
	var branches []string
	for _, group := range groupByIndex(c.fields) {
		var alternatives []string
		for _, field := range group {
			sql, ok := s.boxFieldSQL(field, term, params)
			if ok {
				alternatives = append(alternatives, sql)
			}
		}
		if len(alternatives) == 0 {
			continue
		}
		where := []string{}
		if tenancy != "" {
			where = append(where, tenancy)
		}
		where = append(where, disjunction(alternatives))
		branches = append(branches, fmt.Sprintf("SELECT %s.%s AS %s FROM %s WHERE %s", resource, key, searchKeyAlias, query, strings.Join(where, " AND ")))
	}
	for i := range c.branches {
		branch := &c.branches[i]
		grant := s.branchGrants[branch.Name]
		if grant.dropped {
			continue
		}
		match, ok := s.boxMatchSQL(branch.Column, branch.spec(), term, params)
		if !ok {
			continue
		}
		table := s.quoteIdentifier(branch.Table)
		where := []string{}
		if branch.TenantColumn != "" {
			where = append(where, fmt.Sprintf("(%s.%s = @%s)", table, s.quoteIdentifier(branch.TenantColumn), domainParamName))
		}
		if grant.condition != "" {
			where = append(where, grant.condition)
		}
		where = append(where, match)
		branches = append(branches, fmt.Sprintf("SELECT %s.%s AS %s FROM %s WHERE %s", table, s.quoteIdentifier(branch.JoinColumn), searchKeyAlias, table, strings.Join(where, " AND ")))
	}

	return branches
}

// groupByIndex groups a box's fields by the search index carrying them, in first
// appearance order, so each key-set branch touches one index.
func groupByIndex(fields []searchField) [][]*searchField {
	var order []string
	groups := make(map[string][]*searchField)
	for i := range fields {
		index := fields[i].spec.Index
		if _, seen := groups[index]; !seen {
			order = append(order, index)
		}
		groups[index] = append(groups[index], &fields[i])
	}
	out := make([][]*searchField, 0, len(order))
	for _, index := range order {
		out = append(out, groups[index])
	}

	return out
}

// needsKeySet reports whether a box renders as a key set on Spanner: its resource
// declares @searchAlso branches, or its fields come from more than one search index.
func (c *searchCondition) needsKeySet() bool {
	if len(c.branches) > 0 {
		return true
	}

	return len(groupByIndex(c.fields)) > 1
}

// splitSearchConjunct lifts the box out of a filter tree it is joined to by AND alone:
// the box, and the residual tree without it, nil when the box was the whole filter. ok
// is false when the tree holds no box, or holds it under an OR, where no key set can
// stand for it.
func splitSearchConjunct(node ExpressionNode) (box *ConditionNode, residual ExpressionNode, ok bool) {
	switch n := node.(type) {
	case *ConditionNode:
		if n.Condition.search != nil {
			return n, nil, true
		}

		return nil, n, false
	case *GroupNode:
		box, inner, ok := splitSearchConjunct(n.Expression)
		if !ok {
			return nil, n, false
		}
		if inner == nil {
			return box, nil, true
		}

		return box, &GroupNode{Expression: inner}, true
	case *LogicalOpNode:
		if n.Operator != OperatorAnd {
			return nil, n, false
		}
		if box, left, ok := splitSearchConjunct(n.Left); ok {
			if left == nil {
				return box, n.Right, true
			}

			return box, &LogicalOpNode{Left: left, Operator: OperatorAnd, Right: n.Right}, true
		}
		if box, right, ok := splitSearchConjunct(n.Right); ok {
			if right == nil {
				return box, n.Left, true
			}

			return box, &LogicalOpNode{Left: n.Left, Operator: OperatorAnd, Right: right}, true
		}

		return nil, n, false
	default:
		return nil, node, false
	}
}

// findSearchBox returns the box condition anywhere in a tree, nil when there is none.
func findSearchBox(node ExpressionNode) *ConditionNode {
	for _, leaf := range leaves(node) {
		if leaf.Condition.search != nil {
			return leaf
		}
	}

	return nil
}

// hasSearchPredicate reports whether a tree carries a search function: a match or the
// box, which a Spanner statement names its optimizer version for.
func hasSearchPredicate(node ExpressionNode) bool {
	for _, leaf := range leaves(node) {
		if leaf.Condition.search != nil || leaf.Condition.match != nil {
			return true
		}
	}

	return false
}
