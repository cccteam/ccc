package generation

import (
	"reflect"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
)

func Test_extractStructsByInterface(t *testing.T) {
	type args struct {
		packagePath string
		packageName string
		interfaces  []string
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr bool
	}{
		{
			name:    "gets the structs with the methods",
			args:    args{packagePath: "./testdata/rpc", packageName: "rpc", interfaces: []string{"TxnRunner"}},
			want:    []string{"Banana", "Cofveve"},
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

			rpcStructs, err := extractStructsByInterface(pkgMap[tt.args.packageName], tt.args.interfaces...)
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
