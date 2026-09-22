package generation

import (
	"go/types"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

// Storage for a struct-typed column. A struct, a named slice of structs, a type
// carrying a @typescript declaration, or a type JSON by its declaration, held by a JSON
// column, needs Spanner's Encoder and Decoder to cross into and out of the column; an
// application writes neither. The generator emits them into a zz_gen_storage.go in the
// package that declares the type, so the type's own file carries nothing but the
// shape. A type that implements the pair itself keeps them, whatever column it
// handles. A type that needs the generated pair on a column that is not JSON is
// refused, naming the column and the two fixes, and so is one declared in a package
// the generator does not write into (writablePackages).

// storageOutputName is the stem of the storage-methods file.
const storageOutputName = "storage"

// jsonSpannerType is how INFORMATION_SCHEMA.COLUMNS spells a JSON column.
const jsonSpannerType = "JSON"

// storageMethods are the Spanner methods a column type carries, on either receiver.
var storageMethods = []string{"EncodeSpanner", "DecodeSpanner"}

// writablePackages is the set of packages the generator writes method pairs into, by
// import path: the resources package, the virtual, computed, and RPC packages when
// configured, and every package WithTypes names. A package the run did not load (a
// directory with no Go package) is left out.
func (r *resourceGenerator) writablePackages() map[string]packageDir {
	writable := map[string]packageDir{}
	for _, dir := range r.writableDirs() {
		if path, ok := r.packagePath(dir); ok {
			writable[path] = dir
		}
	}

	return writable
}

// writableDirs lists the directories of writablePackages, the configured ones first
// and the WithTypes packages in option order.
func (r *resourceGenerator) writableDirs() []packageDir {
	dirs := []packageDir{r.resource}
	if r.genVirtualResources {
		dirs = append(dirs, r.virtual)
	}
	if r.genComputedResources {
		dirs = append(dirs, r.computed)
	}
	if r.genRPCMethods {
		dirs = append(dirs, r.rpc)
	}

	return append(dirs, r.types...)
}

// unwritableRefusal is the message for a type needing a generated pair in a package the
// generator does not write into: what the pair does, where the type is, and the three
// fixes.
func (r *resourceGenerator) unwritableRefusal(res *resourceInfo, field *resourceField, carrier *types.Named, does string, methods []string) error {
	return errors.Newf("%s.%s: %s is %s by generated JSON methods, but it is declared in %s, where the generator writes nothing; declare the type in the resources package, implement %s on it, or name its package with WithTypes", r.pluralize(res.Name()), field.Name(), typeStringer(carrier), does, carrier.Obj().Pkg().Path(), strings.Join(methods, " and "))
}

// resolveColumnStorage finds every type the resources' columns hold that needs the
// generated storage methods, refusing the ones it cannot store: the type's declaring
// package must be one the generator writes into, and a table column must be JSON.
func (r *resourceGenerator) resolveColumnStorage() (map[packageDir][]string, error) {
	writable := r.writablePackages()

	storage := make(map[packageDir][]string)
	var errs []error
	for _, res := range r.resources {
		for _, field := range res.Fields {
			class, err := r.leaves().classifyColumn(field.GoType())
			if err != nil {
				// The TypeScript pass reports the type; storage has nothing to add.
				continue
			}
			carrier, needs, err := r.storageCarrier(res, field, class)
			if err != nil {
				errs = append(errs, err)

				continue
			}
			if !needs {
				continue
			}
			dir, ok := writable[carrier.Obj().Pkg().Path()]
			if !ok {
				errs = append(errs, r.unwritableRefusal(res, field, carrier, "stored", storageMethods))

				continue
			}
			if !res.IsVirtual && field.SpannerType != jsonSpannerType {
				errs = append(errs, errors.Newf("%s.%s: %s is stored by generated JSON methods, but column %s is %s; declare the column JSON, or implement EncodeSpanner and DecodeSpanner on the type", r.pluralize(res.Name()), field.Name(), typeStringer(carrier), fieldColumn(field), field.SpannerType))

				continue
			}
			if !slices.Contains(storage[dir], carrier.Obj().Name()) {
				storage[dir] = append(storage[dir], carrier.Obj().Name())
			}
		}
	}
	if len(errs) > 0 {
		return nil, errors.Wrapf(errors.Join(errs...), "encountered %d column storage errors", len(errs))
	}
	for _, names := range storage {
		slices.Sort(names)
	}

	return storage, nil
}

// storageCarrier reads the type a field's column stores: the named type after the
// pointer, when the column path derives it, a @typescript declaration types it, or it
// is JSON by its declaration (a defined type over json.RawMessage, or over any type
// with JSON methods), and it implements no Spanner methods of its own. A built-in row
// needs nothing, and a type with hand-written methods keeps them. A field typed by an
// unnamed slice of such a type has nothing for a method to attach to and is refused.
func (r *resourceGenerator) storageCarrier(res *resourceInfo, field *resourceField, class columnClass) (carrier *types.Named, needs bool, err error) {
	if class.Derive == nil {
		// A leaf. A built-in row is stored natively, and so is a declared type over a
		// basic type; a declared type over anything else, or a type JSON by
		// declaration (a named byte slice to the client, which would store BYTES), is
		// JSON the client cannot read or write on its own.
		if class.Leaf.Import == nil {
			byDeclaration, err := r.jsonByDeclaration(class.Named)
			if err != nil {
				return nil, false, errors.Wrapf(err, "%s.%s", r.pluralize(res.Name()), field.Name())
			}
			if !byDeclaration {
				return nil, false, nil
			}
		}
		if class.Carrier != nil {
			if _, basic := class.Carrier.Underlying().(*types.Basic); basic {
				return nil, false, nil
			}
		}
	}
	if class.Carrier == nil {
		return nil, false, errors.Newf("%s.%s: %s is stored as JSON, but an unnamed slice carries no methods; declare a named slice type for the column", r.pluralize(res.Name()), field.Name(), typeStringer(field.GoType()))
	}

	own, err := r.ownMethodPair(class.Carrier, storageMethods, "store")
	if err != nil {
		return nil, false, errors.Wrapf(err, "%s.%s", r.pluralize(res.Name()), field.Name())
	}

	return class.Carrier, !own, nil
}

// jsonByDeclaration reports whether the named type is JSON by its declaration: declared
// over a type with JSON methods (leafResolver.jsonRHS). False for nil.
func (r *resourceGenerator) jsonByDeclaration(named *types.Named) (bool, error) {
	if named == nil {
		return false, nil
	}
	over, err := r.leaves().jsonRHS(named)

	return over != nil, err
}

// ownMethodPair reports whether the type implements the method pair itself, in a file
// the generator does not own. The generated pair is not the type's own: the run that
// finds it regenerates it, so the output is a fixed point. One method without the
// other is refused; does names what the generated pair would do (store, carry).
func (r *resourceGenerator) ownMethodPair(named *types.Named, methods []string, does string) (bool, error) {
	var own int
	for _, name := range methods {
		obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(named), true, named.Obj().Pkg(), name)
		if fn, ok := obj.(*types.Func); ok && !r.generatedPosition(fn) {
			own++
		}
	}
	switch {
	case own == len(methods):
		return true, nil
	case own > 0:
		return false, errors.Newf("%s implements one of %s but not the other; implement both, or neither and let the generator %s the type", typeStringer(named), strings.Join(methods, " and "), does)
	default:
		return false, nil
	}
}

