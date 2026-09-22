package computedresources

import (
	"context"
	"fmt"
	"iter"
	"math"
	"math/big"
	"slices"
	"strings"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/spxscan"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

type (
	// ServiceLedger is headquarters' per-sector rollup: open missions, fees outstanding on
	// them, and settlements made. It is list-only (the read handler is suppressed) and
	// global: the ledger is a headquarters concern.
	//
	// It is the PUSHDOWN computed resource: the body takes the request's filter on the
	// key and the name (Filter().Take), the total order (TakeSort), and the page bounds
	// (TakePage) into one SQL statement, so a fleet with ten thousand sectors pages as
	// cheaply as one with three. A filter on OpenMissions is left to the handler on
	// purpose: the sort stays the body's, the page does not (TakePage answers false), and
	// the handler pages over the rows the body yielded in its own order. The hazard board
	// keeps the in-memory fold as the contrast; the wire cannot tell which is which.
	//
	// LastReturnAt, the return of the sector's most recent sortie, is NULL where no sortie
	// has come home. Sorted by it, the ledger crosses the NULL boundary in Spanner's
	// placement (NULL first ascending, last descending) whether the body pages itself or
	// the handler pages over its order: the body's plain ORDER BY, its cursor predicate,
	// and the handler's boundary test all place NULL where the application's database
	// does, the same end as the tables beside it.
	//
	// Demonstrates: @computed, computed.pushdown, computed.take-sort, computed.take-page, computed.take-filter, computed.null-placement, @suppress, @order, @page, filter.validated-at-decode.
	//
	// @computed
	// @suppress(readHandler)
	// @order(FeesOutstanding desc)
	// @page(default: 25, max: 200)
	ServiceLedger struct {
		SectorID        string          `spanner:"SectorId"        allow_filter:"true"` // @primarykey
		Name            string          `spanner:"Name"            allow_filter:"true"`
		OpenMissions    int64           `spanner:"OpenMissions"    allow_filter:"true"`
		FeesOutstanding decimal.Decimal `spanner:"FeesOutstanding"`
		Settlements     decimal.Decimal `spanner:"Settlements"`
		LastReturnAt    *time.Time      `spanner:"LastReturnAt"`
	}
)

// Resource implements resource.Resourcer; computed resources declare their resource name
// by hand (there is no generated file to carry it).
func (ServiceLedger) Resource() accesstypes.Resource {
	return "ServiceLedgers"
}

// ledgerColumns maps the ledger's Go fields to the columns of the SQL below, so the sort
// and the cursor the request names in Go field terms render into the statement.
var ledgerColumns = map[string]string{
	"SectorID":        "SectorId",
	"Name":            "Name",
	"OpenMissions":    "OpenMissions",
	"FeesOutstanding": "FeesOutstanding",
	"Settlements":     "Settlements",
	lastReturnAt:      lastReturnAt,
}

// lastReturnAt is the ledger's one nullable field; its Go name and its column coincide.
const lastReturnAt = "LastReturnAt"

// ledgerNullable names the ledger columns that can be NULL. Their NULL region sits where
// Spanner puts it, first ascending and last descending: the placement the plain ORDER BY
// below produces, the cursor predicate admits, and the generated handler's own sort and
// boundary test use for this application.
var ledgerNullable = map[string]bool{
	lastReturnAt: true,
}

// ledgerSQL rolls the missions up per sector. Open missions are those not finished; the
// settlements are the completed missions' net; the last return is the latest sortie home
// on any of the sector's missions, NULL where none has returned. The sorties are rolled
// up per mission first, so a mission with several sorties still counts once.
const ledgerSQL = `SELECT s.Id AS SectorId, s.Name AS Name,
       COUNTIF(m.StatusId NOT IN ('completed', 'failed', 'stood_down')) AS OpenMissions,
       COALESCE(SUM(IF(m.StatusId NOT IN ('completed', 'failed', 'stood_down'), m.Fee, NUMERIC '0')), NUMERIC '0') AS FeesOutstanding,
       COALESCE(SUM(IF(m.StatusId = 'completed', m.Settlement, NUMERIC '0')), NUMERIC '0') AS Settlements,
       MAX(so.ReturnedAt) AS LastReturnAt
  FROM Sectors s
  LEFT JOIN Missions m ON m.SectorId = s.Id
  LEFT JOIN (SELECT MissionId, MAX(ReturnedAt) AS ReturnedAt FROM Sorties GROUP BY MissionId) so ON so.MissionId = m.Id
 GROUP BY s.Id, s.Name`

// ListServiceLedger computes one ledger row per sector in SQL, taking the filter on
// SectorId and Name, the sort, and the page into the statement. Whatever it does not
// take (a filter on OpenMissions, a request for every row, a count) the generated
// handler applies over the rows it yields, in the order the body gave them.
func ListServiceLedger(ctx context.Context, qSet *resource.QuerySet[ServiceLedger], client resource.Client, _ *Client) iter.Seq2[*ServiceLedger, error] {
	return func(yield func(*ServiceLedger, error) bool) {
		params := map[string]any{}
		var where []string
		for _, condition := range qSet.Filter().Take("SectorID", "Name") {
			clause, err := ledgerCondition(&condition, params)
			if err != nil {
				yield(nil, err)

				return
			}
			where = append(where, clause)
		}

		order := qSet.TakeSort()
		var limit string
		if bounds, ok := qSet.TakePage(); ok {
			order = bounds.Order()
			if boundary := bounds.Boundary(); boundary != nil {
				clause, err := cursorPredicate(order, boundary, params)
				if err != nil {
					yield(nil, err)

					return
				}
				where = append(where, clause)
			}
			params["fetch"] = fetchLimit(bounds.Fetch())
			limit = " LIMIT @fetch"
		}

		sql := "SELECT * FROM (" + ledgerSQL + ") l"
		if len(where) > 0 {
			sql += " WHERE " + strings.Join(where, " AND ")
		}
		sql += " ORDER BY " + orderBy(order) + limit

		txn := client.ReadOnlyTransaction()
		defer txn.Close()

		var rows []ServiceLedger
		if err := spxscan.Select(ctx, txn.SpannerReadOnlyTransaction(), &rows, cloudspanner.Statement{SQL: sql, Params: params}); err != nil {
			yield(nil, errors.Wrap(err, "spxscan.Select()"))

			return
		}
		for i := range rows {
			if !yield(&rows[i], nil) {
				return
			}
		}
	}
}

// ledgerCondition renders one taken filter condition into SQL, binding its value. Take is
// all-or-nothing per field, so every operator the request may use on a filterable
// column is rendered here.
func ledgerCondition(c *resource.Condition, params map[string]any) (string, error) {
	column, ok := ledgerColumns[c.Field]
	if !ok {
		return "", errors.Newf("ListServiceLedger: %s is not a ledger column", c.Field)
	}
	name := fmt.Sprintf("f%d", len(params))
	switch c.Operator {
	case opEqual, "ne", "gt", "gte", "lt", "lte":
		params[name] = c.Value

		return fmt.Sprintf("l.%s %s @%s", column, sqlOperator(c.Operator), name), nil
	case "in", "notin":
		params[name] = c.TypedValues()
		not := ""
		if c.Operator == "notin" {
			not = "NOT "
		}

		return fmt.Sprintf("l.%s %sIN UNNEST(@%s)", column, not, name), nil
	case "isnull":
		return fmt.Sprintf("l.%s IS NULL", column), nil
	case "isnotnull":
		return fmt.Sprintf("l.%s IS NOT NULL", column), nil
	default:
		return "", errors.Newf("ListServiceLedger: operator %s is not supported on %s", c.Operator, c.Field)
	}
}

// opEqual is the equality operator as the filter grammar spells it.
const opEqual = "eq"

// maxFetch bounds the rows one pushed-down page asks Spanner for, well inside what the
// declared maximum page size plus one can reach.
const maxFetch = 1 << 20

// fetchLimit converts the page's fetch count to the int64 Spanner binds, bounded.
func fetchLimit(fetch uint64) int64 {
	if fetch > math.MaxInt64 {
		return math.MaxInt64
	}

	return int64(fetch)
}

func sqlOperator(op string) string {
	switch op {
	case opEqual:
		return "="
	case "ne":
		return "!="
	case "gt":
		return ">"
	case "gte":
		return ">="
	case "lt":
		return "<"
	default:
		return "<="
	}
}

// orderBy renders the taken total order as the plain direction per column. A nullable
// column (LastReturnAt) sorts in Spanner's own placement, NULL first ascending and last
// descending: the placement TakeSort promises, so the handler's sort and boundary test
// agree with these rows whenever they still run.
func orderBy(order []resource.SortField) string {
	parts := make([]string, 0, len(order))
	for _, sf := range order {
		direction := "ASC"
		if sf.Direction == resource.SortDescending {
			direction = "DESC"
		}
		parts = append(parts, "l."+ledgerColumns[sf.Field]+" "+direction)
	}

	return strings.Join(parts, ", ")
}

// cursorPredicate renders "strictly after the boundary row in the read order" as the
// row-value comparison a keyset cursor needs: (a > b0) OR (a = b0 AND b > b1) ...,
// with each field's comparator following its direction. A nullable field admits the
// NULL region on Spanner's side of its direction: ascending, NULL comes first, so every
// non-null row follows a NULL boundary (a nil entry) and no NULL row follows a value;
// descending, NULL comes last, so the NULL rows follow a value and nothing follows a
// NULL boundary. Equality on a NULL boundary is IS NULL.
func cursorPredicate(order []resource.SortField, boundary []any, params map[string]any) (string, error) {
	if len(order) != len(boundary) {
		return "", errors.New("ListServiceLedger: the cursor does not match the order")
	}
	var disjuncts []string
	var equalities []string
	for i, sf := range order {
		column := "l." + ledgerColumns[sf.Field]
		param := fmt.Sprintf("@c%d", i)
		nullable := ledgerNullable[sf.Field]
		var after, equal string
		switch {
		case boundary[i] == nil && !nullable:
			return "", errors.Newf("ListServiceLedger: the cursor holds a NULL for %s, which is never NULL", sf.Field)
		case boundary[i] == nil:
			equal = column + " IS NULL"
			if sf.Direction == resource.SortAscending {
				after = column + " IS NOT NULL"
			}
		case sf.Direction == resource.SortAscending:
			params[param[1:]] = spannerValue(boundary[i])
			equal = column + " = " + param
			after = column + " > " + param
		default:
			params[param[1:]] = spannerValue(boundary[i])
			equal = column + " = " + param
			after = column + " < " + param
			if nullable {
				after = "(" + after + " OR " + column + " IS NULL)"
			}
		}
		if after != "" {
			conjuncts := append(slices.Clone(equalities), after)
			disjuncts = append(disjuncts, "("+strings.Join(conjuncts, " AND ")+")")
		}
		equalities = append(equalities, equal)
	}
	if len(disjuncts) == 0 {
		// A NULL boundary at the NULL region's end: nothing follows it.
		return "FALSE", nil
	}

	return "(" + strings.Join(disjuncts, " OR ") + ")", nil
}

// spannerValue converts a boundary value into a type the Spanner client binds: a decimal
// travels as a NUMERIC rational.
func spannerValue(value any) any {
	if d, ok := value.(decimal.Decimal); ok {
		return d.Rat()
	}
	if r, ok := value.(*big.Rat); ok {
		return r
	}

	return value
}
