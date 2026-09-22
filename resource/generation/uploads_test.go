package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser/genlang"
)

// TestResolveUpload pins the @upload contract: the declaration and the
// resource.Files signature go together, the transaction form is the only one, the
// maximum is required and a size, and each malformed shape is refused naming the
// struct.
func TestResolveUpload(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "rpcform"))

	tests := []struct {
		name         string
		structName   string
		wantMaxBytes int64
		wantErr      string
		wantClassify string
	}{
		{name: "a declared upload with the files signature", structName: "UploadForm", wantMaxBytes: 5 << 20},
		{name: "an upload may answer", structName: "UploadAnswers", wantMaxBytes: 5 << 20},
		{name: "the files signature without a declaration is refused", structName: "UploadUndeclared", wantErr: "takes resource.Files but the method declares no @upload"},
		{name: "a declaration without the files signature is refused", structName: "UploadNoFiles", wantErr: "does not take resource.Files"},
		{name: "the client form cannot upload", structName: "UploadClientForm", wantClassify: "an upload runs inside the handler's transaction"},
		{name: "a maximum that is not a size is refused", structName: "UploadBadSize", wantErr: `"lots" is not a size`},
		{name: "the maximum is required, as max:", structName: "UploadNoMax", wantErr: "expected 0 positional argument(s), found 1"},
		{name: "the files come third", structName: "UploadFilesNotThird", wantClassify: "not resource.Files"},
		{name: "a JSON method declares nothing", structName: "TxnForm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs[tt.structName]
			if s == nil {
				t.Fatalf("struct %q not found in fixture package", tt.structName)
			}
			annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(s)
			if err != nil {
				t.Fatalf("ScanStruct(%s) error = %v", tt.structName, err)
			}
			signature, err := classifyExecute(s)
			if tt.wantClassify != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantClassify) {
					t.Fatalf("classifyExecute(%s) error = %v, want containing %q", tt.structName, err, tt.wantClassify)
				}

				return
			}
			if err != nil {
				t.Fatalf("classifyExecute(%s) error = %v", tt.structName, err)
			}
			rpcMethod := &rpcMethodInfo{Struct: s, Form: signature.form, takesFiles: signature.takesFiles}
			if signature.result != nil {
				rpcMethod.Result = &wireShape{Source: signature.result.Obj().Name()}
			}

			err = resolveUpload(rpcMethod, s, annotations)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveUpload(%s) error = %v, want containing %q", tt.structName, err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.structName) {
					t.Errorf("resolveUpload(%s) error does not name the struct: %v", tt.structName, err)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveUpload(%s) error = %v", tt.structName, err)
			}
			switch {
			case tt.wantMaxBytes == 0 && rpcMethod.Upload != nil:
				t.Errorf("Upload = %+v, want none", rpcMethod.Upload)
			case tt.wantMaxBytes != 0 && (rpcMethod.Upload == nil || rpcMethod.Upload.MaxBytes != tt.wantMaxBytes):
				t.Errorf("Upload = %+v, want max %d", rpcMethod.Upload, tt.wantMaxBytes)
			}
		})
	}
}
