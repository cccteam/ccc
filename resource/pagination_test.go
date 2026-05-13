package resource

import (
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

func TestEncodeDecodePageToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token *PageToken
	}{
		{
			name: "single string field",
			token: &PageToken{
				Values: []PageTokenValue{
					{Field: "ID", Value: "abc123"},
				},
			},
		},
		{
			name: "multiple fields",
			token: &PageToken{
				Values: []PageTokenValue{
					{Field: "Name", Value: "John"},
					{Field: "ID", Value: "abc123"},
				},
			},
		},
		{
			name: "numeric value",
			token: &PageToken{
				Values: []PageTokenValue{
					{Field: "Age", Value: float64(42)},
					{Field: "ID", Value: "xyz"},
				},
			},
		},
		{
			name: "empty values",
			token: &PageToken{
				Values: []PageTokenValue{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := EncodePageToken(tt.token)
			if err != nil {
				t.Fatalf("EncodePageToken() error = %v", err)
			}

			if encoded == "" {
				t.Fatal("EncodePageToken() returned empty string")
			}

			decoded, err := DecodePageToken(encoded)
			if err != nil {
				t.Fatalf("DecodePageToken() error = %v", err)
			}

			if diff := cmp.Diff(tt.token, decoded); diff != "" {
				t.Errorf("Round-trip mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDecodePageToken_Invalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "not base64",
			input: "!!!not-base64!!!",
		},
		{
			name:  "valid base64 but not JSON",
			input: "bm90LWpzb24=", // "not-json" in base64
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodePageToken(tt.input)
			if err == nil {
				t.Error("Expected error for invalid token, got nil")
			}
		})
	}
}

func TestCursorFieldNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		sortFields       []SortField
		primaryKeyFields []accesstypes.Field
		want             []string
	}{
		{
			name:             "PK only, no sort",
			sortFields:       nil,
			primaryKeyFields: []accesstypes.Field{"ID"},
			want:             []string{"ID"},
		},
		{
			name: "sort fields + PK tiebreaker",
			sortFields: []SortField{
				{Field: "Name", Direction: SortAscending},
			},
			primaryKeyFields: []accesstypes.Field{"ID"},
			want:             []string{"Name", "ID"},
		},
		{
			name: "PK already in sort fields - no duplicate",
			sortFields: []SortField{
				{Field: "ID", Direction: SortAscending},
			},
			primaryKeyFields: []accesstypes.Field{"ID"},
			want:             []string{"ID"},
		},
		{
			name: "multiple sort + compound PK",
			sortFields: []SortField{
				{Field: "Name", Direction: SortAscending},
				{Field: "Age", Direction: SortDescending},
			},
			primaryKeyFields: []accesstypes.Field{"TenantID", "ID"},
			want:             []string{"Name", "Age", "TenantID", "ID"},
		},
		{
			name: "partial overlap with compound PK",
			sortFields: []SortField{
				{Field: "TenantID", Direction: SortAscending},
				{Field: "Name", Direction: SortDescending},
			},
			primaryKeyFields: []accesstypes.Field{"TenantID", "ID"},
			want:             []string{"TenantID", "Name", "ID"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cursorFieldNames(tt.sortFields, tt.primaryKeyFields)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("cursorFieldNames() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCursorFieldDirection(t *testing.T) {
	t.Parallel()

	sortFields := []SortField{
		{Field: "Name", Direction: SortAscending},
		{Field: "Age", Direction: SortDescending},
	}

	tests := []struct {
		fieldName string
		want      SortDirection
	}{
		{"Name", SortAscending},
		{"Age", SortDescending},
		{"ID", SortAscending}, // PK tiebreaker defaults to ascending
	}

	for _, tt := range tests {
		t.Run(tt.fieldName, func(t *testing.T) {
			t.Parallel()

			got := cursorFieldDirection(tt.fieldName, sortFields)
			if got != tt.want {
				t.Errorf("cursorFieldDirection(%q) = %v, want %v", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestBuildKeysetWhereClause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fields     []cursorField
		dbType     DBType
		wantSQL    string
		wantParams map[string]any
	}{
		{
			name: "single ascending field - spanner",
			fields: []cursorField{
				{ColumnName: "id", ParamName: "_cursor_0", Value: "abc", Direction: SortAscending},
			},
			dbType:  SpannerDBType,
			wantSQL: "(`id` > @_cursor_0)",
			wantParams: map[string]any{
				"_cursor_0": "abc",
			},
		},
		{
			name: "single descending field - postgres",
			fields: []cursorField{
				{ColumnName: "age", ParamName: "_cursor_0", Value: 42, Direction: SortDescending},
			},
			dbType:  PostgresDBType,
			wantSQL: `("age" < @_cursor_0)`,
			wantParams: map[string]any{
				"_cursor_0": 42,
			},
		},
		{
			name: "two fields mixed direction",
			fields: []cursorField{
				{ColumnName: "name_sql", ParamName: "_cursor_0", Value: "John", Direction: SortAscending},
				{ColumnName: "id", ParamName: "_cursor_1", Value: "xyz", Direction: SortAscending},
			},
			dbType:  SpannerDBType,
			wantSQL: "(`name_sql` > @_cursor_0) OR (`name_sql` = @_cursor_0 AND `id` > @_cursor_1)",
			wantParams: map[string]any{
				"_cursor_0": "John",
				"_cursor_1": "xyz",
			},
		},
		{
			name: "three fields with descending middle",
			fields: []cursorField{
				{ColumnName: "name_sql", ParamName: "_cursor_0", Value: "Alice", Direction: SortAscending},
				{ColumnName: "age_sql", ParamName: "_cursor_1", Value: 30, Direction: SortDescending},
				{ColumnName: "id", ParamName: "_cursor_2", Value: "abc", Direction: SortAscending},
			},
			dbType:  SpannerDBType,
			wantSQL: "(`name_sql` > @_cursor_0) OR (`name_sql` = @_cursor_0 AND `age_sql` < @_cursor_1) OR (`name_sql` = @_cursor_0 AND `age_sql` = @_cursor_1 AND `id` > @_cursor_2)",
			wantParams: map[string]any{
				"_cursor_0": "Alice",
				"_cursor_1": 30,
				"_cursor_2": "abc",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotSQL, gotParams := buildKeysetWhereClause(tt.fields, tt.dbType)
			if gotSQL != tt.wantSQL {
				t.Errorf("SQL mismatch:\nwant: %s\ngot:  %s", tt.wantSQL, gotSQL)
			}
			if diff := cmp.Diff(tt.wantParams, gotParams); diff != "" {
				t.Errorf("Params mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildPageToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		values          map[string]any
		cursorFields    []string
		wantFieldCount  int
		wantFirstField  string
		wantFirstValue  any
		wantSecondField string
		wantSecondValue any
	}{
		{
			name:           "single field",
			values:         map[string]any{"ID": "abc123"},
			cursorFields:   []string{"ID"},
			wantFieldCount: 1,
			wantFirstField: "ID",
			wantFirstValue: "abc123",
		},
		{
			name:            "multiple fields",
			values:          map[string]any{"ID": "abc123", "Name": "John"},
			cursorFields:    []string{"Name", "ID"},
			wantFieldCount:  2,
			wantFirstField:  "Name",
			wantFirstValue:  "John",
			wantSecondField: "ID",
			wantSecondValue: "abc123",
		},
		{
			name:           "missing field - skipped",
			values:         map[string]any{"Name": "John"},
			cursorFields:   []string{"Name", "ID"},
			wantFieldCount: 1,
			wantFirstField: "Name",
			wantFirstValue: "John",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			token := buildPageToken(tt.values, tt.cursorFields)

			if len(token.Values) != tt.wantFieldCount {
				t.Fatalf("Expected %d token values, got %d", tt.wantFieldCount, len(token.Values))
			}

			if token.Values[0].Field != tt.wantFirstField {
				t.Errorf("First field = %q, want %q", token.Values[0].Field, tt.wantFirstField)
			}
			if token.Values[0].Value != tt.wantFirstValue {
				t.Errorf("First value = %v, want %v", token.Values[0].Value, tt.wantFirstValue)
			}

			if tt.wantFieldCount > 1 {
				if token.Values[1].Field != tt.wantSecondField {
					t.Errorf("Second field = %q, want %q", token.Values[1].Field, tt.wantSecondField)
				}
				if token.Values[1].Value != tt.wantSecondValue {
					t.Errorf("Second value = %v, want %v", token.Values[1].Value, tt.wantSecondValue)
				}
			}
		})
	}
}

func TestCollectPage(t *testing.T) {
	t.Parallel()

	type item struct {
		ID   string
		Name string
	}

	makeIter := func(items []*item) func(func(*item, error) bool) {
		return func(yield func(*item, error) bool) {
			for _, it := range items {
				if !yield(it, nil) {
					return
				}
			}
		}
	}

	extractor := func(it *item, fields []string) map[string]any {
		m := make(map[string]any, len(fields))
		for _, f := range fields {
			switch f {
			case "ID":
				m["ID"] = it.ID
			case "Name":
				m["Name"] = it.Name
			}
		}
		return m
	}

	t.Run("no pagination - returns all", func(t *testing.T) {
		t.Parallel()

		items := []*item{
			{ID: "1", Name: "A"},
			{ID: "2", Name: "B"},
			{ID: "3", Name: "C"},
		}

		results, nextToken, err := CollectPage(makeIter(items), nil, nil, nil, extractor)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if nextToken != "" {
			t.Errorf("expected empty nextToken, got %q", nextToken)
		}
		if len(results) != 3 {
			t.Errorf("expected 3 results, got %d", len(results))
		}
	})

	t.Run("page size with no next page", func(t *testing.T) {
		t.Parallel()

		items := []*item{
			{ID: "1", Name: "A"},
			{ID: "2", Name: "B"},
		}
		pageSize := uint64(5)

		results, nextToken, err := CollectPage(makeIter(items), &pageSize, nil, []accesstypes.Field{"ID"}, extractor)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if nextToken != "" {
			t.Errorf("expected empty nextToken, got %q", nextToken)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("page size with next page", func(t *testing.T) {
		t.Parallel()

		// Simulate pageSize+1 results (the extra row signals more pages)
		items := []*item{
			{ID: "1", Name: "A"},
			{ID: "2", Name: "B"},
			{ID: "3", Name: "C"},
		}
		pageSize := uint64(2)

		results, nextToken, err := CollectPage(makeIter(items), &pageSize, nil, []accesstypes.Field{"ID"}, extractor)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if nextToken == "" {
			t.Error("expected non-empty nextToken")
		}
		if len(results) != 2 {
			t.Errorf("expected 2 results, got %d", len(results))
		}

		// Verify the token can be decoded and contains the last row's ID
		token, err := DecodePageToken(nextToken)
		if err != nil {
			t.Fatalf("DecodePageToken() error: %v", err)
		}
		if len(token.Values) != 1 {
			t.Fatalf("expected 1 token value, got %d", len(token.Values))
		}
		if token.Values[0].Field != "ID" {
			t.Errorf("token field = %q, want %q", token.Values[0].Field, "ID")
		}
		if token.Values[0].Value != "2" {
			t.Errorf("token value = %v, want %q", token.Values[0].Value, "2")
		}
	})

	t.Run("page size exactly matches result count", func(t *testing.T) {
		t.Parallel()

		items := []*item{
			{ID: "1", Name: "A"},
			{ID: "2", Name: "B"},
		}
		pageSize := uint64(2)

		results, nextToken, err := CollectPage(makeIter(items), &pageSize, nil, []accesstypes.Field{"ID"}, extractor)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if nextToken != "" {
			t.Errorf("expected empty nextToken when exactly pageSize results, got %q", nextToken)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 results, got %d", len(results))
		}
	})
}
