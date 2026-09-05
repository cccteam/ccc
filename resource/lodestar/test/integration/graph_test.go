package integration

// graph_test (design plan §9): reads the two committed DOT files and asserts every
// non-terminal state has an in-edge and an out-edge, so no orphan state can be
// reintroduced silently. The terminal states are the ones no declared transition
// leaves.

import (
	"os"
	"regexp"
	"testing"
)

func TestWorkflowGraphsHaveNoOrphanStates(t *testing.T) {
	t.Parallel()

	stateNode := regexp.MustCompile(`"state:([a-z_]+)" \[label=`)
	stateEdge := regexp.MustCompile(`"state:([a-z_]+)" -> "state:([a-z_]+)" \[label="([A-Za-z]+)"\]`)

	tests := []struct {
		name          string
		path          string
		wantStates    int
		wantEdges     int
		wantDefault   string
		wantTerminals []string
	}{
		{
			name:          "mission workflow",
			path:          "../../pkg/resources/zz_gen_workflow_mission.dot",
			wantStates:    7,
			wantEdges:     9, // seven transitions, StandDown drawn from three sources
			wantDefault:   "open",
			wantTerminals: []string{"completed", "failed", "stood_down"},
		},
		{
			name:          "refit workflow",
			path:          "../../pkg/resources/zz_gen_workflow_refit.dot",
			wantStates:    6,
			wantEdges:     8, // six transitions, Scrap drawn from three sources
			wantDefault:   "docked",
			wantTerminals: []string{"cleared", "scrapped"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("reading %s: %v", tt.path, err)
			}
			dot := string(raw)

			states := make(map[string]bool)
			for _, m := range stateNode.FindAllStringSubmatch(dot, -1) {
				states[m[1]] = true
			}
			if len(states) != tt.wantStates {
				t.Errorf("states = %d (%v), want %d", len(states), states, tt.wantStates)
			}

			in := make(map[string]int)
			out := make(map[string]int)
			edges := stateEdge.FindAllStringSubmatch(dot, -1)
			for _, m := range edges {
				out[m[1]]++
				in[m[2]]++
			}
			if len(edges) != tt.wantEdges {
				t.Errorf("edges = %d, want %d", len(edges), tt.wantEdges)
			}

			terminal := make(map[string]bool)
			for _, s := range tt.wantTerminals {
				terminal[s] = true
			}
			for state := range states {
				if state != tt.wantDefault && in[state] == 0 {
					t.Errorf("state %q is never entered", state)
				}
				if terminal[state] {
					if out[state] != 0 {
						t.Errorf("terminal state %q has an out-edge", state)
					}

					continue
				}
				if out[state] == 0 {
					t.Errorf("non-terminal state %q is never left", state)
				}
			}
		})
	}
}
