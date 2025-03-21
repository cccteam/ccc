package generation

import (
	"reflect"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
)

func Test_extractStructsByMethod(t *testing.T) {
	type args struct {
		packagePath string
		packageName string
		methods     []string
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr bool
	}{
		{
			name:    "gets the structs with the methods",
			args:    args{packagePath: "./testdata/rpc", packageName: "rpc", methods: []string{"Method", "Execute"}},
			want:    []string{"Cofveve"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pkgMap, err := parser.LoadPackages(tt.args.packagePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadPackages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			rpcStructs, err := extractStructsByMethod(pkgMap[tt.args.packageName], tt.args.methods...)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractRPCMethods() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			var rpcStructNames []string
			for _, s := range rpcStructs {
				rpcStructNames = append(rpcStructNames, s.Name())
			}

			if !reflect.DeepEqual(rpcStructNames, tt.want) {
				t.Errorf("extractRPCMethods() = %v, want %v", rpcStructNames, tt.want)
			}
		})
	}
}
