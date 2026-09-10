// Package rpcform holds one struct per Execute shape the RPC extraction
// classifies or refuses: the two accepted forms on either receiver, and each
// departure from them.
package rpcform

import (
	"context"

	"github.com/cccteam/ccc/resource"
)

// Client stands in for the application's RPC client.
type Client struct{}

type (
	// @rpc
	TxnForm struct{}

	// @rpc
	ClientForm struct{}

	// @rpc
	ValueReceiver struct{}

	// @rpc
	NoExecute struct{}

	// @rpc
	TwoParams struct{}

	// @rpc
	Variadic struct{}

	// @rpc
	FirstNotContext struct{}

	// @rpc
	SecondUnknown struct{}

	// @rpc
	ThirdNotPointer struct{}

	// @rpc
	NoResult struct{}

	// @rpc
	ResultNotError struct{}

	// @rpc
	TwoResults struct{}

	// @rpc
	AnswersPointer struct{}

	// @rpc
	AnswersBasic struct{}

	// @rpc
	ThreeResults struct{}

	// Report is the struct the answering methods return.
	Report struct {
		Note string
	}

	// Verdict is a result that chooses its status: the @answers fixtures below
	// answer with it.
	Verdict struct {
		Accepted bool
	}

	// @rpc
	// @answers(200, 409)
	AnswersDeclared struct{}

	// @rpc
	// @answers(200, 204)
	AnswersNoContentPointer struct{}

	// @rpc
	// @answers(200, 204)
	AnswersNoContentValue struct{}

	// @rpc
	// @answers(204)
	AnswerlessNoContent struct{}

	// @rpc
	// @answers(200)
	AnswerlessOther struct{}

	// @rpc
	AnswersUndeclared struct{}

	// @rpc
	// @answers(200, 409)
	AnswersNoChooser struct{}

	// @rpc
	// @answers(200, 404)
	AnswersFrameCode struct{}

	// @rpc
	// @answers(200, 500)
	AnswersServerCode struct{}

	// @rpc
	// @answers(200, 409, 409)
	AnswersDuplicate struct{}

	// @rpc
	// @answers(409)
	AnswersNoSuccess struct{}

	// @rpc
	// @answers(200, teapot)
	AnswersNotACode struct{}

	// @rpc
	// @upload(max: 5MB)
	UploadForm struct{}

	// @rpc
	// @upload(max: 5MB)
	UploadAnswers struct{}

	// @rpc
	UploadUndeclared struct{}

	// @rpc
	// @upload(max: 5MB)
	UploadNoFiles struct{}

	// @rpc
	// @upload(max: 5MB)
	UploadClientForm struct{}

	// @rpc
	// @upload(max: lots)
	UploadBadSize struct{}

	// @rpc
	// @upload(5MB)
	UploadNoMax struct{}

	// @rpc
	UploadFilesNotThird struct{}
)

func (Verdict) HTTPStatus() int { return 200 }

func (*TxnForm) Execute(context.Context, resource.ReadWriteTransaction, *Client) error { return nil }
func (*ClientForm) Execute(context.Context, resource.Client, *Client) error            { return nil }
func (ValueReceiver) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
func (*TwoParams) Execute(context.Context, resource.ReadWriteTransaction) error { return nil }
func (*Variadic) Execute(context.Context, resource.ReadWriteTransaction, ...*Client) error {
	return nil
}
func (*FirstNotContext) Execute(string, resource.ReadWriteTransaction, *Client) error { return nil }
func (*SecondUnknown) Execute(context.Context, *resource.Client, *Client) error       { return nil }
func (*ThirdNotPointer) Execute(context.Context, resource.ReadWriteTransaction, Client) error {
	return nil
}
func (*NoResult) Execute(context.Context, resource.ReadWriteTransaction, *Client) {}
func (*ResultNotError) Execute(context.Context, resource.ReadWriteTransaction, *Client) string {
	return ""
}
func (*TwoResults) Execute(context.Context, resource.ReadWriteTransaction, *Client) (Report, error) {
	return Report{}, nil
}
func (*AnswersPointer) Execute(context.Context, resource.Client, *Client) (*Report, error) {
	return nil, nil
}
func (*AnswersBasic) Execute(context.Context, resource.ReadWriteTransaction, *Client) (string, error) {
	return "", nil
}
func (*ThreeResults) Execute(context.Context, resource.ReadWriteTransaction, *Client) (Report, int, error) {
	return Report{}, 0, nil
}

func (*AnswersDeclared) Execute(context.Context, resource.ReadWriteTransaction, *Client) (Verdict, error) {
	return Verdict{}, nil
}
func (*AnswersNoContentPointer) Execute(context.Context, resource.ReadWriteTransaction, *Client) (*Verdict, error) {
	return nil, nil
}
func (*AnswersNoContentValue) Execute(context.Context, resource.ReadWriteTransaction, *Client) (Verdict, error) {
	return Verdict{}, nil
}
func (*AnswerlessNoContent) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
func (*AnswerlessOther) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
func (*AnswersUndeclared) Execute(context.Context, resource.ReadWriteTransaction, *Client) (Verdict, error) {
	return Verdict{}, nil
}
func (*AnswersNoChooser) Execute(context.Context, resource.ReadWriteTransaction, *Client) (Report, error) {
	return Report{}, nil
}
func (*AnswersFrameCode) Execute(context.Context, resource.ReadWriteTransaction, *Client) (Verdict, error) {
	return Verdict{}, nil
}
func (*AnswersServerCode) Execute(context.Context, resource.ReadWriteTransaction, *Client) (Verdict, error) {
	return Verdict{}, nil
}
func (*AnswersDuplicate) Execute(context.Context, resource.ReadWriteTransaction, *Client) (Verdict, error) {
	return Verdict{}, nil
}
func (*AnswersNoSuccess) Execute(context.Context, resource.ReadWriteTransaction, *Client) (Verdict, error) {
	return Verdict{}, nil
}
func (*AnswersNotACode) Execute(context.Context, resource.ReadWriteTransaction, *Client) (Verdict, error) {
	return Verdict{}, nil
}

func (*UploadForm) Execute(context.Context, resource.ReadWriteTransaction, resource.Files, *Client) error {
	return nil
}
func (*UploadAnswers) Execute(context.Context, resource.ReadWriteTransaction, resource.Files, *Client) (Report, error) {
	return Report{}, nil
}
func (*UploadUndeclared) Execute(context.Context, resource.ReadWriteTransaction, resource.Files, *Client) error {
	return nil
}
func (*UploadNoFiles) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
func (*UploadClientForm) Execute(context.Context, resource.Client, resource.Files, *Client) error {
	return nil
}
func (*UploadBadSize) Execute(context.Context, resource.ReadWriteTransaction, resource.Files, *Client) error {
	return nil
}
func (*UploadNoMax) Execute(context.Context, resource.ReadWriteTransaction, resource.Files, *Client) error {
	return nil
}
func (*UploadFilesNotThird) Execute(context.Context, resource.ReadWriteTransaction, *Client, resource.Files) error {
	return nil
}
