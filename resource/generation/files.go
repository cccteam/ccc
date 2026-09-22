package generation

import (
	"fmt"
	"go/types"
	"regexp"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

// A field or a computed struct declares that a file belongs to the row (@file), and the
// generator serves it: one GET route under the resource's read route, gated by Read on
// the resource and a grant on the route's own field, the segment (content), answering
// the bytes with their type, name, size, time, and validator, and refusing as every
// generated route does.
//
// Two placements. Field scope, on the column holding the store key of a @resource,
// @virtual, or keyed @computed struct: @file bare, or @file(segment), with optional
// name: Field and type: Field naming the sibling columns that carry the file's name
// and media type. The key column goes off the wire in both directions: never returned
// on read or list, never accepted on create or update, absent from the TypeScript
// interface and metadata; a NOT NULL key means a row is added by the @upload method
// that stores its file, so Create is not registered. Struct scope, on a keyed @computed
// struct: @file or @file(segment), and the computed package declares
// <Name><Segment>(ctx, key…, qSet, client, computedClient) (*resource.Content, error)
// beside Read<Name>, rendering the document at request time; a nil content is 404.
// Refused at generation, naming the struct: a key-less struct, an unknown sibling, two
// declarations on one segment, a field that is not a string or a nullable string, a
// struct-scope declaration on anything but a computed struct, a declaration on a struct
// whose read route is suppressed, and a content function that is missing or has another
// signature.

// The named arguments of a field-scope @file.
const (
	fileNameArgKey = "name"
	fileTypeArgKey = "type"
	// defaultFileSegment is the segment a bare @file serves under.
	defaultFileSegment = "content"
)

// fileSegmentPattern is the shape a segment takes: a lowercase path segment,
// kebab-cased, as the generated routes spell every segment.
var fileSegmentPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// fileRoute is one @file declaration: a file served at GET <read route>/<Segment>,
// gated by Read on the resource and on Segment as a route-own field. A stored file
// names its key column (Key) and, optionally, the columns carrying the file's name and
// type; a rendered file (struct scope) has none: it is the computed package's
// <Name><Suffix> function.
type fileRoute struct {
	Segment string
	Key     *fileField
	Name    *fileField
	Type    *fileField
}

// fileField is a column the file route's frame reads for itself: the Go field, its
// declared type (string or *string), and whether the column allows NULL.
type fileField struct {
	Name     string
	Type     string
	Pointer  bool
	Nullable bool
}

// Rendered reports whether the file is rendered by a content function rather than
// opened from the store.
func (f *fileRoute) Rendered() bool {
	return f.Key == nil
}

// Suffix is the segment as the generated handler and the content function spell it:
// Content, Thumbnail.
func (f *fileRoute) Suffix() string {
	return strcase.ToPascal(f.Segment)
}

// FrameFields are the columns the frame projects beside the key: the store key, then
// the name and the type where declared.
func (f *fileRoute) FrameFields() []*fileField {
	var fields []*fileField
	for _, field := range []*fileField{f.Key, f.Name, f.Type} {
		if field != nil {
			fields = append(fields, field)
		}
	}

	return fields
}

// fileArgSpec is @file's argument shape: an optional segment, then name: and type:.
func fileArgSpec() *genlang.ArgSpec {
	return &genlang.ArgSpec{Positional: 1, OptionalPositional: true, Keys: []string{fileNameArgKey, fileTypeArgKey}}
}

// parseFile reads one @file's arguments: the segment, defaulted, and the named siblings.
func parseFile(arg genlang.Arg) (segment, nameField, typeField string, err error) {
	if strings.TrimSpace(string(arg)) == "" {
		// The bare form: the default segment, the file's name and type from the store.
		return defaultFileSegment, "", "", nil
	}
	invocations, err := arg.ParseInvocations(fileArgSpec())
	if err != nil {
		return "", "", "", errors.Wrap(err, "genlang.Arg.ParseInvocations()")
	}
	invocation := invocations[0]
	segment = defaultFileSegment
	if len(invocation.Positional) == 1 {
		segment = strings.TrimSpace(invocation.Positional[0])
	}
	if !fileSegmentPattern.MatchString(segment) {
		return "", "", "", errors.Newf("segment %q is not a route segment; write it lowercase and kebab-cased, like %s", segment, defaultFileSegment)
	}
	nameField, _ = invocation.Named(fileNameArgKey)
	typeField, _ = invocation.Named(fileTypeArgKey)

	return segment, strings.TrimSpace(nameField), strings.TrimSpace(typeField), nil
}

// fileFieldLookup resolves a sibling field a declaration names to the frame's view of
// it: the field's Go type, nullability, and whether the struct declares it at all.
type fileFieldLookup func(name string) (goType types.Type, nullable bool, ok bool)

// fileFieldOf checks a column the frame reads: it exists and is a string or a
// nullable string (*string).
func fileFieldOf(structName, declaring, role, name string, lookup fileFieldLookup) (*fileField, error) {
	goType, nullable, ok := lookup(name)
	if !ok {
		return nil, errors.Newf("struct %s field %s: @%s names %s %s, which is not a field of the struct", structName, declaring, fileKeyword, role, name)
	}
	t := types.Unalias(goType)
	pointer := false
	if p, isPointer := t.(*types.Pointer); isPointer {
		pointer = true
		t = types.Unalias(p.Elem())
	}
	if !types.Identical(t, types.Typ[types.String]) {
		return nil, errors.Newf("struct %s field %s: the %s of a @%s is a string or a nullable string (*string), not %s", structName, name, role, fileKeyword, typeStringer(goType))
	}

	return &fileField{Name: name, Type: typeStringer(goType), Pointer: pointer, Nullable: nullable || pointer}, nil
}

// fileRouteOf builds the stored-file route a field-scope @file declares.
func fileRouteOf(structName, keyField string, arg genlang.Arg, lookup fileFieldLookup) (*fileRoute, error) {
	segment, nameField, typeField, err := parseFile(arg)
	if err != nil {
		return nil, errors.Wrapf(err, "struct %s field %s: @%s", structName, keyField, fileKeyword)
	}
	route := &fileRoute{Segment: segment}
	if route.Key, err = fileFieldOf(structName, keyField, "store key", keyField, lookup); err != nil {
		return nil, err
	}
	if nameField != "" {
		if route.Name, err = fileFieldOf(structName, keyField, "name field", nameField, lookup); err != nil {
			return nil, err
		}
	}
	if typeField != "" {
		if route.Type, err = fileFieldOf(structName, keyField, "type field", typeField, lookup); err != nil {
			return nil, err
		}
	}

	return route, nil
}

// checkFileRoutes enforces what every kind shares: a keyed struct, a read route, and
// one declaration per segment.
func checkFileRoutes(structName string, files []*fileRoute, keyed, readDisabled bool) error {
	if len(files) == 0 {
		return nil
	}
	if !keyed {
		return errors.Newf("struct %s: @%s needs a row to belong to, and %s declares no @%s; declare the key, or drop @%s", structName, fileKeyword, structName, primarykeyKeyword, fileKeyword)
	}
	if readDisabled {
		return errors.Newf("struct %s: @%s serves the file under the read route, which %s suppresses; remove @%s(%s), or drop @%s", structName, fileKeyword, structName, suppressKeyword, ReadHandler, fileKeyword)
	}
	seen := make(map[string]string, len(files))
	for _, file := range files {
		declaring := "the struct"
		if file.Key != nil {
			declaring = "field " + file.Key.Name
		}
		if prior, dup := seen[file.Segment]; dup {
			return errors.Newf("struct %s: @%s declares segment %q twice, on %s and on %s; give one another segment, @%s(%s)", structName, fileKeyword, file.Segment, prior, declaring, fileKeyword, "thumbnail")
		}
		seen[file.Segment] = declaring
	}

	return nil
}

// resolveResourceFiles reads a table or view struct's @file declarations onto res:
// field-scope only, since a table row's file is stored, never rendered.
func resolveResourceFiles(res *resourceInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	if annotations.Struct.Has(fileKeyword) {
		return errors.Newf("struct %s: a struct-scope @%s is a rendered file, which is a @%s struct's content; on a table-backed or virtual resource, @%s goes on the field holding the store key", pStruct.Name(), fileKeyword, computedKeyword, fileKeyword)
	}

	byName := make(map[string]*resourceField, len(res.Fields))
	for _, field := range res.Fields {
		byName[field.Name()] = field
	}
	lookup := func(name string) (types.Type, bool, bool) {
		field, ok := byName[name]
		if !ok {
			return nil, false, false
		}

		return field.GoType(), field.IsNullable, true
	}

	for i, pField := range pStruct.Fields() {
		if !annotations.Fields[i].Has(fileKeyword) {
			continue
		}
		field, ok := byName[pField.Name()]
		if !ok {
			return errors.Newf("struct %s field %s: @%s requires a schema-backed field", pStruct.Name(), pField.Name(), fileKeyword)
		}
		if field.IsPrimaryKey {
			return errors.Newf("struct %s field %s: @%s goes on the column holding the store key, not on the primary key", pStruct.Name(), pField.Name(), fileKeyword)
		}
		route, err := fileRouteOf(pStruct.Name(), pField.Name(), annotations.Fields[i].Get(fileKeyword), lookup)
		if err != nil {
			return err
		}
		field.IsFileKey = true
		res.Files = append(res.Files, route)
	}

	return checkFileRoutes(pStruct.Name(), res.Files, res.HasPrimaryKey(), res.ReadHandlerDisabled())
}

// resolveComputedFiles reads a computed struct's @file declarations onto res: the
// struct-scope rendered file, and the field-scope stored files.
func resolveComputedFiles(res *computedResource, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	if annotations.Struct.Has(fileKeyword) {
		segment, nameField, typeField, err := parseFile(annotations.Struct.Get(fileKeyword))
		if err != nil {
			return errors.Wrapf(err, "struct %s: @%s", pStruct.Name(), fileKeyword)
		}
		if nameField != "" || typeField != "" {
			return errors.Newf("struct %s: a struct-scope @%s renders its file, whose name and type come from the resource.Content the content function returns; %s: and %s: name columns of a stored file", pStruct.Name(), fileKeyword, fileNameArgKey, fileTypeArgKey)
		}
		res.Files = append(res.Files, &fileRoute{Segment: segment})
	}

	byName := make(map[string]*computedField, len(res.Fields))
	for _, field := range res.Fields {
		byName[field.Name()] = field
	}
	lookup := func(name string) (types.Type, bool, bool) {
		field, ok := byName[name]
		if !ok {
			return nil, false, false
		}

		return field.GoType(), field.IsPointer(), true
	}

	for i, pField := range pStruct.Fields() {
		if !annotations.Fields[i].Has(fileKeyword) {
			continue
		}
		field := byName[pField.Name()]
		if field.IsPrimaryKey {
			return errors.Newf("struct %s field %s: @%s goes on the field holding the store key, not on the primary key", pStruct.Name(), pField.Name(), fileKeyword)
		}
		route, err := fileRouteOf(pStruct.Name(), pField.Name(), annotations.Fields[i].Get(fileKeyword), lookup)
		if err != nil {
			return err
		}
		field.IsFileKey = true
		res.Files = append(res.Files, route)
	}

	return checkFileRoutes(pStruct.Name(), res.Files, res.HasPrimaryKey(), res.ReadHandlerDisabled())
}

// rejectFileAnnotations refuses @file on a kind that serves no read route to hang a
// file under: an RPC method.
func rejectFileAnnotations(pStruct *parser.Struct, annotations genlang.StructAnnotations, kind string) error {
	var errs []error
	if annotations.Struct.Has(fileKeyword) {
		errs = append(errs, errors.Newf("struct %s: @%s serves a file under a resource's read route; a %s has none", pStruct.Name(), fileKeyword, kind))
	}
	for i, field := range pStruct.Fields() {
		if annotations.Fields[i].Has(fileKeyword) {
			errs = append(errs, errors.Newf("struct %s field %s: @%s serves a file under a resource's read route; a %s has none", pStruct.Name(), field.Name(), fileKeyword, kind))
		}
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "file annotation error")
	}

	return nil
}

