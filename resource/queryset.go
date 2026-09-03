package resource

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/cloudspannerecosystem/memefish"
	"github.com/cloudspannerecosystem/memefish/token"
	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// QuerySet represents a query for a resource, including fields, keys, filters, and permissions.
type QuerySet[Resource Resourcer] struct {
	keys                   *fieldSet
	fields                 []accesstypes.Field
	sortFields             []SortField
	limit                  *uint64
	offset                 *uint64
	returnAccessibleFields bool
	requestableFields      []accesstypes.Field
	rMeta                  *Metadata[Resource]
	resourceSet            *Set[Resource]
	userPermissions        UserPermissions
	scope                  accesstypes.Scope
	requiredPermission     accesstypes.Permission
	filterAst              ExpressionNode
	filterParser           func(DBType) (ExpressionNode, error)

	// env is the request's decision context, stamped by the decoder that built
	// the QuerySet (a QuerySet built by hand carries the empty Environment).
	// The permission checks fold conditions against it, and condition rendering
	// binds the same value as SQL parameters.
	env accesstypes.Environment

	// conditionalDecisions carries the Conditional decisions the permission
	// checks returned, keyed by checked resource — a conditional grant is a
	// grant, so its resources pass the gate and the conditions travel here for
	// the E-phase lowering (read WHERE/CASE mask rendering, the delete
	// check-SELECT). While the engine holds no conditions the map stays empty
	// and behavior is byte-identical RBAC.
	conditionalDecisions accesstypes.Decisions

	// collection resolves condition rendering: the checked resource's bindings
	// and the app's subject anchors. Stamped by the decoder; nil on a
	// hand-built QuerySet, which errors if conditions ever need rendering.
	collection *GeneratedCollection

	// jsonNames maps resource fields to their request-type JSON names for the
	// masked-names column. Stamped by the decoder; a missing entry falls back
	// to the Go field name.
	jsonNames map[accesstypes.Field]string

	// capabilities are the write permissions the request asked to evaluate
	// per row (the §13 capability envelope), in request order;
	// capabilityDecisions carries each one's full engine answer for the
	// statement's capability plan. Both stay empty unless the request opted
	// in, keeping capability-free statements byte-identical.
	capabilities        []accesstypes.Permission
	capabilityDecisions map[accesstypes.Permission]accesstypes.Decisions
}

// NewQuerySet creates a new, empty QuerySet for a given resource metadata.
func NewQuerySet[Resource Resourcer](rMeta *Metadata[Resource]) *QuerySet[Resource] {
	return &QuerySet[Resource]{
		keys:  newFieldSet(),
		rMeta: rMeta,
	}
}

// Resource returns the name of the resource this QuerySet applies to.
func (q *QuerySet[Resource]) Resource() accesstypes.Resource {
	var r Resource

	return r.Resource()
}

func (q *QuerySet[Resource]) query() (withClause, query string, params map[string]any) {
	var r Resource

	switch t := any(r).(type) {
	case virtualQuerier:
		query, params = t.Subquery()

		withClause, query = extractWithClause(query)

		// newlines before final parenthesis is necessary to combat any trailing comments
		query = fmt.Sprintf("(%s\n) AS %s", query, r.Resource())

		for pramName := range params {
			if strings.HasPrefix(pramName, "_") {
				panic(fmt.Sprintf("Subquery params for %s can not start with an _", r.Resource()))
			}
		}

		return withClause, query, params
	default:
		return "", string(r.Resource()), nil
	}
}

// RequiredPermission returns the permission required to execute the query.
func (q *QuerySet[Resource]) RequiredPermission() accesstypes.Permission {
	return q.requiredPermission
}

// ReturnAccessibleFields configures the QuerySet to automatically include all fields
// the user has access to if no specific fields are requested.
func (q *QuerySet[Resource]) ReturnAccessibleFields(b bool) *QuerySet[Resource] {
	q.returnAccessibleFields = b

	return q
}