// generatedPosition reports whether the function is declared in a generated file.
func (r *resourceGenerator) generatedPosition(fn *types.Func) bool {
	pkg := fn.Pkg()
	if pkg == nil {
		return false
	}
	for _, loaded := range r.loadedPackages {
		if loaded.PkgPath != pkg.Path() || loaded.Fset == nil {
			continue
		}
		position := loaded.Fset.Position(fn.Pos())

		return strings.HasPrefix(filepath.Base(position.Filename), genPrefix)
	}

	return false
}

// packagePath is the import path of one of the generator's package directories, from
// the packages the run loaded.
func (r *resourceGenerator) packagePath(dir packageDir) (string, bool) {
	pkg, ok := r.loadedPackages[dir.Package()]
	if !ok {
		return "", false
	}

	return pkg.PkgPath, true
}

// generateStorageFiles writes one zz_gen_storage.go per package that declares a type
// needing the generated methods. A package with none gets no file (runMethodGeneration
// removed the previous one).
func (r *resourceGenerator) generateStorageFiles(storage map[packageDir][]string) error {
	for dir, names := range storage {
		begin := time.Now()
		destination := filepath.Join(dir.Dir(), generatedGoFileName(storageOutputName))
		if err := r.writeFormattedGoFile(destination, "storageFileTemplate", storageFileTemplate, &storageFileData{
			Source:  r.resource.Dir(),
			Package: dir.Package(),
			Types:   names,
		}); err != nil {
			return errors.Wrap(err, "writeFormattedGoFile()")
		}

		log.Printf("Generated storage methods in %s: %s\n", time.Since(begin), destination)
	}

	return nil
}
