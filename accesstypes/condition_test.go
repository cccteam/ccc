package accesstypes

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes/condition"
)

// TestNewCondition pins the wrapper contract: a Condition carries the
// verbatim source beside the compiled tree, the zero Condition carries
// nothing, and compilation errors surface.
func TestNewCondition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		source      string
		wantErr     bool
		wantContain string
	}{
		{
			name:   "valid condition compiles",
			source: "crew IN subject.crews AND now < '2027-03-01T00:00:00Z'",
		},
		{
			name:        "malformed condition errors",
			source:      "crew IN",
			wantErr:     true,
			wantContain: "condition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, err := NewCondition(tt.source)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewCondition(%q) expected an error, got nil", tt.source)
				}
				if !strings.Contains(err.Error(), tt.wantContain) {
					t.Errorf("NewCondition(%q) error = %q, want containing %q", tt.source, err, tt.wantContain)
				}

				return
			}
			if err != nil {
				t.Fatalf("NewCondition(%q) error = %v", tt.source, err)
			}
			if c.IsZero() {
				t.Error("IsZero() = true for a compiled Condition")
			}
			if got := c.Source(); got != tt.source {
				t.Errorf("Source() = %q, want the verbatim %q", got, tt.source)
			}
			if c.Expr() == nil {
				t.Error("Expr() = nil for a compiled Condition")
			}
		})
	}
}

func TestCondition_zeroAndFromExpr(t *testing.T) {
	t.Parallel()

	var zero Condition
	if !zero.IsZero() {
		t.Error("zero Condition IsZero() = false, want true")
	}
	if zero.Source() != "" || zero.Expr() != nil {
		t.Error("zero Condition must carry nothing")
	}
	if !ConditionFromExpr(nil).IsZero() {
		t.Error("ConditionFromExpr(nil).IsZero() = false, want true")
	}

	expr, err := condition.Parse("state = 'open' OR crew IN subject.crews")
	if err != nil {
		t.Fatalf("condition.Parse() error = %v", err)
	}
	c := ConditionFromExpr(expr)
	if c.IsZero() {
		t.Error("ConditionFromExpr() IsZero() = true, want false")
	}
	if got, want := c.Source(), expr.String(); got != want {
		t.Errorf("Source() = %q, want the canonical rendering %q", got, want)
	}
}
