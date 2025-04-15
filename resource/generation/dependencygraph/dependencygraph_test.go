package dependencygraph_test

import (
	"testing"

	"github.com/cccteam/ccc/resource/generation/dependencygraph"
	"github.com/google/go-cmp/cmp"
)

func Test_DepGraph(t *testing.T) {
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
					"A": {"B", "C", "D"}, // outdegree: 0
					"B": {"D"},           // outdegree: 2
					"C": {"B", "D"},      // outdegree: 1
					// D outdegree: 3
				},
			},
			want: []string{"D", "B", "C", "A"},
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
			dg := dependencygraph.New()

			for node, edges := range tt.args.nodes {
				for _, e := range edges {
					err := dg.AddEdge(node, e)
					if err != nil && !tt.wantErr {
						t.Errorf("AddEdge() error:\n%s", err)
					} else if err != nil && tt.wantErr {
						return
					}
				}
			}

			got := dg.OrderedList()
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("OrderedList() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
