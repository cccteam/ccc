package resource

// The no-conditions differential (design plan §11, invariant 10): an
// application whose engine holds no conditions must render exactly what the
// renderer rendered before enforcement existed. The oracle is the unenforced
// QuerySet — the same decoded request with no permission set bound, in the
// same partition — so what the differential isolates is the enforcement
// machinery: the decisions, the read-condition plan, the capability plan, the
// write-check groups, and their rendering. Under all-Granted decisions every
// one of those must be a no-op, byte for byte.
//
// Random query shapes cover projection, filter, sort, cursor page in both
// directions, global and partitioned scope, a keyed read, and the capability
// envelope; random mutation shapes cover create, update, and delete over a
// bare-column and a join-path tenancy binding. The generators are seeded, so
// a failing case reproduces and prints its request.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/spxscan/spxapi"
	"github.com/google/go-cmp/cmp"
)

const differentialResourceName = accesstypes.Resource("differentialResources")

// differentialResource carries every column shape the statement builder
// distinguishes: a UUID key, a string tenant key, text, integer, nullable
// float, boolean, timestamp, and nullable text.
type differentialResource struct {
	ID       ccc.UUID  `spanner:"Id"`
	Station  string    `spanner:"Station"`
	Name     string    `spanner:"Name"`
	Rank     int64     `spanner:"Rank"`
	Score    *float64  `spanner:"Score"`
	Active   bool      `spanner:"Active"`
	Launched time.Time `spanner:"Launched"`
	Note     *string   `spanner:"Note"`
}

func (differentialResource) Resource() accesstypes.Resource { return differentialResourceName }

func (differentialResource) DefaultConfig() Config { return Config{} }

// differentialListRequest is the read shape: the key exempt, the tenant key
// wire-closed, three indexed and two filterable columns, and one column that
// is neither.
type differentialListRequest struct {
	ID       ccc.UUID  `json:"id"       perm:"-"`
	Station  string    `json:"-"`
	Name     string    `json:"name"     index:"true"`
	Rank     int64     `json:"rank"     index:"true"`
	Score    *float64  `json:"score"    allow_filter:"true"`
	Active   bool      `json:"active"   allow_filter:"true"`
	Launched time.Time `json:"launched" index:"true"`
	Note     *string   `json:"note"`
}

// differentialPatchRequest is the write shape: the tenant key wire-closed.
type differentialPatchRequest struct {
	Station  string    `json:"-"`
	Name     string    `json:"name"`
	Rank     int64     `json:"rank"`
	Score    *float64  `json:"score"`
	Active   bool      `json:"active"`
	Launched time.Time `json:"launched"`
	Note     *string   `json:"note"`
}

// differentialCollection wires the fixture resource with a bare-column tenancy
// binding and the tenancy child (tenancy_test.go) with a join-path binding.
func differentialCollection(t *testing.T) *GeneratedCollection {
	t.Helper()

	g, err := NewGeneratedCollection(CollectionData{Resources: []CollectionResource{
		{
			Name:        differentialResourceName,
			Scope:       accesstypes.DomainPermissionScope,
			Permissions: []accesstypes.Permission{accesstypes.List, accesstypes.Read, accesstypes.Create, accesstypes.Update, accesstypes.Delete},
			Attributes: []AttributeData{
				{Name: "name", Column: "Name", Type: AttributeTypeString},
				{Name: "rank", Column: "Rank", Type: AttributeTypeNumber},
				{Name: "active", Column: "Active", Type: AttributeTypeBool},
				{Name: "launched", Column: "Launched", Type: AttributeTypeTimestamp},
			},
			Domain: &DomainBindingData{Column: "Station"},
		},
		{
			Name:        tenancyChildTestResource,
			Scope:       accesstypes.DomainPermissionScope,
			Permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update, accesstypes.Delete},
			Domain:      &DomainBindingData{Column: "ParentId", Path: []BindingHop{{Table: "Parents", JoinColumn: "Id", Column: "StationId"}}},
		},
	}})
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	return g
}