// contentFunctionName is the content function a struct-scope @file expects beside
// Read<Name>: <Name><Segment>.
func contentFunctionName(res *computedResource, file *fileRoute) string {
	return res.Name() + file.Suffix()
}

// contentFunctionSignature renders the signature a content function must have, for
// the refusal.
func contentFunctionSignature(res *computedResource, file *fileRoute, computedPackage string) string {
	params := make([]string, 0, len(res.PrimaryKeys())+4)
	params = append(params, "ctx context.Context")
	for _, key := range res.PrimaryKeys() {
		params = append(params, strcase.ToGoCamel(key.Name())+" "+key.Type())
	}
	params = append(params, fmt.Sprintf("qSet *resource.QuerySet[%s]", res.Name()), "client resource.Client", "computedClient *Client")

	return fmt.Sprintf("func %s(%s) (*resource.Content, error) in package %s", contentFunctionName(res, file), strings.Join(params, ", "), computedPackage)
}

// validateComputedContentFunctions checks, for every struct-scope @file, that the
// computed package declares the content function with the signature the generated
// handler calls: the key parameters as Read<Name> takes them, the QuerySet carrying the
// checked scope and identity, the resource client, and the computed client, answering
// (*resource.Content, error). Checked here, before anything renders, so the refusal
// names the function and its shape instead of a compile error in generated code.
func validateComputedContentFunctions(pkg *types.Package, resources []*computedResource) error {
	if pkg == nil {
		return nil
	}
	var errs []error
	for _, res := range resources {
		for _, file := range res.Files {
			if !file.Rendered() {
				continue
			}
			if err := checkContentFunction(pkg, res, file); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "content function errors")
	}

	return nil
}

