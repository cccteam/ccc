package resource

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestExtractWithClause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		query      string
		wantWith   string
		wantRemain string
	}{
		{
			name:       "no WITH clause",
			query:      "SELECT * FROM table",
			wantWith:   "",
			wantRemain: "SELECT * FROM table",
		},
		{
			name:       "simple WITH clause",
			query:      "WITH a AS (SELECT 1) SELECT * FROM a",
			wantWith:   "WITH a AS (SELECT 1)",
			wantRemain: " SELECT * FROM a",
		},
		{
			name:       "multiple WITH clauses (CTE)",
			query:      "WITH a AS (SELECT 1), b AS (SELECT 2) SELECT * FROM a CROSS JOIN b",
			wantWith:   "WITH a AS (SELECT 1), b AS (SELECT 2)",
			wantRemain: " SELECT * FROM a CROSS JOIN b",
		},
		{
			name:       "nested WITH clause",
			query:      "WITH a AS (WITH b AS (SELECT 1) SELECT * FROM b) SELECT * FROM a",
			wantWith:   "WITH a AS (WITH b AS (SELECT 1) SELECT * FROM b)",
			wantRemain: " SELECT * FROM a",
		},
		{
			name:       "newline formatting",
			query:      "WITH a AS (\n\tSELECT 1\n)\nSELECT * FROM a",
			wantWith:   "WITH a AS (\n\tSELECT 1\n)",
			wantRemain: "\nSELECT * FROM a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotWith, gotRemain := extractWithClause(tt.query)
			if diff := cmp.Diff(tt.wantWith, gotWith); diff != "" {
				t.Errorf("extractWithClause() withClause mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantRemain, gotRemain); diff != "" {
				t.Errorf("extractWithClause() remainingQuery mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