// EnableUserPermissionEnforcement enables the checking of user permissions for the QuerySet,
// evaluating requiredPermission for the user in the given scope partition.
//
// An enforced QuerySet is single-shot: it binds the user (via userPermissions) and the
// scope for a single operation's evaluation. Build a new QuerySet per operation; never
// reuse one across requests or scopes.
func (q *QuerySet[Resource]) EnableUserPermissionEnforcement(rSet *Set[Resource], userPermissions UserPermissions, scope accesstypes.Scope, requiredPermission accesstypes.Permission) *QuerySet[Resource] {
	q.resourceSet = rSet
	q.userPermissions = userPermissions
	q.scope = scope
	q.requiredPermission = requiredPermission

	return q
}

// checkPermissions runs the read's own permission gates and, when the request
// opted into the capability envelope, the advisory capability checks — all
// against the same environment.
func (q *QuerySet[Resource]) checkPermissions(ctx context.Context, dbType DBType) error {
	if err := q.checkReadPermissions(ctx, dbType); err != nil {
		return err
	}

	return q.checkCapabilityPermissions(ctx)
}

func (q *QuerySet[Resource]) checkReadPermissions(ctx context.Context, dbType DBType) error {
	if q.resourceSet != nil {
		decisions, err := q.userPermissions.Check(ctx, q.env, q.scope, q.requiredPermission, q.resourceSet.BaseResource())
		if err != nil {
			return errors.Wrap(err, "resource.UserPermissions.Check()")
		}
		if denied := decisions.DeniedResources(); len(denied) > 0 {
			return httpio.NewForbiddenMessagef("scope (%s), user (%s) does not have (%s) on %s", q.scope, q.userPermissions.User(), q.requiredPermission, denied)
		}
		q.carryConditionalDecisions(decisions)
	}

	fields := q.Fields()

	if len(fields) == 0 && q.returnAccessibleFields {
		return q.addAccessibleFields(ctx, dbType)
	}

	if q.resourceSet != nil {
		resources := make([]accesstypes.Resource, 0, len(fields)+1)

		for _, fieldName := range fields {
			if q.resourceSet.PermissionRequired(fieldName, q.requiredPermission) {
				resources = append(resources, q.resourceSet.Resource(fieldName))
			}
		}

		// A conditional grant is a grant: explicitly requested fields it covers
		// pass this gate, and their conditions ride the set. Only a field no
		// grant covers at all is Forbidden.
		decisions, err := q.userPermissions.Check(ctx, q.env, q.scope, q.requiredPermission, resources...)
		if err != nil {
			return errors.Wrap(err, "resource.UserPermissions.Check()")
		}
		if denied := decisions.DeniedResources(); len(denied) > 0 {
			return httpio.NewForbiddenMessagef("scope (%s), user (%s) does not have (%s) on %s", q.scope, q.userPermissions.User(), q.requiredPermission, denied)
		}
		q.carryConditionalDecisions(decisions)
	}

	return nil
}

// RequestCapabilities asks the read to evaluate per-row write affordances for
// perms and attach them under the reserved capability property (§13). The
// supported permissions are Update (a positive list of editable JSON field
// names), Delete (a boolean), and Execute (a positive list of the RPC methods
// whose declared transitions apply to the row). The answers are advisory
// hints for the UI; enforcement stays with the write stages.
func (q *QuerySet[Resource]) RequestCapabilities(perms ...accesstypes.Permission) {
	q.capabilities = perms
}

// carryConditionalDecisions records the Conditional decisions from one Check
// call on the QuerySet for the E-phase condition lowering.
func (q *QuerySet[Resource]) carryConditionalDecisions(decisions accesstypes.Decisions) {
	for res, decision := range decisions {
		if !decision.IsConditional() {
			continue
		}
		if q.conditionalDecisions == nil {
			q.conditionalDecisions = accesstypes.Decisions{}
		}
		q.conditionalDecisions[res] = decision
	}
}

