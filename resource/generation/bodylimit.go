package generation

import (
	"strconv"
	"strings"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

// rpcMaxArgKey is @rpc's body limit argument: the method's own request body limit.
const rpcMaxArgKey = "max"

// rpcArgSpec is @rpc's argument shape: max:, the body limit, and the words log:,
// fraction:, trace: and rate: (requestlog.go). A bare @rpc takes none.
func rpcArgSpec() *genlang.ArgSpec {
	return &genlang.ArgSpec{Keys: append(append([]string{rpcMaxArgKey}, requestLogArgKeys...), tracesArgKeys...)}
}

// rpcArguments parses @rpc's arguments once, for the body limit and the words; ok
// reports whether the annotation carries any.
func rpcArguments(pStruct *parser.Struct, annotations genlang.StructAnnotations) (args genlang.NamedArgs, ok bool, err error) {
	arg := annotations.Struct.Get(rpcKeyword)
	if arg.Count() == 0 {
		return genlang.NamedArgs{}, false, nil
	}
	invocations, err := arg.ParseInvocations(rpcArgSpec())
	if err != nil {
		return genlang.NamedArgs{}, false, errors.Wrapf(err, "@%s on %s", rpcKeyword, pStruct.Name())
	}

	return invocations[0], true, nil
}

// resolveRPCBodyLimit reads @rpc's max: argument, the method's own request body limit,
// which its generated handler applies in place of the router's BodyLimit. A bare @rpc
// takes the application's limit. An upload declares its maximum on @upload and a
// scheduled method takes no body, so max: on either is refused naming the struct.
func resolveRPCBodyLimit(rpcMethod *rpcMethodInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	args, ok, err := rpcArguments(pStruct, annotations)
	if err != nil {
		return err
	}
	maxText, declared := args.Named(rpcMaxArgKey)
	if !ok || !declared {
		return nil
	}
	maxText = strings.TrimSpace(maxText)
	if rpcMethod.Upload != nil {
		return errors.Newf("struct %s: @%s(%s: %s) on an upload; an upload declares its maximum on @%s(%s: ...), which bounds the whole multipart body", pStruct.Name(), rpcKeyword, rpcMaxArgKey, maxText, uploadKeyword, uploadMaxArgKey)
	}
	if annotations.Struct.Has(scheduleKeyword) {
		return errors.Newf("struct %s: @%s(%s: %s) on a scheduled method, which takes no request body", pStruct.Name(), rpcKeyword, rpcMaxArgKey, maxText)
	}
	maxBytes, err := resource.ParseByteSize(maxText)
	if err != nil {
		return errors.Wrapf(err, "@%s(%s: %s) on %s", rpcKeyword, rpcMaxArgKey, maxText, pStruct.Name())
	}
	rpcMethod.MaxBytes, rpcMethod.MaxBytesText = maxBytes, maxText

	return nil
}

// rpcBodyLimit is the limit a method's generated handler wraps the body with, as the
// expression the handler renders and the words its comment says: the declared maximum
// as a literal, or the router package's BodyLimit when the method declares none, which
// is the literal application limit when no router is generated to declare it.
func (r *resourceGenerator) rpcBodyLimit(method *rpcMethodInfo) (expr, text string) {
	if method.MaxBytes > 0 {
		return strconv.FormatInt(method.MaxBytes, 10), "the method's declared maximum, " + method.MaxBytesText
	}
	if r.genRoutes {
		limit := r.router.Package() + ".BodyLimit"

		return limit, "the application's limit, " + limit
	}

	return strconv.FormatInt(r.bodyLimit, 10), "the application's limit, " + resource.FormatByteSize(r.bodyLimit)
}
