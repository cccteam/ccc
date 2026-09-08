package resource

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"reflect"
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
	returnAccessibleFields bool
	requestableFields      []accesstypes.Field
	rMeta                  *Metadata[Resource]
	resourceSet            *Set[Resource]
	userPermissions        UserPermissions
	scope                  accesstypes.Scope
	requiredPermission     accesstypes.Permission
	filterAst              ExpressionNode
	filterParser           func(DBType) (ExpressionNode, error)
	// filterFields names the fields the decoded filter expression touches. With
	// sortFields it is the set a caller must hold an unconditional grant on:
	// ordering or filtering by a field the caller cannot read on every row is
	// an inference channel. Empty on a hand-built QuerySet.
	filterFields []accesstypes.Field
	// keyFields are the resource's primary-key fields, in declaration order,
	// appended to every decoded list's order so the order is total: two rows
	// equal on every sort column are separated by the key. Stamped by the
	// decoder from the request type's primary-key markers; empty on a
	// hand-built QuerySet, whose order is exactly what the caller set.
	keyFields []accesstypes.Field
	// defaultOrder is the resource's declared order (Paging.Order), taken when
	// the request carries no sort.
	defaultOrder []SortField
	// cursor is the opened cursor the request carries, positioning the page
	// after (or, walking back, before) its boundary row; nil on a first page.
	// cursorKey seals the cursors the page emits; nil when the application
	// wired none, which refuses paging past the first page.
	cursor    *cursor
	cursorKey *CursorKey
	// page is the page the request asked for; nil on a hand-built QuerySet,
	// which reads exactly its limit.
	page *pageRequest

	// The computed-resource pushdown state: the filter over Go field names the
	// body and handler share, and whether the body took the sort and the page
	// (TakeSort, TakePage), leaving the handler nothing to do for them.
	filterShape *FilterShape
	sortTaken   bool
	pageTaken   bool
	// filterString is the request's filter exactly as sent, fingerprinted into
	// every cursor the page emits so a cursor cannot be carried to another
	// query.
	filterString string

	// armError is why Enforce could not bind the query set to a caller; it
	// surfaces at execution so an unarmed operation never runs unchecked.
	armError error
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

// Scope returns the permission scope the query is checked in: the request's tenant
// domain for a domain-scoped resource, the global scope otherwise. It is set by
// EnableUserPermissionEnforcement, which every generated handler runs at decode, so
// application query logic — a computed resource's List and Read — partitions its
// rows on the same value the permission check ran against.
func (q *QuerySet[Resource]) Scope() accesstypes.Scope {
	return q.scope
}

