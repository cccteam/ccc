package commentlang_test

import (
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser/commentlang"
	"github.com/google/go-cmp/cmp"
)

func Test_commentLang(t *testing.T) {
	type args struct {
		comment []string
		mode    commentlang.ScanMode
	}
	tests := []struct {
		name    string
		args    args
		want    map[commentlang.Keyword][]string
		wantErr bool
	}{
		{
			name: "multiline",
			args: args{
				comment: []string{`/* @uniqueindex (Id, Description)

@substring (SUBSTR(@self-4))
@substring (Id)
@substring(Other)
*/`},
				mode: commentlang.ScanStruct,
			},
			want: map[commentlang.Keyword][]string{
				commentlang.UniqueIndex: {"Id, Description"},
				commentlang.Substring:   {"SUBSTR(@self-4)", "Id", "Other"},
			},
		},
		{
			name: "singular",
			args: args{comment: []string{`// @primarykey (Id, Description)`}, mode: commentlang.ScanStruct},
			want: map[commentlang.Keyword][]string{
				commentlang.PrimaryKey: {"Id, Description"},
			},
		},
		{
			name:    "singular missing args error",
			args:    args{comment: []string{`// @primarykey`}, mode: commentlang.ScanStruct},
			wantErr: true,
		},
		{
			name:    "typo returns an error",
			args:    args{comment: []string{`// @primarkyey (Id, Description)`}, mode: commentlang.ScanStruct},
			wantErr: true,
		},
		{
			name:    "primarykey with args returns an error when using ScanField mode",
			args:    args{comment: []string{`// @primarykey (Id, Description)`}, mode: commentlang.ScanField},
			wantErr: true,
		},
		{
			name: "primarykey without args does not error when using ScanField mode",
			args: args{comment: []string{`// @primarykey`}, mode: commentlang.ScanField},
			want: map[commentlang.Keyword][]string{
				commentlang.PrimaryKey: {},
			},
		},
		{
			name: "multiple singleline field comments",
			args: args{
				comment: []string{`// @primarykey`, `// @substring (@self - 4)`, `// @check (@self = 'S')`},
				mode:    commentlang.ScanField,
			},
			want: map[commentlang.Keyword][]string{
				commentlang.PrimaryKey: {},
				commentlang.Substring:  {`@self - 4`},
				commentlang.Check:      {`@self = 'S'`},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			argMap, err := commentlang.Scan(tt.args.comment, tt.args.mode)
			if err != nil && tt.wantErr {
				return
			}
			if err != nil {
				t.Errorf("%s: commentlang.Scan() error (wantErr=%v): %s", t.Name(), tt.wantErr, err.Error())
			}
			if tt.wantErr {
				t.Errorf("%s: commentlang.Scan() did not error (wantErr=%v)", t.Name(), tt.wantErr)

				return
			}

			got := make(map[commentlang.Keyword][]string)
			for key, args := range argMap {
				if _, ok := got[key]; !ok {
					got[key] = make([]string, 0)
				}

				for _, arg := range args {
					got[key] = append(got[key], arg.Arguments()...)
				}
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s: commentlang.Result() mismatch (-want +got):\n%s", t.Name(), diff)
			}
		})
	}
}
