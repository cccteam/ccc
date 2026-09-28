package migration

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestSequence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		entries  []Entry
		want     []Problem
		wantSpan Span
	}{
		{name: "none", wantSpan: Span{}},
		{
			name:     "a consolidated history starting above 000001, with downs and other files",
			entries:  []Entry{{Name: "000004_Base.up.sql"}, {Name: "000004_Base.down.sql"}, {Name: "000005_Next.up.sql"}, {Name: "README.md"}},
			wantSpan: Span{Count: 2, Low: 4, High: 5},
		},
		{
			name:     "a gap, a bad name, two ups with the note carried, a lone down",
			entries:  []Entry{{Name: "000001_A.up.sql"}, {Name: "000002_B.up.sql"}, {Name: "000002_C.up.sql", Note: "on master"}, {Name: "000005_E.down.sql"}, {Name: "notes.sql"}},
			wantSpan: Span{Count: 2, Low: 1, High: 5},
			want: []Problem{
				{File: "notes.sql", Text: "not a migration file name (NNNNNN_name.up.sql or NNNNNN_name.down.sql)"},
				{File: "000002_B.up.sql", Text: "index 000002 has 2 up files; one migration per index"},
				{File: "000002_C.up.sql", Note: "on master", Text: "index 000002 has 2 up files; one migration per index"},
				{File: "000005_E.down.sql", Text: "index 000005 has a down file and no up file"},
				{Text: "gap: no migration 000003 between 000001 and 000005; the sequence is contiguous so nothing is skipped"},
				{Text: "gap: no migration 000004 between 000001 and 000005; the sequence is contiguous so nothing is skipped"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, span := Sequence(tt.entries)
			if diff := cmp.Diff(tt.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("problems (-want +got):\n%s", diff)
			}
			if span != tt.wantSpan {
				t.Errorf("span = %+v, want %+v", span, tt.wantSpan)
			}
		})
	}
}

func TestBlobSHA(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		// git hash-object /dev/null, and of a file holding "hello\n".
		{name: "empty", content: "", want: "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"},
		{name: "a line", content: "hello\n", want: "ce013625030ba8dba906f756967f9e9ca394464a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := BlobSHA([]byte(tt.content)); got != tt.want {
				t.Errorf("BlobSHA(%q) = %s, want %s", tt.content, got, tt.want)
			}
		})
	}
}
