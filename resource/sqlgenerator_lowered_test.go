package resource

import (
	"math/big"
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