// differentialCursorKey is a fixed sealing key, so a failing case's cursor
// reproduces.
func differentialCursorKey(t *testing.T) *CursorKey {
	t.Helper()

	key, err := NewCursorKey(base64.StdEncoding.EncodeToString([]byte("differential-property-cookie-key")))
	if err != nil {
		t.Fatalf("NewCursorKey() error = %v", err)
	}

	return key
}

// differentialPaging declares a default order, so a sort-less list still
// issues cursors, and no maximum, so limit=all is admitted.
var differentialPaging = Paging{Order: []SortField{{Field: "Name", Direction: SortAscending}}, DefaultLimit: 20}

// differentialScopes are the partitions a shape may run in.
var differentialScopes = []accesstypes.Scope{
	accesstypes.GlobalScope(),
	accesstypes.DomainScope("station-alpha"),
	accesstypes.DomainScope("station-beta"),
}

// readShape is one random read request.
type readShape struct {
	// keyed marks a Read by primary key; otherwise the shape is a List.
	keyed bool
	// columns are the requested JSON names; nil asks for the default
	// projection.
	columns []string
	filter  string
	// sort holds json[:direction] terms.
	sort []string
	// limit is "", a page size, or all.
	limit string
	// cursor walks from a boundary row in the direction; nil on a first page.
	cursor *pageDirection
	// nullBoundary puts the nullable order columns' boundary in the NULL
	// region.
	nullBoundary bool
	count        bool
	capabilities []accesstypes.Permission
	scope        accesstypes.Scope
}

// String renders the shape the way a failure reports it.
func (s *readShape) String() string {
	kind := "list"
	if s.keyed {
		kind = "read"
	}
	cursor := "first page"
	if s.cursor != nil {
		cursor = fmt.Sprintf("cursor direction %d (null boundary %v)", *s.cursor, s.nullBoundary)
	}

	return fmt.Sprintf("%s in %s: %s, %s", kind, s.scope, s.query().Encode(), cursor)
}

// query renders the shape's query parameters, cursor excluded.
func (s *readShape) query() url.Values {
	values := url.Values{}
	if len(s.columns) > 0 {
		values.Set(columnsParam, strings.Join(s.columns, ","))
	}
	if s.filter != "" {
		values.Set(filterParam, s.filter)
	}
	if len(s.sort) > 0 {
		values.Set(sortParam, strings.Join(s.sort, ","))
	}
	if s.limit != "" {
		values.Set(limitParam, s.limit)
	}
	if s.count {
		values.Set(countParam, trueStr)
	}
	if len(s.capabilities) > 0 {
		names := make([]string, 0, len(s.capabilities))
		for _, perm := range s.capabilities {
			names = append(names, string(perm))
		}
		values.Set(capabilitiesParam, strings.Join(names, ","))
	}

	return values
}

// The read shape's vocabulary: the JSON names the request type exposes, by
// role.
var (
	differentialColumns   = []string{"id", "name", "rank", "score", "active", "launched", "note"}
	differentialSortable  = []string{"id", "name", "rank", "score", "active", "launched", "note"}
	differentialIndexed   = []string{"name", "rank", "launched"}
	differentialFiltered  = []string{"name", "rank", "score", "active", "launched"}
	differentialCapabilty = []accesstypes.Permission{accesstypes.Update, accesstypes.Delete, accesstypes.Create, accesstypes.Execute}
)

