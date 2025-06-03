package graph_test

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/graph"
	"github.com/google/go-cmp/cmp"
)

func Test_DirectedGraph(t *testing.T) {
	t.Parallel()

	type args struct {
		nodes map[string][]string
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr bool
	}{
		{
			name: "empty",
			args: args{
				nodes: map[string][]string{},
			},
			want: []string{},
		},
		{
			name: "base cases",
			args: args{
				nodes: map[string][]string{
					"A": {"B", "C", "D"}, // outdegree: 3
					"B": {"D"},           // outdegree: 1
					"C": {"B", "D"},      // outdegree: 2
					// D outdegree: 0
				},
			},
			want: []string{"D", "B", "C", "A"},
		},
		{
			name: "base cases, equal outdegree",
			args: args{
				nodes: map[string][]string{
					"A": {"B"}, // outdegree: 1
					"B": {"C"}, // outdegree: 1
					"C": {"D"}, // outdegree: 1
					"D": {"E"}, // outdegree: 1
					// E outdegree: 0
				},
			},
			want: []string{"E", "A", "B", "C", "D"},
		},
		{
			name: "alphabetically sorted nodes of equal outdegree",
			args: args{
				nodes: map[string][]string{
					"Apple":              {"ZZZZZZZZZZZZZZZZZZ", "Banana", "Empire", "Electro", "Electronic", "Election"},
					"Banana":             {"Chiropractor"},
					"Date":               {"Electron"},
					"ZZZZZZZZZZZZZZZZZZ": {"Banana"},
					"Empire":             {"Electrons"},
					"Chiropractor":       {"Date"},
				},
			},
			want: []string{
				"Election", "Electro", "Electron", "Electronic",
				"Electrons", "Banana", "Chiropractor", "Date", "Empire", "ZZZZZZZZZZZZZZZZZZ", "Apple",
			},
		},
		{
			name: "trivial dependency cycle",
			args: args{
				nodes: map[string][]string{
					"A": {"B"},
					"B": {"A"},
				},
			},
			wantErr: true,
		},
		{
			name: "deep dependency cycle",
			args: args{
				nodes: map[string][]string{
					"A": {"B"},
					"B": {"C"},
					"C": {"D"},
					"D": {"E"},
					"E": {"A"},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := graph.New[string](uint(len(tt.args.nodes)))

			for node, edges := range tt.args.nodes {
				for _, e := range edges {
					srcNode := g.Insert(node)
					dstNode := g.Insert(e)

					g.AddPath(srcNode, dstNode)
				}
			}

			err := g.CycleCheck()
			if err != nil && !tt.wantErr {
				t.Errorf("%s: CycleCheck() error:\n%s", tt.name, err)
			} else if err != nil && tt.wantErr {
				return
			} else if err == nil && tt.wantErr {
				t.Errorf("%s: CycleCheck() expected error, got nil", tt.name)
			}

			got := g.OrderedList(strings.Compare)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s: OrderedList() mismatch (-want +got):\n%s", tt.name, diff)
			}
		})
	}
}