// requestable reports whether a field can be requested by a client. A field outside the
// requestable set (e.g. excluded from the request type with json:"-") can never be
// requested explicitly, so it must not be returned by default either. A nil set (the
// QuerySet was not built by a QueryDecoder) places no restriction.
func (q *QuerySet[Resource]) requestable(field accesstypes.Field) bool {
	return q.requestableFields == nil || slices.Contains(q.requestableFields, field)
}

func (q *QuerySet[Resource]) addAccessibleFields(ctx context.Context, dbType DBType) error {
	fields := make([]accesstypes.Field, 0, q.rMeta.DBFieldCount(dbType))

	if q.resourceSet != nil {
		// A candidate with a zero resource is exempt (the perm:"-" primary-key marker,
		// whose readability follows the resource-level grant already checked above) or
		// json-hidden; every other requestable field is registered, and those candidates
		// are evaluated in a single set-oriented Check call.
		type candidate struct {
			field accesstypes.Field
			res   accesstypes.Resource
		}

		candidates := make([]candidate, 0, q.rMeta.DBFieldCount(dbType))
		resources := make([]accesstypes.Resource, 0, q.rMeta.DBFieldCount(dbType))

		for _, field := range q.rMeta.DBFields(dbType) {
			if !q.requestable(field) {
				continue
			}

			if !q.resourceSet.PermissionRequired(field, q.RequiredPermission()) {
				candidates = append(candidates, candidate{field: field})
			} else {
				res := q.resourceSet.Resource(field)
				candidates = append(candidates, candidate{field: field, res: res})
				resources = append(resources, res)
			}
		}

		// The default projection is every field some grant mentions: Granted and
		// Conditional candidates are included (a blocked cell is the rendering's
		// job, not the projection's), Denied candidates are filtered out.
		var decisions accesstypes.Decisions
		if len(resources) > 0 {
			var err error
			decisions, err = q.userPermissions.Check(ctx, q.env, q.scope, q.requiredPermission, resources...)
			if err != nil {
				return errors.Wrap(err, "resource.UserPermissions.Check()")
			}
			q.carryConditionalDecisions(decisions)
		}

		for _, c := range candidates {
			if c.res == "" || !decisions[c.res].IsDenied() {
				fields = append(fields, c.field)
			}
		}
	} else {
		// If we don't have a resourceSet, return all requestable fields
		for _, field := range q.rMeta.DBFields(dbType) {
			if q.requestable(field) {
				fields = append(fields, field)
			}
		}
	}

	for _, field := range fields {
		q.AddField(field)
	}

	return nil
}

// AddField adds a field to be returned by the query.
func (q *QuerySet[Resource]) AddField(field accesstypes.Field) *QuerySet[Resource] {
	if !slices.Contains(q.fields, field) {
		q.fields = append(q.fields, field)
	}

	return q
}

// Fields returns the list of fields to be returned by the query.
func (q *QuerySet[Resource]) Fields() []accesstypes.Field {
	return q.fields
}

// SetKey sets a primary key field and value for the query's WHERE clause.
func (q *QuerySet[Resource]) SetKey(field accesstypes.Field, value any) {
	q.keys.Set(field, value)
}

// Key retrieves the value of a primary key field.
func (q *QuerySet[Resource]) Key(field accesstypes.Field) any {
	return q.keys.Get(field)
}

// Len returns the number of fields to be returned by the query.
func (q *QuerySet[Resource]) Len() int {
	return len(q.fields)
}

// KeySet returns the KeySet containing the primary key(s) for the resource.
func (q *QuerySet[Resource]) KeySet() KeySet {
	return q.keys.KeySet()
}