// genReadShape draws one read request.
func genReadShape(rng *rand.Rand) *readShape {
	shape := &readShape{
		keyed: rng.IntN(5) == 0,
		scope: pickOne(rng, differentialScopes),
	}
	if rng.IntN(2) == 0 {
		shape.columns = subset(rng, differentialColumns, 1)
	}
	if rng.IntN(3) > 0 {
		shape.capabilities = subset(rng, differentialCapabilty, 0)
	}
	if shape.keyed {
		return shape
	}

	if rng.IntN(2) == 0 {
		shape.filter = genFilter(rng)
	}
	sortFields := subset(rng, differentialSortable, 0)
	for _, field := range sortFields[:min(len(sortFields), rng.IntN(4))] {
		switch rng.IntN(3) {
		case 0:
			shape.sort = append(shape.sort, field)
		case 1:
			shape.sort = append(shape.sort, field+":asc")
		default:
			shape.sort = append(shape.sort, field+":desc")
		}
	}
	switch rng.IntN(4) {
	case 0:
		shape.limit = strconv.Itoa(1 + rng.IntN(100))
	case 1:
		shape.limit = allLimit
	}
	// A paged request needs an order (requireOrder); an unsorted shape reads
	// the whole list.
	if len(shape.sort) == 0 {
		shape.limit = allLimit
	}
	switch {
	case shape.limit != allLimit && rng.IntN(3) == 0:
		direction := pageNext
		if rng.IntN(2) == 0 {
			direction = pagePrev
		}
		shape.cursor = &direction
		shape.nullBoundary = rng.IntN(3) == 0
	case rng.IntN(3) == 0:
		shape.count = true
	}

	return shape
}

// genFilter draws a filter over the filterable fields, always touching an
// indexed one (the table filter rule), with AND, OR, and one optional group.
func genFilter(rng *rand.Rand) string {
	conditions := []string{genCondition(rng, pickOne(rng, differentialIndexed))}
	for range rng.IntN(3) {
		conditions = append(conditions, genCondition(rng, pickOne(rng, differentialFiltered)))
	}
	rng.Shuffle(len(conditions), func(i, j int) { conditions[i], conditions[j] = conditions[j], conditions[i] })

	joiner := func() string {
		if rng.IntN(2) == 0 {
			return ","
		}

		return "|"
	}

	if len(conditions) >= 3 && rng.IntN(2) == 0 {
		grouped := "(" + conditions[0] + joiner() + conditions[1] + ")"
		conditions = append([]string{grouped}, conditions[2:]...)
	}

	var b strings.Builder
	for i, condition := range conditions {
		if i > 0 {
			b.WriteString(joiner())
		}
		b.WriteString(condition)
	}

	return b.String()
}

// genCondition draws one filter condition typed to the field.
func genCondition(rng *rand.Rand, field string) string {
	relational := []string{"eq", "ne", "gt", "lt", "gte", "lte"}
	switch field {
	case "name":
		values := []string{"alpha", "beta", "gamma", "x"}
		switch rng.IntN(4) {
		case 0:
			return fmt.Sprintf("name:%s:%s", pickOne(rng, []string{"eq", "ne"}), pickOne(rng, values))
		case 1:
			return fmt.Sprintf("name:%s:(%s)", pickOne(rng, []string{"in", "notin"}), strings.Join(subset(rng, values, 1), ","))
		default:
			return "name:" + pickOne(rng, []string{isnullStr, isnotnullStr})
		}
	case "rank":
		if rng.IntN(3) == 0 {
			return fmt.Sprintf("rank:%s:(%d,%d)", pickOne(rng, []string{"in", "notin"}), rng.IntN(10), 10+rng.IntN(10))
		}

		return fmt.Sprintf("rank:%s:%d", pickOne(rng, relational), rng.IntN(100))
	case "score":
		if rng.IntN(3) == 0 {
			return "score:" + pickOne(rng, []string{isnullStr, isnotnullStr})
		}

		return fmt.Sprintf("score:%s:%s", pickOne(rng, relational), pickOne(rng, []string{"0", "1.5", "-2.25", "99"}))
	case "active":
		return "active:eq:" + strconv.FormatBool(rng.IntN(2) == 0)
	default:
		return fmt.Sprintf("launched:%s:%s", pickOne(rng, relational), pickOne(rng, []string{"2026-01-02T15:04:05Z", "1999-12-31T23:59:59Z"}))
	}
}

