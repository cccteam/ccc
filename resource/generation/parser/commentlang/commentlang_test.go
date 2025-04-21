package commentlang_test

import (
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser/commentlang"
	"github.com/google/go-cmp/cmp"
)

func Test_commentLang(t *testing.T) {
	type args struct {
		comment string
	}
	tests := []struct {
		name    string
		args    args
		want    map[commentlang.Keyword][]string
		wantErr bool
	}{
		{
			name: "multiline",
			args: args{comment: `/* @uniqueindex
@substring

@substring (SUBSTR(%self%-4))

*/`},
			want: map[commentlang.Keyword][]string{
				commentlang.UniqueIndex: {},
				commentlang.Substring:   {"SUBSTR(%self%-4)"},
			},
		},
		{
			name: "multiline with and without space before arguments",
			args: args{comment: `/* @uniqueindex (Id)
@uniqueindex(Id2)
@substring (Id)
@substring(Other)
*/`},
			want: map[commentlang.Keyword][]string{
				commentlang.Substring:   {"Id", "Other"},
				commentlang.UniqueIndex: {"Id", "Id2"},
			},
		},
		{
			name: "singular",
			args: args{comment: `// @primarykey (Id, Description)`},
			want: map[commentlang.Keyword][]string{
				commentlang.PrimaryKey: {"Id, Description"},
			},
		},
		{
			name: "singular no args",
			args: args{comment: `// @primarykey`},
			want: map[commentlang.Keyword][]string{
				commentlang.PrimaryKey: {},
			},
		},
		{
			name:    "typo returns an error",
			args:    args{comment: `// @primarkyey (Id, Description)`},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := commentlang.Scan([]string{tt.args.comment})
			if err != nil && tt.wantErr {
				return
			}
			if err != nil {
				t.Errorf("commentlang.Scan() error (wantErr=%v): %s", tt.wantErr, err.Error())
			}
			if tt.wantErr {
				t.Errorf("commentlang.Scan() did not error (wantErr=%v)", tt.wantErr)

				return
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("commentlang.Result() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
