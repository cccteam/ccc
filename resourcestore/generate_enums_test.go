package resourcestore

import (
	"iter"
	"maps"
	"reflect"
	"testing"
)

type S string

func Test_collectSortedStrings(t *testing.T) {
	type args struct {
		seq iter.Seq[S]
	}
	tests := []struct {
		name        string
		args        args
		wantStrings []string
	}{
		{
			name: "test collectStrings",
			args: args{
				seq: maps.Keys(map[S]struct{}{"a": {}, "b": {}}),
			},
			wantStrings: []string{"a", "b"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if gotStrings := collectSortedStrings(tt.args.seq); !reflect.DeepEqual(gotStrings, tt.wantStrings) {
				t.Errorf("collectStrings() = %v, want %v", gotStrings, tt.wantStrings)
			}
		})
	}
}