// boundaryText draws a cursor boundary value for an order field, nil in the
// NULL region where the column admits one.
func boundaryText(rng *rand.Rand, field string, nullBoundary bool) *string {
	text := func(s string) *string { return &s }
	switch field {
	case "ID":
		return text(ccc.Must(ccc.NewUUID()).String())
	case "Name":
		return text(pickOne(rng, []string{"m", "alpha", ""}))
	case "Rank":
		return text(strconv.Itoa(rng.IntN(50)))
	case "Score":
		if nullBoundary {
			return nil
		}

		return text("2.5")
	case "Active":
		return text(strconv.FormatBool(rng.IntN(2) == 0))
	case "Launched":
		return text(time.Date(2026, 3, 4, 5, 6, 7, 8, time.UTC).Format(time.RFC3339Nano))
	default:
		if nullBoundary {
			return nil
		}

		return text("n")
	}
}

func pickOne[T any](rng *rand.Rand, values []T) T {
	return values[rng.IntN(len(values))]
}

// subset draws a random subset of at least atLeast members, in a random
// order.
func subset[T ~string](rng *rand.Rand, values []T, atLeast int) []T {
	chosen := make(map[T]struct{}, len(values))
	for _, v := range values {
		if rng.IntN(2) == 0 {
			chosen[v] = struct{}{}
		}
	}
	for len(chosen) < atLeast {
		chosen[pickOne(rng, values)] = struct{}{}
	}

	out := slices.Sorted(maps.Keys(chosen))
	rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })

	return out
}

// renderedRead is the observable output of one read: the page statement and
// the count statement, SQL and parameters.
type renderedRead struct {
	SQL         string
	Params      map[string]any
	Where       string
	Masked      string
	CountSQL    string
	CountParams map[string]any
}

// renderRead runs the permission stage and builds both statements.
func renderRead(t *testing.T, qSet *QuerySet[differentialResource]) (renderedRead, *Statement) {
	t.Helper()

	if err := qSet.checkPermissions(t.Context(), SpannerDBType); err != nil {
		t.Fatalf("checkPermissions() error = %v", err)
	}
	stmt, err := qSet.stmt(SpannerDBType)
	if err != nil {
		t.Fatalf("stmt() error = %v", err)
	}
	count, err := qSet.countStmt(SpannerDBType)
	if err != nil {
		t.Fatalf("countStmt() error = %v", err)
	}

	return renderedRead{
		SQL:         stmt.SQL,
		Params:      stmt.Params,
		Where:       stmt.resolvedWhereClause,
		Masked:      stmt.maskedNamesColumn,
		CountSQL:    count.SQL,
		CountParams: count.Params,
	}, stmt
}

// differentialDecoders builds the list and read decoders over the fixture,
// wired to the collection, the paging contract, and the cursor key.
func differentialDecoders(t *testing.T, collection *GeneratedCollection, key *CursorKey) (list, read *QueryDecoder[differentialResource, differentialListRequest]) {
	t.Helper()

	build := func(perm accesstypes.Permission) *QueryDecoder[differentialResource, differentialListRequest] {
		resSet, err := NewSet[differentialResource, differentialListRequest](perm)
		if err != nil {
			t.Fatalf("NewSet() error = %v", err)
		}
		decoder, err := NewQueryDecoder[differentialResource, differentialListRequest](resSet)
		if err != nil {
			t.Fatalf("NewQueryDecoder() error = %v", err)
		}
		decoder.collection = collection

		return decoder.WithPaging(differentialPaging).WithCursorKey(key)
	}

	return build(accesstypes.List), build(accesstypes.Read)
}

