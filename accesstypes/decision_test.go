package accesstypes

import (
	"slices"
	"testing"
)

// equalGroups reports whether two condition-group slices are equal. Condition
// is an empty placeholder (pending the expression language), so groups
// compare by their resource scopes.
func equalGroups(a, b []ConditionGroup) bool {
	return slices.EqualFunc(a, b, func(x, y ConditionGroup) bool {
		return slices.Equal(x.Resources, y.Resources)
	})
}

func TestDecision(t *testing.T) {
	t.Parallel()

	group := ConditionGroup{Resources: []Resource{"employees.name"}}

	tests := []struct {
		name            string
		decision        Decision
		wantDenied      bool
		wantGranted     bool
		wantConditional bool
		wantGroups      []ConditionGroup
		wantString      string
	}{
		{
			name:       "zero decision fails closed",
			decision:   Decision{},
			wantDenied: true,
			wantString: "denied",
		},
		{
			name:       "denied",
			decision:   Denied(),
			wantDenied: true,
			wantString: "denied",
		},
		{
			name:        "granted",
			decision:    Granted(),
			wantGranted: true,
			wantString:  "granted",
		},
		{
			name:            "conditional carries its groups",
			decision:        Conditional(group),
			wantConditional: true,
			wantGroups:      []ConditionGroup{group},
			wantString:      "conditional",
		},
		{
			name: "conditional keeps groups separate",
			decision: Conditional(
				ConditionGroup{Resources: []Resource{"employees.name", "employees.title"}},
				ConditionGroup{Resources: []Resource{"employees.salary"}},
			),
			wantConditional: true,
			wantGroups: []ConditionGroup{
				{Resources: []Resource{"employees.name", "employees.title"}},
				{Resources: []Resource{"employees.salary"}},
			},
			wantString: "conditional",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.decision.IsDenied(); got != tt.wantDenied {
				t.Errorf("IsDenied() = %v, want %v", got, tt.wantDenied)
			}
			if got := tt.decision.IsGranted(); got != tt.wantGranted {
				t.Errorf("IsGranted() = %v, want %v", got, tt.wantGranted)
			}
			if got := tt.decision.IsConditional(); got != tt.wantConditional {
				t.Errorf("IsConditional() = %v, want %v", got, tt.wantConditional)
			}
			if got := tt.decision.ConditionGroups(); !equalGroups(tt.wantGroups, got) {
				t.Errorf("ConditionGroups() = %+v, want %+v", got, tt.wantGroups)
			}
			if got := tt.decision.String(); got != tt.wantString {
				t.Errorf("String() = %q, want %q", got, tt.wantString)
			}
		})
	}
}

// TestConditional_requiresGroups pins the constructor invariant: a
// Conditional decision with nothing to evaluate is a programming bug.
func TestConditional_requiresGroups(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("Conditional() with no groups did not panic, want panic")
		}
	}()

	Conditional()
}

func TestDecisions_DeniedResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		decisions Decisions
		want      []Resource
	}{
		{
			name: "nil decisions deny nothing",
		},
		{
			name: "all granted",
			decisions: Decisions{
				"employees":      Granted(),
				"employees.name": Granted(),
			},
		},
		{
			name: "denied resources are sorted",
			decisions: Decisions{
				"employees.title": Denied(),
				"employees.name":  Denied(),
				"employees.id":    Granted(),
			},
			want: []Resource{"employees.name", "employees.title"},
		},
		{
			name: "conditional is not denied",
			decisions: Decisions{
				"employees.name":   Conditional(ConditionGroup{Resources: []Resource{"employees.name"}}),
				"employees.salary": Denied(),
			},
			want: []Resource{"employees.salary"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.decisions.DeniedResources(); !slices.Equal(tt.want, got) {
				t.Errorf("DeniedResources() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecisions_ConditionalResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		decisions Decisions
		want      []Resource
	}{
		{
			name: "nil decisions condition nothing",
		},
		{
			name: "granted and denied are not conditional",
			decisions: Decisions{
				"employees":      Granted(),
				"employees.name": Denied(),
			},
		},
		{
			name: "conditional resources are sorted",
			decisions: Decisions{
				"employees.title": Conditional(ConditionGroup{Resources: []Resource{"employees.title"}}),
				"employees.name":  Conditional(ConditionGroup{Resources: []Resource{"employees.name"}}),
				"employees.id":    Granted(),
			},
			want: []Resource{"employees.name", "employees.title"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.decisions.ConditionalResources(); !slices.Equal(tt.want, got) {
				t.Errorf("ConditionalResources() = %v, want %v", got, tt.want)
			}
		})
	}
}
