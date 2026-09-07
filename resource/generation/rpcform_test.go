package generation

import (
	"strings"
	"testing"
)

func Test_classifyExecute(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "rpcform"))

	tests := []struct {
		name       string
		structName string
		want       rpcForm
		wantErr    string
	}{
		{name: "transaction form", structName: "TxnForm", want: rpcFormTxn},
		{name: "client form", structName: "ClientForm", want: rpcFormClient},
		{name: "value receiver classifies like a pointer receiver", structName: "ValueReceiver", want: rpcFormTxn},
		{name: "no Execute", structName: "NoExecute", wantErr: "struct NoExecute has no Execute method"},
		{name: "two parameters", structName: "TwoParams", wantErr: "struct TwoParams: Execute takes (context.Context, resource.ReadWriteTransaction)"},
		{name: "variadic", structName: "Variadic", wantErr: "struct Variadic: Execute takes (context.Context, resource.ReadWriteTransaction, ...*rpcform.Client)"},
		{name: "first parameter not context", structName: "FirstNotContext", wantErr: "struct FirstNotContext: Execute's first parameter is string, not context.Context"},
		{name: "second parameter neither form", structName: "SecondUnknown", wantErr: "struct SecondUnknown: Execute's second parameter is *resource.Client, neither resource.ReadWriteTransaction nor resource.Client"},
		{name: "third parameter not a pointer", structName: "ThirdNotPointer", wantErr: "struct ThirdNotPointer: Execute's third parameter is rpcform.Client, not a pointer to the application's RPC client type"},
		{name: "no result", structName: "NoResult", wantErr: "struct NoResult: Execute returns nothing; it returns error"},
		{name: "result not error", structName: "ResultNotError", wantErr: "struct ResultNotError: Execute returns (string); it returns error"},
		{name: "two results", structName: "TwoResults", wantErr: "struct TwoResults: Execute returns (string, error); it returns error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs[tt.structName]
			if s == nil {
				t.Fatalf("struct %q not in fixture", tt.structName)
			}
			got, err := classifyExecute(s)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("classifyExecute(%s) error = %v, want containing %q", tt.structName, err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), "declares Execute in one of two forms") {
					t.Errorf("classifyExecute(%s) error omits the accepted forms:\n%v", tt.structName, err)
				}

				return
			}
			if err != nil {
				t.Fatalf("classifyExecute(%s) error = %v", tt.structName, err)
			}
			if got != tt.want {
				t.Errorf("classifyExecute(%s) = %v, want %v", tt.structName, got, tt.want)
			}
		})
	}
}