// TestQuerySet_noConditionsDifferential: for random read shapes, the statement
// built with enforcement enabled and every decision Granted equals the
// statement built without enforcement in the same partition — SQL and
// parameters byte for byte, for the page and the count — and a capability
// request renders the capability-free statement with no data-dependent group.
func TestQuerySet_noConditionsDifferential(t *testing.T) {
	t.Parallel()

	collection := differentialCollection(t)
	key := differentialCursorKey(t)
	listDecoder, readDecoder := differentialDecoders(t, collection, key)
	rowID := mustUUIDFromString("8a6570c8-1e51-4870-9def-3f68d0447d09")

	rng := rand.New(rand.NewPCG(20260911, 3))
	for i := range 600 {
		shape := genReadShape(rng)
		decoder := listDecoder
		if shape.keyed {
			decoder = readDecoder
		}

		target := "/?" + shape.query().Encode()
		if shape.cursor != nil {
			target += "&" + cursorParam + "=" + sealShapeCursor(t, rng, decoder, shape, target)
		}

		// The enforced side: decoded against the caller, every decision
		// Granted.
		enforced, err := decoder.Decode(httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody), renderStubPermissions{}, shape.scope)
		if err != nil {
			t.Fatalf("case %d (%s): Decode() error = %v", i, shape, err)
		}

		// The oracle: the same request with no permission set bound, in the
		// same partition, its cursor bound to the same scope.
		unenforced, err := decoder.DecodeWithoutPermissions(httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody))
		if err != nil {
			t.Fatalf("case %d (%s): DecodeWithoutPermissions() error = %v", i, shape, err)
		}
		unenforced.scope = shape.scope
		if err := unenforced.bindCursor(shape.scope); err != nil {
			t.Fatalf("case %d (%s): bindCursor() error = %v", i, shape, err)
		}

		if shape.keyed {
			enforced.SetKey("ID", rowID)
			unenforced.SetKey("ID", rowID)
		}

		got, stmt := renderRead(t, enforced)
		want, _ := renderRead(t, unenforced)
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("case %d (%s): enforced statement diverged from the unenforced oracle (-unenforced +enforced):\n%s", i, shape, diff)
		}

		// The capability plan settles from grants alone: no checks column,
		// no group, every affordance structural.
		if stmt.capabilityPlan != nil {
			if stmt.capabilityPlan.checksColumn != "" || stmt.capabilityPlan.groups != 0 {
				t.Fatalf("case %d (%s): capability plan rendered %d data-dependent groups under all-Granted decisions", i, shape, stmt.capabilityPlan.groups)
			}
			for _, planned := range stmt.capabilityPlan.perms {
				if planned.group >= 0 {
					t.Fatalf("case %d (%s): %s capability depends on group %d under all-Granted decisions", i, shape, planned.perm, planned.group)
				}
				for _, field := range planned.fields {
					if field.group >= 0 {
						t.Fatalf("case %d (%s): %s capability for %s depends on group %d under all-Granted decisions", i, shape, planned.perm, field.jsonName, field.group)
					}
				}
			}
		}
	}
}

// sealShapeCursor issues a cursor for the shape's walk: the query hash and the
// total order come from decoding the cursor-less request, the boundary values
// are drawn per order field.
func sealShapeCursor(t *testing.T, rng *rand.Rand, decoder *QueryDecoder[differentialResource, differentialListRequest], shape *readShape, target string) string {
	t.Helper()

	first, err := decoder.DecodeWithoutPermissions(httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody))
	if err != nil {
		t.Fatalf("DecodeWithoutPermissions(%s) error = %v", target, err)
	}

	order := first.Order()
	keys := make([]*string, 0, len(order))
	for _, sf := range order {
		keys = append(keys, boundaryText(rng, sf.Field, shape.nullBoundary))
	}

	token, err := first.cursorKey.seal(cursor{Query: first.queryHash(shape.scope), Direction: *shape.cursor, Keys: keys})
	if err != nil {
		t.Fatalf("seal() error = %v", err)
	}

	return token
}

// writeShape is one random mutation.
type writeShape struct {
	// child selects the join-path tenancy fixture over the bare-column one.
	child bool
	op    OperationType
	body  map[string]any
	scope accesstypes.Scope
}

func (s *writeShape) String() string {
	res := differentialResourceName
	if s.child {
		res = tenancyChildTestResource
	}
	body, _ := json.Marshal(s.body)

	return fmt.Sprintf("%s %s in %s: %s", s.op, res, s.scope, body)
}

