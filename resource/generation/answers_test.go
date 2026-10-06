package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/google/go-cmp/cmp"
)

// TestResolveAnswers pins the @answers contract: the declared statuses ride the
// method, the result's HTTPStatus() and the declaration go together, 204 needs a
// nil-able result or an answerless method, and each malformed declaration is
// refused naming the reason.
func TestResolveAnswers(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "rpcform"))

	tests := []struct {
		name         string
		structName   string
		wantStatuses []int
		wantErr      string
	}{
		{name: "declared statuses ride the method", structName: "AnswersDeclared", wantStatuses: []int{200, 409}},
		{name: "204 with a pointer result", structName: "AnswersNoContentPointer", wantStatuses: []int{200, 204}},
		{name: "204 with a value result is refused", structName: "AnswersNoContentValue", wantErr: "declares 204, which writes no body, but Execute answers with a Verdict value"},
		{name: "an answerless method may declare 204 alone", structName: "AnswerlessNoContent", wantStatuses: []int{204}},
		{name: "an answerless method may declare nothing else", structName: "AnswerlessOther", wantErr: "the only status such a method may declare is 204"},
		{name: "a result that chooses without a declaration is refused", structName: "AnswersUndeclared", wantErr: "declares HTTPStatus() int but the method declares no @answers"},
		{name: "a declaration without a chooser is refused", structName: "AnswersNoChooser", wantErr: "has no HTTPStatus() int method"},
		{name: "a frame refusal cannot be declared", structName: "AnswersFrameCode", wantErr: "404 may not be declared"},
		{name: "a 5xx cannot be declared", structName: "AnswersServerCode", wantErr: "500 may not be declared"},
		{name: "a repeated status is refused", structName: "AnswersDuplicate", wantErr: "declares 409 twice"},
		{name: "a declaration with no success status is refused", structName: "AnswersNoSuccess", wantErr: "declares no success status"},
		{name: "a non-numeric status is refused", structName: "AnswersNotACode", wantErr: `"teapot" is not an HTTP status code`},
		{name: "a plain method declares nothing", structName: "TwoResults"},
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
			if err != nil {
				t.Fatalf("classifyExecute(%s) error = %v", tt.structName, err)
			}
			rpcMethod := &rpcMethodInfo{Struct: s, Form: signature.form, ResultPointer: signature.resultPointer, choosesStatus: signature.choosesStatus}
			if signature.result != nil {
				rpcMethod.Result = &wireShape{Source: signature.result.Obj().Name()}
			}

			err = resolveAnswers(rpcMethod, s, annotations)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveAnswers(%s) error = %v, want containing %q", tt.structName, err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.structName) {
					t.Errorf("resolveAnswers(%s) error does not name the struct: %v", tt.structName, err)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveAnswers(%s) error = %v", tt.structName, err)
			}
			if diff := cmp.Diff(tt.wantStatuses, rpcMethod.Statuses); diff != "" {
				t.Errorf("Statuses mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_choosesStatus pins the method-set read: HTTPStatus() int on either receiver
// marks a result that chooses its status; any other shape does not.
func Test_choosesStatus(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "rpcform"))

	tests := []struct {
		name       string
		structName string
		want       bool
	}{
		{name: "a value-receiver HTTPStatus chooses", structName: "AnswersDeclared", want: true},
		{name: "through a pointer result too", structName: "AnswersNoContentPointer", want: true},
		{name: "a result without the method does not", structName: "TwoResults", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			signature, err := classifyExecute(structs[tt.structName])
			if err != nil {
				t.Fatalf("classifyExecute(%s) error = %v", tt.structName, err)
			}
			if signature.choosesStatus != tt.want {
				t.Errorf("choosesStatus = %v, want %v", signature.choosesStatus, tt.want)
			}
		})
	}
}
