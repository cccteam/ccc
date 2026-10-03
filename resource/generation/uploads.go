package generation

import (
	"go/types"
	"strings"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

// An upload is its own endpoint form (decided 2026-09-08): an @rpc struct with
// @upload(max: 5MB) whose Execute takes resource.Files, or @upload(max: 5MB, store: S)
// whose Execute takes resource.FilesIn[S] for the named store S. The declaration and
// the signature go together; generation refuses one without the other, naming the
// struct, and a store named on one side and not the other, or two different stores.
// The maximum is required, since the frame bounds the body before it reads a byte.
// Once every kind is extracted, an upload's store must be held by some @file column
// (validateUploadStores): a key with no column to land in would be the orphaned-file
// cleanup's the moment it was stored.

// The named arguments of @upload.
const (
	uploadMaxArgKey   = "max"
	uploadStoreArgKey = "store"
)

// resolveUpload reads and validates the struct's @upload against its Execute.
func resolveUpload(rpcMethod *rpcMethodInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	if !annotations.Struct.Has(uploadKeyword) {
		if rpcMethod.takesFiles {
			return errors.Newf("struct %s: Execute takes %s but the method declares no @%s; declare the maximum body size, for example @%s(max: 5MB)", pStruct.Name(), filesTypeOf(rpcMethod.filesStore), uploadKeyword, uploadKeyword)
		}

		return nil
	}
	if !rpcMethod.takesFiles {
		return errors.Newf("struct %s: @%s is declared but Execute does not take resource.Files; an upload's Execute is Execute(ctx, txn resource.ReadWriteTransaction, files resource.Files, client *Client), or files resource.FilesIn[S] for the named store S", pStruct.Name(), uploadKeyword)
	}

	invocations, err := annotations.Struct.Get(uploadKeyword).ParseInvocations(&genlang.ArgSpec{
		Keys:     []string{uploadMaxArgKey, uploadStoreArgKey},
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
	storeText, _ := invocations[0].Named(uploadStoreArgKey)
	if err := checkUploadStore(pStruct, rpcMethod.filesStore, strings.TrimSpace(storeText)); err != nil {
		return err
	}
	rpcMethod.Upload = &rpcUpload{MaxBytes: maxBytes, Store: rpcMethod.filesStore}

	return nil
}

// checkUploadStore holds the declaration's store: argument and the Execute signature's
// Files type to one store: both the default (no argument, resource.Files), or both the
// named store S (store: S, resource.FilesIn[S]).
func checkUploadStore(pStruct *parser.Struct, filesStore *types.Named, storeText string) error {
	switch {
	case storeText == "" && filesStore == nil:
		return nil
	case storeText == "":
		return errors.Newf("struct %s: Execute takes %s, the named store's files, but @%s names no store; declare @%s(max: ..., %s: %s)", pStruct.Name(), filesTypeOf(filesStore), uploadKeyword, uploadKeyword, uploadStoreArgKey, typeStringer(filesStore))
	case filesStore == nil:
		return errors.Newf("struct %s: @%s names %s: %s but Execute takes resource.Files, the default store's; take resource.FilesIn[%s], or drop the argument", pStruct.Name(), uploadKeyword, uploadStoreArgKey, storeText, storeText)
	}
	if !storeTextNames(storeText, filesStore, structPackage(pStruct)) {
		return errors.Newf("struct %s: @%s names %s: %s but Execute takes %s; the declaration and the signature name one store", pStruct.Name(), uploadKeyword, uploadStoreArgKey, storeText, filesTypeOf(filesStore))
	}

	return nil
}

// storeTextNames reports whether the store: argument, as written in the declaring
// package, names the store type: the unqualified name for a type of that package, or
// the package-qualified name otherwise.
func storeTextNames(text string, store *types.Named, pkg *types.Package) bool {
	obj := store.Obj()
	if obj.Pkg() == nil {
		return text == obj.Name()
	}
	if obj.Pkg() == pkg && text == obj.Name() {
		return true
	}

	return text == obj.Pkg().Name()+"."+obj.Name()
}

// filesTypeOf spells the Files type an Execute takes for a store: resource.Files for
// the default, resource.FilesIn[S] for the named store S.
func filesTypeOf(store *types.Named) string {
	if store == nil {
		return "resource.Files"
	}

	return "resource.FilesIn[" + typeStringer(store) + "]"
}
