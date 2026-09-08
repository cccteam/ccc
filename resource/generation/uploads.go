package generation

import (
	"strings"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

// An upload is its own endpoint form (decided 2026-09-08): an @rpc struct with
// @upload(max: 5MB) whose Execute takes resource.Files. The declaration and the
// signature go together; generation refuses one without the other, naming the
// struct. The maximum is required, since the frame bounds the body before it
// reads a byte.

const uploadMaxArgKey = "max"

// resolveUpload reads and validates the struct's @upload against its Execute.
func resolveUpload(rpcMethod *rpcMethodInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	if !annotations.Struct.Has(uploadKeyword) {
		if rpcMethod.takesFiles {
			return errors.Newf("struct %s: Execute takes resource.Files but the method declares no @%s; declare the maximum body size, for example @%s(max: 5MB)", pStruct.Name(), uploadKeyword, uploadKeyword)
		}

		return nil
	}
	if !rpcMethod.takesFiles {
		return errors.Newf("struct %s: @%s is declared but Execute does not take resource.Files; an upload's Execute is Execute(ctx, txn resource.ReadWriteTransaction, files resource.Files, client *Client)", pStruct.Name(), uploadKeyword)
	}

	invocations, err := annotations.Struct.Get(uploadKeyword).ParseInvocations(&genlang.ArgSpec{
		Keys:     []string{uploadMaxArgKey},
		Required: []string{uploadMaxArgKey},
	})
	if err != nil {
		return errors.Wrapf(err, "@%s on %s", uploadKeyword, pStruct.Name())
	}
	maxText, _ := invocations[0].Named(uploadMaxArgKey)
	maxBytes, err := resource.ParseByteSize(strings.TrimSpace(maxText))
	if err != nil {
		return errors.Wrapf(err, "@%s(%s: %s) on %s", uploadKeyword, uploadMaxArgKey, maxText, pStruct.Name())
	}
	rpcMethod.Upload = &rpcUpload{MaxBytes: maxBytes}

	return nil
}