// genWriteShape draws one mutation: a create, update, or delete with a random
// body over one of the two fixtures, in a random partition.
func genWriteShape(rng *rand.Rand) *writeShape {
	shape := &writeShape{
		child: rng.IntN(3) == 0,
		op:    pickOne(rng, []OperationType{OperationCreate, OperationUpdate, OperationDelete}),
		scope: pickOne(rng, differentialScopes),
		body:  map[string]any{},
	}
	if shape.op == OperationDelete {
		return shape
	}

	if shape.child {
		if shape.op == OperationCreate || rng.IntN(2) == 0 {
			shape.body["parentId"] = ccc.Must(ccc.NewUUID()).String()
		}
		if shape.op == OperationCreate && rng.IntN(2) == 0 || len(shape.body) == 0 {
			shape.body["note"] = "spare part"
		}

		return shape
	}

	fields := map[string]func() any{
		"name":     func() any { return pickOne(rng, []string{"alpha", "beta"}) },
		"rank":     func() any { return rng.IntN(100) },
		"score":    func() any { return 1.5 },
		"active":   func() any { return rng.IntN(2) == 0 },
		"launched": func() any { return "2026-01-02T15:04:05Z" },
		"note":     func() any { return "n" },
	}
	for _, name := range subset(rng, slices.Sorted(maps.Keys(fields)), 1) {
		shape.body[name] = fields[name]()
	}

	return shape
}

// renderedWrite is the observable output of one mutation: whether the
// tenancy obliges a check query and, if so, its SQL and parameters; otherwise
// the mutation buffered with no query at all.
type renderedWrite struct {
	NeedsQuery bool
	CheckSQL   string
	Params     map[string]any
	Buffered   []map[string]any
}

// noQueryTxn refuses every query: under all-Granted decisions with no
// partition obligation the write stages issue none.
type noQueryTxn struct {
	t        *testing.T
	buffered []map[string]any
}

func (x *noQueryTxn) DBType() DBType { return SpannerDBType }

func (x *noQueryTxn) SpannerReadOnlyTransaction() spxapi.Querier { return refusingQuerier{t: x.t} }

func (x *noQueryTxn) PostgresReadOnlyTransaction() any {
	panic("noQueryTxn.PostgresReadOnlyTransaction() should never be called")
}

func (x *noQueryTxn) BufferMap(_ PatchSetMetadata, patch map[string]any) error {
	x.buffered = append(x.buffered, patch)

	return nil
}

func (x *noQueryTxn) BufferStruct(PatchSetMetadata) error { return nil }

func (x *noQueryTxn) DataChangeEventIndex(accesstypes.Resource, string) int { return 0 }

type refusingQuerier struct {
	t *testing.T
}

func (q refusingQuerier) Query(_ context.Context, stmt spanner.Statement) *spanner.RowIterator {
	q.t.Fatalf("a check query was issued with nothing to check:\n%s", stmt.SQL)

	return nil
}

// TestPatchSet_noConditionsDifferential: for random mutations under
// all-Granted decisions, the write stages carry no condition group; without a
// partition obligation they buffer the mutation with no check query, and with
// one they issue the same locate (or insert-proof) query as the unenforced
// path — SQL and parameters byte for byte.
func TestPatchSet_noConditionsDifferential(t *testing.T) {
	t.Parallel()

	collection := differentialCollection(t)
	rowID := mustUUIDFromString("8a6570c8-1e51-4870-9def-3f68d0447d09")

	rng := rand.New(rand.NewPCG(20260911, 7))
	for i := range 400 {
		shape := genWriteShape(rng)

		var got, want renderedWrite
		if shape.child {
			got = renderWrite(t, decodeWrite[tenancyChildResource, tenancyChildCreateRequest](t, collection, shape, rowID, true))
			want = renderWrite(t, decodeWrite[tenancyChildResource, tenancyChildCreateRequest](t, collection, shape, rowID, false))
		} else {
			got = renderWrite(t, decodeWrite[differentialResource, differentialPatchRequest](t, collection, shape, rowID, true))
			want = renderWrite(t, decodeWrite[differentialResource, differentialPatchRequest](t, collection, shape, rowID, false))
		}

		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("case %d (%s): enforced mutation diverged from the unenforced oracle (-unenforced +enforced):\n%s", i, shape, diff)
		}
	}
}

