package resource

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/cccteam/httpio"
	"github.com/google/go-cmp/cmp"
)

func TestOperations(t *testing.T) {
	t.Parallel()

	type args struct {
		r       *http.Request
		pattern string
		opts    []Option
	}
	tests := []struct {
		name       string
		args       args
		wantMethod []string
		wantValues []string
		wantParams []map[string]string
		wantErr    bool
	}{
		{
			name: "Test Requests with invalid path",
			args: args{
				r: &http.Request{
					Method: "POST",
					Body:   io.NopCloser(bytes.NewBufferString(`[{"op":"patch","path":"/a/b/c","value":{"c":1}}]`)),
				},
				pattern: "",
			},
			wantErr: true,
		},
		{
			name: "Test Requests with invalid first json token",
			args: args{
				r: &http.Request{
					Method: "POST",
					Body:   io.NopCloser(bytes.NewBufferString(`x[{"op":"patch","path":"/","value":{"c":1}}]`)),
				},
				pattern: "/{id}",
			},
			wantErr: true,
		},
		{
			name: "Test Requests with wrong first json token",
			args: args{
				r: &http.Request{
					Method: "POST",
					Body:   io.NopCloser(bytes.NewBufferString(`{[{"op":"patch","path":"/","value":{"c":1}}]`)),
				},
				pattern: "/{id}",
			},
			wantErr: true,
		},
		{
			name: "Test Requests with wrong last json token",
			args: args{
				r: &http.Request{
					Method: "POST",
					Body:   io.NopCloser(bytes.NewBufferString(`{[{"op":"patch","path":"/","value":{"c":1}}}`)),
				},
				pattern: "/{id}",
			},
			wantErr: true,
		},
		{
			name: "Test patch Requests with resource and multiple ids",
			args: args{
				r: &http.Request{
					Method: "POST",
					Body: io.NopCloser(bytes.NewBufferString(
						`[
							{"op":"patch","path":"/resource1/10/20","value":{"c":1}},
							{"op":"patch","path":"/resource2/11/21","value":{"a":2}}
						]`,
					)),
				},
				pattern: "/{resource}/{id1}/{id2}",
			},
			wantMethod: []string{"PATCH", "PATCH"},
			wantParams: []map[string]string{
				{"resource": "resource1", "id1": "10", "id2": "20"},
				{"resource": "resource2", "id1": "11", "id2": "21"},
			},
			wantValues: []string{`{"c":1}`, `{"a":2}`},
		},
		{
			name: "Test patch Requests with id",
			args: args{
				r: &http.Request{
					Method: "POST",
					Body: io.NopCloser(bytes.NewBufferString(
						`[
							{"op":"patch","path":"/10","value":{"c":1}},
							{"op":"patch","path":"/11","value":{"a":2}}
						]`,
					)),
				},
				pattern: "/{id}",
			},
			wantMethod: []string{"PATCH", "PATCH"},
			wantParams: []map[string]string{
				{"id": "10"},
				{"id": "11"},
			},
			wantValues: []string{`{"c":1}`, `{"a":2}`},
		},
		{
			name: "Test patch Requests with resource and id",
			args: args{
				r: &http.Request{
					Method: "POST",
					Body: io.NopCloser(bytes.NewBufferString(
						`[
							{"op":"patch","path":"/resource1/10","value":{"c":1}},
							{"op":"patch","path":"/resource2/11","value":{"a":2}}
						]`,
					)),
				},
				pattern: "/{resource}/{id}",
			},
			wantMethod: []string{"PATCH", "PATCH"},
			wantParams: []map[string]string{
				{"resource": "resource1", "id": "10"},
				{"resource": "resource2", "id": "11"},
			},
			wantValues: []string{`{"c":1}`, `{"a":2}`},
		},
		{
			name: "Test add Requests with id",
			args: args{
				r: &http.Request{
					Method: "POST",
					Body: io.NopCloser(bytes.NewBufferString(
						`[
							{"op":"add","value":{"c":1}},
							{"op":"add","value":{"a":2}}
						]`,
					)),
				},
				pattern: "/{id}",
			},
			wantMethod: []string{"POST", "POST"},
			wantValues: []string{`{"c":1}`, `{"a":2}`},
		},
		{
			name: "Test delete Requests with id",
			args: args{
				r: &http.Request{
					Method: "POST",
					Body: io.NopCloser(bytes.NewBufferString(
						`[
							{"op":"remove","path":"/10"},
							{"op":"remove","path":"/11"}
						]`,
					)),
				},
				pattern: "/{id}",
			},
			wantMethod: []string{"DELETE", "DELETE"},
			wantParams: []map[string]string{
				{"id": "10"},
				{"id": "11"},
			},
		},
		{
			name: "Test add Requests with no values, all fields in path",
			args: args{
				r: &http.Request{
					Method: "POST",
					Body: io.NopCloser(bytes.NewBufferString(
						`[
							{
							"op": "add",
							"path": "/10/11"
							}
						]`,
					)),
				},
				pattern: "/{id1}/{id2}",
				opts: []Option{
					RequireCreatePath(),
				},
			},
			wantMethod: []string{"POST"},
			wantParams: []map[string]string{
				{"id1": "10", "id2": "11"},
			},
		},
		{
			name: "Test extra space Requests with id",
			args: args{
				r: &http.Request{
					Method: "POST",
					Body: io.NopCloser(bytes.NewBufferString(
						`
							[
								{"op":"add","value":{"c":1}}

							]
						`,
					)),
				},
				pattern: "/{id}",
			},
			wantMethod: []string{"POST"},
			wantValues: []string{`{"c":1}`},
		},
		{
			name: "Test mixed operations",
			args: args{
				r: &http.Request{
					Method: "PATCH",
					Body: io.NopCloser(bytes.NewBufferString(
						`[
						  {
							"op": "add",
							"path": "/X",
							"value": {
								"description": "Office X"
							}
						  },
						  {
							"op": "patch",
							"path": "/O",
							"value": {
								"description": "Office O 2"
							}
						  },
						  {
							"op": "remove",
							"path": "/W"
						  }
						]`,
					)),
				},
				pattern: "/{id}",
				opts: []Option{
					RequireCreatePath(),
				},
			},
			wantMethod: []string{"POST", "PATCH", "DELETE"},
			wantParams: []map[string]string{
				{"id": "X"},
				{"id": "O"},
				{"id": "W"},
			},
			wantValues: []string{
				`{"description":"Office X"}`,
				`{"description":"Office O 2"}`,
				``,
			},
		},
		{
			name: "Test invalid op",
			args: args{
				r: &http.Request{
					Method: "PATCH",
					Body: io.NopCloser(bytes.NewBufferString(
						`[{"op": "invalid", "path": "/W"}]`,
					)),
				},
				pattern: "/{id}",
			},
			wantErr: true,
		},
		{
			name: "Test malformed JSON",
			args: args{
				r: &http.Request{
					Method: "PATCH",
					Body: io.NopCloser(bytes.NewBufferString(
						`[{"op": "add", "path": "/W", "value": "invalid"}`,
					)),
				},
				pattern: "/{id}",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod []string
			var gotValues []string
			var gotParams []map[string]string

			for oper, err := range Operations(tt.args.r, tt.args.pattern, tt.args.opts...) {
				if (err != nil) != tt.wantErr {
					t.Fatalf("Requests() error = %v, wantErr %v", err, tt.wantErr)
				}
				if tt.wantErr {
					return
				}

				gotMethod = append(gotMethod, oper.Req.Method)

				if tt.wantParams != nil {
					params := make(map[string]string)
					for key := range tt.wantParams[len(gotParams)] {
						params[key] = httpio.Param[string](oper.Req, httpio.ParamType(key))
					}
					gotParams = append(gotParams, params)
				}

				if len(tt.wantValues) > 0 {
					val, err := io.ReadAll(oper.Req.Body)
					if err != nil {
						t.Fatalf("io.ReadAll() error: %s", err)
					}
					if len(val) > 0 {
						var prettyVal bytes.Buffer
						if err := json.Compact(&prettyVal, val); err != nil {
							t.Fatalf("json.Compact() error: %s", err)
						}
						gotValues = append(gotValues, prettyVal.String())
					} else {
						gotValues = append(gotValues, "")
					}
				}
			}

			if diff := cmp.Diff(tt.wantMethod, gotMethod); diff != "" {
				t.Errorf("Requests() methods mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tt.wantParams, gotParams); diff != "" {
				t.Errorf("Requests() params mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tt.wantValues, gotValues); diff != "" {
				t.Errorf("Requests() values mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
