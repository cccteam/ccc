package generation

import (
	"go/types"
	"log"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/go-playground/errors/v5"
)

// The stores a generated package uses are checked and written once every kind that
// can name one is extracted, the RPC methods last: an @upload's store must be held by
// an @file column, a named store's type must be one the generated code can spell, and
// the resources package's FileHolders names every holder the orphaned-file cleanup
// reads a store's live keys through.

// validateStores runs every store check: the uploads against the columns, the store
// types against the loaded packages, and the computed holders against the resources
// package's imports.
func (r *resourceGenerator) validateStores() error {
	if err := r.validateUploadStores(); err != nil {
		return err
	}
	if err := r.validateStoreTypes(); err != nil {
		return err
	}

	return r.validateHolderStores()
}

// validateHolderStores refuses a computed resource's @file column naming a store whose
// package imports the resources package: the resources package's FileHolders names the
// store, and importing its package there would be a cycle. The fix is to declare the
// store in the resources package.
func (r *resourceGenerator) validateHolderStores() error {
	if !r.genComputedResources {
		return nil
	}
	resourcesPath := r.resourcesPackagePath()
	for _, res := range r.computedResources {
		for _, field := range res.FileKeyFields() {
			if field.Store == nil || field.Store.Obj().Pkg() == nil {
				continue
			}
			pkg := field.Store.Obj().Pkg()
			if pkg.Path() == resourcesPath || !importsPackage(pkg, resourcesPath, map[string]bool{}) {
				continue
			}

			return errors.Newf("computed resource %s: the @%s column %s names the store %s, declared in %s, which imports the resources package; the resources package's FileHolders names the store, so declare it in the resources package", res.Name(), fileKeyword, field.Name, field.StoreType(), pkg.Path())
		}
	}

	return nil
}

// importsPackage reports whether pkg imports the package at path, directly or through
// its imports.
func importsPackage(pkg *types.Package, path string, seen map[string]bool) bool {
	if seen[pkg.Path()] {
		return false
	}
	seen[pkg.Path()] = true
	for _, imported := range pkg.Imports() {
		if imported.Path() == path || importsPackage(imported, path, seen) {
			return true
		}
	}

	return false
}

// resourcesPackagePath is the import path of the resources package: read off its first
// table struct, or derived from the module when it declares none.
func (r *resourceGenerator) resourcesPackagePath() string {
	for _, res := range r.resources {
		if res.IsVirtual {
			continue
		}
		if pkg := typePackage(res.GoType()); pkg != nil {
			return pkg.Path()
		}
	}
	path, _ := r.outputPath(r.resource)

	return path
}

// typePackage is the package a named type is declared in, nil for any other type.
func typePackage(t types.Type) *types.Package {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil
	}

	return named.Obj().Pkg()
}

// storeExprFrom spells a store's name as a file of the package at fromPath does:
// resource.DefaultStore for the default, resource.StoreNameFor[T]() for a store declared
// in that package, and resource.StoreNameFor[pkg.T]() with pkg's import path otherwise.
func storeExprFrom(store *types.Named, fromPath string) (expr, importPath string) {
	if store == nil {
		return storeNameExpr(""), ""
	}
	pkg := store.Obj().Pkg()
	if pkg == nil || pkg.Path() == fromPath {
		return storeNameExpr(store.Obj().Name()), ""
	}

	return storeNameExpr(pkg.Name() + "." + store.Obj().Name()), pkg.Path()
}

// generateFileHolders writes the resources package's zz_gen_file_holders.go:
// FileHolders(), every resource whose rows record stored files' keys with the store each
// key column names. It is written on every application, so a job process calls it
// unconditionally. The table resources read their own keys (resource.FileHolderOf); a
// view's keys are its tables', so views are left out; a computed resource's rows come
// from application code, so its holder names the columns and the application supplies
// the keys.
func (r *resourceGenerator) generateFileHolders() error {
	begin := time.Now()
	destinationFilePath := filepath.Join(r.resource.Dir(), generatedGoFileName(fileHoldersOutputName))
	if err := r.writeFormattedGoFile(destinationFilePath, "fileHoldersTemplate", fileHoldersTemplate, r.fileHoldersData()); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated file holders file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

// fileHoldersData gathers the holders: the table resources with a stored @file and the
// computed ones with their key columns, each column's store spelled from the resources
// package.
func (r *resourceGenerator) fileHoldersData() *fileHoldersData {
	data := &fileHoldersData{Source: r.resource.Dir(), Package: r.resource.Package()}
	for _, res := range r.resources {
		if res.IsVirtual || len(res.FileKeyFields()) == 0 {
			continue
		}
		data.Tables = append(data.Tables, res.Name())
	}
	if r.genComputedResources {
		resourcesPath := r.resourcesPackagePath()
		for _, res := range r.computedResources {
			fields := res.FileKeyFields()
			if len(fields) == 0 {
				continue
			}
			holder := computedHolder{Resource: r.pluralize(res.Name())}
			for _, field := range fields {
				expr, importPath := storeExprFrom(field.Store, resourcesPath)
				if importPath != "" && !slices.Contains(data.Imports, importPath) {
					data.Imports = append(data.Imports, importPath)
				}
				holder.Keys = append(holder.Keys, computedHolderKey{Field: field.Name, StoreExpr: expr})
			}
			data.Computed = append(data.Computed, holder)
		}
		sort.Strings(data.Imports)
	}

	return data
}

// servedFileStores lists the stores the generated router requires on the resource
// client at start, each as the router spells its name and as the router test wires a
// stub under it, with the packages the named stores' types come from.
func (r *resourceGenerator) servedFileStores() (stores []servedFileStore, imports []string) {
	uses := r.storesUsed()
	stores = make([]servedFileStore, 0, len(uses))
	for _, use := range uses {
		option := "resource.WithFileStore(stubFileStore{})"
		if use.Store != nil {
			option = "resource.WithNamedFileStore[" + use.Qualified + "](stubFileStore{})"
			if pkg := use.Store.Obj().Pkg(); pkg != nil && !slices.Contains(imports, pkg.Path()) {
				imports = append(imports, pkg.Path())
			}
		}
		stores = append(stores, servedFileStore{NameExpr: use.NameExpr(), OptionExpr: option})
	}
	sort.Strings(imports)

	return stores, imports
}