// buildOrderByClause builds an ORDER BY clause from the QuerySet's sort fields.
func (q *QuerySet[Resource]) buildOrderByClause(dbType DBType) (string, error) {
	orderByParts := make([]string, 0, len(q.sortFields))
	for _, sf := range q.sortFields {
		dbField, ok := q.rMeta.dbFieldMap(dbType)[accesstypes.Field(sf.Field)]
		if !ok {
			return "", errors.Newf("sort field '%s' not found in resource metadata for query", sf.Field)
		}

		var quotedColumnName string
		switch dbType {
		case SpannerDBType:
			quotedColumnName = fmt.Sprintf("`%s`", dbField.ColumnName)
		case PostgresDBType:
			quotedColumnName = fmt.Sprintf(`"%s"`, dbField.ColumnName)
		default:
			return "", errors.Newf("unsupported dbType for sorting: %s", dbType)
		}

		directionSQL := "ASC"
		if sf.Direction == SortDescending {
			directionSQL = "DESC"
		}
		orderByParts = append(orderByParts, fmt.Sprintf("%s %s", quotedColumnName, directionSQL))
	}
	if len(orderByParts) == 0 {
		return "", nil
	}

	return "ORDER BY " + strings.Join(orderByParts, ", "), nil
}

// fieldColumnMetadata pairs a projected field with its database metadata.
type fieldColumnMetadata struct {
	field accesstypes.Field
	meta  dbFieldMetadata
}

// orderedDBFields returns the projected fields with their database metadata,
// in struct declaration order — the projection order of every select list.
func (q *QuerySet[Resource]) orderedDBFields(dbType DBType) ([]fieldColumnMetadata, error) {
	fieldColumns := make([]fieldColumnMetadata, 0, q.Len())
	for _, field := range q.Fields() {
		dbField, ok := q.rMeta.dbFieldMap(dbType)[field]
		if !ok {
			return nil, errors.Newf("field %s not found in db struct", field)
		}

		fieldColumns = append(fieldColumns, fieldColumnMetadata{field: field, meta: dbField})
	}
	sort.Slice(fieldColumns, func(i, j int) bool {
		return fieldColumns[i].meta.index < fieldColumns[j].meta.index
	})

	return fieldColumns, nil
}

// columns returns the select list for the fields the user has access to view.
// With no rendered conditions it is the plain column list; conditionally
// granted columns render as their CASE, and the reserved masked-names column
// is appended when any CASE survives pruning.
func (q *QuerySet[Resource]) columns(dbType DBType, rendered *renderedReadConditions, capChecksItem string) (Columns, error) {
	fieldColumns, err := q.orderedDBFields(dbType)
	if err != nil {
		return "", err
	}

	columns := make([]string, 0, len(fieldColumns)+1)
	for _, fieldColumn := range fieldColumns {
		if rendered != nil {
			if override, ok := rendered.overrides[fieldColumn.field]; ok {
				columns = append(columns, override)

				continue
			}
		}
		switch dbType {
		case SpannerDBType:
			columns = append(columns, fieldColumn.meta.ColumnName)
		case PostgresDBType:
			columns = append(columns, fmt.Sprintf(`"%s"`, fieldColumn.meta.ColumnName))
		default:
			return "", errors.Newf("unsupported dbType: %s", dbType)
		}
	}
	if rendered != nil && rendered.maskColumn != "" {
		columns = append(columns, rendered.maskColumn)
	}
	if capChecksItem != "" {
		columns = append(columns, capChecksItem)
	}

	return Columns(strings.Join(columns, ", ")), nil
}

func (q *QuerySet[Resource]) astWhereClause(dbType DBType, filterAst ExpressionNode) (*Statement, error) {
	switch dbType {
	case SpannerDBType:
		sql, params, err := NewSpannerGenerator().GenerateSQL(filterAst)
		if err != nil {
			return nil, errors.Wrap(err, "SpannerGenerator.GenerateSQL()")
		}

		return &Statement{SQL: "WHERE " + sql, Params: params}, nil
	case PostgresDBType:
		sql, params, err := NewPostgreSQLGenerator().GenerateSQL(filterAst)
		if err != nil {
			return nil, errors.Wrap(err, "PostgreSQLGenerator.GenerateSQL()")
		}

		return &Statement{SQL: "WHERE " + sql, Params: params}, nil
	default:
		return nil, errors.Newf("unsupported dbType: %s", dbType)
	}
}

