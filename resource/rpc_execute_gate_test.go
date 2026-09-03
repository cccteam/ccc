package resource

import (
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

// TestExecuteGate_checkStatement pins the gate's check-SELECT (§12): the
// carried Execute condition lowered against the target resource's bindings as
// one boolean, the row located by its primary key, and the reserved
// parameters bound to the same values the permission check folded with.
func TestExecuteGate_checkStatement(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		source     string
		wantSQL    string
		wantParams map[string]any
	}{
		{
			name:       "subject comparison binds @subject",
			source:     "owner = subject",
			wantSQL:    "SELECT (`enforcementResources`.`Owner` = @subject) AS g0 FROM enforcementResources WHERE Id = @zzTargetKey",
			wantParams: map[string]any{"zzTargetKey": "task-1", "subject": "dana"},
		},
		{
			name:       "state exclusion lowers against the uniform state binding",
			source:     "state NOT IN ('closed', 'canceled')",
			wantSQL:    "SELECT (`enforcementResources`.`State` NOT IN (@_c1, @_c2)) AS g0 FROM enforcementResources WHERE Id = @zzTargetKey",
			wantParams: map[string]any{"zzTargetKey": "task-1", "_c1": "closed", "_c2": "canceled"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gate := &ExecuteGate{
				method:     "NudgeTask",
				user:       "dana",
				scope:      testScope,
				env:        accesstypes.EnvironmentAt(now),
				collection: executeCollection(t),
				cond:       mustCondition(tt.source).Expr(),
			}

			stmt, err := gate.checkStatement(ExecuteTarget{Resource: enforcedResource, Label: "EnforcementResource", PKColumn: "Id"}, "task-1")
			if err != nil {
				t.Fatalf("ExecuteGate.checkStatement() error = %v", err)
			}
			if got := normalizeSQL(stmt.SQL); got != tt.wantSQL {
				t.Errorf("ExecuteGate.checkStatement() SQL =\n%s\nwant\n%s", got, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantParams, stmt.Params); diff != "" {
				t.Errorf("ExecuteGate.checkStatement() params mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestExecuteGate_Enforce_noCondition pins the no-op paths: a nil gate and a
// gate whose grant was unconditional enforce nothing and touch no transaction.
func TestExecuteGate_Enforce_noCondition(t *testing.T) {
	t.Parallel()

	var gate *ExecuteGate
	if err := gate.Enforce(t.Context(), nil, ExecuteTarget{}, "id"); err != nil {
		t.Errorf("nil gate Enforce() error = %v, want nil", err)
	}

	gate = &ExecuteGate{method: "NudgeTask"}
	if err := gate.Enforce(t.Context(), nil, ExecuteTarget{}, "id"); err != nil {
		t.Errorf("condition-free gate Enforce() error = %v, want nil", err)
	}
}