// decodeWrite decodes the shape through the production decoder: armed against
// the caller with every decision Granted, or — the oracle — with no permission
// set, the same partition and collection, and the tenant key a hand-built
// partitioned create must carry itself.
func decodeWrite[Resource Resourcer, Request any](t *testing.T, collection *GeneratedCollection, shape *writeShape, rowID ccc.UUID, enforce bool) *PatchSet[Resource] {
	t.Helper()

	resSet, err := NewSet[Resource, Request](accesstypes.Create, accesstypes.Update, accesstypes.Delete)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	decoder, err := NewDecoder[Resource, Request](resSet)
	if err != nil {
		t.Fatalf("NewDecoder() error = %v", err)
	}
	decoder.collection = collection

	body, err := json.Marshal(shape.body)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	method, err := httpMethod(string(shape.op))
	if err != nil {
		t.Fatalf("httpMethod() error = %v", err)
	}
	req := httptest.NewRequestWithContext(t.Context(), method, "/", strings.NewReader(string(body)))
	perm := permissionFromType(shape.op)

	var patchSet *PatchSet[Resource]
	switch {
	case enforce && shape.op == OperationDelete:
		patchSet, err = decoder.DecodeOperation(&Operation{Type: OperationDelete, Req: req}, renderStubPermissions{}, shape.scope)
	case enforce:
		patchSet, err = decoder.Decode(req, renderStubPermissions{}, shape.scope, perm)
	case shape.op == OperationDelete:
		patchSet, err = decoder.DecodeOperationWithoutPermissions(&Operation{Type: OperationDelete, Req: req})
	default:
		patchSet, err = decoder.DecodeWithoutPermissions(req)
	}
	if err != nil {
		t.Fatalf("decoding %s error = %v", shape, err)
	}

	if !enforce {
		patchSet.querySet.scope = shape.scope
		patchSet.querySet.collection = collection
		if domain, ok := shape.scope.Domain(); ok && !shape.child && shape.op == OperationCreate {
			patchSet.Set("Station", string(domain))
		}
	}

	switch shape.op {
	case OperationCreate:
		patchSet.SetPatchType(CreatePatchType)
	case OperationUpdate:
		patchSet.SetPatchType(UpdatePatchType)
	case OperationDelete:
		patchSet.SetPatchType(DeletePatchType)
	}
	patchSet.SetKey("ID", rowID)

	return patchSet
}

// renderWrite runs the write stages' policy half and observes the outcome.
func renderWrite[Resource Resourcer](t *testing.T, patchSet *PatchSet[Resource]) renderedWrite {
	t.Helper()

	groups, err := patchSet.writeConditionGroups()
	if err != nil {
		t.Fatalf("writeConditionGroups() error = %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("writeConditionGroups() = %d groups under all-Granted decisions, want none", len(groups))
	}

	tenancy, err := patchSet.mutationTenancy()
	if err != nil {
		t.Fatalf("mutationTenancy() error = %v", err)
	}

	if tenancy.needsQuery() {
		stmt, err := patchSet.writeCheckStatement(SpannerDBType, groups, tenancy)
		if err != nil {
			t.Fatalf("writeCheckStatement() error = %v", err)
		}

		return renderedWrite{NeedsQuery: true, CheckSQL: stmt.SQL, Params: stmt.Params}
	}

	txn := &noQueryTxn{t: t}
	if err := patchSet.Buffer(t.Context(), txn); err != nil {
		t.Fatalf("Buffer() error = %v", err)
	}

	return renderedWrite{Buffered: txn.buffered}
}
