package genlang_test

import (
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser/genlang"
)

func Test_Suggest(t *testing.T) {
	t.Parallel()

	candidates := []string{"immutable", "pii", "input_only", "output_only"}

	tests := []struct {
		name   string
		word   string
		want   string
		wantOK bool
	}{
		{name: "one letter dropped", word: "immutble", want: "immutable", wantOK: true},
		{name: "two letters transposed", word: "immutabel", want: "immutable", wantOK: true},
		{name: "a short word shifted by a space is not close enough", word: " pii", wantOK: false},
		{name: "a long word shifted by a space is", word: " output_only", want: "output_only", wantOK: true},
		{name: "an exact word suggests itself", word: "pii", want: "pii", wantOK: true},
		{name: "nothing close", word: "readonly", wantOK: false},
		{name: "the empty word", word: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := genlang.Suggest(tt.word, candidates)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("Suggest(%q) = (%q, %v), want (%q, %v)", tt.word, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
