package generation

import (
	"fmt"
	"strings"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

// @formerly(Name) declares a field's or a method's former name, so a browser
// application built before the rename keeps working while a newer release answers it:
// the column never changes, only the wire name moves. On a field of a @resource or
// @virtual struct, or a request or result field of an @rpc struct, the former wire name
// (derived from the name as the current wire name is) is accepted in bodies, columns,
// sort and filter beside the current one, and every row carries both keys; on an @rpc
// struct the former route is registered beside the current one. The generated
// collection carries the former names for the role migration, which writes grant rows
// for both. A former name that is the current one, or another field's or method's name,
// current or former, is refused, and a @computed struct refuses the annotation: its
// rows are hand-written.

// resolveFieldFormerly resolves a table or view resource's @formerly declarations: each
// renamed field's former name, refusing the struct form (a resource keeps its name), a
// former name equal to the field's current wire name, and one that is another field's
// wire name or former name.
func resolveFieldFormerly(res *resourceInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	if annotations.Struct.Has(formerlyKeyword) {
		return errors.Newf("struct %s: @%s renames a field or a method; a resource keeps its name", pStruct.Name(), formerlyKeyword)
	}

	byWire := make(map[string]string, len(res.Fields))
	for _, field := range res.Fields {
		byWire[formerWireName(field.Name())] = field.Name()
	}

	var errs []error
	former := make(map[string]string, len(res.Fields))
	for i, field := range res.Fields {
		fieldAnnotations := annotationsOfField(pStruct, annotations, field.Name())
		if !fieldAnnotations.Has(formerlyKeyword) {
			continue
		}
		where := fmt.Sprintf("struct %s field %s", pStruct.Name(), field.Name())
		name, err := formerNameOf(fieldAnnotations.Get(formerlyKeyword), where)
		if err != nil {
			errs = append(errs, err)

			continue
		}
		if err := checkFormerWireName(where, field.Name(), name, byWire, former); err != nil {
			errs = append(errs, err)

			continue
		}
		res.Fields[i].Formerly = name
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "formerly annotation error")
	}

	return nil
}

// checkFormerWireName refuses a former name whose wire name is the field's own, another
// field's (byWire, every field's wire name to its field), or one another field already
// claims as its former name (former, which the check records into).
func checkFormerWireName(where, fieldName, name string, byWire, former map[string]string) error {
	wire := formerWireName(name)
	if other, ok := byWire[wire]; ok {
		if other == fieldName {
			return errors.Newf("%s: @%s(%s) names the field's current name; a former name is the one older applications still send", where, formerlyKeyword, name)
		}

		return errors.Newf("%s: @%s(%s) names field %s's wire name %q; a former name belongs to no field", where, formerlyKeyword, name, other, wire)
	}
	if other, ok := former[wire]; ok {
		return errors.Newf("%s: @%s(%s) names the former name field %s already carries; a former name belongs to one field", where, formerlyKeyword, name, other)
	}
	former[wire] = fieldName

	return nil
}

// formerNameOf reads the one name a @formerly carries.
func formerNameOf(arg genlang.Arg, where string) (string, error) {
	name := strings.TrimSpace(string(arg))
	if name == "" || strings.ContainsAny(name, ", \t\"()") {
		return "", errors.Newf("%s: @%s(%s) takes one name, written as the field or method was: @%s(Title)", where, formerlyKeyword, name, formerlyKeyword)
	}

	return name, nil
}

// rejectFormerlyAnnotations refuses @formerly on a kind that cannot carry it, on the
// struct or any field.
func rejectFormerlyAnnotations(pStruct *parser.Struct, annotations genlang.StructAnnotations, kind string) error {
	var errs []error
	if annotations.Struct.Has(formerlyKeyword) {
		errs = append(errs, errors.Newf("struct %s: @%s renames a field of a resource or an RPC method; a %s carries no former name", pStruct.Name(), formerlyKeyword, kind))
	}
	for i, field := range pStruct.Fields() {
		if i < len(annotations.Fields) && annotations.Fields[i].Has(formerlyKeyword) {
			errs = append(errs, errors.Newf("struct %s field %s: @%s renames a field of a resource or an RPC method; a %s carries no former name", pStruct.Name(), field.Name(), formerlyKeyword, kind))
		}
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "formerly annotation error")
	}

	return nil
}

