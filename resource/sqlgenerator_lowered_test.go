package resource

import (
	"fmt"
	"math/big"
	"slices"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/google/go-cmp/cmp"
	"github.com/shopspring/decimal"
)

func TestParamValue(t *testing.T) {
	t.Parallel()

	ptr := decimal.RequireFromString("12.5")

	tests := []struct {
		name  string
		value any
		want  any
	}{
		{
			name:  "decimal binds as big.Rat so the parameter types NUMERIC",
			value: decimal.RequireFromString("120.50"),
			want:  big.NewRat(241, 2),
		},
		{
			name:  "decimal pointer binds its value",
			value: &ptr,
			want:  big.NewRat(25, 2),
		},
		{
			name:  "nil decimal pointer binds a typed nil big.Rat",
			value: (*decimal.Decimal)(nil),
			want:  (*big.Rat)(nil),
		},
		{
			name:  "valid NullDecimal binds as NullNumeric",
			value: decimal.NullDecimal{Decimal: decimal.RequireFromString("3"), Valid: true},
			want:  spanner.NullNumeric{Numeric: *big.NewRat(3, 1), Valid: true},
		},
		{
			name:  "invalid NullDecimal binds as null NullNumeric",
			value: decimal.NullDecimal{},
			want:  spanner.NullNumeric{},
		},
		{
			name:  "other values pass through untouched",
			value: int64(7),
			want:  int64(7),
		},
		{
			name:  "strings pass through untouched",
			value: "sealed",
			want:  "sealed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := paramValue(tt.value)
			opts := []cmp.Option{
				cmp.Comparer(func(a, b *big.Rat) bool {
					if a == nil || b == nil {
						return a == b
					}

					return a.Cmp(b) == 0
				}),
				cmp.Comparer(func(a, b spanner.NullNumeric) bool {
					return a.Valid == b.Valid && a.Numeric.Cmp(&b.Numeric) == 0
				}),
			}
			if diff := cmp.Diff(tt.want, got, opts...); diff != "" {
				t.Errorf("paramValue() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_generateConditionSQL_paramTyping pins that a condition's values bind through
// paramValue: a decimal reaches Spanner as a NUMERIC (big.Rat), in a comparison and in
// a list alike, while text and numbers bind as they are.
func Test_generateConditionSQL_paramTyping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		condition Condition
		wantTypes []string
	}{
		{name: "a decimal comparison binds a big.Rat", condition: Condition{Field: "Fee", Operator: gtStr, Value: decimal.RequireFromString("30000")}, wantTypes: []string{"*big.Rat"}},
		{name: "a decimal list binds a big.Rat per value", condition: Condition{Field: "Fee", Operator: inStr, Values: []any{decimal.RequireFromString("1"), decimal.RequireFromString("2.5")}}, wantTypes: []string{"*big.Rat", "*big.Rat"}},
		{name: "a string binds as itself", condition: Condition{Field: "Title", Operator: eqStr, Value: "Lantern"}, wantTypes: []string{"string"}},
		{name: "an integer binds as itself", condition: Condition{Field: "Hazard", Operator: gteStr, Value: 3}, wantTypes: []string{"int"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, params, err := NewSpannerGenerator().sqlGenerator.GenerateSQL(&ConditionNode{Condition: tt.condition})
			if err != nil {
				t.Fatalf("GenerateSQL() error = %v", err)
			}
			got := make([]string, 0, len(params))
			for _, p := range params {
				got = append(got, fmt.Sprintf("%T", p.Value))
			}
			if !slices.Equal(got, tt.wantTypes) {
				t.Errorf("parameter types = %v, want %v", got, tt.wantTypes)
			}
		})
	}
}
