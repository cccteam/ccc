package resource

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_spannerQueryParser_parseToSearchSubstring(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		tokenlist SearchKey
		want      Statement
	}{
		{
			name:      "elem",
			query:     "mike",
			tokenlist: "NameTokens",
			want: Statement{
				Sql:    "SEARCH_SUBSTRING(NameTokens, @searchsubstringterm0)",
				Params: map[string]any{"searchsubstringterm0": "mike"},
			},
		},
		{
			name:      "elem AND elem",
			query:     "mike john",
			tokenlist: "NameTokens",
			want: Statement{
				Sql: "SEARCH_SUBSTRING(NameTokens, @searchsubstringterm0) AND SEARCH_SUBSTRING(NameTokens, @searchsubstringterm1)",
				Params: map[string]any{
					"searchsubstringterm0": "mike",
					"searchsubstringterm1": "john",
				},
			},
		},
		{
			name:      "elem AND elem AND elem",
			query:     "mike john bill",
			tokenlist: "NameTokens",
			want: Statement{
				Sql: "SEARCH_SUBSTRING(NameTokens, @searchsubstringterm0) AND SEARCH_SUBSTRING(NameTokens, @searchsubstringterm1) AND SEARCH_SUBSTRING(NameTokens, @searchsubstringterm2)",
				Params: map[string]any{
					"searchsubstringterm0": "mike",
					"searchsubstringterm1": "john",
					"searchsubstringterm2": "bill",
				},
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := spannerQueryParser{
				query: tt.query,
			}
			if got := s.parseToSearchSubstring(tt.tokenlist); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("spannerQueryParser.parseToSearchSubstring() = %v, want %v", got, tt.want)
			}

			got := s.parseToSearchSubstring(tt.tokenlist)

			if diff := cmp.Diff(tt.want.Sql, got.Sql); diff != "" {
				t.Errorf("(parseToSearchSubstring().Sql) mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.want.Params, got.Params); diff != "" {
				t.Errorf("(parseToSearchSubstring().Params) mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_spannerQueryParser_parseToNgramScore(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		tokenlist SearchKey
		want      Statement
	}{
		{
			name:      "elem",
			query:     "mike",
			tokenlist: "NameTokens",
			want: Statement{
				Sql:    "SCORE_NGRAMS(NameTokens, @ngramscoreterm0)",
				Params: map[string]any{"ngramscoreterm0": "mike"},
			},
		},
		{
			name:      "elem + elem",
			query:     "mike john",
			tokenlist: "NameTokens",
			want: Statement{
				Sql: "SCORE_NGRAMS(NameTokens, @ngramscoreterm0) + SCORE_NGRAMS(NameTokens, @ngramscoreterm1)",
				Params: map[string]any{
					"ngramscoreterm0": "mike",
					"ngramscoreterm1": "john",
				},
			},
		},
		{
			name:      "elem + elem + elem",
			query:     "mike john bill",
			tokenlist: "NameTokens",
			want: Statement{
				Sql: "SCORE_NGRAMS(NameTokens, @ngramscoreterm0) + SCORE_NGRAMS(NameTokens, @ngramscoreterm1) + SCORE_NGRAMS(NameTokens, @ngramscoreterm2)",
				Params: map[string]any{
					"ngramscoreterm0": "mike",
					"ngramscoreterm1": "john",
					"ngramscoreterm2": "bill",
				},
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := spannerQueryParser{
				query: tt.query,
			}
			if got := s.parseToNgramScore(tt.tokenlist); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("spannerQueryParser.parseToNgramScore() = %v, want %v", got, tt.want)
			}

			got := s.parseToNgramScore(tt.tokenlist)

			if diff := cmp.Diff(tt.want.Sql, got.Sql); diff != "" {
				t.Errorf("(parseToNgramScore().Sql) mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tt.want.Params, got.Params); diff != "" {
				t.Errorf("(parseToNgramScore().Params) mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
