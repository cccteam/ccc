package generation

import (
	"go/types"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

// The JSON pair of a defined type. A defined type inherits none of the methods of the
// type it is declared over, so `type Payload json.RawMessage` marshals as its underlying
// byte slice (base64) where json.RawMessage writes JSON, and `type Stamp time.Time` as
// a struct with no exported fields where time.Time writes a string. The generator gives
// the pair back: for every defined type a field uses on any path (a table or view
// column, a computed field, an RPC request or result field, and the fields of every
// struct those reach) whose right-hand side is a type with JSON methods, and which has
// none of its own, it writes MarshalJSON and UnmarshalJSON converting to and from the
// right-hand side into a zz_gen_json.go in the type's package, so the type is its
// declaration and its @typescript annotation and nothing else. A type that implements
// the pair itself keeps it, and one method without the other is refused, as the storage
// pair is; a type declared in a package the generator does not write into is refused
// naming the fixes (writablePackages).

// jsonOutputName is the stem of the JSON-methods file.
const jsonOutputName = "json"

// jsonMethods are the encoding/json methods the pair consists of, on either receiver.
var jsonMethods = []string{"MarshalJSON", "UnmarshalJSON"}

// jsonPair is one generated pair: the defined type and the type it is declared over,
// spelled as the generated file's package sees it.
type jsonPair struct {
	Name string
	// Over is the right-hand side, qualified by package name when it is another
	// package's.
	Over string
	// imports are the packages Over reaches, for the import fixer.
	imports []fixerImport
}

// fieldUse is where a field uses a type, for the refusal's path.
type fieldUse struct {
	path  string
	named *types.Named
}

// resolveJSONMethods finds every defined type a field uses on any path that needs the
// generated JSON pair, by the package that declares it, refusing the ones it cannot
// carry: one method without the other, and a type declared where the generator writes
// nothing.
func (r *resourceGenerator) resolveJSONMethods() (map[packageDir][]jsonPair, error) {
	writable := r.writablePackages()

	pairs := make(map[packageDir][]jsonPair)
	var errs []error
	for _, use := range r.fieldTypes() {
		over, err := r.leaves().jsonRHS(use.named)
		if err != nil {
			// The TypeScript pass reports the type; the pair has nothing to add.
			continue
		}
		if over == nil {
			continue
		}
		own, err := r.ownMethodPair(use.named, jsonMethods, "carry")
		if err != nil {
			errs = append(errs, errors.Newf("%s: %s", use.path, errors.Cause(err).Error()))

			continue
		}
		if own {
			continue
		}
		pkg := use.named.Obj().Pkg()
		dir, ok := writable[pkg.Path()]
		if !ok {
			errs = append(errs, errors.Newf("%s: %s is carried by generated JSON methods, but it is declared in %s, where the generator writes nothing; declare the type in the resources package, implement %s on it, or name its package with WithTypes", use.path, typeStringer(use.named), pkg.Path(), strings.Join(jsonMethods, " and ")))

			continue
		}
		pair := jsonPair{
			Name:    use.named.Obj().Name(),
			Over:    types.TypeString(over, qualifierOutside(pkg)),
			imports: packageImports(over, pkg),
		}
		if !slices.ContainsFunc(pairs[dir], func(p jsonPair) bool {
			return p.Name == pair.Name
		}) {
			pairs[dir] = append(pairs[dir], pair)
		}
	}
	if len(errs) > 0 {
		return nil, errors.Wrapf(errors.Join(errs...), "encountered %d JSON method errors", len(errs))
	}
	for _, list := range pairs {
		slices.SortFunc(list, func(a, b jsonPair) int {
			return strings.Compare(a.Name, b.Name)
		})
	}

	return pairs, nil
}

// qualifierOutside qualifies a type by package name everywhere but inside pkg, where
// its own types are unqualified.
func qualifierOutside(pkg *types.Package) types.Qualifier {
	return func(p *types.Package) string {
		if p == pkg {
			return ""
		}

		return p.Name()
	}
}

// packageImports lists the packages the type reaches other than pkg, as the import
// fixer takes them: a named type's package, and its type arguments' packages.
func packageImports(t types.Type, pkg *types.Package) []fixerImport {
	var imports []fixerImport
	var walk func(types.Type)
	walk = func(t types.Type) {
		named, ok := types.Unalias(t).(*types.Named)
		if !ok {
			return
		}
		if p := named.Obj().Pkg(); p != nil && p != pkg {
			imports = append(imports, fixerImport{name: p.Name(), path: p.Path()})
		}
		for i := range named.TypeArgs().Len() {
			walk(named.TypeArgs().At(i))
		}
	}
	walk(t)

	return imports
}

// fieldTypes lists every named type a field uses on any path, in a stable order: the
// table and view columns, the computed fields, and the RPC request and result fields,
// each read through pointers and slices, and the fields of every struct they reach that
// is no leaf and does not write its own JSON (its fields marshal as declared inside it,
// so their types' pairs matter as the field's own does). A leaf is a candidate too and
// is not walked; a struct whose JSON is its own carries its fields off the wire.
func (r *resourceGenerator) fieldTypes() []fieldUse {
	seen := make(map[string]bool)
	var uses []fieldUse
	var walk func(t types.Type, path string)
	walk = func(t types.Type, path string) {
		switch u := types.Unalias(t).(type) {
		case *types.Pointer:
			walk(u.Elem(), path)
		case *types.Slice:
			walk(u.Elem(), path)
		case *types.Array:
			walk(u.Elem(), path)
		case *types.Named:
			key := types.TypeString(u, (*types.Package).Path)
			if seen[key] {
				return
			}
			seen[key] = true
			uses = append(uses, fieldUse{path: path, named: u})
			if _, ok, err := r.leaves().resolveNamed(u); err != nil || ok {
				return
			}
			if writes, err := r.leaves().writesJSON(u); err != nil || writes {
				return
			}
			st, isStruct := u.Underlying().(*types.Struct)
			if !isStruct {
				return
			}
			for i := range st.NumFields() {
				field := st.Field(i)
				if !field.Exported() || jsonTagOmits(st.Tag(i)) {
					continue
				}
				walk(field.Type(), path+"."+field.Name())
			}
		}
	}

	for _, res := range r.resources {
		for _, field := range res.Fields {
			walk(field.GoType(), r.pluralize(res.Name())+"."+field.Name())
		}
	}
	for _, res := range r.computedResources {
		for _, field := range res.Fields {
			walk(field.GoType(), r.pluralize(res.Name())+"."+field.Name())
		}
	}
	for _, method := range r.rpcMethods {
		for _, field := range method.Fields {
			walk(field.GoType(), method.Name()+"."+field.Name())
		}
		if method.ResultNamed != nil {
			walk(method.ResultNamed, method.Name()+" result")
		}
	}

	return uses
}

// jsonTagOmits reports whether a struct tag leaves the field off the wire (json:"-").
func jsonTagOmits(tag string) bool {
	name, _, _ := strings.Cut(reflect.StructTag(tag).Get(jsonTagKey), ",")

	return name == "-"
}

// methodFileNames are the generated files a writable package may carry, and the only
// files the method pass ever removes.
var methodFileNames = []string{generatedGoFileName(storageOutputName), generatedGoFileName(jsonOutputName)}

// runMethodGeneration writes the generated method files, the Spanner storage pair and
// the JSON pair, one file each per package that declares a type needing them. It runs
// once every path is extracted (the RPC methods last), so the JSON pair covers each
// field on every path, and after every package sweep. The two files are removed from
// every writable package first, so a package whose types no longer need a pair loses
// its stale file; nothing else in a WithTypes or computed package is touched.
func (r *resourceGenerator) runMethodGeneration(storage map[packageDir][]string) error {
	pairs, err := r.resolveJSONMethods()
	if err != nil {
		return err
	}
	for _, dir := range r.writableDirs() {
		for _, name := range methodFileNames {
			if err := os.Remove(filepath.Join(dir.Dir(), name)); err != nil && !os.IsNotExist(err) {
				return errors.Wrap(err, "os.Remove()")
			}
		}
	}
	if err := r.generateStorageFiles(storage); err != nil {
		return err
	}

	return r.generateJSONFiles(pairs)
}

// generateJSONFiles writes one zz_gen_json.go per package that declares a type needing
// the generated pair.
func (r *resourceGenerator) generateJSONFiles(pairs map[packageDir][]jsonPair) error {
	for dir, list := range pairs {
		begin := time.Now()
		destination := filepath.Join(dir.Dir(), generatedGoFileName(jsonOutputName))
		if err := r.writeFormattedGoFile(destination, "jsonFileTemplate", jsonFileTemplate, &jsonFileData{
			Source:  r.resource.Dir(),
			Package: dir.Package(),
			Types:   list,
		}); err != nil {
			return errors.Wrap(err, "writeFormattedGoFile()")
		}

		log.Printf("Generated JSON methods in %s: %s\n", time.Since(begin), destination)
	}

	return nil
}
