package commentlang_test

import (
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser/commentlang"
	"github.com/google/go-cmp/cmp"
)

func Test_ScanStruct(t *testing.T) {
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
			args: args{
				comment: `@uniqueindex (Id, Description)
					@foreignkey (Type) (StatusTypes(Id))
					@foreignkey (Status) (Statuses(Id))`,
			},
			want: map[commentlang.Keyword][]string{
				commentlang.UniqueIndex: {"Id, Description"},
				commentlang.ForeignKey:  {"Type", "StatusTypes(Id)", "Status", "Statuses(Id)"},
			},
		},
		{
			name: "singular",
			args: args{comment: `@primarykey (Id, Description)`},
			want: map[commentlang.Keyword][]string{
				commentlang.PrimaryKey: {"Id, Description"},
			},
		},
		{
			name:    "singular missing args error",
			args:    args{comment: "@primarykey"},
			wantErr: true,
		},
		{
			name:    "typo returns an error",
			args:    args{comment: "@primarkyey (Id, Description)"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			argMap, err := commentlang.ScanStruct(tt.args.comment)
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
					got[key] = append(got[key], arg.Arg1)
					if arg.Arg2 != nil {
						got[key] = append(got[key], *arg.Arg2)
					}
				}
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s: commentlang.Result() mismatch (-want +got):\n%s", t.Name(), diff)
			}
		})
	}
}

func Test_ScanField(t *testing.T) {
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
			name:    "primarykey with args returns an error when using ScanField mode",
			args:    args{comment: "@primarykey (Id, Description)"},
			wantErr: true,
		},
		{
			name: "multiple exclusive keyword use returns error",
			args: args{
				comment: `@primarykey
@check(@self != 0)
@foreignkey (Foobar(Id))
@primarykey`,
			},
			wantErr: true,
		},
		{
			name: "primarykey without args does not error when using ScanField mode",
			args: args{comment: "@primarykey"},
			want: map[commentlang.Keyword][]string{
				commentlang.PrimaryKey: {},
			},
		},
		{
			name: "multiple singleline field comments",
			args: args{
				comment: "@primarykey\n@substring (@self - 4)\n@check (@self = 'S')",
			},
			want: map[commentlang.Keyword][]string{
				commentlang.PrimaryKey: {},
				commentlang.Substring:  {"@self - 4"},
				commentlang.Check:      {"@self = 'S'"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			argMap, err := commentlang.ScanField(tt.args.comment)
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
					got[key] = append(got[key], arg.Arg1)
					if arg.Arg2 != nil {
						got[key] = append(got[key], *arg.Arg2)
					}
				}
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s: commentlang.Result() mismatch (-want +got):\n%s", t.Name(), diff)
			}
		})
	}
}