// where translates the the fields to database struct tags in databaseType when building the where clause
func (q *QuerySet[Resource]) where(dbType DBType, filterAst ExpressionNode) (*Statement, error) {
	if filterAst != nil {
		return q.astWhereClause(dbType, filterAst)
	}

	parts := q.KeySet().Parts()
	if len(parts) == 0 {
		return &Statement{Params: map[string]any{}}, nil
	}

	builder := strings.Builder{}
	params := make(map[string]any, len(parts))
	for _, part := range parts {
		f, ok := q.rMeta.dbFieldMap(dbType)[part.Key]
		if !ok {
			return nil, errors.Newf("field %s not found in struct", part.Key)
		}
		switch dbType {
		case SpannerDBType:
			fmt.Fprintf(&builder, " AND `%s` = @_%s", f.ColumnName, strings.ToLower(f.ColumnName))
		case PostgresDBType:
			fmt.Fprintf(&builder, ` AND "%s" = @_%s`, f.ColumnName, strings.ToLower(f.ColumnName))
		default:
			return nil, errors.Newf("unsupported dbType: %s", dbType)
		}
		params["_"+strings.ToLower(f.ColumnName)] = part.Value
	}

	return &Statement{
		SQL:    "WHERE " + builder.String()[5:],
		Params: params,
	}, nil
}

// whereWithPredicates builds the WHERE clause and appends the lowered
// predicate fragments: the tenancy AND sits in the WHERE before the read
// rules' row predicate (design plan §06) — checked domain == filtered domain
// by construction.
func (q *QuerySet[Resource]) whereWithPredicates(dbType DBType, filterAst ExpressionNode, tenancy string, rendered *renderedReadConditions) (*Statement, error) {
	where, err := q.where(dbType, filterAst)
	if err != nil {
		return nil, errors.Wrap(err, "patcher.Where()")
	}

	predicates := []string{tenancy}
	if rendered != nil && rendered.rowPredicate != "" {
		predicates = append(predicates, rendered.rowPredicate)
	}
	for _, predicate := range predicates {
		if predicate == "" {
			continue
		}
		if where.SQL == "" {
			where.SQL = "WHERE " + predicate
		} else {
			where.SQL += " AND " + predicate
		}
	}

	return where, nil
}

// stmt builds a SQL statement for the given database type from the QuerySet.
func (q *QuerySet[Resource]) stmt(dbType DBType) (*Statement, error) {
	filterAst, err := q.FilterAst(dbType)
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.FilterAst()")
	}

	if moreThan(1, q.KeySet().Len() != 0, filterAst != nil) {
		return nil, httpio.NewBadRequestMessage("cannot use multiple sources for WHERE clause together (e.g. QueryClause and KeySet)")
	}

	plan, err := q.readConditionPlan()
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.readConditionPlan()")
	}

	// The registry is statement-scoped: the read-condition fragments and the
	// tenancy predicate share it, so aliases and placeholders stay unique.
	registry := newParamRegistry()

	var rendered *renderedReadConditions
	if plan != nil {
		rendered, err = q.renderReadConditions(dbType, plan, registry)
		if err != nil {
			return nil, errors.Wrap(err, "QuerySet.renderReadConditions()")
		}
	}

	tenancy, err := q.tenancyPredicate(dbType, registry)
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.tenancyPredicate()")
	}

	capPlan, capChecksItem, err := q.renderCapabilities(dbType, registry)
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.renderCapabilities()")
	}

	columns, err := q.columns(dbType, rendered, capChecksItem)
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.Columns()")
	}

	where, err := q.whereWithPredicates(dbType, filterAst, tenancy, rendered)
	if err != nil {
		return nil, err
	}

	orderByClause, err := q.buildOrderByClause(dbType)
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.buildOrderByClause()")
	}

	limitClause, offsetClause := q.pageClauses()

	withClause, query, subqueryParams := q.query()
	for k := range subqueryParams {
		if _, ok := where.Params[k]; ok {
			return nil, errors.Newf("named parameter collision: %s subquery and where clause both contain named parameter %q", q.Resource(), k)
		}

		where.Params[k] = subqueryParams[k]
	}

	if err := q.mergeRegistryParams(registry, where.Params); err != nil {
		return nil, err
	}

	sql := fmt.Sprintf(`
			%s
			SELECT
				%s
			FROM %s
			%s
			%s
			%s
			%s`, withClause, columns, query, where.SQL, orderByClause, limitClause, offsetClause,
	)

	resolvedSQL, err := substituteSQLParams(where.SQL, where.Params)
	if err != nil {
		return nil, errors.Wrap(err, "failed to substitute SQL params for resolvedWhereClause")
	}

	stmt := &Statement{resolvedWhereClause: resolvedSQL, SQL: sql, Params: where.Params}
	if rendered != nil && rendered.maskColumn != "" {
		stmt.maskedNamesColumn = maskedNamesColumnName
	}
	stmt.capabilityPlan = capPlan

	return stmt, nil
}