// User returns the identity the permission check ran as: the session's user, the
// viewed person under a view-as session, or the real actor under an act-as-role
// session. A computed resource's List or Read function reads it to yield the caller's
// own row without reaching into the session; empty when no check was bound.
func (q *QuerySet[Resource]) User() accesstypes.User {
	if q.userPermissions == nil {
		return ""
	}

	return q.userPermissions.User()
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
	if q.armError != nil {
		return q.armError
	}
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

		if err := q.checkQueryFieldsReadable(ctx, q.resourceSet, q.userPermissions); err != nil {
			return err
		}
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

// queryFields returns the fields the request orders or filters by, each once.
func (q *QuerySet[Resource]) queryFields() []accesstypes.Field {
	fields := make([]accesstypes.Field, 0, len(q.sortFields)+len(q.filterFields))
	for _, sf := range q.sortFields {
		if !slices.Contains(fields, accesstypes.Field(sf.Field)) {
			fields = append(fields, accesstypes.Field(sf.Field))
		}
	}
	for _, field := range q.filterFields {
		if !slices.Contains(fields, field) {
			fields = append(fields, field)
		}
	}

	return fields
}

// queryColumns returns the fields whose columns the statement names outside
// the select list: the read order (the request's sort or the declared default,
// then the key) and the filter's fields, each once.
func (q *QuerySet[Resource]) queryColumns() []accesstypes.Field {
	order := q.Order()
	fields := make([]accesstypes.Field, 0, len(order)+len(q.filterFields))
	for _, sf := range order {
		if !slices.Contains(fields, accesstypes.Field(sf.Field)) {
			fields = append(fields, accesstypes.Field(sf.Field))
		}
	}
	for _, field := range q.filterFields {
		if !slices.Contains(fields, field) {
			fields = append(fields, field)
		}
	}

	return fields
}

// checkQueryFieldsReadable refuses a sort or filter over a field the caller
// cannot read at all. A denied field would let the caller infer its values
// from the order or the membership of the result. A conditionally granted
// field passes: the query runs over the visible projection, where a masked
// cell is NULL (see read_rendering.go), so its decision is carried for the
// rendering. The exempt primary key follows the resource-level grant already
// checked.
func (q *QuerySet[Resource]) checkQueryFieldsReadable(ctx context.Context, rSet *Set[Resource], userPermissions UserPermissions) error {
	decisions, names, err := q.queryFieldDecisions(ctx, rSet, userPermissions)
	if err != nil {
		return err
	}
	for res, field := range names {
		if decisions[res].IsDenied() {
			return httpio.NewForbiddenMessagef("scope (%s), user (%s) cannot sort or filter on %s: (%s) on %s is denied", q.scope, userPermissions.User(), q.jsonName(field), q.requiredPermission, res)
		}
	}
	q.carryConditionalDecisions(decisions)

	return nil
}

// checkQueryFieldsGranted is the computed resource's readability rule: a sort
// or filter field needs the caller's unconditional grant, because the handler
// orders and filters the body's rows by the field's real values and no
// statement renders a visible projection over them.
func (q *QuerySet[Resource]) checkQueryFieldsGranted(ctx context.Context, rSet *Set[Resource], userPermissions UserPermissions) error {
	decisions, names, err := q.queryFieldDecisions(ctx, rSet, userPermissions)
	if err != nil {
		return err
	}
	for res, field := range names {
		if !decisions[res].IsGranted() {
			return httpio.NewForbiddenMessagef("scope (%s), user (%s) cannot sort or filter on %s: (%s) on %s must be granted unconditionally", q.scope, userPermissions.User(), q.jsonName(field), q.requiredPermission, res)
		}
	}

	return nil
}

// queryFieldDecisions checks the grant-bearing sort and filter fields in one
// call and returns the decisions with each resource's field name.
func (q *QuerySet[Resource]) queryFieldDecisions(ctx context.Context, rSet *Set[Resource], userPermissions UserPermissions) (accesstypes.Decisions, map[accesstypes.Resource]accesstypes.Field, error) {
	fields := q.queryFields()
	resources := make([]accesstypes.Resource, 0, len(fields))
	names := make(map[accesstypes.Resource]accesstypes.Field, len(fields))
	for _, field := range fields {
		if rSet.PermissionRequired(field, q.requiredPermission) {
			res := rSet.Resource(field)
			resources = append(resources, res)
			names[res] = field
		}
	}
	if len(resources) == 0 {
		return nil, nil, nil
	}

	decisions, err := userPermissions.Check(ctx, q.env, q.scope, q.requiredPermission, resources...)
	if err != nil {
		return nil, nil, errors.Wrap(err, "resource.UserPermissions.Check()")
	}

	return decisions, names, nil
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

// Order returns the total order the list is read in: the request's sort fields,
// or the resource's declared order when the request states none, followed by the
// primary-key fields not already named, ascending. On a hand-built QuerySet it is
// exactly the sort the caller set.
func (q *QuerySet[Resource]) Order() []SortField {
	order := q.sortFields
	if len(order) == 0 {
		order = q.defaultOrder
	}
	order = slices.Clone(order)
	for _, key := range q.keyFields {
		if !slices.ContainsFunc(order, func(sf SortField) bool { return sf.Field == string(key) }) {
			order = append(order, SortField{Field: string(key), Direction: SortAscending})
		}
	}

	return order
}

// readOrder is the order the statement reads rows in: the total order, or its
// reverse when the request walks back to the previous page, whose rows the
// handler reverses again before encoding.
func (q *QuerySet[Resource]) readOrder() []SortField {
	if q.cursor != nil && q.cursor.Direction == pagePrev {
		return flipped(q.Order())
	}

	return q.Order()
}

// orderColumn is one read-order term as the statement names it: the quoted
// column or its visible-projection override, and whether the term can be NULL.
type orderColumn struct {
	sql      string
	nullable bool
	meta     dbFieldMetadata
}

// orderColumn resolves one sort field to the expression the statement orders
// and pages by. A conditional field with a query override sorts on the override
// and is nullable regardless of its Go type: its masked cells are NULL.
func (q *QuerySet[Resource]) orderColumn(dbType DBType, rendered *renderedReadConditions, sf SortField) (orderColumn, error) {
	dbField, ok := q.rMeta.dbFieldMap(dbType)[accesstypes.Field(sf.Field)]
	if !ok {
		return orderColumn{}, errors.Newf("sort field '%s' not found in resource metadata for query", sf.Field)
	}
	if override := rendered.queryOverride(accesstypes.Field(sf.Field)); override != "" {
		return orderColumn{sql: override, nullable: true, meta: dbField}, nil
	}

	var quoted string
	switch dbType {
	case SpannerDBType:
		quoted = fmt.Sprintf("`%s`", dbField.ColumnName)
	case PostgresDBType:
		quoted = fmt.Sprintf(`"%s"`, dbField.ColumnName)
	default:
		return orderColumn{}, errors.Newf("unsupported dbType for sorting: %s", dbType)
	}

	return orderColumn{sql: quoted, nullable: isNullableType(dbField.fieldType), meta: dbField}, nil
}

// buildOrderByClause renders the ORDER BY for the QuerySet's read order. A
// nullable column states its NULL placement — NULLS LAST ascending, NULLS FIRST
// descending — because the two databases default differently and a page walk
// needs one order.
func (q *QuerySet[Resource]) buildOrderByClause(dbType DBType, rendered *renderedReadConditions) (string, error) {
	order := q.readOrder()
	orderByParts := make([]string, 0, len(order))
	for _, sf := range order {
		column, err := q.orderColumn(dbType, rendered, sf)
		if err != nil {
			return "", err
		}

		orderByParts = append(orderByParts, column.sql+" "+orderDirectionSQL(sf.Direction, column.nullable))
	}
	if len(orderByParts) == 0 {
		return "", nil
	}

	return "ORDER BY " + strings.Join(orderByParts, ", "), nil
}

// cursorPredicate renders the request cursor's position as a predicate over the
// read order: the rows strictly after the boundary row. Empty on a first page.
func (q *QuerySet[Resource]) cursorPredicate(dbType DBType, registry *paramRegistry, rendered *renderedReadConditions) (string, error) {
	if q.cursor == nil {
		return "", nil
	}

	order := q.readOrder()
	if len(q.cursor.Keys) != len(order) {
		return "", errInvalidCursor
	}

	terms := make([]cursorTerm, 0, len(order))
	for i, sf := range order {
		column, err := q.orderColumn(dbType, rendered, sf)
		if err != nil {
			return "", err
		}
		terms = append(terms, cursorTerm{
			column:    column.sql,
			direction: sf.Direction,
			nullable:  column.nullable,
			fieldType: column.meta.fieldType,
			boundary:  q.cursor.Keys[i],
		})
	}

	predicate, err := renderCursorPredicate(terms, registry)
	if err != nil {
		return "", errors.Wrap(err, "renderCursorPredicate()")
	}

	return predicate, nil
}

// orderDirectionSQL renders one ORDER BY term's direction, with the NULL
// placement a nullable column needs for the order to be the same on every
// database.
func orderDirectionSQL(direction SortDirection, nullable bool) string {
	if direction == SortDescending {
		if nullable {
			return "DESC NULLS FIRST"
		}

		return "DESC"
	}
	if nullable {
		return "ASC NULLS LAST"
	}

	return "ASC"
}

// isNullableType reports whether a resource field's Go type can hold a database
// NULL: a pointer, or one of the Null* wrapper structs (spanner.NullString,
// ccc.NullUUID, …), recognized by their Valid flag.
func isNullableType(t reflect.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind() {
	case reflect.Pointer, reflect.Interface:
		return true
	case reflect.Struct:
		valid, ok := t.FieldByName("Valid")

		return ok && valid.Type.Kind() == reflect.Bool
	default:
		return false
	}
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

// filterColumnExpressions maps the filter's column names to their
// visible-projection overrides, for the SQL generator to consult where it
// renders a condition's column.
func (q *QuerySet[Resource]) filterColumnExpressions(dbType DBType, rendered *renderedReadConditions) (map[string]string, error) {
	if rendered == nil || len(rendered.queryOverrides) == 0 {
		return nil, nil
	}

	dbFields := q.rMeta.dbFieldMap(dbType)
	expressions := make(map[string]string, len(q.filterFields))
	for _, field := range q.filterFields {
		override := rendered.queryOverride(field)
		if override == "" {
			continue
		}
		dbField, ok := dbFields[field]
		if !ok {
			return nil, errors.Newf("filter field %s not found in db struct", field)
		}
		expressions[dbField.ColumnName] = override
	}

	return expressions, nil
}

func (q *QuerySet[Resource]) astWhereClause(dbType DBType, filterAst ExpressionNode, rendered *renderedReadConditions) (*Statement, error) {
	expressions, err := q.filterColumnExpressions(dbType, rendered)
	if err != nil {
		return nil, err
	}

	switch dbType {
	case SpannerDBType:
		gen := NewSpannerGenerator()
		gen.setColumnExpressions(expressions)
		sql, params, err := gen.GenerateSQL(filterAst)
		if err != nil {
			return nil, errors.Wrap(err, "SpannerGenerator.GenerateSQL()")
		}

		return &Statement{SQL: "WHERE " + sql, Params: params}, nil
	case PostgresDBType:
		gen := NewPostgreSQLGenerator()
		gen.setColumnExpressions(expressions)
		sql, params, err := gen.GenerateSQL(filterAst)
		if err != nil {
			return nil, errors.Wrap(err, "PostgreSQLGenerator.GenerateSQL()")
		}

		return &Statement{SQL: "WHERE " + sql, Params: params}, nil
	default:
		return nil, errors.Newf("unsupported dbType: %s", dbType)
	}
}

// where translates the the fields to database struct tags in databaseType when building the where clause
func (q *QuerySet[Resource]) where(dbType DBType, filterAst ExpressionNode, rendered *renderedReadConditions) (*Statement, error) {
	if filterAst != nil {
		return q.astWhereClause(dbType, filterAst, rendered)
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
func (q *QuerySet[Resource]) whereWithPredicates(dbType DBType, filterAst ExpressionNode, tenancy string, rendered *renderedReadConditions, cursorPredicate string) (*Statement, error) {
	where, err := q.where(dbType, filterAst, rendered)
	if err != nil {
		return nil, errors.Wrap(err, "patcher.Where()")
	}

	predicates := []string{tenancy}
	if rendered != nil && rendered.rowPredicate != "" {
		predicates = append(predicates, rendered.rowPredicate)
	}
	// The cursor's position sits last: it narrows the rows the permission and
	// filter predicates admit to the page after the boundary.
	predicates = append(predicates, cursorPredicate)
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

	cursorPredicate, err := q.cursorPredicate(dbType, registry, rendered)
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.cursorPredicate()")
	}

	where, err := q.whereWithPredicates(dbType, filterAst, tenancy, rendered, cursorPredicate)
	if err != nil {
		return nil, err
	}

	orderByClause, limitClause, err := q.pageClauses(dbType, rendered)
	if err != nil {
		return nil, err
	}

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
			%s`, withClause, columns, query, where.SQL, orderByClause, limitClause,
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

// pageClauses renders the statement's ORDER BY and LIMIT.
func (q *QuerySet[Resource]) pageClauses(dbType DBType, rendered *renderedReadConditions) (orderByClause, limitClause string, err error) {
	orderByClause, err = q.buildOrderByClause(dbType, rendered)
	if err != nil {
		return "", "", errors.Wrap(err, "QuerySet.buildOrderByClause()")
	}
	limitClause, err = q.limitClause()
	if err != nil {
		return "", "", err
	}

	return orderByClause, limitClause, nil
}

// limitClause renders the statement's LIMIT. A decoded page fetches one row
// more than its size, so the handler learns whether a next page exists without
// a second query (Page.Add drops the extra row); limit=all fetches everything;
// a hand-built QuerySet reads exactly the limit it set. A cursor the decoder
// never bound to a scope fails closed here rather than silently serving the
// first page.
func (q *QuerySet[Resource]) limitClause() (string, error) {
	if q.page == nil {
		if q.limit != nil {
			return fmt.Sprintf("LIMIT %d", *q.limit), nil
		}

		return "", nil
	}
	if q.page.token != "" && q.cursor == nil {
		return "", errors.New("resource.QuerySet: the request carries a cursor that was never bound to a scope; decode with Decode, not DecodeWithoutPermissions")
	}
	if q.page.all {
		return "", nil
	}

	return fmt.Sprintf("LIMIT %d", q.page.size+1), nil
}

// bindCursor opens the request's cursor under the decoder's key and checks its
// fingerprint against the query being decoded: the resource, the checked scope,
// the filter, the total order, and the page size. A first page has nothing to
// bind. A resource with no declared order and no requested sort lists in
// primary-key order, which carries no meaning to page through, so a cursor on
// it is refused: paging further requires a sort.
func (q *QuerySet[Resource]) bindCursor(scope accesstypes.Scope) error {
	if q.page == nil || q.page.token == "" {
		return nil
	}
	if q.cursorKey == nil {
		return errors.New("resource.QuerySet: the request carries a cursor but the decoder has no cursor key; pass resource.NewCursorKey(cookieKey) to WithCursorKey")
	}
	if !q.issuesCursors() {
		return httpio.NewBadRequestMessage("this resource lists in primary-key order when no sort is given; paging past the first page requires a sort")
	}

	c, err := q.cursorKey.open(q.page.token)
	if err != nil {
		return err
	}
	if c.Query != q.queryHash(scope) {
		return errInvalidCursor
	}
	q.cursor = &c

	return nil
}

// issuesCursors reports whether the list has an order worth walking: a
// requested sort or a declared default. Primary-key order alone is total but
// meaningless, so such a list serves first pages only.
func (q *QuerySet[Resource]) issuesCursors() bool {
	return len(q.sortFields) > 0 || len(q.defaultOrder) > 0
}

// queryHash fingerprints this query for its cursors.
func (q *QuerySet[Resource]) queryHash(scope accesstypes.Scope) string {
	return queryHash(q.Resource(), scope, q.filterString, q.Order(), q.page.limitString())
}

// Count executes the query's WHERE under SELECT COUNT(*) and returns the number
// of rows it admits: the same filter, tenancy, and read-rule predicates as List,
// with no order, page, or cursor. The generated list handler runs it before the
// page query when the request asks count=true.
func (q *QuerySet[Resource]) Count(ctx context.Context, txn ReadOnlyTransaction) (int64, error) {
	r := newReader[Resource](txn)
	if err := q.checkPermissions(ctx, r.DBType()); err != nil {
		return 0, err
	}

	stmt, err := q.countStmt(r.DBType())
	if err != nil {
		return 0, errors.Wrap(err, "QuerySet.countStmt()")
	}

	total, err := r.Count(ctx, stmt)
	if err != nil {
		return 0, errors.Wrapf(err, "Reader[%s].Count()", q.Resource())
	}

	return total, nil
}

// countStmt builds the COUNT(*) statement over the same WHERE as stmt.
func (q *QuerySet[Resource]) countStmt(dbType DBType) (*Statement, error) {
	filterAst, err := q.FilterAst(dbType)
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.FilterAst()")
	}

	plan, err := q.readConditionPlan()
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.readConditionPlan()")
	}

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

	where, err := q.whereWithPredicates(dbType, filterAst, tenancy, rendered, "")
	if err != nil {
		return nil, err
	}

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
			SELECT COUNT(*)
			FROM %s
			%s`, withClause, query, where.SQL,
	)

	return &Statement{SQL: sql, Params: where.Params}, nil
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
