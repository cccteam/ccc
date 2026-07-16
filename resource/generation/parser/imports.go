package parser

import (
	"go/types"
)

// Import identifies a package a type reference draws from: Name is the package's
// declared name (the qualifier that appears in rendered source, matching the
// qualifier produced by TypeInfo's Type/TypeName methods) and Path is its import path.
type Import struct {
	Name string
	Path string
}

// collectTypeImports records into seen, keyed by import path, the package of
// every named type reachable from tt. See TypeInfo.Imports.
func collectTypeImports(tt types.Type, seen map[string]Import) {
	switch t := tt.(type) {
	case *types.Pointer:
		collectTypeImports(t.Elem(), seen)
	case *types.Slice:
		collectTypeImports(t.Elem(), seen)
	case *types.Array:
		collectTypeImports(t.Elem(), seen)
	case *types.Map:
		collectTypeImports(t.Key(), seen)
		collectTypeImports(t.Elem(), seen)
	case *types.Chan:
		collectTypeImports(t.Elem(), seen)
	case *types.Alias:
		if pkg := t.Obj().Pkg(); pkg != nil {
			seen[pkg.Path()] = Import{Name: pkg.Name(), Path: pkg.Path()}
		}
		for typ := range t.TypeArgs().Types() {
			collectTypeImports(typ, seen)
		}
	case *types.Named:
		if pkg := t.Obj().Pkg(); pkg != nil {
			seen[pkg.Path()] = Import{Name: pkg.Name(), Path: pkg.Path()}
		}
		for typ := range t.TypeArgs().Types() {
			collectTypeImports(typ, seen)
		}
	}
}