// pageClauses renders the statement's LIMIT and OFFSET clauses.
func (q *QuerySet[Resource]) pageClauses() (limitClause, offsetClause string) {
	if q.limit != nil {
		limitClause = fmt.Sprintf("LIMIT %d", *q.limit)
	}
	if q.offset != nil {
		offsetClause = fmt.Sprintf("OFFSET %d", *q.offset)
	}

	return limitClause, offsetClause
}

// Read executes the query and returns a single result wrapped in the Row envelope.
func (q *QuerySet[Resource]) Read(ctx context.Context, txn ReadOnlyTransaction) (*Row[Resource], error) {
	r := newReader[Resource](txn)
	if err := q.checkPermissions(ctx, r.DBType()); err != nil {
		return nil, err
	}

	stmt, err := q.stmt(r.DBType())
	if err != nil {
		return nil, errors.Wrap(err, "patcher.Stmt()")
	}

	dst, err := r.Read(ctx, stmt)
	if err != nil {
		return nil, errors.Wrapf(err, "Reader[%s].Read()", q.Resource())
	}

	return dst, nil
}

// List executes the query and returns an iterator for the results, each wrapped in the Row envelope.
func (q *QuerySet[Resource]) List(ctx context.Context, txn ReadOnlyTransaction) iter.Seq2[*Row[Resource], error] {
	return func(yield func(*Row[Resource], error) bool) {
		r := newReader[Resource](txn)
		if err := q.checkPermissions(ctx, r.DBType()); err != nil {
			yield(nil, err)

			return
		}

		stmt, err := q.stmt(r.DBType())
		if err != nil {
			yield(nil, errors.Wrap(err, "patcher.Stmt()"))

			return
		}

		for r, err := range r.List(ctx, stmt) {
			if !yield(r, err) {
				return
			}
		}
	}
}

// BatchList executes the query and returns an iterator for the results in batches, each result wrapped in the Row envelope.
func (q *QuerySet[Resource]) BatchList(ctx context.Context, client Client, size int) iter.Seq[iter.Seq2[*Row[Resource], error]] {
	return ccc.BatchIter2(q.List(ctx, client), size)
}

// SetWhereClause sets the filter condition for the query using a QueryClause.
func (q *QuerySet[Resource]) SetWhereClause(qc QueryClause) {
	q.filterAst = qc.tree
}

// SetFilterAst sets the filter condition for the query using a raw expression tree.
func (q *QuerySet[Resource]) SetFilterAst(ast ExpressionNode) {
	q.filterAst = ast
}

// FilterAst returns the filter AST for the query.
func (q *QuerySet[Resource]) FilterAst(dbType DBType) (ExpressionNode, error) {
	if q.filterAst == nil && q.filterParser != nil {
		filterAst, err := q.filterParser(dbType)
		if err != nil {
			return nil, errors.Wrap(err, "filterParser()")
		}

		return filterAst, nil
	}

	return q.filterAst, nil
}