// resolveRPCFormerly resolves an RPC method's @formerly declarations: the method's
// former name on the struct, each request field's, and each result field's where the
// result struct is declared in the method's package (structsByName), since that is
// where its doc comments are read. The former names are checked against the method's
// own name and the fields' wire names as a resource's are; former names across
// methods are checked once every method is extracted (validateFormerMethodNames).
func resolveRPCFormerly(method *rpcMethodInfo, annotations genlang.StructAnnotations, structsByName map[string]*parser.Struct) error {
	var errs []error
	if annotations.Struct.Has(formerlyKeyword) {
		name, err := formerNameOf(annotations.Struct.Get(formerlyKeyword), "struct "+method.Name())
		switch {
		case err != nil:
			errs = append(errs, err)
		case name == method.Name():
			errs = append(errs, errors.Newf("struct %s: @%s(%s) names the method's current name; a former name is the one older applications still execute", method.Name(), formerlyKeyword, name))
		default:
			method.Formerly = name
		}
	}

	byWire := make(map[string]string, len(method.Fields))
	for _, field := range method.Fields {
		byWire[formerWireName(field.Name())] = field.Name()
	}
	former := make(map[string]string, len(method.Fields))
	for i, field := range method.Struct.Fields() {
		if i >= len(annotations.Fields) || !annotations.Fields[i].Has(formerlyKeyword) {
			continue
		}
		where := fmt.Sprintf("struct %s field %s", method.Name(), field.Name())
		name, err := formerNameOf(annotations.Fields[i].Get(formerlyKeyword), where)
		if err != nil {
			errs = append(errs, err)

			continue
		}
		if err := checkFormerWireName(where, field.Name(), name, byWire, former); err != nil {
			errs = append(errs, err)

			continue
		}
		method.Fields[i].Formerly = name
	}

	if err := resolveResultFormerly(method, structsByName); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "formerly annotation error")
	}

	return nil
}

// resolveResultFormerly resolves @formerly on the fields of the struct a method answers
// with, read from the struct's declaration in the method's package; a result declared
// elsewhere carries none, as its doc comments are not read.
func resolveResultFormerly(method *rpcMethodInfo, structsByName map[string]*parser.Struct) error {
	if method.Result == nil || method.ResultNamed == nil {
		return nil
	}
	pStruct, ok := structsByName[method.ResultNamed.Obj().Name()]
	if !ok || pStruct == nil {
		return nil
	}
	annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(pStruct)
	if err != nil {
		return errors.Wrapf(err, "scanning %s, the result of %s", pStruct.Name(), method.Name())
	}
	if annotations.Struct.Has(formerlyKeyword) {
		return errors.Newf("struct %s: @%s on the result of %s renames a field; put it on the field", pStruct.Name(), formerlyKeyword, method.Name())
	}

	byWire := make(map[string]string, len(method.Result.Fields))
	byName := make(map[string]*wireField, len(method.Result.Fields))
	for _, field := range method.Result.Fields {
		byWire[field.JSONName] = field.Name
		byName[field.Name] = field
	}
	var errs []error
	former := make(map[string]string, len(method.Result.Fields))
	for i, field := range pStruct.Fields() {
		if i >= len(annotations.Fields) || !annotations.Fields[i].Has(formerlyKeyword) {
			continue
		}
		where := fmt.Sprintf("struct %s field %s, the result of %s", pStruct.Name(), field.Name(), method.Name())
		name, err := formerNameOf(annotations.Fields[i].Get(formerlyKeyword), where)
		if err != nil {
			errs = append(errs, err)

			continue
		}
		if err := checkFormerWireName(where, field.Name(), name, byWire, former); err != nil {
			errs = append(errs, err)

			continue
		}
		if wire, ok := byName[field.Name()]; ok {
			wire.FormerName = formerWireName(name)
		}
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "result former names")
	}

	return nil
}

// validateFormerMethodNames refuses a method's former name that is another method's
// name or former name: the former route would collide with a route that answers.
func validateFormerMethodNames(methods []*rpcMethodInfo) error {
	names := make(map[string]string, len(methods))
	for _, method := range methods {
		names[method.Name()] = method.Name()
	}
	var errs []error
	former := make(map[string]string, len(methods))
	for _, method := range methods {
		if method.Formerly == "" {
			continue
		}
		if other, ok := names[method.Formerly]; ok {
			errs = append(errs, errors.Newf("struct %s: @%s(%s) names method %s; a former name belongs to no method", method.Name(), formerlyKeyword, method.Formerly, other))

			continue
		}
		if other, ok := former[method.Formerly]; ok {
			errs = append(errs, errors.Newf("struct %s: @%s(%s) names the former name method %s already carries; a former name belongs to one method", method.Name(), formerlyKeyword, method.Formerly, other))

			continue
		}
		former[method.Formerly] = method.Name()
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "formerly annotation error")
	}

	return nil
}
