package resource

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"iter"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

// PageToken represents a pagination cursor containing the values of the sort fields and primary key
// from the last row returned in a page. It is encoded as an opaque base64 string for the client.
type PageToken struct {
	Values []PageTokenValue `json:"v"`
}

// PageTokenValue holds a single cursor field's name and value.
type PageTokenValue struct {
	Field string `json:"f"`
	Value any    `json:"v"`
}

// PageResponse is the response envelope for paginated list endpoints.
type PageResponse[T any] struct {
	Data          []T    `json:"data"`
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// cursorField represents a field used for keyset pagination cursor comparison.
type cursorField struct {
	ColumnName string
	ParamName  string
	Value      any
	Direction  SortDirection
}

// EncodePageToken serializes a PageToken to a base64-encoded string.
func EncodePageToken(token *PageToken) (string, error) {
	data, err := json.Marshal(token)
	if err != nil {
		return "", errors.Wrap(err, "json.Marshal()")
	}

	return base64.URLEncoding.EncodeToString(data), nil
}

// DecodePageToken deserializes a base64-encoded string into a PageToken.
func DecodePageToken(s string) (*PageToken, error) {
	data, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, httpio.NewBadRequestMessagef("invalid page token")
	}

	var token PageToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, httpio.NewBadRequestMessagef("invalid page token")
	}

	return &token, nil
}

// buildPageToken creates a PageToken from a map of cursor field values.
func buildPageToken(values map[string]any, cursorFields []string) *PageToken {
	token := &PageToken{
		Values: make([]PageTokenValue, 0, len(cursorFields)),
	}

	for _, fieldName := range cursorFields {
		if v, ok := values[fieldName]; ok {
			token.Values = append(token.Values, PageTokenValue{
				Field: fieldName,
				Value: v,
			})
		}
	}

	return token
}

// cursorFieldNames returns the ordered list of field names used for keyset cursor:
// the explicit sort fields followed by any PK fields not already in the sort list.
func cursorFieldNames(sortFields []SortField, primaryKeyFields []accesstypes.Field) []string {
	names := make([]string, 0, len(sortFields)+len(primaryKeyFields))
	seen := make(map[string]struct{}, len(sortFields)+len(primaryKeyFields))

	for _, sf := range sortFields {
		names = append(names, sf.Field)
		seen[sf.Field] = struct{}{}
	}

	for _, pk := range primaryKeyFields {
		if _, ok := seen[string(pk)]; !ok {
			names = append(names, string(pk))
			seen[string(pk)] = struct{}{}
		}
	}

	return names
}

// cursorFieldDirection returns the sort direction for a cursor field name.
// Sort fields use their specified direction; PK tiebreaker fields default to ascending.
func cursorFieldDirection(fieldName string, sortFields []SortField) SortDirection {
	for _, sf := range sortFields {
		if sf.Field == fieldName {
			return sf.Direction
		}
	}

	return SortAscending
}

// buildKeysetWhereClause generates the keyset pagination WHERE clause and parameters.
// For cursor fields (f1 ASC, f2 DESC, f3 ASC) with values (v1, v2, v3), this generates:
//
//	(f1 > v1) OR (f1 = v1 AND f2 < v2) OR (f1 = v1 AND f2 = v2 AND f3 > v3)
func buildKeysetWhereClause(fields []cursorField, dbType DBType) (string, map[string]any) {
	params := make(map[string]any, len(fields))
	conditions := make([]string, 0, len(fields))

	for i, cf := range fields {
		parts := make([]string, 0, i+1)

		// Equality conditions for all preceding fields
		for j := range i {
			eqParam := fields[j].ParamName
			parts = append(parts, fmt.Sprintf("%s = @%s", quoteColumnName(fields[j].ColumnName, dbType), eqParam))
			params[eqParam] = fields[j].Value
		}

		// Comparison condition for the current field
		op := ">"
		if cf.Direction == SortDescending {
			op = "<"
		}

		parts = append(parts, fmt.Sprintf("%s %s @%s", quoteColumnName(cf.ColumnName, dbType), op, cf.ParamName))
		params[cf.ParamName] = cf.Value

		conditions = append(conditions, "("+strings.Join(parts, " AND ")+")")
	}

	return strings.Join(conditions, " OR "), params
}

func quoteColumnName(name string, dbType DBType) string {
	switch dbType {
	case SpannerDBType:
		return "`" + name + "`"
	case PostgresDBType:
		return `"` + name + `"`
	default:
		return name
	}
}

// CollectPage collects results from an iterator, handling pagination truncation and token creation.
// extractCursorValues extracts cursor field values from a row by Go struct field name.
// This is used for computed resources; standard resources should use QuerySet.ListPage() instead.
func CollectPage[Resource any](seq iter.Seq2[*Resource, error], pageSize *uint64, sortFields []SortField, primaryKeyFields []accesstypes.Field, extractCursorValues func(*Resource, []string) map[string]any) ([]*Resource, string, error) {
	var results []*Resource
	for row, err := range seq {
		if err != nil {
			return nil, "", err
		}

		results = append(results, row)
	}

	if pageSize == nil {
		return results, "", nil
	}

	if uint64(len(results)) <= *pageSize {
		return results, "", nil
	}

	// Trim the extra row used for next-page detection
	results = results[:*pageSize]

	lastRow := results[len(results)-1]
	cfNames := cursorFieldNames(sortFields, primaryKeyFields)
	token := buildPageToken(extractCursorValues(lastRow, cfNames), cfNames)

	nextPageToken, err := EncodePageToken(token)
	if err != nil {
		return nil, "", errors.Wrap(err, "EncodePageToken()")
	}

	return results, nextPageToken, nil
}