// SetFilterParser sets the filter parser.
func (q *QuerySet[Resource]) SetFilterParser(parser func(DBType) (ExpressionNode, error)) {
	q.filterParser = parser
}

// SetSortFields sets the sorting order for the query results.
func (q *QuerySet[Resource]) SetSortFields(sortFields []SortField) {
	q.sortFields = sortFields
}

// SetLimit sets the maximum number of results to return.
func (q *QuerySet[Resource]) SetLimit(limit *uint64) {
	q.limit = limit
}

// SetOffset sets the starting point for returning results.
func (q *QuerySet[Resource]) SetOffset(offset *uint64) {
	q.offset = offset
}

func extractWithClause(query string) (withClause, remainingQuery string) {
	lex := &memefish.Lexer{
		File: &token.File{Buffer: query},
	}

	depth := 0
	lastClosingParenEnd := -1
	startedWith := false

	for {
		if err := lex.NextToken(); err != nil {
			break
		}

		if lex.Token.Kind == token.TokenEOF {
			break
		}

		// First meaningful token must be WITH
		if !startedWith && depth == 0 && lastClosingParenEnd == -1 {
			if strings.EqualFold(lex.Token.Raw, "WITH") {
				startedWith = true
			} else {
				return "", query
			}
		}

		switch lex.Token.Kind {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				lastClosingParenEnd = int(lex.Token.End)
			}
		default:
			if depth == 0 {
				raw := lex.Token.Raw
				if strings.EqualFold(raw, "SELECT") || strings.EqualFold(raw, "UPDATE") || strings.EqualFold(raw, "DELETE") || strings.EqualFold(raw, "INSERT") {
					if startedWith && lastClosingParenEnd != -1 {
						return query[:lastClosingParenEnd], query[lastClosingParenEnd:]
					}
				}
			}
		}
	}

	return "", query
}

// moreThan checks if more than a given count of boolean expressions are true.
func moreThan(cnt int, exp ...bool) bool {
	count := 0
	for _, v := range exp {
		if v {
			count++
		}
	}

	return count > cnt
}

var _ QuerySetComparer = (*QuerySet[nilResource])(nil)

// QuerySetComparer is an interface for comparing two QuerySet-like objects.
type QuerySetComparer interface {
	Resource() accesstypes.Resource
	Fields() []accesstypes.Field
	KeySet() KeySet
}

// QuerySetDiff compares two QuerySetComparer objects for equality. It checks resource, fields, and primary keys.
func QuerySetDiff(opts ...cmp.Option) func(a, b QuerySetComparer) string {
	return func(a, b QuerySetComparer) string {
		if a.Resource() != b.Resource() {
			return fmt.Sprintf("Resource() mismatch (-want +got):\n- %s\n+ %s", a.Resource(), b.Resource())
		}

		if diff := cmp.Diff(a.Fields(), b.Fields(), cmpopts.SortSlices(func(x, y accesstypes.Field) bool { return x < y })); diff != "" {
			return fmt.Sprintf("Fields mismatch (-want +got):\n%s", diff)
		}

		aKeyData, bKeyData := a.KeySet().KeyMap(), b.KeySet().KeyMap()
		if diff := cmp.Diff(
			slices.Collect(maps.Keys(aKeyData)),
			slices.Collect(maps.Keys(bKeyData)),
			cmpopts.SortSlices(func(x, y accesstypes.Field) bool { return x < y })); diff != "" {
			return fmt.Sprintf("Query Fields mismatch (-want +got):\n%s", diff)
		}

		for k, v := range aKeyData {
			if diff := cmp.Diff(v, bKeyData[k], opts...); diff != "" {
				return fmt.Sprintf("Query Value mismatch for field %s, (-want +got):\n%s", k, diff)
			}
		}

		return ""
	}
}