func checkContentFunction(pkg *types.Package, res *computedResource, file *fileRoute) error {
	name := contentFunctionName(res, file)
	want := contentFunctionSignature(res, file, pkg.Name())
	obj := pkg.Scope().Lookup(name)
	if obj == nil {
		return errors.Newf("struct %s declares @%s(%s) but package %s declares no %s; declare %s", res.Name(), fileKeyword, file.Segment, pkg.Name(), name, want)
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return errors.Newf("struct %s: %s in package %s is not a function; declare %s", res.Name(), name, pkg.Name(), want)
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Variadic() {
		return errors.Newf("struct %s: %s has an unexpected signature; declare %s", res.Name(), name, want)
	}

	keys := res.PrimaryKeys()
	params := sig.Params()
	if params.Len() != len(keys)+4 {
		return errors.Newf("struct %s: %s takes %s, not the key, the QuerySet, and the clients; declare %s", res.Name(), name, paramsString(sig), want)
	}
	if !isNamedType(params.At(0).Type(), "context", "Context") {
		return errors.Newf("struct %s: %s's first parameter is %s, not context.Context; declare %s", res.Name(), name, typeStringer(params.At(0).Type()), want)
	}
	for i, key := range keys {
		if got := params.At(1 + i).Type(); !types.Identical(got, key.GoType()) {
			return errors.Newf("struct %s: %s's parameter %d is %s, not the key field %s's %s; declare %s", res.Name(), name, i+2, typeStringer(got), key.Name(), key.Type(), want)
		}
	}
	qSetParam := params.At(1 + len(keys)).Type()
	if ptr, isPtr := types.Unalias(qSetParam).(*types.Pointer); !isPtr || !isNamedType(ptr.Elem(), resourcePackagePath, "QuerySet") || !querySetOf(ptr.Elem(), res) {
		return errors.Newf("struct %s: %s's parameter %d is %s, not *resource.QuerySet[%s]; declare %s", res.Name(), name, len(keys)+2, typeStringer(qSetParam), res.Name(), want)
	}
	if !isNamedType(params.At(2+len(keys)).Type(), resourcePackagePath, "Client") {
		return errors.Newf("struct %s: %s's parameter %d is %s, not resource.Client; declare %s", res.Name(), name, len(keys)+3, typeStringer(params.At(2+len(keys)).Type()), want)
	}
	if client, isPtr := params.At(3 + len(keys)).Type().(*types.Pointer); !isPtr || !isNamed(client.Elem()) {
		return errors.Newf("struct %s: %s's last parameter is %s, not a pointer to the computed package's Client; declare %s", res.Name(), name, typeStringer(params.At(3+len(keys)).Type()), want)
	}

	results := sig.Results()
	errType := types.Universe.Lookup("error").Type()
	if results.Len() != 2 || !types.Identical(results.At(1).Type(), errType) {
		return errors.Newf("struct %s: %s returns %s, not (*resource.Content, error); declare %s", res.Name(), name, typeTupleString(results), want)
	}
	if ptr, isPtr := types.Unalias(results.At(0).Type()).(*types.Pointer); !isPtr || !isNamedType(ptr.Elem(), resourcePackagePath, "Content") {
		return errors.Newf("struct %s: %s answers with %s, not *resource.Content; declare %s", res.Name(), name, typeStringer(results.At(0).Type()), want)
	}

	return nil
}

// querySetOf reports whether the named resource.QuerySet is instantiated over the
// computed struct.
func querySetOf(t types.Type, res *computedResource) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.TypeArgs().Len() != 1 {
		return false
	}

	return types.Identical(named.TypeArgs().At(0), res.GoType())
}

