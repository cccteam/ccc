package genlang_test

import (
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/google/go-cmp/cmp"
)

var keywords = map[string]genlang.KeywordOpts{
	"primarykey":  {genlang.ScanStruct: genlang.ArgsRequired | genlang.Exclusive, genlang.ScanField: genlang.NoArgs | genlang.Exclusive},
	"foreignkey":  {genlang.ScanStruct: genlang.DualArgsRequired, genlang.ScanField: genlang.ArgsRequired},
	"check":       {genlang.ScanField: genlang.ArgsRequired | genlang.Exclusive},
	"default":     {genlang.ScanField: genlang.ArgsRequired | genlang.Exclusive},
	"hidden":      {genlang.ScanField: genlang.NoArgs | genlang.Exclusive},
	"substring":   {genlang.ScanField: genlang.ArgsRequired},
	"fulltext":    {genlang.ScanField: genlang.ArgsRequired},
	"ngram":       {genlang.ScanField: genlang.ArgsRequired},
	"index":       {genlang.ScanStruct: genlang.ArgsRequired, genlang.ScanField: genlang.NoArgs},
	"uniqueindex": {genlang.ScanStruct: genlang.ArgsRequired, genlang.ScanField: genlang.NoArgs},
	"view":        {genlang.ScanStruct: genlang.NoArgs | genlang.Exclusive},
	"query":       {genlang.ScanStruct: genlang.ArgsRequired | genlang.Exclusive},
	"using":       {genlang.ScanField: genlang.ArgsRequired | genlang.Exclusive},
	"suppress":    {genlang.ScanField: genlang.NoArgs | genlang.Exclusive},
	"omit":        {genlang.ScanField: genlang.NoArgs | genlang.Exclusive},
	"policy":      {genlang.ScanStruct: genlang.ArgsRequired},
}

func Test_ScanStruct(t *testing.T) {
	type args struct {
		filepath string
	}
	tests := []struct {
		name       string
		args       args
		wantStruct map[string][]string
		wantFields []map[string][]string
		wantErr    bool
	}{
		{
			name: "multiline comments",
			args: args{
				filepath: "./testdata/multiline.go",
			},
			wantStruct: map[string][]string{
				"uniqueindex": {"Id, Description"},
				"foreignkey":  {"Type", "StatusTypes(Id)", "Status", "Statuses(Id)"},
			},
			wantFields: []map[string][]string{
				{
					"primarykey": {},
					"check":      {"@self = 'N'"},
					"hidden":     {},
					"substring":  {"@self"},
				},
			},
		},
		{
			name: "single-line comments",
			args: args{filepath: "./testdata/singular.go"},
			wantStruct: map[string][]string{
				"primarykey": {"Id, Description"},
			},
			wantFields: []map[string][]string{
				{"primarykey": {}},
			},
		},
		{
			name:    "singular missing args error",
			args:    args{filepath: "./testdata/singular_err.go"},
			wantErr: true,
		},
		{
			name:    "typo returns a helpful error",
			args:    args{filepath: "./testdata/typo.go"},
			wantErr: true,
		},
		{
			name:    "exclusive error",
			args:    args{filepath: "./testdata/exclusive_err.go"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pkgMap, err := parser.LoadPackages(tt.args.filepath)
			if err != nil {
				t.Errorf("loadPackages() error = %v", err)
				return
			}

			parsedStructs := parser.ParseStructs(pkgMap["resources"])

			for _, pStruct := range parsedStructs {
				scanner := genlang.NewScanner(keywords)

				results, err := scanner.ScanStruct(pStruct)
				if err != nil && tt.wantErr {
					return
				}
				if err != nil {
					t.Errorf("%s: genlang.Scan() error (wantErr=%v): %s", t.Name(), tt.wantErr, err.Error())
				}
				if tt.wantErr {
					t.Errorf("%s: genlang.Scan() did not error (wantErr=%v)", t.Name(), tt.wantErr)

					return
				}

				got := make(map[string][]string)
				for key := range results.Struct.Keys() {

					if _, ok := got[key]; !ok {
						got[key] = make([]string, 0)
					}

					for arg1, arg2 := range results.Struct.GetDualArgs(key) {
						got[key] = append(got[key], arg1)

						if arg2 != nil {
							got[key] = append(got[key], *arg2)
						}
					}
				}

				if diff := cmp.Diff(tt.wantStruct, got); diff != "" {
					t.Errorf("%s: genlang.Result() mismatch (-want +got):\n%s", t.Name(), diff)
				}

				if tt.wantFields == nil {
					continue
				}

				for i := range pStruct.Fields() {
					gotField := make(map[string][]string)
					for key := range results.Fields[i].Keys() {
						if _, ok := gotField[key]; !ok {
							gotField[key] = make([]string, 0)
						}

						for arg1, arg2 := range results.Fields[i].GetDualArgs(key) {
							gotField[key] = append(gotField[key], arg1)

							if arg2 != nil {
								gotField[key] = append(gotField[key], *arg2)
							}
						}
					}

					if diff := cmp.Diff(tt.wantFields[i], gotField); diff != "" {
						t.Errorf("%s: genlang.Result() fields mismatch (-want +gotField):\n%s", t.Name(), diff)
					}

				}
			}
		})
	}
}
