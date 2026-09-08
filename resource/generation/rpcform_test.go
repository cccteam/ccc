package generation

import (
	"strings"
	"testing"
)

func Test_classifyExecute(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "rpcform"))

	tests := []struct {
		name        string
		structName  string
		want        rpcForm
		wantResult  string
		wantPointer bool
		wantFiles   bool
		wantErr     string
	}{
		{name: "transaction form", structName: "TxnForm", want: rpcFormTxn},
		{name: "client form", structName: "ClientForm", want: rpcFormClient},
		{name: "value receiver classifies like a pointer receiver", structName: "ValueReceiver", want: rpcFormTxn},
		{name: "a struct result answers", structName: "TwoResults", want: rpcFormTxn, wantResult: "rpcform.Report"},
		{name: "a pointer result answers", structName: "AnswersPointer", want: rpcFormClient, wantResult: "rpcform.Report", wantPointer: true},
		{name: "the upload form takes the files third", structName: "UploadForm", want: rpcFormTxn, wantFiles: true},
		{name: "the upload form may answer", structName: "UploadAnswers", want: rpcFormTxn, wantResult: "rpcform.Report", wantFiles: true},
		{name: "a basic result is refused", structName: "AnswersBasic", wantErr: "struct AnswersBasic: Execute answers with string, which is not a struct type or a pointer to one"},
		{name: "three results are refused", structName: "ThreeResults", wantErr: "struct ThreeResults: Execute returns (rpcform.Report, int, error); it returns error, or (Result, error)"},
		{name: "no Execute", structName: "NoExecute", wantErr: "struct NoExecute has no Execute method"},
		{name: "two parameters", structName: "TwoParams", wantErr: "struct TwoParams: Execute takes (context.Context, resource.ReadWriteTransaction)"},
		{name: "variadic", structName: "Variadic", wantErr: "struct Variadic: Execute takes (context.Context, resource.ReadWriteTransaction, ...*rpcform.Client)"},
		{name: "first parameter not context", structName: "FirstNotContext", wantErr: "struct FirstNotContext: Execute's first parameter is string, not context.Context"},
		{name: "second parameter neither form", structName: "SecondUnknown", wantErr: "struct SecondUnknown: Execute's second parameter is *resource.Client, neither resource.ReadWriteTransaction nor resource.Client"},
		{name: "third parameter not a pointer", structName: "ThirdNotPointer", wantErr: "struct ThirdNotPointer: Execute's client parameter is rpcform.Client, not a pointer to the application's RPC client type"},
		{name: "no result", structName: "NoResult", wantErr: "struct NoResult: Execute returns nothing; it returns error, or (Result, error)"},
		{name: "result not error", structName: "ResultNotError", wantErr: "struct ResultNotError: Execute returns (string); it returns error, or (Result, error)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs[tt.structName]
			if s == nil {
				t.Fatalf("struct %q not in fixture", tt.structName)
			}
			signature, err := classifyExecute(s)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("classifyExecute(%s) error = %v, want containing %q", tt.structName, err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), "declares Execute in one of three forms") {
					t.Errorf("classifyExecute(%s) error omits the accepted forms:\n%v", tt.structName, err)
				}

				return
			}
			if err != nil {
				t.Fatalf("classifyExecute(%s) error = %v", tt.structName, err)
			}
			if signature.form != tt.want {
				t.Errorf("classifyExecute(%s) form = %v, want %v", tt.structName, signature.form, tt.want)
			}
			var gotResult string
			if signature.result != nil {
				gotResult = typeStringer(signature.result)
			}
			if gotResult != tt.wantResult || signature.resultPointer != tt.wantPointer {
				t.Errorf("classifyExecute(%s) result = %q (pointer %v), want %q (pointer %v)", tt.structName, gotResult, signature.resultPointer, tt.wantResult, tt.wantPointer)
			}
			if signature.takesFiles != tt.wantFiles {
				t.Errorf("classifyExecute(%s) takesFiles = %v, want %v", tt.structName, signature.takesFiles, tt.wantFiles)
			}
		})
	}
}