// fileSetTags renders the route-own fields the Read set registers for a struct's file
// routes: one tag per segment, with no column behind it, so a Read grant may name it
// and columns= on the read route still knows nothing of it.
func fileSetTags(files []*fileRoute) []resource.FieldTags {
	tags := make([]resource.FieldTags, 0, len(files))
	for _, file := range files {
		tags = append(tags, resource.FieldTags{Field: accesstypes.Field(file.Suffix()), JSON: file.Segment})
	}

	return tags
}

// fileSegments lists a struct's file segments for the TypeScript descriptor.
func fileSegments(files []*fileRoute) []string {
	segments := make([]string, 0, len(files))
	for _, file := range files {
		segments = append(segments, file.Segment)
	}

	return segments
}

// fileRouteFrom derives a file route from the read route it hangs under: the same
// parameters, the segment appended, the handler named for the resource and segment.
func fileRouteFrom(read *generatedRoute, resourceName string, file *fileRoute) *generatedRoute {
	return &generatedRoute{
		Method:       fileHandler.method(),
		Path:         read.Path + "/" + file.Segment,
		HandlerFunc:  resourceName + file.Suffix(),
		HandlerType:  fileHandler,
		DomainScoped: read.DomainScoped,
		TestURL:      read.TestURL + "/" + file.Segment,
		TestParams:   slices.Clone(read.TestParams),
	}
}

// keyParamList renders the key parameters as the generated handlers name them: id for
// a single key, the camel-cased field names for a compound key.
func keyParamList(compound bool, names []string) string {
	if !compound {
		return idIdentifier
	}
	params := make([]string, 0, len(names))
	for _, name := range names {
		params = append(params, strcase.ToGoCamel(name))
	}

	return strings.Join(params, ", ")
}

// keyFormat renders one %v per key part, slash-joined.
func keyFormat(count int) string {
	parts := make([]string, 0, count)
	for range count {
		parts = append(parts, "%v")
	}

	return strings.Join(parts, "/")
}
